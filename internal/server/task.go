package server

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

func (s *Server) HandleStopTask(w http.ResponseWriter, r *http.Request) {
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
