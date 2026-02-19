package server

import (
	"net/http"
	"pinn/internal/docker"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

type Server struct {
	router        *chi.Mux
	dockerManager *docker.Manager
}

func New(dockerManager *docker.Manager) *Server {
	s := &Server{
		router:        chi.NewRouter(),
		dockerManager: dockerManager,
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
}
