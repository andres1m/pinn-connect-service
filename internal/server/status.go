package server

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"pinn/internal/domain"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

func (s *Server) HandleStatus(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		http.Error(w, "missing id", http.StatusBadRequest)
		return
	}

	uuid, err := uuid.Parse(id)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	task, err := s.taskService.GetTask(r.Context(), uuid)
	if err != nil {
		slog.Error("getting task from task service", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	if task == nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(mapTaskToResp(task)); err != nil {
		slog.Error("encoding task status", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
}

func mapTaskToResp(task *domain.Task) *domain.TaskStatusResponse {
	resp := domain.TaskStatusResponse{
		ID:        task.ID.String(),
		Status:    string(task.Status),
		CreatedAt: task.CreatedAt,
	}

	if task.Status == domain.TaskScheduled {
		resp.ScheduledAt = task.ScheduledAt
	}

	if task.Status == domain.TaskRunning {
		resp.StartedAt = task.StartedAt
	}

	if task.Status == domain.TaskFailed {
		resp.StartedAt = task.StartedAt
		resp.ErrLog = task.ErrorLog
	}

	if task.Status == domain.TaskCompleted {
		resp.FinishedAt = task.FinishedAt
		resp.ResultPath = task.ResultPath
		resp.StartedAt = task.StartedAt
	}

	return &resp
}
