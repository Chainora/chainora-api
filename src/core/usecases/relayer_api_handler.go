package usecases

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"

	entityrequest "chainora-api/core/entities/request"
	entityresponse "chainora-api/core/entities/response"

	"chainora-api/core/usecases/requests"
	"chainora-api/core/usecases/response"

	"github.com/gin-gonic/gin"
	"github.com/go-playground/validator/v10"
	"github.com/gorilla/websocket"
)

// RelayerHandler exposes username relayer APIs.
type RelayerHandler struct {
	relayer  RelayerUsecase
	hub      *WSHub
	validate *validator.Validate
	upgrader websocket.Upgrader
}

func NewRelayerHandler(relayer RelayerUsecase, hub *WSHub) *RelayerHandler {
	return NewRelayerHandlerWithOriginChecker(relayer, hub, nil)
}

func NewRelayerHandlerWithOriginChecker(relayer RelayerUsecase, hub *WSHub, wsOriginChecker func(r *http.Request) bool) *RelayerHandler {
	if wsOriginChecker == nil {
		wsOriginChecker = func(_ *http.Request) bool { return false }
	}

	return &RelayerHandler{
		relayer:  relayer,
		hub:      hub,
		validate: validator.New(),
		upgrader: websocket.Upgrader{CheckOrigin: wsOriginChecker},
	}
}

// CreateUnsignedPayload godoc
// @Summary Create username relayer QR payload
// @Description Builds unsigned relayer payload (session + message) for mobile hardware signing.
// @Tags relayer
// @Accept json
// @Produce json
// @Router /v1/relayer/payload [post]
func (h *RelayerHandler) CreateUnsignedPayload(ctx *gin.Context) {
	var req entityrequest.CreateRelayerPayloadRequest
	if err := requests.Serialize(ctx, &req); err != nil {
		response.WriteError(ctx, err)
		return
	}
	if err := h.validate.Struct(req); err != nil {
		response.WriteError(ctx, err)
		return
	}

	payload, err := h.relayer.CreateUnsignedPayload(ctx.Request.Context(), req, strings.TrimSpace(ctx.ClientIP()))
	if err != nil {
		response.WriteError(ctx, err)
		return
	}

	response.Write(ctx.Writer, response.Ok(payload))
}

// CreatePrimaryPayload godoc
// @Summary Create primary username relayer payload
// @Description Builds unsigned payload (session + message) for selecting primary username.
// @Tags relayer
// @Accept json
// @Produce json
// @Router /v1/relayer/primary/payload [post]
func (h *RelayerHandler) CreatePrimaryPayload(ctx *gin.Context) {
	var req entityrequest.CreatePrimaryPayloadRequest
	if err := requests.Serialize(ctx, &req); err != nil {
		response.WriteError(ctx, err)
		return
	}
	if err := h.validate.Struct(req); err != nil {
		response.WriteError(ctx, err)
		return
	}

	payload, err := h.relayer.CreatePrimaryPayload(ctx.Request.Context(), req, strings.TrimSpace(ctx.ClientIP()))
	if err != nil {
		response.WriteError(ctx, err)
		return
	}

	response.Write(ctx.Writer, response.Ok(payload))
}

// WaitForRelayerWS godoc
// @Summary Subscribe username relayer websocket
// @Description Waits for relayer completion event by session ID.
// @Tags relayer
// @Produce json
// @Router /v1/relayer/ws/{sessionId} [get]
func (h *RelayerHandler) WaitForRelayerWS(ctx *gin.Context) {
	req := entityrequest.WaitForRelayerRequest{SessionID: strings.TrimSpace(ctx.Param("sessionId"))}
	if err := h.validate.Struct(req); err != nil {
		response.WriteError(ctx, err)
		return
	}
	if err := h.relayer.ValidateRelayerSession(req.SessionID); err != nil {
		response.WriteError(ctx, err)
		return
	}

	conn, err := h.upgrader.Upgrade(ctx.Writer, ctx.Request, nil)
	if err != nil {
		response.WriteError(ctx, fmt.Errorf("ws upgrade: %w", err))
		return
	}

	h.hub.Register(req.SessionID, conn)
	log.Printf("relayer ws connected sessionId=%s", req.SessionID)
	defer func() {
		h.hub.Unregister(req.SessionID, conn)
		_ = conn.Close()
		log.Printf("relayer ws disconnected sessionId=%s", req.SessionID)
	}()

	for {
		if _, _, readErr := conn.ReadMessage(); readErr != nil {
			return
		}
	}
}

// RegisterUsername godoc
// @Summary Register username with sponsored gas
// @Description Verifies hardware signature and relays sponsored transaction to Initia.
// @Tags relayer
// @Accept json
// @Produce json
// @Router /v1/relayer/register [post]
func (h *RelayerHandler) RegisterUsername(ctx *gin.Context) {
	var req entityrequest.RegisterUsernameRelayerRequest
	if err := requests.Serialize(ctx, &req); err != nil {
		response.WriteError(ctx, err)
		return
	}
	if err := h.validate.Struct(req); err != nil {
		response.WriteError(ctx, err)
		return
	}

	resp, err := h.relayer.RegisterUsername(ctx.Request.Context(), req, strings.TrimSpace(ctx.ClientIP()))
	if err != nil {
		payload, _ := json.Marshal(entityresponse.WSRelayerEvent{
			Status:    "failed",
			SessionID: req.SessionID,
			Address:   req.Address,
			Username:  req.Username,
			Error:     err.Error(),
		})
		h.hub.Broadcast(req.SessionID, payload)
		response.WriteError(ctx, err)
		return
	}

	payload, _ := json.Marshal(entityresponse.WSRelayerEvent{
		Status:    "registered",
		SessionID: req.SessionID,
		Address:   resp.Address,
		Username:  resp.Username,
		TxHash:    resp.TxHash,
	})
	h.hub.Broadcast(req.SessionID, payload)

	response.Write(ctx.Writer, response.Ok(resp))
}

// SetPrimaryUsername godoc
// @Summary Select primary username with signature
// @Description Verifies hardware signature and relays signed primary-username selection transaction.
// @Tags relayer
// @Accept json
// @Produce json
// @Router /v1/relayer/primary/select [post]
func (h *RelayerHandler) SetPrimaryUsername(ctx *gin.Context) {
	var req entityrequest.SetPrimaryUsernameRelayerRequest
	if err := requests.Serialize(ctx, &req); err != nil {
		response.WriteError(ctx, err)
		return
	}
	if err := h.validate.Struct(req); err != nil {
		response.WriteError(ctx, err)
		return
	}

	resp, err := h.relayer.SetPrimaryUsername(ctx.Request.Context(), req, strings.TrimSpace(ctx.ClientIP()))
	if err != nil {
		payload, _ := json.Marshal(entityresponse.WSRelayerEvent{
			Status:    "failed",
			SessionID: req.SessionID,
			Address:   req.Address,
			Username:  req.Username,
			Error:     err.Error(),
		})
		h.hub.Broadcast(req.SessionID, payload)
		response.WriteError(ctx, err)
		return
	}

	payload, _ := json.Marshal(entityresponse.WSRelayerEvent{
		Status:    "primary_selected",
		SessionID: req.SessionID,
		Address:   resp.Address,
		Username:  resp.Username,
		TxHash:    resp.TxHash,
	})
	h.hub.Broadcast(req.SessionID, payload)

	response.Write(ctx.Writer, response.Ok(resp))
}
