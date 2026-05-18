-- Reference copy; runtime applies internal/dbmigrate/migrations/004_user_storage_objects.sql

-- +goose Up
CREATE TABLE IF NOT EXISTS user_storage_objects (
  id          VARCHAR(64)   NOT NULL PRIMARY KEY,
  user_id     VARCHAR(64)   NOT NULL,
  object_key  VARCHAR(1024) NOT NULL,
  byte_size   BIGINT        NOT NULL,
  kind        VARCHAR(32)   NOT NULL DEFAULT 'output',
  job_id      VARCHAR(64)   NOT NULL DEFAULT '',
  created_at  DATETIME(3)   NOT NULL,
  INDEX idx_user_created (user_id, created_at),
  INDEX idx_job (job_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- +goose Down
DROP TABLE IF EXISTS user_storage_objects;
