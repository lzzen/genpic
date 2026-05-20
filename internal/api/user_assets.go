package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"genpic/internal/auth"
	"genpic/internal/userstorage"
	pkgerrors "genpic/pkg/errors"
	"genpic/pkg/provider"
)

type userAssetItem struct {
	ID        string `json:"id"`
	URL       string `json:"url"`
	ThumbURL  string `json:"thumb_url,omitempty"`
	Kind      string `json:"kind"`
	JobID     string `json:"job_id,omitempty"`
	ByteSize  int64  `json:"byte_size"`
	CreatedAt int64  `json:"created_at"`
}

func kindsForAssetTab(tab string) []string {
	switch strings.ToLower(strings.TrimSpace(tab)) {
	case "upload", "reference", "user":
		return []string{"reference"}
	case "generated", "output", "ai":
		return []string{"output"}
	default:
		return nil
	}
}

func requireLoggedInUser(w http.ResponseWriter, r *http.Request) *auth.User {
	if getObjectStore() == nil || getQuotaDB() == nil {
		Error(w, pkgerrors.New(http.StatusServiceUnavailable, pkgerrors.TypeInternal, "storage_disabled", "object storage requires database and OSS configuration"))
		return nil
	}
	u := CurrentUser(r)
	if u == nil {
		Error(w, pkgerrors.New(http.StatusUnauthorized, pkgerrors.TypeAuthentication, "unauthenticated", "login required to use your resource library"))
		return nil
	}
	return u
}

// HandleListUserAssets serves GET /api/user/assets — paginated resource library for the logged-in user.
func HandleListUserAssets(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	u := requireLoggedInUser(w, r)
	if u == nil {
		return
	}

	tab := r.URL.Query().Get("tab")
	kinds := kindsForAssetTab(tab)
	if tk := strings.TrimSpace(r.URL.Query().Get("kind")); tk != "" {
		kinds = []string{tk}
	}

	limit := 48
	if s := r.URL.Query().Get("limit"); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n > 0 {
			limit = n
		}
	}
	cursor := strings.TrimSpace(r.URL.Query().Get("cursor"))

	rows, next, err := userstorage.ListUserAssets(r.Context(), getQuotaDB(), u.ID, kinds, limit, cursor)
	if err != nil {
		Error(w, pkgerrors.New(http.StatusInternalServerError, pkgerrors.TypeInternal, "user_assets_list_error", err.Error()))
		return
	}

	items := make([]userAssetItem, 0, len(rows))
	for _, row := range rows {
		if row.Kind == "output_thumb" {
			continue
		}
		pub, err := resolveObjectHTTPSURL(r.Context(), row.ObjectKey)
		if err != nil {
			continue
		}
		item := userAssetItem{
			ID:        row.ID,
			URL:       pub,
			Kind:      row.Kind,
			JobID:     row.JobID,
			ByteSize:  row.ByteSize,
			CreatedAt: row.CreatedAt.Unix(),
		}
		if row.Kind == "output" {
			if tk := userstorage.ThumbKeyForOutput(row.ObjectKey); tk != "" {
				if tu, err := resolveObjectHTTPSURL(r.Context(), tk); err == nil {
					item.ThumbURL = tu
				}
			}
		}
		if item.ThumbURL == "" {
			item.ThumbURL = item.URL
		}
		items = append(items, item)
	}

	JSON(w, http.StatusOK, map[string]any{
		"object":      "list",
		"data":        items,
		"next_cursor": next,
		"tab":         tab,
	})
}

type uploadUserAssetsRequest struct {
	ReferenceImages []struct {
		MIMEType string `json:"mime_type"`
		B64JSON  string `json:"b64_json"`
	} `json:"reference_images"`
}

// HandleUploadUserAssets serves POST /api/user/assets — upload reference images into the user's library.
func HandleUploadUserAssets(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	u := requireLoggedInUser(w, r)
	if u == nil {
		return
	}

	var body uploadUserAssetsRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		Error(w, pkgerrors.BadRequest("parse_error", "could not parse request body"))
		return
	}
	if len(body.ReferenceImages) == 0 {
		Error(w, pkgerrors.BadRequest("missing_field", "reference_images is required"))
		return
	}
	refs := make([]provider.ReferenceImage, 0, len(body.ReferenceImages))
	for _, im := range body.ReferenceImages {
		refs = append(refs, provider.ReferenceImage{
			MIMEType: im.MIMEType,
			B64:      im.B64JSON,
		})
	}
	assets, err := uploadReferenceImagesToOSS(r.Context(), u.ID, refs)
	if err != nil {
		if ae, ok := pkgerrors.As(err); ok {
			Error(w, ae)
			return
		}
		Error(w, pkgerrors.New(http.StatusInternalServerError, pkgerrors.TypeInternal, "user_assets_upload_error", err.Error()))
		return
	}
	data := make([]map[string]any, 0, len(assets))
	for _, a := range assets {
		data = append(data, map[string]any{
			"url":       a.URL,
			"mime_type": a.MIMEType,
			"kind":      "reference",
		})
	}
	JSON(w, http.StatusCreated, map[string]any{
		"object": "user.asset_upload",
		"data":   data,
	})
}
