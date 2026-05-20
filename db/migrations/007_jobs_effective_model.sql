-- effective_model / effective_provider: upstream wire model and adapter that produced output.
ALTER TABLE generation_jobs ADD COLUMN effective_model VARCHAR(128) NOT NULL DEFAULT '';
ALTER TABLE generation_jobs ADD COLUMN effective_provider VARCHAR(64) NOT NULL DEFAULT '';
ALTER TABLE generation_jobs ADD INDEX idx_jobs_finished_effective_model (finished_at, status, effective_model);
