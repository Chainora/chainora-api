package usecases

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"chainora-api/core/constants"
	entityrequest "chainora-api/core/entities/request"
	entityresponse "chainora-api/core/entities/response"
)

var usernamePattern = regexp.MustCompile(`^[a-zA-Z0-9_.-]{3,20}$`)

type UsernameRelayerService interface {
	RegisterUsername(ctx context.Context, address string, username string) (string, error)
	SetPrimaryUsername(ctx context.Context, address string, username string) (string, error)
}

type RelayerUsecase interface {
	CreateUnsignedPayload(ctx context.Context, req entityrequest.CreateRelayerPayloadRequest, requestKey string) (entityresponse.UnsignedTxPayloadResponse, error)
	CreatePrimaryPayload(ctx context.Context, req entityrequest.CreatePrimaryPayloadRequest, requestKey string) (entityresponse.UnsignedTxPayloadResponse, error)
	RegisterUsername(ctx context.Context, req entityrequest.RegisterUsernameRelayerRequest, requestKey string) (entityresponse.RegisterUsernameRelayerResponse, error)
	SetPrimaryUsername(ctx context.Context, req entityrequest.SetPrimaryUsernameRelayerRequest, requestKey string) (entityresponse.SetPrimaryUsernameRelayerResponse, error)
	ValidateRelayerSession(sessionID string) error
	BuildRelayerMessage(username, sessionID string) string
	BuildPrimaryRelayerMessage(username, sessionID string) string
}

type relayerSession struct {
	SessionID string
	Address   string
	Username  string
	Feature   string
	CreatedAt time.Time
}

type relayerUsecase struct {
	repo                 AuthRepository
	verifier             SignatureVerifier
	relayer              UsernameRelayerService
	relayerMessageFormat string
	primaryMessageFormat string
	rateLimitWindow      time.Duration
	sessionTimeout       time.Duration

	mu              sync.Mutex
	sessions        map[string]relayerSession
	lastRequestAt   map[string]time.Time
	lastRequestByIP map[string]time.Time
}

func NewRelayerUsecase(repo AuthRepository, verifier SignatureVerifier, relayer UsernameRelayerService, messageFormat string, primaryMessageFormat string) RelayerUsecase {
	trimmed := strings.TrimSpace(messageFormat)
	if trimmed == "" {
		trimmed = "Register Chainora username '%s' (session: %s)"
	}

	primaryTrimmed := strings.TrimSpace(primaryMessageFormat)
	if primaryTrimmed == "" {
		primaryTrimmed = "Set Chainora primary username '%s' (session: %s)"
	}

	return &relayerUsecase{
		repo:                 repo,
		verifier:             verifier,
		relayer:              relayer,
		relayerMessageFormat: trimmed,
		primaryMessageFormat: primaryTrimmed,
		rateLimitWindow:      5 * time.Second,
		sessionTimeout:       10 * time.Minute,
		sessions:             make(map[string]relayerSession),
		lastRequestAt:        make(map[string]time.Time),
		lastRequestByIP:      make(map[string]time.Time),
	}
}

func (u *relayerUsecase) BuildRelayerMessage(username, sessionID string) string {
	return fmt.Sprintf(u.relayerMessageFormat, strings.TrimSpace(username), strings.TrimSpace(sessionID))
}

func (u *relayerUsecase) BuildPrimaryRelayerMessage(username, sessionID string) string {
	return fmt.Sprintf(u.primaryMessageFormat, strings.TrimSpace(username), strings.TrimSpace(sessionID))
}

func (u *relayerUsecase) ValidateRelayerSession(sessionID string) error {
	u.mu.Lock()
	defer u.mu.Unlock()

	s, ok := u.sessions[strings.TrimSpace(sessionID)]
	if !ok {
		return fmt.Errorf("%w: relayer session not found", constants.ErrSessionExpired)
	}

	if time.Now().UTC().After(s.CreatedAt.Add(u.sessionTimeout)) {
		delete(u.sessions, s.SessionID)
		return constants.ErrSessionExpired
	}

	return nil
}

func (u *relayerUsecase) CreateUnsignedPayload(
	_ context.Context,
	req entityrequest.CreateRelayerPayloadRequest,
	requestKey string,
) (entityresponse.UnsignedTxPayloadResponse, error) {
	address := strings.ToLower(strings.TrimSpace(req.Address))
	username := strings.TrimSpace(req.Username)
	if err := validateUsername(username); err != nil {
		return entityresponse.UnsignedTxPayloadResponse{}, err
	}

	if err := u.enforceRateLimit(address, requestKey); err != nil {
		return entityresponse.UnsignedTxPayloadResponse{}, err
	}

	sessionID, err := generateSessionID()
	if err != nil {
		return entityresponse.UnsignedTxPayloadResponse{}, err
	}

	u.mu.Lock()
	u.sessions[sessionID] = relayerSession{
		SessionID: sessionID,
		Address:   address,
		Username:  username,
		Feature:   "username.register",
		CreatedAt: time.Now().UTC(),
	}
	u.mu.Unlock()

	return entityresponse.UnsignedTxPayloadResponse{
		Feature:   "username.register",
		SessionID: sessionID,
		Address:   address,
		Username:  username,
		Message:   u.BuildRelayerMessage(username, sessionID),
	}, nil
}

func (u *relayerUsecase) CreatePrimaryPayload(
	_ context.Context,
	req entityrequest.CreatePrimaryPayloadRequest,
	requestKey string,
) (entityresponse.UnsignedTxPayloadResponse, error) {
	address := strings.ToLower(strings.TrimSpace(req.Address))
	username := strings.TrimSpace(req.Username)
	if err := validateUsername(username); err != nil {
		return entityresponse.UnsignedTxPayloadResponse{}, err
	}

	if err := u.enforceRateLimit(address, requestKey); err != nil {
		return entityresponse.UnsignedTxPayloadResponse{}, err
	}

	sessionID, err := generateSessionID()
	if err != nil {
		return entityresponse.UnsignedTxPayloadResponse{}, err
	}

	u.mu.Lock()
	u.sessions[sessionID] = relayerSession{
		SessionID: sessionID,
		Address:   address,
		Username:  username,
		Feature:   "username.set_primary",
		CreatedAt: time.Now().UTC(),
	}
	u.mu.Unlock()

	return entityresponse.UnsignedTxPayloadResponse{
		Feature:   "username.set_primary",
		SessionID: sessionID,
		Address:   address,
		Username:  username,
		Message:   u.BuildPrimaryRelayerMessage(username, sessionID),
	}, nil
}

func (u *relayerUsecase) RegisterUsername(
	ctx context.Context,
	req entityrequest.RegisterUsernameRelayerRequest,
	requestKey string,
) (entityresponse.RegisterUsernameRelayerResponse, error) {
	sessionID := strings.TrimSpace(req.SessionID)
	address := strings.ToLower(strings.TrimSpace(req.Address))
	username := strings.TrimSpace(req.Username)

	if err := validateUsername(username); err != nil {
		return entityresponse.RegisterUsernameRelayerResponse{}, err
	}
	if err := u.enforceRateLimit(address, requestKey); err != nil {
		return entityresponse.RegisterUsernameRelayerResponse{}, err
	}

	u.mu.Lock()
	session, ok := u.sessions[sessionID]
	u.mu.Unlock()
	if !ok {
		return entityresponse.RegisterUsernameRelayerResponse{}, constants.ErrSessionExpired
	}
	if session.Feature != "username.register" {
		return entityresponse.RegisterUsernameRelayerResponse{}, fmt.Errorf("%w: relayer feature mismatch", constants.ErrForbidden)
	}
	if strings.ToLower(session.Address) != address || session.Username != username {
		return entityresponse.RegisterUsernameRelayerResponse{}, fmt.Errorf("%w: relayer payload mismatch", constants.ErrForbidden)
	}
	if time.Now().UTC().After(session.CreatedAt.Add(u.sessionTimeout)) {
		u.mu.Lock()
		delete(u.sessions, sessionID)
		u.mu.Unlock()
		return entityresponse.RegisterUsernameRelayerResponse{}, constants.ErrSessionExpired
	}

	user, err := u.repo.GetUser(address)
	if err != nil {
		return entityresponse.RegisterUsernameRelayerResponse{}, err
	}
	if user.GasSponsored {
		return entityresponse.RegisterUsernameRelayerResponse{}, fmt.Errorf("%w: gas sponsorship already used", constants.ErrForbidden)
	}

	exists, existsErr := u.repo.UsernameExists(username)
	if existsErr != nil {
		return entityresponse.RegisterUsernameRelayerResponse{}, existsErr
	}
	if exists && !strings.EqualFold(user.Username, username) {
		return entityresponse.RegisterUsernameRelayerResponse{}, fmt.Errorf("%w: username already exists", constants.ErrConflict)
	}

	message := u.BuildRelayerMessage(username, sessionID)
	verification, verifyErr := u.verifier.VerifyEIP191Signature(message, address, req.Signature, req.V)
	if verifyErr != nil {
		return entityresponse.RegisterUsernameRelayerResponse{}, verifyErr
	}
	if !verification.Valid {
		return entityresponse.RegisterUsernameRelayerResponse{}, constants.ErrInvalidSignature
	}

	txHash, relayErr := u.relayer.RegisterUsername(ctx, address, username)
	if relayErr != nil {
		return entityresponse.RegisterUsernameRelayerResponse{}, mapRelayerExecutionError(relayErr)
	}

	user.Username = username
	user.GasSponsored = true
	user.IsHardwareVerified = true
	user.PublicKey = strings.TrimSpace(verification.PublicKey)
	if updateErr := u.repo.UpdateUser(user); updateErr != nil {
		return entityresponse.RegisterUsernameRelayerResponse{}, updateErr
	}

	u.mu.Lock()
	delete(u.sessions, sessionID)
	u.mu.Unlock()

	return entityresponse.RegisterUsernameRelayerResponse{
		Accepted: true,
		TxHash:   txHash,
		Address:  address,
		Username: username,
	}, nil
}

func (u *relayerUsecase) SetPrimaryUsername(
	ctx context.Context,
	req entityrequest.SetPrimaryUsernameRelayerRequest,
	requestKey string,
) (entityresponse.SetPrimaryUsernameRelayerResponse, error) {
	sessionID := strings.TrimSpace(req.SessionID)
	address := strings.ToLower(strings.TrimSpace(req.Address))
	username := strings.TrimSpace(req.Username)

	if err := validateUsername(username); err != nil {
		return entityresponse.SetPrimaryUsernameRelayerResponse{}, err
	}
	if err := u.enforceRateLimit(address, requestKey); err != nil {
		return entityresponse.SetPrimaryUsernameRelayerResponse{}, err
	}

	u.mu.Lock()
	session, ok := u.sessions[sessionID]
	u.mu.Unlock()
	if !ok {
		return entityresponse.SetPrimaryUsernameRelayerResponse{}, constants.ErrSessionExpired
	}
	if session.Feature != "username.set_primary" {
		return entityresponse.SetPrimaryUsernameRelayerResponse{}, fmt.Errorf("%w: relayer feature mismatch", constants.ErrForbidden)
	}
	if strings.ToLower(session.Address) != address || session.Username != username {
		return entityresponse.SetPrimaryUsernameRelayerResponse{}, fmt.Errorf("%w: relayer payload mismatch", constants.ErrForbidden)
	}
	if time.Now().UTC().After(session.CreatedAt.Add(u.sessionTimeout)) {
		u.mu.Lock()
		delete(u.sessions, sessionID)
		u.mu.Unlock()
		return entityresponse.SetPrimaryUsernameRelayerResponse{}, constants.ErrSessionExpired
	}

	user, err := u.repo.GetUser(address)
	if err != nil {
		return entityresponse.SetPrimaryUsernameRelayerResponse{}, err
	}

	message := u.BuildPrimaryRelayerMessage(username, sessionID)
	verification, verifyErr := u.verifier.VerifyEIP191Signature(message, address, req.Signature, req.V)
	if verifyErr != nil {
		return entityresponse.SetPrimaryUsernameRelayerResponse{}, verifyErr
	}
	if !verification.Valid {
		return entityresponse.SetPrimaryUsernameRelayerResponse{}, constants.ErrInvalidSignature
	}

	txHash, relayErr := u.relayer.SetPrimaryUsername(ctx, address, username)
	if relayErr != nil {
		return entityresponse.SetPrimaryUsernameRelayerResponse{}, mapRelayerExecutionError(relayErr)
	}

	user.Username = username
	user.IsHardwareVerified = true
	user.PublicKey = strings.TrimSpace(verification.PublicKey)
	if updateErr := u.repo.UpdateUser(user); updateErr != nil {
		return entityresponse.SetPrimaryUsernameRelayerResponse{}, updateErr
	}

	u.mu.Lock()
	delete(u.sessions, sessionID)
	u.mu.Unlock()

	return entityresponse.SetPrimaryUsernameRelayerResponse{
		Accepted: true,
		TxHash:   txHash,
		Address:  address,
		Username: username,
	}, nil
}

func (u *relayerUsecase) enforceRateLimit(address, requestKey string) error {
	now := time.Now().UTC()
	u.mu.Lock()
	defer u.mu.Unlock()

	if address != "" {
		if last, ok := u.lastRequestAt[address]; ok && now.Sub(last) < u.rateLimitWindow {
			return fmt.Errorf("%w: please wait before retrying", constants.ErrRateLimited)
		}
		u.lastRequestAt[address] = now
	}

	key := strings.TrimSpace(requestKey)
	if key != "" {
		if last, ok := u.lastRequestByIP[key]; ok && now.Sub(last) < u.rateLimitWindow {
			return fmt.Errorf("%w: too many requests", constants.ErrRateLimited)
		}
		u.lastRequestByIP[key] = now
	}

	return nil
}

func validateUsername(username string) error {
	trimmed := strings.TrimSpace(username)
	if !usernamePattern.MatchString(trimmed) {
		return fmt.Errorf("invalid username: use 3-20 chars [a-zA-Z0-9_.-]")
	}
	return nil
}

func mapRelayerExecutionError(err error) error {
	if err == nil {
		return nil
	}

	msg := strings.ToLower(strings.TrimSpace(err.Error()))
	if strings.Contains(msg, "::usernames") && strings.Contains(msg, "code=524292") {
		return fmt.Errorf("%w: username is already registered on chain", constants.ErrConflict)
	}

	return err
}

func generateSessionID() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate relayer session: %w", err)
	}
	return hex.EncodeToString(buf), nil
}
