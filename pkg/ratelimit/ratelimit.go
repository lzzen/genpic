package ratelimit

import (
	"sync"
	"time"
)

// Limiter is the rate-limiting interface. Multiple dimensions can be composed
// by chaining Limiter calls: check global, then per-key, then per-IP.
type Limiter interface {
	// Allow reports whether the given key is within its rate limit.
	// It records the attempt regardless of the return value.
	Allow(key string) bool
}

// ─── In-memory sliding-window implementation ───────────────────────────────

// InMemory is a fixed-window counter implementation suitable for single-node
// deployments. It is goroutine-safe.
//
// For production multi-node deployments, replace with a Redis-backed
// implementation that uses INCR + EXPIRE (see docs/decisions/ADR-002).
type InMemory struct {
	mu       sync.Mutex
	windows  map[string]*window
	maxCalls int
	period   time.Duration
}

type window struct {
	count   int
	resetAt time.Time
}

// NewInMemory creates a rate limiter that allows at most maxCalls requests
// per period for each unique key.
func NewInMemory(maxCalls int, period time.Duration) *InMemory {
	return &InMemory{
		windows:  make(map[string]*window),
		maxCalls: maxCalls,
		period:   period,
	}
}

// Allow reports whether key is within its rate limit and records the attempt.
func (m *InMemory) Allow(key string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	w, ok := m.windows[key]
	if !ok || now.After(w.resetAt) {
		m.windows[key] = &window{count: 1, resetAt: now.Add(m.period)}
		return true
	}
	w.count++
	return w.count <= m.maxCalls
}

// Unlimited is a Limiter that never rejects. Use in tests or when rate
// limiting is disabled via configuration.
type Unlimited struct{}

func (Unlimited) Allow(_ string) bool { return true }
