package docker

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"

	"github.com/containerd/errdefs"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/client"
)

type Manager struct {
	Client *client.Client
	hasGPU bool
}

// NewManager create and return new docker manager.
func NewManager(ctx context.Context) (*Manager, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())

	if err != nil {
		return nil, err
	}

	manager := &Manager{
		Client: cli,
		hasGPU: false,
	}

	manager.setGPUSupport(ctx)

	return manager, nil
}

// StartContainer starts container with given options.
// It returns the container id and an error if the process fails.
func (m *Manager) StartContainer(ctx context.Context, image string, options ...RunOption) (string, error) {
	if err := m.pullImage(ctx, image); err != nil {
		return "", fmt.Errorf("pre-pulling image: %w", err)
	}

	opts := defaultRunOptions()
	for _, opt := range options {
		opt(opts)
	}

	config := &container.Config{
		Image: image,
		Env:   opts.Env,
		Cmd:   opts.Cmd,
	}

	hostConfig := &container.HostConfig{
		Binds: opts.Volumes,
		Resources: container.Resources{
			Memory:   int64(opts.MemoryLimit) * 1024 * 1024,
			NanoCPUs: int64(float64(opts.CPULimit) * 1e7),
		},
	}

	if opts.GPU {
		if m.hasGPU {
			hostConfig.DeviceRequests = []container.DeviceRequest{
				{
					Driver:       "nvidia",
					Count:        -1,
					Capabilities: [][]string{{"gpu"}},
				},
			}
		} else {
			slog.Warn("GPU requested but not supported", "fallback", "CPU", "hint", "install nvidia-container-toolkit")
		}
	}

	res, err := m.Client.ContainerCreate(ctx, config, hostConfig, nil, nil, "")
	if err != nil {
		return "", fmt.Errorf("creating container: %w", err)
	}

	containerID := res.ID

	err = m.Client.ContainerStart(ctx, containerID, container.StartOptions{})

	if err != nil && opts.GPU && isGPUDriverError(err) {
		slog.Warn("GPU runtime failed at start. Retrying without GPU...", "error", err)

		err = m.RemoveContainer(ctx, containerID)
		if err != nil {
			slog.Warn("Error removing garbage container.", "error", err)
		}

		hostConfig.DeviceRequests = nil
		m.hasGPU = false

		slog.Warn("GPU support disabled until service restart.")

		res, err = m.Client.ContainerCreate(ctx, config, hostConfig, nil, nil, "")
		if err != nil {
			return "", fmt.Errorf("creating container: %w", err)
		}

		containerID = res.ID

		err = m.Client.ContainerStart(ctx, containerID, container.StartOptions{})
	}
	if err != nil {
		slog.Error("failed to start container, cleaning up...", "id", containerID, "error", err)
		_ = m.RemoveContainer(ctx, containerID)
		return "", fmt.Errorf("starting container: %w", err)
	}

	return containerID, nil
}

// GetContainerLogs get container logs from docker client and returns io.ReadCloser.
func (m *Manager) GetContainerLogs(ctx context.Context, containerID string, follow bool) (io.ReadCloser, error) {
	opts := container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     follow,
		Tail:       "all",
	}

	result, err := m.Client.ContainerLogs(ctx, containerID, opts)
	if err != nil {
		return nil, fmt.Errorf("getting container logs: %w", err)
	}

	return result, nil
}

// RemoveContainer removes container with given id.
func (m *Manager) RemoveContainer(ctx context.Context, containerID string) error {
	err := m.Client.ContainerRemove(ctx, containerID, container.RemoveOptions{})
	if err != nil {
		return fmt.Errorf("removing container: %w", err)
	}

	return nil
}

func (m *Manager) pullImage(ctx context.Context, img string) error {
	exists, err := m.isImageExists(ctx, img)
	if err != nil {
		return fmt.Errorf("checking if image exists: %w", err)
	}

	if exists {
		return nil
	}

	rc, err := m.Client.ImagePull(ctx, img, image.PullOptions{})
	if err != nil {
		return fmt.Errorf("pulling image: %w", err)
	}

	defer rc.Close()
	_, err = io.Copy(os.Stdout, rc)
	if err != nil {
		return fmt.Errorf("pulling image: %w", err)
	}

	return nil
}

func (m *Manager) isImageExists(ctx context.Context, img string) (bool, error) {
	_, err := m.Client.ImageInspect(ctx, img)

	if err != nil {
		if errdefs.IsNotFound(err) {
			return false, nil
		}

		return false, fmt.Errorf("checking if image exists: %w", err)
	}

	return true, nil
}

// hasPGUSupport check if current docker client has gpu support (nvidia only)
func (m *Manager) hasGPUSupport(ctx context.Context) (bool, error) {
	info, err := m.Client.Info(ctx)
	if err != nil {
		return false, fmt.Errorf("checking GPU support: %w", err)
	}

	for name := range info.Runtimes {
		if name == "nvidia" {
			return true, nil
		}
	}

	return false, nil
}

// setGPUSupport check and set GPU support bool
func (m *Manager) setGPUSupport(ctx context.Context) {
	hasGPU, err := m.hasGPUSupport(ctx)
	if err != nil {
		slog.Warn("Error checking GPU support. Proceeding without GPU.", "error", err)
		m.hasGPU = false
		return
	}

	m.hasGPU = hasGPU
}

func isGPUDriverError(err error) bool {
	if err == nil {
		return false
	}

	// maybe only possible way to check this error
	return strings.Contains(err.Error(), "could not select device driver")
}
