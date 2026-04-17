package bootstrap

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"chainora-api/core/dbpool"
	"chainora-api/worker/config"
	"chainora-api/worker/jobs"
	"chainora-api/worker/orchestrators"
	"chainora-api/worker/routers"
	"chainora-api/worker/scanners"
	"chainora-api/worker/sources"

	_ "github.com/lib/pq"
)

type App struct {
	server    *http.Server
	scheduler *orchestrators.Scheduler
	db        *sql.DB
}

func Build(cfg config.Config) *App {
	mux := http.NewServeMux()
	routers.Register(mux)

	var db *sql.DB
	var addressSource jobs.UsernameAddressSource
	if strings.TrimSpace(cfg.DBURL) != "" {
		postgresDB, err := sql.Open("postgres", cfg.DBURL)
		if err != nil {
			log.Printf("[worker] postgres open failed for dynamic username sync source: %v", err)
		} else {
			pingCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			pingErr := postgresDB.PingContext(pingCtx)
			cancel()
			if pingErr != nil {
				log.Printf("[worker] postgres ping failed for dynamic username sync source: %v", pingErr)
				_ = postgresDB.Close()
			} else {
				poolSettings := dbpool.ConfigureFromEnv(postgresDB, dbpool.Settings{
					MaxOpenConns:    10,
					MaxIdleConns:    4,
					ConnMaxLifetime: 20 * time.Minute,
					ConnMaxIdleTime: 4 * time.Minute,
				})
				log.Printf(
					"[worker] postgres pool configured max_open=%d max_idle=%d max_lifetime=%s max_idle_time=%s",
					poolSettings.MaxOpenConns,
					poolSettings.MaxIdleConns,
					poolSettings.ConnMaxLifetime,
					poolSettings.ConnMaxIdleTime,
				)
				db = postgresDB
				addressSource = sources.NewPostgresAddressSource(db)
				log.Printf("[worker] username-sync dynamic source enabled via postgres")
			}
		}
	} else {
		log.Printf("[worker] dynamic username source disabled: DB_URL/DATABASE_URL is empty")
	}

	scanner := scanners.NewUsernameScanner(cfg.InitiaAPIURL, cfg.RequestTimeout)
	usernameJob := jobs.NewUsernameSyncJob(scanner, cfg.UsernameSyncList, addressSource)

	rpcURL := strings.TrimSpace(os.Getenv("CHAINORA_RPC_URL"))
	inviteNotificationJob := jobs.NewGroupInviteNotificationJob(db, rpcURL)
	fundingReminderJob := jobs.NewFundingReminderNotificationJob(db)

	scheduler := orchestrators.NewScheduler(cfg.ScanInterval, usernameJob, inviteNotificationJob, fundingReminderJob)

	server := &http.Server{
		Addr:              fmt.Sprintf(":%s", cfg.Port),
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	return &App{server: server, scheduler: scheduler, db: db}
}

func (a *App) Run(ctx context.Context) error {
	defer func() {
		if a.db != nil {
			if err := a.db.Close(); err != nil {
				log.Printf("[worker] postgres close failed: %v", err)
			}
		}
	}()

	go a.scheduler.Run(ctx)

	errCh := make(chan error, 1)
	go func() {
		log.Printf("[worker] http listening on %s", a.server.Addr)
		if err := a.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return a.server.Shutdown(shutdownCtx)
	case err := <-errCh:
		if err != nil {
			return err
		}
		return nil
	}
}
