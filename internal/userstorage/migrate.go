package userstorage

import (
	"database/sql"
	"errors"
	"fmt"

	mysqldriver "github.com/go-sql-driver/mysql"
)

// MigrateUserQuotaColumns adds storage_quota_bytes / storage_used_bytes to users when missing.
// Safe to call on every startup (ignores duplicate column errors).
func MigrateUserQuotaColumns(db *sql.DB) error {
	if db == nil {
		return nil
	}
	stmts := []string{
		`ALTER TABLE users ADD COLUMN storage_quota_bytes BIGINT NOT NULL DEFAULT 536870912`,
		`ALTER TABLE users ADD COLUMN storage_used_bytes BIGINT NOT NULL DEFAULT 0`,
	}
	for _, q := range stmts {
		if _, err := db.Exec(q); err != nil {
			var me *mysqldriver.MySQLError
			if errors.As(err, &me) && me.Number == 1060 {
				continue
			}
			return fmt.Errorf("userstorage migrate: %w", err)
		}
	}
	return nil
}
