package jobstore

// OwnerScope identifies who may list or read generation jobs.
// UserID is for signed-in users (opaque string from your auth layer later).
// SessionID is for anonymous clients: a stable random id per browser/device.
type OwnerScope struct {
	UserID    string
	SessionID string
}

// ListContains reports whether job j should appear in GET /v1/jobs for scope o.
// When UserID is set, only jobs with the same user_id match.
// Otherwise when SessionID is set, only anonymous jobs (empty user_id) with that session match.
// When both are empty, only legacy rows (no owner columns set) match.
func (o OwnerScope) ListContains(j *Job) bool {
	if o.UserID != "" {
		return j.UserID == o.UserID
	}
	if o.SessionID != "" {
		return j.UserID == "" && j.SessionID == o.SessionID
	}
	return j.UserID == "" && j.SessionID == ""
}

// CanViewJob reports whether a caller with scope o may read job j by id.
// Legacy rows (both owner fields empty) remain readable without headers.
func (o OwnerScope) CanViewJob(j *Job) bool {
	if j.UserID == "" && j.SessionID == "" {
		return true
	}
	if o.UserID != "" {
		return j.UserID == o.UserID
	}
	if o.SessionID != "" {
		return j.UserID == "" && j.SessionID == o.SessionID
	}
	return false
}
