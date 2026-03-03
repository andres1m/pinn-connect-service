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
	defer s.workspace.Cleanup(task.ID)

	// start and wait container
	containerID, err := s.manager.StartContainer(ctx, &domain.ContainerConfig{
		Image:  task.ContainerImage,
		Mounts: *createMounts(s.config.TmpDir, task.ID),
	})

	task.ContainerID = containerID

	// mark as running
	err = s.repository.MarkTaskRunning(ctx, task)
	if err != nil {
		return fmt.Errorf("marking task running: %w", err)
	}

	if err != nil {
		return fmt.Errorf("starting container: %w", err)
	}

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
	entries, err := os.ReadDir(s.workspace.ResultDir(task.ID))
	if err != nil {
		return fmt.Errorf("reading result dir: %w", err)
	}

	var resultFileName string
	for _, entry := range entries {
		if !entry.IsDir() {
			resultFileName = entry.Name()
			break
		}
	}

	if resultFileName == "" {
		return fmt.Errorf("no result file found in directory")
	}

	resultFilePath := filepath.Join(s.workspace.ResultDir(task.ID), resultFileName)

	file, err := os.Open(resultFilePath)
	if err != nil {
		return fmt.Errorf("opening result file: %w", err)
	}
	defer file.Close()

	stat, err := file.Stat()
	if err != nil {
		return fmt.Errorf("getting file stat: %w", err)
	}

	objectKey := fmt.Sprintf("tasks/%s/%s", task.ID, resultFileName)
	_, err = s.storage.Upload(ctx, objectKey, file, stat.Size())
	if err != nil {
		return fmt.Errorf("saving to S3 storage: %w", err)
	}

	task.ResultPath = objectKey

	// mark as completed
	err = s.repository.MarkTaskCompleted(ctx, task)
	if err != nil {
		return fmt.Errorf("marking task completed: %w", err)
	}

	err = s.manager.RemoveContainer(ctx, task.ContainerID)
	if err != nil {
		return fmt.Errorf("removing container: %w", err)
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
	taskID := uuid.New()

	if err := s.workspace.Prepare(taskID); err != nil {
		return "", fmt.Errorf("preparing dirs for mock run: %w", err)
	}

	if err := s.copyFromMock(taskID.String()); err != nil {
		return "", fmt.Errorf("copying from mock: %w", err)
	}

	tmpdir := s.config.TmpDir

	cfg := &domain.ContainerConfig{
		Image: "python:3.9-slim",
		Mounts: []domain.Mount{
			{
				Source:   filepath.Join(tmpdir, taskID.String(), "data"),
				Target:   "/app/data",
				ReadOnly: true,
			},
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
		},
		Envs: []string{
			"DATA_DIR=/app/data",
			"RESULT_DIR=/app/result",
			"INPUT_DIR=/app/input",
		},
		Cmd: []string{"python3", "/app/data/main.py"},
	}

	containerID, err := s.manager.StartContainer(ctx, cfg)
	if err != nil {
		return "", fmt.Errorf("starting container: %w", err)
	}

	defer s.manager.RemoveContainer(context.WithoutCancel(ctx), containerID)

	status, err := s.manager.WaitContainer(ctx, containerID)
	if err != nil {
		return "", fmt.Errorf("waiting container: %w", err)
	}

	if status != 0 {
		slog.Warn("Mock container exited with not zero status", "code", status)
	}

	resultDirPath := filepath.Join(tmpdir, taskID.String(), "result")

	entries, err := os.ReadDir(resultDirPath)
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

	resultFilePath := filepath.Join(resultDirPath, resultFileName)

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
		return "", fmt.Errorf("uploading artifact to storage: %w", err)
	}

	if err := os.RemoveAll(filepath.Join(tmpdir, taskID.String())); err != nil {
		slog.Error("Failed to cleanup task directory", "taskID", taskID, "error", err)
	}

	return objectKey, nil
}

func (s *TaskService) copyFromMock(taskID string) error {
	tmpdir := s.config.TmpDir
	mockDir := s.config.MockDir

	err := copyFile(filepath.Join(mockDir, "data.json"), filepath.Join(tmpdir, taskID, "data", "data.json"))
	if err != nil {
		return fmt.Errorf("copying from mock data: %w", err)
	}

	err = copyFile(filepath.Join(mockDir, "main.py"), filepath.Join(tmpdir, taskID, "data", "main.py"))
	if err != nil {
		return fmt.Errorf("copying from mock data: %w", err)
	}

	err = copyFile(filepath.Join(mockDir, "input.json"), filepath.Join(tmpdir, taskID, "input", "input.json"))
	if err != nil {
		return fmt.Errorf("copying from mock input: %w", err)
	}

	return nil
}

func copyFile(src, dst string) error {
	sourceFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer sourceFile.Close()

	sourceInfo, err := sourceFile.Stat()
	if err != nil {
		return err
	}

	destFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer destFile.Close()

	_, err = io.Copy(destFile, sourceFile)
	if err != nil {
		return err
	}

	err = os.Chmod(dst, sourceInfo.Mode())
	if err != nil {
		return err
	}

	return nil
}

func createMounts(tmpdir string, taskID uuid.UUID) *[]domain.Mount {
	return &[]domain.Mount{
		{
			Source:   filepath.Join(tmpdir, taskID.String(), "data"),
			Target:   "/app/data",
			ReadOnly: true,
		},
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
