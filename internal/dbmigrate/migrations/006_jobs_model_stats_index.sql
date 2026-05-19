-- +goose Up
ALTER TABLE generation_jobs ADD INDEX idx_jobs_finished_model (finished_at, status, model);

-- +goose Down
ALTER TABLE generation_jobs DROP INDEX idx_jobs_finished_model;
