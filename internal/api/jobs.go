package api

import (
	"net/http"
	"strconv"

	pkgerrors "genpic/pkg/errors"
	"genpic/internal/jobstore"
)

// jobStoreInstance is set by SetJobStore during server startup.
var jobStoreInstance jobstore.Store

// SetJobStore wires the job store into the API layer. Must be called before
// HandleGetJob or HandleListJobs are invoked.
func SetJobStore(s jobstore.Store) { jobStoreInstance = s }

// jobResponse is the JSON shape returned for a single job.
type jobResponse struct {
	ID         string            `json:"id"`
	Object     string            `json:"object"`
	Model      string            `json:"model"`
	Provider   string            `json:"provider,omitempty"`
	Status     string            `json:"status"`
	CreatedAt  int64             `json:"created_at"`
	StartedAt  *int64            `json:"started_at,omitempty"`
	FinishedAt *int64            `json:"finished_at,omitempty"`
	Data       []jobImageData    `json:"data,omitempty"`
	Error      *jobErrorData     `json:"error,omitempty"`
}

type jobImageData struct {
	URL           string `json:"url,omitempty"`
	B64JSON       string `json:"b64_json,omitempty"`
	MIMEType      string `json:"mime_type,omitempty"`
	RevisedPrompt string `json:"revised_prompt,omitempty"`
}

type jobErrorData struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func toJobResponse(j *jobstore.Job) jobResponse {
	r := jobResponse{
		ID:        j.ID,
		Object:    "generation.job",
		Model:     j.Model,
		Provider:  j.Provider,
		Status:    string(j.Status),
		CreatedAt: j.CreatedAt.Unix(),
	}
	if !j.StartedAt.IsZero() {
		t := j.StartedAt.Unix()
		r.StartedAt = &t
	}
	if !j.FinishedAt.IsZero() {
		t := j.FinishedAt.Unix()
		r.FinishedAt = &t
	}
	for _, img := range j.Images {
		r.Data = append(r.Data, jobImageData{
			URL:           img.URL,
			B64JSON:       img.B64JSON,
			MIMEType:      img.MIMEType,
			RevisedPrompt: img.RevisedPrompt,
		})
	}
	if j.ErrorCode != "" {
		r.Error = &jobErrorData{Code: j.ErrorCode, Message: j.ErrorMsg}
	}
	return r
}

// HandleGetJob serves GET /v1/jobs/{job_id}.
// Returns the current status and images (when succeeded) for the given job.
func HandleGetJob(w http.ResponseWriter, r *http.Request) {
	if jobStoreInstance == nil {
		Error(w, pkgerrors.New(http.StatusServiceUnavailable, pkgerrors.TypeInternal, "not_ready", "job store not initialised"))
		return
	}
	jobID := r.PathValue("job_id")
	if jobID == "" {
		Error(w, pkgerrors.BadRequest("missing_path_param", "job_id is required in path"))
		return
	}
	j, ok := jobStoreInstance.Get(jobID)
	if !ok {
		Error(w, pkgerrors.NotFound("job"))
		return
	}
	owner := callerScopeFromRequest(r)
	if !owner.CanViewJob(j) {
		Error(w, pkgerrors.NotFound("job"))
		return
	}
	JSON(w, http.StatusOK, toJobResponse(j))
}

// HandleListJobs serves GET /v1/jobs.
// Supports ?limit= (default 20, max 100) and ?cursor= for pagination.
func HandleListJobs(w http.ResponseWriter, r *http.Request) {
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
	if limit > 100 {
		limit = 100
	}
	cursor := r.URL.Query().Get("cursor")

	owner := callerScopeFromRequest(r)
	jobs, nextCursor := jobStoreInstance.List(limit, cursor, owner)

	items := make([]jobResponse, 0, len(jobs))
	for _, j := range jobs {
		items = append(items, toJobResponse(j))
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
