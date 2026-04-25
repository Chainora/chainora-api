package controllers

import (
	"net/http"
	"strings"
	"time"

	"chainora-api/core/usecases"

	"github.com/gin-gonic/gin"
)

type WalletRelayController struct {
	handler *usecases.WalletRelayHandler
}

func NewWalletRelayController(
	hub *usecases.RelayHub,
	wsOriginChecker func(r *http.Request) bool,
	relayWSBase string,
	pingInterval time.Duration,
) *WalletRelayController {
	return &WalletRelayController{
		handler: usecases.NewWalletRelayHandler(
			hub,
			wsOriginChecker,
			strings.TrimSpace(relayWSBase),
			pingInterval,
		),
	}
}

func (c *WalletRelayController) Pair(ctx *gin.Context)      { c.handler.Pair(ctx) }
func (c *WalletRelayController) ConnectWS(ctx *gin.Context) { c.handler.ConnectWS(ctx) }
