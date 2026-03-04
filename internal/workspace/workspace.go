package workspace

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"pinn/internal/config"

	"github.com/google/uuid"
)

type LocalWorkspace struct {
	config *config.Config
}

func NewLocalWorkspace(config *config.Config) *LocalWorkspace {
	return &LocalWorkspace{config: config}
}

func (w *LocalWorkspace) Prepare(taskID uuid.UUID) error {
	mainPath := filepath.Join(w.config.TmpDir, taskID.String())

	if err := createDir(mainPath, "input"); err != nil {
		return fmt.Errorf("creating input dir: %w", err)
	}

	if err := createDir(mainPath, "result"); err != nil {
		return fmt.Errorf("creating result dir: %w", err)
	}

	return nil
}

func (w *LocalWorkspace) SaveInput(taskID uuid.UUID, filename string, r io.Reader) error {
	dirPath := w.InputDir(taskID)
	filePath := filepath.Join(dirPath, filename)

	file, err := os.Create(filePath)
	if err != nil {
		return fmt.Errorf("creating file: %w", err)
	}
	defer file.Close()

	_, err = io.Copy(file, r)
	if err != nil {
		return fmt.Errorf("copying file: %w", err)
	}

	return nil
}

func (w *LocalWorkspace) Cleanup(taskID uuid.UUID) error {
	err := os.RemoveAll(filepath.Join(w.config.TmpDir, taskID.String()))
	if err != nil {
		return fmt.Errorf("removing task workspace: %w", err)
	}

	return nil
}

func (w *LocalWorkspace) ResultDir(taskID uuid.UUID) string {
	return filepath.Join(w.config.TmpDir, taskID.String(), "result")
}

func (w *LocalWorkspace) InputDir(taskID uuid.UUID) string {
	return filepath.Join(w.config.TmpDir, taskID.String(), "input")
}

func createDir(mainPath string, dir string) error {
	return os.MkdirAll(filepath.Join(mainPath, dir), 0755)
}
