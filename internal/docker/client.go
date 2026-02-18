package docker

import (
	"context"
	"fmt"
	"io"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
)

type RunOptions struct {
	Image   string
	Env     []string
	Volumes []string
	Cmd     []string
}

type Manager struct {
	Client *client.Client
}

func (m *Manager) StartContainer(ctx context.Context, options *RunOptions) (container_id string, err error) {
	if options == nil {
		//TODO error handling OR default values
		return "", fmt.Errorf("container run options are nil")
	}

	config := &container.Config{
		Image: options.Image,
		Env:   options.Env,
		Cmd:   options.Cmd,
	}

	hostConfig := &container.HostConfig{
		Binds: options.Volumes,
	}

	createOpts := client.ContainerCreateOptions{
		Config:     config,
		HostConfig: hostConfig,
	}

	res, err := m.Client.ContainerCreate(ctx, createOpts)
	if err != nil {
		return res.ID, fmt.Errorf("creating container: %w", err)
	}

	_, err = m.Client.ContainerStart(ctx, res.ID, client.ContainerStartOptions{})
	if err != nil {
		return res.ID, fmt.Errorf("starting container: %w", err)
	}

	return res.ID, nil
}

func (m *Manager) GetContainerLogs(ctx context.Context, containerID string, follow bool) (io.ReadCloser, error) {
	opts := client.ContainerLogsOptions{
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
	_, err := m.Client.ContainerRemove(ctx, containerID, client.ContainerRemoveOptions{})
	if err != nil {
		return fmt.Errorf("removing container: %w", err)
	}

	return nil
}

func NewManager() (*Manager, error) {
	cli, err := client.New(client.FromEnv, client.WithAPIVersionFromEnv())

	if err != nil {
		return nil, err
	}

	return &Manager{Client: cli}, nil
}
