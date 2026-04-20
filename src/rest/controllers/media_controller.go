package controllers

import (
	"chainora-api/core/usecases"

	"github.com/gin-gonic/gin"
)

type MediaController struct {
	handler *usecases.MediaHandler
}

func NewMediaController(issuer usecases.TokenIssuer, cloudName, apiKey, apiSecret, uploadPreset string) *MediaController {
	return &MediaController{handler: usecases.NewMediaHandler(issuer, cloudName, apiKey, apiSecret, uploadPreset)}
}

func (c *MediaController) UploadImage(ctx *gin.Context) { c.handler.UploadImage(ctx) }
