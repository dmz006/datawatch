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
	"slices"
	"sort"
	"strings"
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

// BackoffReasonNoResetTime is the reason of a rate_limit entry whose
// deadline is the configured maximum back-off, set because the rate-limit
// message carried no reset time. Every other rate_limit entry's deadline was
// parsed from the message.
const BackoffReasonNoResetTime = "rate-limit — no reset time (max back-off)"

// Entry is one active back-off flag for a backend.
type Entry struct {
	Backend string    `json:"backend"`
	Until   time.Time `json:"until"`
	Reason  string    `json:"reason"`
	Source  string    `json:"source"`
	// ResetKnown is true when Until is a reset time the provider (or an
	// operator) named, false when it is only the maximum back-off applied
	// because the rate-limit message carried no reset time.
	ResetKnown bool `json:"reset_known"`
	// Rung names the ladder rung that set this flag (for example the burn
	// guard's hold_claude hold). It is empty on rate-limit and operator
	// entries, which older clients still read without it.
	Rung string `json:"rung,omitempty"`
	// Models, when non-empty, limits the flag to these models of the backend
	// (normalised: trimmed, lower-case, unique, sorted). Work on any other
	// model, or work that names no model, is admitted as usual. Empty means
	// the whole backend is held, as before the list existed.
	Models []string `json:"models,omitempty"`
}

// NormalizeModels trims, lower-cases, de-duplicates and sorts a model list,
// dropping empty names. It returns nil when nothing is left.
func NormalizeModels(models []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, m := range models {
		m = strings.ToLower(strings.TrimSpace(m))
		if m == "" || seen[m] {
			continue
		}
		seen[m] = true
		out = append(out, m)
	}
	sort.Strings(out)
	return out
}

// HoldsModel reports whether the entry holds work on model. A whole-backend
// entry (no Models) holds everything; a model-scoped entry holds only its
// listed models, so an unlisted or empty model is not held.
func (e Entry) HoldsModel(model string) bool {
	if len(e.Models) == 0 {
		return true
	}
	m := strings.ToLower(strings.TrimSpace(model))
	if m == "" {
		return false
	}
	for _, h := range e.Models {
		if h == m {
			return true
		}
	}
	return false
}

// Backoff is a goroutine-safe in-memory registry of per-backend back-off
// flags. The zero value is not usable; construct with NewBackoff.
type Backoff struct {
	// Clock returns the current time. Defaults to time.Now; tests inject
	// a fake so expiry is deterministic.
	Clock func() time.Time

	mu      sync.Mutex
	entries map[string]Entry

	// persistPath, when set (see backoff_persist.go), is the JSON file every
	// mutation is written to; saveErr is the last write error.
	persistPath string
	saveErr     error

	// KnownModels, when set, lists the models a backend can run; the status
	// read uses it to name the open (not held) models of a model-scoped flag.
	KnownModels func(backend string) []string
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

// ResetParsed reports whether the entry's deadline is a reset time parsed from
// a rate-limit message, as opposed to an operator-set deadline or the
// configured fallback used when the message carried no reset time.
func (e Entry) ResetParsed() bool {
	return e.Source == BackoffSourceRateLimit && e.Reason != BackoffReasonNoResetTime
}

// Set records (or replaces) the back-off flag for backend, treating until as
// a known reset time.
func (b *Backoff) Set(backend string, until time.Time, reason string, source string) {
	b.SetWithReset(backend, until, reason, source, true)
}

// SetWithReset is Set with an explicit flag for whether until is a known
// reset time (false when it is the maximum back-off applied for lack of one).
// It sets no rung.
func (b *Backoff) SetWithReset(backend string, until time.Time, reason string, source string, resetKnown bool) {
	b.SetWithRung(backend, until, reason, source, resetKnown, "")
}

// SetWithRung is SetWithReset for a flag set by one rung of a ladder: the
// entry carries that rung's name (for example the burn guard's
// hold_claude hold) while the free-form text stays in Reason. An empty rung
// behaves like SetWithReset.
func (b *Backoff) SetWithRung(backend string, until time.Time, reason string, source string, resetKnown bool, rung string) {
	b.SetWithModels(backend, until, reason, source, resetKnown, rung, nil)
}

// SetWithModels is SetWithRung limited to a list of models. The list is
// normalised (see NormalizeModels); an empty list holds the whole backend.
func (b *Backoff) SetWithModels(backend string, until time.Time, reason string, source string, resetKnown bool, rung string, models []string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.entries == nil {
		b.entries = map[string]Entry{}
	}
	b.entries[backend] = Entry{Backend: backend, Until: until, Reason: reason, Source: source, ResetKnown: resetKnown, Rung: rung, Models: NormalizeModels(models)}
	_ = b.saveLocked() // the error is kept in saveErr for the status read
}

// Clear removes the flag for backend and reports whether one existed.
func (b *Backoff) Clear(backend string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	_, ok := b.entries[backend]
	delete(b.entries, backend)
	if ok {
		_ = b.saveLocked() // the error is kept in saveErr for the status read
	}
	return ok
}

// ClearModels narrows a clear to the named models: it removes them from the
// model list of the flag on backend and drops the flag entirely when that
// empties the list. A whole-backend flag (no model list) is never touched, and
// a model-scoped flag whose list is not covered by the named models is left
// unchanged. It reports whether the flag was changed.
func (b *Backoff) ClearModels(backend string, models []string) bool {
	given := NormalizeModels(models)
	if len(given) == 0 {
		return false
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	e, ok := b.entries[backend]
	if !ok || len(e.Models) == 0 {
		return false
	}
	kept := make([]string, 0, len(e.Models))
	changed := false
	for _, m := range e.Models {
		if slices.Contains(given, m) {
			changed = true
			continue
		}
		kept = append(kept, m)
	}
	if !changed {
		return false
	}
	if len(kept) == 0 {
		delete(b.entries, backend)
	} else {
		e.Models = kept
		b.entries[backend] = e
	}
	_ = b.saveLocked() // the error is kept in saveErr for the status read
	return true
}

// ClearAll removes every flag and returns how many were removed.
func (b *Backoff) ClearAll() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := len(b.entries)
	b.entries = map[string]Entry{}
	_ = b.saveLocked() // the error is kept in saveErr for the status read
	return n
}

// Blocked reports whether backend is currently backed off — only while its
// deadline is still in the future — and, when it is, a wait reason that
// includes the human-readable deadline. It names no model, so a flag limited
// to a model list does not block it (see BlockedModel).
func (b *Backoff) Blocked(backend string) (bool, string) {
	return b.BlockedModel(backend, "")
}

// BlockedModel is Blocked for work on one model. A whole-backend flag blocks
// every model; a model-scoped flag blocks only its listed models, so work on
// an unlisted model, or naming no model, is admitted.
func (b *Backoff) BlockedModel(backend, model string) (bool, string) {
	now := b.now()
	b.mu.Lock()
	e, ok := b.entries[backend]
	b.mu.Unlock()
	if !ok || !e.Until.After(now) || !e.HoldsModel(model) {
		return false, ""
	}
	note := ""
	if e.Source == BackoffSourceOperator {
		note = e.Reason
	}
	return true, BackoffModelsWaitText(b.now, backend, e.Models, e.Until, e.ResetKnown, note)
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
	if len(cleared) > 0 {
		_ = b.saveLocked() // the error is kept in saveErr for the status read
	}
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
	Mode                 string `json:"mode"`
	MaxWaitMinutes       int    `json:"max_wait_minutes"`
	ReserveForReview     int    `json:"reserve_for_review"`
	ReserveTokensPerHour int64  `json:"reserve_tokens_per_hour"`
}

// ReserveUsage is how much of the review-reserve per-hour token budget one
// backed-off backend has already spent in the current window.
type ReserveUsage struct {
	Backend    string `json:"backend"`
	TokensUsed int64  `json:"tokens_used"`
}

// UsageBackoffStatus is the body shared by GET /api/usage-backoff and the
// usage_backoff field of the autonomous status.
type UsageBackoffStatus struct {
	Active []Entry `json:"active"`
	// Holds names, per backend with a live flag, the held and open models.
	Holds  []BackendHold      `json:"holds"`
	Config UsageBackoffConfig `json:"config"`
	// Reserve carries the live per-hour reserve usage per backed-off
	// backend; it is present only when the daemon runs the review-reserve
	// gate and at least one flag is live.
	Reserve []ReserveUsage `json:"reserve,omitempty"`
	// PatternsVersion is the short fingerprint of the effective rate-limit
	// detection patterns; PatternsStale is true when it differs from the one
	// an operator last rechecked (pattern-drift guard). ClaudeCLIVersion is
	// the cached `claude --version` text that is mixed into the combined
	// fingerprint ("unknown" when the tool is absent).
	PatternsVersion  string `json:"patterns_version"`
	PatternsStale    bool   `json:"patterns_stale"`
	ClaudeCLIVersion string `json:"claude_cli_version"`
}

// PatternsStatusFunc reports the rate-limit pattern fingerprint, the cached
// claude CLI version text, and whether the guard is stale (not yet rechecked
// by an operator).
type PatternsStatusFunc func() (version, claudeCLIVersion string, stale bool)

// BudgetsStatusFunc reports the budget state of every LLM that has a budget.
type BudgetsStatusFunc func() []BudgetStatus

// BuildUsageBackoffStatus snapshots the live flags of b (nil = none) with
// the effective back-off config. Active is never nil, so it always
// marshals as an array.
func BuildUsageBackoffStatus(b *Backoff, cfg UsageBackoffConfig) UsageBackoffStatus {
	st := UsageBackoffStatus{Active: []Entry{}, Holds: []BackendHold{}, Config: cfg}
	if b != nil {
		st.Active = b.Live()
		st.Holds = b.Holds()
	}
	return st
}

// BackendHold describes what one live flag holds. WholeBackend is true when
// the flag has no model list. HeldModels is the model list; OpenModels names
// the known models of the backend that are not held (from KnownModels), and
// OtherModelsOpen is true when any model outside HeldModels is admitted.
type BackendHold struct {
	Backend         string   `json:"backend"`
	WholeBackend    bool     `json:"whole_backend"`
	HeldModels      []string `json:"held_models"`
	OpenModels      []string `json:"open_models"`
	OtherModelsOpen bool     `json:"other_models_open"`
}

// HoldFor builds the hold view of one entry, with known listing the models
// the backend can run (may be nil).
func HoldFor(e Entry, known []string) BackendHold {
	h := BackendHold{Backend: e.Backend, HeldModels: []string{}, OpenModels: []string{}}
	if len(e.Models) == 0 {
		h.WholeBackend = true
		return h
	}
	h.HeldModels = append(h.HeldModels, e.Models...)
	h.OtherModelsOpen = true
	for _, m := range NormalizeModels(known) {
		if !e.HoldsModel(m) {
			h.OpenModels = append(h.OpenModels, m)
		}
	}
	return h
}

// Holds returns the hold view of every live flag, ordered by backend.
func (b *Backoff) Holds() []BackendHold {
	out := []BackendHold{}
	for _, e := range b.Live() {
		var known []string
		if b.KnownModels != nil {
			known = b.KnownModels(e.Backend)
		}
		out = append(out, HoldFor(e, known))
	}
	return out
}
