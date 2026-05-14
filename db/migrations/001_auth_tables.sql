-- Migration 001: User system tables
-- Applied automatically by internal/auth.NewStore on startup.
-- Do NOT run manually unless you are bootstrapping without a Go binary.

CREATE TABLE IF NOT EXISTS users (
  id            VARCHAR(64)   NOT NULL PRIMARY KEY,
  email         VARCHAR(254)  NOT NULL,
  password_hash VARCHAR(256)  NOT NULL,
  display_name  VARCHAR(128)  NOT NULL DEFAULT '',
  created_at    DATETIME(3)   NOT NULL,
  updated_at    DATETIME(3)   NOT NULL,
  UNIQUE KEY uk_email (email)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS user_sessions (
  id         VARCHAR(128) NOT NULL PRIMARY KEY,
  user_id    VARCHAR(64)  NOT NULL,
  expires_at DATETIME(3)  NOT NULL,
  created_at DATETIME(3)  NOT NULL,
  INDEX idx_sess_user    (user_id),
  INDEX idx_sess_expires (expires_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS user_settings (
  user_id               VARCHAR(64) NOT NULL PRIMARY KEY,
  community_auto_public TINYINT(1)  NOT NULL DEFAULT 0,
  prompt_public         TINYINT(1)  NOT NULL DEFAULT 0,
  updated_at            DATETIME(3) NOT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
