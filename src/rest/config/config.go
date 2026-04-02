package config

import (
	"os"
	"strconv"
	"strings"
	"time"

	"chainora-api/rest/properties"
)

func Load() properties.AppProperties {
	port := readEnv("PORT", "8080")
	jwtSecret := readEnv("JWT_SECRET", "chainora-dev-secret")
	jwtIssuer := readEnv("JWT_ISSUER", "chainora-api")
	authTemplate := readEnv("AUTH_MESSAGE_TEMPLATE", "Sign this to login to Chainora: %s")
	initiaRPC := readEnv("INITIA_RPC_URL", "http://23.94.63.207:8545")
	dbURL := readEnv("DB_URL", readEnv("DATABASE_URL", ""))

	ttlMinutes := 15
	if raw := strings.TrimSpace(os.Getenv("JWT_TTL_MINUTES")); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			ttlMinutes = parsed
		}
	}

	return properties.AppProperties{
		ServerPort:          port,
		JWTSecret:           jwtSecret,
		JWTIssuer:           jwtIssuer,
		JWTTTL:              time.Duration(ttlMinutes) * time.Minute,
		AuthMessageTemplate: authTemplate,
		InitiaRPCURL:        initiaRPC,
		DBURL:               dbURL,
	}
}

func readEnv(key, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}
