package server

import (
	"context"
	"log"
	"net/http"
	"os"
	"pinn/internal/service"
	"time"

	"github.com/docker/docker/pkg/stdcopy"
)

func (s *Server) HandleRun(w http.ResponseWriter, r *http.Request) {

}

func (s *Server) HandleRunMock(w http.ResponseWriter, r *http.Request) {
	manager := s.dockerManager
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Minute)
	defer cancel()

	taskService := service.NewTaskService(manager, s.config)
	containerID, err := taskService.RunMock(ctx, "123321")
	if err != nil {
		log.Printf("Error while starting mock task: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	logs, err := manager.GetContainerLogs(ctx, containerID, true)
	if err != nil {
		log.Printf("Ошибка при чтении логов: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer logs.Close()

	_, err = stdcopy.StdCopy(os.Stdout, os.Stderr, logs)
	if err != nil {
		log.Printf("Ошибка при перенаправлении логов в консоль: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	err = manager.RemoveContainer(ctx, containerID)
	if err != nil {
		log.Printf("Error removing container: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}
