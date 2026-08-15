// Package state defines the persistence boundary for observation state.
package state

import (
	"context"
	"sync"
	"time"

	"github.com/example/image-watch/internal/image"
)

// Key identifies the unit of persisted observation state.
type Key struct {
	Registry   string
	Repository string
	Tag        string
	Platform   image.Platform
}

// Status reflects whether an observation is fresh or stale.
type Status string

const (
	StatusFresh   Status = "fresh"
	StatusStale   Status = "stale"
	StatusUnknown Status = "unknown"
)

// Observation is the persisted registry-side state for one Key.
type Observation struct {
	Key Key

	PlatformManifestDigest string
	IndexDigest            string

	LastSuccess time.Time
	LastError   string
	LastErrorAt time.Time

	Status Status
}

// Store is the persistence interface the observer depends on.
type Store interface {
	GetObservation(ctx context.Context, key Key) (Observation, bool, error)
	PutObservation(ctx context.Context, obs Observation) error

	// HasNotified reports whether a notification with this fingerprint has already been delivered.
	HasNotified(ctx context.Context, fingerprint string) (bool, error)

	// MarkNotified records a fingerprint as delivered.
	MarkNotified(ctx context.Context, fingerprint string) error
}

// MemoryStore is an in-memory Store.
type MemoryStore struct {
	mu            sync.Mutex
	observations  map[Key]Observation
	notifications map[string]time.Time
}

// NewMemoryStore constructs an empty in-memory Store.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		observations:  make(map[Key]Observation),
		notifications: make(map[string]time.Time),
	}
}

var _ Store = (*MemoryStore)(nil)

func (m *MemoryStore) GetObservation(_ context.Context, key Key) (Observation, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	obs, ok := m.observations[key]
	return obs, ok, nil
}

func (m *MemoryStore) PutObservation(_ context.Context, obs Observation) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.observations[obs.Key] = obs
	return nil
}

func (m *MemoryStore) HasNotified(_ context.Context, fingerprint string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.notifications[fingerprint]
	return ok, nil
}

func (m *MemoryStore) MarkNotified(_ context.Context, fingerprint string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.notifications[fingerprint] = time.Now()
	return nil
}
