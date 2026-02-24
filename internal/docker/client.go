package docker

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"pinn/internal/domain"
	"strings"

	"github.com/containerd/errdefs"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/mount"
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
func (m *Manager) StartContainer(ctx context.Context, image string, cfg *domain.ContainerConfig) (string, error) {
	if err := m.pullImage(ctx, image); err != nil {
		return "", fmt.Errorf("pre-pulling image: %w", err)
	}

	config := &container.Config{
		Image: image,
		Env:   cfg.Envs,
		Cmd:   cfg.Cmd,
	}

	hostConfig := &container.HostConfig{
		Mounts: mapMounts(cfg),
		Resources: container.Resources{
			Memory:   int64(cfg.MemoryLimit) * 1024 * 1024,
			NanoCPUs: int64(float64(cfg.CPULimit) * 1e7),
		},
	}

	if cfg.GPU {
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

	if err != nil && cfg.GPU && isGPUDriverError(err) {
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

// WaitContainer blocks goroutine and waiting for container finished his job.
// Returns status code and an error if occurs.
func (m *Manager) WaitContainer(ctx context.Context, containerID string) (int64, error) {
	statusCh, errorCh := m.Client.ContainerWait(ctx, containerID, container.WaitConditionNotRunning)

	select {
	case err := <-errorCh:
		if err != nil {
			return 0, fmt.Errorf("waiting container: %w", err)
		}

	case res := <-statusCh:
		if res.Error != nil {
			return 0, fmt.Errorf("waiting container: container exit error: %s", res.Error.Message)
		}
		return res.StatusCode, nil

	case <-ctx.Done():
		return 0, ctx.Err()
	}

	return 0, nil
}

// InspectContainer returns the container information.
func (m *Manager) InspectContainer(ctx context.Context, containerID string) (*domain.ContainerStateResponse, error) {
	result, err := m.Client.ContainerInspect(ctx, containerID)
	if err != nil {
		return nil, fmt.Errorf("inspecting container: %w", err)
	}

	state := result.State

	return &domain.ContainerStateResponse{
		Status:     state.Status,
		ExitCode:   state.ExitCode,
		StartedAt:  state.StartedAt,
		FinishedAt: state.FinishedAt,
		Running:    state.Running,
		OOMKilled:  state.OOMKilled,
		Error:      state.Error,
	}, nil
}

func (m *Manager) CheckStatus(ctx context.Context) error {
	_, err := m.Client.Ping(ctx)
	return err
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

// hasGPUSupport check if current docker client has gpu support (nvidia only)
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

func mapMounts(cfg *domain.ContainerConfig) []mount.Mount {
	var dockerMounts []mount.Mount
	for _, mnt := range cfg.Mounts {
		dockerMounts = append(dockerMounts, mount.Mount{
			Type:     mount.TypeBind,
			Source:   mnt.Source,
			Target:   mnt.Target,
			ReadOnly: mnt.ReadOnly,
		})
	}

	return dockerMounts
}
