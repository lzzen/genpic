package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestParseAdminModelStatsWindowDays(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/admin/model-stats?days=14", nil)
	since, until, err := parseAdminModelStatsWindow(req)
	if err != nil {
		t.Fatal(err)
	}
	if !until.After(since) {
		t.Fatal("bad window")
	}
	if until.Sub(since) < 13*24*time.Hour || until.Sub(since) > 15*24*time.Hour {
		t.Fatalf("unexpected span %v", until.Sub(since))
	}
}
