package main

import (
	"log"
	"os"
	"strings"

	"chainora-api/rest/bootstrap"
)

func main() {
	if len(os.Args) > 1 && strings.EqualFold(strings.TrimSpace(os.Args[1]), "reputation-backfill") {
		poolAddress := ""
		if len(os.Args) > 2 {
			poolAddress = strings.TrimSpace(os.Args[2])
		}
		if err := bootstrap.RunReputationBackfill(poolAddress); err != nil {
			log.Fatalf("reputation backfill failed: %v", err)
		}
		log.Printf("reputation backfill finished successfully")
		return
	}

	app := bootstrap.Build()
	log.Printf("chainora-api listening on %s", app.Address())
	if err := app.Engine.Run(app.Address()); err != nil {
		log.Fatal(err)
	}
}
