package routers

import (
	"chainora-api/rest/controllers"
	"chainora-api/rest/handler"

	"github.com/gin-gonic/gin"
)

func RegisterRoutes(
	v1 *gin.RouterGroup,
	authHandler *handler.AuthHandler,
	txController *controllers.TxController,
	relayerHandler *handler.RelayerHandler,
	cardHandler *handler.CardHandler,
	groupHandler *handler.GroupHandler,
	mediaHandler *handler.MediaHandler,
	notificationHandler *handler.NotificationHandler,
) {
	auth := v1.Group("/auth")
	auth.GET("/session", authHandler.InitSession)
	auth.GET("/ws/:sessionId", authHandler.WaitForLoginWS)
	auth.GET("/me", authHandler.Me)
	auth.GET("/profile", authHandler.GetProfile)
	auth.PATCH("/profile", authHandler.UpdateProfile)
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
	card.POST("/device-attestation", cardHandler.CreateDeviceAttestation)

	groups := v1.Group("/groups")
	groups.GET("", groupHandler.ListGroups)
	groups.GET("/:poolId", groupHandler.GetGroup)
	groups.POST("", groupHandler.CreateGroup)

	media := v1.Group("/media")
	media.POST("/upload", mediaHandler.UploadImage)

	notifications := v1.Group("/notifications")
	notifications.GET("", notificationHandler.ListNotifications)
	notifications.GET("/unread-count", notificationHandler.UnreadCount)
	notifications.PATCH("/read-all", notificationHandler.MarkReadAll)
	notifications.PATCH("/:id/read", notificationHandler.MarkRead)
}
