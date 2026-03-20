package docker

// Strategy: mock the Docker daemon with httptest.Server.
// The Docker client communicates over plain HTTP when given a tcp:// host,
// so httptest.NewServer (plain HTTP) is a perfect stand-in.
// All tests run in the same package, giving access to unexported fields/methods.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"pinn-connect-service/internal/domain"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/docker/docker/client"
	"github.com/google/uuid"
)

// ─────────────────────────────────────────────
// TEST INFRASTRUCTURE
// ─────────────────────────────────────────────

// stripVersion removes the /vX.XX/ API-version prefix so handlers can be
// registered without knowing which Docker API version the client negotiates.
//
//	/v1.41/containers/json  →  /containers/json
//	/_ping                  →  /_ping  (unchanged)
func stripVersion(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// path starts with /v<digit>
		if len(r.URL.Path) > 2 && r.URL.Path[1] == 'v' {
			if idx := strings.Index(r.URL.Path[2:], "/"); idx >= 0 {
				r.URL.Path = r.URL.Path[2+idx:]
			}
		}
		next.ServeHTTP(w, r)
	})
}

// newTestManager wires a Manager to an httptest.Server using the supplied mux.
func newTestManager(t *testing.T, mux *http.ServeMux) *Manager {
	t.Helper()
	srv := httptest.NewServer(stripVersion(mux))
	t.Cleanup(srv.Close)

	addr := strings.TrimPrefix(srv.URL, "http://")
	cli, err := client.NewClientWithOpts(
		client.WithHost("tcp://"+addr),
		client.WithHTTPClient(srv.Client()),
		client.WithVersion("1.41"),
	)
	if err != nil {
		t.Fatalf("creating docker client: %v", err)
	}
	return &Manager{Client: cli, hasGPU: false}
}

// jsonResp writes v as JSON with the given status code.
func jsonResp(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

// errResp writes a Docker-style error response.
func errResp(w http.ResponseWriter, code int, msg string) {
	jsonResp(w, code, map[string]string{"message": msg})
}

// makeContainerConfig returns a minimal valid ContainerConfig for tests.
func makeContainerConfig() *domain.ContainerConfig {
	return &domain.ContainerConfig{
		Image:  "myimg:latest",
		TaskID: uuid.New(),
		Cmd:    []string{"run"},
		Envs:   []string{"ENV=1"},
		Mounts: []domain.Mount{
			{Source: "/tmp/input", Target: "/app/input", ReadOnly: true},
		},
	}
}

// pingMux returns a mux that serves a Docker-compatible GET /_ping.
func pingMux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/_ping", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "OK")
	})
	return mux
}

// imageExistsMux adds a /images/json handler that always reports the image as present.
func imageExistsMux(mux *http.ServeMux) *http.ServeMux {
	mux.HandleFunc("/images/json", func(w http.ResponseWriter, _ *http.Request) {
		jsonResp(w, http.StatusOK, []map[string]any{{"Id": "sha256:abc"}})
	})
	return mux
}

// ─────────────────────────────────────────────
// PURE FUNCTIONS
// ─────────────────────────────────────────────

func TestIsGPUDriverError_Nil(t *testing.T) {
	if isGPUDriverError(nil) {
		t.Error("expected false for nil error")
	}
}

func TestIsGPUDriverError_GPUError(t *testing.T) {
	if !isGPUDriverError(errors.New("could not select device driver \"nvidia\"")) {
		t.Error("expected true for GPU driver error string")
	}
}

func TestIsGPUDriverError_OtherError(t *testing.T) {
	if isGPUDriverError(errors.New("container failed to start")) {
		t.Error("expected false for unrelated error")
	}
}

func TestDomainToDockerMounts_Empty(t *testing.T) {
	mounts := domainToDockerMounts(&domain.ContainerConfig{})
	if len(mounts) != 0 {
		t.Errorf("expected 0 mounts, got %d", len(mounts))
	}
}

func TestDomainToDockerMounts_Single_ReadOnly(t *testing.T) {
	cfg := &domain.ContainerConfig{
		Mounts: []domain.Mount{
			{Source: "/host/src", Target: "/ctr/dst", ReadOnly: true},
		},
	}
	mounts := domainToDockerMounts(cfg)
	if len(mounts) != 1 {
		t.Fatalf("expected 1 mount, got %d", len(mounts))
	}
	if mounts[0].Source != "/host/src" || mounts[0].Target != "/ctr/dst" || !mounts[0].ReadOnly {
		t.Errorf("mount fields not mapped correctly: %+v", mounts[0])
	}
}

func TestDomainToDockerMounts_Multiple(t *testing.T) {
	cfg := &domain.ContainerConfig{
		Mounts: []domain.Mount{
			{Source: "/a", Target: "/ta", ReadOnly: true},
			{Source: "/b", Target: "/tb", ReadOnly: false},
		},
	}
	mounts := domainToDockerMounts(cfg)
	if len(mounts) != 2 {
		t.Fatalf("expected 2 mounts, got %d", len(mounts))
	}
	if mounts[1].ReadOnly {
		t.Error("second mount should be writable")
	}
}

// ─────────────────────────────────────────────
// handleBuildOutput
// ─────────────────────────────────────────────

// flusherBuf implements io.Writer + http.Flusher.
type flusherBuf struct {
	bytes.Buffer
	flushCalled bool
}

func (f *flusherBuf) Flush() { f.flushCalled = true }

func TestHandleBuildOutput_SuccessStream(t *testing.T) {
	input := `{"stream":"Step 1/2 : FROM scratch\n"}` + "\n" +
		`{"stream":"Successfully built abc\n"}` + "\n"

	var out bytes.Buffer
	m := &Manager{}
	if err := m.handleBuildOutput(strings.NewReader(input), &out); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out.String(), "Step 1/2") {
		t.Errorf("expected build output in writer, got %q", out.String())
	}
}

func TestHandleBuildOutput_ErrorLine(t *testing.T) {
	input := `{"error":"build failed","errorDetail":{"message":"no space"}}` + "\n"
	m := &Manager{}
	if err := m.handleBuildOutput(strings.NewReader(input), io.Discard); err == nil {
		t.Fatal("expected error from error JSON line, got nil")
	}
}

func TestHandleBuildOutput_NilWriter_NoStream(t *testing.T) {
	input := `{"stream":""}` + "\n"
	m := &Manager{}
	if err := m.handleBuildOutput(strings.NewReader(input), nil); err != nil {
		t.Fatalf("unexpected error with nil writer: %v", err)
	}
}

func TestHandleBuildOutput_WithNilWriter_HasStream(t *testing.T) {
	// Stream is non-empty but writer is nil — must not panic.
	input := `{"stream":"building...\n"}` + "\n"
	m := &Manager{}
	if err := m.handleBuildOutput(strings.NewReader(input), nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestHandleBuildOutput_WithFlusher(t *testing.T) {
	input := `{"stream":"step\n"}` + "\n"
	fw := &flusherBuf{}
	m := &Manager{}
	if err := m.handleBuildOutput(strings.NewReader(input), fw); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !fw.flushCalled {
		t.Error("expected Flush() to be called on flusher-writer")
	}
}

func TestHandleBuildOutput_DecodeError(t *testing.T) {
	m := &Manager{}
	if err := m.handleBuildOutput(strings.NewReader("{invalid"), io.Discard); err == nil {
		t.Fatal("expected JSON decode error, got nil")
	}
}

// ─────────────────────────────────────────────
// CheckStatus
// ─────────────────────────────────────────────

func TestCheckStatus_Success(t *testing.T) {
	m := newTestManager(t, pingMux())
	if err := m.CheckStatus(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCheckStatus_Error(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/_ping", func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "daemon unavailable", http.StatusInternalServerError)
	})
	m := newTestManager(t, mux)
	if err := m.CheckStatus(context.Background()); err == nil {
		t.Fatal("expected error, got nil")
	}
}

// ─────────────────────────────────────────────
// IsContainerExists
// ─────────────────────────────────────────────

func TestIsContainerExists_EmptyID(t *testing.T) {
	m := &Manager{} // no HTTP needed; error is returned early
	if _, err := m.IsContainerExists(context.Background(), ""); err == nil {
		t.Fatal("expected error for empty container ID")
	}
}

func TestIsContainerExists_Exists(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/containers/ctr-1/json", func(w http.ResponseWriter, _ *http.Request) {
		jsonResp(w, http.StatusOK, map[string]any{"Id": "ctr-1", "State": map[string]any{"Status": "running"}})
	})
	m := newTestManager(t, mux)
	exists, err := m.IsContainerExists(context.Background(), "ctr-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !exists {
		t.Error("expected container to exist")
	}
}

func TestIsContainerExists_NotFound(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/containers/ghost/json", func(w http.ResponseWriter, _ *http.Request) {
		errResp(w, http.StatusNotFound, "No such container: ghost")
	})
	m := newTestManager(t, mux)
	exists, err := m.IsContainerExists(context.Background(), "ghost")
	if err != nil {
		t.Fatalf("unexpected error for not-found: %v", err)
	}
	if exists {
		t.Error("expected container NOT to exist")
	}
}

func TestIsContainerExists_OtherError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/containers/ctr-err/json", func(w http.ResponseWriter, _ *http.Request) {
		errResp(w, http.StatusInternalServerError, "internal error")
	})
	m := newTestManager(t, mux)
	if _, err := m.IsContainerExists(context.Background(), "ctr-err"); err == nil {
		t.Fatal("expected error, got nil")
	}
}

// ─────────────────────────────────────────────
// GetContainerState
// ─────────────────────────────────────────────

func TestGetContainerState_Success(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/containers/ctr-1/json", func(w http.ResponseWriter, _ *http.Request) {
		jsonResp(w, http.StatusOK, map[string]any{
			"Id": "ctr-1",
			"State": map[string]any{
				"Status":     "running",
				"Running":    true,
				"ExitCode":   0,
				"StartedAt":  "2023-01-01T00:00:00Z",
				"FinishedAt": "0001-01-01T00:00:00Z",
				"OOMKilled":  false,
				"Error":      "",
			},
		})
	})
	m := newTestManager(t, mux)
	state, err := m.GetContainerState(context.Background(), "ctr-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if state.Status != "running" || !state.Running {
		t.Errorf("unexpected state: %+v", state)
	}
}

func TestGetContainerState_Error(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/containers/bad/json", func(w http.ResponseWriter, _ *http.Request) {
		errResp(w, http.StatusNotFound, "No such container")
	})
	m := newTestManager(t, mux)
	if _, err := m.GetContainerState(context.Background(), "bad"); err == nil {
		t.Fatal("expected error, got nil")
	}
}

// ─────────────────────────────────────────────
// GetContainerLogs
// ─────────────────────────────────────────────

func TestGetContainerLogs_Success(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/containers/ctr-1/logs", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "log data")
	})
	m := newTestManager(t, mux)
	rc, err := m.GetContainerLogs(context.Background(), "ctr-1", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	rc.Close()
}

func TestGetContainerLogs_WithFollow(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/containers/ctr-1/logs", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("follow") != "1" {
			http.Error(w, "expected follow=1", http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	m := newTestManager(t, mux)
	rc, err := m.GetContainerLogs(context.Background(), "ctr-1", true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	rc.Close()
}

func TestGetContainerLogs_Error(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/containers/bad/logs", func(w http.ResponseWriter, _ *http.Request) {
		errResp(w, http.StatusNotFound, "No such container")
	})
	m := newTestManager(t, mux)
	if _, err := m.GetContainerLogs(context.Background(), "bad", false); err == nil {
		t.Fatal("expected error, got nil")
	}
}

// ─────────────────────────────────────────────
// StopContainer
// ─────────────────────────────────────────────

func TestStopContainer_Success(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/containers/ctr-1/stop", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	m := newTestManager(t, mux)
	if err := m.StopContainer(context.Background(), "ctr-1", 5*time.Second); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestStopContainer_Error(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/containers/bad/stop", func(w http.ResponseWriter, _ *http.Request) {
		errResp(w, http.StatusInternalServerError, "cannot stop")
	})
	m := newTestManager(t, mux)
	if err := m.StopContainer(context.Background(), "bad", 5*time.Second); err == nil {
		t.Fatal("expected error, got nil")
	}
}

// ─────────────────────────────────────────────
// RemoveContainer
// ─────────────────────────────────────────────

func TestRemoveContainer_Success(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/containers/ctr-1", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusNoContent)
		}
	})
	m := newTestManager(t, mux)
	if err := m.RemoveContainer(context.Background(), "ctr-1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRemoveContainer_Error(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/containers/bad", func(w http.ResponseWriter, _ *http.Request) {
		errResp(w, http.StatusNotFound, "No such container")
	})
	m := newTestManager(t, mux)
	if err := m.RemoveContainer(context.Background(), "bad"); err == nil {
		t.Fatal("expected error, got nil")
	}
}

// ─────────────────────────────────────────────
// RemoveImage
// ─────────────────────────────────────────────

func TestRemoveImage_Success(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/images/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			jsonResp(w, http.StatusOK, []map[string]string{{"Deleted": "sha256:abc"}})
		}
	})
	m := newTestManager(t, mux)
	if err := m.RemoveImage(context.Background(), "myimg:latest"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRemoveImage_Error(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/images/", func(w http.ResponseWriter, _ *http.Request) {
		errResp(w, http.StatusNotFound, "No such image")
	})
	m := newTestManager(t, mux)
	if err := m.RemoveImage(context.Background(), "missing:latest"); err == nil {
		t.Fatal("expected error, got nil")
	}
}

// ─────────────────────────────────────────────
// ListManagedContainers
// ─────────────────────────────────────────────

func TestListManagedContainers_Success(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/containers/json", func(w http.ResponseWriter, _ *http.Request) {
		jsonResp(w, http.StatusOK, []map[string]any{
			{
				"Id":      "abc123",
				"Names":   []string{"/mycontainer"},
				"Image":   "ubuntu:latest",
				"ImageID": "sha256:abc",
				"Command": "/bin/bash",
				"Created": 1234567890,
				"Labels":  map[string]string{"pinn.managed": "true"},
				"Status":  "running",
			},
		})
	})
	m := newTestManager(t, mux)
	containers, err := m.ListManagedContainers(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(containers) != 1 {
		t.Fatalf("expected 1 container, got %d", len(containers))
	}
	if containers[0].ID != "abc123" {
		t.Errorf("expected ID 'abc123', got %q", containers[0].ID)
	}
}

func TestListManagedContainers_Empty(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/containers/json", func(w http.ResponseWriter, _ *http.Request) {
		jsonResp(w, http.StatusOK, []any{})
	})
	m := newTestManager(t, mux)
	containers, err := m.ListManagedContainers(context.Background())
	if err != nil || len(containers) != 0 {
		t.Fatalf("expected empty/nil, got %v / %v", containers, err)
	}
}

func TestListManagedContainers_Error(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/containers/json", func(w http.ResponseWriter, _ *http.Request) {
		errResp(w, http.StatusInternalServerError, "daemon error")
	})
	m := newTestManager(t, mux)
	if _, err := m.ListManagedContainers(context.Background()); err == nil {
		t.Fatal("expected error, got nil")
	}
}

// ─────────────────────────────────────────────
// isImageExists
// ─────────────────────────────────────────────

func TestIsImageExists_Exists(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/images/json", func(w http.ResponseWriter, _ *http.Request) {
		jsonResp(w, http.StatusOK, []map[string]any{{"Id": "sha256:abc"}})
	})
	m := newTestManager(t, mux)
	exists, err := m.isImageExists(context.Background(), "myimg:latest")
	if err != nil || !exists {
		t.Fatalf("expected exists=true/nil, got %v/%v", exists, err)
	}
}

func TestIsImageExists_NotExists(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/images/json", func(w http.ResponseWriter, _ *http.Request) {
		jsonResp(w, http.StatusOK, []any{})
	})
	m := newTestManager(t, mux)
	exists, err := m.isImageExists(context.Background(), "noimg:latest")
	if err != nil || exists {
		t.Fatalf("expected exists=false/nil, got %v/%v", exists, err)
	}
}

func TestIsImageExists_Error(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/images/json", func(w http.ResponseWriter, _ *http.Request) {
		errResp(w, http.StatusInternalServerError, "daemon error")
	})
	m := newTestManager(t, mux)
	if _, err := m.isImageExists(context.Background(), "anyimg:latest"); err == nil {
		t.Fatal("expected error, got nil")
	}
}

// ─────────────────────────────────────────────
// hasGPUSupport
// ─────────────────────────────────────────────

func TestHasGPUSupport_WithNvidia(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/info", func(w http.ResponseWriter, _ *http.Request) {
		jsonResp(w, http.StatusOK, map[string]any{
			"Runtimes": map[string]any{
				"nvidia": map[string]any{"path": "/usr/bin/nvidia-container-runtime"},
			},
		})
	})
	m := newTestManager(t, mux)
	has, err := m.hasGPUSupport(context.Background())
	if err != nil || !has {
		t.Fatalf("expected has=true/nil, got %v/%v", has, err)
	}
}

func TestHasGPUSupport_WithoutNvidia(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/info", func(w http.ResponseWriter, _ *http.Request) {
		jsonResp(w, http.StatusOK, map[string]any{
			"Runtimes": map[string]any{
				"runc": map[string]any{"path": "/usr/bin/runc"},
			},
		})
	})
	m := newTestManager(t, mux)
	has, err := m.hasGPUSupport(context.Background())
	if err != nil || has {
		t.Fatalf("expected has=false/nil, got %v/%v", has, err)
	}
}

func TestHasGPUSupport_Error(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/info", func(w http.ResponseWriter, _ *http.Request) {
		errResp(w, http.StatusInternalServerError, "daemon error")
	})
	m := newTestManager(t, mux)
	if _, err := m.hasGPUSupport(context.Background()); err == nil {
		t.Fatal("expected error, got nil")
	}
}

// ─────────────────────────────────────────────
// setGPUSupport
// ─────────────────────────────────────────────

func TestSetGPUSupport_GPUFound(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/info", func(w http.ResponseWriter, _ *http.Request) {
		jsonResp(w, http.StatusOK, map[string]any{
			"Runtimes": map[string]any{"nvidia": map[string]any{}},
		})
	})
	m := newTestManager(t, mux)
	m.setGPUSupport(context.Background())
	if !m.hasGPU {
		t.Error("expected hasGPU=true after detecting nvidia runtime")
	}
}

func TestSetGPUSupport_NoGPU(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/info", func(w http.ResponseWriter, _ *http.Request) {
		jsonResp(w, http.StatusOK, map[string]any{"Runtimes": map[string]any{}})
	})
	m := newTestManager(t, mux)
	m.hasGPU = true // preset to true to verify it gets cleared
	m.setGPUSupport(context.Background())
	if m.hasGPU {
		t.Error("expected hasGPU=false when nvidia runtime absent")
	}
}

func TestSetGPUSupport_ErrorLogsAndSetsToFalse(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/info", func(w http.ResponseWriter, _ *http.Request) {
		errResp(w, http.StatusInternalServerError, "daemon error")
	})
	m := newTestManager(t, mux)
	m.hasGPU = true
	m.setGPUSupport(context.Background()) // should log a warning and set hasGPU=false
	if m.hasGPU {
		t.Error("expected hasGPU=false on error from hasGPUSupport")
	}
}

// ─────────────────────────────────────────────
// WaitContainer
// ─────────────────────────────────────────────

func TestWaitContainer_Success_ExitCode0(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/containers/ctr-1/wait", func(w http.ResponseWriter, _ *http.Request) {
		jsonResp(w, http.StatusOK, map[string]any{"StatusCode": 0})
	})
	m := newTestManager(t, mux)
	code, err := m.WaitContainer(context.Background(), "ctr-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if code != 0 {
		t.Errorf("expected exit code 0, got %d", code)
	}
}

func TestWaitContainer_Success_NonZeroExitCode(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/containers/ctr-1/wait", func(w http.ResponseWriter, _ *http.Request) {
		jsonResp(w, http.StatusOK, map[string]any{"StatusCode": 1})
	})
	m := newTestManager(t, mux)
	code, err := m.WaitContainer(context.Background(), "ctr-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if code != 1 {
		t.Errorf("expected exit code 1, got %d", code)
	}
}

func TestWaitContainer_ContainerExitError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/containers/ctr-err/wait", func(w http.ResponseWriter, _ *http.Request) {
		jsonResp(w, http.StatusOK, map[string]any{
			"StatusCode": 0,
			"Error":      map[string]string{"Message": "container crashed"},
		})
	})
	m := newTestManager(t, mux)
	if _, err := m.WaitContainer(context.Background(), "ctr-err"); err == nil {
		t.Fatal("expected error from container exit error, got nil")
	}
}

func TestWaitContainer_APIError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/containers/bad/wait", func(w http.ResponseWriter, _ *http.Request) {
		errResp(w, http.StatusNotFound, "No such container")
	})
	m := newTestManager(t, mux)
	if _, err := m.WaitContainer(context.Background(), "bad"); err == nil {
		t.Fatal("expected error from API 404, got nil")
	}
}

func TestWaitContainer_ContextCanceled(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/containers/ctr-1/wait", func(w http.ResponseWriter, r *http.Request) {
		// stall until client disconnects
		<-r.Context().Done()
	})
	m := newTestManager(t, mux)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := m.WaitContainer(ctx, "ctr-1"); err == nil {
		t.Fatal("expected error from cancelled context, got nil")
	}
}

// ─────────────────────────────────────────────
// pullImage
// ─────────────────────────────────────────────

func TestPullImage_AlreadyExists_SkipsPull(t *testing.T) {
	pullCalled := false
	mux := http.NewServeMux()
	mux.HandleFunc("/images/json", func(w http.ResponseWriter, _ *http.Request) {
		jsonResp(w, http.StatusOK, []map[string]any{{"Id": "sha256:abc"}})
	})
	mux.HandleFunc("/images/create", func(w http.ResponseWriter, _ *http.Request) {
		pullCalled = true
	})
	m := newTestManager(t, mux)
	if err := m.pullImage(context.Background(), "myimg:latest"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pullCalled {
		t.Error("expected pull NOT to be called when image already exists")
	}
}

func TestPullImage_LocalImage_NotFound_Error(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/images/json", func(w http.ResponseWriter, _ *http.Request) {
		jsonResp(w, http.StatusOK, []any{}) // not found locally
	})
	m := newTestManager(t, mux)
	// "localimage" contains no "/" or "." → looks local → error without pulling
	if err := m.pullImage(context.Background(), "localimage"); err == nil {
		t.Fatal("expected error for local-looking image not found")
	}
}

func TestPullImage_RemotePull_Success(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/images/json", func(w http.ResponseWriter, _ *http.Request) {
		jsonResp(w, http.StatusOK, []any{})
	})
	mux.HandleFunc("/images/create", func(w http.ResponseWriter, _ *http.Request) {
		// Docker pull response is a stream of JSON lines
		jsonResp(w, http.StatusOK, map[string]string{"status": "pulling"})
	})
	m := newTestManager(t, mux)
	// "registry.example.com/img:latest" has "." → remote
	if err := m.pullImage(context.Background(), "registry.example.com/img:latest"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPullImage_RemotePull_WithSlash_Success(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/images/json", func(w http.ResponseWriter, _ *http.Request) {
		jsonResp(w, http.StatusOK, []any{})
	})
	mux.HandleFunc("/images/create", func(w http.ResponseWriter, _ *http.Request) {
		jsonResp(w, http.StatusOK, map[string]string{"status": "pulling"})
	})
	m := newTestManager(t, mux)
	// "myrepo/myimage" has "/" but no "." → still considered remote
	if err := m.pullImage(context.Background(), "myrepo/myimage:latest"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPullImage_ImageListError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/images/json", func(w http.ResponseWriter, _ *http.Request) {
		errResp(w, http.StatusInternalServerError, "daemon error")
	})
	m := newTestManager(t, mux)
	if err := m.pullImage(context.Background(), "registry.example.com/img:latest"); err == nil {
		t.Fatal("expected error from image list, got nil")
	}
}

func TestPullImage_PullError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/images/json", func(w http.ResponseWriter, _ *http.Request) {
		jsonResp(w, http.StatusOK, []any{})
	})
	mux.HandleFunc("/images/create", func(w http.ResponseWriter, _ *http.Request) {
		errResp(w, http.StatusInternalServerError, "unauthorized")
	})
	m := newTestManager(t, mux)
	if err := m.pullImage(context.Background(), "registry.example.com/img:latest"); err == nil {
		t.Fatal("expected pull error, got nil")
	}
}

// ─────────────────────────────────────────────
// BuildImage
// ─────────────────────────────────────────────

func buildStreamBody(lines ...string) string {
	return strings.Join(lines, "\n") + "\n"
}

func TestBuildImage_Success(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/build", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, buildStreamBody(
			`{"stream":"Step 1/1 : FROM scratch\n"}`,
			`{"stream":"Successfully built abc\n"}`,
		))
	})
	m := newTestManager(t, mux)
	var out bytes.Buffer
	if err := m.BuildImage(context.Background(), strings.NewReader("archive"), "myimg:latest", &out); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out.String(), "Step 1/1") {
		t.Errorf("expected build output in writer, got %q", out.String())
	}
}

func TestBuildImage_APIError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/build", func(w http.ResponseWriter, _ *http.Request) {
		errResp(w, http.StatusInternalServerError, "daemon error")
	})
	m := newTestManager(t, mux)
	if err := m.BuildImage(context.Background(), strings.NewReader("archive"), "myimg:latest", io.Discard); err == nil {
		t.Fatal("expected error from ImageBuild API call, got nil")
	}
}

func TestBuildImage_StreamError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/build", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"error":"Dockerfile parse error line 3"}`)
	})
	m := newTestManager(t, mux)
	if err := m.BuildImage(context.Background(), strings.NewReader("archive"), "myimg:latest", io.Discard); err == nil {
		t.Fatal("expected error from build stream error JSON, got nil")
	}
}

// ─────────────────────────────────────────────
// StartContainer
// ─────────────────────────────────────────────

func TestStartContainer_Success(t *testing.T) {
	mux := http.NewServeMux()
	imageExistsMux(mux)
	mux.HandleFunc("/containers/create", func(w http.ResponseWriter, _ *http.Request) {
		jsonResp(w, http.StatusCreated, map[string]any{"Id": "ctr-1", "Warnings": []string{}})
	})
	mux.HandleFunc("/containers/ctr-1/start", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	m := newTestManager(t, mux)
	id, err := m.StartContainer(context.Background(), makeContainerConfig())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != "ctr-1" {
		t.Errorf("expected container ID 'ctr-1', got %q", id)
	}
}

func TestStartContainer_PullImageFails(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/images/json", func(w http.ResponseWriter, _ *http.Request) {
		errResp(w, http.StatusInternalServerError, "daemon error")
	})
	m := newTestManager(t, mux)
	if _, err := m.StartContainer(context.Background(), makeContainerConfig()); err == nil {
		t.Fatal("expected error from pullImage, got nil")
	}
}

func TestStartContainer_ContainerCreateFails(t *testing.T) {
	mux := http.NewServeMux()
	imageExistsMux(mux)
	mux.HandleFunc("/containers/create", func(w http.ResponseWriter, _ *http.Request) {
		errResp(w, http.StatusInternalServerError, "create failed")
	})
	m := newTestManager(t, mux)
	if _, err := m.StartContainer(context.Background(), makeContainerConfig()); err == nil {
		t.Fatal("expected error from ContainerCreate, got nil")
	}
}

func TestStartContainer_StartFails_NonGPU(t *testing.T) {
	mux := http.NewServeMux()
	imageExistsMux(mux)
	mux.HandleFunc("/containers/create", func(w http.ResponseWriter, _ *http.Request) {
		jsonResp(w, http.StatusCreated, map[string]any{"Id": "ctr-1", "Warnings": []string{}})
	})
	mux.HandleFunc("/containers/ctr-1/start", func(w http.ResponseWriter, _ *http.Request) {
		errResp(w, http.StatusInternalServerError, "start failed")
	})
	mux.HandleFunc("/containers/ctr-1", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusNoContent)
		}
	})

	m := newTestManager(t, mux)
	if _, err := m.StartContainer(context.Background(), makeContainerConfig()); err == nil {
		t.Fatal("expected error from ContainerStart, got nil")
	}
}

func TestStartContainer_GPU_NotAvailable_SkipsDeviceRequest(t *testing.T) {
	// GPU requested but Manager.hasGPU=false → warning logged, GPU skipped, proceed normally.
	mux := http.NewServeMux()
	imageExistsMux(mux)
	mux.HandleFunc("/containers/create", func(w http.ResponseWriter, _ *http.Request) {
		jsonResp(w, http.StatusCreated, map[string]any{"Id": "ctr-1", "Warnings": []string{}})
	})
	mux.HandleFunc("/containers/ctr-1/start", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	cfg := makeContainerConfig()
	cfg.GPU = true
	m := newTestManager(t, mux)
	m.hasGPU = false // GPU not available

	id, err := m.StartContainer(context.Background(), cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id == "" {
		t.Error("expected a container ID")
	}
}

func TestStartContainer_GPUFallback_Success(t *testing.T) {
	// GPU enabled + GPU driver error on first start → remove + retry without GPU → success.
	var mu sync.Mutex
	createCount := 0

	mux := http.NewServeMux()
	imageExistsMux(mux)

	mux.HandleFunc("/containers/create", func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		createCount++
		id := fmt.Sprintf("ctr-%d", createCount)
		mu.Unlock()
		jsonResp(w, http.StatusCreated, map[string]any{"Id": id, "Warnings": []string{}})
	})
	mux.HandleFunc("/containers/ctr-1/start", func(w http.ResponseWriter, _ *http.Request) {
		errResp(w, http.StatusInternalServerError, "could not select device driver \"nvidia\"")
	})
	mux.HandleFunc("/containers/ctr-1", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusNoContent)
		}
	})
	mux.HandleFunc("/containers/ctr-2/start", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	cfg := makeContainerConfig()
	cfg.GPU = true
	m := newTestManager(t, mux)
	m.hasGPU = true

	id, err := m.StartContainer(context.Background(), cfg)
	if err != nil {
		t.Fatalf("unexpected error after GPU fallback: %v", err)
	}
	if id != "ctr-2" {
		t.Errorf("expected 'ctr-2' after GPU retry, got %q", id)
	}
	if m.hasGPU {
		t.Error("expected hasGPU to be disabled after GPU driver error")
	}
}

func TestStartContainer_GPUFallback_RemoveContainerFails_ContinuesRetry(t *testing.T) {
	// RemoveContainer in the fallback path fails → warning logged, retry continues.
	var mu sync.Mutex
	createCount := 0

	mux := http.NewServeMux()
	imageExistsMux(mux)
	mux.HandleFunc("/containers/create", func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		createCount++
		id := fmt.Sprintf("ctr-%d", createCount)
		mu.Unlock()
		jsonResp(w, http.StatusCreated, map[string]any{"Id": id, "Warnings": []string{}})
	})
	mux.HandleFunc("/containers/ctr-1/start", func(w http.ResponseWriter, _ *http.Request) {
		errResp(w, http.StatusInternalServerError, "could not select device driver")
	})
	mux.HandleFunc("/containers/ctr-1", func(w http.ResponseWriter, _ *http.Request) {
		// RemoveContainer fails → warning logged, fallback continues anyway
		errResp(w, http.StatusInternalServerError, "cannot remove")
	})
	mux.HandleFunc("/containers/ctr-2/start", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	cfg := makeContainerConfig()
	cfg.GPU = true
	m := newTestManager(t, mux)
	m.hasGPU = true

	id, err := m.StartContainer(context.Background(), cfg)
	if err != nil {
		t.Fatalf("unexpected error (remove failure should not abort retry): %v", err)
	}
	if id != "ctr-2" {
		t.Errorf("expected 'ctr-2', got %q", id)
	}
}

func TestStartContainer_GPUFallback_SecondCreateFails(t *testing.T) {
	var mu sync.Mutex
	createCount := 0

	mux := http.NewServeMux()
	imageExistsMux(mux)
	mux.HandleFunc("/containers/create", func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		createCount++
		call := createCount
		mu.Unlock()
		if call == 1 {
			jsonResp(w, http.StatusCreated, map[string]any{"Id": "ctr-1", "Warnings": []string{}})
		} else {
			errResp(w, http.StatusInternalServerError, "second create failed")
		}
	})
	mux.HandleFunc("/containers/ctr-1/start", func(w http.ResponseWriter, _ *http.Request) {
		errResp(w, http.StatusInternalServerError, "could not select device driver")
	})
	mux.HandleFunc("/containers/ctr-1", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusNoContent)
		}
	})

	cfg := makeContainerConfig()
	cfg.GPU = true
	m := newTestManager(t, mux)
	m.hasGPU = true

	if _, err := m.StartContainer(context.Background(), cfg); err == nil {
		t.Fatal("expected error from second ContainerCreate failure, got nil")
	}
}

func TestStartContainer_GPUFallback_SecondStartFails(t *testing.T) {
	var mu sync.Mutex
	createCount := 0

	mux := http.NewServeMux()
	imageExistsMux(mux)
	mux.HandleFunc("/containers/create", func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		createCount++
		id := fmt.Sprintf("ctr-%d", createCount)
		mu.Unlock()
		jsonResp(w, http.StatusCreated, map[string]any{"Id": id, "Warnings": []string{}})
	})
	mux.HandleFunc("/containers/ctr-1/start", func(w http.ResponseWriter, _ *http.Request) {
		errResp(w, http.StatusInternalServerError, "could not select device driver")
	})
	mux.HandleFunc("/containers/ctr-1", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusNoContent)
		}
	})
	mux.HandleFunc("/containers/ctr-2/start", func(w http.ResponseWriter, _ *http.Request) {
		errResp(w, http.StatusInternalServerError, "second start failed")
	})
	mux.HandleFunc("/containers/ctr-2", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusNoContent)
		}
	})

	cfg := makeContainerConfig()
	cfg.GPU = true
	m := newTestManager(t, mux)
	m.hasGPU = true

	if _, err := m.StartContainer(context.Background(), cfg); err == nil {
		t.Fatal("expected error from second ContainerStart failure, got nil")
	}
}
