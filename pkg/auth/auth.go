package auth

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// Identity represents an authenticated caller. It is stored on the request
// context so handlers can read it without re-parsing the Authorization header.
type Identity struct {
	// KeyID is the opaque identifier of the API key (not the raw secret).
	KeyID string
	// UserID is the owning user (empty for machine-to-machine keys with no owner).
	UserID string
	// Scopes is the set of allowed model IDs; nil means "all models allowed".
	Scopes []string
	// RPMLimit is the per-minute request cap for this key (0 = unlimited).
	RPMLimit int
	// ExpiresAt is when the key expires (zero value = never).
	ExpiresAt time.Time
}

// HasScope reports whether the identity is allowed to call the given model.
// If Scopes is nil (no restriction), all models are permitted.
func (id *Identity) HasScope(modelID string) bool {
	if id.Scopes == nil {
		return true
	}
	for _, s := range id.Scopes {
		if s == modelID {
			return true
		}
	}
	return false
}

type contextKey struct{}

// WithIdentity stores the identity on the context for downstream handlers.
func WithIdentity(ctx context.Context, id *Identity) context.Context {
	return context.WithValue(ctx, contextKey{}, id)
}

// IdentityFromContext retrieves the identity stored by middleware.
// Returns (nil, false) when the request was not authenticated.
func IdentityFromContext(ctx context.Context) (*Identity, bool) {
	id, ok := ctx.Value(contextKey{}).(*Identity)
	return id, ok && id != nil
}

// BearerToken extracts the raw token from an "Authorization: Bearer …" header.
// Returns ("", false) when the header is absent or malformed.
func BearerToken(r *http.Request) (string, bool) {
	v := r.Header.Get("Authorization")
	if !strings.HasPrefix(v, "Bearer ") {
		return "", false
	}
	tok := strings.TrimPrefix(v, "Bearer ")
	tok = strings.TrimSpace(tok)
	if tok == "" {
		return "", false
	}
	return tok, true
}

// Validator is the interface the HTTP middleware calls to verify a raw token.
// Implementations query the DB and compare against the stored hash.
type Validator interface {
	// Validate returns the Identity for the given raw token, or an error if
	// the token is invalid, revoked, or expired.
	Validate(ctx context.Context, rawToken string) (*Identity, error)
}

// Middleware returns an http.Handler that enforces bearer-token authentication.
// On success it stores the Identity on the context. On failure it writes an
// OpenAI-compatible 401 JSON response.
func Middleware(v Validator, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tok, ok := BearerToken(r)
		if !ok {
			writeUnauthorized(w, "missing or malformed Authorization header")
			return
		}
		id, err := v.Validate(r.Context(), tok)
		if err != nil {
			writeUnauthorized(w, "invalid API key")
			return
		}
		next.ServeHTTP(w, r.WithContext(WithIdentity(r.Context(), id)))
	})
}

func writeUnauthorized(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	_, _ = w.Write([]byte(`{"error":{"type":"authentication_error","message":"` + msg + `"}}`))
}

// NoopValidator is a test double that accepts any non-empty token. Never use
// in production; wire a real DB-backed implementation instead.
type NoopValidator struct{}

func (NoopValidator) Validate(_ context.Context, token string) (*Identity, error) {
	if token == "" {
		return nil, errInvalidToken
	}
	return &Identity{KeyID: "noop-" + token}, nil
}

var errInvalidToken = fmt.Errorf("invalid token")
