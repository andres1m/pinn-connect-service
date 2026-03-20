package service

import (
	"bytes"
	"context"
	"errors"
	"io"
	"pinn-connect-service/internal/config"
	"pinn-connect-service/internal/domain"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

// ─────────────────────────────────────────────
// MOCKS
// ─────────────────────────────────────────────

type mockContainerManager struct {
	ContainerManager
	startFunc func(context.Context, *domain.ContainerConfig) (string, error)
	waitFunc  func(context.Context, string) (int64, error)
	stopFunc  func(context.Context, string, time.Duration) error
	logsFunc  func(context.Context, string, bool) (io.ReadCloser, error)
}

func (m *mockContainerManager) StartContainer(ctx context.Context, c *domain.ContainerConfig) (string, error) {
	if m.startFunc != nil {
		return m.startFunc(ctx, c)
	}
	return "ctr-1", nil
}
func (m *mockContainerManager) WaitContainer(ctx context.Context, id string) (int64, error) {
	if m.waitFunc != nil {
		return m.waitFunc(ctx, id)
	}
	return 0, nil
}
func (m *mockContainerManager) StopContainer(ctx context.Context, id string, t time.Duration) error {
	if m.stopFunc != nil {
		return m.stopFunc(ctx, id, t)
	}
	return nil
}
func (m *mockContainerManager) GetContainerLogs(ctx context.Context, id string, f bool) (io.ReadCloser, error) {
	if m.logsFunc != nil {
		return m.logsFunc(ctx, id, f)
	}
	// docker multiplexed stream: header (8 bytes) + payload
	return io.NopCloser(bytes.NewReader([]byte{0x02, 0, 0, 0, 0, 0, 0, 2, 'e', 'r'})), nil
}
func (m *mockContainerManager) RemoveContainer(ctx context.Context, id string) error { return nil }
func (m *mockContainerManager) GetContainerState(ctx context.Context, id string) (*domain.ContainerState, error) {
	return nil, nil
}

// ─────────────────────────────────────────────

type mockRepository struct {
	TaskRepository
	getByIdFunc      func(context.Context, uuid.UUID) (*domain.Task, error)
	getNextFunc      func(context.Context) (*domain.Task, error)
	getScheduledFunc func(context.Context, time.Time) ([]domain.Task, error)
	markFunc         func(context.Context, *domain.Task, domain.TaskStatus) error
	findCachedFunc   func(context.Context, string) (string, error)
	countFunc        func(context.Context) (int64, error)
	listFunc         func(context.Context, int32, int32) ([]domain.Task, error)
	deleteFunc       func(context.Context, uuid.UUID) error
	createFunc       func(context.Context, *domain.Task) error
}

func (m *mockRepository) Create(ctx context.Context, t *domain.Task) error {
	if m.createFunc != nil {
		return m.createFunc(ctx, t)
	}
	return nil
}
func (m *mockRepository) GetTaskById(ctx context.Context, id uuid.UUID) (*domain.Task, error) {
	if m.getByIdFunc != nil {
		return m.getByIdFunc(ctx, id)
	}
	return &domain.Task{ID: id, Status: domain.TaskQueued}, nil
}
func (m *mockRepository) GetNextQueuedTask(ctx context.Context) (*domain.Task, error) {
	if m.getNextFunc != nil {
		return m.getNextFunc(ctx)
	}
	return nil, nil
}
func (m *mockRepository) GetScheduledTasks(ctx context.Context, t time.Time) ([]domain.Task, error) {
	if m.getScheduledFunc != nil {
		return m.getScheduledFunc(ctx, t)
	}
	return nil, nil
}
func (m *mockRepository) Mark(ctx context.Context, t *domain.Task, s domain.TaskStatus) error {
	if m.markFunc != nil {
		return m.markFunc(ctx, t, s)
	}
	return nil
}
func (m *mockRepository) FindCachedTask(ctx context.Context, s string) (string, error) {
	if m.findCachedFunc != nil {
		return m.findCachedFunc(ctx, s)
	}
	return "", nil
}
func (m *mockRepository) DeleteTask(ctx context.Context, id uuid.UUID) error {
	if m.deleteFunc != nil {
		return m.deleteFunc(ctx, id)
	}
	return nil
}
func (m *mockRepository) GetTasksCount(ctx context.Context) (int64, error) {
	if m.countFunc != nil {
		return m.countFunc(ctx)
	}
	return 5, nil
}
func (m *mockRepository) GetTasksPaginated(ctx context.Context, l, o int32) ([]domain.Task, error) {
	if m.listFunc != nil {
		return m.listFunc(ctx, l, o)
	}
	return []domain.Task{{ID: uuid.New()}}, nil
}

// ─────────────────────────────────────────────

type mockArtifactStorage struct{ ArtifactStorage }

func (m *mockArtifactStorage) UploadToStorage(ctx context.Context, id uuid.UUID, d string) (string, error) {
	if d == "fail" {
		return "", errors.New("upload error")
	}
	return "result-key", nil
}
func (m *mockArtifactStorage) GetDownloadURL(ctx context.Context, k string) (string, error) {
	if k == "fail" {
		return "", errors.New("url error")
	}
	return "https://example.com/result", nil
}
func (m *mockArtifactStorage) DeleteArtifacts(ctx context.Context, id uuid.UUID) error {
	// fixed UUID triggers artifact delete error
	if id.String() == "00000000-0000-0000-0000-000000000002" {
		return errors.New("delete artifact error")
	}
	return nil
}

// ─────────────────────────────────────────────

type mockWorkspace struct {
	Workspace
	prepFunc      func(uuid.UUID) error
	saveInputFunc func(uuid.UUID, string, io.Reader) error
}

func (m *mockWorkspace) Prepare(id uuid.UUID) error {
	if m.prepFunc != nil {
		return m.prepFunc(id)
	}
	return nil
}
func (m *mockWorkspace) ResultDir(id uuid.UUID) string {
	// fixed UUID triggers upload failure via the "fail" dir
	if id.String() == "00000000-0000-0000-0000-000000000001" {
		return "fail"
	}
	return "/tmp/result"
}
func (m *mockWorkspace) Cleanup(id uuid.UUID) error {
	// fixed UUID triggers cleanup error
	if id.String() == "00000000-0000-0000-0000-000000000003" {
		return errors.New("cleanup error")
	}
	return nil
}
func (m *mockWorkspace) SaveInput(id uuid.UUID, n string, r io.Reader) error {
	if m.saveInputFunc != nil {
		return m.saveInputFunc(id, n, r)
	}
	io.Copy(io.Discard, r)
	return nil
}

// ─────────────────────────────────────────────

type mockModelRepo struct {
	ModelRepository
	getByIDFunc func(context.Context, string) (*domain.Model, error)
}

func (m *mockModelRepo) GetModelByID(ctx context.Context, id string) (*domain.Model, error) {
	if m.getByIDFunc != nil {
		return m.getByIDFunc(ctx, id)
	}
	if id == "not-found" {
		return nil, nil
	}
	return &domain.Model{ID: id, ContainerImage: "img:latest"}, nil
}

// ─────────────────────────────────────────────
// HELPERS
// ─────────────────────────────────────────────

func defaultCfg() *config.Config {
	return &config.Config{
		Worker: config.WorkerConfig{
			Interval:                  time.Millisecond,
			MaxWorkers:                2,
			ProcessTaskCleanupTimeout: time.Second,
		},
		Scheduler: config.SchedulerConfig{
			Interval:    time.Millisecond,
			TaskExpires: time.Hour,
		},
		TmpDir: "/tmp",
	}
}

func buildSvc(repo *mockRepository, mgr *mockContainerManager, ws *mockWorkspace, modelRepo *mockModelRepo) *TaskService {
	if modelRepo == nil {
		modelRepo = &mockModelRepo{}
	}
	ms := NewModelService(modelRepo, nil)
	return NewTaskService(mgr, &mockArtifactStorage{}, defaultCfg(), repo, ws, ms)
}

func defaultSvc() (*TaskService, *mockRepository, *mockContainerManager, *mockWorkspace) {
	repo := &mockRepository{}
	mgr := &mockContainerManager{}
	ws := &mockWorkspace{}
	return buildSvc(repo, mgr, ws, nil), repo, mgr, ws
}

// ─────────────────────────────────────────────
// SaveInput
// ─────────────────────────────────────────────

func TestSaveInput_PrepareError(t *testing.T) {
	svc, _, _, ws := defaultSvc()
	ws.prepFunc = func(id uuid.UUID) error { return errors.New("prepare failed") }

	_, err := svc.SaveInput(uuid.New(), "input.txt", bytes.NewBufferString("data"))
	if err == nil {
		t.Fatal("expected error from Prepare, got nil")
	}
}

func TestSaveInput_SaveError(t *testing.T) {
	svc, _, _, ws := defaultSvc()
	ws.saveInputFunc = func(_ uuid.UUID, _ string, r io.Reader) error {
		io.Copy(io.Discard, r)
		return errors.New("save failed")
	}

	_, err := svc.SaveInput(uuid.New(), "input.txt", bytes.NewBufferString("data"))
	if err == nil {
		t.Fatal("expected error from SaveInput, got nil")
	}
}

func TestSaveInput_Success(t *testing.T) {
	svc, _, _, _ := defaultSvc()

	hash, err := svc.SaveInput(uuid.New(), "input.txt", bytes.NewBufferString("hello"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(hash) == 0 {
		t.Fatal("expected non-empty hash")
	}
}

// ─────────────────────────────────────────────
// CreateTask
// ─────────────────────────────────────────────

func TestCreateTask_ModelServiceError(t *testing.T) {
	repo := &mockRepository{}
	mgr := &mockContainerManager{}
	ws := &mockWorkspace{}
	mr := &mockModelRepo{
		getByIDFunc: func(_ context.Context, _ string) (*domain.Model, error) {
			return nil, errors.New("db error")
		},
	}
	svc := buildSvc(repo, mgr, ws, mr)

	err := svc.CreateTask(context.Background(), &domain.Task{ModelID: "m1"}, []byte("hash"))
	if err == nil {
		t.Fatal("expected error from model service, got nil")
	}
}

func TestCreateTask_ModelNotFound(t *testing.T) {
	svc, _, _, _ := defaultSvc()

	err := svc.CreateTask(context.Background(), &domain.Task{ModelID: "not-found"}, []byte("hash"))
	if !errors.Is(err, domain.ErrModelNotFound) {
		t.Fatalf("expected ErrModelNotFound, got %v", err)
	}
}

func TestCreateTask_CacheHit(t *testing.T) {
	svc, repo, _, _ := defaultSvc()
	repo.findCachedFunc = func(_ context.Context, _ string) (string, error) {
		return "cached-result-path", nil
	}

	task := &domain.Task{ModelID: "m1"}
	err := svc.CreateTask(context.Background(), task, []byte("hash"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if task.Status != domain.TaskCompleted {
		t.Errorf("expected TaskCompleted, got %v", task.Status)
	}
	if task.ResultPath != "cached-result-path" {
		t.Errorf("expected cached result path, got %q", task.ResultPath)
	}
}

func TestCreateTask_CacheError(t *testing.T) {
	svc, repo, _, _ := defaultSvc()
	repo.findCachedFunc = func(_ context.Context, _ string) (string, error) {
		return "", errors.New("cache lookup failed")
	}

	// cache error is logged but processing continues → task should be queued
	task := &domain.Task{ModelID: "m1"}
	err := svc.CreateTask(context.Background(), task, []byte("hash"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if task.Status != domain.TaskQueued {
		t.Errorf("expected TaskQueued, got %v", task.Status)
	}
}

func TestCreateTask_Scheduled(t *testing.T) {
	svc, _, _, _ := defaultSvc()

	future := time.Now().Add(time.Hour)
	task := &domain.Task{ModelID: "m1", ScheduledAt: &future}
	err := svc.CreateTask(context.Background(), task, []byte("hash"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if task.Status != domain.TaskScheduled {
		t.Errorf("expected TaskScheduled, got %v", task.Status)
	}
}

func TestCreateTask_Queued(t *testing.T) {
	svc, _, _, _ := defaultSvc()

	task := &domain.Task{ModelID: "m1", ContainerEnvs: []string{"Z=1", "A=2"}}
	err := svc.CreateTask(context.Background(), task, []byte("hash"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if task.Status != domain.TaskQueued {
		t.Errorf("expected TaskQueued, got %v", task.Status)
	}
	if task.Signature == "" {
		t.Error("expected non-empty signature")
	}
}

func TestCreateTask_PastScheduledAt(t *testing.T) {
	svc, _, _, _ := defaultSvc()

	past := time.Now().Add(-time.Hour)
	task := &domain.Task{ModelID: "m1", ScheduledAt: &past}
	err := svc.CreateTask(context.Background(), task, []byte("hash"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if task.Status != domain.TaskQueued {
		t.Errorf("expected TaskQueued for past schedule, got %v", task.Status)
	}
}

func TestCreateTask_InitError(t *testing.T) {
	svc, repo, _, _ := defaultSvc()
	repo.createFunc = func(_ context.Context, _ *domain.Task) error {
		return errors.New("db insert failed")
	}

	err := svc.CreateTask(context.Background(), &domain.Task{ModelID: "m1"}, []byte("hash"))
	if err == nil {
		t.Fatal("expected error from Create, got nil")
	}
}

func TestCreateTask_CacheHit_InitError(t *testing.T) {
	svc, repo, _, _ := defaultSvc()
	repo.findCachedFunc = func(_ context.Context, _ string) (string, error) { return "path", nil }
	repo.createFunc = func(_ context.Context, _ *domain.Task) error { return errors.New("create err") }

	err := svc.CreateTask(context.Background(), &domain.Task{ModelID: "m1"}, []byte("hash"))
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// ─────────────────────────────────────────────
// StopTask
// ─────────────────────────────────────────────

func TestStopTask_GetByIdError(t *testing.T) {
	svc, repo, _, _ := defaultSvc()
	repo.getByIdFunc = func(_ context.Context, _ uuid.UUID) (*domain.Task, error) {
		return nil, errors.New("not found")
	}

	err := svc.StopTask(context.Background(), uuid.New(), time.Second)
	if err == nil {
		t.Fatal("expected error from GetTaskById, got nil")
	}
}

func TestStopTask_EmptyContainerID(t *testing.T) {
	svc, repo, _, _ := defaultSvc()
	repo.getByIdFunc = func(_ context.Context, id uuid.UUID) (*domain.Task, error) {
		return &domain.Task{ID: id, ContainerID: ""}, nil
	}

	err := svc.StopTask(context.Background(), uuid.New(), time.Second)
	if err == nil {
		t.Fatal("expected error for empty container ID, got nil")
	}
}

func TestStopTask_MarkError(t *testing.T) {
	svc, repo, _, _ := defaultSvc()
	repo.getByIdFunc = func(_ context.Context, id uuid.UUID) (*domain.Task, error) {
		return &domain.Task{ID: id, ContainerID: "ctr-1"}, nil
	}
	repo.markFunc = func(_ context.Context, _ *domain.Task, _ domain.TaskStatus) error {
		return errors.New("mark failed")
	}

	err := svc.StopTask(context.Background(), uuid.New(), time.Second)
	if err == nil {
		t.Fatal("expected error from Mark, got nil")
	}
}

func TestStopTask_StopContainerError(t *testing.T) {
	svc, repo, mgr, _ := defaultSvc()
	repo.getByIdFunc = func(_ context.Context, id uuid.UUID) (*domain.Task, error) {
		return &domain.Task{ID: id, ContainerID: "ctr-1"}, nil
	}
	mgr.stopFunc = func(_ context.Context, _ string, _ time.Duration) error {
		return errors.New("stop failed")
	}

	err := svc.StopTask(context.Background(), uuid.New(), time.Second)
	if err == nil {
		t.Fatal("expected error from StopContainer, got nil")
	}
}

func TestStopTask_Success(t *testing.T) {
	svc, repo, _, _ := defaultSvc()
	repo.getByIdFunc = func(_ context.Context, id uuid.UUID) (*domain.Task, error) {
		return &domain.Task{ID: id, ContainerID: "ctr-1"}, nil
	}

	err := svc.StopTask(context.Background(), uuid.New(), time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// ─────────────────────────────────────────────
// GetTask
// ─────────────────────────────────────────────

func TestGetTask_RepoError(t *testing.T) {
	svc, repo, _, _ := defaultSvc()
	repo.getByIdFunc = func(_ context.Context, _ uuid.UUID) (*domain.Task, error) {
		return nil, errors.New("db error")
	}

	_, err := svc.GetTask(context.Background(), uuid.New())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestGetTask_Success(t *testing.T) {
	svc, _, _, _ := defaultSvc()
	id := uuid.New()

	task, err := svc.GetTask(context.Background(), id)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if task.ID != id {
		t.Errorf("expected task ID %v, got %v", id, task.ID)
	}
}

// ─────────────────────────────────────────────
// ListTasks
// ─────────────────────────────────────────────

func TestListTasks_Success(t *testing.T) {
	svc, _, _, _ := defaultSvc()

	tasks, total, err := svc.ListTasks(context.Background(), 1, 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tasks) == 0 {
		t.Error("expected at least one task")
	}
	if total == 0 {
		t.Error("expected non-zero total")
	}
}

func TestListTasks_DefaultPagination(t *testing.T) {
	svc, _, _, _ := defaultSvc()

	// page=0 and pageSize=0 should be corrected to 1 and 10
	_, _, err := svc.ListTasks(context.Background(), 0, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestListTasks_PaginatedError(t *testing.T) {
	svc, repo, _, _ := defaultSvc()
	repo.listFunc = func(_ context.Context, _ int32, _ int32) ([]domain.Task, error) {
		return nil, errors.New("db error")
	}

	_, _, err := svc.ListTasks(context.Background(), 1, 10)
	if err == nil {
		t.Fatal("expected error from GetTasksPaginated, got nil")
	}
}

func TestListTasks_CountError(t *testing.T) {
	svc, repo, _, _ := defaultSvc()
	repo.countFunc = func(_ context.Context) (int64, error) {
		return 0, errors.New("count error")
	}

	_, _, err := svc.ListTasks(context.Background(), 1, 10)
	if err == nil {
		t.Fatal("expected error from GetTasksCount, got nil")
	}
}

// ─────────────────────────────────────────────
// DeleteTask
// ─────────────────────────────────────────────

func TestDeleteTask_ArtifactError(t *testing.T) {
	svc, _, _, _ := defaultSvc()
	id := mustParseUUID("00000000-0000-0000-0000-000000000002")

	err := svc.DeleteTask(context.Background(), id)
	if err == nil {
		t.Fatal("expected artifact delete error, got nil")
	}
}

func TestDeleteTask_WorkspaceError(t *testing.T) {
	svc, _, _, _ := defaultSvc()
	id := mustParseUUID("00000000-0000-0000-0000-000000000003")

	err := svc.DeleteTask(context.Background(), id)
	if err == nil {
		t.Fatal("expected workspace cleanup error, got nil")
	}
}

func TestDeleteTask_RepoError(t *testing.T) {
	svc, repo, _, _ := defaultSvc()
	repo.deleteFunc = func(_ context.Context, _ uuid.UUID) error {
		return errors.New("delete error")
	}

	err := svc.DeleteTask(context.Background(), uuid.New())
	if err == nil {
		t.Fatal("expected error from DeleteTask, got nil")
	}
}

func TestDeleteTask_Success(t *testing.T) {
	svc, _, _, _ := defaultSvc()

	err := svc.DeleteTask(context.Background(), uuid.New())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// ─────────────────────────────────────────────
// GetResultURL
// ─────────────────────────────────────────────

func TestGetResultURL_GetByIdError(t *testing.T) {
	svc, repo, _, _ := defaultSvc()
	repo.getByIdFunc = func(_ context.Context, _ uuid.UUID) (*domain.Task, error) {
		return nil, errors.New("db error")
	}

	_, err := svc.GetResultURL(context.Background(), uuid.New())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestGetResultURL_NotCompleted(t *testing.T) {
	svc, repo, _, _ := defaultSvc()
	repo.getByIdFunc = func(_ context.Context, id uuid.UUID) (*domain.Task, error) {
		return &domain.Task{ID: id, Status: domain.TaskRunning}, nil
	}

	url, err := svc.GetResultURL(context.Background(), uuid.New())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if url != "" {
		t.Errorf("expected empty URL for non-completed task, got %q", url)
	}
}

func TestGetResultURL_StorageError(t *testing.T) {
	svc, repo, _, _ := defaultSvc()
	repo.getByIdFunc = func(_ context.Context, id uuid.UUID) (*domain.Task, error) {
		return &domain.Task{ID: id, Status: domain.TaskCompleted, ResultPath: "fail"}, nil
	}

	_, err := svc.GetResultURL(context.Background(), uuid.New())
	if err == nil {
		t.Fatal("expected storage error, got nil")
	}
}

func TestGetResultURL_Success(t *testing.T) {
	svc, repo, _, _ := defaultSvc()
	repo.getByIdFunc = func(_ context.Context, id uuid.UUID) (*domain.Task, error) {
		return &domain.Task{ID: id, Status: domain.TaskCompleted, ResultPath: "some-key"}, nil
	}

	url, err := svc.GetResultURL(context.Background(), uuid.New())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if url == "" {
		t.Error("expected non-empty URL")
	}
}

// ─────────────────────────────────────────────
// RecoverTask / getNextRecoverTask
// ─────────────────────────────────────────────

func TestRecoverTask_AddsToQueue(t *testing.T) {
	svc, _, _, _ := defaultSvc()

	task := &domain.Task{ID: uuid.New()}
	svc.RecoverTask(task)

	if len(svc.recoverTaskQueue) != 1 {
		t.Fatalf("expected queue length 1, got %d", len(svc.recoverTaskQueue))
	}
}

func TestGetNextRecoverTask_EmptyQueue(t *testing.T) {
	svc, _, _, _ := defaultSvc()

	result := svc.getNextRecoverTask()
	if result != nil {
		t.Errorf("expected nil from empty queue, got %v", result)
	}
}

func TestGetNextRecoverTask_FIFO(t *testing.T) {
	svc, _, _, _ := defaultSvc()

	first := &domain.Task{ID: uuid.New()}
	second := &domain.Task{ID: uuid.New()}
	svc.RecoverTask(first)
	svc.RecoverTask(second)

	got := svc.getNextRecoverTask()
	if got.ID != first.ID {
		t.Errorf("expected first task, got %v", got.ID)
	}

	got2 := svc.getNextRecoverTask()
	if got2.ID != second.ID {
		t.Errorf("expected second task, got %v", got2.ID)
	}

	if svc.getNextRecoverTask() != nil {
		t.Error("expected nil after draining queue")
	}
}

// ─────────────────────────────────────────────
// getFinalHash
// ─────────────────────────────────────────────

func TestGetFinalHash_SortsEnvs(t *testing.T) {
	task := &domain.Task{
		ModelID:       "model-x",
		ContainerEnvs: []string{"Z=3", "A=1", "M=2"},
		ContainerCmd:  []string{"run"},
	}
	hash1, err := getFinalHash(task, []byte("file-hash"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Same task, different env order — should produce same hash after sorting
	task2 := &domain.Task{
		ModelID:       "model-x",
		ContainerEnvs: []string{"M=2", "Z=3", "A=1"},
		ContainerCmd:  []string{"run"},
	}
	hash2, err := getFinalHash(task2, []byte("file-hash"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if hash1 != hash2 {
		t.Errorf("expected same hash regardless of env order, got %q vs %q", hash1, hash2)
	}
}

func TestGetFinalHash_DifferentInputDifferentHash(t *testing.T) {
	task := &domain.Task{ModelID: "m1", ContainerEnvs: []string{"A=1"}}
	h1, _ := getFinalHash(task, []byte("hash-a"))
	h2, _ := getFinalHash(task, []byte("hash-b"))
	if h1 == h2 {
		t.Error("expected different hashes for different file inputs")
	}
}

func TestGetFinalHash_NonEmpty(t *testing.T) {
	task := &domain.Task{ModelID: "m"}
	hash, err := getFinalHash(task, []byte("h"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if hash == "" {
		t.Error("expected non-empty hash")
	}
}

// ─────────────────────────────────────────────
// waitAndSaveTask
// ─────────────────────────────────────────────

func TestWaitAndSaveTask_DeadlineExceeded(t *testing.T) {
	svc, _, mgr, _ := defaultSvc()
	mgr.waitFunc = func(_ context.Context, _ string) (int64, error) {
		return 0, context.DeadlineExceeded
	}

	task := &domain.Task{ID: uuid.New(), ContainerID: "ctr-1"}
	err := svc.waitAndSaveTask(context.Background(), task)
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
}

func TestWaitAndSaveTask_ContextCanceled(t *testing.T) {
	svc, _, mgr, _ := defaultSvc()
	mgr.waitFunc = func(_ context.Context, _ string) (int64, error) {
		return 0, context.Canceled
	}

	task := &domain.Task{ID: uuid.New(), ContainerID: "ctr-1"}
	err := svc.waitAndSaveTask(context.Background(), task)
	if err == nil {
		t.Fatal("expected canceled error, got nil")
	}
}

func TestWaitAndSaveTask_WaitError_LogsError(t *testing.T) {
	svc, _, mgr, _ := defaultSvc()
	mgr.waitFunc = func(_ context.Context, _ string) (int64, error) {
		return 0, errors.New("container crashed")
	}
	mgr.logsFunc = func(_ context.Context, _ string, _ bool) (io.ReadCloser, error) {
		return nil, errors.New("logs unavailable")
	}

	task := &domain.Task{ID: uuid.New(), ContainerID: "ctr-1"}
	err := svc.waitAndSaveTask(context.Background(), task)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestWaitAndSaveTask_WaitError_WithLogs(t *testing.T) {
	svc, _, mgr, _ := defaultSvc()
	mgr.waitFunc = func(_ context.Context, _ string) (int64, error) {
		return 0, errors.New("container crashed")
	}
	// logs returns valid docker multiplexed stream (stderr stream type = 0x02)
	mgr.logsFunc = func(_ context.Context, _ string, _ bool) (io.ReadCloser, error) {
		payload := []byte("stderr output")
		hdr := []byte{0x02, 0, 0, 0, 0, 0, 0, byte(len(payload))}
		return io.NopCloser(bytes.NewReader(append(hdr, payload...))), nil
	}

	task := &domain.Task{ID: uuid.New(), ContainerID: "ctr-1"}
	err := svc.waitAndSaveTask(context.Background(), task)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if task.ErrorLog == "" {
		t.Error("expected ErrorLog to be populated")
	}
}

func TestWaitAndSaveTask_NonZeroExitCode_LogError(t *testing.T) {
	svc, _, mgr, _ := defaultSvc()
	mgr.waitFunc = func(_ context.Context, _ string) (int64, error) { return 1, nil }
	mgr.logsFunc = func(_ context.Context, _ string, _ bool) (io.ReadCloser, error) {
		return nil, errors.New("logs error")
	}

	task := &domain.Task{ID: uuid.New(), ContainerID: "ctr-1"}
	err := svc.waitAndSaveTask(context.Background(), task)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestWaitAndSaveTask_NonZeroExitCode(t *testing.T) {
	svc, _, mgr, _ := defaultSvc()
	mgr.waitFunc = func(_ context.Context, _ string) (int64, error) { return 2, nil }

	task := &domain.Task{ID: uuid.New(), ContainerID: "ctr-1"}
	err := svc.waitAndSaveTask(context.Background(), task)
	if err == nil {
		t.Fatal("expected error for non-zero exit code, got nil")
	}
}

func TestWaitAndSaveTask_GetTaskByIdError(t *testing.T) {
	svc, repo, _, _ := defaultSvc()
	repo.getByIdFunc = func(_ context.Context, _ uuid.UUID) (*domain.Task, error) {
		return nil, errors.New("db error")
	}

	task := &domain.Task{ID: uuid.New(), ContainerID: "ctr-1"}
	err := svc.waitAndSaveTask(context.Background(), task)
	if err == nil {
		t.Fatal("expected error from GetTaskById, got nil")
	}
}

func TestWaitAndSaveTask_TaskStopped(t *testing.T) {
	svc, repo, _, _ := defaultSvc()
	repo.getByIdFunc = func(_ context.Context, id uuid.UUID) (*domain.Task, error) {
		return &domain.Task{ID: id, Status: domain.TaskStopped}, nil
	}

	task := &domain.Task{ID: uuid.New(), ContainerID: "ctr-1"}
	err := svc.waitAndSaveTask(context.Background(), task)
	if err != nil {
		t.Fatalf("expected nil for stopped task, got %v", err)
	}
}

func TestWaitAndSaveTask_UploadError(t *testing.T) {
	svc, _, _, _ := defaultSvc()

	// ID triggers ResultDir to return "fail", which causes UploadToStorage to fail
	id := mustParseUUID("00000000-0000-0000-0000-000000000001")
	task := &domain.Task{ID: id, ContainerID: "ctr-1"}
	err := svc.waitAndSaveTask(context.Background(), task)
	if err == nil {
		t.Fatal("expected upload error, got nil")
	}
}

func TestWaitAndSaveTask_MarkCompletedError(t *testing.T) {
	svc, repo, _, _ := defaultSvc()
	markCalls := 0
	repo.markFunc = func(_ context.Context, _ *domain.Task, s domain.TaskStatus) error {
		markCalls++
		if s == domain.TaskCompleted {
			return errors.New("mark completed failed")
		}
		return nil
	}

	task := &domain.Task{ID: uuid.New(), ContainerID: "ctr-1"}
	err := svc.waitAndSaveTask(context.Background(), task)
	if err == nil {
		t.Fatal("expected error from Mark(completed), got nil")
	}
}

func TestWaitAndSaveTask_Success(t *testing.T) {
	svc, _, _, _ := defaultSvc()

	task := &domain.Task{ID: uuid.New(), ContainerID: "ctr-1"}
	err := svc.waitAndSaveTask(context.Background(), task)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if task.ResultPath == "" {
		t.Error("expected ResultPath to be set")
	}
}

// ─────────────────────────────────────────────
// processTask
// ─────────────────────────────────────────────

func TestProcessTask_TaskStopped(t *testing.T) {
	svc, repo, _, _ := defaultSvc()
	repo.getByIdFunc = func(_ context.Context, id uuid.UUID) (*domain.Task, error) {
		return &domain.Task{ID: id, Status: domain.TaskStopped}, nil
	}

	task := &domain.Task{ID: uuid.New(), ContainerID: "", ContainerImage: "img:latest"}
	err := svc.processTask(context.Background(), task)
	if err == nil {
		t.Fatal("expected error for stopped task, got nil")
	}
}

func TestProcessTask_StartContainerError(t *testing.T) {
	svc, _, mgr, _ := defaultSvc()
	mgr.startFunc = func(_ context.Context, _ *domain.ContainerConfig) (string, error) {
		return "", errors.New("cannot start container")
	}

	task := &domain.Task{ID: uuid.New(), ContainerImage: "img:latest"}
	err := svc.processTask(context.Background(), task)
	if err == nil {
		t.Fatal("expected error from StartContainer, got nil")
	}
}

func TestProcessTask_MarkRunningError(t *testing.T) {
	svc, repo, _, _ := defaultSvc()
	repo.markFunc = func(_ context.Context, _ *domain.Task, s domain.TaskStatus) error {
		if s == domain.TaskRunning {
			return errors.New("mark running failed")
		}
		return nil
	}

	task := &domain.Task{ID: uuid.New(), ContainerImage: "img:latest"}
	err := svc.processTask(context.Background(), task)
	if err == nil {
		t.Fatal("expected error from Mark(running), got nil")
	}
}

func TestProcessTask_Success(t *testing.T) {
	svc, _, _, _ := defaultSvc()

	task := &domain.Task{ID: uuid.New(), ContainerImage: "img:latest", TimeoutSec: 5}
	err := svc.processTask(context.Background(), task)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// ─────────────────────────────────────────────
// processQueue
// ─────────────────────────────────────────────

func TestProcessQueue_SemFull(t *testing.T) {
	svc, _, _, _ := defaultSvc()

	// Fill the semaphore to capacity — processQueue should return immediately
	sem := make(chan struct{}, 1)
	sem <- struct{}{}

	wg := &sync.WaitGroup{}
	svc.processQueue(context.Background(), sem, wg) // must not block
}

func TestProcessQueue_RecoverTask(t *testing.T) {
	svc, _, _, _ := defaultSvc()

	task := &domain.Task{ID: uuid.New(), ContainerID: "ctr-1"}
	svc.RecoverTask(task)

	sem := make(chan struct{}, 1)
	wg := &sync.WaitGroup{}
	svc.processQueue(context.Background(), sem, wg)
	wg.Wait()
}

func TestProcessQueue_NoQueuedTask(t *testing.T) {
	svc, repo, _, _ := defaultSvc()
	repo.getNextFunc = func(_ context.Context) (*domain.Task, error) { return nil, nil }

	sem := make(chan struct{}, 1)
	wg := &sync.WaitGroup{}
	svc.processQueue(context.Background(), sem, wg)
}

func TestProcessQueue_GetNextError(t *testing.T) {
	svc, repo, _, _ := defaultSvc()
	repo.getNextFunc = func(_ context.Context) (*domain.Task, error) {
		return nil, errors.New("queue error")
	}

	sem := make(chan struct{}, 1)
	wg := &sync.WaitGroup{}
	svc.processQueue(context.Background(), sem, wg)
}

func TestProcessQueue_CacheHit(t *testing.T) {
	svc, repo, _, _ := defaultSvc()
	repo.getNextFunc = func(_ context.Context) (*domain.Task, error) {
		return &domain.Task{ID: uuid.New(), Signature: "sig"}, nil
	}
	repo.findCachedFunc = func(_ context.Context, _ string) (string, error) {
		return "cached-path", nil
	}

	sem := make(chan struct{}, 1)
	wg := &sync.WaitGroup{}
	svc.processQueue(context.Background(), sem, wg)
}

func TestProcessQueue_CacheLookupError(t *testing.T) {
	svc, repo, _, _ := defaultSvc()
	repo.getNextFunc = func(_ context.Context) (*domain.Task, error) {
		return &domain.Task{ID: uuid.New(), Signature: "sig", ContainerImage: "img:latest"}, nil
	}
	repo.findCachedFunc = func(_ context.Context, _ string) (string, error) {
		return "", errors.New("cache err")
	}

	sem := make(chan struct{}, 1)
	wg := &sync.WaitGroup{}
	svc.processQueue(context.Background(), sem, wg)
	wg.Wait()
}

func TestProcessQueue_StartsTask(t *testing.T) {
	svc, repo, _, _ := defaultSvc()
	taskID := uuid.New()
	repo.getNextFunc = func(_ context.Context) (*domain.Task, error) {
		return &domain.Task{ID: taskID, Signature: "sig", ContainerImage: "img:latest", TimeoutSec: 1}, nil
	}
	repo.findCachedFunc = func(_ context.Context, _ string) (string, error) { return "", nil }

	sem := make(chan struct{}, 2)
	wg := &sync.WaitGroup{}
	svc.processQueue(context.Background(), sem, wg)
	wg.Wait()
}

// ─────────────────────────────────────────────
// getErrLogs
// ─────────────────────────────────────────────

func TestGetErrLogs_LogsError(t *testing.T) {
	svc, _, mgr, _ := defaultSvc()
	mgr.logsFunc = func(_ context.Context, _ string, _ bool) (io.ReadCloser, error) {
		return nil, errors.New("logs fetch error")
	}

	_, err := svc.getErrLogs(context.Background(), "ctr-1")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestGetErrLogs_Success(t *testing.T) {
	svc, _, _, _ := defaultSvc()

	logs, err := svc.getErrLogs(context.Background(), "ctr-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// default mock returns stderr content "er"
	_ = logs
}

// ─────────────────────────────────────────────
// mark / findCachedTask / cleanupWorkspace
// ─────────────────────────────────────────────

func TestMark_Error(t *testing.T) {
	svc, repo, _, _ := defaultSvc()
	repo.markFunc = func(_ context.Context, _ *domain.Task, _ domain.TaskStatus) error {
		return errors.New("mark error")
	}

	err := svc.mark(context.Background(), &domain.Task{}, domain.TaskCompleted)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestMark_Success(t *testing.T) {
	svc, _, _, _ := defaultSvc()

	err := svc.mark(context.Background(), &domain.Task{}, domain.TaskCompleted)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestFindCachedTask_Error(t *testing.T) {
	svc, repo, _, _ := defaultSvc()
	repo.findCachedFunc = func(_ context.Context, _ string) (string, error) {
		return "", errors.New("cache error")
	}

	_, err := svc.findCachedTask(context.Background(), "sig")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestFindCachedTask_Success(t *testing.T) {
	svc, repo, _, _ := defaultSvc()
	repo.findCachedFunc = func(_ context.Context, _ string) (string, error) {
		return "path", nil
	}

	path, err := svc.findCachedTask(context.Background(), "sig")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if path != "path" {
		t.Errorf("expected 'path', got %q", path)
	}
}

func TestCleanupWorkspace_Error(t *testing.T) {
	svc, _, _, _ := defaultSvc()
	id := mustParseUUID("00000000-0000-0000-0000-000000000003")

	err := svc.cleanupWorkspace(id)
	if err == nil {
		t.Fatal("expected cleanup error, got nil")
	}
}

func TestCleanupWorkspace_Success(t *testing.T) {
	svc, _, _, _ := defaultSvc()

	err := svc.cleanupWorkspace(uuid.New())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// ─────────────────────────────────────────────
// StartWorker / StartScheduler (integration smoke)
// ─────────────────────────────────────────────

func TestStartWorker_LifeCycle(t *testing.T) {
	svc, repo, _, _ := defaultSvc()
	repo.getNextFunc = func(_ context.Context) (*domain.Task, error) { return nil, nil }

	ctx, cancel := context.WithCancel(context.Background())
	wg := &sync.WaitGroup{}
	svc.StartWorker(ctx, wg)

	time.Sleep(20 * time.Millisecond)
	cancel()
	// Give the worker goroutine a moment to drain
	time.Sleep(20 * time.Millisecond)
}

func TestStartScheduler_LifeCycle(t *testing.T) {
	svc, repo, _, _ := defaultSvc()
	repo.getScheduledFunc = func(_ context.Context, _ time.Time) ([]domain.Task, error) {
		return nil, nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	wg := &sync.WaitGroup{}
	svc.StartScheduler(ctx, wg)

	time.Sleep(20 * time.Millisecond)
	cancel()
	time.Sleep(20 * time.Millisecond)
}

func TestStartScheduler_WithScheduledTask(t *testing.T) {
	svc, repo, _, _ := defaultSvc()

	taskID := uuid.New()
	scheduledAt := time.Now().Add(10 * time.Millisecond)
	called := false

	repo.getScheduledFunc = func(_ context.Context, _ time.Time) ([]domain.Task, error) {
		if called {
			return nil, nil
		}
		called = true
		return []domain.Task{{ID: taskID, ScheduledAt: &scheduledAt, Status: domain.TaskScheduled}}, nil
	}
	repo.getByIdFunc = func(_ context.Context, id uuid.UUID) (*domain.Task, error) {
		return &domain.Task{ID: id, Status: domain.TaskScheduled}, nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	wg := &sync.WaitGroup{}
	svc.StartScheduler(ctx, wg)

	time.Sleep(100 * time.Millisecond)
	cancel()
	time.Sleep(20 * time.Millisecond)
}

func TestStartScheduler_ScheduledError(t *testing.T) {
	svc, repo, _, _ := defaultSvc()
	repo.getScheduledFunc = func(_ context.Context, _ time.Time) ([]domain.Task, error) {
		return nil, errors.New("scheduler db error")
	}

	ctx, cancel := context.WithCancel(context.Background())
	wg := &sync.WaitGroup{}
	svc.StartScheduler(ctx, wg)

	time.Sleep(20 * time.Millisecond)
	cancel()
	time.Sleep(20 * time.Millisecond)
}

// ─────────────────────────────────────────────
// createMounts
// ─────────────────────────────────────────────

func TestCreateMounts(t *testing.T) {
	id := uuid.New()
	mounts := createMounts("/tmp", id)

	if len(mounts) != 2 {
		t.Fatalf("expected 2 mounts, got %d", len(mounts))
	}

	input := mounts[0]
	if input.Target != "/app/input" {
		t.Errorf("unexpected input target: %q", input.Target)
	}
	if !input.ReadOnly {
		t.Error("expected input mount to be read-only")
	}

	result := mounts[1]
	if result.Target != "/app/result" {
		t.Errorf("unexpected result target: %q", result.Target)
	}
	if result.ReadOnly {
		t.Error("expected result mount to be writable")
	}
}

// ─────────────────────────────────────────────
// Helpers
// ─────────────────────────────────────────────

func mustParseUUID(s string) uuid.UUID {
	id, err := uuid.Parse(s)
	if err != nil {
		panic(err)
	}
	return id
}
