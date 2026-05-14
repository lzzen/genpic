package api

import (
	"net/http"
	"strings"
	"unicode/utf8"

	"genpic/internal/auth"
	"genpic/internal/jobstore"
)

// Optional HTTP headers for attributing generation jobs to a caller.
// X-Genpic-User-Id is kept for direct API callers that set their own user id;
// authenticated web users are identified via the session cookie (see auth package).
const (
	hdrGenpicUser    = "X-Genpic-User-Id"
	hdrGenpicSession = "X-Genpic-Session"
)

const maxOwnerFieldBytes = 128

// callerScopeFromRequest builds a jobstore.OwnerScope for job listing/filtering.
//
// Resolution order for UserID:
//  1. Authenticated user from session cookie (auth.UserFromContext)
//  2. X-Genpic-User-Id header (legacy / direct API callers)
//
// When neither yields a UserID, SessionID is populated from X-Genpic-Session.
func callerScopeFromRequest(r *http.Request) jobstore.OwnerScope {
	if u := auth.UserFromContext(r.Context()); u != nil {
		return jobstore.OwnerScope{UserID: u.ID}
	}
	return jobstore.OwnerScope{
		UserID:    sanitizeOwnerField(r.Header.Get(hdrGenpicUser)),
		SessionID: sanitizeOwnerField(r.Header.Get(hdrGenpicSession)),
	}
}

// callerUserID returns the authenticated user's ID, or empty string.
func callerUserID(r *http.Request) string {
	if u := auth.UserFromContext(r.Context()); u != nil {
		return u.ID
	}
	return sanitizeOwnerField(r.Header.Get(hdrGenpicUser))
}

func sanitizeOwnerField(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if len(s) > maxOwnerFieldBytes {
		s = s[:maxOwnerFieldBytes]
	}
	if !utf8.ValidString(s) {
		return ""
	}
	return s
}
