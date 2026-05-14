// Package auth implements user registration/login, server-side sessions,
// and HTTP middleware for the Genpic platform.
//
// Password storage uses PBKDF2-HMAC-SHA256 via crypto/pbkdf2 (Go 1.24+).
//
// Session tokens are random hex strings stored in user_sessions and sent as the
// HTTP-only cookie genpic_session (SameSite=Lax).
//
// Schema: SQL embedded under internal/dbmigrate/migrations (applied by cmd/genpic).
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

var (
	ErrEmailTaken         = errors.New("auth: email already registered")
	ErrInvalidCredentials = errors.New("auth: invalid email or password")
	ErrSessionNotFound    = errors.New("auth: session not found or expired")
)

const (
	pbkdf2Iter      = 100_000
	pbkdf2SaltBytes = 16
	pbkdf2KeyBytes  = 32
	DefaultSessionTTL = 30 * 24 * time.Hour
	SessionCookie      = "genpic_session"
)

type User struct {
	ID          string
	Email       string
	DisplayName string
	CreatedAt   time.Time
}

type UserSettings struct {
	UserID              string `json:"user_id"`
	CommunityAutoPublic bool   `json:"community_auto_public"`
	PromptPublic        bool   `json:"prompt_public"`
}

type Store struct {
	db         *sql.DB
	sessionTTL time.Duration
}

func NewStore(db *sql.DB, sessionTTL time.Duration) (*Store, error) {
	if db == nil {
		return nil, fmt.Errorf("auth: nil db")
	}
	if sessionTTL <= 0 {
		sessionTTL = DefaultSessionTTL
	}
	return &Store{db: db, sessionTTL: sessionTTL}, nil
}

func (s *Store) SessionTTL() time.Duration { return s.sessionTTL }

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

func hashPassword(password string) (string, error) {
	salt := make([]byte, pbkdf2SaltBytes)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key, err := pbkdf2.Key(sha256.New, password, salt, pbkdf2Iter, pbkdf2KeyBytes)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("pbkdf2-sha256:%d:%s:%s",
		pbkdf2Iter,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key),
	), nil
}

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
	computed, err := pbkdf2.Key(sha256.New, password, salt, iter, len(expected))
	if err != nil {
		return false
	}
	if len(computed) != len(expected) {
		return false
	}
	diff := byte(0)
	for i := range computed {
		diff |= computed[i] ^ expected[i]
	}
	return diff == 0
}

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

// HashPassword derives a stored credential string (for tests or tooling).
func HashPassword(password string) (string, error) { return hashPassword(password) }

// CheckPassword verifies a password against a stored hash string.
func CheckPassword(password, stored string) bool { return checkPassword(password, stored) }
