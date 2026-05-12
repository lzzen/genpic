package api

import (
	"net/http"
	"strings"
	"unicode/utf8"

	"genpic/internal/jobstore"
)

// Optional HTTP headers for attributing generation jobs to a caller.
// Main-site SSO is out of scope; User-Id is an opaque string you may set later.
const (
	hdrGenpicUser    = "X-Genpic-User-Id"
	hdrGenpicSession = "X-Genpic-Session"
)

const maxOwnerFieldBytes = 128

// callerScopeFromRequest builds a jobstore.OwnerScope from identity headers.
func callerScopeFromRequest(r *http.Request) jobstore.OwnerScope {
	return jobstore.OwnerScope{
		UserID:    sanitizeOwnerField(r.Header.Get(hdrGenpicUser)),
		SessionID: sanitizeOwnerField(r.Header.Get(hdrGenpicSession)),
	}
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
