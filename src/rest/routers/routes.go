package routers

import (
	"chainora-api/rest/controllers"

	"github.com/gin-gonic/gin"
)

func RegisterRoutes(
	v1 *gin.RouterGroup,
	authController *controllers.AuthController,
	txController *controllers.TxController,
	relayerController *controllers.RelayerController,
	walletRelayController *controllers.WalletRelayController,
	cardController *controllers.CardController,
	groupController *controllers.GroupController,
	mediaController *controllers.MediaController,
	notificationController *controllers.NotificationController,
) {
	auth := v1.Group("/auth")
	auth.GET("/session", authController.InitSession)
	auth.GET("/me", authController.Me)
	auth.GET("/profile", authController.GetProfile)
	auth.GET("/profiles", authController.ListProfiles)
	auth.PATCH("/profile", authController.UpdateProfile)
	auth.POST("/verify", authController.VerifySignature)
	auth.POST("/refresh", authController.RefreshToken)

	tx := v1.Group("/tx")
	tx.POST("/decode", txController.DecodeTx)
	tx.POST("/typed-data", txController.BuildTransferTypedData)

	relayer := v1.Group("/relayer")
	relayer.POST("/payload", relayerController.CreateUnsignedPayload)
	relayer.POST("/primary/payload", relayerController.CreatePrimaryPayload)
	relayer.GET("/ws/:sessionId", relayerController.WaitForRelayerWS)
	relayer.POST("/register", relayerController.RegisterUsername)
	relayer.POST("/primary/select", relayerController.SetPrimaryUsername)

	walletRelay := v1.Group("/wallet-relay")
	walletRelay.POST("/pair", walletRelayController.Pair)
	walletRelay.GET("/ws/:sessionId", walletRelayController.ConnectWS)

	card := v1.Group("/card")
	card.POST("/challenge", cardController.CreateChallenge)
	card.POST("/verify", cardController.VerifyChallenge)
	card.POST("/device-attestation", cardController.CreateDeviceAttestation)

	groups := v1.Group("/groups")
	groups.GET("", groupController.ListGroups)
	groups.GET("/:poolId/view", groupController.GetGroupView)
	groups.GET("/:poolId/sync-status", groupController.GetGroupSyncStatus)
	groups.GET("/:poolId", groupController.GetGroup)
	groups.POST("", groupController.CreateGroup)

	media := v1.Group("/media")
	media.POST("/upload", mediaController.UploadImage)

	notifications := v1.Group("/notifications")
	notifications.GET("", notificationController.ListNotifications)
	notifications.DELETE("", notificationController.ClearAll)
	notifications.GET("/unread-count", notificationController.UnreadCount)
	notifications.PATCH("/read-all", notificationController.MarkReadAll)
	notifications.PATCH("/:id/read", notificationController.MarkRead)
}
