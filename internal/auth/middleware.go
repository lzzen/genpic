package auth

import (
	"context"
	"net/http"
)

type ctxUserKey struct{}

// UserFromContext returns the authenticated user injected by OptionalAuth or
// RequireAuth middleware, or nil when the request is unauthenticated.
func UserFromContext(ctx context.Context) *User {
	u, _ := ctx.Value(ctxUserKey{}).(*User)
	return u
}

// withUser returns a child context carrying u.
func withUser(ctx context.Context, u *User) context.Context {
	return context.WithValue(ctx, ctxUserKey{}, u)
}

// ContextWithUser returns a context carrying u (for tests and internal wiring).
func ContextWithUser(ctx context.Context, u *User) context.Context {
	return withUser(ctx, u)
}

// OptionalAuth reads the session cookie and, when valid, injects the User into
// the request context. Unauthenticated requests are passed through unchanged.
func OptionalAuth(store *Store) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if u := resolveUser(store, r); u != nil {
				r = r.WithContext(withUser(r.Context(), u))
			}
			next.ServeHTTP(w, r)
		})
	}
}

// RequireAuth is like OptionalAuth but returns 401 when no valid session is present.
func RequireAuth(store *Store) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			u := resolveUser(store, r)
			if u == nil {
				http.Error(w, `{"error":{"type":"auth_error","code":"unauthenticated","message":"login required"}}`, http.StatusUnauthorized)
				return
			}
			r = r.WithContext(withUser(r.Context(), u))
			next.ServeHTTP(w, r)
		})
	}
}

// resolveUser reads the session cookie and validates it against the store.
// Returns nil when the store is nil, cookie is absent, or token is invalid.
func resolveUser(store *Store, r *http.Request) *User {
	if store == nil {
		return nil
	}
	c, err := r.Cookie(SessionCookie)
	if err != nil || c.Value == "" {
		return nil
	}
	u, err := store.ValidateSession(c.Value)
	if err != nil {
		return nil
	}
	return u
}
