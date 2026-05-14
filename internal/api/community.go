// Package api — M5 community handlers.
//
// PUT /api/jobs/{job_id}/visibility — set a job to public or private (auth required)
// GET /api/community/feed           — paginated public feed (unauthenticated)
package api

import (
	"encoding/json"
	"net/http"
	"strconv"

	"genpic/internal/jobstore"
	pkgerrors "genpic/pkg/errors"
)

// HandleSetVisibility serves PUT /api/jobs/{job_id}/visibility.
// The caller must be authenticated and own the job.
func HandleSetVisibility(w http.ResponseWriter, r *http.Request) {
	if jobStoreInstance == nil {
		Error(w, pkgerrors.New(http.StatusServiceUnavailable, pkgerrors.TypeInternal, "not_ready", "job store not initialised"))
		return
	}
	user := CurrentUser(r)
	if user == nil {
		Error(w, pkgerrors.New(http.StatusUnauthorized, pkgerrors.TypeAuthentication, "unauthenticated", "login required"))
		return
	}
	jobID := r.PathValue("job_id")
	if jobID == "" {
		Error(w, pkgerrors.BadRequest("missing_path_param", "job_id is required in path"))
		return
	}

	var body struct {
		Visibility string `json:"visibility"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		Error(w, pkgerrors.BadRequest("parse_error", "could not parse request body"))
		return
	}
	if body.Visibility != "public" && body.Visibility != "private" {
		Error(w, pkgerrors.BadRequest("invalid_visibility", "visibility must be 'public' or 'private'"))
		return
	}

	if err := jobStoreInstance.SetVisibility(jobID, user.ID, body.Visibility); err != nil {
		Error(w, pkgerrors.NotFound("job"))
		return
	}

	j, ok := jobStoreInstance.Get(jobID)
	if !ok {
		Error(w, pkgerrors.NotFound("job"))
		return
	}
	JSON(w, http.StatusOK, toJobResponseOwner(j))
}

// HandleCommunityFeed serves GET /api/community/feed.
// Returns public jobs newest-community_listed_at first.
// Supports ?limit= (default 20, max 50) and ?cursor= for keyset pagination.
// Prompt visibility: shown only when the job owner has prompt_public=true,
// or the caller is the job owner.
func HandleCommunityFeed(w http.ResponseWriter, r *http.Request) {
	if jobStoreInstance == nil {
		JSON(w, http.StatusOK, map[string]any{"object": "list", "data": []any{}, "next_cursor": nil})
		return
	}

	limit := 20
	if s := r.URL.Query().Get("limit"); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n > 0 {
			limit = n
		}
	}
	if limit > 50 {
		limit = 50
	}
	cursor := r.URL.Query().Get("cursor")

	jobs, nextCursor := jobStoreInstance.ListPublic(limit, cursor)
	callerUID := callerUserID(r)

	items := make([]jobResponse, 0, len(jobs))
	for _, j := range jobs {
		resp := toJobResponse(j, callerUID)
		// For non-owners: prompt is gated by owner's prompt_public setting.
		if callerUID != j.UserID {
			resp.Prompt = communityFeedPrompt(j)
			resp.Params = nil // params are only for the owner
		}
		items = append(items, resp)
	}

	var nc any
	if nextCursor != "" {
		nc = nextCursor
	}
	JSON(w, http.StatusOK, map[string]any{
		"object":      "list",
		"data":        items,
		"next_cursor": nc,
	})
}

// communityFeedPrompt returns the prompt visible to non-owner community viewers.
// It checks the job owner's prompt_public setting via the auth store.
// Returns empty string when the setting is false or the auth store is unavailable.
func communityFeedPrompt(j *jobstore.Job) string {
	if authStoreInstance == nil || j.UserID == "" {
		return ""
	}
	st, err := authStoreInstance.GetSettings(j.UserID)
	if err != nil || !st.PromptPublic {
		return ""
	}
	return j.Prompt
}
