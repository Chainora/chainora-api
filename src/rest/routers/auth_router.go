package routers

import (
	"chainora-api/rest/handler"

	"github.com/gin-gonic/gin"
)

func RegisterAuthRoutes(v1 *gin.RouterGroup, authHandler *handler.AuthHandler) {
	auth := v1.Group("/auth")
	auth.GET("/session", authHandler.InitSession)
	auth.GET("/ws/:sessionId", authHandler.WaitForLoginWS)
	auth.POST("/verify", authHandler.VerifySignature)
}
