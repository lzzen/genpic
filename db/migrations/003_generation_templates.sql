-- Reference copy of goose migration (applied by the binary via internal/dbmigrate).
-- Source of truth: internal/dbmigrate/migrations/003_generation_templates.sql

-- +goose Up
CREATE TABLE IF NOT EXISTS generation_templates (
  id                     VARCHAR(64)   NOT NULL PRIMARY KEY,
  user_id                VARCHAR(64)   NOT NULL,
  visibility             VARCHAR(16)   NOT NULL DEFAULT 'private',
  title                  VARCHAR(256)  NOT NULL DEFAULT '',
  primary_model          VARCHAR(128)  NOT NULL,
  models_json            TEXT          NOT NULL,
  prompt                 TEXT          NOT NULL,
  params_json            TEXT          NULL,
  reference_images_json  MEDIUMTEXT    NULL,
  result_image_url       VARCHAR(768)  NOT NULL DEFAULT '',
  created_at             DATETIME(3)   NOT NULL,
  updated_at             DATETIME(3)   NOT NULL,
  INDEX idx_tpl_model_public (primary_model, visibility, created_at),
  INDEX idx_tpl_user_model (user_id, primary_model, created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- +goose Down
DROP TABLE IF EXISTS generation_templates;
