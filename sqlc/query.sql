-- name: CreateTask :one
INSERT INTO tasks (
    id, model_id, input_path, signature, status, scheduled_at, error_log, mem_lim, cpu_lim, gpu_enable
) VALUES (
    $1, $2, $3, $4, COALESCE(NULLIF($5::task_status, ''), 'initializing'), $6, $7, $8, $9, $10
)
RETURNING *;

-- name: GetTaskByID :one
SELECT * FROM tasks
WHERE id = $1 LIMIT 1;

-- name: FindCachedTask :one
SELECT result_path FROM tasks
WHERE signature = $1 AND status = 'completed'
LIMIT 1;

-- name: GetNextQueuedTask :one
SELECT id, model_id, input_path, signature, scheduled_at, mem_lim, cpu_lim, gpu_enable 
FROM tasks
WHERE status = 'queued' 
  AND (scheduled_at IS NULL OR scheduled_at <= NOW())
ORDER BY scheduled_at ASC
LIMIT 1
FOR UPDATE SKIP LOCKED;

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

-- name: MarkTaskFailed :one
UPDATE tasks
SET 
    status = 'failed',
    error_log = $2,
    finished_at = NOW(),
    updated_at = NOW()
WHERE id = $1
RETURNING *;

-- name: GetRunningTasksContainers :many
SELECT id, container_id FROM tasks
WHERE status = 'running' AND container_id IS NOT NULL;