package domain

import "github.com/google/uuid"

type Mount struct {
	Source   string
	Target   string
	ReadOnly bool
}

type ContainerConfig struct {
	Image       string
	Mounts      []Mount
	Envs        []string
	Cmd         []string
	MemoryLimit int
	CPULimit    int
	GPU         bool

	TaskID uuid.UUID
}

type ContainerState struct {
	Status     string
	ExitCode   int
	StartedAt  string
	FinishedAt string
	Running    bool
	OOMKilled  bool
	Error      string
}

type Container struct {
	ID      string
	Names   []string
	Image   string
	ImageID string
	Command string
	Created int64
	Labels  map[string]string
	Status  string
	Mounts  []Mount
}
