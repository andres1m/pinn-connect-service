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
	"testing"
	"time"

	"github.com/google/uuid"
)

// ─────────────────────────────────────────────
// HandleTaskRun
// ─────────────────────────────────────────────

func TestHandleTaskRun_Success(t *testing.T) {
	srv := testServer(nil, nil, nil)

	body, ct := buildMultipartTask(validTaskJSON(), "payload")
	req := httptest.NewRequest(http.MethodPost, "/task/run", body)
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()

	srv.HandleTaskRun(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if resp["task_id"] == "" {
		t.Error("expected task_id in response")
	}
}

func TestHandleTaskRun_InvalidMultipart(t *testing.T) {
	srv := testServer(nil, nil, nil)

	req := httptest.NewRequest(http.MethodPost, "/task/run", bytes.NewBufferString("not multipart"))
	req.Header.Set("Content-Type", "multipart/form-data; boundary=MISSING")
	rec := httptest.NewRecorder()

	srv.HandleTaskRun(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

func TestHandleTaskRun_InvalidTaskJSON(t *testing.T) {
	srv := testServer(nil, nil, nil)

	body := &bytes.Buffer{}
	w := multipart.NewWriter(body)
	tw, _ := w.CreateFormField("task")
	tw.Write([]byte("{invalid json"))
	w.Close()

	req := httptest.NewRequest(http.MethodPost, "/task/run", body)
	req.Header.Set("Content-Type", w.FormDataContentType())
	rec := httptest.NewRecorder()

	srv.HandleTaskRun(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

func TestHandleTaskRun_MissingFilePart(t *testing.T) {
	srv := testServer(nil, nil, nil)

	// Only "task" part, no "file"
	body := &bytes.Buffer{}
	w := multipart.NewWriter(body)
	tw, _ := w.CreateFormField("task")
	tw.Write([]byte(validTaskJSON()))
	w.Close()

	req := httptest.NewRequest(http.MethodPost, "/task/run", body)
	req.Header.Set("Content-Type", w.FormDataContentType())
	rec := httptest.NewRecorder()

	srv.HandleTaskRun(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

func TestHandleTaskRun_MissingTaskPart(t *testing.T) {
	srv := testServer(nil, nil, nil)

	// Only "file" part, no "task"
	body := &bytes.Buffer{}
	w := multipart.NewWriter(body)
	fw, _ := w.CreateFormFile("file", "input.txt")
	fw.Write([]byte("data"))
	w.Close()

	req := httptest.NewRequest(http.MethodPost, "/task/run", body)
	req.Header.Set("Content-Type", w.FormDataContentType())
	rec := httptest.NewRecorder()

	srv.HandleTaskRun(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

func TestHandleTaskRun_CPULimit_Negative(t *testing.T) {
	srv := testServer(nil, nil, nil)

	body, ct := buildMultipartTask(`{"model_id":"m1","cpu_limit":-1}`, "data")
	req := httptest.NewRequest(http.MethodPost, "/task/run", body)
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()

	srv.HandleTaskRun(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for negative cpu_limit, got %d", rec.Code)
	}
}

func TestHandleTaskRun_CPULimit_ExceedsMax(t *testing.T) {
	srv := testServer(nil, nil, nil)

	body, ct := buildMultipartTask(`{"model_id":"m1","cpu_limit":9999}`, "data")
	req := httptest.NewRequest(http.MethodPost, "/task/run", body)
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()

	srv.HandleTaskRun(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for cpu_limit > max, got %d", rec.Code)
	}
}

func TestHandleTaskRun_MemoryLimit_Negative(t *testing.T) {
	srv := testServer(nil, nil, nil)

	body, ct := buildMultipartTask(`{"model_id":"m1","memory_limit":-1}`, "data")
	req := httptest.NewRequest(http.MethodPost, "/task/run", body)
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()

	srv.HandleTaskRun(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for negative memory_limit, got %d", rec.Code)
	}
}

func TestHandleTaskRun_MemoryLimit_ExceedsMax(t *testing.T) {
	srv := testServer(nil, nil, nil)

	body, ct := buildMultipartTask(`{"model_id":"m1","memory_limit":999999}`, "data")
	req := httptest.NewRequest(http.MethodPost, "/task/run", body)
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()

	srv.HandleTaskRun(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for memory_limit > max, got %d", rec.Code)
	}
}

func TestHandleTaskRun_TimeoutSec_Negative(t *testing.T) {
	srv := testServer(nil, nil, nil)

	body, ct := buildMultipartTask(`{"model_id":"m1","timeout_sec":-5}`, "data")
	req := httptest.NewRequest(http.MethodPost, "/task/run", body)
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()

	srv.HandleTaskRun(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for negative timeout_sec, got %d", rec.Code)
	}
}

func TestHandleTaskRun_DefaultLimitsApplied(t *testing.T) {
	var captured *domain.Task
	ts := &mockTaskSvc{
		createTaskFunc: func(_ context.Context, t *domain.Task, _ []byte) error {
			captured = t
			return nil
		},
	}
	srv := testServer(ts, nil, nil)

	// All limits zero → should fall back to server config defaults
	body, ct := buildMultipartTask(`{"model_id":"m1"}`, "data")
	req := httptest.NewRequest(http.MethodPost, "/task/run", body)
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()

	srv.HandleTaskRun(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d", rec.Code)
	}
	if captured.CPULim != srv.config.MaxCPUByTask {
		t.Errorf("expected CPULim=%v, got %v", srv.config.MaxCPUByTask, captured.CPULim)
	}
	if captured.MemLim != srv.config.MaxMemByTask {
		t.Errorf("expected MemLim=%v, got %v", srv.config.MaxMemByTask, captured.MemLim)
	}
	if captured.TimeoutSec != srv.config.DefaultTaskTimeoutSec {
		t.Errorf("expected TimeoutSec=%v, got %v", srv.config.DefaultTaskTimeoutSec, captured.TimeoutSec)
	}
}

func TestHandleTaskRun_SaveInputError(t *testing.T) {
	ts := &mockTaskSvc{
		saveInputFunc: func(_ uuid.UUID, _ string, r io.Reader) ([]byte, error) {
			io.Copy(io.Discard, r)
			return nil, errors.New("disk full")
		},
	}
	srv := testServer(ts, nil, nil)

	body, ct := buildMultipartTask(validTaskJSON(), "data")
	req := httptest.NewRequest(http.MethodPost, "/task/run", body)
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()

	srv.HandleTaskRun(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", rec.Code)
	}
}

func TestHandleTaskRun_CreateTask_ModelNotFound(t *testing.T) {
	ts := &mockTaskSvc{
		createTaskFunc: func(_ context.Context, _ *domain.Task, _ []byte) error {
			return domain.ErrModelNotFound
		},
	}
	srv := testServer(ts, nil, nil)

	body, ct := buildMultipartTask(validTaskJSON(), "data")
	req := httptest.NewRequest(http.MethodPost, "/task/run", body)
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()

	srv.HandleTaskRun(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for model not found, got %d", rec.Code)
	}
}

func TestHandleTaskRun_CreateTask_InternalError(t *testing.T) {
	ts := &mockTaskSvc{
		createTaskFunc: func(_ context.Context, _ *domain.Task, _ []byte) error {
			return errors.New("unexpected error")
		},
	}
	srv := testServer(ts, nil, nil)

	body, ct := buildMultipartTask(validTaskJSON(), "data")
	req := httptest.NewRequest(http.MethodPost, "/task/run", body)
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()

	srv.HandleTaskRun(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", rec.Code)
	}
}

// ─────────────────────────────────────────────
// HandleTaskStatus
// ─────────────────────────────────────────────

func TestHandleTaskStatus_Success(t *testing.T) {
	id := uuid.New()
	srv := testServer(nil, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/task/"+id.String()+"/status", nil)
	req = withChiParam(req, "id", id.String())
	rec := httptest.NewRecorder()

	srv.HandleTaskStatus(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var resp domain.TaskStatusResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if resp.ID != id.String() {
		t.Errorf("expected ID %v, got %v", id, resp.ID)
	}
}

func TestHandleTaskStatus_MissingID(t *testing.T) {
	srv := testServer(nil, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/task//status", nil)
	req = withChiParam(req, "id", "")
	rec := httptest.NewRecorder()

	srv.HandleTaskStatus(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

func TestHandleTaskStatus_InvalidUUID(t *testing.T) {
	srv := testServer(nil, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/task/not-uuid/status", nil)
	req = withChiParam(req, "id", "not-uuid")
	rec := httptest.NewRecorder()

	srv.HandleTaskStatus(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

func TestHandleTaskStatus_ServiceError(t *testing.T) {
	ts := &mockTaskSvc{
		getTaskFunc: func(_ context.Context, _ uuid.UUID) (*domain.Task, error) {
			return nil, errors.New("db error")
		},
	}
	srv := testServer(ts, nil, nil)
	id := uuid.New()

	req := httptest.NewRequest(http.MethodGet, "/task/"+id.String()+"/status", nil)
	req = withChiParam(req, "id", id.String())
	rec := httptest.NewRecorder()

	srv.HandleTaskStatus(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", rec.Code)
	}
}

func TestHandleTaskStatus_TaskNotFound(t *testing.T) {
	ts := &mockTaskSvc{
		getTaskFunc: func(_ context.Context, _ uuid.UUID) (*domain.Task, error) { return nil, nil },
	}
	srv := testServer(ts, nil, nil)
	id := uuid.New()

	req := httptest.NewRequest(http.MethodGet, "/task/"+id.String()+"/status", nil)
	req = withChiParam(req, "id", id.String())
	rec := httptest.NewRecorder()

	srv.HandleTaskStatus(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rec.Code)
	}
}

// ─────────────────────────────────────────────
// mapTaskToResp — all status branches
// ─────────────────────────────────────────────

func TestMapTaskToResp_ScheduledStatus(t *testing.T) {
	now := time.Now()
	task := &domain.Task{ID: uuid.New(), Status: domain.TaskScheduled, ScheduledAt: &now}

	resp := mapTaskToResp(task)

	if resp.ScheduledAt == nil {
		t.Error("expected ScheduledAt to be set for TaskScheduled")
	}
	if resp.StartedAt != nil {
		t.Error("StartedAt should be nil for TaskScheduled")
	}
}

func TestMapTaskToResp_RunningStatus(t *testing.T) {
	now := time.Now()
	task := &domain.Task{ID: uuid.New(), Status: domain.TaskRunning, StartedAt: &now}

	resp := mapTaskToResp(task)

	if resp.StartedAt == nil {
		t.Error("expected StartedAt to be set for TaskRunning")
	}
}

func TestMapTaskToResp_FailedStatus(t *testing.T) {
	now := time.Now()
	task := &domain.Task{
		ID:        uuid.New(),
		Status:    domain.TaskFailed,
		StartedAt: &now,
		ErrorLog:  "container crashed",
	}

	resp := mapTaskToResp(task)

	if resp.ErrLog != "container crashed" {
		t.Errorf("expected ErrLog 'container crashed', got %q", resp.ErrLog)
	}
	if resp.StartedAt == nil {
		t.Error("expected StartedAt for TaskFailed")
	}
}

func TestMapTaskToResp_CompletedStatus(t *testing.T) {
	now := time.Now()
	task := &domain.Task{
		ID:         uuid.New(),
		Status:     domain.TaskCompleted,
		StartedAt:  &now,
		FinishedAt: &now,
		ResultPath: "s3://bucket/result.zip",
	}

	resp := mapTaskToResp(task)

	if resp.FinishedAt == nil {
		t.Error("expected FinishedAt for TaskCompleted")
	}
	if resp.ResultPath != "s3://bucket/result.zip" {
		t.Errorf("unexpected ResultPath: %q", resp.ResultPath)
	}
	if resp.StartedAt == nil {
		t.Error("expected StartedAt for TaskCompleted")
	}
}

func TestMapTaskToResp_QueuedStatus_NoExtraFields(t *testing.T) {
	now := time.Now()
	task := &domain.Task{
		ID:          uuid.New(),
		Status:      domain.TaskQueued,
		ScheduledAt: &now,
		StartedAt:   &now,
		FinishedAt:  &now,
		ErrorLog:    "ignored",
		ResultPath:  "ignored",
	}

	resp := mapTaskToResp(task)

	if resp.ScheduledAt != nil {
		t.Error("ScheduledAt should be nil for non-scheduled status")
	}
	if resp.StartedAt != nil {
		t.Error("StartedAt should be nil for TaskQueued")
	}
	if resp.FinishedAt != nil {
		t.Error("FinishedAt should be nil for TaskQueued")
	}
	if resp.ErrLog != "" {
		t.Error("ErrLog should be empty for TaskQueued")
	}
}

// ─────────────────────────────────────────────
// HandleTaskDelete
// ─────────────────────────────────────────────

func TestHandleTaskDelete_Success(t *testing.T) {
	id := uuid.New()
	ts := &mockTaskSvc{
		getTaskFunc: func(_ context.Context, i uuid.UUID) (*domain.Task, error) {
			return &domain.Task{ID: i, Status: domain.TaskCompleted}, nil
		},
	}
	srv := testServer(ts, nil, nil)

	req := httptest.NewRequest(http.MethodDelete, "/task/"+id.String(), nil)
	req = withChiParam(req, "id", id.String())
	rec := httptest.NewRecorder()

	srv.HandleTaskDelete(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}

func TestHandleTaskDelete_MissingID(t *testing.T) {
	srv := testServer(nil, nil, nil)

	req := httptest.NewRequest(http.MethodDelete, "/task/", nil)
	req = withChiParam(req, "id", "")
	rec := httptest.NewRecorder()

	srv.HandleTaskDelete(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

func TestHandleTaskDelete_InvalidUUID(t *testing.T) {
	srv := testServer(nil, nil, nil)

	req := httptest.NewRequest(http.MethodDelete, "/task/bad-id", nil)
	req = withChiParam(req, "id", "bad-id")
	rec := httptest.NewRecorder()

	srv.HandleTaskDelete(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

func TestHandleTaskDelete_GetTaskError(t *testing.T) {
	ts := &mockTaskSvc{
		getTaskFunc: func(_ context.Context, _ uuid.UUID) (*domain.Task, error) {
			return nil, errors.New("db error")
		},
	}
	srv := testServer(ts, nil, nil)
	id := uuid.New()

	req := httptest.NewRequest(http.MethodDelete, "/task/"+id.String(), nil)
	req = withChiParam(req, "id", id.String())
	rec := httptest.NewRecorder()

	srv.HandleTaskDelete(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", rec.Code)
	}
}

func TestHandleTaskDelete_TaskNotFound(t *testing.T) {
	ts := &mockTaskSvc{
		getTaskFunc: func(_ context.Context, _ uuid.UUID) (*domain.Task, error) { return nil, nil },
	}
	srv := testServer(ts, nil, nil)
	id := uuid.New()

	req := httptest.NewRequest(http.MethodDelete, "/task/"+id.String(), nil)
	req = withChiParam(req, "id", id.String())
	rec := httptest.NewRecorder()

	srv.HandleTaskDelete(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

func TestHandleTaskDelete_RunningTask(t *testing.T) {
	ts := &mockTaskSvc{
		getTaskFunc: func(_ context.Context, i uuid.UUID) (*domain.Task, error) {
			return &domain.Task{ID: i, Status: domain.TaskRunning}, nil
		},
	}
	srv := testServer(ts, nil, nil)
	id := uuid.New()

	req := httptest.NewRequest(http.MethodDelete, "/task/"+id.String(), nil)
	req = withChiParam(req, "id", id.String())
	rec := httptest.NewRecorder()

	srv.HandleTaskDelete(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for running task, got %d", rec.Code)
	}
}

func TestHandleTaskDelete_DeleteError(t *testing.T) {
	ts := &mockTaskSvc{
		getTaskFunc: func(_ context.Context, i uuid.UUID) (*domain.Task, error) {
			return &domain.Task{ID: i, Status: domain.TaskCompleted}, nil
		},
		deleteTaskFunc: func(_ context.Context, _ uuid.UUID) error {
			return errors.New("delete failed")
		},
	}
	srv := testServer(ts, nil, nil)
	id := uuid.New()

	req := httptest.NewRequest(http.MethodDelete, "/task/"+id.String(), nil)
	req = withChiParam(req, "id", id.String())
	rec := httptest.NewRecorder()

	srv.HandleTaskDelete(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", rec.Code)
	}
}

// ─────────────────────────────────────────────
// HandleTaskStop
// ─────────────────────────────────────────────

func TestHandleTaskStop_Success_DefaultTimeout(t *testing.T) {
	id := uuid.New()
	srv := testServer(nil, nil, nil)

	req := httptest.NewRequest(http.MethodPost, "/task/"+id.String()+"/stop", nil)
	req = withChiParam(req, "id", id.String())
	rec := httptest.NewRecorder()

	srv.HandleTaskStop(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}

func TestHandleTaskStop_TimeoutAsSeconds(t *testing.T) {
	var captured time.Duration
	ts := &mockTaskSvc{
		stopTaskFunc: func(_ context.Context, _ uuid.UUID, d time.Duration) error {
			captured = d
			return nil
		},
	}
	srv := testServer(ts, nil, nil)
	id := uuid.New()

	req := httptest.NewRequest(http.MethodPost, "/task/"+id.String()+"/stop?timeout=15", nil)
	req = withChiParam(req, "id", id.String())
	rec := httptest.NewRecorder()

	srv.HandleTaskStop(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
	if captured != 15*time.Second {
		t.Errorf("expected 15s timeout, got %v", captured)
	}
}

func TestHandleTaskStop_TimeoutAsDurationString(t *testing.T) {
	var captured time.Duration
	ts := &mockTaskSvc{
		stopTaskFunc: func(_ context.Context, _ uuid.UUID, d time.Duration) error {
			captured = d
			return nil
		},
	}
	srv := testServer(ts, nil, nil)
	id := uuid.New()

	req := httptest.NewRequest(http.MethodPost, "/task/"+id.String()+"/stop?timeout=20s", nil)
	req = withChiParam(req, "id", id.String())
	rec := httptest.NewRecorder()

	srv.HandleTaskStop(rec, req)

	if captured != 20*time.Second {
		t.Errorf("expected 20s, got %v", captured)
	}
}

func TestHandleTaskStop_InvalidTimeoutFormat(t *testing.T) {
	id := uuid.New()
	srv := testServer(nil, nil, nil)

	req := httptest.NewRequest(http.MethodPost, "/task/"+id.String()+"/stop?timeout=not-valid", nil)
	req = withChiParam(req, "id", id.String())
	rec := httptest.NewRecorder()

	srv.HandleTaskStop(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

func TestHandleTaskStop_MissingID(t *testing.T) {
	srv := testServer(nil, nil, nil)

	req := httptest.NewRequest(http.MethodPost, "/task//stop", nil)
	req = withChiParam(req, "id", "")
	rec := httptest.NewRecorder()

	srv.HandleTaskStop(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

func TestHandleTaskStop_InvalidUUID(t *testing.T) {
	srv := testServer(nil, nil, nil)

	req := httptest.NewRequest(http.MethodPost, "/task/bad-uuid/stop", nil)
	req = withChiParam(req, "id", "bad-uuid")
	rec := httptest.NewRecorder()

	srv.HandleTaskStop(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

func TestHandleTaskStop_ServiceError(t *testing.T) {
	ts := &mockTaskSvc{
		stopTaskFunc: func(_ context.Context, _ uuid.UUID, _ time.Duration) error {
			return errors.New("stop failed")
		},
	}
	srv := testServer(ts, nil, nil)
	id := uuid.New()

	req := httptest.NewRequest(http.MethodPost, "/task/"+id.String()+"/stop", nil)
	req = withChiParam(req, "id", id.String())
	rec := httptest.NewRecorder()

	srv.HandleTaskStop(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", rec.Code)
	}
}

// ─────────────────────────────────────────────
// HandleTaskResult
// ─────────────────────────────────────────────

func TestHandleTaskResult_Success(t *testing.T) {
	id := uuid.New()
	srv := testServer(nil, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/task/"+id.String()+"/result", nil)
	req = withChiParam(req, "id", id.String())
	rec := httptest.NewRecorder()

	srv.HandleTaskResult(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var resp map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if resp["download_url"] == "" {
		t.Error("expected download_url in response")
	}
}

func TestHandleTaskResult_MissingID(t *testing.T) {
	srv := testServer(nil, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/task//result", nil)
	req = withChiParam(req, "id", "")
	rec := httptest.NewRecorder()

	srv.HandleTaskResult(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

func TestHandleTaskResult_InvalidUUID(t *testing.T) {
	srv := testServer(nil, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/task/bad/result", nil)
	req = withChiParam(req, "id", "bad")
	rec := httptest.NewRecorder()

	srv.HandleTaskResult(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

func TestHandleTaskResult_ServiceError(t *testing.T) {
	ts := &mockTaskSvc{
		getResultURLFunc: func(_ context.Context, _ uuid.UUID) (string, error) {
			return "", errors.New("storage error")
		},
	}
	srv := testServer(ts, nil, nil)
	id := uuid.New()

	req := httptest.NewRequest(http.MethodGet, "/task/"+id.String()+"/result", nil)
	req = withChiParam(req, "id", id.String())
	rec := httptest.NewRecorder()

	srv.HandleTaskResult(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", rec.Code)
	}
}

func TestHandleTaskResult_EmptyURL_NotReady(t *testing.T) {
	ts := &mockTaskSvc{
		getResultURLFunc: func(_ context.Context, _ uuid.UUID) (string, error) { return "", nil },
	}
	srv := testServer(ts, nil, nil)
	id := uuid.New()

	req := httptest.NewRequest(http.MethodGet, "/task/"+id.String()+"/result", nil)
	req = withChiParam(req, "id", id.String())
	rec := httptest.NewRecorder()

	srv.HandleTaskResult(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rec.Code)
	}
}

// ─────────────────────────────────────────────
// HandleGetAllTasks
// ─────────────────────────────────────────────

func TestHandleGetAllTasks_Success(t *testing.T) {
	srv := testServer(nil, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/task/list?page=1&page_size=5", nil)
	rec := httptest.NewRecorder()

	srv.HandleGetAllTasks(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var resp domain.GetAllTasksResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if resp.TotalCount == 0 {
		t.Error("expected non-zero TotalCount")
	}
}

func TestHandleGetAllTasks_DefaultPagination(t *testing.T) {
	var capturedPage, capturedSize int
	ts := &mockTaskSvc{
		listTasksFunc: func(_ context.Context, page, size int) ([]domain.Task, int64, error) {
			capturedPage = page
			capturedSize = size
			return nil, 0, nil
		},
	}
	srv := testServer(ts, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/task/list", nil)
	rec := httptest.NewRecorder()
	srv.HandleGetAllTasks(rec, req)

	if capturedPage != 1 {
		t.Errorf("expected page=1, got %d", capturedPage)
	}
	if capturedSize != 10 {
		t.Errorf("expected page_size=10, got %d", capturedSize)
	}
}

func TestHandleGetAllTasks_NegativePaginationDefaults(t *testing.T) {
	var capturedPage, capturedSize int
	ts := &mockTaskSvc{
		listTasksFunc: func(_ context.Context, page, size int) ([]domain.Task, int64, error) {
			capturedPage = page
			capturedSize = size
			return nil, 0, nil
		},
	}
	srv := testServer(ts, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/task/list?page=-1&page_size=0", nil)
	rec := httptest.NewRecorder()
	srv.HandleGetAllTasks(rec, req)

	if capturedPage != 1 || capturedSize != 10 {
		t.Errorf("expected defaults page=1,size=10; got page=%d,size=%d", capturedPage, capturedSize)
	}
}

func TestHandleGetAllTasks_ServiceError(t *testing.T) {
	ts := &mockTaskSvc{
		listTasksFunc: func(_ context.Context, _, _ int) ([]domain.Task, int64, error) {
			return nil, 0, errors.New("db error")
		},
	}
	srv := testServer(ts, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/task/list", nil)
	rec := httptest.NewRecorder()
	srv.HandleGetAllTasks(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", rec.Code)
	}
}

func TestHandleGetAllTasks_EmptyList(t *testing.T) {
	ts := &mockTaskSvc{
		listTasksFunc: func(_ context.Context, _, _ int) ([]domain.Task, int64, error) {
			return []domain.Task{}, 0, nil
		},
	}
	srv := testServer(ts, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/task/list", nil)
	rec := httptest.NewRecorder()
	srv.HandleGetAllTasks(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}
