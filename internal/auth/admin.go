package auth

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

var (
	// ErrUserNotFound is returned when no user matches an admin lookup.
	ErrUserNotFound = errors.New("auth: user not found")
	// ErrPasswordTooShort matches registration rules (minimum 8 characters).
	ErrPasswordTooShort = errors.New("auth: password must be at least 8 characters")
)

// ResolveAdminTargetUser resolves exactly one of userID or email to a user row.
// Pass empty string for the unused field. Returns ErrUserNotFound when no match.
func (s *Store) ResolveAdminTargetUser(userID, email string) (*User, error) {
	uid := strings.TrimSpace(userID)
	em := strings.ToLower(strings.TrimSpace(email))
	if uid != "" && em != "" {
		return nil, fmt.Errorf("auth: specify only one of user_id or email")
	}
	if uid != "" {
		u, ok := s.GetUser(uid)
		if !ok {
			return nil, ErrUserNotFound
		}
		return u, nil
	}
	if em != "" {
		var u User
		err := s.db.QueryRow(
			`SELECT id, email, display_name, created_at FROM users WHERE email = ?`, em,
		).Scan(&u.ID, &u.Email, &u.DisplayName, &u.CreatedAt)
		if err == sql.ErrNoRows {
			return nil, ErrUserNotFound
		}
		if err != nil {
			return nil, fmt.Errorf("auth: lookup user by email: %w", err)
		}
		return &u, nil
	}
	return nil, fmt.Errorf("auth: user_id or email is required")
}

// AdminSetPassword assigns a new password hash and clears all sessions for the user.
func (s *Store) AdminSetPassword(userID, newPassword string) error {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return fmt.Errorf("auth: empty user id")
	}
	if len(strings.TrimSpace(newPassword)) < 8 {
		return ErrPasswordTooShort
	}
	hash, err := hashPassword(newPassword)
	if err != nil {
		return fmt.Errorf("auth: hash password: %w", err)
	}
	now := time.Now()
	res, err := s.db.Exec(
		`UPDATE users SET password_hash = ?, updated_at = ? WHERE id = ?`,
		hash, now, userID,
	)
	if err != nil {
		return fmt.Errorf("auth: update password: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("auth: rows affected: %w", err)
	}
	if n == 0 {
		return ErrUserNotFound
	}
	s.DeleteAllSessionsForUser(userID)
	return nil
}
