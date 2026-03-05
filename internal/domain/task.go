package domain

import (
	"time"

	"github.com/google/uuid"
)

type TaskStatus string

const (
	TaskInitializing TaskStatus = "initializing"
	TaskScheduled    TaskStatus = "scheduled"
	TaskRunning      TaskStatus = "running"
	TaskCompleted    TaskStatus = "completed"
	TaskFailed       TaskStatus = "failed"
	TaskQueued       TaskStatus = "queued"
)

type Task struct {
	ID             uuid.UUID
	ModelID        string
	InputFilename  string
	ResultPath     string
	Signature      string
	Status         TaskStatus
	ContainerID    string
	ContainerImage string
	ContainerEnvs  []string
	ContainerCmd   []string
	ErrorLog       string
	ScheduledAt    *time.Time
	StartedAt      *time.Time
	FinishedAt     *time.Time
	CreatedAt      time.Time
	UpdatedAt      time.Time
	GPUEnabled     bool
	CPULim         int
	MemLim         int
}

type RunningTasksContainer struct {
	ID          uuid.UUID
	ContainerID string
}
