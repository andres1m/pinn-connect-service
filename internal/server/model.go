package server

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"pinn/internal/domain"

	"github.com/go-chi/chi/v5"
)

func (s *Server) HandleModelCreate(w http.ResponseWriter, r *http.Request) {
	var req domain.CreateModelRequest

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	if req.ContainerImage == "" || req.ID == "" {
		http.Error(w, "both id and container_image are required", http.StatusBadRequest)
		return
	}

	model, err := s.modelService.CreateModel(r.Context(), req.ID, req.ContainerImage)
	if err != nil {
		slog.Error("error creating model", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	if err := json.NewEncoder(w).Encode(model); err != nil {
		slog.Error("error while encoding model", "error", err)
	}
}

func (s *Server) HandleModelDelete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		http.Error(w, "missing id", http.StatusBadRequest)
		return
	}

	if err := s.modelService.DeleteModel(r.Context(), id); err != nil {
		slog.Error("error deleting model", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
}

func (s *Server) HandleModelUpdate(w http.ResponseWriter, r *http.Request) {
	var req domain.UpdateModelRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	if err := s.modelService.UpdateModel(r.Context(), req.ID, req.NewContainerImage); err != nil {
		slog.Error("error updating model", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
}

func (s *Server) HandleModelList(w http.ResponseWriter, r *http.Request) {
	models, err := s.modelService.ListModels(r.Context())
	if err != nil {
		slog.Error("error getting models list", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	if models == nil {
		models = []domain.Model{}
	}

	w.Header().Set("Content-Type", "application/json")

	if err := json.NewEncoder(w).Encode(models); err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

}
