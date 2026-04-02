package constants

import "errors"

var (
	ErrSessionExpired   = errors.New("session expired")
	ErrInvalidSignature = errors.New("invalid signature")
	ErrUserNotFound     = errors.New("user not found")
	ErrInvalidToken     = errors.New("invalid token")
)
