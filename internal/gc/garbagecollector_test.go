package gc

import (
	"context"
	"pinn-connect-service/internal/domain"
	"testing"

	"github.com/google/uuid"
)

// --- MOCKS ---

type mockRepo struct {
	getRunningTasksFunc func(ctx context.Context) ([]*domain.Task, error)
	getActiveTasksFunc  func(ctx context.Context) ([]*domain.Task, error)
	markFunc            func(ctx context.Context, task *domain.Task, status domain.TaskStatus) error
}

func (m *mockRepo) GetRunningTasks(ctx context.Context) ([]*domain.Task, error) {
	if m.getRunningTasksFunc != nil {
		return m.getRunningTasksFunc(ctx)
	}
	return nil, nil
}
func (m *mockRepo) GetActiveTasks(ctx context.Context) ([]*domain.Task, error) {
	if m.getActiveTasksFunc != nil {
		return m.getActiveTasksFunc(ctx)
	}
	return nil, nil
}
func (m *mockRepo) Mark(ctx context.Context, task *domain.Task, status domain.TaskStatus) error {
	if m.markFunc != nil {
		return m.markFunc(ctx, task, status)
	}
	return nil
}

type mockWorkspace struct {
	cleanupFunc   func(taskID uuid.UUID) error
	resultDirFunc func(taskID uuid.UUID) string
}

func (m *mockWorkspace) Cleanup(taskID uuid.UUID) error {
	if m.cleanupFunc != nil {
		return m.cleanupFunc(taskID)
	}
	return nil
}
func (m *mockWorkspace) ResultDir(taskID uuid.UUID) string {
	if m.resultDirFunc != nil {
		return m.resultDirFunc(taskID)
	}
	return ""
}

type mockContainerManager struct {
	isContainerExistsFunc     func(ctx context.Context, id string) (bool, error)
	getContainerStateFunc     func(ctx context.Context, id string) (*domain.ContainerState, error)
	removeContainerFunc       func(ctx context.Context, id string) error
	listManagedContainersFunc func(ctx context.Context) ([]*domain.Container, error)
}

func (m *mockContainerManager) IsContainerExists(ctx context.Context, id string) (bool, error) {
	if m.isContainerExistsFunc != nil {
		return m.isContainerExistsFunc(ctx, id)
	}
	return false, nil
}
func (m *mockContainerManager) GetContainerState(ctx context.Context, id string) (*domain.ContainerState, error) {
	if m.getContainerStateFunc != nil {
		return m.getContainerStateFunc(ctx, id)
	}
	return nil, nil
}
func (m *mockContainerManager) RemoveContainer(ctx context.Context, id string) error {
	if m.removeContainerFunc != nil {
		return m.removeContainerFunc(ctx, id)
	}
	return nil
}
func (m *mockContainerManager) ListManagedContainers(ctx context.Context) ([]*domain.Container, error) {
	if m.listManagedContainersFunc != nil {
		return m.listManagedContainersFunc(ctx)
	}
	return nil, nil
}

type mockStorage struct {
	uploadToStorageFunc func(ctx context.Context, taskID uuid.UUID, resultDir string) (string, error)
}

func (m *mockStorage) UploadToStorage(ctx context.Context, taskID uuid.UUID, resultDir string) (string, error) {
	if m.uploadToStorageFunc != nil {
		return m.uploadToStorageFunc(ctx, taskID, resultDir)
	}
	return "", nil
}

type mockTaskService struct {
	recoverTaskFunc func(task *domain.Task)
}

func (m *mockTaskService) RecoverTask(task *domain.Task) {
	if m.recoverTaskFunc != nil {
		m.recoverTaskFunc(task)
	}
}

// --- TESTS ---

func TestGarbageCollector_Cleanup_DeadTask(t *testing.T) {
	taskID := uuid.New()
	task := &domain.Task{ID: taskID, ContainerID: "dead-cont", Status: domain.TaskRunning}
	
	repo := &mockRepo{
		getRunningTasksFunc: func(ctx context.Context) ([]*domain.Task, error) { return []*domain.Task{task}, nil },
		markFunc: func(ctx context.Context, task *domain.Task, status domain.TaskStatus) error {
			if status != domain.TaskQueued {
				t.Errorf("expected task to be requeued (Queued), got %v", status)
			}
			return nil
		},
	}
	
	cm := &mockContainerManager{
		isContainerExistsFunc: func(ctx context.Context, id string) (bool, error) { return false, nil },
	}

	gc := NewGarbageCollector(repo, &mockWorkspace{}, cm, &mockStorage{}, &mockTaskService{})
	gc.Cleanup(context.Background())
}

func TestGarbageCollector_Cleanup_RecoverTask(t *testing.T) {
	taskID := uuid.New()
	task := &domain.Task{ID: taskID, ContainerID: "live-cont", Status: domain.TaskRunning}
	
	repo := &mockRepo{
		getRunningTasksFunc: func(ctx context.Context) ([]*domain.Task, error) { return []*domain.Task{task}, nil },
	}
	
	cm := &mockContainerManager{
		isContainerExistsFunc: func(ctx context.Context, id string) (bool, error) { return true, nil },
		getContainerStateFunc: func(ctx context.Context, id string) (*domain.ContainerState, error) {
			return &domain.ContainerState{Running: true}, nil
		},
	}

	recovered := false
	ts := &mockTaskService{
		recoverTaskFunc: func(t *domain.Task) {
			if t.ID == taskID {
				recovered = true
			}
		},
	}

	gc := NewGarbageCollector(repo, &mockWorkspace{}, cm, &mockStorage{}, ts)
	gc.Cleanup(context.Background())

	if !recovered {
		t.Error("task should have been sent to RecoverTask")
	}
}

func TestGarbageCollector_Cleanup_Orphans(t *testing.T) {
	orphanTaskID := uuid.New()
	repo := &mockRepo{
		getRunningTasksFunc: func(ctx context.Context) ([]*domain.Task, error) { return nil, nil },
		getActiveTasksFunc:  func(ctx context.Context) ([]*domain.Task, error) { return []*domain.Task{}, nil },
	}

	containerRemoved := false
	cm := &mockContainerManager{
		listManagedContainersFunc: func(ctx context.Context) ([]*domain.Container, error) {
			return []*domain.Container{
				{ID: "orphan-id", Labels: map[string]string{"pinn.task_id": orphanTaskID.String()}},
			}, nil
		},
		removeContainerFunc: func(ctx context.Context, id string) error {
			if id == "orphan-id" {
				containerRemoved = true
			}
			return nil
		},
	}

	workspaceCleaned := false
	ws := &mockWorkspace{
		cleanupFunc: func(id uuid.UUID) error {
			if id == orphanTaskID {
				workspaceCleaned = true
			}
			return nil
		},
	}

	gc := NewGarbageCollector(repo, ws, cm, &mockStorage{}, &mockTaskService{})
	gc.Cleanup(context.Background())

	if !containerRemoved {
		t.Error("orphan container was not removed")
	}
	if !workspaceCleaned {
		t.Error("orphan workspace was not cleaned up")
	}
}
