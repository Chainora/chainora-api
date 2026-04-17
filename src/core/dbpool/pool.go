package dbpool

import (
	"database/sql"
	"os"
	"strconv"
	"strings"
	"time"
)

type Settings struct {
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
	ConnMaxIdleTime time.Duration
}

func ConfigureFromEnv(db *sql.DB, defaults Settings) Settings {
	if db == nil {
		return defaults
	}

	applied := Settings{
		MaxOpenConns:    envInt("CHAINORA_DB_MAX_OPEN_CONNS", envInt("DB_MAX_OPEN_CONNS", defaults.MaxOpenConns)),
		MaxIdleConns:    envInt("CHAINORA_DB_MAX_IDLE_CONNS", envInt("DB_MAX_IDLE_CONNS", defaults.MaxIdleConns)),
		ConnMaxLifetime: envDurationSeconds("CHAINORA_DB_CONN_MAX_LIFETIME_SECONDS", envDurationSeconds("DB_CONN_MAX_LIFETIME_SECONDS", defaults.ConnMaxLifetime)),
		ConnMaxIdleTime: envDurationSeconds("CHAINORA_DB_CONN_MAX_IDLE_TIME_SECONDS", envDurationSeconds("DB_CONN_MAX_IDLE_TIME_SECONDS", defaults.ConnMaxIdleTime)),
	}

	if applied.MaxOpenConns < 1 {
		applied.MaxOpenConns = defaults.MaxOpenConns
	}
	if applied.MaxIdleConns < 0 {
		applied.MaxIdleConns = defaults.MaxIdleConns
	}
	if applied.MaxIdleConns > applied.MaxOpenConns {
		applied.MaxIdleConns = applied.MaxOpenConns
	}
	if applied.ConnMaxLifetime < 0 {
		applied.ConnMaxLifetime = defaults.ConnMaxLifetime
	}
	if applied.ConnMaxIdleTime < 0 {
		applied.ConnMaxIdleTime = defaults.ConnMaxIdleTime
	}

	db.SetMaxOpenConns(applied.MaxOpenConns)
	db.SetMaxIdleConns(applied.MaxIdleConns)
	db.SetConnMaxLifetime(applied.ConnMaxLifetime)
	db.SetConnMaxIdleTime(applied.ConnMaxIdleTime)

	return applied
}

func envInt(key string, fallback int) int {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}

	parsed, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return parsed
}

func envDurationSeconds(key string, fallback time.Duration) time.Duration {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}

	parsed, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return time.Duration(parsed) * time.Second
}
