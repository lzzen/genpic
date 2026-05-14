package auth

import (
	"crypto/rand"
	"fmt"
	"time"
)

// CreateSession generates a cryptographically random session token, stores it in
// user_sessions with the store's configured TTL, and returns the token string.
func (s *Store) CreateSession(userID string) (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("auth: generate session token: %w", err)
	}
	token := fmt.Sprintf("%x", b)
	now := time.Now()
	expires := now.Add(s.sessionTTL)
	_, err := s.db.Exec(
		`INSERT INTO user_sessions (id, user_id, expires_at, created_at) VALUES (?, ?, ?, ?)`,
		token, userID, expires, now,
	)
	if err != nil {
		return "", fmt.Errorf("auth: insert session: %w", err)
	}
	return token, nil
}

// ValidateSession looks up the session token and returns the associated user.
// Returns ErrSessionNotFound when the token does not exist or has expired.
func (s *Store) ValidateSession(token string) (*User, error) {
	if token == "" {
		return nil, ErrSessionNotFound
	}
	var u User
	err := s.db.QueryRow(`
		SELECT u.id, u.email, u.display_name, u.created_at
		FROM user_sessions sess
		JOIN users u ON u.id = sess.user_id
		WHERE sess.id = ? AND sess.expires_at > NOW()`,
		token,
	).Scan(&u.ID, &u.Email, &u.DisplayName, &u.CreatedAt)
	if err != nil {
		return nil, ErrSessionNotFound
	}
	return &u, nil
}

// DeleteSession removes a session token from the database (logout).
func (s *Store) DeleteSession(token string) {
	if token == "" {
		return
	}
	_, _ = s.db.Exec(`DELETE FROM user_sessions WHERE id = ?`, token)
}

// DeleteExpiredSessions removes all expired session rows. Can be called
// periodically (e.g. from a background goroutine) to keep the table tidy.
func (s *Store) DeleteExpiredSessions() {
	_, _ = s.db.Exec(`DELETE FROM user_sessions WHERE expires_at <= NOW()`)
}
