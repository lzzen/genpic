-- +goose Up
ALTER TABLE generation_jobs ADD COLUMN effective_model VARCHAR(128) NOT NULL DEFAULT '';
ALTER TABLE generation_jobs ADD COLUMN effective_provider VARCHAR(64) NOT NULL DEFAULT '';
ALTER TABLE generation_jobs ADD INDEX idx_jobs_finished_effective_model (finished_at, status, effective_model);

-- +goose Down
ALTER TABLE generation_jobs DROP INDEX idx_jobs_finished_effective_model;
ALTER TABLE generation_jobs DROP COLUMN effective_provider;
ALTER TABLE generation_jobs DROP COLUMN effective_model;
