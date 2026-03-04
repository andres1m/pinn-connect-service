package service

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"pinn/internal/config"
	"pinn/internal/domain"

	"github.com/docker/docker/pkg/stdcopy"
	"github.com/google/uuid"
)

type ContainerManager interface {
	StartContainer(ctx context.Context, config *domain.ContainerConfig) (string, error)
	GetContainerLogs(ctx context.Context, containerID string, follow bool) (io.ReadCloser, error)
	RemoveContainer(ctx context.Context, containerID string) error
	WaitContainer(ctx context.Context, containerID string) (int64, error)
	InspectContainer(ctx context.Context, containerID string) (*domain.ContainerStateResponse, error)
}

type ArtifactStorage interface {
	Upload(ctx context.Context, objectKey string, r io.Reader, size int64) (string, error)
	GetDownloadURL(ctx context.Context, objectKey string) (string, error)
}

type TaskRepository interface {
	Create(context.Context, *domain.Task) error
	GetTaskById(context.Context, uuid.UUID) (*domain.Task, error)
	FindCachedTask(context.Context, *domain.Task) (string, error)
	MarkTaskRunning(context.Context, *domain.Task) error
	MarkTaskCompleted(context.Context, *domain.Task) error
	MarkTaskFailed(context.Context, *domain.Task) error
	MarkTaskQueued(context.Context, *domain.Task) error
	GetRunningTasksContainers(ctx context.Context) ([]domain.RunningTasksContainer, error)
}

type Workspace interface {
	Prepare(uuid.UUID) error
	ResultDir(taskID uuid.UUID) string
	Cleanup(taskID uuid.UUID) error
}

type TaskService struct {
	manager    ContainerManager
	config     *config.Config
	storage    ArtifactStorage
	repository TaskRepository
	workspace  Workspace
}

func NewTaskService(manager ContainerManager, storage ArtifactStorage, config *config.Config, repository TaskRepository, workspace Workspace) *TaskService {
	return &TaskService{
		manager:    manager,
		storage:    storage,
		config:     config,
		repository: repository,
		workspace:  workspace,
	}
}

func (s *TaskService) CreateTask(ctx context.Context, task *domain.Task) error {
	err := s.repository.MarkTaskQueued(ctx, task)
	if err != nil {
		return fmt.Errorf("marking task queued: %w", err)
	}

	return nil
}

// worker gouroutine should use it
func (s *TaskService) ProcessTask(ctx context.Context, task *domain.Task) error {
	var err error

	defer func() {
		if err := s.workspace.Cleanup(task.ID); err != nil {
			slog.Error("Error while cleanup workspace", "error", err)
		}
	}()

	// mark failed if err occurs while run
	defer func() {
		if err != nil {
			s.repository.MarkTaskFailed(ctx, task)
		}
	}()

	// start container
	containerID, err := s.manager.StartContainer(ctx, &domain.ContainerConfig{
		Image:  task.ContainerImage,
		Mounts: *createMounts(s.config.TmpDir, task.ID),
		Cmd:    task.ContainerCmd,
		Envs:   task.ContainerEnvs,
	})
	if err != nil {
		return fmt.Errorf("starting container: %w", err)
	}

	defer func() {
		if err := s.manager.RemoveContainer(ctx, task.ContainerID); err != nil {
			slog.Error("Error while removing container", "error", err)
		}
	}()

	task.ContainerID = containerID

	// mark as running
	err = s.repository.MarkTaskRunning(ctx, task)
	if err != nil {
		return fmt.Errorf("marking task running: %w", err)
	}

	// wait for container end
	exitCode, err := s.manager.WaitContainer(ctx, task.ContainerID)
	if err != nil {
		errlog, logerr := s.getErrLogs(ctx, task.ContainerID)
		if logerr != nil {
			return fmt.Errorf("container failed: %w (also error while getting container error logs: %w)", err, logerr)
		}

		task.ErrorLog = errlog

		return fmt.Errorf("container failed: %w, stderr logs: %s", err, errlog)
	}

	// check container status
	if exitCode != 0 {
		errlog, err := s.getErrLogs(ctx, task.ContainerID)
		if err != nil {
			return fmt.Errorf("getting container erorr logs: %w", err)
		}

		task.ErrorLog = errlog

		return fmt.Errorf("container exited with not null value: %s", errlog)
	}

	// upload result to storage

	resPath, err := s.uploadToStorage(ctx, task.ID)
	if err != nil {
		return fmt.Errorf("upload to storage: %w", err)
	}

	task.ResultPath = resPath

	// mark as completed
	err = s.repository.MarkTaskCompleted(ctx, task)
	if err != nil {
		return fmt.Errorf("marking task completed: %w", err)
	}

	return nil
}

func (s *TaskService) GetTask(ctx context.Context, id uuid.UUID) (*domain.Task, error) {
	result, err := s.repository.GetTaskById(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("getting task from repo: %w", err)
	}

	return result, nil
}

func (s *TaskService) GetResultURL(ctx context.Context, id uuid.UUID) (string, error) {
	task, err := s.repository.GetTaskById(ctx, id)
	if err != nil {
		return "", fmt.Errorf("getting task from repo: %w", err)
	}

	if task.Status != domain.TaskComplete {
		return "", nil
	}

	result, err := s.storage.GetDownloadURL(ctx, task.ResultPath)
	if err != nil {
		return "", fmt.Errorf("getting storage download link: %w", err)
	}

	return result, nil
}

func (s *TaskService) RunMock(ctx context.Context) (string, error) {
	return "", nil
}

func createMounts(tmpdir string, taskID uuid.UUID) *[]domain.Mount {
	return &[]domain.Mount{
		{
			Source:   filepath.Join(tmpdir, taskID.String(), "input"),
			Target:   "/app/input",
			ReadOnly: true,
		},
		{
			Source:   filepath.Join(tmpdir, taskID.String(), "result"),
			Target:   "/app/result",
			ReadOnly: false,
		},
	}
}

func (s *TaskService) getErrLogs(ctx context.Context, containerID string) (string, error) {
	rc, err := s.manager.GetContainerLogs(ctx, containerID, false)
	if err != nil {
		return "", fmt.Errorf("fetching container err logs: %w", err)
	}
	defer rc.Close()

	var stdout, stderr bytes.Buffer

	_, err = stdcopy.StdCopy(&stdout, &stderr, rc)
	if err != nil {
		return "", fmt.Errorf("reading container err logs: %w", err)
	}

	return stderr.String(), nil
}

func (s *TaskService) uploadToStorage(ctx context.Context, taskID uuid.UUID) (string, error) {
	entries, err := os.ReadDir(s.workspace.ResultDir(taskID))
	if err != nil {
		return "", fmt.Errorf("reading result dir: %w", err)
	}

	var resultFileName string
	for _, entry := range entries {
		if !entry.IsDir() {
			resultFileName = entry.Name()
			break
		}
	}

	if resultFileName == "" {
		return "", fmt.Errorf("no result file found in directory")
	}

	resultFilePath := filepath.Join(s.workspace.ResultDir(taskID), resultFileName)

	file, err := os.Open(resultFilePath)
	if err != nil {
		return "", fmt.Errorf("opening result file: %w", err)
	}
	defer file.Close()

	stat, err := file.Stat()
	if err != nil {
		return "", fmt.Errorf("getting file stat: %w", err)
	}

	objectKey := fmt.Sprintf("tasks/%s/%s", taskID, resultFileName)
	_, err = s.storage.Upload(ctx, objectKey, file, stat.Size())
	if err != nil {
		return "", fmt.Errorf("saving to S3 storage: %w", err)
	}

	return objectKey, nil
}
