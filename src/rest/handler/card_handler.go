package handler

import (
	"context"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"
	"sync"
	"time"

	adapterethclient "chainora-api/adapter/ethclient"
	"chainora-api/core/constants"
	"chainora-api/core/entities"
	"chainora-api/core/usecases"
	"chainora-api/rest/handler/requests"
	"chainora-api/rest/handler/response"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	gethethclient "github.com/ethereum/go-ethereum/ethclient"
	"github.com/gin-gonic/gin"
	"github.com/go-playground/validator/v10"
)

const (
	cardCertBodyLength        = 83
	cardProofBodyLength       = 49
	cardChallengeTTL          = 60 * time.Second
	cardDeviceAttestationTTL  = 10 * time.Minute
	deviceAdapterReadABIJSON  = `[{"type":"function","name":"isDeviceVerified","stateMutability":"view","inputs":[{"name":"user","type":"address"}],"outputs":[{"name":"","type":"bool"}]},{"type":"function","name":"nextNonce","stateMutability":"view","inputs":[{"name":"user","type":"address"}],"outputs":[{"name":"","type":"uint256"}]},{"type":"function","name":"trustVerifier","stateMutability":"view","inputs":[{"name":"verifier","type":"address"}],"outputs":[{"name":"","type":"bool"}]}]`
	deviceAttestationTypeHash = "DeviceVerificationAttestation(address user,uint256 nonce,uint64 deadline)"
	eip712DomainTypeHash      = "EIP712Domain(string name,string version,uint256 chainId,address verifyingContract)"
	eip712Name                = "ChainoraDeviceAdapter"
	eip712Version             = "1"
)

var (
	bytes32ABIType = mustNewABIType("bytes32")
	addressABIType = mustNewABIType("address")
	uint256ABIType = mustNewABIType("uint256")
	uint64ABIType  = mustNewABIType("uint64")

	eip712DomainHash           = crypto.Keccak256Hash([]byte(eip712DomainTypeHash))
	deviceVerificationTypeHash = crypto.Keccak256Hash([]byte(deviceAttestationTypeHash))
	eip712NameHash             = crypto.Keccak256Hash([]byte(eip712Name))
	eip712VersionHash          = crypto.Keccak256Hash([]byte(eip712Version))
)

type createCardChallengeRequest struct {
	Address           string `json:"address" validate:"required,startswith=0x,len=42"`
	DeviceCertificate string `json:"deviceCertificate" validate:"required"`
}

type verifyCardProofRequest struct {
	ChallengeID      string `json:"challengeId" validate:"required"`
	AttestationProof string `json:"attestationProof" validate:"required"`
}

type createDeviceAttestationRequest struct {
	Address       string `json:"address" validate:"required,startswith=0x,len=42"`
	DeviceAdapter string `json:"deviceAdapter" validate:"required,startswith=0x,len=42"`
}

type createCardChallengeResponse struct {
	ChallengeID string `json:"challengeId"`
	Challenge   string `json:"challenge"`
	DeviceID    string `json:"deviceId"`
	ExpiresAt   string `json:"expiresAt"`
}

type verifyCardProofResponse struct {
	Verified   bool   `json:"verified"`
	Address    string `json:"address"`
	DeviceID   string `json:"deviceId"`
	VerifiedAt string `json:"verifiedAt"`
}

type deviceAttestationPayload struct {
	User     string `json:"user"`
	Nonce    string `json:"nonce"`
	Deadline string `json:"deadline"`
}

type createDeviceAttestationResponse struct {
	AlreadyVerified bool                     `json:"alreadyVerified"`
	Address         string                   `json:"address"`
	DeviceAdapter   string                   `json:"deviceAdapter"`
	ChainID         string                   `json:"chainId"`
	Signer          string                   `json:"signer"`
	Attestation     deviceAttestationPayload `json:"attestation"`
	Signature       string                   `json:"signature"`
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
	repo                 usecases.AuthRepository
	validate             *validator.Validate
	factoryRootPublicKey *ecdsa.PublicKey
	chainoraRPCURL       string
	deviceVerifierKey    *ecdsa.PrivateKey
	deviceVerifierAddr   common.Address
	deviceAdapterReadABI abi.ABI

	mu          sync.Mutex
	pendingByID map[string]pendingCardChallenge
}

func NewCardHandler(repo usecases.AuthRepository, factoryRootPublicKeyHex, chainoraRPCURL, deviceVerifierPrivateKeyHex string) (*CardHandler, error) {
	factoryBytes, err := decodeHexBytes(factoryRootPublicKeyHex)
	if err != nil {
		return nil, fmt.Errorf("parse factory root public key: %w", err)
	}

	factoryKey, err := crypto.UnmarshalPubkey(factoryBytes)
	if err != nil {
		return nil, fmt.Errorf("invalid factory root public key: %w", err)
	}

	var verifierKey *ecdsa.PrivateKey
	verifierAddr := common.Address{}
	if trimmedVerifierKey := strings.TrimSpace(deviceVerifierPrivateKeyHex); trimmedVerifierKey != "" {
		verifierKeyBytes, decodeErr := decodeHexBytes(trimmedVerifierKey)
		if decodeErr != nil {
			return nil, fmt.Errorf("parse card device verifier private key: %w", decodeErr)
		}

		parsedVerifierKey, parseErr := crypto.ToECDSA(verifierKeyBytes)
		if parseErr != nil {
			return nil, fmt.Errorf("invalid card device verifier private key: %w", parseErr)
		}
		verifierKey = parsedVerifierKey
		verifierAddr = crypto.PubkeyToAddress(parsedVerifierKey.PublicKey)
	}

	deviceAdapterReadABI, err := abi.JSON(strings.NewReader(deviceAdapterReadABIJSON))
	if err != nil {
		return nil, fmt.Errorf("parse device adapter read abi: %w", err)
	}

	return &CardHandler{
		repo:                 repo,
		validate:             validator.New(),
		factoryRootPublicKey: factoryKey,
		chainoraRPCURL:       strings.TrimSpace(chainoraRPCURL),
		deviceVerifierKey:    verifierKey,
		deviceVerifierAddr:   verifierAddr,
		deviceAdapterReadABI: deviceAdapterReadABI,
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
		Verified:   true,
		Address:    pending.Address,
		DeviceID:   pending.DeviceID,
		VerifiedAt: time.Now().UTC().Format(time.RFC3339),
	}))
}

func (h *CardHandler) CreateDeviceAttestation(ctx *gin.Context) {
	if h.deviceVerifierKey == nil {
		response.WriteError(ctx, fmt.Errorf("card device attestation signer is not configured"))
		return
	}
	if h.chainoraRPCURL == "" {
		response.WriteError(ctx, fmt.Errorf("chainora rpc url is not configured"))
		return
	}

	var req createDeviceAttestationRequest
	if err := requests.Serialize(ctx, &req); err != nil {
		response.WriteError(ctx, err)
		return
	}
	if err := h.validate.Struct(req); err != nil {
		response.WriteError(ctx, err)
		return
	}
	if !common.IsHexAddress(req.Address) || !common.IsHexAddress(req.DeviceAdapter) {
		response.WriteError(ctx, fmt.Errorf("invalid wallet or device adapter address"))
		return
	}

	normalizedAddress := strings.ToLower(strings.TrimSpace(req.Address))
	user, getErr := h.repo.GetUser(normalizedAddress)
	if getErr != nil || !user.IsHardwareVerified {
		response.WriteError(ctx, fmt.Errorf("%w: wallet is not hardware-verified in backend", constants.ErrForbidden))
		return
	}

	result, err := h.createOnChainDeviceAttestation(
		ctx.Request.Context(),
		common.HexToAddress(normalizedAddress),
		common.HexToAddress(strings.TrimSpace(req.DeviceAdapter)),
	)
	if err != nil {
		response.WriteError(ctx, err)
		return
	}

	response.Write(ctx.Writer, response.Ok(result))
}

func (h *CardHandler) createOnChainDeviceAttestation(
	ctx context.Context,
	walletAddress common.Address,
	deviceAdapterAddress common.Address,
) (createDeviceAttestationResponse, error) {
	client, err := adapterethclient.New(h.chainoraRPCURL)
	if err != nil {
		return createDeviceAttestationResponse{}, fmt.Errorf("connect chainora rpc: %w", err)
	}
	defer client.Close()

	callCtx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()

	chainID, err := client.ChainID(callCtx)
	if err != nil {
		return createDeviceAttestationResponse{}, fmt.Errorf("read chain id: %w", err)
	}

	alreadyVerified, err := h.readDeviceAdapterBool(callCtx, client, deviceAdapterAddress, "isDeviceVerified", walletAddress)
	if err != nil {
		return createDeviceAttestationResponse{}, err
	}
	if alreadyVerified {
		return createDeviceAttestationResponse{
			AlreadyVerified: true,
			Address:         strings.ToLower(walletAddress.Hex()),
			DeviceAdapter:   strings.ToLower(deviceAdapterAddress.Hex()),
			ChainID:         chainID.String(),
			Signer:          strings.ToLower(h.deviceVerifierAddr.Hex()),
			Attestation:     deviceAttestationPayload{},
			Signature:       "",
		}, nil
	}

	trusted, err := h.readDeviceAdapterBool(callCtx, client, deviceAdapterAddress, "trustVerifier", h.deviceVerifierAddr)
	if err != nil {
		return createDeviceAttestationResponse{}, err
	}
	if !trusted {
		return createDeviceAttestationResponse{}, fmt.Errorf(
			"%w: configured verifier %s is not trusted by device adapter %s",
			constants.ErrForbidden,
			strings.ToLower(h.deviceVerifierAddr.Hex()),
			strings.ToLower(deviceAdapterAddress.Hex()),
		)
	}

	nonce, err := h.readDeviceAdapterUint256(callCtx, client, deviceAdapterAddress, "nextNonce", walletAddress)
	if err != nil {
		return createDeviceAttestationResponse{}, err
	}
	if nonce == nil {
		nonce = big.NewInt(0)
	}

	deadline := uint64(time.Now().UTC().Add(cardDeviceAttestationTTL).Unix())
	signature, err := h.signDeviceAttestation(chainID, deviceAdapterAddress, walletAddress, nonce, deadline)
	if err != nil {
		return createDeviceAttestationResponse{}, err
	}

	return createDeviceAttestationResponse{
		AlreadyVerified: false,
		Address:         strings.ToLower(walletAddress.Hex()),
		DeviceAdapter:   strings.ToLower(deviceAdapterAddress.Hex()),
		ChainID:         chainID.String(),
		Signer:          strings.ToLower(h.deviceVerifierAddr.Hex()),
		Attestation: deviceAttestationPayload{
			User:     strings.ToLower(walletAddress.Hex()),
			Nonce:    nonce.String(),
			Deadline: fmt.Sprintf("%d", deadline),
		},
		Signature: "0x" + hex.EncodeToString(signature),
	}, nil
}

func (h *CardHandler) readDeviceAdapterBool(
	ctx context.Context,
	client *gethethclient.Client,
	deviceAdapterAddress common.Address,
	method string,
	arg any,
) (bool, error) {
	values, err := h.callDeviceAdapter(ctx, client, deviceAdapterAddress, method, arg)
	if err != nil {
		return false, err
	}
	if len(values) != 1 {
		return false, fmt.Errorf("unexpected %s output length", method)
	}

	flag, ok := values[0].(bool)
	if !ok {
		return false, fmt.Errorf("unexpected %s output type", method)
	}
	return flag, nil
}

func (h *CardHandler) readDeviceAdapterUint256(
	ctx context.Context,
	client *gethethclient.Client,
	deviceAdapterAddress common.Address,
	method string,
	arg any,
) (*big.Int, error) {
	values, err := h.callDeviceAdapter(ctx, client, deviceAdapterAddress, method, arg)
	if err != nil {
		return nil, err
	}
	if len(values) != 1 {
		return nil, fmt.Errorf("unexpected %s output length", method)
	}

	number, ok := values[0].(*big.Int)
	if !ok {
		return nil, fmt.Errorf("unexpected %s output type", method)
	}
	return number, nil
}

func (h *CardHandler) callDeviceAdapter(
	ctx context.Context,
	client *gethethclient.Client,
	deviceAdapterAddress common.Address,
	method string,
	args ...any,
) ([]any, error) {
	callData, err := h.deviceAdapterReadABI.Pack(method, args...)
	if err != nil {
		return nil, fmt.Errorf("encode %s call: %w", method, err)
	}

	raw, err := client.CallContract(ctx, ethereum.CallMsg{
		To:   &deviceAdapterAddress,
		Data: callData,
	}, nil)
	if err != nil {
		return nil, fmt.Errorf("call device adapter %s: %w", method, err)
	}

	values, err := h.deviceAdapterReadABI.Unpack(method, raw)
	if err != nil {
		return nil, fmt.Errorf("decode device adapter %s response: %w", method, err)
	}

	return values, nil
}

func (h *CardHandler) signDeviceAttestation(
	chainID *big.Int,
	deviceAdapterAddress common.Address,
	walletAddress common.Address,
	nonce *big.Int,
	deadline uint64,
) ([]byte, error) {
	domainEncoded, err := abi.Arguments{
		{Type: bytes32ABIType},
		{Type: bytes32ABIType},
		{Type: bytes32ABIType},
		{Type: uint256ABIType},
		{Type: addressABIType},
	}.Pack(
		eip712DomainHash,
		eip712NameHash,
		eip712VersionHash,
		chainID,
		deviceAdapterAddress,
	)
	if err != nil {
		return nil, fmt.Errorf("encode eip712 domain: %w", err)
	}

	structEncoded, err := abi.Arguments{
		{Type: bytes32ABIType},
		{Type: addressABIType},
		{Type: uint256ABIType},
		{Type: uint64ABIType},
	}.Pack(
		deviceVerificationTypeHash,
		walletAddress,
		nonce,
		deadline,
	)
	if err != nil {
		return nil, fmt.Errorf("encode attestation struct: %w", err)
	}

	domainSeparator := crypto.Keccak256Hash(domainEncoded)
	structHash := crypto.Keccak256Hash(structEncoded)
	digest := crypto.Keccak256Hash([]byte{0x19, 0x01}, domainSeparator.Bytes(), structHash.Bytes())

	sig, err := crypto.Sign(digest.Bytes(), h.deviceVerifierKey)
	if err != nil {
		return nil, fmt.Errorf("sign device attestation: %w", err)
	}

	return sig, nil
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

func mustNewABIType(typeName string) abi.Type {
	typed, err := abi.NewType(typeName, "", nil)
	if err != nil {
		panic(err)
	}
	return typed
}
