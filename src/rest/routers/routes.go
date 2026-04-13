package routers

import (
	"chainora-api/rest/controllers"
	"chainora-api/rest/handler"

	"github.com/gin-gonic/gin"
)

func RegisterRoutes(v1 *gin.RouterGroup, authHandler *handler.AuthHandler, txController *controllers.TxController, relayerHandler *handler.RelayerHandler, cardHandler *handler.CardHandler) {
	auth := v1.Group("/auth")
	auth.GET("/session", authHandler.InitSession)
	auth.GET("/ws/:sessionId", authHandler.WaitForLoginWS)
	auth.GET("/me", authHandler.Me)
	auth.GET("/profile", authHandler.GetProfile)
	auth.POST("/progress", authHandler.NotifyProgress)
	auth.POST("/verify", authHandler.VerifySignature)
	auth.POST("/refresh", authHandler.RefreshToken)

	tx := v1.Group("/tx")
	tx.POST("/decode", txController.DecodeTx)
	tx.POST("/typed-data", txController.BuildTransferTypedData)

	relayer := v1.Group("/relayer")
	relayer.POST("/payload", relayerHandler.CreateUnsignedPayload)
	relayer.POST("/primary/payload", relayerHandler.CreatePrimaryPayload)
	relayer.GET("/ws/:sessionId", relayerHandler.WaitForRelayerWS)
	relayer.POST("/register", relayerHandler.RegisterUsername)
	relayer.POST("/primary/select", relayerHandler.SetPrimaryUsername)

	card := v1.Group("/card")
	card.POST("/challenge", cardHandler.CreateChallenge)
	card.POST("/verify", cardHandler.VerifyChallenge)
}
