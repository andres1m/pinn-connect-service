CREATE TYPE task_status AS ENUM (
    'initializing',
    'queued',
    'running',
    'completed',
    'failed'
);

CREATE TABLE tasks (
    id UUID PRIMARY KEY,
    model_id VARCHAR(255) NOT NULL,
    
    input_path VARCHAR(512) NOT NULL,
    result_path VARCHAR(512),
    
    signature VARCHAR(64) NOT NULL,
    
    status task_status NOT NULL DEFAULT 'initializing',
    
    container_id VARCHAR(64),
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

CREATE INDEX idx_tasks_signature_completed
ON tasks(signature)
WHERE status = 'completed';

CREATE INDEX idx_tasks_queue_pool ON tasks(scheduled_at ASC)
WHERE status = 'queued';