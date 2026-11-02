package session

import (
	"context"
	"sync"
)

// Store is the read/write surface needed by DryRunStore. *Manager implements it;
// pipeline.SessionStore has the same method set.
type Store interface {
	Load(ctx context.Context, key string) (*State, error)
	Save(ctx context.Context, key string, state *State) error
}

// DryRunStore is an in-memory overlay used by dry runs: reads fall through to
// the persistent store, writes stay in memory. A login flow can therefore
// complete (login -> fetch protected page) within one dry run without mutating
// the real session that scheduled runs depend on.
type DryRunStore struct {
	base  Store
	mu    sync.Mutex
	cache map[string]*State
}

// NewDryRunStore wraps base; a nil base is treated as an empty store.
func NewDryRunStore(base Store) *DryRunStore {
	return &DryRunStore{base: base, cache: map[string]*State{}}
}

// Load returns the overlay state when present, otherwise the persisted one.
func (d *DryRunStore) Load(ctx context.Context, key string) (*State, error) {
	d.mu.Lock()
	cached, ok := d.cache[key]
	d.mu.Unlock()
	if ok {
		return cached.Clone(), nil
	}
	if d.base == nil {
		return nil, nil
	}
	state, err := d.base.Load(ctx, key)
	if err != nil {
		return nil, err
	}
	if state == nil {
		return nil, nil
	}
	return state.Clone(), nil
}

// Save stores the state in the overlay only.
func (d *DryRunStore) Save(_ context.Context, key string, state *State) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.cache[key] = state.Clone()
	return nil
}
