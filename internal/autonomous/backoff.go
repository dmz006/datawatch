// Per-backend back-off registry: records that an LLM backend should not be
// handed new work until a deadline, either because a live session hit a
// rate/usage limit ("rate_limit") or because an operator paused it
// ("operator"). Entries are held in memory only — by design (see the
// concurrency-and-budget design doc) a daemon restart rebuilds them from
// live sessions rather than from disk, so there is no persistence here.
//
// BL428 Phase 4 — storage plus automatic set/clear only; the start gate,
// audit events and API surfaces consume this registry elsewhere.
package autonomous

import (
	"fmt"
	"sort"
	"sync"
	"time"
)

// Back-off entry sources.
const (
	// BackoffSourceRateLimit marks an entry set by rate-limit detection.
	BackoffSourceRateLimit = "rate_limit"
	// BackoffSourceOperator marks an entry set by an operator.
	BackoffSourceOperator = "operator"
)

// Entry is one active back-off flag for a backend.
type Entry struct {
	Backend string    `json:"backend"`
	Until   time.Time `json:"until"`
	Reason  string    `json:"reason"`
	Source  string    `json:"source"`
}

// Backoff is a goroutine-safe in-memory registry of per-backend back-off
// flags. The zero value is not usable; construct with NewBackoff.
type Backoff struct {
	// Clock returns the current time. Defaults to time.Now; tests inject
	// a fake so expiry is deterministic.
	Clock func() time.Time

	mu      sync.Mutex
	entries map[string]Entry
}

// NewBackoff returns an empty registry using the wall clock.
func NewBackoff() *Backoff {
	return &Backoff{Clock: time.Now, entries: map[string]Entry{}}
}

func (b *Backoff) now() time.Time {
	if b.Clock != nil {
		return b.Clock()
	}
	return time.Now()
}

// Set records (or replaces) the back-off flag for backend.
func (b *Backoff) Set(backend string, until time.Time, reason string, source string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.entries == nil {
		b.entries = map[string]Entry{}
	}
	b.entries[backend] = Entry{Backend: backend, Until: until, Reason: reason, Source: source}
}

// Clear removes the flag for backend and reports whether one existed.
func (b *Backoff) Clear(backend string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	_, ok := b.entries[backend]
	delete(b.entries, backend)
	return ok
}

// ClearAll removes every flag and returns how many were removed.
func (b *Backoff) ClearAll() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := len(b.entries)
	b.entries = map[string]Entry{}
	return n
}

// Blocked reports whether backend is currently backed off — only while its
// deadline is still in the future — and, when it is, a wait reason that
// includes the human-readable deadline.
func (b *Backoff) Blocked(backend string) (bool, string) {
	now := b.now()
	b.mu.Lock()
	e, ok := b.entries[backend]
	b.mu.Unlock()
	if !ok || !e.Until.After(now) {
		return false, ""
	}
	return true, fmt.Sprintf("backend %s backed off until %s (%s)", backend, e.Until.Format("15:04 MST"), e.Reason)
}

// Sweep removes entries whose deadline is at or before now and returns the
// backends that were cleared, sorted.
func (b *Backoff) Sweep(now time.Time) []string {
	var cleared []string
	for _, e := range b.SweepExpired(now) {
		cleared = append(cleared, e.Backend)
	}
	return cleared
}

// SweepExpired is Sweep returning the removed entries (backend and the
// deadline that passed), sorted by backend, so callers can record what
// expired.
func (b *Backoff) SweepExpired(now time.Time) []Entry {
	b.mu.Lock()
	defer b.mu.Unlock()
	var cleared []Entry
	for name, e := range b.entries {
		if !e.Until.After(now) {
			delete(b.entries, name)
			cleared = append(cleared, e)
		}
	}
	sort.Slice(cleared, func(i, j int) bool { return cleared[i].Backend < cleared[j].Backend })
	return cleared
}

// Active returns a snapshot of all entries ordered by backend name.
func (b *Backoff) Active() []Entry {
	b.mu.Lock()
	out := make([]Entry, 0, len(b.entries))
	for _, e := range b.entries {
		out = append(out, e)
	}
	b.mu.Unlock()
	sort.Slice(out, func(i, j int) bool { return out[i].Backend < out[j].Backend })
	return out
}

// Live returns a snapshot of the entries whose deadline is still in the
// future, ordered by backend name. Unlike Active it omits entries that
// have expired but not yet been swept, so status surfaces never report a
// flag the admission gate has already stopped honouring.
func (b *Backoff) Live() []Entry {
	now := b.now()
	all := b.Active()
	out := make([]Entry, 0, len(all))
	for _, e := range all {
		if e.Until.After(now) {
			out = append(out, e)
		}
	}
	return out
}

// UsageBackoffConfig is the config half of the usage back-off status.
type UsageBackoffConfig struct {
	Mode           string `json:"mode"`
	MaxWaitMinutes int    `json:"max_wait_minutes"`
}

// UsageBackoffStatus is the body shared by GET /api/usage-backoff and the
// usage_backoff field of the autonomous status.
type UsageBackoffStatus struct {
	Active []Entry            `json:"active"`
	Config UsageBackoffConfig `json:"config"`
	// PatternsVersion is the short fingerprint of the effective rate-limit
	// detection patterns; PatternsStale is true when it differs from the one
	// an operator last rechecked (pattern-drift guard).
	PatternsVersion string `json:"patterns_version"`
	PatternsStale   bool   `json:"patterns_stale"`
}

// PatternsStatusFunc reports the rate-limit pattern fingerprint and whether
// it is stale (not yet rechecked by an operator).
type PatternsStatusFunc func() (version string, stale bool)

// BudgetsStatusFunc reports the budget state of every LLM that has a budget.
type BudgetsStatusFunc func() []BudgetStatus

// BuildUsageBackoffStatus snapshots the live flags of b (nil = none) with
// the effective mode and maximum back-off. Active is never nil, so it
// always marshals as an array.
func BuildUsageBackoffStatus(b *Backoff, mode string, maxWaitMinutes int) UsageBackoffStatus {
	st := UsageBackoffStatus{Active: []Entry{}, Config: UsageBackoffConfig{Mode: mode, MaxWaitMinutes: maxWaitMinutes}}
	if b != nil {
		st.Active = b.Live()
	}
	return st
}
