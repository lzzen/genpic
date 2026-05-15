// Package templatestore persists user-defined and public generation templates (MySQL).
package templatestore

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"genpic/internal/jobstore"
)

// ErrDuplicateSourceJob is returned when a template for this generation job already exists.
var ErrDuplicateSourceJob = errors.New("templatestore: template already exists for this job")

// Template is one saved preset (prompt + params + optional reference images + preview URL).
type Template struct {
	ID              string
	UserID          string
	SourceJobID     string // generation_jobs.id; at most one template row per job (unique when set)
	Provider        string // job provider name: openai | gemini | wan (reconstruct catalog id for UI)
	Visibility      string // "private" | "public"
	Title           string
	PrimaryModel    string
	Models          []string
	Prompt          string
	Params          *jobstore.JobParams
	ReferenceImages []map[string]any // {mime_type, b64_json} — same shape as generate API
	ResultImageURL  string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// Store lists and mutates templates.
type Store interface {
	// ListForModel matches primary_model against primaryModelQuery OR primaryModelWire
	// (e.g. catalog id from the SPA vs normalised wire id stored in DB, and vice versa for legacy rows).
	ListForModel(ctx context.Context, primaryModelQuery, primaryModelWire, viewerUserID string, limit int) ([]Template, error)
	Create(ctx context.Context, t *Template) error
	Delete(ctx context.Context, id, actorUserID string, actorIsAdmin bool) (bool, error)
}

func newID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("templatestore: id: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// ModelsJSON builds the persisted models_json column (supported model ids).
func ModelsJSON(models []string) (string, error) {
	if len(models) == 0 {
		return "[]", nil
	}
	b, err := json.Marshal(models)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func ParseModelsJSON(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	var out []string
	if err := json.Unmarshal([]byte(s), &out); err != nil {
		return nil
	}
	return out
}

// Now returns UTC timestamps for persistence.
func Now() time.Time { return time.Now().UTC().Truncate(time.Millisecond) }
