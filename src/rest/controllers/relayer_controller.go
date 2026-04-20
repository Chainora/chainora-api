package controllers

import (
	"net/http"

	"chainora-api/core/usecases"

	"github.com/gin-gonic/gin"
)

type RelayerController struct {
	handler *usecases.RelayerHandler
}

func NewRelayerController(relayer usecases.RelayerUsecase, hub *usecases.WSHub, wsOriginChecker func(r *http.Request) bool) *RelayerController {
	return &RelayerController{handler: usecases.NewRelayerHandlerWithOriginChecker(relayer, hub, wsOriginChecker)}
}

func (c *RelayerController) CreateUnsignedPayload(ctx *gin.Context) {
	c.handler.CreateUnsignedPayload(ctx)
}
func (c *RelayerController) CreatePrimaryPayload(ctx *gin.Context) {
	c.handler.CreatePrimaryPayload(ctx)
}
func (c *RelayerController) WaitForRelayerWS(ctx *gin.Context)   { c.handler.WaitForRelayerWS(ctx) }
func (c *RelayerController) RegisterUsername(ctx *gin.Context)   { c.handler.RegisterUsername(ctx) }
func (c *RelayerController) SetPrimaryUsername(ctx *gin.Context) { c.handler.SetPrimaryUsername(ctx) }
