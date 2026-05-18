package userstorage

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ErrQuotaExceeded is returned when used + delta would exceed the user's quota.
var ErrQuotaExceeded = errors.New("userstorage: storage quota exceeded")

// LedgerRow is one persisted object for quota accounting.
type LedgerRow struct {
	ID       string
	ObjectKey string
	ByteSize int64
	Kind     string
	JobID    string
}

func newLedgerID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// AppendLedgerAfterUpload records objects and increments storage_used_bytes in one transaction.
// Call only after successful OSS puts. On failure, callers should delete uploaded objects.
func AppendLedgerAfterUpload(ctx context.Context, db *sql.DB, userID string, rows []LedgerRow) error {
	if db == nil || userID == "" || len(rows) == 0 {
		return nil
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var used, quota int64
	err = tx.QueryRowContext(ctx,
		`SELECT storage_used_bytes, storage_quota_bytes FROM users WHERE id = ? FOR UPDATE`,
		userID,
	).Scan(&used, &quota)
	if err != nil {
		return fmt.Errorf("userstorage: lock user: %w", err)
	}
	var delta int64
	for i := range rows {
		if rows[i].ByteSize < 0 {
			return fmt.Errorf("userstorage: negative byte_size")
		}
		delta += rows[i].ByteSize
	}
	if used+delta > quota {
		return ErrQuotaExceeded
	}
	now := time.Now().UTC()
	for i := range rows {
		id := strings.TrimSpace(rows[i].ID)
		if id == "" {
			nid, err := newLedgerID()
			if err != nil {
				return err
			}
			id = nid
		}
		kind := strings.TrimSpace(rows[i].Kind)
		if kind == "" {
			kind = "output"
		}
		jid := strings.TrimSpace(rows[i].JobID)
		_, err = tx.ExecContext(ctx,
			`INSERT INTO user_storage_objects (id, user_id, object_key, byte_size, kind, job_id, created_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?)`,
			id, userID, rows[i].ObjectKey, rows[i].ByteSize, kind, jid, now,
		)
		if err != nil {
			return fmt.Errorf("userstorage: insert ledger: %w", err)
		}
	}
	_, err = tx.ExecContext(ctx,
		`UPDATE users SET storage_used_bytes = storage_used_bytes + ? WHERE id = ?`,
		delta, userID,
	)
	if err != nil {
		return fmt.Errorf("userstorage: bump used: %w", err)
	}
	return tx.Commit()
}

// CheckQuota reports whether used + delta would exceed quota (without locking for long).
func CheckQuota(ctx context.Context, db *sql.DB, userID string, delta int64) (ok bool, used, quota int64, err error) {
	if db == nil || userID == "" {
		return true, 0, 0, nil
	}
	err = db.QueryRowContext(ctx,
		`SELECT storage_used_bytes, storage_quota_bytes FROM users WHERE id = ?`,
		userID,
	).Scan(&used, &quota)
	if err != nil {
		return false, 0, 0, err
	}
	return used+delta <= quota, used, quota, nil
}

// Usage returns current usage and quota for a user.
func Usage(ctx context.Context, db *sql.DB, userID string) (used, quota int64, err error) {
	if db == nil || userID == "" {
		return 0, 0, fmt.Errorf("userstorage: usage: missing db or user")
	}
	err = db.QueryRowContext(ctx,
		`SELECT storage_used_bytes, storage_quota_bytes FROM users WHERE id = ?`,
		userID,
	).Scan(&used, &quota)
	if err != nil {
		return 0, 0, err
	}
	return used, quota, nil
}
