package docker

import (
	"context"
	"fmt"
	"io"
	"log"
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

func (m *Manager) StartContainer(ctx context.Context, image string, options ...RunOption) (container_id string, err error) {
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
		return res.ID, fmt.Errorf("creating container: %w", err)
	}

	err = m.Client.ContainerStart(ctx, res.ID, container.StartOptions{})

	if err != nil && opts.GPU && strings.Contains(err.Error(), "could not select device driver") {
		slog.Warn("GPU runtime failed at start. Retrying without GPU...", "error", err)
		m.RemoveContainer(ctx, res.ID)

		hostConfig.DeviceRequests = nil
		m.hasGPU = false

		res, err = m.Client.ContainerCreate(ctx, config, hostConfig, nil, nil, "")
		if err != nil {
			return res.ID, fmt.Errorf("creating container: %w", err)
		}

		err = m.Client.ContainerStart(ctx, res.ID, container.StartOptions{})
	}
	if err != nil {
		return res.ID, fmt.Errorf("starting container: %w", err)
	}

	return res.ID, nil
}

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
		return fmt.Errorf("pulling image: %w", err)
	}

	if exists {
		return nil
	}

	rc, err := m.Client.ImagePull(ctx, img, image.PullOptions{})
	if err != nil {
		return fmt.Errorf("pulling image: %w", err)
	}

	defer rc.Close()

	io.Copy(os.Stdout, rc)

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

func (m *Manager) setGPUSupport(ctx context.Context) {
	hasGPU, err := m.hasGPUSupport(ctx)
	if err != nil {
		log.Printf("Error checking GPU support: %v. Proceeding without СPU.", err)
		m.hasGPU = false
		return
	}

	m.hasGPU = hasGPU
}
