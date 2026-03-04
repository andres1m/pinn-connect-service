package server

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"pinn/internal/domain"
	"time"
)

func (s *Server) HandleRun(w http.ResponseWriter, r *http.Request) {
	//TODO implement
	// generate uuid
	// create workspace
	// mark as queued
}

func (s *Server) HandleRunMock(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 1*time.Minute)
	defer cancel()

	containerID, err := s.taskService.RunMock(ctx)
	if err != nil {
		slog.Error("Error while starting mock task", "error", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	err = json.NewEncoder(w).Encode(domain.RunMockResponse{ContainerID: containerID})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
