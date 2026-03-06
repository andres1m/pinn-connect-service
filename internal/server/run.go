package server

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"pinn/internal/domain"
	"time"

	"github.com/google/uuid"
)

func (s *Server) HandleRun(w http.ResponseWriter, r *http.Request) {
	mr, err := r.MultipartReader()
	if err != nil {
		http.Error(w, "invalid multipart request", http.StatusBadRequest)
		return
	}

	task := domain.Task{ID: uuid.New()}
	var fileProcessed, taskProcessed bool
	var success bool

	if err := s.taskService.PrepareWorkspace(task.ID); err != nil {
		slog.Error("failed to prepare workspace", "task_id", task.ID, "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	defer func() {
		if !success {
			if err := s.taskService.CleanupWorkspace(task.ID); err != nil {
				slog.Error("failed to cleanup workspace", "task_id", task.ID, "error", err)
			}
		}
	}()

	for {
		part, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			http.Error(w, "error reading multipart", http.StatusBadRequest)
			return
		}

		switch part.FormName() {
		case "task":
			req := domain.CreateTaskRequest{}
			if err := json.NewDecoder(part).Decode(&req); err != nil {
				http.Error(w, "invalid json metadata", http.StatusBadRequest)
				return
			}

			mapReqToTask(&req, &task)

			if err := s.taskService.Create(r.Context(), &task); err != nil {
				slog.Error("saving task using task service", "error", err)
				http.Error(w, "internal error", http.StatusInternalServerError)
				return
			}

			taskProcessed = true

		case "file":
			task.InputFilename = part.FileName()
			if err := s.taskService.SaveInput(task.ID, task.InputFilename, part); err != nil {
				slog.Error("save input failed", "task_id", task.ID, "error", err)
				http.Error(w, "internal error", http.StatusInternalServerError)
				return
			}
			fileProcessed = true
		}
	}

	if !fileProcessed || !taskProcessed {
		http.Error(w, "both 'file' and 'task' parts are required", http.StatusBadRequest)
		return
	}

	if task.ScheduledAt != nil && task.ScheduledAt.After(time.Now()) {
		if err := s.taskService.Mark(r.Context(), &task, domain.TaskScheduled); err != nil {
			slog.Error("marking task as scheduled", "error", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
	} else {
		if err := s.taskService.Mark(r.Context(), &task, domain.TaskQueued); err != nil {
			slog.Error("marking task as queued", "error", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
	}

	success = true

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	if err := json.NewEncoder(w).Encode(map[string]string{"task_id": task.ID.String()}); err != nil {
		slog.Error("failed to write response", "task_id", task.ID, "error", err)
	}
}

func mapReqToTask(req *domain.CreateTaskRequest, task *domain.Task) {
	task.ModelID = req.ModelID
	task.ContainerImage = req.ContainerImage
	task.ContainerCmd = req.ContainerCmd
	task.ContainerEnvs = req.ContainerEnvs
	task.CPULim = req.CPULimit
	task.MemLim = req.MemoryLimit
	task.GPUEnabled = req.GPUEnabled
	task.ScheduledAt = req.ScheduledAt
}
