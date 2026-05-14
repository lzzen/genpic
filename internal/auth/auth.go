// Package auth implements user registration/login, server-side sessions,
// and HTTP middleware for the Genpic platform.
//
// Password storage: PBKDF2-HMAC-SHA256 (100 000 iterations, 32-byte key)
// via the Go 1.24 standard library crypto/pbkdf2 package. No external
// dependencies are required.
//
// Session tokens: 32-byte cryptographically random hex strings stored in
// the user_sessions table. Each token is transmitted via an HTTP-only
// cookie (genpic_session) with SameSite=Lax and a 30-day TTL.
//
// Schema: see db/migrations/001_auth_tables.sql. Tables are created
// automatically by NewStore.
package auth

import (
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	mysqldriver "github.com/go-sql-driver/mysql"
)

// Sentinel errors returned by Store methods.
var (
	ErrEmailTaken          = errors.New("auth: email already registered")
	ErrInvalidCredentials  = errors.New("auth: invalid email or password")
	ErrSessionNotFound     = errors.New("auth: session not found or expired")
)

const (
	pbkdf2Iter   = 100_000
	pbkdf2Salt   = 16
	pbkdf2Key    = 32
	sessionTTL   = 30 * 24 * time.Hour
	SessionCookie = "genpic_session"
)

// User is a registered platform user.
type User struct {
	ID          string
	Email       string
	DisplayName string
	CreatedAt   time.Time
}

// UserSettings holds per-user privacy preferences.
type UserSettings struct {
	UserID              string
	CommunityAutoPublic bool
	PromptPublic        bool
}

// Store wraps a *sql.DB and provides all auth operations.
// It shares the application database with the job store.
type Store struct {
	db *sql.DB
}

// NewStore creates the auth tables (if absent) and returns a Store.
// The *sql.DB must already be open and reachable (e.g. pass the same db
// used by jobstore.NewMySQL).
func NewStore(db *sql.DB) (*Store, error) {
	for _, ddl := range authDDLStatements {
		if _, err := db.Exec(ddl); err != nil {
			return nil, fmt.Errorf("auth: apply schema: %w", err)
		}
	}
	return &Store{db: db}, nil
}

// authDDLStatements are run once on startup — idempotent CREATE TABLE IF NOT EXISTS.
var authDDLStatements = []string{
	`CREATE TABLE IF NOT EXISTS users (
		id            VARCHAR(64)   NOT NULL PRIMARY KEY,
		email         VARCHAR(254)  NOT NULL,
		password_hash VARCHAR(256)  NOT NULL,
		display_name  VARCHAR(128)  NOT NULL DEFAULT '',
		created_at    DATETIME(3)   NOT NULL,
		updated_at    DATETIME(3)   NOT NULL,
		UNIQUE KEY uk_email (email)
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`,
	`CREATE TABLE IF NOT EXISTS user_sessions (
		id         VARCHAR(128) NOT NULL PRIMARY KEY,
		user_id    VARCHAR(64)  NOT NULL,
		expires_at DATETIME(3)  NOT NULL,
		created_at DATETIME(3)  NOT NULL,
		INDEX idx_sess_user    (user_id),
		INDEX idx_sess_expires (expires_at)
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`,
	`CREATE TABLE IF NOT EXISTS user_settings (
		user_id               VARCHAR(64) NOT NULL PRIMARY KEY,
		community_auto_public TINYINT(1)  NOT NULL DEFAULT 0,
		prompt_public         TINYINT(1)  NOT NULL DEFAULT 0,
		updated_at            DATETIME(3) NOT NULL
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`,
}

// ── User operations ──────────────────────────────────────────────────────────

// Register creates a new user. Returns ErrEmailTaken if the email is already used.
func (s *Store) Register(email, password, displayName string) (*User, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" || password == "" {
		return nil, fmt.Errorf("auth: email and password are required")
	}
	hash, err := hashPassword(password)
	if err != nil {
		return nil, fmt.Errorf("auth: hash password: %w", err)
	}
	id, err := newID()
	if err != nil {
		return nil, err
	}
	now := time.Now()
	_, err = s.db.Exec(
		`INSERT INTO users (id, email, password_hash, display_name, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		id, email, hash, strings.TrimSpace(displayName), now, now,
	)
	if err != nil {
		var me *mysqldriver.MySQLError
		if errors.As(err, &me) && me.Number == 1062 {
			return nil, ErrEmailTaken
		}
		return nil, fmt.Errorf("auth: insert user: %w", err)
	}
	return &User{ID: id, Email: email, DisplayName: strings.TrimSpace(displayName), CreatedAt: now}, nil
}

// Login verifies credentials and returns the user. Returns ErrInvalidCredentials on mismatch.
func (s *Store) Login(email, password string) (*User, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	var u User
	var hash string
	err := s.db.QueryRow(
		`SELECT id, email, password_hash, display_name, created_at FROM users WHERE email = ?`, email,
	).Scan(&u.ID, &u.Email, &hash, &u.DisplayName, &u.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, ErrInvalidCredentials
	}
	if err != nil {
		return nil, fmt.Errorf("auth: query user: %w", err)
	}
	if !checkPassword(password, hash) {
		return nil, ErrInvalidCredentials
	}
	return &u, nil
}

// GetUser returns a user by ID, or (nil, false) if not found.
func (s *Store) GetUser(id string) (*User, bool) {
	var u User
	err := s.db.QueryRow(
		`SELECT id, email, display_name, created_at FROM users WHERE id = ?`, id,
	).Scan(&u.ID, &u.Email, &u.DisplayName, &u.CreatedAt)
	if err != nil {
		return nil, false
	}
	return &u, true
}

// GetSettings returns the privacy settings for a user. If no settings row exists,
// returns defaults (all false).
func (s *Store) GetSettings(userID string) (*UserSettings, error) {
	st := &UserSettings{UserID: userID}
	err := s.db.QueryRow(
		`SELECT community_auto_public, prompt_public FROM user_settings WHERE user_id = ?`, userID,
	).Scan(&st.CommunityAutoPublic, &st.PromptPublic)
	if err == sql.ErrNoRows {
		return st, nil
	}
	if err != nil {
		return nil, fmt.Errorf("auth: get settings: %w", err)
	}
	return st, nil
}

// UpdateSettings upserts privacy settings for a user.
func (s *Store) UpdateSettings(st *UserSettings) error {
	_, err := s.db.Exec(
		`INSERT INTO user_settings (user_id, community_auto_public, prompt_public, updated_at)
		 VALUES (?, ?, ?, ?)
		 ON DUPLICATE KEY UPDATE
		   community_auto_public = VALUES(community_auto_public),
		   prompt_public         = VALUES(prompt_public),
		   updated_at            = VALUES(updated_at)`,
		st.UserID,
		boolToInt(st.CommunityAutoPublic),
		boolToInt(st.PromptPublic),
		time.Now(),
	)
	if err != nil {
		return fmt.Errorf("auth: update settings: %w", err)
	}
	return nil
}

// MigrateAnonymousJobs reassigns generation_jobs rows that were created under
// sessionID (anonymous) to the now-authenticated userID. Called once on login
// so the user sees their pre-login history.
func (s *Store) MigrateAnonymousJobs(userID, sessionID string) {
	if userID == "" || sessionID == "" {
		return
	}
	_, _ = s.db.Exec(
		`UPDATE generation_jobs SET user_id = ?, session_id = ''
		 WHERE session_id = ? AND user_id = ''`,
		userID, sessionID,
	)
}

// ── Password hashing ─────────────────────────────────────────────────────────

// hashPassword returns a string of the form "pbkdf2-sha256:iter:salt_b64:key_b64".
func hashPassword(password string) (string, error) {
	salt := make([]byte, pbkdf2Salt)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := pbkdf2.Key([]byte(password), salt, pbkdf2Iter, pbkdf2Key, sha256.New)
	return fmt.Sprintf("pbkdf2-sha256:%d:%s:%s",
		pbkdf2Iter,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key),
	), nil
}

// checkPassword verifies a plaintext password against a stored hash string.
func checkPassword(password, stored string) bool {
	parts := strings.Split(stored, ":")
	if len(parts) != 4 || parts[0] != "pbkdf2-sha256" {
		return false
	}
	var iter int
	if _, err := fmt.Sscanf(parts[1], "%d", &iter); err != nil || iter < 1 {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[2])
	if err != nil {
		return false
	}
	expected, err := base64.RawStdEncoding.DecodeString(parts[3])
	if err != nil {
		return false
	}
	computed := pbkdf2.Key([]byte(password), salt, iter, len(expected), sha256.New)
	// constant-time compare
	if len(computed) != len(expected) {
		return false
	}
	diff := byte(0)
	for i := range computed {
		diff |= computed[i] ^ expected[i]
	}
	return diff == 0
}

// ── Helpers ──────────────────────────────────────────────────────────────────

func newID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("auth: generate id: %w", err)
	}
	return fmt.Sprintf("%x", b), nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
