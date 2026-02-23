package server

import (
	"context"
	"net/http"

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

func (s *Server) Run(addr string) error {
	return http.ListenAndServe(addr, s.router)
}

func (s *Server) setRoutes() {
	s.router.Use(middleware.Logger)
	s.router.Get("/health", s.HandleHealth)
	s.router.Post("/run", s.HandleRun)
	s.router.Get("/status", s.HandleStatus)
	s.router.Post("/runmock", s.HandleRunMock)
}
