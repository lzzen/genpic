package api

import (
	"net/http"
	"strconv"
	"strings"

	"genpic/internal/jobstore"
	pkgerrors "genpic/pkg/errors"
)

const adminMaxPromptRunes = 200

// adminJobRow is a compact job row for GET /admin/jobs (no image payloads).
type adminJobRow struct {
	ID                string `json:"id"`
	Status            string `json:"status"`
	Provider          string `json:"provider,omitempty"`
	Model             string `json:"model,omitempty"`
	EffectiveProvider string `json:"effective_provider,omitempty"`
	EffectiveModel    string `json:"effective_model,omitempty"`
	Prompt            string `json:"prompt,omitempty"`
	CreatedAt  int64  `json:"created_at"`
	StartedAt  *int64 `json:"started_at,omitempty"`
	FinishedAt *int64 `json:"finished_at,omitempty"`
	ImageCount int    `json:"image_count"`
	UserID     string `json:"user_id,omitempty"`
	SessionID  string `json:"session_id,omitempty"`
	Error      string `json:"error,omitempty"`
}

func truncatePrompt(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	r := []rune(s)
	if len(r) <= adminMaxPromptRunes {
		return s
	}
	return string(r[:adminMaxPromptRunes]) + "…"
}

func toAdminJobRow(j *jobstore.Job) adminJobRow {
	row := adminJobRow{
		ID:                j.ID,
		Status:            string(j.Status),
		Provider:          j.Provider,
		Model:             j.Model,
		EffectiveProvider: j.EffectiveProvider,
		EffectiveModel:    j.EffectiveModel,
		Prompt:            truncatePrompt(j.Prompt),
		CreatedAt:  j.CreatedAt.Unix(),
		ImageCount: len(j.Images),
		UserID:     j.UserID,
		SessionID:  j.SessionID,
	}
	if !j.StartedAt.IsZero() {
		t := j.StartedAt.Unix()
		row.StartedAt = &t
	}
	if !j.FinishedAt.IsZero() {
		t := j.FinishedAt.Unix()
		row.FinishedAt = &t
	}
	if j.ErrorMsg != "" {
		row.Error = j.ErrorMsg
		if len(row.Error) > 300 {
			row.Error = row.Error[:300] + "…"
		}
	}
	return row
}

// HandleAdminJobs serves GET /admin/jobs — all jobs, newest first (unauthenticated; protect at proxy).
func HandleAdminJobs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	if jobStoreInstance == nil {
		Error(w, pkgerrors.New(http.StatusServiceUnavailable, pkgerrors.TypeInternal, "not_ready", "job store not initialised"))
		return
	}

	limit := 50
	if s := r.URL.Query().Get("limit"); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n > 0 {
			limit = n
		}
	}
	if limit > 500 {
		limit = 500
	}
	offset := 0
	if s := r.URL.Query().Get("offset"); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n >= 0 {
			offset = n
		}
	}

	jobs, total := jobStoreInstance.AdminList(limit, offset)
	rows := make([]adminJobRow, 0, len(jobs))
	for _, j := range jobs {
		if j == nil {
			continue
		}
		rows = append(rows, toAdminJobRow(j))
	}

	JSON(w, http.StatusOK, map[string]any{
		"object": "admin.job_list",
		"data":   rows,
		"total":  total,
		"limit":  limit,
		"offset": offset,
	})
}

// HandleAdminStats serves GET /admin/stats — aggregate job statistics (unauthenticated; protect at proxy).
func HandleAdminStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	if jobStoreInstance == nil {
		JSON(w, http.StatusOK, jobstore.AdminStatsSummary{ByProvider: map[string]int64{}})
		return
	}
	s := jobStoreInstance.AdminStats()
	if s.ByProvider == nil {
		s.ByProvider = map[string]int64{}
	}
	JSON(w, http.StatusOK, map[string]any{
		"object": "admin.stats",
		"stats":  s,
	})
}
