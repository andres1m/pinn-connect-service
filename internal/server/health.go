package server

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"pinn/internal/domain"
)

func (s *Server) HandleHealth(w http.ResponseWriter, r *http.Request) {
	if err := s.healthService.CheckStatus(r.Context()); err != nil {
		slog.Error(err.Error(), err)

		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(domain.HealthResponse{Status: fmt.Sprintf("error: %v", err.Error())})
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
