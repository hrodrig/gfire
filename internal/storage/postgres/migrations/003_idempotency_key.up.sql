-- B5-010: idempotency key for client retry deduplication.
ALTER TABLE gfire.jobs ADD COLUMN IF NOT EXISTS idempotency_key TEXT;
CREATE UNIQUE INDEX IF NOT EXISTS idx_jobs_idempotency_key ON gfire.jobs(idempotency_key) WHERE idempotency_key IS NOT NULL;
