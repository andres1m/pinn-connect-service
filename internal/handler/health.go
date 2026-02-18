package handler

import (
	"encoding/json"
	"net/http"
	"pinn/internal/domain"
)

func Health(w http.ResponseWriter, r *http.Request) {
	res := domain.HealthResponse{Status: "ok"}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(res)
}
