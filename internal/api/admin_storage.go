package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"genpic/internal/userstorage"
	pkgerrors "genpic/pkg/errors"
)

func adminStorageJSON(row userstorage.UserStorageRow) map[string]any {
	return map[string]any{
		"user_id":         row.UserID,
		"email":           row.Email,
		"display_name":    row.DisplayName,
		"used_bytes":      row.UsedBytes,
		"quota_bytes":     row.QuotaBytes,
		"remaining_bytes": userstorage.RemainingBytes(row.UsedBytes, row.QuotaBytes),
	}
}

func requireAdminActor(w http.ResponseWriter, r *http.Request) bool {
	actor := CurrentUser(r)
	if actor == nil {
		Error(w, pkgerrors.New(http.StatusUnauthorized, pkgerrors.TypeAuthentication, "unauthenticated", "login required"))
		return false
	}
	if !isAdminUser(actor) {
		Error(w, pkgerrors.New(http.StatusForbidden, pkgerrors.TypePermission, "forbidden", "administrator privileges required"))
		return false
	}
	return true
}

func requireQuotaDB(w http.ResponseWriter) bool {
	if getQuotaDB() == nil {
		Error(w, pkgerrors.New(http.StatusServiceUnavailable, pkgerrors.TypeInternal, "storage_disabled", "user storage requires a database"))
		return false
	}
	return true
}

// HandleAdminListUserStorage serves GET /api/admin/users/storage — paginated user list with quota fields.
func HandleAdminListUserStorage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	if !requireAdminActor(w, r) || !requireQuotaDB(w) {
		return
	}

	limit := 30
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
	q := strings.TrimSpace(r.URL.Query().Get("q"))

	rows, total, err := userstorage.ListUsersStorage(r.Context(), getQuotaDB(), q, limit, offset)
	if err != nil {
		Error(w, pkgerrors.New(http.StatusInternalServerError, pkgerrors.TypeInternal, "admin_storage_list_error", err.Error()))
		return
	}
	data := make([]map[string]any, 0, len(rows))
	for i := range rows {
		data = append(data, adminStorageJSON(rows[i]))
	}
	JSON(w, http.StatusOK, map[string]any{
		"object": "admin.user_storage_list",
		"data":   data,
		"total":  total,
		"limit":  limit,
		"offset": offset,
	})
}

type adminPatchUserStorageBody struct {
	UserID     string `json:"user_id"`
	QuotaBytes *int64 `json:"quota_bytes"`
	DeltaBytes *int64 `json:"delta_bytes"`
}

// HandleAdminPatchUserStorage serves PATCH /api/admin/users/storage — set or adjust quota for one user.
func HandleAdminPatchUserStorage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPatch {
		w.Header().Set("Allow", http.MethodPatch)
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	if !requireAdminActor(w, r) {
		return
	}

	var body adminPatchUserStorageBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		Error(w, pkgerrors.BadRequest("parse_error", "could not parse request body"))
		return
	}
	userID := strings.TrimSpace(body.UserID)
	if userID == "" {
		Error(w, pkgerrors.BadRequest("missing_field", "user_id is required"))
		return
	}
	hasQuota := body.QuotaBytes != nil
	hasDelta := body.DeltaBytes != nil
	if hasQuota == hasDelta {
		Error(w, pkgerrors.BadRequest("invalid_quota", "provide exactly one of quota_bytes or delta_bytes"))
		return
	}

	if !requireQuotaDB(w) {
		return
	}

	db := getQuotaDB()
	ctx := r.Context()
	if hasQuota {
		if *body.QuotaBytes < 0 {
			Error(w, pkgerrors.BadRequest("invalid_quota", "quota_bytes must be non-negative"))
			return
		}
		if err := userstorage.AdminSetQuota(ctx, db, userID, *body.QuotaBytes); err != nil {
			if errors.Is(err, userstorage.ErrUserNotFound) {
				Error(w, pkgerrors.NotFound("user"))
				return
			}
			if strings.Contains(err.Error(), "non-negative") {
				Error(w, pkgerrors.BadRequest("invalid_quota", err.Error()))
				return
			}
			Error(w, pkgerrors.New(http.StatusInternalServerError, pkgerrors.TypeInternal, "admin_storage_patch_error", err.Error()))
			return
		}
	} else {
		if _, err := userstorage.AdminAdjustQuota(ctx, db, userID, *body.DeltaBytes); err != nil {
			if errors.Is(err, userstorage.ErrUserNotFound) {
				Error(w, pkgerrors.NotFound("user"))
				return
			}
			if strings.Contains(err.Error(), "negative") {
				Error(w, pkgerrors.BadRequest("invalid_quota", err.Error()))
				return
			}
			Error(w, pkgerrors.New(http.StatusInternalServerError, pkgerrors.TypeInternal, "admin_storage_patch_error", err.Error()))
			return
		}
	}

	row, err := userstorage.UsageRow(ctx, db, userID)
	if err != nil {
		Error(w, pkgerrors.New(http.StatusInternalServerError, pkgerrors.TypeInternal, "admin_storage_patch_error", err.Error()))
		return
	}
	JSON(w, http.StatusOK, map[string]any{
		"object": "admin.user_storage",
		"user":   adminStorageJSON(row),
	})
}
