package gc

import (
	"context"
	"log/slog"
	"pinn-connect-service/internal/domain"
	"sync"

	"github.com/google/uuid"
)

type Repository interface {
	GetRunningTasks(context.Context) ([]*domain.Task, error)
	GetActiveTasks(context.Context) ([]*domain.Task, error)
	Mark(context.Context, *domain.Task, domain.TaskStatus) error
}

type Workspace interface {
	Cleanup(taskID uuid.UUID) error
	ResultDir(taskID uuid.UUID) string
}

type ContainerManager interface {
	IsContainerExists(context.Context, string) (bool, error)
	GetContainerState(ctx context.Context, containerID string) (*domain.ContainerState, error)
	RemoveContainer(context.Context, string) error
	ListManagedContainers(context.Context) ([]*domain.Container, error)
}

type Storage interface {
	UploadToStorage(ctx context.Context, taskID uuid.UUID, resultDir string) (string, error)
}

type TaskService interface {
	RecoverTask(*domain.Task)
}

type GarbageCollector struct {
	repo             Repository
	workspace        Workspace
	containerManager ContainerManager
	storage          Storage
	taskService      TaskService
}

func NewGarbageCollector(repo Repository, workspace Workspace, containerManager ContainerManager,
	storage Storage, taskService TaskService) *GarbageCollector {
	return &GarbageCollector{
		repo:             repo,
		workspace:        workspace,
		containerManager: containerManager,
		storage:          storage,
		taskService:      taskService,
	}
}

// Cleanup starts a cleanup for dead running tasks.
// It is a blocking call, not async.
func (gc *GarbageCollector) Cleanup(ctx context.Context) {
	var wg sync.WaitGroup

	runningTasks, err := gc.repo.GetRunningTasks(ctx)
	if err != nil {
		slog.Error("gc: getting running task", "error", err)
		return
	}

	for _, task := range runningTasks {
		wg.Go(func() { gc.cleanupTask(ctx, task) })
	}

	gc.cleanupOrphans(ctx)

	wg.Wait()
}

func (gc *GarbageCollector) cleanupTask(ctx context.Context, task *domain.Task) {
	exists, err := gc.containerManager.IsContainerExists(ctx, task.ContainerID)
	if err != nil {
		slog.Error("gc: checking container exists", "error", err)
		return
	}

	if !exists {
		if err := gc.repo.Mark(ctx, task, domain.TaskQueued); err != nil {
			slog.Error("gc: marking task queued", "error", err)
		}
		return
	}

	status, err := gc.containerManager.GetContainerState(ctx, task.ContainerID)
	if err != nil {
		slog.Error("gc: checking container status", "error", err)
		return
	}

	if status.Error != "" || status.ExitCode != 0 || status.OOMKilled {
		if err := gc.containerManager.RemoveContainer(ctx, task.ContainerID); err != nil {
			slog.Error("gc: removing container", "error", err)
			return
		}

		if err := gc.repo.Mark(ctx, task, domain.TaskQueued); err != nil {
			slog.Error("gc: marking task queued", "error", err)
			return
		}
	} else if status.Running {
		gc.taskService.RecoverTask(task)
		return
	}

	resultPath, err := gc.storage.UploadToStorage(ctx, task.ID, gc.workspace.ResultDir(task.ID))
	if err != nil {
		slog.Error("gc: uploading to storage", "error", err)
		return
	}

	task.ResultPath = resultPath
	if err := gc.repo.Mark(ctx, task, domain.TaskCompleted); err != nil {
		slog.Error("gc: marking task completed", "error", err)
		return
	}
	if err := gc.workspace.Cleanup(task.ID); err != nil {
		slog.Error("gc: removing workspace", "error", err)
		return
	}
}

func (gc *GarbageCollector) cleanupOrphans(ctx context.Context) {
	dockerContainers, err := gc.containerManager.ListManagedContainers(ctx)
	if err != nil {
		slog.Error("gc: error getting list of managed containers", "error", err)
		return
	}

	activeTasks, err := gc.repo.GetActiveTasks(ctx)
	if err != nil {
		slog.Error("gc: error getting active tasks", "error", err)
		return
	}

	activeMap := make(map[string]bool)
	for _, task := range activeTasks {
		activeMap[task.ID.String()] = true
	}

	for _, dockerCont := range dockerContainers {
		taskID := dockerCont.Labels["pinn.task_id"]
		if !activeMap[taskID] {
			err := gc.containerManager.RemoveContainer(ctx, dockerCont.ID)
			if err != nil {
				slog.Error("gc: removing container", "error", err)
			}

			uuID, err := uuid.Parse(taskID)
			if err != nil {
				slog.Error("gc: parsing uuid", "error", err)
				return
			}

			err = gc.workspace.Cleanup(uuID)
			if err != nil {
				slog.Error("gc: cleaning up workspace", "error", err)
			}
		}
	}
}
