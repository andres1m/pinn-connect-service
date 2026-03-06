package domain

import "time"

type CreateTaskRequest struct {
	ModelID       string     `json:"model_id"`
	ContainerCmd  []string   `json:"container_cmd"`
	ContainerEnvs []string   `json:"container_envs"`
	CPULimit      int        `json:"cpu_limit"`
	MemoryLimit   int        `json:"memory_limit"`
	GPUEnabled    bool       `json:"gpu_enabled"`
	ScheduledAt   *time.Time `json:"scheduled_at"`
}

type CreateModelRequest struct {
	ID             string `json:"id"`
	ContainerImage string `json:"container_image"`
}

type UpdateModelRequest struct {
	ID                string `json:"id"`
	NewContainerImage string `json:"new_container_image"`
}
