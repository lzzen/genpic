package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"genpic/internal/auth"
	"genpic/internal/templatestore"
	pkgerrors "genpic/pkg/errors"
)

type adminResetPasswordRequest struct {
	UserID      string `json:"user_id"`
	Email       string `json:"email"`
	NewPassword string `json:"new_password"`
}

// HandleAdminResetPassword serves POST /api/admin/users/reset-password — admin only; invalidates target sessions.
func HandleAdminResetPassword(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	if authStoreInstance == nil {
		Error(w, pkgerrors.New(http.StatusServiceUnavailable, pkgerrors.TypeInternal, "auth_disabled", "auth not available"))
		return
	}
	actor := CurrentUser(r)
	if actor == nil {
		Error(w, pkgerrors.New(http.StatusUnauthorized, pkgerrors.TypeAuthentication, "unauthenticated", "login required"))
		return
	}
	if !isAdminUser(actor) {
		Error(w, pkgerrors.New(http.StatusForbidden, pkgerrors.TypePermission, "forbidden", "administrator privileges required"))
		return
	}

	var req adminResetPasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		Error(w, pkgerrors.BadRequest("parse_error", "could not parse request body"))
		return
	}
	uid := strings.TrimSpace(req.UserID)
	em := strings.TrimSpace(req.Email)
	if uid == "" && em == "" {
		Error(w, pkgerrors.BadRequest("invalid_target", "provide exactly one of user_id or email"))
		return
	}
	if uid != "" && em != "" {
		Error(w, pkgerrors.BadRequest("invalid_target", "provide exactly one of user_id or email"))
		return
	}
	newPw := req.NewPassword
	if strings.TrimSpace(newPw) == "" {
		Error(w, pkgerrors.BadRequest("missing_field", "new_password is required"))
		return
	}

	target, err := authStoreInstance.ResolveAdminTargetUser(uid, em)
	if err != nil {
		if errors.Is(err, auth.ErrUserNotFound) {
			Error(w, pkgerrors.NotFound("user"))
			return
		}
		Error(w, pkgerrors.BadRequest("invalid_target", err.Error()))
		return
	}

	if err := authStoreInstance.AdminSetPassword(target.ID, newPw); err != nil {
		if errors.Is(err, auth.ErrPasswordTooShort) {
			Error(w, pkgerrors.BadRequest("password_too_short", "password must be at least 8 characters"))
			return
		}
		if errors.Is(err, auth.ErrUserNotFound) {
			Error(w, pkgerrors.NotFound("user"))
			return
		}
		Error(w, pkgerrors.New(http.StatusInternalServerError, pkgerrors.TypeInternal, "password_reset_error", err.Error()))
		return
	}

	JSON(w, http.StatusOK, map[string]any{
		"object":  "admin.password_reset",
		"user_id": target.ID,
		"email":   target.Email,
	})
}

// HandleAdminListTemplates serves GET /api/admin/templates — admin only; paginated all templates.
func HandleAdminListTemplates(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	if templateStoreInstance == nil {
		Error(w, pkgerrors.New(http.StatusServiceUnavailable, pkgerrors.TypeInternal, "templates_disabled", "templates require a database"))
		return
	}
	actor := CurrentUser(r)
	if actor == nil {
		Error(w, pkgerrors.New(http.StatusUnauthorized, pkgerrors.TypeAuthentication, "unauthenticated", "login required"))
		return
	}
	if !isAdminUser(actor) {
		Error(w, pkgerrors.New(http.StatusForbidden, pkgerrors.TypePermission, "forbidden", "administrator privileges required"))
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
	visFilter := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("visibility")))

	list, total, err := templateStoreInstance.ListAllForAdmin(r.Context(), limit, offset, visFilter)
	if err != nil {
		Error(w, pkgerrors.New(http.StatusInternalServerError, pkgerrors.TypeInternal, "admin_template_list_error", err.Error()))
		return
	}

	rows := make([]map[string]any, 0, len(list))
	for i := range list {
		row := list[i]
		rows = append(rows, map[string]any{
			"id":               row.ID,
			"user_id":          row.UserID,
			"owner_email":      row.OwnerEmail,
			"visibility":       row.Visibility,
			"title":            row.Title,
			"primary_model":    row.PrimaryModel,
			"provider":         row.Provider,
			"prompt_preview":   row.PromptPreview,
			"result_image_url": row.ResultImageURL,
			"created_at":       row.CreatedAt.Unix(),
		})
	}

	JSON(w, http.StatusOK, map[string]any{
		"object": "admin.template_list",
		"data":   rows,
		"total":  total,
		"limit":  limit,
		"offset": offset,
	})
}

type adminTemplateVisibilityBody struct {
	Visibility string `json:"visibility"`
}

// HandleAdminPutTemplateVisibility serves PUT /api/admin/templates/{id}/visibility — admin only.
func HandleAdminPutTemplateVisibility(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		w.Header().Set("Allow", http.MethodPut)
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	if templateStoreInstance == nil {
		Error(w, pkgerrors.New(http.StatusServiceUnavailable, pkgerrors.TypeInternal, "templates_disabled", "templates require a database"))
		return
	}
	actor := CurrentUser(r)
	if actor == nil {
		Error(w, pkgerrors.New(http.StatusUnauthorized, pkgerrors.TypeAuthentication, "unauthenticated", "login required"))
		return
	}
	if !isAdminUser(actor) {
		Error(w, pkgerrors.New(http.StatusForbidden, pkgerrors.TypePermission, "forbidden", "administrator privileges required"))
		return
	}

	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" {
		Error(w, pkgerrors.BadRequest("missing_path_param", "id is required in path"))
		return
	}

	var body adminTemplateVisibilityBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		Error(w, pkgerrors.BadRequest("parse_error", "could not parse request body"))
		return
	}
	vis := strings.ToLower(strings.TrimSpace(body.Visibility))

	if err := templateStoreInstance.AdminSetTemplateVisibility(r.Context(), id, vis); err != nil {
		if errors.Is(err, templatestore.ErrTemplateNotFound) {
			Error(w, pkgerrors.NotFound("template"))
			return
		}
		if strings.Contains(err.Error(), "invalid visibility") {
			Error(w, pkgerrors.BadRequest("invalid_visibility", "visibility must be public or private"))
			return
		}
		Error(w, pkgerrors.New(http.StatusInternalServerError, pkgerrors.TypeInternal, "admin_template_visibility_error", err.Error()))
		return
	}

	JSON(w, http.StatusOK, map[string]any{
		"object":     "admin.template_visibility",
		"id":         id,
		"visibility": vis,
	})
}

func parseFlexibleTime(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, fmt.Errorf("empty time")
	}
	if n, err := strconv.ParseInt(s, 10, 64); err == nil {
		if n > 1_000_000_000_000 {
			return time.UnixMilli(n), nil
		}
		return time.Unix(n, 0), nil
	}
	return time.Parse(time.RFC3339, s)
}

// parseAdminModelStatsWindow returns [since, until) from query params.
// Default: last `days` (default 7) until now. Explicit since/until override days.
func parseAdminModelStatsWindow(r *http.Request) (since, until time.Time, err error) {
	q := r.URL.Query()
	days := 7
	if s := q.Get("days"); s != "" {
		if n, e := strconv.Atoi(s); e == nil && n > 0 {
			days = n
		}
	}
	if days > 366 {
		days = 366
	}
	until = time.Now()
	since = until.Add(-time.Duration(days) * 24 * time.Hour)

	if ss := strings.TrimSpace(q.Get("since")); ss != "" {
		t, e := parseFlexibleTime(ss)
		if e != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("since: %w", e)
		}
		since = t
	}
	if us := strings.TrimSpace(q.Get("until")); us != "" {
		t, e := parseFlexibleTime(us)
		if e != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("until: %w", e)
		}
		until = t
	}
	if !until.After(since) {
		return time.Time{}, time.Time{}, fmt.Errorf("until must be after since")
	}
	return since, until, nil
}

func parseModelsCSV(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	var out []string
	for _, p := range strings.Split(s, ",") {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// HandleAdminModelStats serves GET /api/admin/model-stats — admin only.
func HandleAdminModelStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	if jobStoreInstance == nil {
		Error(w, pkgerrors.New(http.StatusServiceUnavailable, pkgerrors.TypeInternal, "not_ready", "job store not initialised"))
		return
	}
	actor := CurrentUser(r)
	if actor == nil {
		Error(w, pkgerrors.New(http.StatusUnauthorized, pkgerrors.TypeAuthentication, "unauthenticated", "login required"))
		return
	}
	if !isAdminUser(actor) {
		Error(w, pkgerrors.New(http.StatusForbidden, pkgerrors.TypePermission, "forbidden", "administrator privileges required"))
		return
	}
	since, until, err := parseAdminModelStatsWindow(r)
	if err != nil {
		Error(w, pkgerrors.BadRequest("invalid_window", err.Error()))
		return
	}
	stats := jobStoreInstance.AdminModelStats(since, until)
	JSON(w, http.StatusOK, map[string]any{
		"object": "admin.model_stats",
		"stats":  stats,
	})
}

// HandleAdminModelStatsTimeseries serves GET /api/admin/model-stats/timeseries — admin only.
func HandleAdminModelStatsTimeseries(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	if jobStoreInstance == nil {
		Error(w, pkgerrors.New(http.StatusServiceUnavailable, pkgerrors.TypeInternal, "not_ready", "job store not initialised"))
		return
	}
	actor := CurrentUser(r)
	if actor == nil {
		Error(w, pkgerrors.New(http.StatusUnauthorized, pkgerrors.TypeAuthentication, "unauthenticated", "login required"))
		return
	}
	if !isAdminUser(actor) {
		Error(w, pkgerrors.New(http.StatusForbidden, pkgerrors.TypePermission, "forbidden", "administrator privileges required"))
		return
	}
	since, until, err := parseAdminModelStatsWindow(r)
	if err != nil {
		Error(w, pkgerrors.BadRequest("invalid_window", err.Error()))
		return
	}
	g := strings.TrimSpace(r.URL.Query().Get("granularity"))
	if g == "" {
		g = "day"
	}
	gl := strings.ToLower(g)
	if gl != "day" && gl != "hour" {
		Error(w, pkgerrors.BadRequest("invalid_granularity", "granularity must be day or hour"))
		return
	}
	if gl == "hour" && until.Sub(since) > 7*24*time.Hour+time.Millisecond {
		Error(w, pkgerrors.BadRequest("invalid_granularity", "hour granularity requires window <= 7 days"))
		return
	}
	models := parseModelsCSV(r.URL.Query().Get("models"))
	stats := jobStoreInstance.AdminModelStatsTimeseries(since, until, gl, models)
	JSON(w, http.StatusOK, map[string]any{
		"object": "admin.model_stats_timeseries",
		"stats":  stats,
	})
}
