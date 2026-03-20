package repository

import (
	"context"
	"errors"
	"pinn-connect-service/internal/db"
	"pinn-connect-service/internal/domain"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	pgxmock "github.com/pashagolub/pgxmock/v4"
)

// ─────────────────────────────────────────────
// HELPERS
// ─────────────────────────────────────────────

func newTaskRepoMock(t *testing.T) (*TaskRepository, pgxmock.PgxPoolIface) {
	t.Helper()
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatalf("pgxmock.NewPool: %v", err)
	}
	return &TaskRepository{queries: db.New(mock)}, mock
}

// anyArgs returns n AnyArg() values for use with WithArgs.
func anyArgs(n int) []any {
	args := make([]any, n)
	for i := range args {
		args[i] = pgxmock.AnyArg()
	}
	return args
}

// taskColumns matches the SELECT column order produced by sqlc.
//
// Fix applied vs. previous version: container_id (pgtype.Text) appears at
// position 4 and status (string) at position 5.  The original order was
// reversed, which caused pgxmock to feed pgtype.Text to the TaskStatus scan
// destination and fail with "unsupported scan type for TaskStatus: pgtype.Text".
//
// If your query.sql selects columns in a different order, reorder this slice
// and the corresponding taskRow() values to match.
var taskColumns = []string{
	"id", "model_id", "input_filename", "result_path", "signature", "status",
	"container_id", "container_image", "container_envs", "container_cmd",
	"error_log", "scheduled_at", "started_at", "finished_at",
	"created_at", "updated_at",
	"mem_lim", "cpu_lim", "gpu_enable", "timeout_sec",
}

// taskRow returns column values in taskColumns order.
// status is a plain string so pgx can scan it into db.TaskStatus (a string alias).
func taskRow(id uuid.UUID, status db.TaskStatus) []any {
	now := pgtype.Timestamptz{Time: time.Now(), Valid: true}
	return []any{
		pgtype.UUID{Bytes: id, Valid: true},            // 0  id
		"model-1",                                      // 1  model_id
		"input.txt",                                    // 2  input_filename
		pgtype.Text{String: "result-path", Valid: true}, // 3  result_path
		"sig-abc",                                      // 4  signature
		string(status),                                 // 5  status
		pgtype.Text{String: "ctr-1", Valid: true},      // 6  container_id
		pgtype.Text{String: "img:latest", Valid: true}, // 7  container_image
		[]string{"ENV=1"},                              // 8  container_envs
		[]string{"run"},                                // 9  container_cmd
		pgtype.Text{String: "", Valid: true},           // 10 error_log
		pgtype.Timestamptz{Valid: false},               // 11 scheduled_at
		pgtype.Timestamptz{Valid: false},               // 12 started_at
		pgtype.Timestamptz{Valid: false},               // 13 finished_at
		now,                                            // 14 created_at
		now,                                            // 15 updated_at
		pgtype.Int4{Int32: 512, Valid: true},           // 16 mem_lim
		pgtype.Int4{Int32: 2, Valid: true},             // 17 cpu_lim
		pgtype.Bool{Bool: false, Valid: true},          // 18 gpu_enable
		int32(30),                                      // 19 timeout_sec
	}
}

// expectMarkQuery sets up UPDATE expectation with n args returning the given status.
func expectMarkQuery(mock pgxmock.PgxPoolIface, id uuid.UUID, status db.TaskStatus, argCount int) {
	mock.ExpectQuery(`UPDATE tasks`).
		WithArgs(anyArgs(argCount)...).
		WillReturnRows(pgxmock.NewRows(taskColumns).AddRow(taskRow(id, status)...))
}

// ─────────────────────────────────────────────
// Create
// ─────────────────────────────────────────────

func TestTaskRepository_Create_Success(t *testing.T) {
	repo, mock := newTaskRepoMock(t)
	id := uuid.New()

	// CreateTaskParams has 15 fields
	mock.ExpectQuery(`INSERT INTO tasks`).
		WithArgs(anyArgs(15)...).
		WillReturnRows(pgxmock.NewRows(taskColumns).AddRow(taskRow(id, db.TaskStatusQueued)...))

	task := &domain.Task{ID: id, ModelID: "m1", Status: domain.TaskQueued}
	if err := repo.Create(context.Background(), task); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if task.CreatedAt.IsZero() {
		t.Error("expected CreatedAt to be populated after Create")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestTaskRepository_Create_WithScheduledAt(t *testing.T) {
	repo, mock := newTaskRepoMock(t)
	id := uuid.New()
	future := time.Now().Add(time.Hour)

	mock.ExpectQuery(`INSERT INTO tasks`).
		WithArgs(anyArgs(15)...).
		WillReturnRows(pgxmock.NewRows(taskColumns).AddRow(taskRow(id, db.TaskStatusScheduled)...))

	task := &domain.Task{ID: id, ModelID: "m1", Status: domain.TaskScheduled, ScheduledAt: &future}
	if err := repo.Create(context.Background(), task); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestTaskRepository_Create_EmptyStatus_DefaultsToInitializing(t *testing.T) {
	repo, mock := newTaskRepoMock(t)
	id := uuid.New()

	mock.ExpectQuery(`INSERT INTO tasks`).
		WithArgs(anyArgs(15)...).
		WillReturnRows(pgxmock.NewRows(taskColumns).AddRow(taskRow(id, db.TaskStatusInitializing)...))

	// Empty Status → repository must substitute TaskInitializing
	task := &domain.Task{ID: id, ModelID: "m1"}
	if err := repo.Create(context.Background(), task); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestTaskRepository_Create_DBError(t *testing.T) {
	repo, mock := newTaskRepoMock(t)

	mock.ExpectQuery(`INSERT INTO tasks`).
		WithArgs(anyArgs(15)...).
		WillReturnError(errors.New("unique violation"))

	if err := repo.Create(context.Background(), &domain.Task{ID: uuid.New(), ModelID: "m1"}); err == nil {
		t.Fatal("expected error, got nil")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

// ─────────────────────────────────────────────
// GetTaskById
// ─────────────────────────────────────────────

func TestTaskRepository_GetTaskById_Success(t *testing.T) {
	repo, mock := newTaskRepoMock(t)
	id := uuid.New()

	mock.ExpectQuery(`SELECT`).
		WithArgs(pgxmock.AnyArg()).
		WillReturnRows(pgxmock.NewRows(taskColumns).AddRow(taskRow(id, db.TaskStatusQueued)...))

	task, err := repo.GetTaskById(context.Background(), id)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if task == nil || task.ID != id {
		t.Errorf("expected task with ID %v", id)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestTaskRepository_GetTaskById_NotFound_ReturnsNil(t *testing.T) {
	repo, mock := newTaskRepoMock(t)

	mock.ExpectQuery(`SELECT`).
		WithArgs(pgxmock.AnyArg()).
		WillReturnError(pgx.ErrNoRows)

	task, err := repo.GetTaskById(context.Background(), uuid.New())
	if err != nil {
		t.Fatalf("expected nil error for not-found, got %v", err)
	}
	if task != nil {
		t.Error("expected nil task for not-found row")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestTaskRepository_GetTaskById_DBError(t *testing.T) {
	repo, mock := newTaskRepoMock(t)

	mock.ExpectQuery(`SELECT`).
		WithArgs(pgxmock.AnyArg()).
		WillReturnError(errors.New("connection lost"))

	if _, err := repo.GetTaskById(context.Background(), uuid.New()); err == nil {
		t.Fatal("expected error, got nil")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestTaskRepository_GetTaskById_PopulatesOptionalTimes(t *testing.T) {
	repo, mock := newTaskRepoMock(t)
	id := uuid.New()
	now := pgtype.Timestamptz{Time: time.Now(), Valid: true}

	row := taskRow(id, db.TaskStatusRunning)
	row[11] = now // scheduled_at
	row[12] = now // started_at
	row[13] = now // finished_at

	mock.ExpectQuery(`SELECT`).
		WithArgs(pgxmock.AnyArg()).
		WillReturnRows(pgxmock.NewRows(taskColumns).AddRow(row...))

	task, err := repo.GetTaskById(context.Background(), id)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if task.ScheduledAt == nil || task.StartedAt == nil || task.FinishedAt == nil {
		t.Error("expected optional time fields to be populated")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

// ─────────────────────────────────────────────
// GetNextQueuedTask
// ─────────────────────────────────────────────

func TestTaskRepository_GetNextQueuedTask_Success(t *testing.T) {
	repo, mock := newTaskRepoMock(t)

	// No args — GetNextQueuedTask takes no parameters
	mock.ExpectQuery(`SELECT`).
		WillReturnRows(pgxmock.NewRows(taskColumns).AddRow(taskRow(uuid.New(), db.TaskStatusQueued)...))

	task, err := repo.GetNextQueuedTask(context.Background())
	if err != nil || task == nil {
		t.Fatalf("expected task, got task=%v err=%v", task, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestTaskRepository_GetNextQueuedTask_EmptyQueue_ReturnsNil(t *testing.T) {
	repo, mock := newTaskRepoMock(t)

	mock.ExpectQuery(`SELECT`).WillReturnError(pgx.ErrNoRows)

	task, err := repo.GetNextQueuedTask(context.Background())
	if err != nil || task != nil {
		t.Errorf("expected nil/nil, got task=%v err=%v", task, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestTaskRepository_GetNextQueuedTask_DBError(t *testing.T) {
	repo, mock := newTaskRepoMock(t)

	mock.ExpectQuery(`SELECT`).WillReturnError(errors.New("db error"))

	if _, err := repo.GetNextQueuedTask(context.Background()); err == nil {
		t.Fatal("expected error, got nil")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

// ─────────────────────────────────────────────
// FindCachedTask
// ─────────────────────────────────────────────

func TestTaskRepository_FindCachedTask_Found(t *testing.T) {
	repo, mock := newTaskRepoMock(t)

	mock.ExpectQuery(`SELECT`).
		WithArgs(pgxmock.AnyArg()).
		WillReturnRows(pgxmock.NewRows([]string{"result_path"}).
			AddRow(pgtype.Text{String: "s3://bucket/key", Valid: true}))

	path, err := repo.FindCachedTask(context.Background(), "sig-123")
	if err != nil || path == "" {
		t.Fatalf("expected path, got %q / %v", path, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestTaskRepository_FindCachedTask_NotFound_ReturnsEmpty(t *testing.T) {
	repo, mock := newTaskRepoMock(t)

	mock.ExpectQuery(`SELECT`).
		WithArgs(pgxmock.AnyArg()).
		WillReturnError(pgx.ErrNoRows)

	path, err := repo.FindCachedTask(context.Background(), "unknown")
	if err != nil || path != "" {
		t.Errorf("expected empty/nil, got %q / %v", path, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestTaskRepository_FindCachedTask_DBError(t *testing.T) {
	repo, mock := newTaskRepoMock(t)

	mock.ExpectQuery(`SELECT`).
		WithArgs(pgxmock.AnyArg()).
		WillReturnError(errors.New("db error"))

	if _, err := repo.FindCachedTask(context.Background(), "sig"); err == nil {
		t.Fatal("expected error, got nil")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

// ─────────────────────────────────────────────
// GetScheduledTasks
// ─────────────────────────────────────────────

func TestTaskRepository_GetScheduledTasks_Success(t *testing.T) {
	repo, mock := newTaskRepoMock(t)

	mock.ExpectQuery(`SELECT`).
		WithArgs(pgxmock.AnyArg()).
		WillReturnRows(pgxmock.NewRows(taskColumns).
			AddRow(taskRow(uuid.New(), db.TaskStatusScheduled)...))

	tasks, err := repo.GetScheduledTasks(context.Background(), time.Now())
	if err != nil || len(tasks) == 0 {
		t.Fatalf("expected tasks, got %v / %v", tasks, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestTaskRepository_GetScheduledTasks_Empty(t *testing.T) {
	repo, mock := newTaskRepoMock(t)

	mock.ExpectQuery(`SELECT`).
		WithArgs(pgxmock.AnyArg()).
		WillReturnRows(pgxmock.NewRows(taskColumns))

	tasks, err := repo.GetScheduledTasks(context.Background(), time.Now())
	if err != nil || len(tasks) != 0 {
		t.Errorf("expected empty/nil, got %v / %v", tasks, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestTaskRepository_GetScheduledTasks_DBError(t *testing.T) {
	repo, mock := newTaskRepoMock(t)

	mock.ExpectQuery(`SELECT`).
		WithArgs(pgxmock.AnyArg()).
		WillReturnError(errors.New("db error"))

	if _, err := repo.GetScheduledTasks(context.Background(), time.Now()); err == nil {
		t.Fatal("expected error, got nil")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

// ─────────────────────────────────────────────
// Mark — all 7 status branches
// ─────────────────────────────────────────────

func TestTaskRepository_Mark_Initializing_Success(t *testing.T) {
	repo, mock := newTaskRepoMock(t)
	id := uuid.New()
	expectMarkQuery(mock, id, db.TaskStatusInitializing, 1)

	if err := repo.Mark(context.Background(), &domain.Task{ID: id}, domain.TaskInitializing); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestTaskRepository_Mark_Initializing_DBError(t *testing.T) {
	repo, mock := newTaskRepoMock(t)

	mock.ExpectQuery(`UPDATE tasks`).WithArgs(pgxmock.AnyArg()).WillReturnError(errors.New("db error"))

	if err := repo.Mark(context.Background(), &domain.Task{ID: uuid.New()}, domain.TaskInitializing); err == nil {
		t.Fatal("expected error, got nil")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestTaskRepository_Mark_Scheduled_Success(t *testing.T) {
	repo, mock := newTaskRepoMock(t)
	id := uuid.New()
	future := time.Now().Add(time.Hour)
	expectMarkQuery(mock, id, db.TaskStatusScheduled, 2)

	if err := repo.Mark(context.Background(), &domain.Task{ID: id, ScheduledAt: &future}, domain.TaskScheduled); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestTaskRepository_Mark_Scheduled_NilScheduledAt_Error(t *testing.T) {
	repo, mock := newTaskRepoMock(t)

	if err := repo.Mark(context.Background(), &domain.Task{ID: uuid.New(), ScheduledAt: nil}, domain.TaskScheduled); err == nil {
		t.Fatal("expected error for nil ScheduledAt, got nil")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestTaskRepository_Mark_Scheduled_DBError(t *testing.T) {
	repo, mock := newTaskRepoMock(t)
	future := time.Now().Add(time.Hour)

	mock.ExpectQuery(`UPDATE tasks`).WithArgs(anyArgs(2)...).WillReturnError(errors.New("db error"))

	if err := repo.Mark(context.Background(), &domain.Task{ID: uuid.New(), ScheduledAt: &future}, domain.TaskScheduled); err == nil {
		t.Fatal("expected error, got nil")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestTaskRepository_Mark_Queued_Success(t *testing.T) {
	repo, mock := newTaskRepoMock(t)
	id := uuid.New()
	expectMarkQuery(mock, id, db.TaskStatusQueued, 1)

	task := &domain.Task{ID: id}
	if err := repo.Mark(context.Background(), task, domain.TaskQueued); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if task.Status != domain.TaskQueued {
		t.Errorf("expected TaskQueued, got %v", task.Status)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestTaskRepository_Mark_Queued_DBError(t *testing.T) {
	repo, mock := newTaskRepoMock(t)

	mock.ExpectQuery(`UPDATE tasks`).WithArgs(pgxmock.AnyArg()).WillReturnError(errors.New("db error"))

	if err := repo.Mark(context.Background(), &domain.Task{ID: uuid.New()}, domain.TaskQueued); err == nil {
		t.Fatal("expected error, got nil")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestTaskRepository_Mark_Running_Success(t *testing.T) {
	repo, mock := newTaskRepoMock(t)
	id := uuid.New()
	expectMarkQuery(mock, id, db.TaskStatusRunning, 2)

	task := &domain.Task{ID: id, ContainerID: "ctr-1"}
	if err := repo.Mark(context.Background(), task, domain.TaskRunning); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if task.Status != domain.TaskRunning {
		t.Errorf("expected TaskRunning, got %v", task.Status)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestTaskRepository_Mark_Running_DBError(t *testing.T) {
	repo, mock := newTaskRepoMock(t)

	mock.ExpectQuery(`UPDATE tasks`).WithArgs(anyArgs(2)...).WillReturnError(errors.New("db error"))

	if err := repo.Mark(context.Background(), &domain.Task{ID: uuid.New()}, domain.TaskRunning); err == nil {
		t.Fatal("expected error, got nil")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestTaskRepository_Mark_Failed_Success(t *testing.T) {
	repo, mock := newTaskRepoMock(t)
	id := uuid.New()
	expectMarkQuery(mock, id, db.TaskStatusFailed, 2)

	if err := repo.Mark(context.Background(), &domain.Task{ID: id, ErrorLog: "oom"}, domain.TaskFailed); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestTaskRepository_Mark_Failed_DBError(t *testing.T) {
	repo, mock := newTaskRepoMock(t)

	mock.ExpectQuery(`UPDATE tasks`).WithArgs(anyArgs(2)...).WillReturnError(errors.New("db error"))

	if err := repo.Mark(context.Background(), &domain.Task{ID: uuid.New()}, domain.TaskFailed); err == nil {
		t.Fatal("expected error, got nil")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestTaskRepository_Mark_Completed_Success(t *testing.T) {
	repo, mock := newTaskRepoMock(t)
	id := uuid.New()
	expectMarkQuery(mock, id, db.TaskStatusCompleted, 2)

	if err := repo.Mark(context.Background(), &domain.Task{ID: id, ResultPath: "s3://key"}, domain.TaskCompleted); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestTaskRepository_Mark_Completed_DBError(t *testing.T) {
	repo, mock := newTaskRepoMock(t)

	mock.ExpectQuery(`UPDATE tasks`).WithArgs(anyArgs(2)...).WillReturnError(errors.New("db error"))

	if err := repo.Mark(context.Background(), &domain.Task{ID: uuid.New()}, domain.TaskCompleted); err == nil {
		t.Fatal("expected error, got nil")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestTaskRepository_Mark_Stopped_Success(t *testing.T) {
	repo, mock := newTaskRepoMock(t)
	id := uuid.New()

	row := taskRow(id, db.TaskStatusStopped)
	row[13] = pgtype.Timestamptz{Time: time.Now(), Valid: true} // finished_at at index 13
	mock.ExpectQuery(`UPDATE tasks`).
		WithArgs(pgxmock.AnyArg()).
		WillReturnRows(pgxmock.NewRows(taskColumns).AddRow(row...))

	task := &domain.Task{ID: id}
	if err := repo.Mark(context.Background(), task, domain.TaskStopped); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if task.FinishedAt == nil {
		t.Error("expected FinishedAt to be set after marking stopped")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestTaskRepository_Mark_Stopped_DBError(t *testing.T) {
	repo, mock := newTaskRepoMock(t)

	mock.ExpectQuery(`UPDATE tasks`).WithArgs(pgxmock.AnyArg()).WillReturnError(errors.New("db error"))

	if err := repo.Mark(context.Background(), &domain.Task{ID: uuid.New()}, domain.TaskStopped); err == nil {
		t.Fatal("expected error, got nil")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestTaskRepository_Mark_UnknownStatus_Error(t *testing.T) {
	repo, mock := newTaskRepoMock(t)

	if err := repo.Mark(context.Background(), &domain.Task{ID: uuid.New()}, domain.TaskStatus("bogus")); err == nil {
		t.Fatal("expected error for unknown status, got nil")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

// ─────────────────────────────────────────────
// GetRunningTasks
// ─────────────────────────────────────────────

func TestTaskRepository_GetRunningTasks_Success(t *testing.T) {
	repo, mock := newTaskRepoMock(t)

	mock.ExpectQuery(`SELECT`).
		WillReturnRows(pgxmock.NewRows(taskColumns).
			AddRow(taskRow(uuid.New(), db.TaskStatusRunning)...))

	tasks, err := repo.GetRunningTasks(context.Background())
	if err != nil || len(tasks) == 0 {
		t.Fatalf("expected tasks, got %v / %v", tasks, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestTaskRepository_GetRunningTasks_Empty(t *testing.T) {
	repo, mock := newTaskRepoMock(t)

	mock.ExpectQuery(`SELECT`).WillReturnRows(pgxmock.NewRows(taskColumns))

	tasks, err := repo.GetRunningTasks(context.Background())
	if err != nil || len(tasks) != 0 {
		t.Errorf("expected empty/nil, got %v / %v", tasks, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestTaskRepository_GetRunningTasks_DBError(t *testing.T) {
	repo, mock := newTaskRepoMock(t)

	mock.ExpectQuery(`SELECT`).WillReturnError(errors.New("db error"))

	if _, err := repo.GetRunningTasks(context.Background()); err == nil {
		t.Fatal("expected error, got nil")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

// ─────────────────────────────────────────────
// GetActiveTasks
// ─────────────────────────────────────────────

func TestTaskRepository_GetActiveTasks_Success(t *testing.T) {
	repo, mock := newTaskRepoMock(t)

	mock.ExpectQuery(`SELECT`).
		WillReturnRows(pgxmock.NewRows(taskColumns).
			AddRow(taskRow(uuid.New(), db.TaskStatusRunning)...))

	tasks, err := repo.GetActiveTasks(context.Background())
	if err != nil || len(tasks) == 0 {
		t.Fatalf("expected tasks, got %v / %v", tasks, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestTaskRepository_GetActiveTasks_DBError(t *testing.T) {
	repo, mock := newTaskRepoMock(t)

	mock.ExpectQuery(`SELECT`).WillReturnError(errors.New("db error"))

	if _, err := repo.GetActiveTasks(context.Background()); err == nil {
		t.Fatal("expected error, got nil")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

// ─────────────────────────────────────────────
// GetTasksPaginated
// ─────────────────────────────────────────────

func TestTaskRepository_GetTasksPaginated_Success(t *testing.T) {
	repo, mock := newTaskRepoMock(t)

	mock.ExpectQuery(`SELECT`).
		WithArgs(anyArgs(2)...).
		WillReturnRows(pgxmock.NewRows(taskColumns).
			AddRow(taskRow(uuid.New(), db.TaskStatusQueued)...))

	tasks, err := repo.GetTasksPaginated(context.Background(), 10, 0)
	if err != nil || len(tasks) == 0 {
		t.Fatalf("expected tasks, got %v / %v", tasks, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestTaskRepository_GetTasksPaginated_DBError(t *testing.T) {
	repo, mock := newTaskRepoMock(t)

	mock.ExpectQuery(`SELECT`).
		WithArgs(anyArgs(2)...).
		WillReturnError(errors.New("db error"))

	if _, err := repo.GetTasksPaginated(context.Background(), 10, 0); err == nil {
		t.Fatal("expected error, got nil")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

// ─────────────────────────────────────────────
// GetTasksCount
// ─────────────────────────────────────────────

func TestTaskRepository_GetTasksCount_Success(t *testing.T) {
	repo, mock := newTaskRepoMock(t)

	mock.ExpectQuery(`SELECT`).
		WillReturnRows(pgxmock.NewRows([]string{"count"}).AddRow(int64(42)))

	count, err := repo.GetTasksCount(context.Background())
	if err != nil || count != 42 {
		t.Fatalf("expected 42/nil, got %d/%v", count, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestTaskRepository_GetTasksCount_DBError(t *testing.T) {
	repo, mock := newTaskRepoMock(t)

	mock.ExpectQuery(`SELECT`).WillReturnError(errors.New("db error"))

	if _, err := repo.GetTasksCount(context.Background()); err == nil {
		t.Fatal("expected error, got nil")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

// ─────────────────────────────────────────────
// DeleteTask
// ─────────────────────────────────────────────

func TestTaskRepository_DeleteTask_Success(t *testing.T) {
	repo, mock := newTaskRepoMock(t)

	mock.ExpectExec(`DELETE`).
		WithArgs(pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("DELETE", 1))

	if err := repo.DeleteTask(context.Background(), uuid.New()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestTaskRepository_DeleteTask_DBError(t *testing.T) {
	repo, mock := newTaskRepoMock(t)

	mock.ExpectExec(`DELETE`).
		WithArgs(pgxmock.AnyArg()).
		WillReturnError(errors.New("foreign key constraint"))

	if err := repo.DeleteTask(context.Background(), uuid.New()); err == nil {
		t.Fatal("expected error, got nil")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

// ─────────────────────────────────────────────
// dbTaskToDomainTask — pure unit, no DB
// ─────────────────────────────────────────────

func TestDbTaskToDomainTask_FullMapping(t *testing.T) {
	id := uuid.New()
	now := time.Now().Truncate(time.Microsecond)
	pg := func(tm time.Time) pgtype.Timestamptz { return pgtype.Timestamptz{Time: tm, Valid: true} }

	src := db.Task{
		ID:             pgtype.UUID{Bytes: id, Valid: true},
		ModelID:        "model-x",
		InputFilename:  "data.csv",
		Signature:      "sig-42",
		Status:         db.TaskStatusRunning,
		ContainerID:    pgtype.Text{String: "ctr-99", Valid: true},
		ContainerImage: pgtype.Text{String: "img:v3", Valid: true},
		ContainerEnvs:  []string{"A=1", "B=2"},
		ContainerCmd:   []string{"python", "run.py"},
		ErrorLog:       pgtype.Text{String: "err msg", Valid: true},
		ResultPath:     pgtype.Text{String: "s3://bucket/key", Valid: true},
		CreatedAt:      pg(now),
		UpdatedAt:      pg(now),
		ScheduledAt:    pg(now),
		StartedAt:      pg(now),
		FinishedAt:     pg(now),
		GpuEnable:      pgtype.Bool{Bool: true, Valid: true},
		CpuLim:         pgtype.Int4{Int32: 4, Valid: true},
		MemLim:         pgtype.Int4{Int32: 1024, Valid: true},
		TimeoutSec:     120,
	}

	got := dbTaskToDomainTask(&src)

	checks := map[string]bool{
		"ID":          got.ID == id,
		"ModelID":     got.ModelID == "model-x",
		"Status":      got.Status == domain.TaskRunning,
		"ContainerID": got.ContainerID == "ctr-99",
		"GPUEnabled":  got.GPUEnabled,
		"CPULim":      got.CPULim == 4,
		"MemLim":      got.MemLim == 1024,
		"TimeoutSec":  got.TimeoutSec == 120,
		"ScheduledAt": got.ScheduledAt != nil,
		"StartedAt":   got.StartedAt != nil,
		"FinishedAt":  got.FinishedAt != nil,
	}
	for field, ok := range checks {
		if !ok {
			t.Errorf("field %s mapping failed", field)
		}
	}
}

func TestDbTaskToDomainTask_NullOptionalTimes(t *testing.T) {
	src := db.Task{
		ID:      pgtype.UUID{Bytes: uuid.New(), Valid: true},
		ModelID: "m1",
	}
	got := dbTaskToDomainTask(&src)

	if got.ScheduledAt != nil {
		t.Error("ScheduledAt should be nil when Valid=false")
	}
	if got.StartedAt != nil {
		t.Error("StartedAt should be nil when Valid=false")
	}
	if got.FinishedAt != nil {
		t.Error("FinishedAt should be nil when Valid=false")
	}
}
