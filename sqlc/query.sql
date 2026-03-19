-- name: CreateTask :one
INSERT INTO tasks (
    id, model_id, input_filename, signature, status, scheduled_at,
     container_image, container_envs, container_cmd, error_log, mem_lim,
      cpu_lim, gpu_enable, result_path, timeout_sec
) VALUES (
    $1, $2, $3, $4, sqlc.arg('status')::task_status, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14
)
RETURNING *;

-- name: GetTaskByID :one
SELECT * FROM tasks
WHERE id = $1 LIMIT 1;

-- name: GetTasksPaginated :many
SELECT * FROM tasks
ORDER BY created_at DESC
LIMIT $1 OFFSET $2;

-- name: GetTasksCount :one
SELECT COUNT(*) FROM tasks;

-- name: FindCachedTask :one
SELECT result_path FROM tasks
WHERE signature = $1
    AND status = 'completed'::task_status
    AND result_path IS NOT NULL
    AND result_path != ''
ORDER BY created_at DESC
LIMIT 1;

-- name: GetNextQueuedTask :one
UPDATE tasks
SET status = 'running', updated_at = NOW()
WHERE id = (
    SELECT id
    FROM tasks
    WHERE status = 'queued'
    AND (scheduled_at IS NULL OR scheduled_at <= NOW())
    ORDER BY scheduled_at ASC
LIMIT 1
FOR UPDATE SKIP LOCKED
)
RETURNING *;

-- name: MarkTaskRunning :one
UPDATE tasks
SET 
    status = 'running',
    container_id = $2,
    started_at = NOW(),
    updated_at = NOW()
WHERE id = $1
RETURNING *;

-- name: MarkTaskCompleted :one
UPDATE tasks
SET 
    status = 'completed',
    result_path = $2,
    finished_at = NOW(),
    updated_at = NOW()
WHERE id = $1
RETURNING *;

-- name: MarkTaskQueued :one
UPDATE tasks
SET 
    status = 'queued',
    updated_at = NOW()
WHERE id = $1
RETURNING *;

-- name: MarkTaskFailed :one
UPDATE tasks
SET 
    status = 'failed',
    error_log = $2,
    finished_at = NOW(),
    updated_at = NOW()
WHERE id = $1 AND status != 'stopped'
RETURNING *;

-- name: MarkTaskInitializing :one
UPDATE tasks
SET 
    status = 'initializing',
    updated_at = NOW()
WHERE id = $1
RETURNING *;

-- name: MarkTaskScheduled :one
UPDATE tasks
SET 
    status = 'scheduled',
    updated_at = NOW(),
    scheduled_at = $2
WHERE id = $1
RETURNING *;

-- name: MarkTaskStopped :one
UPDATE tasks
SET 
    status = 'stopped',
    finished_at = NOW(),
    updated_at = NOW()
WHERE id = $1
RETURNING *;

-- name: GetActiveTasks :many
SELECT * FROM tasks
WHERE status = 'running' 
    OR status = 'scheduled' 
    OR status = 'queued' 
    OR status = 'initializing';

-- name: GetRunningTasksContainers :many
SELECT * FROM tasks
WHERE status = 'running' AND container_id IS NOT NULL;

-- name: GetUpcomingScheduledTasks :many
SELECT * FROM tasks
WHERE status = 'scheduled'
AND scheduled_at <= $1
ORDER BY scheduled_at ASC;

-- name: GetModelByID :one
SELECT * FROM models WHERE id = $1 LIMIT 1;

-- name: ListModels :many
SELECT * FROM models ORDER BY id;

-- name: CreateModel :one
INSERT INTO models (id, container_image) VALUES ($1, $2) RETURNING *;

-- name: DeleteModel :exec
DELETE FROM models WHERE id = $1;

-- name: UpdateModel :exec
UPDATE models
SET
    container_image = $2,
    updated_at = NOW()
WHERE id = $1;

-- name: ExistsModelByID :one
SELECT EXISTS(SELECT 1 FROM models WHERE id = $1);

-- name: DeleteTask :exec
DELETE FROM tasks WHERE id = $1;