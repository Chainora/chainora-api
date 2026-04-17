package constants

import "errors"

var (
	ErrSessionExpired   = errors.New("session expired")
	ErrInvalidSignature = errors.New("invalid signature")
	ErrUserNotFound     = errors.New("user not found")
	ErrNotFound         = errors.New("not found")
	ErrInvalidToken     = errors.New("invalid token")
	ErrForbidden        = errors.New("forbidden")
	ErrRateLimited      = errors.New("rate limited")
	ErrConflict         = errors.New("conflict")
)
