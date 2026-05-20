// Package dbmigrate applies SQL migrations shipped next to this package.
// Files use goose-style section markers (-- +goose Up / Down); only the Up
// section is executed at runtime (no external goose binary required).
package dbmigrate

import (
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"strings"

	mysqldriver "github.com/go-sql-driver/mysql"
)

//go:embed migrations/*.sql
var embedded embed.FS

// Up applies all *.sql migrations in lexical order.
func Up(db *sql.DB) error {
	entries, err := embedded.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("dbmigrate: read migrations: %w", err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		b, err := embedded.ReadFile("migrations/" + e.Name())
		if err != nil {
			return fmt.Errorf("dbmigrate: read %s: %w", e.Name(), err)
		}
		sqlText := extractGooseUp(string(b))
		for _, stmt := range splitSQLStatements(sqlText) {
			if _, err := db.Exec(stmt); err != nil {
				var me *mysqldriver.MySQLError
				if errors.As(err, &me) && (me.Number == 1060 || me.Number == 1061) {
					// Duplicate column (1060) or index (1061) — idempotent re-run.
					continue
				}
				return fmt.Errorf("dbmigrate: exec %s: %w", e.Name(), err)
			}
		}
	}
	return nil
}

func extractGooseUp(s string) string {
	const up = "-- +goose Up"
	const down = "-- +goose Down"
	i := strings.Index(s, up)
	if i < 0 {
		return s
	}
	s = s[i+len(up):]
	if j := strings.Index(s, down); j >= 0 {
		s = s[:j]
	}
	return s
}

func splitSQLStatements(s string) []string {
	var stmts []string
	var b strings.Builder
	for _, line := range strings.Split(s, "\n") {
		t := strings.TrimSpace(line)
		if t == "" || strings.HasPrefix(t, "--") {
			continue
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	body := strings.TrimSpace(b.String())
	for _, part := range strings.Split(body, ";") {
		q := strings.TrimSpace(part)
		if q != "" {
			stmts = append(stmts, q)
		}
	}
	return stmts
}
