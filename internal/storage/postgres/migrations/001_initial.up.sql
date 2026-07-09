CREATE SCHEMA IF NOT EXISTS gfire;

-- Core job table
CREATE TABLE gfire.jobs (
    id          UUID PRIMARY KEY,
    queue_token BIGSERIAL NOT NULL,
    name        TEXT NOT NULL,
    args        JSONB NOT NULL DEFAULT '[]',
    queue       TEXT NOT NULL DEFAULT 'default',
    state       TEXT NOT NULL DEFAULT 'Enqueued',
    retry_max   INT NOT NULL DEFAULT 10,
    timeout_ms  BIGINT NOT NULL DEFAULT 0,
    progress_at TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- State history (immutable append-only)
CREATE TABLE gfire.job_states (
    id         BIGSERIAL PRIMARY KEY,
    job_id     UUID NOT NULL REFERENCES gfire.jobs(id) ON DELETE CASCADE,
    name       TEXT NOT NULL,
    reason     TEXT,
    data       JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Scheduled jobs (delayed execution + retry scheduling)
CREATE TABLE gfire.scheduled_jobs (
    id         UUID PRIMARY KEY,
    job_id     UUID NOT NULL REFERENCES gfire.jobs(id) ON DELETE CASCADE,
    enqueue_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Recurring job definitions
CREATE TABLE gfire.recurring_jobs (
    id         TEXT PRIMARY KEY,
    job_name   TEXT NOT NULL,
    args       JSONB NOT NULL DEFAULT '[]',
    queue      TEXT NOT NULL DEFAULT 'default',
    cron_expr  TEXT NOT NULL,
    last_run   TIMESTAMPTZ,
    next_run   TIMESTAMPTZ,
    enabled    BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Continuations (job chaining)
CREATE TABLE gfire.continuations (
    id          BIGSERIAL PRIMARY KEY,
    parent_id   UUID NOT NULL REFERENCES gfire.jobs(id) ON DELETE CASCADE,
    child_name  TEXT NOT NULL,
    child_args  JSONB NOT NULL DEFAULT '[]',
    child_queue TEXT NOT NULL DEFAULT 'default',
    condition   TEXT NOT NULL CHECK (condition IN ('OnSucceeded', 'OnFailed', 'OnAny')),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Server registry
CREATE TABLE gfire.servers (
    id             TEXT PRIMARY KEY,
    started_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_heartbeat TIMESTAMPTZ NOT NULL DEFAULT now(),
    worker_count   INT NOT NULL,
    queues         TEXT[] NOT NULL DEFAULT '{default}',
    status         TEXT NOT NULL DEFAULT 'active'
);

-- Counters (aggregated stats, UPSERT pattern)
CREATE TABLE gfire.counters (
    key        TEXT PRIMARY KEY,
    value      BIGINT NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Distributed locks
CREATE TABLE gfire.locks (
    resource   TEXT PRIMARY KEY,
    owner_id   TEXT NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL
);

-- ──────────────────────────────────────────────────────────
-- Indexes
-- ──────────────────────────────────────────────────────────

CREATE INDEX idx_jobs_dequeue
    ON gfire.jobs (queue, queue_token)
    WHERE state = 'Enqueued';

CREATE INDEX idx_jobs_state_queue
    ON gfire.jobs (state, queue);

CREATE INDEX idx_jobs_orphan
    ON gfire.jobs (progress_at)
    WHERE state = 'Processing';

CREATE INDEX idx_scheduled_jobs_enqueue_at
    ON gfire.scheduled_jobs (enqueue_at);

CREATE INDEX idx_job_states_lookup
    ON gfire.job_states (job_id, created_at);

CREATE INDEX idx_continuations_parent
    ON gfire.continuations (parent_id);

CREATE INDEX idx_servers_heartbeat
    ON gfire.servers (last_heartbeat);

CREATE INDEX idx_locks_expires
    ON gfire.locks (expires_at);
