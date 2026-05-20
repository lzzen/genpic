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
  id                   VARCHAR(64)   NOT NULL PRIMARY KEY,
  model                VARCHAR(128)  NOT NULL DEFAULT '',
  provider             VARCHAR(64)   NOT NULL DEFAULT '',
  effective_model      VARCHAR(128)  NOT NULL DEFAULT '',
  effective_provider   VARCHAR(64)   NOT NULL DEFAULT '',
  prompt               TEXT          NOT NULL,
  status               VARCHAR(32)   NOT NULL DEFAULT 'queued',
  error_code           VARCHAR(64)   NOT NULL DEFAULT '',
  error_msg            TEXT          NOT NULL,
  images               MEDIUMTEXT,
  tokens_used          INT           NOT NULL DEFAULT 0,
  upstream_request_id  VARCHAR(128)  NOT NULL DEFAULT '',
  key_id               VARCHAR(64)   NOT NULL DEFAULT '',
  user_id              VARCHAR(128)  NOT NULL DEFAULT '',
  session_id           VARCHAR(128)  NOT NULL DEFAULT '',
  visibility           VARCHAR(16)   NOT NULL DEFAULT 'private',
  community_listed_at  DATETIME(3)   NULL,
  params               TEXT          NULL,
  created_at           DATETIME(3)   NOT NULL,
  started_at           DATETIME(3)   NULL,
  finished_at          DATETIME(3)   NULL,
  INDEX idx_created_id (created_at, id),
  INDEX idx_list_session (session_id, created_at, id),
  INDEX idx_list_user (user_id, created_at, id),
  INDEX idx_visibility_created (visibility, created_at, id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
`

const mysqlCols = `id, model, provider, effective_model, effective_provider, prompt, status, error_code, error_msg,
  images, tokens_used, upstream_request_id, key_id, user_id, session_id, visibility, community_listed_at, params,
  created_at, started_at, finished_at`

// MySQL is a MySQL-backed job store satisfying the Store interface.
type MySQL struct {
	db *sql.DB
}

// DB returns the underlying *sql.DB so callers (e.g. main.go for auth setup)
// can share the connection pool without opening a second connection.
func (s *MySQL) DB() *sql.DB { return s.db }

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

// migrateMySQLSchema adds columns/indexes to older deployments (idempotent).
// MySQL error 1060 = duplicate column, 1061 = duplicate index — both are ignored.
func migrateMySQLSchema(db *sql.DB) error {
	stmts := []string{
		// M1 → M3 columns (may already exist on fresh installs that use the DDL above)
		`ALTER TABLE generation_jobs ADD COLUMN user_id VARCHAR(128) NOT NULL DEFAULT ''`,
		`ALTER TABLE generation_jobs ADD COLUMN session_id VARCHAR(128) NOT NULL DEFAULT ''`,
		`ALTER TABLE generation_jobs ADD INDEX idx_list_session (session_id, created_at, id)`,
		`ALTER TABLE generation_jobs ADD INDEX idx_list_user (user_id, created_at, id)`,
		// M5 columns
		`ALTER TABLE generation_jobs ADD COLUMN visibility VARCHAR(16) NOT NULL DEFAULT 'private'`,
		`ALTER TABLE generation_jobs ADD COLUMN community_listed_at DATETIME(3) NULL`,
		`ALTER TABLE generation_jobs ADD COLUMN params TEXT NULL`,
		`ALTER TABLE generation_jobs ADD INDEX idx_visibility_created (visibility, created_at, id)`,
		`ALTER TABLE generation_jobs ADD COLUMN upstream_request_id VARCHAR(128) NOT NULL DEFAULT ''`,
		`ALTER TABLE generation_jobs ADD INDEX idx_jobs_finished_model (finished_at, status, model)`,
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
	paramsJSON, err := marshalParams(job.Params)
	if err != nil {
		return "", err
	}
	vis := job.Visibility
	if vis == "" {
		vis = "private"
	}
	_, err = s.db.Exec(`
		INSERT INTO generation_jobs
		  (id, model, provider, effective_model, effective_provider, prompt, status, error_code, error_msg,
		   images, tokens_used, upstream_request_id, key_id, user_id, session_id,
		   visibility, community_listed_at, params,
		   created_at, started_at, finished_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		job.ID, job.Model, job.Provider, job.EffectiveModel, job.EffectiveProvider, job.Prompt,
		string(job.Status), job.ErrorCode, job.ErrorMsg,
		imagesJSON, job.TokensUsed, job.UpstreamRequestID, job.KeyID, job.UserID, job.SessionID,
		vis, nullTime(job.CommunityListedAt), paramsJSON,
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
	paramsJSON, err := marshalParams(j.Params)
	if err != nil {
		return false
	}
	_, err = tx.Exec(`
		UPDATE generation_jobs SET
		  status = ?, error_code = ?, error_msg = ?,
		  images = ?, tokens_used = ?, upstream_request_id = ?,
		  effective_model = ?, effective_provider = ?, params = ?,
		  started_at = ?, finished_at = ?
		WHERE id = ?`,
		string(j.Status), j.ErrorCode, j.ErrorMsg,
		imagesJSON, j.TokensUsed, j.UpstreamRequestID,
		j.EffectiveModel, j.EffectiveProvider, paramsJSON,
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

// AdminList returns jobs globally, newest first.
func (s *MySQL) AdminList(limit, offset int) ([]*Job, int64) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 500 {
		limit = 500
	}
	if offset < 0 {
		offset = 0
	}
	var total int64
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM generation_jobs`).Scan(&total); err != nil {
		return nil, 0
	}
	rows, err := s.db.Query(
		`SELECT `+mysqlCols+` FROM generation_jobs
		ORDER BY created_at DESC, id DESC
		LIMIT ? OFFSET ?`,
		limit, offset,
	)
	if err != nil {
		return nil, total
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
	return jobs, total
}

// AdminStats returns aggregate counts from the database.
func (s *MySQL) AdminStats() AdminStatsSummary {
	out := AdminStatsSummary{ByProvider: map[string]int64{}}
	row := s.db.QueryRow(`
		SELECT
			COUNT(*),
			COALESCE(SUM(CASE WHEN status = 'succeeded' THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN status = 'failed' THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN status IN ('queued','running') THEN 1 ELSE 0 END), 0)
		FROM generation_jobs`)
	var q int64
	if err := row.Scan(&out.TotalJobs, &out.Succeeded, &out.Failed, &q); err != nil {
		return out
	}
	out.QueuedRunning = q

	rows, err := s.db.Query(`
		SELECT COALESCE(NULLIF(TRIM(provider), ''), '(unknown)'), COUNT(*)
		FROM generation_jobs GROUP BY COALESCE(NULLIF(TRIM(provider), ''), '(unknown)')`)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var p string
		var c int64
		if err := rows.Scan(&p, &c); err != nil {
			continue
		}
		out.ByProvider[p] = c
	}
	return out
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
		j                 Job
		status            string
		imgData           []byte
		paramsData        []byte
		communityListedAt sql.NullTime
		startedAt         sql.NullTime
		finishedAt        sql.NullTime
	)
	if err := s.Scan(
		&j.ID, &j.Model, &j.Provider, &j.EffectiveModel, &j.EffectiveProvider, &j.Prompt,
		&status, &j.ErrorCode, &j.ErrorMsg,
		&imgData, &j.TokensUsed, &j.UpstreamRequestID, &j.KeyID, &j.UserID, &j.SessionID,
		&j.Visibility, &communityListedAt, &paramsData,
		&j.CreatedAt, &startedAt, &finishedAt,
	); err != nil {
		return nil, err
	}
	j.Status = Status(status)
	if j.Visibility == "" {
		j.Visibility = "private"
	}
	if len(imgData) > 0 {
		_ = json.Unmarshal(imgData, &j.Images)
	}
	if len(paramsData) > 0 {
		var p JobParams
		if err := json.Unmarshal(paramsData, &p); err == nil {
			j.Params = &p
		}
	}
	if communityListedAt.Valid {
		j.CommunityListedAt = communityListedAt.Time
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

func marshalParams(p *JobParams) ([]byte, error) {
	if p == nil {
		return nil, nil
	}
	b, err := json.Marshal(p)
	if err != nil {
		return nil, fmt.Errorf("jobstore/mysql: marshal params: %w", err)
	}
	return b, nil
}

// ListPublic returns public jobs, newest community_listed_at first.
func (s *MySQL) ListPublic(limit int, cursor string) ([]*Job, string) {
	if limit <= 0 {
		limit = 20
	}
	sel := `SELECT j.` + mysqlCols

	var (
		rows *sql.Rows
		err  error
	)
	if cursor == "" {
		rows, err = s.db.Query(
			sel+` FROM generation_jobs j
			WHERE j.visibility = 'public'
			ORDER BY j.community_listed_at DESC, j.id DESC
			LIMIT ?`, limit+1,
		)
	} else {
		rows, err = s.db.Query(
			sel+` FROM generation_jobs j
			INNER JOIN generation_jobs c ON c.id = ?
			WHERE j.visibility = 'public'
			AND (j.community_listed_at < c.community_listed_at
			   OR (j.community_listed_at = c.community_listed_at AND j.id < c.id))
			ORDER BY j.community_listed_at DESC, j.id DESC
			LIMIT ?`, cursor, limit+1,
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
	return jobs[:limit], jobs[limit-1].ID
}

// SetVisibility updates a job's visibility. Returns an error if the job is not
// owned by userID or does not exist.
func (s *MySQL) SetVisibility(id, userID, visibility string) error {
	if visibility != "private" && visibility != "public" {
		return fmt.Errorf("jobstore/mysql: invalid visibility %q", visibility)
	}
	var communityListedAt interface{}
	if visibility == "public" {
		// Only stamp community_listed_at the first time a job is made public.
		var existing sql.NullTime
		_ = s.db.QueryRow(
			`SELECT community_listed_at FROM generation_jobs WHERE id = ? AND user_id = ?`, id, userID,
		).Scan(&existing)
		if !existing.Valid {
			communityListedAt = time.Now()
		} else {
			communityListedAt = existing.Time
		}
	}

	var res sql.Result
	var err error
	if visibility == "public" {
		res, err = s.db.Exec(
			`UPDATE generation_jobs SET visibility = ?, community_listed_at = ? WHERE id = ? AND user_id = ?`,
			visibility, communityListedAt, id, userID,
		)
	} else {
		res, err = s.db.Exec(
			`UPDATE generation_jobs SET visibility = ? WHERE id = ? AND user_id = ?`,
			visibility, id, userID,
		)
	}
	if err != nil {
		return fmt.Errorf("jobstore/mysql: set visibility: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("jobstore/mysql: job not found or permission denied")
	}
	return nil
}

func nullTime(t time.Time) sql.NullTime {
	if t.IsZero() {
		return sql.NullTime{}
	}
	return sql.NullTime{Valid: true, Time: t}
}
