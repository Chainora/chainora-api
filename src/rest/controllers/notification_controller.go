package controllers

import (
	"database/sql"

	"chainora-api/core/usecases"

	"github.com/gin-gonic/gin"
)

type NotificationController struct {
	handler *usecases.NotificationHandler
}

func NewNotificationController(db *sql.DB, issuer usecases.TokenIssuer) *NotificationController {
	return &NotificationController{handler: usecases.NewNotificationHandler(db, issuer)}
}

func (c *NotificationController) ListNotifications(ctx *gin.Context) {
	c.handler.ListNotifications(ctx)
}
func (c *NotificationController) UnreadCount(ctx *gin.Context) { c.handler.UnreadCount(ctx) }
func (c *NotificationController) MarkRead(ctx *gin.Context)    { c.handler.MarkRead(ctx) }
func (c *NotificationController) MarkReadAll(ctx *gin.Context) { c.handler.MarkReadAll(ctx) }
func (c *NotificationController) ClearAll(ctx *gin.Context)    { c.handler.ClearAll(ctx) }
