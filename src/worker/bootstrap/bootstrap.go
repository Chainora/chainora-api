package bootstrap

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"

	"chainora-api/worker/config"
	"chainora-api/worker/jobs"
	"chainora-api/worker/orchestrators"
	"chainora-api/worker/routers"
	"chainora-api/worker/scanners"
)

type App struct {
	server    *http.Server
	scheduler *orchestrators.Scheduler
}

func Build(cfg config.Config) *App {
	mux := http.NewServeMux()
	routers.Register(mux)

	scanner := scanners.NewUsernameScanner(cfg.InitiaAPIURL, cfg.RequestTimeout)
	usernameJob := jobs.NewUsernameSyncJob(scanner, cfg.UsernameSyncList)
	scheduler := orchestrators.NewScheduler(cfg.ScanInterval, usernameJob)

	server := &http.Server{
		Addr:              fmt.Sprintf(":%s", cfg.Port),
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	return &App{server: server, scheduler: scheduler}
}

func (a *App) Run(ctx context.Context) error {
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
