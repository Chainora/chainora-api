package middlewares

import (
	"bytes"
	"io"
	"log"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

type responseBodyWriter struct {
	gin.ResponseWriter
	body bytes.Buffer
}

func (w *responseBodyWriter) Write(data []byte) (int, error) {
	w.body.Write(data)
	return w.ResponseWriter.Write(data)
}

func (w *responseBodyWriter) WriteString(s string) (int, error) {
	w.body.WriteString(s)
	return w.ResponseWriter.WriteString(s)
}

func trimPreview(value string) string {
	if len(value) > maxBodyLogPreview {
		return value[:maxBodyLogPreview] + "...(truncated)"
	}
	return value
}

const (
	requestIDHeader   = "X-Request-Id"
	maxBodyLogPreview = 2048
)

func ensureRequestID(c *gin.Context) string {
	requestID := strings.TrimSpace(c.GetHeader(requestIDHeader))
	if requestID == "" {
		requestID = time.Now().UTC().Format("20060102T150405.000000000")
	}
	c.Writer.Header().Set(requestIDHeader, requestID)
	return requestID
}

func readRequestBody(c *gin.Context) string {
	if c.Request == nil || c.Request.Body == nil {
		return ""
	}

	if c.Request.Method == "GET" || c.Request.Method == "HEAD" {
		return ""
	}

	raw, err := io.ReadAll(c.Request.Body)
	if err != nil {
		return "<failed to read body>"
	}

	c.Request.Body = io.NopCloser(bytes.NewBuffer(raw))
	if len(raw) == 0 {
		return ""
	}

	body := string(raw)
	if len(body) > maxBodyLogPreview {
		return body[:maxBodyLogPreview] + "...(truncated)"
	}
	return body
}

func RequestLogger() gin.HandlerFunc {
	return func(c *gin.Context) {
		requestID := ensureRequestID(c)
		requestBody := readRequestBody(c)
		start := time.Now()

		wrapped := &responseBodyWriter{ResponseWriter: c.Writer}
		c.Writer = wrapped

		c.Next()

		latency := time.Since(start)
		status := c.Writer.Status()
		path := c.Request.URL.Path
		if query := c.Request.URL.RawQuery; query != "" {
			path = path + "?" + query
		}
		errText := c.Errors.ByType(gin.ErrorTypeAny).String()
		responseBody := trimPreview(strings.TrimSpace(wrapped.body.String()))

		log.Printf("%s => %s => %d (%s) [request_id=%s]", c.Request.Method, path, status, latency.String(), requestID)

		if status >= 400 || errText != "" {
			log.Printf(
				"%s => %s => %d => request_body=%q response_body=%q err=%q [request_id=%s]",
				c.Request.Method,
				path,
				status,
				requestBody,
				responseBody,
				errText,
				requestID,
			)
		}
	}
}
