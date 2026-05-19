package userstorage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// ErrUserNotFound is returned when no user matches an admin storage operation.
var ErrUserNotFound = errors.New("userstorage: user not found")

// UserStorageRow is one user's quota summary for admin listing.
type UserStorageRow struct {
	UserID      string
	Email       string
	DisplayName string
	UsedBytes   int64
	QuotaBytes  int64
}

// RemainingBytes returns max(0, quota-used).
func RemainingBytes(used, quota int64) int64 {
	r := quota - used
	if r < 0 {
		return 0
	}
	return r
}

// ListUsersStorage returns users with storage columns, newest first.
func ListUsersStorage(ctx context.Context, db *sql.DB, q string, limit, offset int) ([]UserStorageRow, int, error) {
	if db == nil {
		return nil, 0, fmt.Errorf("userstorage: list: missing db")
	}
	if limit <= 0 {
		limit = 30
	}
	if limit > 500 {
		limit = 500
	}
	if offset < 0 {
		offset = 0
	}
	q = strings.ToLower(strings.TrimSpace(q))

	var total int
	var err error
	if q != "" {
		pat := q + "%"
		err = db.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM users WHERE LOWER(email) LIKE ?`, pat,
		).Scan(&total)
	} else {
		err = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&total)
	}
	if err != nil {
		return nil, 0, fmt.Errorf("userstorage: count users: %w", err)
	}

	var rows *sql.Rows
	if q != "" {
		pat := q + "%"
		rows, err = db.QueryContext(ctx,
			`SELECT id, email, display_name, storage_used_bytes, storage_quota_bytes
			 FROM users WHERE LOWER(email) LIKE ?
			 ORDER BY created_at DESC LIMIT ? OFFSET ?`,
			pat, limit, offset,
		)
	} else {
		rows, err = db.QueryContext(ctx,
			`SELECT id, email, display_name, storage_used_bytes, storage_quota_bytes
			 FROM users ORDER BY created_at DESC LIMIT ? OFFSET ?`,
			limit, offset,
		)
	}
	if err != nil {
		return nil, 0, fmt.Errorf("userstorage: list users: %w", err)
	}
	defer rows.Close()

	var out []UserStorageRow
	for rows.Next() {
		var row UserStorageRow
		if err := rows.Scan(&row.UserID, &row.Email, &row.DisplayName, &row.UsedBytes, &row.QuotaBytes); err != nil {
			return nil, 0, fmt.Errorf("userstorage: scan user: %w", err)
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return out, total, nil
}

// AdminSetQuota sets storage_quota_bytes for a user.
func AdminSetQuota(ctx context.Context, db *sql.DB, userID string, quotaBytes int64) error {
	if db == nil || strings.TrimSpace(userID) == "" {
		return fmt.Errorf("userstorage: set quota: missing db or user")
	}
	if quotaBytes < 0 {
		return fmt.Errorf("userstorage: quota must be non-negative")
	}
	res, err := db.ExecContext(ctx,
		`UPDATE users SET storage_quota_bytes = ? WHERE id = ?`,
		quotaBytes, userID,
	)
	if err != nil {
		return fmt.Errorf("userstorage: set quota: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrUserNotFound
	}
	return nil
}

// AdminAdjustQuota adds delta to storage_quota_bytes and returns the new quota.
func AdminAdjustQuota(ctx context.Context, db *sql.DB, userID string, delta int64) (int64, error) {
	if db == nil || strings.TrimSpace(userID) == "" {
		return 0, fmt.Errorf("userstorage: adjust quota: missing db or user")
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()

	var quota int64
	err = tx.QueryRowContext(ctx,
		`SELECT storage_quota_bytes FROM users WHERE id = ? FOR UPDATE`,
		userID,
	).Scan(&quota)
	if err == sql.ErrNoRows {
		return 0, ErrUserNotFound
	}
	if err != nil {
		return 0, fmt.Errorf("userstorage: lock user quota: %w", err)
	}
	newQuota := quota + delta
	if newQuota < 0 {
		return 0, fmt.Errorf("userstorage: quota would be negative")
	}
	_, err = tx.ExecContext(ctx,
		`UPDATE users SET storage_quota_bytes = ? WHERE id = ?`,
		newQuota, userID,
	)
	if err != nil {
		return 0, fmt.Errorf("userstorage: update quota: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return newQuota, nil
}

// UsageRow loads one user's storage fields after an update.
func UsageRow(ctx context.Context, db *sql.DB, userID string) (UserStorageRow, error) {
	if db == nil || strings.TrimSpace(userID) == "" {
		return UserStorageRow{}, fmt.Errorf("userstorage: usage row: missing db or user")
	}
	var row UserStorageRow
	err := db.QueryRowContext(ctx,
		`SELECT id, email, display_name, storage_used_bytes, storage_quota_bytes FROM users WHERE id = ?`,
		userID,
	).Scan(&row.UserID, &row.Email, &row.DisplayName, &row.UsedBytes, &row.QuotaBytes)
	if err == sql.ErrNoRows {
		return UserStorageRow{}, ErrUserNotFound
	}
	if err != nil {
		return UserStorageRow{}, err
	}
	return row, nil
}
