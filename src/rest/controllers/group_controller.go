package controllers

import (
	"database/sql"

	"chainora-api/core/usecases"

	"github.com/gin-gonic/gin"
)

type GroupController struct {
	handler *usecases.GroupHandler
}

func NewGroupController(db *sql.DB, issuer usecases.TokenIssuer, rpcURL string) *GroupController {
	return &GroupController{handler: usecases.NewGroupHandler(db, issuer, rpcURL)}
}

func (c *GroupController) ListGroups(ctx *gin.Context)   { c.handler.ListGroups(ctx) }
func (c *GroupController) GetGroupView(ctx *gin.Context) { c.handler.GetGroupView(ctx) }
func (c *GroupController) GetGroup(ctx *gin.Context)     { c.handler.GetGroup(ctx) }
func (c *GroupController) CreateGroup(ctx *gin.Context)  { c.handler.CreateGroup(ctx) }
