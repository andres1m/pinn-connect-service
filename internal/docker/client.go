package docker

import (
	"context"
	"fmt"
	"io"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
)

type Manager struct {
	Client *client.Client
}

func (m *Manager) StartContainer(ctx context.Context, image string, options ...RunOption) (container_id string, err error) {
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
	}

	res, err := m.Client.ContainerCreate(ctx, config, hostConfig, nil, nil, "")
	if err != nil {
		return res.ID, fmt.Errorf("creating container: %w", err)
	}

	err = m.Client.ContainerStart(ctx, res.ID, container.StartOptions{})
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

func NewManager() (*Manager, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())

	if err != nil {
		return nil, err
	}

	return &Manager{Client: cli}, nil
}
