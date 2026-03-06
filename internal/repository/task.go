package repository

import (
	"context"
	"errors"
	"fmt"
	"pinn/internal/db"
	"pinn/internal/domain"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

type TaskRepository struct {
	queries *db.Queries
}

func NewTaskRepository(pool *pgxpool.Pool) *TaskRepository {
	return &TaskRepository{queries: db.New(pool)}
}

func (r *TaskRepository) Create(ctx context.Context, task *domain.Task) error {
	var pgScheduledAt pgtype.Timestamptz
	if task.ScheduledAt != nil {
		pgScheduledAt = pgtype.Timestamptz{Time: *task.ScheduledAt, Valid: true}
	}

	pgstatus := task.Status
	if pgstatus == "" {
		pgstatus = domain.TaskInitializing
	}

	dbtask, err := r.queries.CreateTask(ctx, db.CreateTaskParams{
		ID:             pgtype.UUID{Bytes: task.ID, Valid: true},
		ModelID:        task.ModelID,
		InputFilename:  task.InputFilename,
		Signature:      task.Signature,
		Status:         db.TaskStatus(pgstatus),
		ScheduledAt:    pgScheduledAt,
		ContainerImage: pgtype.Text{String: task.ContainerImage, Valid: task.ContainerImage != ""},
		ContainerEnvs:  task.ContainerEnvs,
		ContainerCmd:   task.ContainerCmd,
		ErrorLog:       pgtype.Text{String: task.ErrorLog, Valid: true},
		MemLim:         pgtype.Int4{Int32: int32(task.MemLim), Valid: true},
		CpuLim:         pgtype.Int4{Int32: int32(task.CPULim), Valid: true},
		GpuEnable:      pgtype.Bool{Bool: task.GPUEnabled, Valid: true},
	})
	if err != nil {
		return fmt.Errorf("creating task: %w", err)
	}

	task.CreatedAt = dbtask.CreatedAt.Time
	task.UpdatedAt = dbtask.UpdatedAt.Time
	task.Status = domain.TaskStatus(dbtask.Status)

	return nil
}

func (r *TaskRepository) GetTaskById(ctx context.Context, uuid uuid.UUID) (*domain.Task, error) {
	dbtask, err := r.queries.GetTaskByID(ctx, pgtype.UUID{Bytes: uuid, Valid: true})
	if err != nil {
		return nil, fmt.Errorf("getting task by id: %w", err)
	}

	return dbTaskToDomainTask(&dbtask), nil
}

func (r *TaskRepository) GetNextQueuedTask(ctx context.Context) (*domain.Task, error) {
	dbtask, err := r.queries.GetNextQueuedTask(ctx)
	if err != nil && errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("getting next queued task: %w", err)
	}

	return dbTaskToDomainTask(&dbtask), nil
}

func (r *TaskRepository) FindCachedTask(ctx context.Context, task *domain.Task) (string, error) {
	resultPath, err := r.queries.FindCachedTask(ctx, task.Signature)
	if err != nil && errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("find cached task: %w", err)
	}

	return resultPath.String, nil
}

func (r *TaskRepository) GetScheduledTasks(ctx context.Context, time time.Time) ([]domain.Task, error) {
	resp, err := r.queries.GetUpcomingScheduledTasks(ctx, pgtype.Timestamptz{Time: time, Valid: true})
	if err != nil {
		return nil, fmt.Errorf("getting scheduled tasks: %w", err)
	}

	result := []domain.Task{}
	for _, res := range resp {
		result = append(result, *dbTaskToDomainTask(&res))
	}

	return result, nil
}

func (r *TaskRepository) Mark(ctx context.Context, task *domain.Task, status domain.TaskStatus) error {
	switch status {
	case domain.TaskInitializing:
		if err := r.markTaskInitializing(ctx, task); err != nil {
			return fmt.Errorf("marking task initializing: %w", err)
		}
		return nil
	case domain.TaskScheduled:
		if err := r.markTaskScheduled(ctx, task); err != nil {
			return fmt.Errorf("marking task scheduled: %w", err)
		}
		return nil
	case domain.TaskQueued:
		if err := r.markTaskQueued(ctx, task); err != nil {
			return fmt.Errorf("marking task queued: %w", err)
		}
		return nil
	case domain.TaskRunning:
		if err := r.markTaskRunning(ctx, task); err != nil {
			return fmt.Errorf("marking task running: %w", err)
		}
		return nil
	case domain.TaskFailed:
		if err := r.markTaskFailed(ctx, task); err != nil {
			return fmt.Errorf("marking task failed: %w", err)
		}
		return nil
	case domain.TaskCompleted:
		if err := r.markTaskCompleted(ctx, task); err != nil {
			return fmt.Errorf("marking task completed: %w", err)
		}
		return nil
	}

	return errors.New("unsupported task status")
}

func (r *TaskRepository) markTaskRunning(ctx context.Context, task *domain.Task) error {
	dbtask, err := r.queries.MarkTaskRunning(ctx, db.MarkTaskRunningParams{
		ID:          pgtype.UUID{Bytes: task.ID, Valid: true},
		ContainerID: pgtype.Text{String: task.ContainerID, Valid: true},
	})
	if err != nil {
		return fmt.Errorf("db query for marking task running: %w", err)
	}

	task.UpdatedAt = dbtask.UpdatedAt.Time
	task.Status = domain.TaskStatus(dbtask.Status)

	return nil
}

func (r *TaskRepository) markTaskCompleted(ctx context.Context, task *domain.Task) error {
	dbtask, err := r.queries.MarkTaskCompleted(ctx, db.MarkTaskCompletedParams{
		ID:         pgtype.UUID{Bytes: task.ID, Valid: true},
		ResultPath: pgtype.Text{String: task.ResultPath, Valid: true},
	})
	if err != nil {
		return fmt.Errorf("db query for marking task completed: %w", err)
	}

	task.UpdatedAt = dbtask.UpdatedAt.Time
	task.Status = domain.TaskStatus(dbtask.Status)

	return nil
}

func (r *TaskRepository) markTaskFailed(ctx context.Context, task *domain.Task) error {
	dbtask, err := r.queries.MarkTaskFailed(ctx, db.MarkTaskFailedParams{
		ID:       pgtype.UUID{Bytes: task.ID, Valid: true},
		ErrorLog: pgtype.Text{String: task.ErrorLog, Valid: true},
	})
	if err != nil {
		return fmt.Errorf("db query for marking task failed: %w", err)
	}

	task.UpdatedAt = dbtask.UpdatedAt.Time
	task.Status = domain.TaskStatus(dbtask.Status)

	return nil
}

func (r *TaskRepository) markTaskQueued(ctx context.Context, task *domain.Task) error {
	dbtask, err := r.queries.MarkTaskQueued(ctx, pgtype.UUID{Bytes: task.ID, Valid: true})
	if err != nil {
		return fmt.Errorf("db query for marking task queued: %w", err)
	}

	task.UpdatedAt = dbtask.UpdatedAt.Time
	task.Status = domain.TaskStatus(dbtask.Status)

	return nil
}

func (r *TaskRepository) markTaskInitializing(ctx context.Context, task *domain.Task) error {
	dbtask, err := r.queries.MarkTaskInitializing(ctx, pgtype.UUID{Bytes: task.ID, Valid: true})
	if err != nil {
		return fmt.Errorf("db query for marking task initializing: %w", err)
	}

	task.UpdatedAt = dbtask.UpdatedAt.Time
	task.Status = domain.TaskStatus(dbtask.Status)

	return nil
}

func (r *TaskRepository) markTaskScheduled(ctx context.Context, task *domain.Task) error {
	if task.ScheduledAt == nil {
		return errors.New("scheduledAt field must be not null")
	}

	dbtask, err := r.queries.MarkTaskScheduled(ctx, db.MarkTaskScheduledParams{
		ID:          pgtype.UUID{Bytes: task.ID, Valid: true},
		ScheduledAt: pgtype.Timestamptz{Time: *task.ScheduledAt, Valid: true},
	})
	if err != nil {
		return fmt.Errorf("db query for marking task scheduled: %w", err)
	}

	task.UpdatedAt = dbtask.UpdatedAt.Time
	task.Status = domain.TaskStatus(dbtask.Status)

	return nil
}

func (r *TaskRepository) GetRunningTasksContainers(ctx context.Context) ([]domain.RunningTasksContainer, error) {
	resp, err := r.queries.GetRunningTasksContainers(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting running tasks containers: %w", err)
	}

	result := make([]domain.RunningTasksContainer, 0, len(resp))
	for _, row := range resp {
		result = append(result, domain.RunningTasksContainer{
			ID:          uuid.UUID(row.ID.Bytes),
			ContainerID: row.ContainerID.String,
		})
	}

	return result, nil
}

func dbTaskToDomainTask(task *db.Task) *domain.Task {
	d := &domain.Task{
		ID:             uuid.UUID(task.ID.Bytes),
		ModelID:        task.ModelID,
		InputFilename:  task.InputFilename,
		ResultPath:     task.ResultPath.String,
		Signature:      task.Signature,
		Status:         domain.TaskStatus(task.Status),
		ContainerID:    task.ContainerID.String,
		ContainerImage: task.ContainerImage.String,
		ContainerEnvs:  task.ContainerEnvs,
		ContainerCmd:   task.ContainerCmd,
		ErrorLog:       task.ErrorLog.String,
		CreatedAt:      task.CreatedAt.Time,
		UpdatedAt:      task.UpdatedAt.Time,
		GPUEnabled:     task.GpuEnable.Bool,
		CPULim:         int(task.CpuLim.Int32),
		MemLim:         int(task.MemLim.Int32),
	}

	if task.ScheduledAt.Valid {
		d.ScheduledAt = &task.ScheduledAt.Time
	}
	if task.StartedAt.Valid {
		d.StartedAt = &task.StartedAt.Time
	}
	if task.FinishedAt.Valid {
		d.FinishedAt = &task.FinishedAt.Time
	}

	return d
}
