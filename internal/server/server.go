package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"pinn-connect-service/internal/config"
	"pinn-connect-service/internal/domain"
	"time"

	_ "pinn-connect-service/docs"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"
	httpSwagger "github.com/swaggo/http-swagger"
)

type TaskService interface {
	SaveInput(taskID uuid.UUID, filename string, r io.Reader) ([]byte, error)
	GetTask(ctx context.Context, id uuid.UUID) (*domain.Task, error)
	GetResultURL(ctx context.Context, id uuid.UUID) (string, error)
	CreateTask(ctx context.Context, task *domain.Task, fileHash []byte) error
	StopTask(ctx context.Context, taskID uuid.UUID, timeout time.Duration) error
}

type ModelService interface {
	GetImageByID(ctx context.Context, id string) (string, error)
	CreateModel(ctx context.Context, modelID string, containerImage string) (*domain.Model, error)
	DeleteModel(ctx context.Context, modelID string) error
	ListModels(ctx context.Context) ([]domain.Model, error)
	UpdateModel(ctx context.Context, modelID string, newContainerImage string) error
	Exists(context.Context, string) (bool, error)
	BuildModel(ctx context.Context, modelID string, archive io.Reader, logWriter io.Writer) error
	RebuildModel(ctx context.Context, modelID string, archive io.Reader, logWriter io.Writer) error
	DeleteImageByModelId(context.Context, string) error
}

type HealthService interface {
	CheckStatus(ctx context.Context) error
}

type Server struct {
	router        *chi.Mux
	taskService   TaskService
	modelService  ModelService
	healthService HealthService
	config        *config.Config
}

func New(taskService TaskService, modelService ModelService, healthService HealthService, config *config.Config) *Server {
	s := &Server{
		router:        chi.NewRouter(),
		taskService:   taskService,
		modelService:  modelService,
		healthService: healthService,
		config:        config,
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

	s.router.Get("/swagger/*", httpSwagger.Handler(
		httpSwagger.URL("http://localhost:8080/swagger/doc.json"), //The url pointing to API definition
	))

	s.router.Get("/health", s.HandleHealth)

	// Protected routes group
	s.router.Group(func(r chi.Router) {
		r.Use(s.AuthMiddleware)

		r.Get("/stats", s.HandleStats)

		r.Route("/task", func(r chi.Router) {
			r.Post("/run", s.HandleTaskRun)
			r.Get("/{id}/status", s.HandleTaskStatus)
			r.Post("/{id}/stop", s.HandleTaskStop)
		})

		r.Route("/model", func(r chi.Router) {
			r.Get("/", s.HandleModelList)
			r.Post("/", s.HandleModelAdd)
			r.Put("/", s.HandleModelUpdate)
			r.Delete("/{id}", s.HandleModelDelete)
			r.Post("/build", s.HandleModelBuild)
			r.Put("/build", s.HandleModelBuildUpdate)
		})
	})
}

func (s *Server) AuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := s.config.Server.APIToken
		if token == "" {
			next.ServeHTTP(w, r)
			return
		}

		if r.Header.Get("X-API-Token") != token {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		next.ServeHTTP(w, r)
	})
}
