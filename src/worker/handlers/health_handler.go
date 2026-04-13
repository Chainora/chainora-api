package handlers

import (
	"encoding/json"
	"net/http"
)

type healthPayload struct {
	OK      bool   `json:"ok"`
	Service string `json:"service"`
}

func Health(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(healthPayload{OK: true, Service: "chainora-worker"})
}
