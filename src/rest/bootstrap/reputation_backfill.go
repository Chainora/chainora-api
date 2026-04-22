package bootstrap

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"strings"
	"time"

	"chainora-api/core/dbpool"
	"chainora-api/core/usecases"
	restconfig "chainora-api/rest/config"

	_ "github.com/lib/pq"
)

func RunReputationBackfill(poolAddress string) error {
	cfg := restconfig.Load()
	dbURL := strings.TrimSpace(cfg.DBURL)
	if dbURL == "" {
		return fmt.Errorf("database is required for reputation backfill: set DB_URL or DATABASE_URL")
	}

	db, err := sql.Open("postgres", dbURL)
	if err != nil {
		return fmt.Errorf("open postgres for backfill: %w", err)
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		return fmt.Errorf("ping postgres for backfill: %w", err)
	}

	dbpool.ConfigureFromEnv(db, dbpool.Settings{
		MaxOpenConns:    12,
		MaxIdleConns:    6,
		ConnMaxLifetime: 30 * time.Minute,
		ConnMaxIdleTime: 5 * time.Minute,
	})

	handler := usecases.NewGroupHandlerWithOptions(
		db,
		nil,
		cfg.ChainoraRPCURL,
		usecases.GroupHandlerOptions{
			ReputationSyncConfig: buildReputationSyncConfig(cfg),
		},
	)

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Minute)
	defer cancel()

	result, err := handler.RunReputationBackfill(ctx, poolAddress, cfg.ReputationSyncBatchSize)
	if err != nil {
		return err
	}

	log.Printf(
		"[reputation-backfill] completed pool=%s adapter=%s processed=%d updated=%d txs=%d",
		result.PoolAddress,
		result.AdapterAddress,
		result.ProcessedUsers,
		result.UpdatedUsers,
		result.SubmittedTxs,
	)

	return nil
}
