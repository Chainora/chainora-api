package controllers

import (
	"database/sql"
	"net/http"

	"chainora-api/core/usecases"

	"github.com/gin-gonic/gin"
)

type AuthController struct {
	handler *usecases.AuthHandler
}

func NewAuthController(
	authUsecase usecases.AuthUsecase,
	issuer usecases.TokenIssuer,
	db *sql.DB,
	hub *usecases.WSHub,
	usernameResolver usecases.UsernameResolver,
	wsOriginChecker func(r *http.Request) bool,
) *AuthController {
	return &AuthController{handler: usecases.NewAuthHandler(authUsecase, issuer, db, hub, usernameResolver, wsOriginChecker)}
}

func (c *AuthController) InitSession(ctx *gin.Context)     { c.handler.InitSession(ctx) }
func (c *AuthController) WaitForLoginWS(ctx *gin.Context)  { c.handler.WaitForLoginWS(ctx) }
func (c *AuthController) NotifyProgress(ctx *gin.Context)  { c.handler.NotifyProgress(ctx) }
func (c *AuthController) VerifySignature(ctx *gin.Context) { c.handler.VerifySignature(ctx) }
func (c *AuthController) RefreshToken(ctx *gin.Context)    { c.handler.RefreshToken(ctx) }
func (c *AuthController) Me(ctx *gin.Context)              { c.handler.Me(ctx) }
func (c *AuthController) GetProfile(ctx *gin.Context)      { c.handler.GetProfile(ctx) }
func (c *AuthController) ListProfiles(ctx *gin.Context)    { c.handler.ListProfiles(ctx) }
func (c *AuthController) UpdateProfile(ctx *gin.Context)   { c.handler.UpdateProfile(ctx) }
