-- Add optional handler result payload (B3-011).
ALTER TABLE gfire.jobs ADD COLUMN IF NOT EXISTS result JSONB;
