package idempotency

import (
	"context"
	"sync"
	"time"
)

// Record holds the cached result for an idempotency key.
type Record struct {
	// StatusCode is the HTTP status that was returned on the original request.
	StatusCode int
	// Body is the response body that was returned on the original request.
	Body []byte
	// CreatedAt is when the record was first stored.
	CreatedAt time.Time
}

// Store caches and retrieves idempotency records.
type Store interface {
	// Get returns the stored record for key, or (nil, false) if absent or expired.
	Get(ctx context.Context, key string) (*Record, bool)
	// Set stores the record for key. Implementations should respect a TTL.
	Set(ctx context.Context, key string, rec *Record) error
}

// InMemory is a goroutine-safe, in-memory Store with TTL eviction.
type InMemory struct {
	mu      sync.RWMutex
	records map[string]*Record
	ttl     time.Duration
}

// NewInMemory creates an InMemory store with the given record TTL.
func NewInMemory(ttl time.Duration) *InMemory {
	return &InMemory{
		records: make(map[string]*Record),
		ttl:     ttl,
	}
}

func (s *InMemory) Get(_ context.Context, key string) (*Record, bool) {
	s.mu.RLock()
	rec, ok := s.records[key]
	s.mu.RUnlock()
	if !ok {
		return nil, false
	}
	if time.Since(rec.CreatedAt) > s.ttl {
		// Expired — treat as absent; lazy deletion
		s.mu.Lock()
		delete(s.records, key)
		s.mu.Unlock()
		return nil, false
	}
	return rec, true
}

func (s *InMemory) Set(_ context.Context, key string, rec *Record) error {
	s.mu.Lock()
	s.records[key] = rec
	s.mu.Unlock()
	return nil
}
