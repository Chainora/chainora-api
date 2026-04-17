package usecases

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"chainora-api/core/constants"
	"chainora-api/core/entities"
	"chainora-api/core/properties"
)

// AuthRepository defines persistence operations required by auth usecases.
type AuthRepository interface {
	SaveSession(session entities.AuthSession) error
	GetSession(sessionID string) (entities.AuthSession, error)
	GetUser(address string) (entities.User, error)
	UsernameExists(username string) (bool, error)
	UpsertUser(user entities.User) error
	UpdateUser(user entities.User) error
	DeleteSession(sessionID string) error
}

// VerificationResult is returned from signature verification service.
type VerificationResult struct {
	Valid     bool
	Address   string
	PublicKey string
}

// SignatureVerifier abstracts cryptographic verification.
type SignatureVerifier interface {
	VerifyEIP191Signature(message, expectedAddress, signatureHex string, recoveryV *int) (VerificationResult, error)
}

// AuthUsecase defines QR sign-in usecase operations.
type AuthUsecase interface {
	GenerateLoginSession() (entities.AuthSession, error)
	ValidateLoginSession(sessionID string) error
	AuthenticateUser(sessionID, address, signatureHex, username string, recoveryV *int) (entities.User, error)
	GetUserProfile(address string) (entities.User, error)
	UpdateUserAvatar(address, avatarURL string) (entities.User, error)
}

type authUsecase struct {
	repo     AuthRepository
	verifier SignatureVerifier
	props    properties.AuthProperties
	now      func() time.Time
}

func NewAuthUsecase(repo AuthRepository, verifier SignatureVerifier, props properties.AuthProperties) AuthUsecase {
	if strings.TrimSpace(props.AuthMessageTemplate) == "" {
		props.AuthMessageTemplate = "Sign this to login to Chainora: %s"
	}

	return &authUsecase{
		repo:     repo,
		verifier: verifier,
		props:    props,
		now:      time.Now,
	}
}

func (u *authUsecase) GenerateLoginSession() (entities.AuthSession, error) {
	nonceRaw := make([]byte, constants.NonceLength)
	if _, err := rand.Read(nonceRaw); err != nil {
		return entities.AuthSession{}, fmt.Errorf("generate nonce: %w", err)
	}
	idRaw := make([]byte, 16)
	if _, err := rand.Read(idRaw); err != nil {
		return entities.AuthSession{}, fmt.Errorf("generate session id: %w", err)
	}

	session := entities.AuthSession{
		ID:        hex.EncodeToString(idRaw),
		Nonce:     hex.EncodeToString(nonceRaw),
		CreatedAt: u.now().UTC(),
	}

	if err := u.repo.SaveSession(session); err != nil {
		return entities.AuthSession{}, fmt.Errorf("save session: %w", err)
	}
	return session, nil
}

func (u *authUsecase) ValidateLoginSession(sessionID string) error {
	session, err := u.repo.GetSession(sessionID)
	if err != nil {
		return fmt.Errorf("get session: %w", err)
	}

	if u.now().UTC().After(session.CreatedAt.UTC().Add(constants.SessionTimeout)) {
		_ = u.repo.DeleteSession(sessionID)
		return constants.ErrSessionExpired
	}

	return nil
}

func (u *authUsecase) AuthenticateUser(sessionID, address, signatureHex, username string, recoveryV *int) (entities.User, error) {
	session, err := u.repo.GetSession(sessionID)
	if err != nil {
		return entities.User{}, fmt.Errorf("get session: %w", err)
	}

	if u.now().UTC().After(session.CreatedAt.UTC().Add(constants.SessionTimeout)) {
		_ = u.repo.DeleteSession(sessionID)
		return entities.User{}, constants.ErrSessionExpired
	}

	raw := strings.TrimPrefix(strings.TrimSpace(signatureHex), "0x")
	if len(raw) < constants.MinSignatureLength*2 {
		return entities.User{}, constants.ErrInvalidSignature
	}

	message := fmt.Sprintf(u.props.AuthMessageTemplate, session.Nonce)
	result, err := u.verifier.VerifyEIP191Signature(message, address, signatureHex, recoveryV)
	if err != nil {
		return entities.User{}, fmt.Errorf("verify signature: %w", err)
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
		Username:  strings.TrimSpace(username),
		PublicKey: strings.TrimSpace(result.PublicKey),
		LastLogin: u.now().UTC(),
	}

	if err := u.repo.UpsertUser(user); err != nil {
		if errors.Is(err, constants.ErrUserNotFound) {
			return entities.User{}, constants.ErrUserNotFound
		}
		return entities.User{}, fmt.Errorf("upsert user: %w", err)
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

func (u *authUsecase) GetUserProfile(address string) (entities.User, error) {
	resolvedAddress := strings.ToLower(strings.TrimSpace(address))
	if resolvedAddress == "" {
		return entities.User{}, constants.ErrUserNotFound
	}

	user, err := u.repo.GetUser(resolvedAddress)
	if err == nil {
		return user, nil
	}

	if !errors.Is(err, constants.ErrUserNotFound) {
		return entities.User{}, fmt.Errorf("get user: %w", err)
	}

	return entities.User{}, constants.ErrUserNotFound
}

func (u *authUsecase) UpdateUserAvatar(address, avatarURL string) (entities.User, error) {
	resolvedAddress := strings.ToLower(strings.TrimSpace(address))
	if resolvedAddress == "" {
		return entities.User{}, constants.ErrUserNotFound
	}

	resolvedAvatarURL := strings.TrimSpace(avatarURL)
	if resolvedAvatarURL == "" {
		return entities.User{}, fmt.Errorf("avatarUrl is required")
	}

	parsedURL, parseErr := url.Parse(resolvedAvatarURL)
	if parseErr != nil || parsedURL == nil || parsedURL.Scheme == "" || parsedURL.Host == "" {
		return entities.User{}, fmt.Errorf("avatarUrl must be a valid URL")
	}

	scheme := strings.ToLower(strings.TrimSpace(parsedURL.Scheme))
	if scheme != "http" && scheme != "https" {
		return entities.User{}, fmt.Errorf("avatarUrl scheme must be http or https")
	}

	user, err := u.repo.GetUser(resolvedAddress)
	if err != nil {
		if !errors.Is(err, constants.ErrUserNotFound) {
			return entities.User{}, fmt.Errorf("get user: %w", err)
		}

		user = entities.User{Address: resolvedAddress, LastLogin: u.now().UTC()}
	}

	user.Address = resolvedAddress
	user.AvatarURL = resolvedAvatarURL
	if user.LastLogin.IsZero() {
		user.LastLogin = u.now().UTC()
	}

	if upsertErr := u.repo.UpsertUser(user); upsertErr != nil {
		return entities.User{}, fmt.Errorf("update avatar: %w", upsertErr)
	}

	updated, updatedErr := u.repo.GetUser(resolvedAddress)
	if updatedErr == nil {
		return updated, nil
	}

	return user, nil
}
