package server

import (
	"net/http"
	"pinn/internal/config"
	"pinn/internal/docker"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

type Server struct {
	router        *chi.Mux
	dockerManager *docker.Manager
	config        *config.Config
}

func New(dockerManager *docker.Manager, config *config.Config) *Server {
	s := &Server{
		router:        chi.NewRouter(),
		dockerManager: dockerManager,
		config:        config,
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
