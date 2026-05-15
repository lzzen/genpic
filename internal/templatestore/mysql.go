package templatestore

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"genpic/internal/jobstore"
)

// MySQL implements Store against generation_templates.
type MySQL struct {
	db *sql.DB
}

// NewMySQL returns a template store using the shared application DB pool.
func NewMySQL(db *sql.DB) *MySQL {
	if db == nil {
		return nil
	}
	return &MySQL{db: db}
}

const listSQL = `
SELECT id, user_id, visibility, title, primary_model, models_json, prompt, params_json,
       reference_images_json, result_image_url, created_at, updated_at
  FROM generation_templates
 WHERE primary_model = ?
   AND (visibility = 'public' OR (visibility = 'private' AND user_id = ?))
 ORDER BY visibility = 'public' DESC, created_at DESC
 LIMIT ?`

// ListForModel returns public templates for the model plus the viewer's private templates.
// viewerUserID empty → only public rows match the OR branch for private (none).
func (s *MySQL) ListForModel(ctx context.Context, primaryModel, viewerUserID string, limit int) ([]Template, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("templatestore: nil db")
	}
	pm := strings.TrimSpace(primaryModel)
	if pm == "" {
		return nil, nil
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	uid := strings.TrimSpace(viewerUserID)
	rows, err := s.db.QueryContext(ctx, listSQL, pm, uid, limit)
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
		&t.ID, &t.UserID, &t.Visibility, &t.Title, &t.PrimaryModel, &modelsJSON,
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
  (id, user_id, visibility, title, primary_model, models_json, prompt, params_json,
   reference_images_json, result_image_url, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

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
	_, err = s.db.ExecContext(ctx, insertSQL,
		t.ID, t.UserID, t.Visibility, t.Title, t.PrimaryModel, mj, t.Prompt,
		paramsJSON, refJSON, t.ResultImageURL, t.CreatedAt, t.UpdatedAt,
	)
	if err != nil {
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
