package main

import (
	"context"
	"log"
	"os/signal"
	"syscall"

	"chainora-api/worker/bootstrap"
	"chainora-api/worker/config"
)

func main() {
	cfg := config.Load()
	app := bootstrap.Build(cfg)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := app.Run(ctx); err != nil {
		log.Fatalf("[worker] stopped with error: %v", err)
	}

	log.Printf("[worker] graceful shutdown complete")
}
