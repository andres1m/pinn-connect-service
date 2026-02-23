package server

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"pinn/internal/domain"
	"time"
)

func (s *Server) HandleRun(w http.ResponseWriter, r *http.Request) {

}

func (s *Server) HandleRunMock(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Minute)
	defer cancel()

	containerID, err := s.taskService.RunMock(ctx, "mockrun")
	if err != nil {
		log.Printf("Error while starting mock task: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	json.NewEncoder(w).Encode(domain.RunMockResponse{ContainerID: containerID})
}
