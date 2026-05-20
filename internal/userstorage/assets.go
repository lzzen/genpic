package userstorage

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// UserAssetRow is one row from user_storage_objects for listing.
type UserAssetRow struct {
	ID        string
	ObjectKey string
	ByteSize  int64
	Kind      string
	JobID     string
	CreatedAt time.Time
}

// ListUserAssets returns the user's stored objects newest first.
// kinds filters by kind (empty = all). cursor is opaque (created_at RFC3339Nano|id).
func ListUserAssets(ctx context.Context, db *sql.DB, userID string, kinds []string, limit int, cursor string) ([]UserAssetRow, string, error) {
	if db == nil || userID == "" {
		return nil, "", fmt.Errorf("userstorage: list assets: missing db or user")
	}
	if limit <= 0 {
		limit = 30
	}
	if limit > 100 {
		limit = 100
	}

	var kindClause string
	var args []any
	args = append(args, userID)
	if len(kinds) > 0 {
		placeholders := strings.TrimSuffix(strings.Repeat("?,", len(kinds)), ",")
		kindClause = " AND kind IN (" + placeholders + ")"
		for _, k := range kinds {
			args = append(args, strings.TrimSpace(k))
		}
	}

	cursorClause := ""
	if c := strings.TrimSpace(cursor); c != "" {
		parts := strings.SplitN(c, "|", 2)
		if len(parts) == 2 {
			t, err := time.Parse(time.RFC3339Nano, parts[0])
			if err == nil {
				cursorClause = " AND (created_at < ? OR (created_at = ? AND id < ?))"
				args = append(args, t, t, parts[1])
			}
		}
	}

	q := `SELECT id, object_key, byte_size, kind, job_id, created_at
		FROM user_storage_objects
		WHERE user_id = ?` + kindClause + cursorClause + `
		ORDER BY created_at DESC, id DESC
		LIMIT ?`
	args = append(args, limit+1)

	rows, err := db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, "", fmt.Errorf("userstorage: list assets query: %w", err)
	}
	defer rows.Close()

	var out []UserAssetRow
	for rows.Next() {
		var r UserAssetRow
		if err := rows.Scan(&r.ID, &r.ObjectKey, &r.ByteSize, &r.Kind, &r.JobID, &r.CreatedAt); err != nil {
			continue
		}
		out = append(out, r)
	}
	next := ""
	if len(out) > limit {
		last := out[limit-1]
		next = last.CreatedAt.UTC().Format(time.RFC3339Nano) + "|" + last.ID
		out = out[:limit]
	}
	return out, next, nil
}

// ThumbKeyForOutput derives the ledger thumb object key for an output key, if applicable.
func ThumbKeyForOutput(objectKey string) string {
	objectKey = strings.TrimSpace(objectKey)
	if objectKey == "" || !strings.Contains(objectKey, "/jobs/") {
		return ""
	}
	if strings.HasSuffix(objectKey, "_thumb.jpg") {
		return ""
	}
	dot := strings.LastIndex(objectKey, ".")
	if dot <= 0 {
		return ""
	}
	base := objectKey[:dot]
	return base + "_thumb.jpg"
}

// GetUserAsset returns one ledger row owned by userID, or nil if missing.
func GetUserAsset(ctx context.Context, db *sql.DB, userID, assetID string) (*UserAssetRow, error) {
	if db == nil || userID == "" || assetID == "" {
		return nil, fmt.Errorf("userstorage: get asset: missing args")
	}
	var r UserAssetRow
	err := db.QueryRowContext(ctx,
		`SELECT id, object_key, byte_size, kind, job_id, created_at
		 FROM user_storage_objects WHERE id = ? AND user_id = ?`,
		assetID, userID,
	).Scan(&r.ID, &r.ObjectKey, &r.ByteSize, &r.Kind, &r.JobID, &r.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}
