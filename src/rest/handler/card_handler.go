package handler

import (
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"time"

	"chainora-api/core/constants"
	"chainora-api/core/entities"
	"chainora-api/core/usecases"
	"chainora-api/rest/handler/requests"
	"chainora-api/rest/handler/response"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/gin-gonic/gin"
	"github.com/go-playground/validator/v10"
)

const (
	cardCertBodyLength  = 83
	cardProofBodyLength = 49
	cardChallengeTTL    = 60 * time.Second
)

type createCardChallengeRequest struct {
	Address           string `json:"address" validate:"required,startswith=0x,len=42"`
	DeviceCertificate string `json:"deviceCertificate" validate:"required"`
}

type verifyCardProofRequest struct {
	ChallengeID      string `json:"challengeId" validate:"required"`
	AttestationProof string `json:"attestationProof" validate:"required"`
}

type createCardChallengeResponse struct {
	ChallengeID string `json:"challengeId"`
	Challenge   string `json:"challenge"`
	DeviceID    string `json:"deviceId"`
	ExpiresAt   string `json:"expiresAt"`
}

type verifyCardProofResponse struct {
	Verified  bool   `json:"verified"`
	Address   string `json:"address"`
	DeviceID  string `json:"deviceId"`
	VerifiedAt string `json:"verifiedAt"`
}

type pendingCardChallenge struct {
	Address             string
	DeviceID            string
	ChallengeHex        string
	DeviceAuthPublicKey []byte
	ExpiresAt           time.Time
	Used                bool
}

type CardHandler struct {
	repo                usecases.AuthRepository
	validate            *validator.Validate
	factoryRootPublicKey *ecdsa.PublicKey

	mu        sync.Mutex
	pendingByID map[string]pendingCardChallenge
}

func NewCardHandler(repo usecases.AuthRepository, factoryRootPublicKeyHex string) (*CardHandler, error) {
	factoryBytes, err := decodeHexBytes(factoryRootPublicKeyHex)
	if err != nil {
		return nil, fmt.Errorf("parse factory root public key: %w", err)
	}

	factoryKey, err := crypto.UnmarshalPubkey(factoryBytes)
	if err != nil {
		return nil, fmt.Errorf("invalid factory root public key: %w", err)
	}

	return &CardHandler{
		repo:                 repo,
		validate:             validator.New(),
		factoryRootPublicKey: factoryKey,
		pendingByID:          make(map[string]pendingCardChallenge),
	}, nil
}

func (h *CardHandler) CreateChallenge(ctx *gin.Context) {
	var req createCardChallengeRequest
	if err := requests.Serialize(ctx, &req); err != nil {
		response.WriteError(ctx, err)
		return
	}
	if err := h.validate.Struct(req); err != nil {
		response.WriteError(ctx, err)
		return
	}

	certBytes, err := decodeHexBytes(req.DeviceCertificate)
	if err != nil {
		response.WriteError(ctx, fmt.Errorf("invalid device certificate hex"))
		return
	}

	certDeviceID, certDevicePubKey, verifyErr := h.verifyCertificate(certBytes)
	if verifyErr != nil {
		response.WriteError(ctx, verifyErr)
		return
	}

	challenge := make([]byte, 32)
	if _, err := rand.Read(challenge); err != nil {
		response.WriteError(ctx, err)
		return
	}

	challengeIDBytes := make([]byte, 16)
	if _, err := rand.Read(challengeIDBytes); err != nil {
		response.WriteError(ctx, err)
		return
	}

	challengeID := hex.EncodeToString(challengeIDBytes)
	challengeHex := hex.EncodeToString(challenge)
	deviceIDHex := hex.EncodeToString(certDeviceID)
	expiresAt := time.Now().UTC().Add(cardChallengeTTL)

	h.mu.Lock()
	h.pendingByID[challengeID] = pendingCardChallenge{
		Address:             strings.ToLower(strings.TrimSpace(req.Address)),
		DeviceID:            deviceIDHex,
		ChallengeHex:        challengeHex,
		DeviceAuthPublicKey: certDevicePubKey,
		ExpiresAt:           expiresAt,
		Used:                false,
	}
	h.mu.Unlock()

	response.Write(ctx.Writer, response.Ok(createCardChallengeResponse{
		ChallengeID: challengeID,
		Challenge:   challengeHex,
		DeviceID:    deviceIDHex,
		ExpiresAt:   expiresAt.Format(time.RFC3339),
	}))
}

func (h *CardHandler) VerifyChallenge(ctx *gin.Context) {
	var req verifyCardProofRequest
	if err := requests.Serialize(ctx, &req); err != nil {
		response.WriteError(ctx, err)
		return
	}
	if err := h.validate.Struct(req); err != nil {
		response.WriteError(ctx, err)
		return
	}

	h.mu.Lock()
	pending, ok := h.pendingByID[strings.TrimSpace(req.ChallengeID)]
	h.mu.Unlock()
	if !ok {
		response.WriteError(ctx, constants.ErrSessionExpired)
		return
	}
	if pending.Used {
		response.WriteError(ctx, constants.ErrForbidden)
		return
	}
	if time.Now().UTC().After(pending.ExpiresAt) {
		h.mu.Lock()
		delete(h.pendingByID, strings.TrimSpace(req.ChallengeID))
		h.mu.Unlock()
		response.WriteError(ctx, constants.ErrSessionExpired)
		return
	}

	proofBytes, err := decodeHexBytes(req.AttestationProof)
	if err != nil {
		response.WriteError(ctx, fmt.Errorf("invalid attestation proof hex"))
		return
	}

	proofDeviceID, proofChallenge, verifyErr := verifyAttestationProof(proofBytes, pending.DeviceAuthPublicKey)
	if verifyErr != nil {
		response.WriteError(ctx, verifyErr)
		return
	}
	if proofDeviceID != pending.DeviceID || proofChallenge != pending.ChallengeHex {
		response.WriteError(ctx, fmt.Errorf("%w: proof payload mismatch", constants.ErrForbidden))
		return
	}

	user, getErr := h.repo.GetUser(pending.Address)
	if getErr != nil {
		user = entities.User{Address: pending.Address}
	}
	user.IsHardwareVerified = true
	if err := h.repo.UpsertUser(user); err != nil {
		response.WriteError(ctx, err)
		return
	}

	h.mu.Lock()
	pending.Used = true
	h.pendingByID[strings.TrimSpace(req.ChallengeID)] = pending
	h.mu.Unlock()

	response.Write(ctx.Writer, response.Ok(verifyCardProofResponse{
		Verified:  true,
		Address:   pending.Address,
		DeviceID:  pending.DeviceID,
		VerifiedAt: time.Now().UTC().Format(time.RFC3339),
	}))
}

func (h *CardHandler) verifyCertificate(certificate []byte) ([]byte, []byte, error) {
	if len(certificate) < cardCertBodyLength+1 {
		return nil, nil, fmt.Errorf("invalid certificate length")
	}

	if certificate[0] != 0x01 {
		return nil, nil, fmt.Errorf("invalid certificate version")
	}

	devicePubKey := certificate[17:82]
	if len(devicePubKey) != 65 || devicePubKey[0] != 0x04 {
		return nil, nil, fmt.Errorf("invalid device auth public key")
	}

	capabilities := certificate[82]
	if capabilities&0x01 == 0 {
		return nil, nil, fmt.Errorf("backup capability missing in certificate")
	}

	sigLen := int(certificate[83])
	if sigLen <= 0 || len(certificate) != cardCertBodyLength+1+sigLen {
		return nil, nil, fmt.Errorf("invalid certificate signature length")
	}

	body := certificate[:cardCertBodyLength]
	signature := certificate[84:]
	digest := sha256.Sum256(body)
	if !ecdsa.VerifyASN1(h.factoryRootPublicKey, digest[:], signature) {
		return nil, nil, fmt.Errorf("invalid certificate signature")
	}

	deviceID := make([]byte, 16)
	copy(deviceID, certificate[1:17])
	pubCopy := make([]byte, 65)
	copy(pubCopy, devicePubKey)

	return deviceID, pubCopy, nil
}

func verifyAttestationProof(proof []byte, deviceAuthPublicKey []byte) (string, string, error) {
	if len(proof) < cardProofBodyLength+1 {
		return "", "", fmt.Errorf("invalid attestation proof length")
	}
	if proof[0] != 0x01 {
		return "", "", fmt.Errorf("invalid attestation proof version")
	}

	sigLen := int(proof[49])
	if sigLen <= 0 || len(proof) != cardProofBodyLength+1+sigLen {
		return "", "", fmt.Errorf("invalid attestation proof signature length")
	}

	pubKey, err := crypto.UnmarshalPubkey(deviceAuthPublicKey)
	if err != nil {
		return "", "", fmt.Errorf("invalid device auth public key")
	}

	body := proof[:cardProofBodyLength]
	signature := proof[50:]
	digest := sha256.Sum256(body)
	if !ecdsa.VerifyASN1(pubKey, digest[:], signature) {
		return "", "", fmt.Errorf("invalid attestation proof signature")
	}

	deviceIDHex := hex.EncodeToString(proof[1:17])
	challengeHex := hex.EncodeToString(proof[17:49])
	return deviceIDHex, challengeHex, nil
}

func decodeHexBytes(value string) ([]byte, error) {
	trimmed := strings.TrimSpace(value)
	trimmed = strings.TrimPrefix(trimmed, "0x")
	trimmed = strings.TrimPrefix(trimmed, "0X")
	if trimmed == "" {
		return nil, fmt.Errorf("hex value is empty")
	}
	if len(trimmed)%2 != 0 {
		return nil, fmt.Errorf("hex value has odd length")
	}
	decoded, err := hex.DecodeString(trimmed)
	if err != nil {
		return nil, err
	}
	return decoded, nil
}
