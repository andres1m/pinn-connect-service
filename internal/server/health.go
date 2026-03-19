package server

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"pinn-connect-service/internal/domain"
)

// HandleHealth godoc
// @Summary      Check service health
// @Description  Returns the health status of the application and its dependencies
// @Tags         system
// @Produce      json
// @Success      200  {object}  domain.HealthResponse
// @Failure      503  {object}  domain.HealthResponse
// @Router       /health [get]
func (s *Server) HandleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if err := s.healthService.CheckStatus(r.Context()); err != nil {
		slog.Error(err.Error())

		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(domain.HealthResponse{Status: fmt.Sprintf("error: %v", err.Error())})
		return
	}

	if err := json.NewEncoder(w).Encode(domain.HealthResponse{Status: "ok"}); err != nil {
		slog.Error("failed to encode health response", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
}
