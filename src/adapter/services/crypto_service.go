package services

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"chainora-api/core/usecases"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/signer/core/apitypes"
)

// CryptoService verifies EIP-191 signatures using secp256k1 recovery.
type CryptoService struct{}

func NewCryptoService() *CryptoService {
	return &CryptoService{}
}

func (s *CryptoService) VerifyEIP191Signature(message, expectedAddress, signatureHex string, recoveryV *int) (usecases.VerificationResult, error) {
	hash := eip191Hash(message)
	candidates, err := buildSignatureCandidates(signatureHex, recoveryV)
	if err != nil {
		return usecases.VerificationResult{}, err
	}

	target := common.HexToAddress(expectedAddress)
	for _, sig := range candidates {
		pubKey, recoverErr := crypto.SigToPub(hash, sig)
		if recoverErr != nil {
			continue
		}

		recoveredAddress := crypto.PubkeyToAddress(*pubKey)
		if !strings.EqualFold(recoveredAddress.Hex(), target.Hex()) {
			continue
		}

		pubKeyBytes := crypto.FromECDSAPub(pubKey)
		return usecases.VerificationResult{
			Valid:     true,
			Address:   recoveredAddress.Hex(),
			PublicKey: "0x" + hex.EncodeToString(pubKeyBytes),
		}, nil
	}

	return usecases.VerificationResult{Valid: false}, nil
}

func (s *CryptoService) VerifyEIP712Signature(typedDataJSON, expectedAddress, signatureHex string, recoveryV *int) (usecases.VerificationResult, error) {
	var typedData apitypes.TypedData
	if err := json.Unmarshal([]byte(typedDataJSON), &typedData); err != nil {
		return usecases.VerificationResult{}, fmt.Errorf("decode typed data: %w", err)
	}

	hash, _, err := apitypes.TypedDataAndHash(typedData)
	if err != nil {
		return usecases.VerificationResult{}, fmt.Errorf("typed data hash: %w", err)
	}

	candidates, err := buildSignatureCandidates(signatureHex, recoveryV)
	if err != nil {
		return usecases.VerificationResult{}, err
	}

	target := common.HexToAddress(expectedAddress)
	for _, sig := range candidates {
		pubKey, recoverErr := crypto.SigToPub(hash, sig)
		if recoverErr != nil {
			continue
		}

		recoveredAddress := crypto.PubkeyToAddress(*pubKey)
		if !strings.EqualFold(recoveredAddress.Hex(), target.Hex()) {
			continue
		}

		pubKeyBytes := crypto.FromECDSAPub(pubKey)
		return usecases.VerificationResult{
			Valid:     true,
			Address:   recoveredAddress.Hex(),
			PublicKey: "0x" + hex.EncodeToString(pubKeyBytes),
		}, nil
	}

	return usecases.VerificationResult{Valid: false}, nil
}

func eip191Hash(message string) []byte {
	msg := []byte(message)
	prefix := fmt.Sprintf("\x19Ethereum Signed Message:\n%d", len(msg))
	return crypto.Keccak256([]byte(prefix), msg)
}

func buildSignatureCandidates(signatureHex string, recoveryV *int) ([][]byte, error) {
	raw := strings.TrimPrefix(strings.TrimSpace(signatureHex), "0x")
	sig, err := hex.DecodeString(raw)
	if err != nil {
		return nil, fmt.Errorf("decode signature: %w", err)
	}

	switch len(sig) {
	case 64:
		if recoveryV != nil {
			v, vErr := normalizeRecoveryV(byte(*recoveryV))
			if vErr != nil {
				return nil, vErr
			}
			full := make([]byte, 65)
			copy(full, sig)
			full[64] = v
			return [][]byte{full}, nil
		}

		candidate0 := make([]byte, 65)
		candidate1 := make([]byte, 65)
		copy(candidate0, sig)
		copy(candidate1, sig)
		candidate0[64] = 0
		candidate1[64] = 1
		return [][]byte{candidate0, candidate1}, nil
	case 65:
		providedV, vErr := normalizeRecoveryV(sig[64])
		if vErr != nil {
			return nil, vErr
		}
		candidate := make([]byte, 65)
		copy(candidate, sig)
		candidate[64] = providedV
		return [][]byte{candidate}, nil
	default:
		return nil, fmt.Errorf("signature must be 64 (r||s) or 65 (r||s||v) bytes")
	}
}

func normalizeRecoveryV(v byte) (byte, error) {
	switch v {
	case 0, 1:
		return v, nil
	case 27, 28:
		return v - 27, nil
	default:
		return 0, fmt.Errorf("invalid recovery v: %d", v)
	}
}
