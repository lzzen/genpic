-- +goose Up
CREATE TABLE IF NOT EXISTS materialize_fetch_failures (
  id           VARCHAR(64)   NOT NULL PRIMARY KEY,
  job_id       VARCHAR(64)   NOT NULL DEFAULT '',
  user_id      VARCHAR(64)   NOT NULL DEFAULT '',
  image_index  INT           NOT NULL DEFAULT 0,
  source_url   TEXT          NOT NULL,
  err_message  VARCHAR(4096) NOT NULL DEFAULT '',
  created_at   DATETIME(3)   NOT NULL,
  INDEX idx_mff_created (created_at),
  INDEX idx_mff_job (job_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- +goose Down
DROP TABLE IF EXISTS materialize_fetch_failures;
