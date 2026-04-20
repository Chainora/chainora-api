package controllers

import (
	"chainora-api/core/usecases"

	"github.com/gin-gonic/gin"
)

type CardController struct {
	handler *usecases.CardHandler
}

func NewCardController(repo usecases.AuthRepository, factoryRootPublicKeyHex, chainoraRPCURL, deviceVerifierPrivateKeyHex string) (*CardController, error) {
	handler, err := usecases.NewCardHandler(repo, factoryRootPublicKeyHex, chainoraRPCURL, deviceVerifierPrivateKeyHex)
	if err != nil {
		return nil, err
	}
	return &CardController{handler: handler}, nil
}

func (c *CardController) CreateChallenge(ctx *gin.Context) { c.handler.CreateChallenge(ctx) }
func (c *CardController) VerifyChallenge(ctx *gin.Context) { c.handler.VerifyChallenge(ctx) }
func (c *CardController) CreateDeviceAttestation(ctx *gin.Context) {
	c.handler.CreateDeviceAttestation(ctx)
}
