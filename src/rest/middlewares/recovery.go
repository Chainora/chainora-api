package middlewares

import (
	"log"
	"net/http"
	"runtime/debug"

	"github.com/gin-gonic/gin"
)

func RecoveryWithStackTrace() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if rec := recover(); rec != nil {
				requestID := ensureRequestID(c)
				stack := string(debug.Stack())
				path := c.Request.URL.Path
				if query := c.Request.URL.RawQuery; query != "" {
					path = path + "?" + query
				}

				log.Printf(
					"%s => %s => %d => panic=%v [request_id=%s] stacktrace=\n%s",
					c.Request.Method,
					path,
					http.StatusInternalServerError,
					rec,
					requestID,
					stack,
				)

				c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{
					"error":       "internal server error",
					"requestId":   requestID,
					"stackLogged": true,
				})
			}
		}()

		c.Next()
	}
}
