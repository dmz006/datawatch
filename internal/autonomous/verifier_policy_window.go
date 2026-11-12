package autonomous

import (
	"sync"
	"time"
)

// VerifierPolicyWindow counts Claude verifier sessions started inside a
// sliding window, for the verifier policy's per-window session cap. It is
// in-memory only: a daemon restart starts the window empty.
type VerifierPolicyWindow struct {
	mu     sync.Mutex
	now    func() time.Time
	starts []time.Time
}

// NewVerifierPolicyWindow returns an empty window. now supplies the current
// time; nil means the wall clock (tests pass a fake).
func NewVerifierPolicyWindow(now func() time.Time) *VerifierPolicyWindow {
	if now == nil {
		now = time.Now
	}
	return &VerifierPolicyWindow{now: now}
}

// Record notes that one Claude verifier session started now.
func (w *VerifierPolicyWindow) Record() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.starts = append(w.starts, w.now())
}

// Count returns how many sessions started within the last window. Entries
// older than the window are dropped.
func (w *VerifierPolicyWindow) Count(window time.Duration) int {
	w.mu.Lock()
	defer w.mu.Unlock()
	cutoff := w.now().Add(-window)
	keep := w.starts[:0]
	for _, t := range w.starts {
		if t.After(cutoff) {
			keep = append(keep, t)
		}
	}
	w.starts = keep
	return len(keep)
}
