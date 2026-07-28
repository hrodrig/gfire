DROP INDEX IF EXISTS gfire.idx_jobs_idempotency_key;
ALTER TABLE gfire.jobs DROP COLUMN IF EXISTS idempotency_key;
