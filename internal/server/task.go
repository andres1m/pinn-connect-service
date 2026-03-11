package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"pinn/internal/domain"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

func (s *Server) HandleTaskRun(w http.ResponseWriter, r *http.Request) {
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

			if req.CPULimit != 0 && req.CPULimit > s.config.MaxCPUByTask {
				fmt.Println(s.config.MaxCPUByTask)
				http.Error(w, "cpu limit exceeded", http.StatusBadRequest)
				return
			}

			if req.MemoryLimit != 0 && req.MemoryLimit > s.config.MaxMemByTask {
				http.Error(w, "memory limit exceeded", http.StatusBadRequest)
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

func (s *Server) HandleTaskStatus(w http.ResponseWriter, r *http.Request) {
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

func (s *Server) HandleTaskStop(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		http.Error(w, "missing id", http.StatusBadRequest)
		return
	}

	uuID, err := uuid.Parse(id)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	var timeout time.Duration

	timeoutStr := r.URL.Query().Get("timeout")
	if timeoutStr == "" {
		timeout = s.config.Server.DefaultTaskStopTimeout
	} else if sec, err := strconv.Atoi(timeoutStr); err == nil {
		timeout = time.Duration(sec) * time.Second
	} else {
		timeout, err = time.ParseDuration(timeoutStr)
		if err != nil {
			http.Error(w, "invalid timeout format, expected seconds or duration string", http.StatusBadRequest)
			return
		}
	}

	ctx, cancel := context.WithTimeout(r.Context(), timeout+(5*time.Second))
	defer cancel()

	if err := s.taskService.StopTask(ctx, uuID, timeout); err != nil {
		slog.Error("failed to stop task", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}
