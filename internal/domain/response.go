package domain

import "time"

type HealthResponse struct {
	Status string `json:"status"`
}

type ContainerStateResponse struct {
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

type TaskStatusResponse struct {
	ID          string     `json:"id"`
	Status      string     `json:"status"`
	CreatedAt   time.Time  `json:"created_at"`
	StartedAt   *time.Time `json:"started_at,omitempty"`
	FinishedAt  *time.Time `json:"finished_at,omitempty"`
	ScheduledAt *time.Time `json:"scheduled_at,omitempty"`
	ResultPath  string     `json:"result_path,omitempty"`
	ErrLog      string     `json:"err_log,omitempty"`
}

type StatsResponse struct {
	AvailableMemoryBytes uint64  `json:"available_memory_bytes"`
	CPUUtilization       float64 `json:"cpu_utilization"`
}
