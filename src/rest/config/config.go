package config

import (
	"bufio"
	"errors"
	"os"
	"strconv"
	"strings"
	"time"

	"chainora-api/rest/properties"
)

func Load() properties.AppProperties {
	loadEnvFile()

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

	refreshTTLMinutes := 60 * 24 * 7
	if raw := strings.TrimSpace(os.Getenv("JWT_REFRESH_TTL_MINUTES")); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			refreshTTLMinutes = parsed
		}
	}

	return properties.AppProperties{
		ServerPort:          port,
		JWTSecret:           jwtSecret,
		JWTIssuer:           jwtIssuer,
		JWTTTL:              time.Duration(ttlMinutes) * time.Minute,
		JWTRefreshTTL:       time.Duration(refreshTTLMinutes) * time.Minute,
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

func loadEnvFile() {
	if explicit := strings.TrimSpace(os.Getenv("CHAINORA_ENV_FILE")); explicit != "" {
		_ = readEnvLikeFile(explicit)
		return
	}

	defaultCandidates := []string{
		"../migration/config/staging.env",
	}

	for _, path := range defaultCandidates {
		if err := readEnvLikeFile(path); err == nil {
			return
		}
	}
}

func readEnvLikeFile(path string) error {
	file, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return os.ErrNotExist
		}
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		sep := "="
		idx := strings.Index(line, sep)
		if idx <= 0 {
			sep = ":"
			idx = strings.Index(line, sep)
		}
		if idx <= 0 {
			continue
		}

		key := strings.TrimSpace(line[:idx])
		value := strings.TrimSpace(line[idx+1:])
		value = strings.Trim(value, "\"'")
		if key == "" {
			continue
		}

		if _, exists := os.LookupEnv(key); !exists {
			_ = os.Setenv(key, value)
		}
	}

	return scanner.Err()
}
