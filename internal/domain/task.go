package domain

import "time"

type TaskStatus string

const (
	Initializing TaskStatus = "initializing"
	Running      TaskStatus = "running"
	Complete     TaskStatus = "complete"
	Failed       TaskStatus = "failed"
	Scheduled    TaskStatus = "scheduled"
	Queued       TaskStatus = "queued"
)

type Task struct {
	ContainerID string
	StartedAt   time.Time
	FinishedAt  time.Time
	GPUEnabled  bool
	CPULim      int
	MemLim      int
	Status      TaskStatus
}
