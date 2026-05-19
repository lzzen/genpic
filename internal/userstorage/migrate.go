package userstorage

import (
	"database/sql"
	"errors"
	"fmt"

	mysqldriver "github.com/go-sql-driver/mysql"
)

// MigrateUserQuotaColumns adds storage_quota_bytes / storage_used_bytes to users when missing.
// Safe to call on every startup. When columns already exist, skips ALTER entirely so startup
// does not pay two failed ALTER round-trips (and avoids extra metadata locks on busy DBs).
func MigrateUserQuotaColumns(db *sql.DB) error {
	if db == nil {
		return nil
	}
	hasQuota, hasUsed, err := userStorageQuotaColumnsExist(db)
	if err != nil {
		return err
	}
	if hasQuota && hasUsed {
		return nil
	}
	stmts := []struct {
		need bool
		q    string
	}{
		{!hasQuota, `ALTER TABLE users ADD COLUMN storage_quota_bytes BIGINT NOT NULL DEFAULT 536870912`},
		{!hasUsed, `ALTER TABLE users ADD COLUMN storage_used_bytes BIGINT NOT NULL DEFAULT 0`},
	}
	for _, row := range stmts {
		if !row.need {
			continue
		}
		if _, err := db.Exec(row.q); err != nil {
			var me *mysqldriver.MySQLError
			if errors.As(err, &me) && me.Number == 1060 {
				continue
			}
			return fmt.Errorf("userstorage migrate: %w", err)
		}
	}
	return nil
}

func userStorageQuotaColumnsExist(db *sql.DB) (quota, used bool, err error) {
	const q = `SELECT COUNT(*) FROM information_schema.COLUMNS
WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'users' AND COLUMN_NAME = ?`
	var n int
	if err := db.QueryRow(q, "storage_quota_bytes").Scan(&n); err != nil {
		return false, false, fmt.Errorf("userstorage migrate: check storage_quota_bytes: %w", err)
	}
	quota = n > 0
	if err := db.QueryRow(q, "storage_used_bytes").Scan(&n); err != nil {
		return false, false, fmt.Errorf("userstorage migrate: check storage_used_bytes: %w", err)
	}
	used = n > 0
	return quota, used, nil
}
