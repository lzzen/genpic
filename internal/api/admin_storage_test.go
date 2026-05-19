package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"genpic/internal/auth"
)

func TestHandleAdminListUserStorage_Unauthorized(t *testing.T) {
	SetAdminEmails([]string{"admin@example.com"})
	t.Cleanup(func() { SetAdminEmails(nil) })

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/admin/users/storage", nil)
	HandleAdminListUserStorage(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d", rr.Code)
	}
}

func TestHandleAdminListUserStorage_Forbidden(t *testing.T) {
	SetAdminEmails([]string{"admin@example.com"})
	t.Cleanup(func() { SetAdminEmails(nil) })

	u := &auth.User{ID: "u1", Email: "user@example.com"}
	req := httptest.NewRequest(http.MethodGet, "/api/admin/users/storage", nil)
	req = req.WithContext(auth.ContextWithUser(req.Context(), u))
	rr := httptest.NewRecorder()
	HandleAdminListUserStorage(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("status=%d", rr.Code)
	}
}

func TestHandleAdminPatchUserStorage_InvalidBody(t *testing.T) {
	SetAdminEmails([]string{"admin@example.com"})
	t.Cleanup(func() { SetAdminEmails(nil) })

	u := &auth.User{ID: "a1", Email: "admin@example.com"}
	body, _ := json.Marshal(map[string]any{
		"user_id":     "u1",
		"quota_bytes": 100,
		"delta_bytes": 50,
	})
	req := httptest.NewRequest(http.MethodPatch, "/api/admin/users/storage", bytes.NewReader(body))
	req = req.WithContext(auth.ContextWithUser(req.Context(), u))
	rr := httptest.NewRecorder()
	HandleAdminPatchUserStorage(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
}

