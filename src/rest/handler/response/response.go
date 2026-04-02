package response

import (
	"encoding/json"
	"errors"
	"net/http"

	"chainora-api/core/constants"

	"github.com/gin-gonic/gin"
	"github.com/go-playground/validator/v10"
)

type Envelope struct {
	Success bool   `json:"success"`
	Data    any    `json:"data,omitempty"`
	Error   string `json:"error,omitempty"`
}

func Ok(data any) Envelope {
	return Envelope{Success: true, Data: data}
}

func Write(w http.ResponseWriter, payload Envelope) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(payload)
}

func WriteError(ctx *gin.Context, err error) {
	status := http.StatusBadRequest

	switch {
	case errors.Is(err, constants.ErrSessionExpired):
		status = http.StatusGone
	case errors.Is(err, constants.ErrInvalidSignature):
		status = http.StatusUnauthorized
	case errors.Is(err, constants.ErrUserNotFound):
		status = http.StatusNotFound
	default:
		var validationErrs validator.ValidationErrors
		if errors.As(err, &validationErrs) {
			status = http.StatusBadRequest
		}
	}

	ctx.JSON(status, Envelope{Success: false, Error: err.Error()})
}
