package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"pinn/internal/domain"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"
)

type TaskService interface {
	PrepareWorkspace(taskID uuid.UUID) error
	SaveInput(taskID uuid.UUID, filename string, r io.Reader) error
	Create(ctx context.Context, task *domain.Task) error
	Mark(ctx context.Context, task *domain.Task, status domain.TaskStatus) error
	CleanupWorkspace(taskID uuid.UUID) error
	GetTask(ctx context.Context, id uuid.UUID) (*domain.Task, error)
	GetResultURL(ctx context.Context, id uuid.UUID) (string, error)
	FindCachedTask(ctx context.Context, signature string) (string, error)
}

type ModelService interface {
	GetImageByID(ctx context.Context, id string) (string, error)
	CreateModel(ctx context.Context, modelID string, containerImage string) (*domain.Model, error)
	DeleteModel(ctx context.Context, modelID string) error
	ListModels(ctx context.Context) ([]domain.Model, error)
	UpdateModel(ctx context.Context, modelID string, newContainerImage string) error
}

type HealthService interface {
	CheckStatus(ctx context.Context) error
}

type Server struct {
	router        *chi.Mux
	taskService   TaskService
	modelService  ModelService
	healthService HealthService
}

func New(taskService TaskService, modelService ModelService, healthService HealthService) *Server {
	s := &Server{
		router:        chi.NewRouter(),
		taskService:   taskService,
		modelService:  modelService,
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

	errch := make(chan error)

	go func() {
		slog.Info("Starting server", "addr", addr)

		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			wrappedErr := fmt.Errorf("http server failed: %w", err)
			select {
			case errch <- wrappedErr:
			default:
				slog.Error("server failed during/after stopped", "error", err)
			}
		}
	}()

	select {
	case <-ctx.Done():
		slog.Info("Shutting down http server...")

		shutdownCtx, cancel := context.WithTimeout(context.Background(), time.Second*10)
		defer cancel()

		if err := srv.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("shutting down server: %w", err)
		}

		slog.Info("HTTP server stopped")

		return nil
	case err := <-errch:
		return err
	}
}

func (s *Server) setRoutes() {
	s.router.Use(middleware.Logger)
	s.router.Get("/health", s.HandleHealth)
	s.router.Post("/run", s.HandleRun)
	s.router.Get("/status/{id}", s.HandleStatus)

	s.router.Route("/models", func(r chi.Router) {
		r.Get("/", s.HandleModelList)
		r.Post("/", s.HandleModelCreate)
		r.Put("/", s.HandleModelUpdate)
		r.Delete("/{id}", s.HandleModelDelete)
	})
}
