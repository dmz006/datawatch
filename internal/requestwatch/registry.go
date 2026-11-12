package requestwatch

import (
	"sort"
	"sync"
)

// Registry holds the latest Observation of every watched session and is the
// Reader the status route reads. A poller writes into it with Put and drops a
// session with Forget. It is safe for concurrent use and never makes a model
// call. The zero value is not ready: use NewRegistry.
type Registry struct {
	mu  sync.RWMutex
	obs map[string]Observation
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{obs: map[string]Observation{}}
}

// Put stores o as the current observation of its session, replacing any
// earlier one. An observation without a session id is ignored.
func (r *Registry) Put(o Observation) {
	if o.SessionID == "" {
		return
	}
	r.mu.Lock()
	r.obs[o.SessionID] = o
	r.mu.Unlock()
}

// Forget drops the observation of a session that has ended.
func (r *Registry) Forget(sessionID string) {
	r.mu.Lock()
	delete(r.obs, sessionID)
	r.mu.Unlock()
}

// Observations returns a copy of the current observations ordered by session
// id. It is never nil.
func (r *Registry) Observations() []Observation {
	r.mu.RLock()
	out := make([]Observation, 0, len(r.obs))
	for _, o := range r.obs {
		out = append(out, o)
	}
	r.mu.RUnlock()
	sort.Slice(out, func(i, j int) bool { return out[i].SessionID < out[j].SessionID })
	return out
}

// Observation returns the current observation of one session.
func (r *Registry) Observation(sessionID string) (Observation, bool) {
	r.mu.RLock()
	o, ok := r.obs[sessionID]
	r.mu.RUnlock()
	return o, ok
}

var _ Reader = (*Registry)(nil)
