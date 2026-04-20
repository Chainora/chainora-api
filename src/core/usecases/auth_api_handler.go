package usecases

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"strings"

	"chainora-api/core/constants"
	entityrequest "chainora-api/core/entities/request"

	"chainora-api/core/usecases/requests"
	"chainora-api/core/usecases/response"

	"github.com/gin-gonic/gin"
	"github.com/go-playground/validator/v10"
)

// AuthHandler exposes auth endpoints using thin handler + usecase trigger pattern.
type AuthHandler struct {
	initSessionUC   *initSessionUsecase
	waitForLoginUC  *waitForLoginUsecase
	progressLoginUC *progressLoginUsecase
	verifySignUC    *verifySignatureUsecase
	refreshTokenUC  *refreshTokenUsecase
	meUC            *meUsecase
	getProfileUC    *getProfileUsecase
	listProfilesUC  *listProfilesUsecase
	updateProfileUC *updateProfileUsecase
}

type UsernameResolver interface {
	ResolvePrimaryUsername(ctx context.Context, address string) (string, error)
}

func NewAuthHandler(
	authUsecase AuthUsecase,
	issuer TokenIssuer,
	db *sql.DB,
	hub *WSHub,
	usernameResolver UsernameResolver,
	wsOriginChecker func(r *http.Request) bool,
) *AuthHandler {
	validate := validator.New()

	return &AuthHandler{
		initSessionUC: &initSessionUsecase{
			auth: authUsecase,
		},
		waitForLoginUC: &waitForLoginUsecase{
			auth:     authUsecase,
			hub:      hub,
			upgrader: defaultUpgrader(wsOriginChecker),
			validate: validate,
		},
		progressLoginUC: &progressLoginUsecase{
			auth:     authUsecase,
			hub:      hub,
			validate: validate,
		},
		verifySignUC: &verifySignatureUsecase{
			auth:     authUsecase,
			issuer:   issuer,
			hub:      hub,
			validate: validate,
		},
		refreshTokenUC: &refreshTokenUsecase{
			issuer:   issuer,
			validate: validate,
		},
		meUC: &meUsecase{
			issuer:   issuer,
			validate: validate,
		},
		getProfileUC: &getProfileUsecase{
			auth:     authUsecase,
			issuer:   issuer,
			validate: validate,
			resolver: usernameResolver,
		},
		listProfilesUC: &listProfilesUsecase{
			auth:     authUsecase,
			issuer:   issuer,
			validate: validate,
			db:       db,
			resolver: usernameResolver,
		},
		updateProfileUC: &updateProfileUsecase{
			auth:     authUsecase,
			issuer:   issuer,
			validate: validate,
		},
	}
}

// InitSession godoc
// @Summary Create QR login session
// @Description Creates a login session and returns sessionId + nonce for QR payload.
// @Tags auth
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} map[string]interface{}
// @Failure 500 {object} map[string]interface{}
// @Router /v1/auth/session [get]
func (h *AuthHandler) InitSession(ctx *gin.Context) {
	resp, err := h.initSessionUC.Trigger(ctx, entityrequest.InitSessionRequest{})
	if err != nil {
		response.WriteError(ctx, err)
		return
	}

	response.Write(ctx.Writer, response.Ok(resp))
}

// WaitForLoginWS godoc
// @Summary Wait for login result over websocket
// @Description Registers websocket by sessionId and waits for verify result.
// @Tags auth
// @Param sessionId path string true "Session ID"
// @Success 101 {string} string "Switching Protocols"
// @Failure 400 {object} map[string]interface{}
// @Failure 410 {object} map[string]interface{}
// @Router /v1/auth/ws/{sessionId} [get]
func (h *AuthHandler) WaitForLoginWS(ctx *gin.Context) {
	req := newWaitForLoginRequest(ctx.Param("sessionId"))
	if err := h.waitForLoginUC.Trigger(ctx, req); err != nil {
		response.WriteError(ctx, err)
		return
	}
}

// NotifyProgress godoc
// @Summary Notify login progress over websocket
// @Description Broadcasts an in-progress login status (e.g. awaiting_card_scan) to dapp websocket subscribers.
// @Tags auth
// @Accept json
// @Produce json
// @Param payload body progressLoginRequest true "Login progress payload"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} map[string]interface{}
// @Failure 410 {object} map[string]interface{}
// @Router /v1/auth/progress [post]
func (h *AuthHandler) NotifyProgress(ctx *gin.Context) {
	var req entityrequest.ProgressLoginRequest
	if err := requests.Serialize(ctx, &req); err != nil {
		response.WriteError(ctx, err)
		return
	}

	if err := h.progressLoginUC.Trigger(ctx, req); err != nil {
		response.WriteError(ctx, err)
		return
	}

	response.Write(ctx.Writer, response.Ok(gin.H{"accepted": true}))
}

// VerifySignature godoc
// @Summary Verify Chainora signature
// @Description Validates EIP-191 signature from mobile app and pushes JWT to waiting dapp websocket.
// @Tags auth
// @Accept json
// @Produce json
// @Param payload body verifySignatureRequest true "Verification payload"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} map[string]interface{}
// @Failure 401 {object} map[string]interface{}
// @Failure 404 {object} map[string]interface{}
// @Failure 410 {object} map[string]interface{}
// @Router /v1/auth/verify [post]
func (h *AuthHandler) VerifySignature(ctx *gin.Context) {
	var req entityrequest.SignInRequest
	if err := requests.Serialize(ctx, &req); err != nil {
		response.WriteError(ctx, err)
		return
	}

	resp, err := h.verifySignUC.Trigger(ctx, req)
	if err != nil {
		response.WriteError(ctx, err)
		return
	}

	response.Write(ctx.Writer, response.Ok(resp))
}

// RefreshToken godoc
// @Summary Refresh access token
// @Description Exchanges a valid refresh token for a new access token and rotated refresh token.
// @Tags auth
// @Accept json
// @Produce json
// @Param payload body refreshTokenRequest true "Refresh token payload"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} map[string]interface{}
// @Failure 401 {object} map[string]interface{}
// @Router /v1/auth/refresh [post]
func (h *AuthHandler) RefreshToken(ctx *gin.Context) {
	var req entityrequest.RefreshTokenRequest
	if err := requests.Serialize(ctx, &req); err != nil {
		response.WriteError(ctx, err)
		return
	}

	resp, err := h.refreshTokenUC.Trigger(ctx, req)
	if err != nil {
		response.WriteError(ctx, err)
		return
	}

	response.Write(ctx.Writer, response.Ok(resp))
}

// Me godoc
// @Summary Get current authenticated user
// @Description Returns auth identity from Bearer access token.
// @Tags auth
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Failure 401 {object} map[string]interface{}
// @Router /v1/auth/me [get]
func (h *AuthHandler) Me(ctx *gin.Context) {
	accessToken, err := extractBearerToken(ctx.GetHeader("Authorization"))
	if err != nil {
		response.WriteError(ctx, err)
		return
	}

	resp, triggerErr := h.meUC.Trigger(ctx, entityrequest.MeRequest{AccessToken: accessToken})
	if triggerErr != nil {
		response.WriteError(ctx, triggerErr)
		return
	}

	response.Write(ctx.Writer, response.Ok(resp))
}

// GetProfile godoc
// @Summary Get current profile
// @Description Returns profile fields (username, tCNR, kyc status) for authenticated user.
// @Tags auth
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Failure 401 {object} map[string]interface{}
// @Router /v1/auth/profile [get]
func (h *AuthHandler) GetProfile(ctx *gin.Context) {
	accessToken, err := extractBearerToken(ctx.GetHeader("Authorization"))
	if err != nil {
		response.WriteError(ctx, err)
		return
	}

	resp, triggerErr := h.getProfileUC.Trigger(ctx, entityrequest.MeRequest{AccessToken: accessToken})
	if triggerErr != nil {
		response.WriteError(ctx, triggerErr)
		return
	}

	response.Write(ctx.Writer, response.Ok(resp))
}

// ListProfiles godoc
// @Summary List basic profiles by wallet addresses or usernames
// @Description Returns profile fields (address, username, avatarUrl) for a batch of wallet addresses/usernames.
// @Tags auth
// @Produce json
// @Param addresses query string false "Comma-separated EVM addresses"
// @Param address query []string false "Repeatable address query param"
// @Param usernames query string false "Comma-separated usernames"
// @Param username query []string false "Repeatable username query param"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} map[string]interface{}
// @Failure 401 {object} map[string]interface{}
// @Router /v1/auth/profiles [get]
func (h *AuthHandler) ListProfiles(ctx *gin.Context) {
	accessToken, err := extractBearerToken(ctx.GetHeader("Authorization"))
	if err != nil {
		response.WriteError(ctx, err)
		return
	}

	rawAddresses := make([]string, 0, 1+len(ctx.QueryArray("address")))
	if addressesParam := strings.TrimSpace(ctx.Query("addresses")); addressesParam != "" {
		rawAddresses = append(rawAddresses, addressesParam)
	}
	rawAddresses = append(rawAddresses, ctx.QueryArray("address")...)

	rawUsernames := make([]string, 0, 1+len(ctx.QueryArray("username")))
	if usernamesParam := strings.TrimSpace(ctx.Query("usernames")); usernamesParam != "" {
		rawUsernames = append(rawUsernames, usernamesParam)
	}
	rawUsernames = append(rawUsernames, ctx.QueryArray("username")...)

	if len(rawAddresses) == 0 && len(rawUsernames) == 0 {
		response.WriteError(ctx, fmt.Errorf("addresses or usernames query is required"))
		return
	}

	resp, triggerErr := h.listProfilesUC.Trigger(ctx, accessToken, rawAddresses, rawUsernames)
	if triggerErr != nil {
		response.WriteError(ctx, triggerErr)
		return
	}

	response.Write(ctx.Writer, response.Ok(resp))
}

// UpdateProfile godoc
// @Summary Update current profile
// @Description Updates mutable profile fields (currently avatarUrl) for authenticated user.
// @Tags auth
// @Accept json
// @Produce json
// @Param payload body request.UpdateProfileRequest true "Profile update payload"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} map[string]interface{}
// @Failure 401 {object} map[string]interface{}
// @Router /v1/auth/profile [patch]
func (h *AuthHandler) UpdateProfile(ctx *gin.Context) {
	accessToken, err := extractBearerToken(ctx.GetHeader("Authorization"))
	if err != nil {
		response.WriteError(ctx, err)
		return
	}

	var req entityrequest.UpdateProfileRequest
	if err := requests.Serialize(ctx, &req); err != nil {
		response.WriteError(ctx, err)
		return
	}

	resp, triggerErr := h.updateProfileUC.Trigger(ctx, accessToken, req)
	if triggerErr != nil {
		response.WriteError(ctx, triggerErr)
		return
	}

	response.Write(ctx.Writer, response.Ok(resp))
}

func extractBearerToken(header string) (string, error) {
	value := strings.TrimSpace(header)
	parts := strings.SplitN(value, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || strings.TrimSpace(parts[1]) == "" {
		return "", fmt.Errorf("%w: missing bearer token", constants.ErrInvalidToken)
	}

	return strings.TrimSpace(parts[1]), nil
}
