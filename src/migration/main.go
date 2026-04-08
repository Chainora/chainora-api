package main

import (
	"bufio"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	_ "github.com/lib/pq"
)

const (
	schemaMigrationsTable = "schema_migrations"
)

type migrationConfig struct {
	DatabaseURL   string
	MigrationsDir string
}

func main() {
	cfg, err := loadConfig()
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	db, err := sql.Open("postgres", cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		log.Fatalf("ping db: %v", err)
	}

	if err := ensureMigrationTable(db); err != nil {
		log.Fatalf("ensure migration table: %v", err)
	}

	migrations, err := listMigrationFiles(cfg.MigrationsDir)
	if err != nil {
		log.Fatalf("list migrations: %v", err)
	}
	if len(migrations) == 0 {
		log.Println("No pending migrations found.")
		return
	}

	applied, err := loadAppliedMigrations(db)
	if err != nil {
		log.Fatalf("load applied migrations: %v", err)
	}

	for _, filename := range migrations {
		if _, ok := applied[filename]; ok {
			continue
		}

		path := filepath.Join(cfg.MigrationsDir, filename)
		if err := applyMigration(db, filename, path); err != nil {
			log.Fatalf("apply migration %s: %v", filename, err)
		}
	}

	log.Println("Migrations completed successfully.")
}

func loadConfig() (migrationConfig, error) {
	cfg := migrationConfig{
		MigrationsDir: "migrations",
	}

	configFile := strings.TrimSpace(os.Getenv("MIGRATION_CONFIG_FILE"))
	if configFile == "" {
		configFile = "config/.env"
	}

	if configFile != "" {
		if err := readEnvLikeFile(configFile); err != nil {
			return migrationConfig{}, fmt.Errorf("read migration config file: %w", err)
		}
	}

	cfg.DatabaseURL = strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if cfg.DatabaseURL == "" {
		host := envOrDefault("POSTGRES_HOST", "localhost")
		port := envOrDefault("POSTGRES_PORT", "5432")
		user := envOrDefault("POSTGRES_USER", "chainora")
		pass := envOrDefault("POSTGRES_PASSWORD", "chainora")
		db := envOrDefault("POSTGRES_DB", "chainora")
		cfg.DatabaseURL = fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable", user, pass, host, port, db)
	}

	if dir := strings.TrimSpace(os.Getenv("MIGRATIONS_DIR")); dir != "" {
		cfg.MigrationsDir = dir
	}

	return cfg, nil
}

func envOrDefault(key, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}

func readEnvLikeFile(path string) error {
	file, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
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

func ensureMigrationTable(db *sql.DB) error {
	query := fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s (
			filename TEXT PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)
	`, schemaMigrationsTable)

	_, err := db.Exec(query)
	return err
}

func listMigrationFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	files := make([]string, 0)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasSuffix(name, ".up.sql") {
			files = append(files, name)
		}
	}

	sort.Strings(files)
	return files, nil
}

func loadAppliedMigrations(db *sql.DB) (map[string]struct{}, error) {
	rows, err := db.Query(fmt.Sprintf("SELECT filename FROM %s", schemaMigrationsTable))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	applied := make(map[string]struct{})
	for rows.Next() {
		var filename string
		if err := rows.Scan(&filename); err != nil {
			return nil, err
		}
		applied[filename] = struct{}{}
	}

	return applied, rows.Err()
}

func applyMigration(db *sql.DB, filename, path string) error {
	sqlBytes, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	log.Printf("Applying migration: %s...", filename)

	tx, err := db.Begin()
	if err != nil {
		return err
	}

	rollback := func(execErr error) error {
		_ = tx.Rollback()
		return execErr
	}

	if _, err := tx.Exec(string(sqlBytes)); err != nil {
		return rollback(err)
	}

	if _, err := tx.Exec(
		fmt.Sprintf("INSERT INTO %s (filename) VALUES ($1)", schemaMigrationsTable),
		filename,
	); err != nil {
		return rollback(err)
	}

	if err := tx.Commit(); err != nil {
		return rollback(err)
	}

	log.Printf("Applying migration: %s... Success", filename)
	return nil
}
