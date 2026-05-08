package api

import (
	"net/http"

	pkgerrors "genpic/pkg/errors"
)

// HandleGetJob serves GET /v1/jobs/{job_id}.
// In MVP Lite / M0 synchronous mode, jobs are not persisted and this endpoint
// returns a 404. The full async implementation is planned for M1.
//
// TODO(@pyq, #10): Implement async job storage and retrieval for M1.
func HandleGetJob(w http.ResponseWriter, r *http.Request) {
	jobID := r.PathValue("job_id")
	if jobID == "" {
		Error(w, pkgerrors.BadRequest("missing_path_param", "job_id is required in path"))
		return
	}
	// Stub: async jobs not yet implemented.
	Error(w, pkgerrors.NotFound("job"))
}

// HandleListJobs serves GET /v1/jobs.
// Stub implementation for M0; returns an empty list.
//
// TODO(@pyq, #11): Implement job list with cursor pagination for M1.
func HandleListJobs(w http.ResponseWriter, _ *http.Request) {
	JSON(w, http.StatusOK, map[string]any{
		"data":        []any{},
		"next_cursor": nil,
	})
}
