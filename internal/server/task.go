package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"pinn-connect-service/internal/domain"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// HandleTaskRun godoc
// @Summary      Create and run a new task
// @Description  Accepts multipart/form-data with task metadata and optional input file
// @Tags         tasks
// @Accept       multipart/form-data
// @Produce      json
// @Param        task  formData  string  true  "Task metadata (JSON)"
// @Param        file  formData  file    false "Input file for the task"
// @Success      202  {object}  map[string]string "Task accepted"
// @Failure      400  {string}  string "Invalid request or missing fields"
// @Router       /task/run [post]
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

			if req.CPULimit < 0 || req.CPULimit > s.config.MaxCPUByTask {
				http.Error(w, "invalid cpu limit", http.StatusBadRequest)
				return
			}
			if req.CPULimit == 0 {
				req.CPULimit = s.config.MaxCPUByTask
			}

			if req.MemoryLimit < 0 || req.MemoryLimit > s.config.MaxMemByTask {
				http.Error(w, "invalid memory limit", http.StatusBadRequest)
				return
			}
			if req.MemoryLimit == 0 {
				req.MemoryLimit = s.config.MaxMemByTask
			}

			if req.TimeoutSec < 0 {
				http.Error(w, "invalid timeout", http.StatusBadRequest)
				return
			}
			if req.TimeoutSec == 0 {
				req.TimeoutSec = s.config.DefaultTaskTimeoutSec
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
	task.TimeoutSec = req.TimeoutSec
}

// HandleTaskStatus godoc
// @Summary      Get task status
// @Description  Returns the current status and metadata of a task
// @Tags         tasks
// @Produce      json
// @Param        id   path      string  true  "Task UUID"
// @Success      200  {object}  domain.TaskStatusResponse
// @Failure      404  {string}  string "Task not found"
// @Router       /task/{id}/status [get]
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
		ModelID:   task.ModelID,
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

// HandleTaskStop godoc
// @Summary      Stop a running task
// @Description  Sends a stop signal to a running container task
// @Tags         tasks
// @Param        id       path      string  true  "Task UUID"
// @Param        timeout  query     string  false "Stop timeout (seconds or duration string)"
// @Success      200  {string}  string "Task stopped"
// @Failure      400  {string}  string "Invalid ID or timeout format"
// @Router       /task/{id}/stop [post]
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

// HandleGetAllTasks godoc
// @Summary      Get all tasks
// @Description  Returns a list of all tasks with their statuses and metadata
// @Tags         tasks
// @Produce      json
// @Success      200  {object}  domain.GetAllTasksResponse
// @Failure      500  {string}  string "Internal server error"
// @Router       /task/list [get]
// HandleTaskResult godoc
// @Summary      Get task result download URL
// @Description  Returns a pre-signed URL to download the task result artifact
// @Tags         tasks
// @Produce      json
// @Param        id   path      string  true  "Task UUID"
// @Success      200  {object}  map[string]string "Download URL"
// @Failure      400  {string}  string "Invalid ID"
// @Failure      404  {string}  string "Task not found or result not ready"
// @Router       /task/{id}/result [get]
func (s *Server) HandleTaskResult(w http.ResponseWriter, r *http.Request) {
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

	url, err := s.taskService.GetResultURL(r.Context(), uuID)
	if err != nil {
		slog.Error("getting task result url", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	if url == "" {
		http.Error(w, "result not found or task not completed", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]string{"download_url": url}); err != nil {
		slog.Error("encoding result url response", "error", err)
	}
}

func (s *Server) HandleGetAllTasks(w http.ResponseWriter, r *http.Request) {
	tasks, err := s.taskService.GetAllTasks(r.Context())
	if err != nil {
		slog.Error("getting all tasks from task service", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	resp := domain.GetAllTasksResponse{
		Tasks: make([]domain.TaskStatusResponse, 0, len(tasks)),
	}

	for _, task := range tasks {
		resp.Tasks = append(resp.Tasks, *mapTaskToResp(&task))
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		slog.Error("encoding all tasks", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
}
