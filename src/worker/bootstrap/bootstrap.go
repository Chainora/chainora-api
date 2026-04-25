package bootstrap

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"net/http"
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
	server              *http.Server
	scheduler           *orchestrators.Scheduler
	projectionScheduler *orchestrators.Scheduler
	db                  *sql.DB
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

	rpcURL := strings.TrimSpace(cfg.ChainoraRPCURL)
	if db == nil {
		log.Printf("[worker] group invite notifications disabled: database is unavailable (set DB_URL or DATABASE_URL)")
	} else if rpcURL == "" {
		log.Printf("[worker] group invite notifications disabled: CHAINORA_RPC_URL is empty")
	}
	inviteNotificationJob := jobs.NewGroupInviteNotificationJob(db, rpcURL)
	if inviteNotificationJob != nil {
		log.Printf("[worker] group invite notifications enabled")
	}
	groupProjectionJob := jobs.NewGroupStateProjectionJob(db, rpcURL)
	if groupProjectionJob == nil {
		if db == nil {
			log.Printf("[worker] group state projection disabled: database is unavailable (set DB_URL or DATABASE_URL)")
		} else {
			log.Printf("[worker] group state projection disabled: CHAINORA_RPC_URL is empty or unavailable")
		}
	} else {
		log.Printf("[worker] group state projection enabled")
	}
	fundingReminderJob := jobs.NewFundingReminderNotificationJob(db, rpcURL)
	if fundingReminderJob == nil {
		if db == nil {
			log.Printf("[worker] funding reminder notifications disabled: database is unavailable (set DB_URL or DATABASE_URL)")
		} else {
			log.Printf("[worker] funding reminder notifications disabled: CHAINORA_RPC_URL is empty or unavailable")
		}
	} else {
		log.Printf("[worker] funding reminder notifications enabled")
	}

	scheduler := orchestrators.NewScheduler(
		cfg.ScanInterval,
		usernameJob,
		inviteNotificationJob,
		fundingReminderJob,
	)
	projectionScheduler := orchestrators.NewScheduler(5*time.Second, groupProjectionJob)

	server := &http.Server{
		Addr:              fmt.Sprintf(":%s", cfg.Port),
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	return &App{
		server:              server,
		scheduler:           scheduler,
		projectionScheduler: projectionScheduler,
		db:                  db,
	}
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
	if a.projectionScheduler != nil {
		go a.projectionScheduler.Run(ctx)
	}

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
