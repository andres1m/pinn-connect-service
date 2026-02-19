package server

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"pinn/internal/domain"
)

func (s *Server) HandleHealth(w http.ResponseWriter, r *http.Request) {
	if _, err := s.dockerManager.Client.Ping(r.Context()); err != nil {
		slog.Error("Docker daemon ping failed", "error", err)

		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(domain.HealthResponse{Status: "error: docker unavailable"})
		return
	}

	res := domain.HealthResponse{Status: "ok"}

	resBytes, err := json.Marshal(res)
	if err != nil {
		slog.Error("failed to encode health response", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(resBytes)
}
