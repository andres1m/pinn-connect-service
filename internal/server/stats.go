package server

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"pinn-connect-service/internal/domain"
	"pinn-connect-service/internal/sysstats"
)

// HandleStats godoc
// @Summary      Get host resources statistics
// @Description  Returns CPU utilization and available memory of the host
// @Tags         system
// @Produce      json
// @Success      200  {object}  domain.StatsResponse
// @Router       /stats [get]
func (s *Server) HandleStats(w http.ResponseWriter, r *http.Request) {
	resources, err := sysstats.GetHostResources()
	if err != nil {
		slog.Error("failed to get host resources", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	resp := domain.StatsResponse{
		AvailableMemoryBytes: resources.AvailableMemoryBytes,
		CPUUtilization:       resources.CPUUtilization,
	}

	w.Header().Set("Content-Type", "application/json")

	if err := json.NewEncoder(w).Encode(resp); err != nil {
		slog.Error("failed to encode response", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
}
