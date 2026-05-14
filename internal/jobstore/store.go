// Package jobstore implements an in-memory generation job store.
//
// Lifecycle: queued → running → succeeded | failed.
// The store is intentionally simple (no DB) for M1; it is meant to be replaced
// with a PostgreSQL-backed implementation in a later milestone while preserving
// the same Store interface.
//
// Jobs are stored in a sync.Map and evicted after TTL (default 2 h) to limit
// memory growth. A background goroutine runs the eviction sweep every minute.
package jobstore

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"time"
)

// Status represents the lifecycle state of a generation job.
type Status string

const (
	StatusQueued    Status = "queued"
	StatusRunning   Status = "running"
	StatusSucceeded Status = "succeeded"
	StatusFailed    Status = "failed"
)

// Image holds a single generated image result.
type Image struct {
	URL           string `json:"url,omitempty"`
	ThumbURL      string `json:"thumb_url,omitempty"`
	B64JSON       string `json:"b64_json,omitempty"`
	MIMEType      string `json:"mime_type,omitempty"`
	RevisedPrompt string `json:"revised_prompt,omitempty"`
}

// JobBBox mirrors provider bounding boxes for persisted job params.
type JobBBox struct {
	X1 int `json:"x1"`
	Y1 int `json:"y1"`
	X2 int `json:"x2"`
	Y2 int `json:"y2"`
}

// JobParams stores the generation parameters used to create the job.
// Persisted alongside the job for "create similar" functionality (M5).
type JobParams struct {
	Model       string `json:"model,omitempty"`
	AspectRatio string `json:"aspect_ratio,omitempty"`
	ImageSize   string `json:"image_size,omitempty"`
	Size        string `json:"size,omitempty"`
	N           int    `json:"n,omitempty"`
	Quality     string `json:"quality,omitempty"`
	Style       string `json:"style,omitempty"`

	ResponseFormat string    `json:"response_format,omitempty"`
	ThinkingBudget int       `json:"thinking_budget,omitempty"`
	ThinkingMode   bool      `json:"thinking_mode,omitempty"`
	WanEditType    string    `json:"wan_edit_type,omitempty"`
	WanBboxList    []JobBBox `json:"wan_bbox_list,omitempty"`
}

// Job is the canonical record for one image-generation request.
type Job struct {
	ID       string
	Model    string
	Provider string
	Prompt   string

	Status     Status
	ErrorCode  string
	ErrorMsg   string
	Images     []Image
	TokensUsed int
	// UpstreamRequestID is the provider-side request id when reported (e.g. Gemini).
	UpstreamRequestID string
	Params            *JobParams

	// Visibility controls community listing: "private" (default) or "public".
	Visibility        string
	CommunityListedAt time.Time

	CreatedAt  time.Time
	StartedAt  time.Time
	FinishedAt time.Time

	// KeyID reserved for future per-caller ACL (currently unset).
	KeyID string

	// UserID is set when the client sends X-Genpic-User-Id or is authenticated via session.
	UserID string
	// SessionID is set for anonymous clients (X-Genpic-Session); ignored when UserID is set.
	SessionID string
}

// IsTerminal reports whether the job has reached a final state.
func (j *Job) IsTerminal() bool {
	return j.Status == StatusSucceeded || j.Status == StatusFailed
}

// AdminStatsSummary is returned by Store.AdminStats for operator dashboards.
type AdminStatsSummary struct {
	TotalJobs     int64            `json:"total_jobs"`
	Succeeded     int64            `json:"succeeded"`
	Failed        int64            `json:"failed"`
	QueuedRunning int64            `json:"queued_running"`
	ByProvider    map[string]int64 `json:"by_provider"`
}

// Store is the interface for job persistence.
// The in-memory implementation (Memory) satisfies this interface.
// A DB-backed implementation can be swapped in without changing callers.
type Store interface {
	// Create stores a new job and returns its assigned ID.
	Create(job *Job) (string, error)
	// Get returns a copy of the job, or (nil, false) if not found.
	Get(id string) (*Job, bool)
	// Update applies fn to the stored job under a lock. Returns false when
	// the job does not exist.
	Update(id string, fn func(*Job)) bool
	// List returns up to limit jobs, newest first, starting after cursor.
	// An empty cursor means start from the beginning. Returns a next cursor
	// (empty string when there are no more results).
	// scope restricts rows to the caller (see OwnerScope).
	List(limit int, cursor string, scope OwnerScope) ([]*Job, string)
	// AdminList returns jobs across all owners, newest first, for operator UIs.
	// total is the number of jobs in the store (for pagination). Not authenticated here.
	AdminList(limit, offset int) ([]*Job, int64)
	// AdminStats returns aggregate counts. Not authenticated here.
	AdminStats() AdminStatsSummary
	// ListPublic returns jobs with visibility="public", newest community_listed_at first.
	// Used for the community feed. In-memory store returns an empty result.
	ListPublic(limit int, cursor string) ([]*Job, string)
	// SetVisibility updates the visibility of a job. userID is the authenticated caller;
	// the method returns an error if the job is not owned by userID.
	SetVisibility(id, userID, visibility string) error
}

// ─── Memory implementation ────────────────────────────────────────────────────

const (
	defaultTTL  = 2 * time.Hour
	evictPeriod = 1 * time.Minute
)

// Memory is a goroutine-safe in-memory job store.
type Memory struct {
	mu      sync.Mutex
	ordered []*Job // insertion order (newest appended; list is reversed)
	index   map[string]*Job
	ttl     time.Duration
}

// NewMemory creates a Memory store and starts a background eviction goroutine
// that removes jobs older than ttl (0 → defaultTTL). The goroutine is stopped
// when ctx is cancelled.
func NewMemory(ctx context.Context, ttl time.Duration) *Memory {
	if ttl <= 0 {
		ttl = defaultTTL
	}
	m := &Memory{
		index: make(map[string]*Job),
		ttl:   ttl,
	}
	go m.evictLoop(ctx)
	return m
}

// Create assigns a random ID and stores the job. A zero CreatedAt is set to now.
func (m *Memory) Create(job *Job) (string, error) {
	id, err := newID()
	if err != nil {
		return "", fmt.Errorf("jobstore: generate id: %w", err)
	}
	job.ID = id
	if job.CreatedAt.IsZero() {
		job.CreatedAt = time.Now()
	}
	cp := *job

	m.mu.Lock()
	m.index[id] = &cp
	m.ordered = append(m.ordered, &cp)
	m.mu.Unlock()

	return id, nil
}

// Get returns a shallow copy of the job, or (nil, false) if not found.
func (m *Memory) Get(id string) (*Job, bool) {
	m.mu.Lock()
	j, ok := m.index[id]
	if !ok {
		m.mu.Unlock()
		return nil, false
	}
	cp := *j
	m.mu.Unlock()
	return &cp, true
}

// Update applies fn to the live job under the store lock. Returns false when
// the job does not exist. fn must not retain a reference to the *Job after it
// returns (the pointer is reused by the store).
func (m *Memory) Update(id string, fn func(*Job)) bool {
	m.mu.Lock()
	j, ok := m.index[id]
	if ok {
		fn(j)
	}
	m.mu.Unlock()
	return ok
}

// List returns up to limit jobs, newest first, starting after the job with ID
// cursor. An empty cursor starts from the newest job.
func (m *Memory) List(limit int, cursor string, scope OwnerScope) ([]*Job, string) {
	if limit <= 0 {
		limit = 20
	}
	m.mu.Lock()
	// Build a reversed snapshot (newest first), filtered by owner scope.
	var snap []*Job
	for i := len(m.ordered) - 1; i >= 0; i-- {
		j := m.ordered[i]
		if scope.ListContains(j) {
			cp := *j
			snap = append(snap, &cp)
		}
	}
	m.mu.Unlock()

	start := 0
	if cursor != "" {
		for i, j := range snap {
			if j.ID == cursor {
				start = i + 1
				break
			}
		}
	}
	if start >= len(snap) {
		return nil, ""
	}
	end := start + limit
	if end > len(snap) {
		end = len(snap)
	}
	page := snap[start:end]

	nextCursor := ""
	if end < len(snap) {
		nextCursor = snap[end-1].ID
	}
	return page, nextCursor
}

// evictLoop removes jobs older than m.ttl in the background.
func (m *Memory) evictLoop(ctx context.Context) {
	t := time.NewTicker(evictPeriod)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			m.evict()
		}
	}
}

func (m *Memory) evict() {
	cutoff := time.Now().Add(-m.ttl)
	m.mu.Lock()
	defer m.mu.Unlock()

	keep := m.ordered[:0]
	for _, j := range m.ordered {
		if j.CreatedAt.After(cutoff) {
			keep = append(keep, j)
		} else {
			delete(m.index, j.ID)
		}
	}
	m.ordered = keep
}

// newID generates a 16-byte cryptographically random hex string.
func newID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// AdminList returns all jobs newest-first (reversed insertion order).
func (m *Memory) AdminList(limit, offset int) ([]*Job, int64) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 500 {
		limit = 500
	}
	if offset < 0 {
		offset = 0
	}
	m.mu.Lock()
	n := len(m.ordered)
	need := offset + limit
	if need > n {
		need = n
	}
	rev := make([]*Job, 0, need)
	for i := n - 1; i >= 0 && len(rev) < need; i-- {
		j := m.ordered[i]
		cp := new(Job)
		*cp = *j
		rev = append(rev, cp)
	}
	total := int64(n)
	m.mu.Unlock()
	if offset >= len(rev) {
		return nil, total
	}
	end := limit
	if offset+end > len(rev) {
		end = len(rev) - offset
	}
	return rev[offset : offset+end], total
}

// AdminStats scans all in-memory jobs.
func (m *Memory) AdminStats() AdminStatsSummary {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := AdminStatsSummary{ByProvider: map[string]int64{}}
	for _, j := range m.ordered {
		s.TotalJobs++
		switch j.Status {
		case StatusSucceeded:
			s.Succeeded++
		case StatusFailed:
			s.Failed++
		case StatusQueued, StatusRunning:
			s.QueuedRunning++
		}
		p := j.Provider
		if p == "" {
			p = "(unknown)"
		}
		s.ByProvider[p]++
	}
	return s
}

// ListPublic is a stub for the in-memory store; the community feed requires MySQL.
func (m *Memory) ListPublic(_ int, _ string) ([]*Job, string) { return nil, "" }

// SetVisibility updates the visibility of a job in-memory.
func (m *Memory) SetVisibility(id, userID, visibility string) error {
	m.mu.Lock()
	j, ok := m.index[id]
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("jobstore: job %s not found", id)
	}
	if j.UserID != userID {
		m.mu.Unlock()
		return fmt.Errorf("jobstore: permission denied")
	}
	j.Visibility = visibility
	if visibility == "public" && j.CommunityListedAt.IsZero() {
		j.CommunityListedAt = time.Now()
	}
	m.mu.Unlock()
	return nil
}
