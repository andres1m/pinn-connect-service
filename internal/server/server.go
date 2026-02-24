package server

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

type TaskService interface {
	RunMock(ctx context.Context, taskID string) (string, error)
}

type HealthService interface {
	CheckStatus(ctx context.Context) error
}

type Server struct {
	router        *chi.Mux
	taskService   TaskService
	healthService HealthService
}

func New(taskService TaskService, healthService HealthService) *Server {
	s := &Server{
		router:        chi.NewRouter(),
		taskService:   taskService,
		healthService: healthService,
	}

	s.setRoutes()

	return s
}

func (s *Server) Run(ctx context.Context, addr string) error {
	srv := &http.Server{
		Addr:    addr,
		Handler: s.router,
	}

	errch := make(chan error, 1)
	go func() {
		<-ctx.Done()

		slog.Info("Shutting down http server...")

		shutdownCtx, cancel := context.WithTimeout(context.Background(), time.Second*10)
		defer cancel()

		if err := srv.Shutdown(shutdownCtx); err != nil {
			errch <- err
		} else {
			errch <- nil
		}

	}()

	err := srv.ListenAndServe()
	if err != nil {
		return fmt.Errorf("starting server: %w", err)
	}

	if err := <-errch; err != nil {
		return fmt.Errorf("shutting down server: %w", err)
	}

	slog.Info("HTTP server stopped")

	return nil
}

func (s *Server) setRoutes() {
	s.router.Use(middleware.Logger)
	s.router.Get("/health", s.HandleHealth)
	s.router.Post("/run", s.HandleRun)
	s.router.Get("/status", s.HandleStatus)
	s.router.Post("/runmock", s.HandleRunMock)
}
