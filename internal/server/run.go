package server

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"pinn/internal/domain"

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
	var fileHashBytes []byte

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
			taskProcessed = true

		case "file":
			task.InputFilename = part.FileName()

			fileHashBytes, err = s.taskService.SaveInput(task.ID, task.InputFilename, part)
			if err != nil {
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

	err = s.taskService.CreateTask(r.Context(), &task, fileHashBytes)
	if err != nil {
		switch {
		case errors.Is(err, domain.ErrModelNotFound):
			http.Error(w, "model not found", http.StatusBadRequest)
		default:
			slog.Error("error creating task", "error", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
		}
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	if err := json.NewEncoder(w).Encode(map[string]string{"task_id": task.ID.String()}); err != nil {
		slog.Error("failed to write response", "task_id", task.ID, "error", err)
	}
}

func mapReqToTask(req *domain.CreateTaskRequest, task *domain.Task) {
	task.ModelID = req.ModelID
	task.ContainerCmd = req.ContainerCmd
	task.ContainerEnvs = req.ContainerEnvs
	task.CPULim = req.CPULimit
	task.MemLim = req.MemoryLimit
	task.GPUEnabled = req.GPUEnabled
	task.ScheduledAt = req.ScheduledAt
}
