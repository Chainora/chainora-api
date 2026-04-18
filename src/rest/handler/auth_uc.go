package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"

	"chainora-api/core/constants"
	entityrequest "chainora-api/core/entities/request"
	entityresponse "chainora-api/core/entities/response"
	"chainora-api/core/usecases"
	"chainora-api/rest/controllers"

	"github.com/gin-gonic/gin"
	"github.com/go-playground/validator/v10"
	"github.com/gorilla/websocket"
)

// TokenIssuer is the abstraction used to mint JWTs.
type TokenIssuer interface {
	GenerateTokenPair(sessionID, address string) (string, string, error)
	RefreshFromToken(refreshToken string) (string, string, string, error)
	ParseAccessToken(accessToken string) (string, string, error)
}

type initSessionUsecase struct {
	auth usecases.AuthUsecase
}

func (u *initSessionUsecase) Trigger(_ *gin.Context, _ entityrequest.InitSessionRequest) (entityresponse.InitSessionResponse, error) {
	session, err := u.auth.GenerateLoginSession()
	if err != nil {
		return entityresponse.InitSessionResponse{}, fmt.Errorf("generate login session: %w", err)
	}

	return entityresponse.InitSessionResponse{SessionID: session.ID, Nonce: session.Nonce}, nil
}

type waitForLoginUsecase struct {
	auth     usecases.AuthUsecase
	hub      *controllers.WSHub
	upgrader websocket.Upgrader
	validate *validator.Validate
}

func (u *waitForLoginUsecase) Trigger(ctx *gin.Context, req entityrequest.WaitForLoginRequest) error {
	if err := u.validate.Struct(req); err != nil {
		return err
	}

	if err := u.auth.ValidateLoginSession(req.SessionID); err != nil {
		return err
	}

	conn, err := u.upgrader.Upgrade(ctx.Writer, ctx.Request, nil)
	if err != nil {
		return fmt.Errorf("ws upgrade: %w", err)
	}

	u.hub.Register(req.SessionID, conn)
	log.Printf("ws connected sessionId=%s", req.SessionID)
	defer func() {
		u.hub.Unregister(req.SessionID, conn)
		_ = conn.Close()
		log.Printf("ws disconnected sessionId=%s", req.SessionID)
	}()

	for {
		if _, _, readErr := conn.ReadMessage(); readErr != nil {
			return nil
		}
	}
}

type verifySignatureUsecase struct {
	auth     usecases.AuthUsecase
	issuer   TokenIssuer
	hub      *controllers.WSHub
	validate *validator.Validate
}

type progressLoginUsecase struct {
	auth     usecases.AuthUsecase
	hub      *controllers.WSHub
	validate *validator.Validate
}

type refreshTokenUsecase struct {
	issuer   TokenIssuer
	validate *validator.Validate
}

type meUsecase struct {
	issuer   TokenIssuer
	validate *validator.Validate
}

type getProfileUsecase struct {
	auth     usecases.AuthUsecase
	issuer   TokenIssuer
	validate *validator.Validate
	resolver interface {
		ResolvePrimaryUsername(ctx context.Context, address string) (string, error)
	}
}

type listProfilesUsecase struct {
	auth     usecases.AuthUsecase
	issuer   TokenIssuer
	validate *validator.Validate
	resolver interface {
		ResolvePrimaryUsername(ctx context.Context, address string) (string, error)
	}
}

type updateProfileUsecase struct {
	auth     usecases.AuthUsecase
	issuer   TokenIssuer
	validate *validator.Validate
}

const placeholderUsername = "Chainora User"

func normalizeProfileUsername(value string) string {
	trimmed := strings.TrimSpace(value)
	if strings.EqualFold(trimmed, placeholderUsername) {
		return ""
	}
	return trimmed
}

func (u *verifySignatureUsecase) Trigger(_ *gin.Context, req entityrequest.SignInRequest) (entityresponse.VerifySignatureResponse, error) {
	if err := u.validate.Struct(req); err != nil {
		return entityresponse.VerifySignatureResponse{}, err
	}

	user, err := u.auth.AuthenticateUser(req.SessionID, req.Address, req.Signature, req.Username, req.V)
	if err != nil {
		return entityresponse.VerifySignatureResponse{}, err
	}

	token, refreshToken, err := u.issuer.GenerateTokenPair(req.SessionID, user.Address)
	if err != nil {
		return entityresponse.VerifySignatureResponse{}, fmt.Errorf("generate token: %w", err)
	}

	message, err := json.Marshal(entityresponse.WSLoginVerifiedEvent{
		Status:       "verified",
		SessionID:    req.SessionID,
		Address:      user.Address,
		Username:     user.Username,
		Token:        token,
		RefreshToken: refreshToken,
	})
	if err != nil {
		return entityresponse.VerifySignatureResponse{}, fmt.Errorf("marshal websocket payload: %w", err)
	}
	u.hub.Broadcast(req.SessionID, message)
	log.Printf("qr verify success sessionId=%s address=%s", req.SessionID, user.Address)

	return entityresponse.VerifySignatureResponse{
		Verified:     true,
		Address:      user.Address,
		Username:     user.Username,
		Token:        token,
		RefreshToken: refreshToken,
	}, nil
}

func (u *progressLoginUsecase) Trigger(_ *gin.Context, req entityrequest.ProgressLoginRequest) error {
	if err := u.validate.Struct(req); err != nil {
		return err
	}

	if err := u.auth.ValidateLoginSession(req.SessionID); err != nil {
		return err
	}

	message, err := json.Marshal(entityresponse.WSLoginProgressEvent{
		Status:    req.Status,
		SessionID: req.SessionID,
	})
	if err != nil {
		return fmt.Errorf("marshal websocket payload: %w", err)
	}

	u.hub.Broadcast(req.SessionID, message)
	log.Printf("qr progress sessionId=%s status=%s", req.SessionID, req.Status)
	return nil
}

func (u *refreshTokenUsecase) Trigger(_ *gin.Context, req entityrequest.RefreshTokenRequest) (entityresponse.RefreshTokenResponse, error) {
	if err := u.validate.Struct(req); err != nil {
		return entityresponse.RefreshTokenResponse{}, err
	}

	accessToken, nextRefreshToken, address, err := u.issuer.RefreshFromToken(req.RefreshToken)
	if err != nil {
		return entityresponse.RefreshTokenResponse{}, err
	}

	return entityresponse.RefreshTokenResponse{
		Token:        accessToken,
		RefreshToken: nextRefreshToken,
		Address:      address,
	}, nil
}

func (u *meUsecase) Trigger(_ *gin.Context, req entityrequest.MeRequest) (entityresponse.MeResponse, error) {
	if err := u.validate.Struct(req); err != nil {
		return entityresponse.MeResponse{}, err
	}

	sessionID, address, err := u.issuer.ParseAccessToken(req.AccessToken)
	if err != nil {
		return entityresponse.MeResponse{}, err
	}

	return entityresponse.MeResponse{
		Address:   address,
		SessionID: sessionID,
	}, nil
}

func (u *getProfileUsecase) Trigger(ctx *gin.Context, req entityrequest.MeRequest) (entityresponse.ProfileResponse, error) {
	if err := u.validate.Struct(req); err != nil {
		return entityresponse.ProfileResponse{}, err
	}

	_, address, err := u.issuer.ParseAccessToken(req.AccessToken)
	if err != nil {
		return entityresponse.ProfileResponse{}, err
	}

	user, err := u.auth.GetUserProfile(address)
	if err != nil {
		return entityresponse.ProfileResponse{}, err
	}

	username := normalizeProfileUsername(user.Username)
	resolvedUsername, resolveErr := u.resolveUsername(ctx.Request.Context(), user.Address)
	if resolveErr == nil && normalizeProfileUsername(resolvedUsername) != "" {
		username = normalizeProfileUsername(resolvedUsername)
	}

	return entityresponse.ProfileResponse{
		Address:                       user.Address,
		Username:                      username,
		AvatarURL:                     strings.TrimSpace(user.AvatarURL),
		UsernameCount:                 user.UsernameCount,
		PrimarySelectionSponsoredUsed: user.PrimarySelectionSponsoredUsed,
		TCNR:                          user.TCNR,
		KYCStatus:                     user.KYCStatus,
	}, nil
}

func (u *listProfilesUsecase) Trigger(ctx *gin.Context, accessToken string, addresses []string) ([]entityresponse.BasicProfileResponse, error) {
	if strings.TrimSpace(accessToken) == "" {
		return nil, fmt.Errorf("%w: missing bearer token", constants.ErrInvalidToken)
	}

	if _, _, err := u.issuer.ParseAccessToken(accessToken); err != nil {
		return nil, err
	}

	normalizedAddresses := normalizeAddressList(addresses)
	if len(normalizedAddresses) == 0 {
		return nil, fmt.Errorf("at least one valid address is required")
	}

	if len(normalizedAddresses) > 120 {
		return nil, fmt.Errorf("too many addresses: max 120")
	}

	profiles := make([]entityresponse.BasicProfileResponse, 0, len(normalizedAddresses))
	for _, address := range normalizedAddresses {
		profile := entityresponse.BasicProfileResponse{
			Address:   address,
			Username:  "",
			AvatarURL: "",
		}

		user, err := u.auth.GetUserProfile(address)
		if err == nil {
			profile.Username = normalizeProfileUsername(user.Username)
			profile.AvatarURL = strings.TrimSpace(user.AvatarURL)
		} else if !errors.Is(err, constants.ErrUserNotFound) {
			return nil, err
		}

		resolvedUsername, resolveErr := u.resolveUsername(ctx.Request.Context(), address)
		if resolveErr == nil && normalizeProfileUsername(resolvedUsername) != "" {
			profile.Username = normalizeProfileUsername(resolvedUsername)
		}

		profiles = append(profiles, profile)
	}

	return profiles, nil
}

func (u *updateProfileUsecase) Trigger(_ *gin.Context, accessToken string, req entityrequest.UpdateProfileRequest) (entityresponse.ProfileResponse, error) {
	if err := u.validate.Struct(req); err != nil {
		return entityresponse.ProfileResponse{}, err
	}

	_, address, err := u.issuer.ParseAccessToken(accessToken)
	if err != nil {
		return entityresponse.ProfileResponse{}, err
	}

	user, updateErr := u.auth.UpdateUserAvatar(address, req.AvatarURL)
	if updateErr != nil {
		return entityresponse.ProfileResponse{}, updateErr
	}

	return entityresponse.ProfileResponse{
		Address:                       user.Address,
		Username:                      normalizeProfileUsername(user.Username),
		AvatarURL:                     strings.TrimSpace(user.AvatarURL),
		UsernameCount:                 user.UsernameCount,
		PrimarySelectionSponsoredUsed: user.PrimarySelectionSponsoredUsed,
		TCNR:                          user.TCNR,
		KYCStatus:                     user.KYCStatus,
	}, nil
}

func (u *getProfileUsecase) resolveUsername(ctx context.Context, address string) (string, error) {
	if u.resolver == nil {
		return "", nil
	}

	username, err := u.resolver.ResolvePrimaryUsername(ctx, address)
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(username), nil
}

func (u *listProfilesUsecase) resolveUsername(ctx context.Context, address string) (string, error) {
	if u.resolver == nil {
		return "", nil
	}

	username, err := u.resolver.ResolvePrimaryUsername(ctx, address)
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(username), nil
}

func normalizeAddressList(rawAddresses []string) []string {
	out := make([]string, 0, len(rawAddresses))
	seen := make(map[string]struct{}, len(rawAddresses))

	for _, chunk := range rawAddresses {
		for _, candidate := range strings.Split(chunk, ",") {
			normalized := strings.ToLower(strings.TrimSpace(candidate))
			if normalized == "" {
				continue
			}

			if !strings.HasPrefix(normalized, "0x") {
				normalized = "0x" + normalized
			}

			if !isLikelyEVMAddress(normalized) {
				continue
			}

			if _, exists := seen[normalized]; exists {
				continue
			}

			seen[normalized] = struct{}{}
			out = append(out, normalized)
		}
	}

	return out
}

func isLikelyEVMAddress(value string) bool {
	if !strings.HasPrefix(value, "0x") || len(value) != 42 {
		return false
	}

	for _, ch := range value[2:] {
		if (ch < '0' || ch > '9') && (ch < 'a' || ch > 'f') {
			return false
		}
	}

	return true
}

func newWaitForLoginRequest(rawSessionID string) entityrequest.WaitForLoginRequest {
	return entityrequest.WaitForLoginRequest{SessionID: strings.TrimSpace(rawSessionID)}
}

func defaultUpgrader(originChecker func(r *http.Request) bool) websocket.Upgrader {
	if originChecker == nil {
		originChecker = func(_ *http.Request) bool { return false }
	}

	return websocket.Upgrader{CheckOrigin: originChecker}
}
