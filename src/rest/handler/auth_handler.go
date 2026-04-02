package handler

import (
	"chainora-api/core/usecases"
	"chainora-api/rest/controllers"
	"chainora-api/rest/handler/requests"
	"chainora-api/rest/handler/response"

	"github.com/gin-gonic/gin"
	"github.com/go-playground/validator/v10"
)

// AuthHandler exposes auth endpoints using thin handler + usecase trigger pattern.
type AuthHandler struct {
	initSessionUC  *initSessionUsecase
	waitForLoginUC *waitForLoginUsecase
	verifySignUC   *verifySignatureUsecase
}

func NewAuthHandler(authUsecase usecases.AuthUsecase, issuer TokenIssuer, hub *controllers.WSHub) *AuthHandler {
	validate := validator.New()

	return &AuthHandler{
		initSessionUC: &initSessionUsecase{
			auth: authUsecase,
		},
		waitForLoginUC: &waitForLoginUsecase{
			auth:     authUsecase,
			hub:      hub,
			upgrader: defaultUpgrader(),
			validate: validate,
		},
		verifySignUC: &verifySignatureUsecase{
			auth:     authUsecase,
			issuer:   issuer,
			hub:      hub,
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
	resp, err := h.initSessionUC.Trigger(ctx, initSessionRequest{})
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
	var req verifySignatureRequest
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
