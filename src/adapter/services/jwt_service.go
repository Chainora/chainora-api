package services

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	"chainora-api/core/constants"

	"github.com/golang-jwt/jwt/v5"
)

// JWTService signs auth tokens for verified sessions.
type JWTService struct {
	secret     []byte
	issuer     string
	accessTTL  time.Duration
	refreshTTL time.Duration
}

func NewJWTService(secret, issuer string, accessTTL, refreshTTL time.Duration) *JWTService {
	if issuer == "" {
		issuer = "chainora-api"
	}
	if accessTTL <= 0 {
		accessTTL = 15 * time.Minute
	}
	if refreshTTL <= 0 {
		refreshTTL = 7 * 24 * time.Hour
	}
	return &JWTService{
		secret:     []byte(secret),
		issuer:     issuer,
		accessTTL:  accessTTL,
		refreshTTL: refreshTTL,
	}
}

func (s *JWTService) GenerateToken(sessionID, address string) (string, error) {
	return s.mintToken(sessionID, address, "access", s.accessTTL)
}

func (s *JWTService) GenerateTokenPair(sessionID, address string) (string, string, error) {
	accessToken, err := s.mintToken(sessionID, address, "access", s.accessTTL)
	if err != nil {
		return "", "", err
	}

	refreshToken, err := s.mintToken(sessionID, address, "refresh", s.refreshTTL)
	if err != nil {
		return "", "", err
	}

	return accessToken, refreshToken, nil
}

func (s *JWTService) RefreshFromToken(refreshToken string) (string, string, string, error) {
	sessionID, address, err := s.parseToken(refreshToken, "refresh")
	if err != nil {
		return "", "", "", err
	}

	accessToken, nextRefreshToken, err := s.GenerateTokenPair(sessionID, address)
	if err != nil {
		return "", "", "", fmt.Errorf("refresh token pair: %w", err)
	}

	return accessToken, nextRefreshToken, address, nil
}

func (s *JWTService) mintToken(sessionID, address, tokenType string, ttl time.Duration) (string, error) {
	tokenID, err := generateTokenID()
	if err != nil {
		return "", fmt.Errorf("generate token id: %w", err)
	}

	now := time.Now().UTC()
	claims := jwt.MapClaims{
		"iss":        s.issuer,
		"sub":        address,
		"session_id": sessionID,
		"token_type": tokenType,
		"jti":        tokenID,
		"iat":        now.Unix(),
		"exp":        now.Add(ttl).Unix(),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString(s.secret)
	if err != nil {
		return "", fmt.Errorf("sign jwt: %w", err)
	}
	return signed, nil
}

func generateTokenID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}

	return hex.EncodeToString(b), nil
}

func (s *JWTService) parseRefreshToken(refreshToken string) (string, string, error) {
	return s.parseToken(refreshToken, "refresh")
}

func (s *JWTService) ParseAccessToken(accessToken string) (string, string, error) {
	return s.parseToken(accessToken, "access")
}

func (s *JWTService) parseToken(rawToken, expectedType string) (string, string, error) {
	claims := jwt.MapClaims{}
	parsed, err := jwt.ParseWithClaims(rawToken, claims, func(token *jwt.Token) (any, error) {
		if token.Method != jwt.SigningMethodHS256 {
			return nil, fmt.Errorf("%w: unsupported signing method", constants.ErrInvalidToken)
		}
		return s.secret, nil
	}, jwt.WithIssuer(s.issuer), jwt.WithExpirationRequired())
	if err != nil {
		return "", "", fmt.Errorf("%w: parse token: %v", constants.ErrInvalidToken, err)
	}

	if !parsed.Valid {
		return "", "", fmt.Errorf("%w: malformed claims", constants.ErrInvalidToken)
	}

	tokenType, _ := claims["token_type"].(string)
	if tokenType != expectedType {
		return "", "", fmt.Errorf("%w: invalid token type", constants.ErrInvalidToken)
	}

	address, _ := claims["sub"].(string)
	sessionID, _ := claims["session_id"].(string)
	if address == "" || sessionID == "" {
		return "", "", fmt.Errorf("%w: missing token claims", constants.ErrInvalidToken)
	}

	return sessionID, address, nil
}
