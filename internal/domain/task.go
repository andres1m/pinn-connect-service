package domain

import (
	"time"

	"github.com/google/uuid"
)

type TaskStatus string

const (
	Initializing TaskStatus = "initializing"
	Running      TaskStatus = "running"
	Complete     TaskStatus = "completed"
	Failed       TaskStatus = "failed"
	Queued       TaskStatus = "queued"
)

type Task struct {
	ID          uuid.UUID
	ModelID     string
	InputPath   string
	ResultPath  string
	Signature   string
	Status      TaskStatus
	ContainerID string
	ErrorLog    string
	ScheduledAt *time.Time
	StartedAt   *time.Time
	FinishedAt  *time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
	GPUEnabled  bool
	CPULim      int
	MemLim      int
}
