package usecases

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"chainora-api/core/constants"
	"chainora-api/core/entities"
)

type EIP712SignatureVerifier interface {
	VerifyEIP712Signature(typedDataJSON, expectedAddress, signatureHex string, recoveryV *int) (VerificationResult, error)
}

type EIP712AuthUsecase interface {
	BuildLoginTypedData(sessionID, address, chainID, verifyingContract string) (EIP712TypedData, error)
	AuthenticateTypedData(sessionID, address, typedDataJSON, signatureHex string, recoveryV *int) (entities.User, error)
}

type eip712AuthInteractor struct {
	repo     AuthRepository
	verifier EIP712SignatureVerifier
	now      func() time.Time
}

func NewEIP712AuthInteractor(repo AuthRepository, verifier EIP712SignatureVerifier) EIP712AuthUsecase {
	return &eip712AuthInteractor{repo: repo, verifier: verifier, now: time.Now}
}

func (u *eip712AuthInteractor) BuildLoginTypedData(sessionID, address, chainID, verifyingContract string) (EIP712TypedData, error) {
	session, err := u.repo.GetSession(sessionID)
	if err != nil {
		return EIP712TypedData{}, fmt.Errorf("get session: %w", err)
	}
	if u.now().UTC().After(session.CreatedAt.UTC().Add(constants.SessionTimeout)) {
		_ = u.repo.DeleteSession(sessionID)
		return EIP712TypedData{}, constants.ErrSessionExpired
	}

	if strings.TrimSpace(chainID) == "" {
		chainID = "1123337227327254"
	}
	if strings.TrimSpace(verifyingContract) == "" {
		verifyingContract = "0x0000000000000000000000000000000000000000"
	}

	return EIP712TypedData{
		Types: map[string][]EIP712Field{
			"EIP712Domain": {
				{Name: "name", Type: "string"},
				{Name: "version", Type: "string"},
				{Name: "chainId", Type: "uint256"},
				{Name: "verifyingContract", Type: "address"},
			},
			"LoginChallenge": {
				{Name: "sessionId", Type: "string"},
				{Name: "nonce", Type: "string"},
				{Name: "wallet", Type: "address"},
				{Name: "issuedAt", Type: "string"},
			},
		},
		PrimaryType: "LoginChallenge",
		Domain: EIP712Domain{
			Name:              "Chainora",
			Version:           "1",
			ChainID:           chainID,
			VerifyingContract: verifyingContract,
		},
		Message: map[string]any{
			"sessionId": session.ID,
			"nonce":     session.Nonce,
			"wallet":    address,
			"issuedAt":  session.CreatedAt.UTC().Format(time.RFC3339),
		},
	}, nil
}

func (u *eip712AuthInteractor) AuthenticateTypedData(sessionID, address, typedDataJSON, signatureHex string, recoveryV *int) (entities.User, error) {
	session, err := u.repo.GetSession(sessionID)
	if err != nil {
		return entities.User{}, fmt.Errorf("get session: %w", err)
	}
	if u.now().UTC().After(session.CreatedAt.UTC().Add(constants.SessionTimeout)) {
		_ = u.repo.DeleteSession(sessionID)
		return entities.User{}, constants.ErrSessionExpired
	}

	result, err := u.verifier.VerifyEIP712Signature(typedDataJSON, address, signatureHex, recoveryV)
	if err != nil {
		return entities.User{}, fmt.Errorf("verify typed signature: %w", err)
	}
	if !result.Valid {
		return entities.User{}, constants.ErrInvalidSignature
	}

	resolvedAddress := strings.TrimSpace(result.Address)
	if resolvedAddress == "" {
		resolvedAddress = strings.TrimSpace(address)
	}

	user := entities.User{
		Address:   resolvedAddress,
		PublicKey: strings.TrimSpace(result.PublicKey),
		LastLogin: u.now().UTC(),
	}
	if err := u.repo.UpdateUser(user); err != nil {
		return entities.User{}, fmt.Errorf("update user: %w", err)
	}

	session.Address = user.Address
	if err := u.repo.SaveSession(session); err != nil {
		return entities.User{}, fmt.Errorf("update session: %w", err)
	}
	if err := u.repo.DeleteSession(sessionID); err != nil {
		return entities.User{}, fmt.Errorf("consume session: %w", err)
	}

	return user, nil
}

func MarshalTypedDataJSON(data EIP712TypedData) (string, error) {
	bytes, err := json.Marshal(data)
	if err != nil {
		return "", err
	}
	return string(bytes), nil
}
