package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"genpic/internal/auth"
	"genpic/internal/jobstore"
)

func TestHandleAdminModelStats_Unauthorized(t *testing.T) {
	ctx := context.Background()
	st := jobstore.NewMemory(ctx, time.Hour)
	SetJobStore(st)
	SetAdminEmails([]string{"admin@example.com"})
	t.Cleanup(func() {
		SetJobStore(nil)
		SetAdminEmails(nil)
	})

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/admin/model-stats?days=7", nil)
	HandleAdminModelStats(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
}

func TestHandleAdminModelStats_Forbidden(t *testing.T) {
	ctx := context.Background()
	st := jobstore.NewMemory(ctx, time.Hour)
	SetJobStore(st)
	SetAdminEmails([]string{"admin@example.com"})
	t.Cleanup(func() {
		SetJobStore(nil)
		SetAdminEmails(nil)
	})

	u := &auth.User{ID: "u1", Email: "user@example.com"}
	req := httptest.NewRequest(http.MethodGet, "/api/admin/model-stats?days=7", nil)
	req = req.WithContext(auth.ContextWithUser(req.Context(), u))
	rr := httptest.NewRecorder()
	HandleAdminModelStats(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
}

func TestHandleAdminModelStatsTimeseries_InvalidGranularity(t *testing.T) {
	ctx := context.Background()
	st := jobstore.NewMemory(ctx, time.Hour)
	SetJobStore(st)
	SetAdminEmails([]string{"admin@example.com"})
	t.Cleanup(func() {
		SetJobStore(nil)
		SetAdminEmails(nil)
	})

	u := &auth.User{ID: "a1", Email: "admin@example.com"}
	req := httptest.NewRequest(http.MethodGet, "/api/admin/model-stats/timeseries?days=30&granularity=hour", nil)
	req = req.WithContext(auth.ContextWithUser(req.Context(), u))
	rr := httptest.NewRecorder()
	HandleAdminModelStatsTimeseries(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
}
