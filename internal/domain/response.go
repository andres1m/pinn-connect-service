package domain

type HealthResponse struct {
	Status string `json:"status"`
}

type ContainerStateRespone struct {
	Status     string
	ExitCode   int
	StartedAt  string
	FinishedAt string
	Running    bool
	OOMKilled  bool
	Error      string
}

type RunMockResponse struct {
	ContainerID string `json:"containerID"`
}
