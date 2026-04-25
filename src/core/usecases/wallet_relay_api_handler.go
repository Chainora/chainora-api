package usecases

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	entityrequest "chainora-api/core/entities/request"
	"chainora-api/core/usecases/requests"
	"chainora-api/core/usecases/response"

	"github.com/gin-gonic/gin"
	"github.com/go-playground/validator/v10"
	"github.com/gorilla/websocket"
)

type WalletRelayHandler struct {
	hub         *RelayHub
	validate    *validator.Validate
	upgrader    websocket.Upgrader
	relayWSBase string
	pingInterval time.Duration
	readTimeout  time.Duration
}

func NewWalletRelayHandler(
	hub *RelayHub,
	wsOriginChecker func(r *http.Request) bool,
	relayWSBase string,
	pingInterval time.Duration,
) *WalletRelayHandler {
	if wsOriginChecker == nil {
		wsOriginChecker = func(_ *http.Request) bool { return false }
	}
	if pingInterval <= 0 {
		pingInterval = 30 * time.Second
	}
	readTimeout := 3 * pingInterval
	if readTimeout < 30*time.Second {
		readTimeout = 30 * time.Second
	}

	return &WalletRelayHandler{
		hub:      hub,
		validate: validator.New(),
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				role := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("role")))
				if role == WalletRelayRoleMobile {
					return true
				}
				return wsOriginChecker(r)
			},
		},
		relayWSBase:  strings.TrimRight(strings.TrimSpace(relayWSBase), "/"),
		pingInterval: pingInterval,
		readTimeout:  readTimeout,
	}
}

// Pair godoc
// @Summary Create wallet relay pairing session
// @Description Creates relay session and returns pairing URI for mobile wallet QR scan.
// @Tags wallet-relay
// @Accept json
// @Produce json
// @Router /v1/wallet-relay/pair [post]
func (h *WalletRelayHandler) Pair(ctx *gin.Context) {
	var req entityrequest.WalletRelayPairRequest
	if err := requests.Serialize(ctx, &req); err != nil {
		response.WriteError(ctx, err)
		return
	}
	if err := h.validate.Struct(req); err != nil {
		response.WriteError(ctx, err)
		return
	}

	origin := resolveRequestOrigin(ctx.Request)
	relayWSBase := h.resolveRelayWSBase(ctx.Request)

	resp, err := h.hub.Pair(req, origin, relayWSBase)
	if err != nil {
		response.WriteError(ctx, err)
		return
	}

	response.Write(ctx.Writer, response.Ok(resp))
}

// ConnectWS godoc
// @Summary Connect wallet relay websocket
// @Description Opens browser/mobile websocket channel and relays connect/sign requests.
// @Tags wallet-relay
// @Produce json
// @Router /v1/wallet-relay/ws/{sessionId} [get]
func (h *WalletRelayHandler) ConnectWS(ctx *gin.Context) {
	req := entityrequest.WalletRelayWSConnectRequest{
		SessionID: strings.TrimSpace(ctx.Param("sessionId")),
		Role:      strings.TrimSpace(ctx.Query("role")),
		Token:     strings.TrimSpace(ctx.Query("token")),
	}
	if err := h.validate.Struct(req); err != nil {
		response.WriteError(ctx, err)
		return
	}

	if err := h.hub.ValidateWSAccess(req, ctx.GetHeader("Origin")); err != nil {
		response.WriteError(ctx, err)
		return
	}

	conn, err := h.upgrader.Upgrade(ctx.Writer, ctx.Request, nil)
	if err != nil {
		response.WriteError(ctx, fmt.Errorf("ws upgrade: %w", err))
		return
	}
	defer conn.Close()

	conn.SetReadLimit(1 << 20)
	_ = conn.SetReadDeadline(time.Now().Add(h.readTimeout))
	conn.SetPongHandler(func(_ string) error {
		return conn.SetReadDeadline(time.Now().Add(h.readTimeout))
	})

	if err := h.hub.AttachPeer(req, conn); err != nil {
		_ = conn.WriteJSON(WalletRelayMessage{
			Type:      WalletRelayMessageTypeError,
			SessionID: req.SessionID,
			Timestamp: time.Now().UnixMilli(),
			Error:     err.Error(),
		})
		return
	}
	defer h.hub.DetachPeer(req.SessionID, req.Role, conn)

	pingTicker := time.NewTicker(h.pingInterval)
	defer pingTicker.Stop()

	done := make(chan struct{})
	defer close(done)

	go func() {
		for {
			select {
			case <-pingTicker.C:
				_ = conn.WriteControl(websocket.PingMessage, []byte("ping"), time.Now().Add(10*time.Second))
			case <-done:
				return
			}
		}
	}()

	for {
		var msg WalletRelayMessage
		if err := conn.ReadJSON(&msg); err != nil {
			return
		}

		if err := h.hub.ProcessMessage(req.SessionID, req.Role, msg); err != nil {
			h.hub.SendProtocolError(req.SessionID, req.Role, msg.RequestID, err.Error())
		}
	}
}

func (h *WalletRelayHandler) resolveRelayWSBase(r *http.Request) string {
	if h.relayWSBase != "" {
		return h.relayWSBase
	}

	host := strings.TrimSpace(r.Host)
	if host == "" {
		host = "localhost:8080"
	}

	scheme := "ws"
	if r.TLS != nil {
		scheme = "wss"
	}
	if forwarded := strings.ToLower(strings.TrimSpace(r.Header.Get("X-Forwarded-Proto"))); forwarded != "" {
		switch forwarded {
		case "https", "wss":
			scheme = "wss"
		case "http", "ws":
			scheme = "ws"
		}
	}

	return fmt.Sprintf("%s://%s/v1/wallet-relay/ws", scheme, host)
}

func resolveRequestOrigin(r *http.Request) string {
	if r == nil {
		return ""
	}

	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin != "" {
		return origin
	}

	referer := strings.TrimSpace(r.Header.Get("Referer"))
	if referer == "" {
		return ""
	}

	parsed, err := url.Parse(referer)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return ""
	}

	return fmt.Sprintf("%s://%s", parsed.Scheme, parsed.Host)
}
