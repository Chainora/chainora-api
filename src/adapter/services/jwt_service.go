package services

import (
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// JWTService signs auth tokens for verified sessions.
type JWTService struct {
	secret []byte
	issuer string
	ttl    time.Duration
}

func NewJWTService(secret, issuer string, ttl time.Duration) *JWTService {
	if issuer == "" {
		issuer = "chainora-api"
	}
	if ttl <= 0 {
		ttl = 15 * time.Minute
	}
	return &JWTService{secret: []byte(secret), issuer: issuer, ttl: ttl}
}

func (s *JWTService) GenerateToken(sessionID, address string) (string, error) {
	now := time.Now().UTC()
	claims := jwt.MapClaims{
		"iss":        s.issuer,
		"sub":        address,
		"session_id": sessionID,
		"iat":        now.Unix(),
		"exp":        now.Add(s.ttl).Unix(),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString(s.secret)
	if err != nil {
		return "", fmt.Errorf("sign jwt: %w", err)
	}
	return signed, nil
}
