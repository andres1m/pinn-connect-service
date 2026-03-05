package domain

import "time"

type CreateTaskRequest struct {
	ModelID        string     `json:"model_id"`
	ContainerImage string     `json:"container_image"`
	ContainerCmd   []string   `json:"container_cmd"`
	ContainerEnvs  []string   `json:"container_envs"`
	CPULimit       int        `json:"cpu_limit"`
	MemoryLimit    int        `json:"memory_limit"`
	GPUEnabled     bool       `json:"gpu_enabled"`
	ScheduledAt    *time.Time `json:"scheduled_at"`
}
