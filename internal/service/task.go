package service

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"pinn/internal/config"
	"pinn/internal/docker"

	"github.com/docker/docker/api/types/mount"
)

type TaskService struct {
	dockerManager *docker.Manager
	config        *config.Config
}

func NewTaskService(dockerManager *docker.Manager, config *config.Config) *TaskService {
	return &TaskService{
		dockerManager: dockerManager,
		config:        config,
	}
}

func (s *TaskService) RunMock(ctx context.Context, taskID string) (string, error) {
	if err := s.prepareDirs(taskID); err != nil {
		return "", fmt.Errorf("preparing dirs for mock run: %w", err)
	}

	if err := s.copyFromMock(taskID); err != nil {
		return "", fmt.Errorf("copying from mock: %w", err)
	}

	tmpdir := s.config.TmpDir

	containerID, err := s.dockerManager.StartContainer(ctx, "python:3.9-slim",
		docker.WithMounts(
			mount.Mount{
				Type:     mount.TypeBind,
				Source:   filepath.Join(tmpdir, taskID, "data"),
				Target:   "/app/data",
				ReadOnly: true,
			},
			mount.Mount{
				Type:     mount.TypeBind,
				Source:   filepath.Join(tmpdir, taskID, "input"),
				Target:   "/app/input",
				ReadOnly: true,
			},
			mount.Mount{
				Type:     mount.TypeBind,
				Source:   filepath.Join(tmpdir, taskID, "result"),
				Target:   "/app/result",
				ReadOnly: false,
			},
		),

		docker.WithEnvs(
			"DATA_DIR=/app/data",
			"RESULT_DIR=/app/result",
			"INPUT_DIR=/app/input"),

		docker.WithCmds([]string{"python3", "/app/data/main.py"}),
	)

	if err != nil {
		return "", fmt.Errorf("starting container: %w", err)
	}

	return containerID, nil
}

func (s *TaskService) prepareDirs(taskID string) error {
	mainPath := filepath.Join(s.config.TmpDir, taskID)

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
