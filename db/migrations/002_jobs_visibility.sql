-- Migration 002: Works visibility + generation params storage
-- Applied automatically by internal/jobstore.NewMySQL via migrateMySQLSchema.
-- Do NOT run manually unless you are bootstrapping without a Go binary.

ALTER TABLE generation_jobs
  ADD COLUMN visibility         VARCHAR(16)  NOT NULL DEFAULT 'private',
  ADD COLUMN community_listed_at DATETIME(3)  NULL,
  ADD COLUMN params             TEXT         NULL,
  ADD INDEX idx_visibility_created (visibility, created_at, id);
