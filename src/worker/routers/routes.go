package routers

import (
	"net/http"

	"chainora-api/worker/handlers"
)

func Register(mux *http.ServeMux) {
	mux.HandleFunc("/healthz", handlers.Health)
	mux.HandleFunc("/readyz", handlers.Health)
}
