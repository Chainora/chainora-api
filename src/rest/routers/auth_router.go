package routers

import (
	"chainora-api/rest/handler"

	"github.com/gin-gonic/gin"
)

func RegisterAuthRoutes(v1 *gin.RouterGroup, authHandler *handler.AuthHandler) {
	auth := v1.Group("/auth")
	auth.GET("/session", authHandler.InitSession)
	auth.GET("/ws/:sessionId", authHandler.WaitForLoginWS)
	auth.GET("/me", authHandler.Me)
	auth.GET("/profile", authHandler.GetProfile)
	auth.POST("/progress", authHandler.NotifyProgress)
	auth.POST("/verify", authHandler.VerifySignature)
	auth.POST("/refresh", authHandler.RefreshToken)
	auth.PUT("/profile", authHandler.UpdateProfile)
}
