package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"pinn-connect-service/internal/domain"
	"strings"
	"testing"
)

// ─────────────────────────────────────────────
// HandleModelAdd
// ─────────────────────────────────────────────

func TestHandleModelAdd_Success(t *testing.T) {
	srv := testServer(nil, nil, nil)

	body, _ := json.Marshal(domain.CreateModelRequest{ID: "m1", ContainerImage: "img:v1"})
	req := httptest.NewRequest(http.MethodPost, "/model/", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	srv.HandleModelAdd(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var model domain.Model
	if err := json.NewDecoder(rec.Body).Decode(&model); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if model.ID != "m1" {
		t.Errorf("expected model ID 'm1', got %q", model.ID)
	}
}

func TestHandleModelAdd_InvalidJSON(t *testing.T) {
	srv := testServer(nil, nil, nil)

	req := httptest.NewRequest(http.MethodPost, "/model/", strings.NewReader("{bad json"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	srv.HandleModelAdd(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

func TestHandleModelAdd_MissingID(t *testing.T) {
	srv := testServer(nil, nil, nil)

	body, _ := json.Marshal(domain.CreateModelRequest{ContainerImage: "img:v1"})
	req := httptest.NewRequest(http.MethodPost, "/model/", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	srv.HandleModelAdd(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for missing ID, got %d", rec.Code)
	}
}

func TestHandleModelAdd_MissingImage(t *testing.T) {
	srv := testServer(nil, nil, nil)

	body, _ := json.Marshal(domain.CreateModelRequest{ID: "m1"})
	req := httptest.NewRequest(http.MethodPost, "/model/", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	srv.HandleModelAdd(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for missing ContainerImage, got %d", rec.Code)
	}
}

func TestHandleModelAdd_ServiceError(t *testing.T) {
	ms := &mockModelSvc{
		createModelFunc: func(_ context.Context, _, _ string) (*domain.Model, error) {
			return nil, errors.New("constraint violation")
		},
	}
	srv := testServer(nil, ms, nil)

	body, _ := json.Marshal(domain.CreateModelRequest{ID: "m1", ContainerImage: "img:v1"})
	req := httptest.NewRequest(http.MethodPost, "/model/", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	srv.HandleModelAdd(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", rec.Code)
	}
}

// ─────────────────────────────────────────────
// HandleModelDelete
// ─────────────────────────────────────────────

func TestHandleModelDelete_Success(t *testing.T) {
	srv := testServer(nil, nil, nil)

	req := httptest.NewRequest(http.MethodDelete, "/model/m1", nil)
	req = withChiParam(req, "id", "m1")
	rec := httptest.NewRecorder()

	srv.HandleModelDelete(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}

func TestHandleModelDelete_MissingID(t *testing.T) {
	srv := testServer(nil, nil, nil)

	req := httptest.NewRequest(http.MethodDelete, "/model/", nil)
	req = withChiParam(req, "id", "")
	rec := httptest.NewRecorder()

	srv.HandleModelDelete(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

func TestHandleModelDelete_ServiceError(t *testing.T) {
	ms := &mockModelSvc{
		deleteModelFunc: func(_ context.Context, _ string) error {
			return errors.New("delete error")
		},
	}
	srv := testServer(nil, ms, nil)

	req := httptest.NewRequest(http.MethodDelete, "/model/m1", nil)
	req = withChiParam(req, "id", "m1")
	rec := httptest.NewRecorder()

	srv.HandleModelDelete(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", rec.Code)
	}
}

// ─────────────────────────────────────────────
// HandleModelUpdate
// ─────────────────────────────────────────────

func TestHandleModelUpdate_Success(t *testing.T) {
	srv := testServer(nil, nil, nil)

	body, _ := json.Marshal(domain.UpdateModelRequest{ID: "m1", ContainerImage: "new-img:v2"})
	req := httptest.NewRequest(http.MethodPut, "/model/", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	srv.HandleModelUpdate(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}

func TestHandleModelUpdate_InvalidJSON(t *testing.T) {
	srv := testServer(nil, nil, nil)

	req := httptest.NewRequest(http.MethodPut, "/model/", strings.NewReader("{invalid"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	srv.HandleModelUpdate(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

func TestHandleModelUpdate_DeleteImageError(t *testing.T) {
	ms := &mockModelSvc{
		deleteImageByModelFunc: func(_ context.Context, _ string) error {
			return errors.New("image not found")
		},
	}
	srv := testServer(nil, ms, nil)

	body, _ := json.Marshal(domain.UpdateModelRequest{ID: "m1", ContainerImage: "new:v2"})
	req := httptest.NewRequest(http.MethodPut, "/model/", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	srv.HandleModelUpdate(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", rec.Code)
	}
}

func TestHandleModelUpdate_UpdateModelError(t *testing.T) {
	ms := &mockModelSvc{
		updateModelFunc: func(_ context.Context, _, _ string) error {
			return errors.New("update error")
		},
	}
	srv := testServer(nil, ms, nil)

	body, _ := json.Marshal(domain.UpdateModelRequest{ID: "m1", ContainerImage: "new:v2"})
	req := httptest.NewRequest(http.MethodPut, "/model/", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	srv.HandleModelUpdate(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", rec.Code)
	}
}

// ─────────────────────────────────────────────
// HandleModelList
// ─────────────────────────────────────────────

func TestHandleModelList_Success(t *testing.T) {
	srv := testServer(nil, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/model/", nil)
	rec := httptest.NewRecorder()

	srv.HandleModelList(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var models []domain.Model
	if err := json.NewDecoder(rec.Body).Decode(&models); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if len(models) == 0 {
		t.Error("expected non-empty model list")
	}
}

func TestHandleModelList_NilResultConvertsToEmptyArray(t *testing.T) {
	ms := &mockModelSvc{
		listModelsFunc: func(_ context.Context) ([]domain.Model, error) { return nil, nil },
	}
	srv := testServer(nil, ms, nil)

	req := httptest.NewRequest(http.MethodGet, "/model/", nil)
	rec := httptest.NewRecorder()
	srv.HandleModelList(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}

	var models []domain.Model
	if err := json.NewDecoder(rec.Body).Decode(&models); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if models == nil {
		t.Error("nil models should be serialized as []")
	}
}

func TestHandleModelList_ServiceError(t *testing.T) {
	ms := &mockModelSvc{
		listModelsFunc: func(_ context.Context) ([]domain.Model, error) {
			return nil, errors.New("db error")
		},
	}
	srv := testServer(nil, ms, nil)

	req := httptest.NewRequest(http.MethodGet, "/model/", nil)
	rec := httptest.NewRecorder()
	srv.HandleModelList(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", rec.Code)
	}
}

// ─────────────────────────────────────────────
// HandleModelBuild (POST) — processModelBuild(isUpdate=false)
// ─────────────────────────────────────────────

func TestHandleModelBuild_Success(t *testing.T) {
	srv := testServer(nil, nil, nil)

	body, ct := buildMultipartModel(`{"id":"new-model"}`)
	req := httptest.NewRequest(http.MethodPost, "/model/build", body)
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()

	srv.HandleModelBuild(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "BUILD SUCCESSFUL") {
		t.Errorf("expected BUILD SUCCESSFUL, got: %s", rec.Body.String())
	}
}

func TestHandleModelBuild_InvalidMultipart(t *testing.T) {
	srv := testServer(nil, nil, nil)

	req := httptest.NewRequest(http.MethodPost, "/model/build", strings.NewReader("not multipart"))
	req.Header.Set("Content-Type", "multipart/form-data; boundary=MISSING")
	rec := httptest.NewRecorder()

	srv.HandleModelBuild(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

func TestHandleModelBuild_InvalidModelJSON(t *testing.T) {
	srv := testServer(nil, nil, nil)

	body := &bytes.Buffer{}
	w := multipart.NewWriter(body)
	mw, _ := w.CreateFormField("model")
	mw.Write([]byte("{invalid json"))
	w.Close()

	req := httptest.NewRequest(http.MethodPost, "/model/build", body)
	req.Header.Set("Content-Type", w.FormDataContentType())
	rec := httptest.NewRecorder()

	srv.HandleModelBuild(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

func TestHandleModelBuild_MissingModelID(t *testing.T) {
	srv := testServer(nil, nil, nil)

	body := &bytes.Buffer{}
	w := multipart.NewWriter(body)
	mw, _ := w.CreateFormField("model")
	mw.Write([]byte(`{"id":""}`))
	w.Close()

	req := httptest.NewRequest(http.MethodPost, "/model/build", body)
	req.Header.Set("Content-Type", w.FormDataContentType())
	rec := httptest.NewRecorder()

	srv.HandleModelBuild(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

func TestHandleModelBuild_ModelAlreadyExists(t *testing.T) {
	ms := &mockModelSvc{
		existsFunc: func(_ context.Context, _ string) (bool, error) { return true, nil },
	}
	srv := testServer(nil, ms, nil)

	body := &bytes.Buffer{}
	w := multipart.NewWriter(body)
	mw, _ := w.CreateFormField("model")
	mw.Write([]byte(`{"id":"existing"}`))
	w.Close()

	req := httptest.NewRequest(http.MethodPost, "/model/build", body)
	req.Header.Set("Content-Type", w.FormDataContentType())
	rec := httptest.NewRecorder()

	srv.HandleModelBuild(rec, req)
	if rec.Code != http.StatusConflict {
		t.Errorf("expected 409, got %d", rec.Code)
	}
}

func TestHandleModelBuild_ExistsCheckError(t *testing.T) {
	ms := &mockModelSvc{
		existsFunc: func(_ context.Context, _ string) (bool, error) {
			return false, errors.New("db error")
		},
	}
	srv := testServer(nil, ms, nil)

	body := &bytes.Buffer{}
	w := multipart.NewWriter(body)
	mw, _ := w.CreateFormField("model")
	mw.Write([]byte(`{"id":"m1"}`))
	w.Close()

	req := httptest.NewRequest(http.MethodPost, "/model/build", body)
	req.Header.Set("Content-Type", w.FormDataContentType())
	rec := httptest.NewRecorder()

	srv.HandleModelBuild(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", rec.Code)
	}
}

func TestHandleModelBuild_ArtifactsBeforeModelPart(t *testing.T) {
	// Sending artifacts before the model metadata part must fail with 400.
	srv := testServer(nil, nil, nil)

	body := &bytes.Buffer{}
	w := multipart.NewWriter(body)
	fw, _ := w.CreateFormFile("artifacts", "archive.tar.gz")
	fw.Write(makeGzipBytes())
	w.Close()

	req := httptest.NewRequest(http.MethodPost, "/model/build", body)
	req.Header.Set("Content-Type", w.FormDataContentType())
	rec := httptest.NewRecorder()

	srv.HandleModelBuild(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

func TestHandleModelBuild_InvalidArchiveContentType(t *testing.T) {
	srv := testServer(nil, nil, nil)

	body := &bytes.Buffer{}
	w := multipart.NewWriter(body)
	mw, _ := w.CreateFormField("model")
	mw.Write([]byte(`{"id":"m1"}`))
	fw, _ := w.CreateFormFile("artifacts", "archive.txt")
	fw.Write([]byte("this is plain text, definitely not gzip"))
	w.Close()

	req := httptest.NewRequest(http.MethodPost, "/model/build", body)
	req.Header.Set("Content-Type", w.FormDataContentType())
	rec := httptest.NewRecorder()

	srv.HandleModelBuild(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for non-gzip archive, got %d", rec.Code)
	}
}

func TestHandleModelBuild_BuildServiceError_BodyContainsBuildFailed(t *testing.T) {
	ms := &mockModelSvc{
		buildModelFunc: func(_ context.Context, _ string, _ io.Reader, _ io.Writer) error {
			return errors.New("docker daemon unreachable")
		},
	}
	srv := testServer(nil, ms, nil)

	body, ct := buildMultipartModel(`{"id":"m1"}`)
	req := httptest.NewRequest(http.MethodPost, "/model/build", body)
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()

	srv.HandleModelBuild(rec, req)

	// Headers already sent (200 streaming), error goes into body.
	if !strings.Contains(rec.Body.String(), "BUILD FAILED") {
		t.Errorf("expected BUILD FAILED in body, got: %s", rec.Body.String())
	}
}

func TestHandleModelBuild_MissingArtifacts(t *testing.T) {
	srv := testServer(nil, nil, nil)

	body := &bytes.Buffer{}
	w := multipart.NewWriter(body)
	mw, _ := w.CreateFormField("model")
	mw.Write([]byte(`{"id":"m1"}`))
	w.Close()

	req := httptest.NewRequest(http.MethodPost, "/model/build", body)
	req.Header.Set("Content-Type", w.FormDataContentType())
	rec := httptest.NewRecorder()

	srv.HandleModelBuild(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for missing artifacts, got %d", rec.Code)
	}
}

func TestHandleModelBuild_MissingAllParts(t *testing.T) {
	srv := testServer(nil, nil, nil)

	body := &bytes.Buffer{}
	w := multipart.NewWriter(body)
	w.Close()

	req := httptest.NewRequest(http.MethodPost, "/model/build", body)
	req.Header.Set("Content-Type", w.FormDataContentType())
	rec := httptest.NewRecorder()

	srv.HandleModelBuild(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for completely empty body, got %d", rec.Code)
	}
}

// ─────────────────────────────────────────────
// HandleModelBuildUpdate (PUT) — processModelBuild(isUpdate=true)
// ─────────────────────────────────────────────

func TestHandleModelBuildUpdate_Success(t *testing.T) {
	ms := &mockModelSvc{
		existsFunc: func(_ context.Context, _ string) (bool, error) { return true, nil },
	}
	srv := testServer(nil, ms, nil)

	body, ct := buildMultipartModel(`{"id":"existing-model"}`)
	req := httptest.NewRequest(http.MethodPut, "/model/build", body)
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()

	srv.HandleModelBuildUpdate(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "BUILD SUCCESSFUL") {
		t.Errorf("expected BUILD SUCCESSFUL, got: %s", rec.Body.String())
	}
}

func TestHandleModelBuildUpdate_ModelDoesNotExist(t *testing.T) {
	ms := &mockModelSvc{
		existsFunc: func(_ context.Context, _ string) (bool, error) { return false, nil },
	}
	srv := testServer(nil, ms, nil)

	body := &bytes.Buffer{}
	w := multipart.NewWriter(body)
	mw, _ := w.CreateFormField("model")
	mw.Write([]byte(`{"id":"ghost"}`))
	w.Close()

	req := httptest.NewRequest(http.MethodPut, "/model/build", body)
	req.Header.Set("Content-Type", w.FormDataContentType())
	rec := httptest.NewRecorder()

	srv.HandleModelBuildUpdate(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rec.Code)
	}
}

func TestHandleModelBuildUpdate_RebuildServiceError(t *testing.T) {
	ms := &mockModelSvc{
		existsFunc: func(_ context.Context, _ string) (bool, error) { return true, nil },
		rebuildModelFunc: func(_ context.Context, _ string, _ io.Reader, _ io.Writer) error {
			return errors.New("rebuild failed")
		},
	}
	srv := testServer(nil, ms, nil)

	body, ct := buildMultipartModel(`{"id":"m1"}`)
	req := httptest.NewRequest(http.MethodPut, "/model/build", body)
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()

	srv.HandleModelBuildUpdate(rec, req)

	if !strings.Contains(rec.Body.String(), "BUILD FAILED") {
		t.Errorf("expected BUILD FAILED, got: %s", rec.Body.String())
	}
}

func TestHandleModelBuildUpdate_InvalidArchiveContentType(t *testing.T) {
	ms := &mockModelSvc{
		existsFunc: func(_ context.Context, _ string) (bool, error) { return true, nil },
	}
	srv := testServer(nil, ms, nil)

	body := &bytes.Buffer{}
	w := multipart.NewWriter(body)
	mw, _ := w.CreateFormField("model")
	mw.Write([]byte(`{"id":"m1"}`))
	fw, _ := w.CreateFormFile("artifacts", "archive.txt")
	fw.Write([]byte("not a gzip file"))
	w.Close()

	req := httptest.NewRequest(http.MethodPut, "/model/build", body)
	req.Header.Set("Content-Type", w.FormDataContentType())
	rec := httptest.NewRecorder()

	srv.HandleModelBuildUpdate(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for non-gzip archive, got %d", rec.Code)
	}
}
