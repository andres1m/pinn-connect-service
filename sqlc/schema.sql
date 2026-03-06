CREATE TYPE task_status AS ENUM (
    'initializing',
    'scheduled',
    'queued',
    'running',
    'completed',
    'failed'
);

CREATE TABLE tasks (
    id UUID PRIMARY KEY,
    model_id VARCHAR(255) NOT NULL,
    
    input_filename VARCHAR(512) NOT NULL,
    result_path VARCHAR(512),
    
    signature VARCHAR(64) NOT NULL,
    
    status task_status NOT NULL DEFAULT 'initializing',
    
    container_id VARCHAR(64),
    container_image VARCHAR(255),
    container_envs TEXT[],
    container_cmd TEXT[],
    error_log TEXT,
    
    scheduled_at TIMESTAMPTZ,
    started_at TIMESTAMPTZ,
    finished_at TIMESTAMPTZ,
    
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    mem_lim INTEGER,
    cpu_lim INTEGER,
    gpu_enable BOOLEAN
);

CREATE TABLE models (
    id TEXT PRIMARY KEY,
    container_image TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_tasks_signature_completed
ON tasks(signature)
WHERE status = 'completed';

CREATE INDEX idx_tasks_queue_pool ON tasks(scheduled_at ASC)
WHERE status = 'queued';

CREATE INDEX idx_tasks_upcoming_scheduled
ON tasks (scheduled_at)
WHERE status = 'scheduled';