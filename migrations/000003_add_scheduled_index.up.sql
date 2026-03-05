CREATE INDEX idx_tasks_upcoming_scheduled
ON tasks (scheduled_at)
WHERE status = 'scheduled';