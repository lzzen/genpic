package templatestore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"genpic/internal/jobstore"

	mysqldriver "github.com/go-sql-driver/mysql"
)

// MySQL implements Store against generation_templates.
type MySQL struct {
	db *sql.DB
}

// NewMySQL returns a template store using the shared application DB pool.
// It applies idempotent schema tweaks (e.g. source_job_id + unique index) once per process start.
func NewMySQL(db *sql.DB) (*MySQL, error) {
	if db == nil {
		return nil, fmt.Errorf("templatestore: nil db")
	}
	if err := migrateTemplateSchema(db); err != nil {
		return nil, err
	}
	return &MySQL{db: db}, nil
}

// migrateTemplateSchema adds columns/indexes expected by this package (idempotent).
// MySQL 1060 = duplicate column, 1061 = duplicate index — ignored. 1146 = no such table — ignored.
func migrateTemplateSchema(db *sql.DB) error {
	stmts := []string{
		`ALTER TABLE generation_templates ADD COLUMN source_job_id VARCHAR(64) NULL AFTER user_id`,
		`ALTER TABLE generation_templates ADD UNIQUE KEY uk_generation_templates_source_job (source_job_id)`,
		`ALTER TABLE generation_templates ADD COLUMN provider VARCHAR(64) NOT NULL DEFAULT '' AFTER source_job_id`,
	}
	for _, q := range stmts {
		if _, err := db.Exec(q); err != nil {
			var me *mysqldriver.MySQLError
			if errors.As(err, &me) && (me.Number == 1060 || me.Number == 1061) {
				continue
			}
			if errors.As(err, &me) && me.Number == 1146 {
				return nil
			}
			return fmt.Errorf("templatestore: migrate: %w", err)
		}
	}
	return nil
}

const listSQL = `
SELECT id, user_id, provider, visibility, title, primary_model, models_json, prompt, params_json,
       reference_images_json, result_image_url, created_at, updated_at
  FROM generation_templates
 WHERE (primary_model = ? OR primary_model = ?)
   AND (visibility = 'public' OR (visibility = 'private' AND user_id = ?))
 ORDER BY visibility = 'public' DESC, created_at DESC
 LIMIT ?`

// ListForModel returns public templates for the model plus the viewer's private templates.
// primaryModelQuery is typically the SPA catalog id; primaryModelWire is the normalised id; rows match either.
func (s *MySQL) ListForModel(ctx context.Context, primaryModelQuery, primaryModelWire, viewerUserID string, limit int) ([]Template, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("templatestore: nil db")
	}
	m1 := strings.TrimSpace(primaryModelQuery)
	m2 := strings.TrimSpace(primaryModelWire)
	if m1 == "" && m2 == "" {
		return nil, nil
	}
	if m2 == "" {
		m2 = m1
	}
	if m1 == "" {
		m1 = m2
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	uid := strings.TrimSpace(viewerUserID)
	rows, err := s.db.QueryContext(ctx, listSQL, m1, m2, uid, limit)
	if err != nil {
		return nil, fmt.Errorf("templatestore: list: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	var out []Template
	for rows.Next() {
		t, err := scanTemplateRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func scanTemplateRow(sc interface {
	Scan(dest ...any) error
}) (Template, error) {
	var t Template
	var modelsJSON string
	var params sql.NullString
	var refImgs sql.NullString
	if err := sc.Scan(
		&t.ID, &t.UserID, &t.Provider, &t.Visibility, &t.Title, &t.PrimaryModel, &modelsJSON,
		&t.Prompt, &params, &refImgs, &t.ResultImageURL, &t.CreatedAt, &t.UpdatedAt,
	); err != nil {
		return Template{}, fmt.Errorf("templatestore: scan: %w", err)
	}
	t.Models = ParseModelsJSON(modelsJSON)
	if params.Valid && strings.TrimSpace(params.String) != "" {
		var jp jobstore.JobParams
		if err := json.Unmarshal([]byte(params.String), &jp); err == nil {
			t.Params = &jp
		}
	}
	if refImgs.Valid && strings.TrimSpace(refImgs.String) != "" {
		var refs []map[string]any
		if err := json.Unmarshal([]byte(refImgs.String), &refs); err == nil {
			t.ReferenceImages = refs
		}
	}
	return t, nil
}

const insertSQL = `
INSERT INTO generation_templates
  (id, user_id, source_job_id, provider, visibility, title, primary_model, models_json, prompt, params_json,
   reference_images_json, result_image_url, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

// Create inserts a new template row. t.ID must be empty — a new id is assigned.
func (s *MySQL) Create(ctx context.Context, t *Template) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("templatestore: nil db")
	}
	if t == nil {
		return fmt.Errorf("templatestore: nil template")
	}
	id, err := newID()
	if err != nil {
		return err
	}
	t.ID = id
	now := Now()
	if t.CreatedAt.IsZero() {
		t.CreatedAt = now
	}
	t.UpdatedAt = now
	if t.Visibility != "public" {
		t.Visibility = "private"
	}
	mj, err := ModelsJSON(t.Models)
	if err != nil {
		return err
	}
	var paramsJSON sql.NullString
	if t.Params != nil {
		b, err := json.Marshal(t.Params)
		if err != nil {
			return fmt.Errorf("templatestore: params json: %w", err)
		}
		paramsJSON = sql.NullString{String: string(b), Valid: true}
	}
	var refJSON sql.NullString
	if len(t.ReferenceImages) > 0 {
		b, err := json.Marshal(t.ReferenceImages)
		if err != nil {
			return fmt.Errorf("templatestore: refs json: %w", err)
		}
		refJSON = sql.NullString{String: string(b), Valid: true}
	}
	sjid := strings.TrimSpace(t.SourceJobID)
	var srcArg any
	if sjid != "" {
		srcArg = sjid
	} else {
		srcArg = nil
	}
	_, err = s.db.ExecContext(ctx, insertSQL,
		t.ID, t.UserID, srcArg, strings.TrimSpace(t.Provider), t.Visibility, t.Title, t.PrimaryModel, mj, t.Prompt,
		paramsJSON, refJSON, t.ResultImageURL, t.CreatedAt, t.UpdatedAt,
	)
	if err != nil {
		var me *mysqldriver.MySQLError
		if errors.As(err, &me) && me.Number == 1062 {
			return ErrDuplicateSourceJob
		}
		return fmt.Errorf("templatestore: insert: %w", err)
	}
	return nil
}

// Delete removes a template if the actor owns it, or if the actor is admin and the row is public.
func (s *MySQL) Delete(ctx context.Context, id, actorUserID string, actorIsAdmin bool) (bool, error) {
	if s == nil || s.db == nil {
		return false, fmt.Errorf("templatestore: nil db")
	}
	id = strings.TrimSpace(id)
	aid := strings.TrimSpace(actorUserID)
	if id == "" || aid == "" {
		return false, nil
	}
	var res sql.Result
	var err error
	if actorIsAdmin {
		// Admins may remove any public template (any owner) or their own rows.
		res, err = s.db.ExecContext(ctx,
			`DELETE FROM generation_templates WHERE id = ? AND (user_id = ? OR visibility = 'public')`,
			id, aid,
		)
	} else {
		res, err = s.db.ExecContext(ctx,
			`DELETE FROM generation_templates WHERE id = ? AND user_id = ?`,
			id, aid,
		)
	}
	if err != nil {
		return false, fmt.Errorf("templatestore: delete: %w", err)
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

func truncateRunes(s string, max int) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "…"
}

const adminListPromptRunes = 200

// ListAllForAdmin returns templates across all users and models (newest first).
func (s *MySQL) ListAllForAdmin(ctx context.Context, limit, offset int, visibilityFilter string) ([]AdminTemplateSummary, int64, error) {
	if s == nil || s.db == nil {
		return nil, 0, fmt.Errorf("templatestore: nil db")
	}
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	vf := strings.ToLower(strings.TrimSpace(visibilityFilter))
	where := "1=1"
	var filterArgs []any
	if vf == "public" || vf == "private" {
		where = "t.visibility = ?"
		filterArgs = append(filterArgs, vf)
	}

	var total int64
	countQ := fmt.Sprintf(`SELECT COUNT(*) FROM generation_templates t WHERE %s`, where)
	if err := s.db.QueryRowContext(ctx, countQ, filterArgs...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("templatestore: admin list count: %w", err)
	}

	args := append(append([]any{}, filterArgs...), limit, offset)
	listQ := fmt.Sprintf(`
SELECT t.id, t.user_id, COALESCE(u.email,''), t.visibility, t.title, t.primary_model, t.provider,
       t.prompt, t.result_image_url, t.created_at
  FROM generation_templates t
  LEFT JOIN users u ON u.id = t.user_id
 WHERE %s
 ORDER BY t.created_at DESC
 LIMIT ? OFFSET ?`, where)

	rows, err := s.db.QueryContext(ctx, listQ, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("templatestore: admin list: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	var out []AdminTemplateSummary
	for rows.Next() {
		var row AdminTemplateSummary
		var prompt string
		if err := rows.Scan(
			&row.ID, &row.UserID, &row.OwnerEmail, &row.Visibility, &row.Title, &row.PrimaryModel, &row.Provider,
			&prompt, &row.ResultImageURL, &row.CreatedAt,
		); err != nil {
			return nil, 0, fmt.Errorf("templatestore: admin scan: %w", err)
		}
		row.PromptPreview = truncateRunes(prompt, adminListPromptRunes)
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return out, total, nil
}

// AdminSetTemplateVisibility sets visibility for any template row.
func (s *MySQL) AdminSetTemplateVisibility(ctx context.Context, templateID, visibility string) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("templatestore: nil db")
	}
	id := strings.TrimSpace(templateID)
	vis := strings.ToLower(strings.TrimSpace(visibility))
	if id == "" {
		return fmt.Errorf("templatestore: empty template id")
	}
	if vis != "public" && vis != "private" {
		return fmt.Errorf("templatestore: invalid visibility %q", visibility)
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE generation_templates SET visibility = ?, updated_at = ? WHERE id = ?`,
		vis, Now(), id,
	)
	if err != nil {
		return fmt.Errorf("templatestore: admin set visibility: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("templatestore: rows affected: %w", err)
	}
	if n == 0 {
		return ErrTemplateNotFound
	}
	return nil
}
