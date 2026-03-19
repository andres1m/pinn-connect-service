package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"pinn/internal/domain"

	"github.com/go-chi/chi/v5"
)

// HandleModelAdd godoc
// @Summary      Register a new model
// @Description  Creates a new model entry with an existing container image
// @Tags         models
// @Accept       json
// @Produce      json
// @Param        request body domain.CreateModelRequest true "Create Model Request"
// @Success      201  {object}  domain.Model
// @Failure      400  {string}  string "Invalid JSON or missing fields"
// @Router       /model [post]
func (s *Server) HandleModelAdd(w http.ResponseWriter, r *http.Request) {
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

// HandleModelDelete godoc
// @Summary      Delete a model
// @Description  Removes a model from the system and its associated container image
// @Tags         models
// @Param        id   path      string  true  "Model ID"
// @Success      200  {string}  string "Model deleted"
// @Failure      400  {string}  string "Missing or invalid ID"
// @Router       /model/{id} [delete]
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

	if err := s.modelService.DeleteModel(r.Context(), id); err != nil {
		slog.Error("deleting model", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
}

// HandleModelUpdate godoc
// @Summary      Update model image
// @Description  Updates the container image for an existing model
// @Tags         models
// @Accept       json
// @Produce      json
// @Param        request body domain.UpdateModelRequest true "Update Model Request"
// @Success      200  {string}  string "Model updated"
// @Failure      400  {string}  string "Invalid JSON"
// @Router       /model [put]
func (s *Server) HandleModelUpdate(w http.ResponseWriter, r *http.Request) {
	var req domain.UpdateModelRequest

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	if err := s.modelService.DeleteImageByModelId(r.Context(), req.ID); err != nil {
		slog.Error("deleting old image", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	if err := s.modelService.UpdateModel(r.Context(), req.ID, req.ContainerImage); err != nil {
		slog.Error("error updating model", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
}

// HandleModelList godoc
// @Summary      List all models
// @Description  Returns a list of all registered models
// @Tags         models
// @Produce      json
// @Success      200  {array}   domain.Model
// @Router       /model [get]
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

// HandleModelBuildUpdate godoc
// @Summary      Update and rebuild model from archive
// @Description  Accepts multipart/form-data to rebuild an existing model image
// @Tags         models
// @Accept       multipart/form-data
// @Produce      text/plain
// @Param        model      formData  string  true  "Model metadata (JSON)"
// @Param        artifacts  formData  file    true  "Tar.gz archive with artifacts"
// @Success      200  {string}  string  "BUILD SUCCESSFUL"
// @Router       /model/build [put]
func (s *Server) HandleModelBuildUpdate(w http.ResponseWriter, r *http.Request) {
	s.processModelBuild(w, r, true)
}

// HandleModelBuild godoc
// @Summary      Build new model from archive
// @Description  Accepts multipart/form-data to build a new model image
// @Tags         models
// @Accept       multipart/form-data
// @Produce      text/plain
// @Param        model      formData  string  true  "Model metadata (JSON)"
// @Param        artifacts  formData  file    true  "Tar.gz archive with artifacts"
// @Success      200  {string}  string  "BUILD SUCCESSFUL"
// @Router       /model/build [post]
func (s *Server) HandleModelBuild(w http.ResponseWriter, r *http.Request) {
	s.processModelBuild(w, r, false)
}

func (s *Server) processModelBuild(w http.ResponseWriter, r *http.Request, isUpdate bool) {
	mr, err := r.MultipartReader()
	if err != nil {
		http.Error(w, "invalid multipart request", http.StatusBadRequest)
		return
	}

	var modelID string

	for {
		part, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			slog.Error("error reading multipart", "error", err)
			http.Error(w, "error reading multipart", http.StatusBadRequest)
			return
		}

		switch part.FormName() {
		case "model":
			var req domain.CreateModelRequest
			if err := json.NewDecoder(part).Decode(&req); err != nil {
				http.Error(w, "invalid json metadata", http.StatusBadRequest)
				return
			}

			if req.ID == "" {
				http.Error(w, "model id is required", http.StatusBadRequest)
				return
			}

			exists, err := s.modelService.Exists(r.Context(), req.ID)
			if err != nil {
				slog.Error("checking model existence", "error", err)
				http.Error(w, "internal server error", http.StatusInternalServerError)
				return
			}

			if isUpdate && !exists {
				http.Error(w, "model does not exist", http.StatusNotFound)
				return
			}
			if !isUpdate && exists {
				http.Error(w, "model already exists", http.StatusConflict)
				return
			}

			modelID = req.ID

		case "artifacts":
			if modelID == "" {
				http.Error(w, "'model' metadata part must precede 'artifacts' part", http.StatusBadRequest)
				return
			}

			buff := make([]byte, 512)
			n, err := part.Read(buff)
			if err != nil && err != io.EOF {
				slog.Error("failed to read artifact header", "error", err)
				http.Error(w, "failed to read artifacts", http.StatusInternalServerError)
				return
			}

			contentType := http.DetectContentType(buff[:n])
			if contentType != "application/x-gzip" && contentType != "application/gzip" {
				http.Error(w, "invalid file type: expected tar.gz", http.StatusBadRequest)
				return
			}

			archiveReader := io.MultiReader(bytes.NewReader(buff[:n]), part)

			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.WriteHeader(http.StatusOK)

			if isUpdate {
				err = s.modelService.RebuildModel(r.Context(), modelID, archiveReader, w)
			} else {
				err = s.modelService.BuildModel(r.Context(), modelID, archiveReader, w)
			}

			if err != nil {
				fmt.Fprintf(w, "\nBUILD FAILED: %v\n", err)
				return
			}

			fmt.Fprintln(w, "\nBUILD SUCCESSFUL")
			return

		default:
			continue
		}
	}

	if modelID == "" {
		http.Error(w, "missing model metadata", http.StatusBadRequest)
	} else {
		http.Error(w, "missing artifacts", http.StatusBadRequest)
	}
}
