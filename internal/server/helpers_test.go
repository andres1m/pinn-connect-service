package server

import (
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"mime/multipart"
	"net/http"
	"pinn-connect-service/internal/config"
	"pinn-connect-service/internal/domain"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// ─────────────────────────────────────────────
// MOCK: TaskService
// ─────────────────────────────────────────────

type mockTaskSvc struct {
	saveInputFunc    func(uuid.UUID, string, io.Reader) ([]byte, error)
	getTaskFunc      func(context.Context, uuid.UUID) (*domain.Task, error)
	getResultURLFunc func(context.Context, uuid.UUID) (string, error)
	createTaskFunc   func(context.Context, *domain.Task, []byte) error
	stopTaskFunc     func(context.Context, uuid.UUID, time.Duration) error
	listTasksFunc    func(context.Context, int, int) ([]domain.Task, int64, error)
	deleteTaskFunc   func(context.Context, uuid.UUID) error
}

func (m *mockTaskSvc) SaveInput(id uuid.UUID, filename string, r io.Reader) ([]byte, error) {
	if m.saveInputFunc != nil {
		return m.saveInputFunc(id, filename, r)
	}
	io.Copy(io.Discard, r)
	return []byte("hash"), nil
}
func (m *mockTaskSvc) GetTask(ctx context.Context, id uuid.UUID) (*domain.Task, error) {
	if m.getTaskFunc != nil {
		return m.getTaskFunc(ctx, id)
	}
	return &domain.Task{ID: id, Status: domain.TaskQueued}, nil
}
func (m *mockTaskSvc) GetResultURL(ctx context.Context, id uuid.UUID) (string, error) {
	if m.getResultURLFunc != nil {
		return m.getResultURLFunc(ctx, id)
	}
	return "https://example.com/result", nil
}
func (m *mockTaskSvc) CreateTask(ctx context.Context, task *domain.Task, hash []byte) error {
	if m.createTaskFunc != nil {
		return m.createTaskFunc(ctx, task, hash)
	}
	return nil
}
func (m *mockTaskSvc) StopTask(ctx context.Context, id uuid.UUID, timeout time.Duration) error {
	if m.stopTaskFunc != nil {
		return m.stopTaskFunc(ctx, id, timeout)
	}
	return nil
}
func (m *mockTaskSvc) ListTasks(ctx context.Context, page, pageSize int) ([]domain.Task, int64, error) {
	if m.listTasksFunc != nil {
		return m.listTasksFunc(ctx, page, pageSize)
	}
	return []domain.Task{{ID: uuid.New(), Status: domain.TaskQueued}}, 1, nil
}
func (m *mockTaskSvc) DeleteTask(ctx context.Context, id uuid.UUID) error {
	if m.deleteTaskFunc != nil {
		return m.deleteTaskFunc(ctx, id)
	}
	return nil
}

// ─────────────────────────────────────────────
// MOCK: ModelService
// ─────────────────────────────────────────────

type mockModelSvc struct {
	getImageByIDFunc       func(context.Context, string) (string, error)
	createModelFunc        func(context.Context, string, string) (*domain.Model, error)
	deleteModelFunc        func(context.Context, string) error
	listModelsFunc         func(context.Context) ([]domain.Model, error)
	updateModelFunc        func(context.Context, string, string) error
	existsFunc             func(context.Context, string) (bool, error)
	buildModelFunc         func(context.Context, string, io.Reader, io.Writer) error
	rebuildModelFunc       func(context.Context, string, io.Reader, io.Writer) error
	deleteImageByModelFunc func(context.Context, string) error
}

func (m *mockModelSvc) GetImageByID(ctx context.Context, id string) (string, error) {
	if m.getImageByIDFunc != nil {
		return m.getImageByIDFunc(ctx, id)
	}
	return "img:latest", nil
}
func (m *mockModelSvc) CreateModel(ctx context.Context, id, img string) (*domain.Model, error) {
	if m.createModelFunc != nil {
		return m.createModelFunc(ctx, id, img)
	}
	return &domain.Model{ID: id, ContainerImage: img}, nil
}
func (m *mockModelSvc) DeleteModel(ctx context.Context, id string) error {
	if m.deleteModelFunc != nil {
		return m.deleteModelFunc(ctx, id)
	}
	return nil
}
func (m *mockModelSvc) ListModels(ctx context.Context) ([]domain.Model, error) {
	if m.listModelsFunc != nil {
		return m.listModelsFunc(ctx)
	}
	return []domain.Model{{ID: "m1"}, {ID: "m2"}}, nil
}
func (m *mockModelSvc) UpdateModel(ctx context.Context, id, img string) error {
	if m.updateModelFunc != nil {
		return m.updateModelFunc(ctx, id, img)
	}
	return nil
}
func (m *mockModelSvc) Exists(ctx context.Context, id string) (bool, error) {
	if m.existsFunc != nil {
		return m.existsFunc(ctx, id)
	}
	return false, nil
}
func (m *mockModelSvc) BuildModel(ctx context.Context, id string, archive io.Reader, lw io.Writer) error {
	if m.buildModelFunc != nil {
		return m.buildModelFunc(ctx, id, archive, lw)
	}
	return nil
}
func (m *mockModelSvc) RebuildModel(ctx context.Context, id string, archive io.Reader, lw io.Writer) error {
	if m.rebuildModelFunc != nil {
		return m.rebuildModelFunc(ctx, id, archive, lw)
	}
	return nil
}
func (m *mockModelSvc) DeleteImageByModelId(ctx context.Context, id string) error {
	if m.deleteImageByModelFunc != nil {
		return m.deleteImageByModelFunc(ctx, id)
	}
	return nil
}

// ─────────────────────────────────────────────
// MOCK: HealthService
// ─────────────────────────────────────────────

type mockHealthSvc struct {
	checkFunc func(context.Context) error
}

func (m *mockHealthSvc) CheckStatus(ctx context.Context) error {
	if m.checkFunc != nil {
		return m.checkFunc(ctx)
	}
	return nil
}

// ─────────────────────────────────────────────
// SERVER FACTORY
// ─────────────────────────────────────────────

func testServer(ts TaskService, ms ModelService, hs HealthService) *Server {
	cfg := &config.Config{
		MaxCPUByTask:          4,
		MaxMemByTask:          2048,
		DefaultTaskTimeoutSec: 60,
		Server: config.ServerConfig{
			DefaultTaskStopTimeout: 30 * time.Second,
			APIToken:               "",
		},
	}
	if ts == nil {
		ts = &mockTaskSvc{}
	}
	if ms == nil {
		ms = &mockModelSvc{}
	}
	if hs == nil {
		hs = &mockHealthSvc{}
	}
	return &Server{
		router:        chi.NewRouter(),
		taskService:   ts,
		modelService:  ms,
		healthService: hs,
		config:        cfg,
	}
}

// ─────────────────────────────────────────────
// REQUEST HELPERS
// ─────────────────────────────────────────────

// withChiParam injects a chi URL param into the request context.
func withChiParam(r *http.Request, key, value string) *http.Request {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add(key, value)
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
}

// buildMultipartTask creates a multipart body with a "task" JSON part and a "file" part.
func buildMultipartTask(taskJSON, fileContent string) (body *bytes.Buffer, contentType string) {
	body = &bytes.Buffer{}
	w := multipart.NewWriter(body)
	tw, _ := w.CreateFormField("task")
	tw.Write([]byte(taskJSON))
	fw, _ := w.CreateFormFile("file", "input.txt")
	fw.Write([]byte(fileContent))
	w.Close()
	return body, w.FormDataContentType()
}

// buildMultipartModel creates a multipart body with a "model" JSON part
// and a valid gzip "artifacts" part.
func buildMultipartModel(modelJSON string) (body *bytes.Buffer, contentType string) {
	body = &bytes.Buffer{}
	w := multipart.NewWriter(body)
	mw, _ := w.CreateFormField("model")
	mw.Write([]byte(modelJSON))
	fw, _ := w.CreateFormFile("artifacts", "model.tar.gz")
	gz := gzip.NewWriter(fw)
	gz.Write([]byte("fake archive"))
	gz.Close()
	w.Close()
	return body, w.FormDataContentType()
}

// makeGzipBytes returns a minimal valid gzip payload.
func makeGzipBytes() []byte {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	gz.Write([]byte("archive"))
	gz.Close()
	return buf.Bytes()
}

// validTaskJSON returns a minimal valid CreateTaskRequest JSON.
func validTaskJSON() string {
	return `{"model_id":"m1","timeout_sec":10}`
}
