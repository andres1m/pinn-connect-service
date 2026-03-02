package service

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"pinn/internal/config"
	"pinn/internal/domain"

	"github.com/google/uuid"
)

type ContainerManager interface {
	StartContainer(ctx context.Context, image string, config *domain.ContainerConfig) (string, error)
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
	GetRunningTasksContainers(ctx context.Context) ([]domain.RunningTasksContainer, error)
}

//TODO workspace interface

type TaskService struct {
	manager    ContainerManager
	config     *config.Config
	storage    ArtifactStorage
	repository TaskRepository
}

func NewTaskService(manager ContainerManager, storage ArtifactStorage, config *config.Config, repository TaskRepository) *TaskService {
	return &TaskService{
		manager:    manager,
		storage:    storage,
		config:     config,
		repository: repository,
	}
}

func (s *TaskService) CreateTask(ctx context.Context, task *domain.Task) error {
	// TODO impl
	// mark as queued
	// maybe something more
	return nil
}

// worker gouroutine should use it
func (s *TaskService) ProcessTask(ctx context.Context, task *domain.Task) error {
	//TODO impl
	// mark as running
	// start and wait container
	// check container status
	// upload result to storage
	// mark as completed
	// cleanup workspace
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

	if err := s.prepareDirs(taskID); err != nil {
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

	containerID, err := s.manager.StartContainer(ctx, "python:3.9-slim", cfg)
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

func (s *TaskService) prepareDirs(taskID uuid.UUID) error {
	mainPath := filepath.Join(s.config.TmpDir, taskID.String())

	if err := createDir(mainPath, "data"); err != nil {
		return fmt.Errorf("creating data dir: %w", err)
	}

	if err := createDir(mainPath, "input"); err != nil {
		return fmt.Errorf("creating input dir: %w", err)
	}

	if err := createDir(mainPath, "result"); err != nil {
		return fmt.Errorf("creating result dir: %w", err)
	}

	return nil
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

func createDir(mainPath string, dir string) error {
	return os.MkdirAll(filepath.Join(mainPath, dir), 0755)
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
