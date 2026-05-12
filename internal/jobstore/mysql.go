package jobstore

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	mysqldriver "github.com/go-sql-driver/mysql"
)

// mysqlDDL is applied on startup (CREATE TABLE IF NOT EXISTS).
// The images column holds JSON for []jobstore.Image: URLs and small metadata only;
// large b64_json is cleared before persist (see jobstore.SanitizeImageForStorage).
// parseTime=true must be present in the DSN for DATETIME columns to scan into time.Time.
const mysqlDDL = `
CREATE TABLE IF NOT EXISTS generation_jobs (
  id          VARCHAR(64)   NOT NULL PRIMARY KEY,
  model       VARCHAR(128)  NOT NULL DEFAULT '',
  provider    VARCHAR(64)   NOT NULL DEFAULT '',
  prompt      TEXT          NOT NULL,
  status      VARCHAR(32)   NOT NULL DEFAULT 'queued',
  error_code  VARCHAR(64)   NOT NULL DEFAULT '',
  error_msg   TEXT          NOT NULL,
  images      MEDIUMTEXT,
  tokens_used INT           NOT NULL DEFAULT 0,
  key_id      VARCHAR(64)   NOT NULL DEFAULT '',
  user_id     VARCHAR(128)  NOT NULL DEFAULT '',
  session_id  VARCHAR(128)  NOT NULL DEFAULT '',
  created_at  DATETIME(3)   NOT NULL,
  started_at  DATETIME(3)   NULL,
  finished_at DATETIME(3)   NULL,
  INDEX idx_created_id (created_at, id),
  INDEX idx_list_session (session_id, created_at, id),
  INDEX idx_list_user (user_id, created_at, id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
`

const mysqlCols = `id, model, provider, prompt, status, error_code, error_msg,
  images, tokens_used, key_id, user_id, session_id, created_at, started_at, finished_at`

// MySQL is a MySQL-backed job store satisfying the Store interface.
type MySQL struct {
	db *sql.DB
}

// NewMySQL opens a MySQL connection pool, runs the schema DDL, and returns a MySQL store.
//
// dsn must include parseTime=true, e.g.:
//
//	user:password@tcp(localhost:3306)/genpic?parseTime=true&charset=utf8mb4
func NewMySQL(dsn string, maxOpen, maxIdle int) (*MySQL, error) {
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("jobstore/mysql: open: %w", err)
	}
	if maxOpen > 0 {
		db.SetMaxOpenConns(maxOpen)
	}
	if maxIdle > 0 {
		db.SetMaxIdleConns(maxIdle)
	}
	// Recycle connections so we do not reuse TCP conns that MySQL or the
	// WSL↔Windows bridge has already half-closed (avoids driver "broken pipe" writes).
	db.SetConnMaxLifetime(30 * time.Minute)
	db.SetConnMaxIdleTime(50 * time.Second)

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("jobstore/mysql: ping: %w", err)
	}
	if _, err := db.Exec(mysqlDDL); err != nil {
		return nil, fmt.Errorf("jobstore/mysql: apply schema: %w", err)
	}
	if err := migrateMySQLSchema(db); err != nil {
		return nil, err
	}
	return &MySQL{db: db}, nil
}

// migrateMySQLSchema adds owner columns and list indexes on older deployments
// that pre-date user_id / session_id. Duplicate column/index errors are ignored.
func migrateMySQLSchema(db *sql.DB) error {
	stmts := []string{
		`ALTER TABLE generation_jobs ADD COLUMN user_id VARCHAR(128) NOT NULL DEFAULT ''`,
		`ALTER TABLE generation_jobs ADD COLUMN session_id VARCHAR(128) NOT NULL DEFAULT ''`,
		`ALTER TABLE generation_jobs ADD INDEX idx_list_session (session_id, created_at, id)`,
		`ALTER TABLE generation_jobs ADD INDEX idx_list_user (user_id, created_at, id)`,
	}
	for _, q := range stmts {
		if _, err := db.Exec(q); err != nil {
			var me *mysqldriver.MySQLError
			if errors.As(err, &me) && (me.Number == 1060 || me.Number == 1061) {
				continue
			}
			return fmt.Errorf("jobstore/mysql: migrate: %w", err)
		}
	}
	return nil
}

func mysqlListWhere(scope OwnerScope) (clause string, args []any) {
	switch {
	case scope.UserID != "":
		return "j.user_id = ?", []any{scope.UserID}
	case scope.SessionID != "":
		return "j.session_id = ? AND j.user_id = ''", []any{scope.SessionID}
	default:
		return "j.user_id = '' AND j.session_id = ''", nil
	}
}

// Create inserts a new job and returns its assigned ID.
func (s *MySQL) Create(job *Job) (string, error) {
	id, err := newID()
	if err != nil {
		return "", fmt.Errorf("jobstore/mysql: generate id: %w", err)
	}
	job.ID = id
	if job.CreatedAt.IsZero() {
		job.CreatedAt = time.Now()
	}
	imagesJSON, err := marshalImages(job.Images)
	if err != nil {
		return "", err
	}
	_, err = s.db.Exec(`
		INSERT INTO generation_jobs
		  (id, model, provider, prompt, status, error_code, error_msg,
		   images, tokens_used, key_id, user_id, session_id, created_at, started_at, finished_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		job.ID, job.Model, job.Provider, job.Prompt,
		string(job.Status), job.ErrorCode, job.ErrorMsg,
		imagesJSON, job.TokensUsed, job.KeyID, job.UserID, job.SessionID,
		job.CreatedAt, nullTime(job.StartedAt), nullTime(job.FinishedAt),
	)
	if err != nil {
		return "", fmt.Errorf("jobstore/mysql: insert: %w", err)
	}
	return id, nil
}

// Get returns a copy of the job by ID, or (nil, false) if not found.
func (s *MySQL) Get(id string) (*Job, bool) {
	row := s.db.QueryRow(
		`SELECT `+mysqlCols+` FROM generation_jobs WHERE id = ?`, id,
	)
	j, err := scanJob(row)
	if err == sql.ErrNoRows {
		return nil, false
	}
	if err != nil {
		return nil, false
	}
	return j, true
}

// Update fetches the job, applies fn under a transaction, and writes it back.
func (s *MySQL) Update(id string, fn func(*Job)) bool {
	tx, err := s.db.Begin()
	if err != nil {
		return false
	}
	defer tx.Rollback() //nolint:errcheck

	row := tx.QueryRow(
		`SELECT `+mysqlCols+` FROM generation_jobs WHERE id = ? FOR UPDATE`, id,
	)
	j, err := scanJob(row)
	if err != nil {
		return false
	}

	fn(j)

	imagesJSON, err := marshalImages(j.Images)
	if err != nil {
		return false
	}
	_, err = tx.Exec(`
		UPDATE generation_jobs SET
		  status = ?, error_code = ?, error_msg = ?,
		  images = ?, tokens_used = ?, started_at = ?, finished_at = ?
		WHERE id = ?`,
		string(j.Status), j.ErrorCode, j.ErrorMsg,
		imagesJSON, j.TokensUsed,
		nullTime(j.StartedAt), nullTime(j.FinishedAt),
		id,
	)
	if err != nil {
		return false
	}
	return tx.Commit() == nil
}

// List returns up to limit jobs newest-first, using cursor (a job ID) for pagination.
func (s *MySQL) List(limit int, cursor string, scope OwnerScope) ([]*Job, string) {
	if limit <= 0 {
		limit = 20
	}

	where, wargs := mysqlListWhere(scope)
	var (
		rows *sql.Rows
		err  error
		sel  = `SELECT j.` + mysqlCols
	)

	if cursor == "" {
		args := append(append([]any{}, wargs...), limit+1)
		rows, err = s.db.Query(
			sel+` FROM generation_jobs j WHERE `+where+`
			ORDER BY j.created_at DESC, j.id DESC LIMIT ?`,
			args...,
		)
	} else {
		// Keyset: select rows with (created_at, id) strictly less than the cursor row.
		args := append([]any{cursor}, wargs...)
		args = append(args, limit+1)
		rows, err = s.db.Query(
			sel+` FROM generation_jobs j
			INNER JOIN generation_jobs c ON c.id = ?
			WHERE `+where+`
			AND (j.created_at < c.created_at
			   OR (j.created_at = c.created_at AND j.id < c.id))
			ORDER BY j.created_at DESC, j.id DESC
			LIMIT ?`,
			args...,
		)
	}
	if err != nil {
		return nil, ""
	}
	defer rows.Close()

	var jobs []*Job
	for rows.Next() {
		j, err := scanJobRows(rows)
		if err != nil {
			continue
		}
		jobs = append(jobs, j)
	}

	if len(jobs) <= limit {
		return jobs, ""
	}
	nextCursor := jobs[limit-1].ID
	return jobs[:limit], nextCursor
}

// ── helpers ───────────────────────────────────────────────────────────────────

type rowScanner interface {
	Scan(dest ...any) error
}

func scanJob(s rowScanner) (*Job, error) {
	return doScan(s)
}

func scanJobRows(r *sql.Rows) (*Job, error) {
	return doScan(r)
}

func doScan(s rowScanner) (*Job, error) {
	var (
		j          Job
		status     string
		imgData    []byte
		startedAt  sql.NullTime
		finishedAt sql.NullTime
	)
	if err := s.Scan(
		&j.ID, &j.Model, &j.Provider, &j.Prompt,
		&status, &j.ErrorCode, &j.ErrorMsg,
		&imgData, &j.TokensUsed, &j.KeyID, &j.UserID, &j.SessionID,
		&j.CreatedAt, &startedAt, &finishedAt,
	); err != nil {
		return nil, err
	}
	j.Status = Status(status)
	if len(imgData) > 0 {
		_ = json.Unmarshal(imgData, &j.Images)
	}
	if startedAt.Valid {
		j.StartedAt = startedAt.Time
	}
	if finishedAt.Valid {
		j.FinishedAt = finishedAt.Time
	}
	return &j, nil
}

func marshalImages(images []Image) (string, error) {
	if images == nil {
		return "[]", nil
	}
	b, err := json.Marshal(images)
	if err != nil {
		return "", fmt.Errorf("jobstore/mysql: marshal images: %w", err)
	}
	return string(b), nil
}

func nullTime(t time.Time) sql.NullTime {
	if t.IsZero() {
		return sql.NullTime{}
	}
	return sql.NullTime{Valid: true, Time: t}
}
