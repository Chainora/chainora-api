package requests

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// Serialize binds request data based on HTTP method.
func Serialize(ctx *gin.Context, req any) error {
	switch ctx.Request.Method {
	case http.MethodGet, http.MethodDelete:
		return ctx.ShouldBindQuery(req)
	default:
		return ctx.ShouldBindJSON(req)
	}
}
