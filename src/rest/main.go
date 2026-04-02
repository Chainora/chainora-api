package main

import (
	"log"

	"chainora-api/rest/bootstrap"
)

func main() {
	app := bootstrap.Build()
	log.Printf("chainora-api listening on %s", app.Address())
	if err := app.Engine.Run(app.Address()); err != nil {
		log.Fatal(err)
	}
}
