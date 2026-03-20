package workspace

import (
	"bytes"
	"os"
	"path/filepath"
	"pinn-connect-service/internal/config"
	"testing"

	"github.com/google/uuid"
)

func setupTestWorkspace(t *testing.T) (*LocalWorkspace, string) {
	tmpDir, err := os.MkdirTemp("", "workspace_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}

	cfg := &config.Config{
		TmpDir:            tmpDir,
		WorkspaceDirsPerm: 0755,
	}

	return NewLocalWorkspace(cfg), tmpDir
}

func TestLocalWorkspace_Prepare(t *testing.T) {
	ws, tmpDir := setupTestWorkspace(t)
	defer os.RemoveAll(tmpDir)

	taskID := uuid.New()

	if err := ws.Prepare(taskID); err != nil {
		t.Fatalf("Prepare() failed: %v", err)
	}

	inputDir := filepath.Join(tmpDir, taskID.String(), "input")
	resultDir := filepath.Join(tmpDir, taskID.String(), "result")

	if _, err := os.Stat(inputDir); os.IsNotExist(err) {
		t.Errorf("input directory was not created: %s", inputDir)
	}

	if _, err := os.Stat(resultDir); os.IsNotExist(err) {
		t.Errorf("result directory was not created: %s", resultDir)
	}
}

func TestLocalWorkspace_SaveInput(t *testing.T) {
	ws, tmpDir := setupTestWorkspace(t)
	defer os.RemoveAll(tmpDir)

	taskID := uuid.New()
	_ = ws.Prepare(taskID)

	filename := "test.txt"
	content := []byte("hello world")
	reader := bytes.NewReader(content)

	if err := ws.SaveInput(taskID, filename, reader); err != nil {
		t.Fatalf("SaveInput() failed: %v", err)
	}

	savedPath := filepath.Join(tmpDir, taskID.String(), "input", filename)
	savedContent, err := os.ReadFile(savedPath)
	if err != nil {
		t.Fatalf("failed to read saved file: %v", err)
	}

	if !bytes.Equal(savedContent, content) {
		t.Errorf("saved content mismatch: expected %s, got %s", content, savedContent)
	}
}

func TestLocalWorkspace_Cleanup(t *testing.T) {
	ws, tmpDir := setupTestWorkspace(t)
	defer os.RemoveAll(tmpDir)

	taskID := uuid.New()
	_ = ws.Prepare(taskID)

	if err := ws.Cleanup(taskID); err != nil {
		t.Fatalf("Cleanup() failed: %v", err)
	}

	taskPath := filepath.Join(tmpDir, taskID.String())
	if _, err := os.Stat(taskPath); !os.IsNotExist(err) {
		t.Errorf("task directory still exists after cleanup: %s", taskPath)
	}
}

func TestLocalWorkspace_Paths(t *testing.T) {
	ws, tmpDir := setupTestWorkspace(t)
	defer os.RemoveAll(tmpDir)

	taskID := uuid.New()
	expectedInput := filepath.Join(tmpDir, taskID.String(), "input")
	expectedResult := filepath.Join(tmpDir, taskID.String(), "result")

	if ws.InputDir(taskID) != expectedInput {
		t.Errorf("InputDir() mismatch: expected %s, got %s", expectedInput, ws.InputDir(taskID))
	}

	if ws.ResultDir(taskID) != expectedResult {
		t.Errorf("ResultDir() mismatch: expected %s, got %s", expectedResult, ws.ResultDir(taskID))
	}
}
