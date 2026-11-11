package observer

// Pull collector (observer pull mode) — periodically pulls a StatsResponse
// from each configured peer (the server task supplies the real HTTP
// PullFetcher; this file only defines the contract plus the collector loop).
// Each target runs in its own goroutine with its own timeout context, so a
// slow or failing peer never delays another peer or the push path.

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"
)

// PullTarget describes one peer the collector periodically pulls from.
type PullTarget struct {
	Name     string
	Interval time.Duration
	Timeout  time.Duration
	Enabled  bool
}

// PullFetcher returns the latest stats for a named peer. Implementations
// (e.g. the server's HTTP pull path) return PullHTTPError for HTTP-level
// failures and wrap ErrBadPayload when the response body decodes, so the
// collector can map failures to stable stale reasons.
type PullFetcher interface {
	Fetch(ctx context.Context, name string) (*StatsResponse, error)
}

// Ticker is the minimal slice of time.Ticker the collector needs. Tests
// inject a channel-driven fake so time advances deterministically.
type Ticker interface {
	Chan() <-chan time.Time
	Stop()
}

// PullCollectorOptions wires the collector's dependencies. Fetcher is
// required; NewTicker and Now are optional and default to the real time
// package.
type PullCollectorOptions struct {
	Fetcher   PullFetcher
	NewTicker func(d time.Duration) Ticker
	Now       func() time.Time
}

// NewPullCollector builds a collector from opts. Callers must set
// opts.Fetcher; a nil Fetcher is a programming error.
func NewPullCollector(opts PullCollectorOptions) *PullCollector {
	if opts.NewTicker == nil {
		opts.NewTicker = func(d time.Duration) Ticker {
			return &defaultTicker{t: time.NewTicker(d)}
		}
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &PullCollector{
		fetcher:   opts.Fetcher,
		newTicker: opts.NewTicker,
		now:       opts.Now,
		ctx:       ctx,
		cancel:    cancel,
		states:    make(map[string]*PullState),
		workers:   make(map[string]workerEntry),
	}
}

type defaultTicker struct{ t *time.Ticker }

func (d *defaultTicker) Chan() <-chan time.Time { return d.t.C }
func (d *defaultTicker) Stop()                  { d.t.Stop() }

// StaleReason values for PullState.StaleReason. A successful fetch always
// leaves StaleReason as ReasonNone; a failed fetch sets Stale to true and
// StaleReason to one of the failure reasons below.
const (
	ReasonNone         = "none"
	ReasonTimeout      = "timeout"
	ReasonError        = "error"
	ReasonForbidden    = "forbidden"
	ReasonNotPermitted = "not_permitted"
	ReasonBadPayload   = "bad_payload"
	ReasonUnknown      = "unknown"
	// ReasonCollectorNotRunning is reported by the server (never recorded by
	// the collector) for an opted-in peer whose collector is nil or stopped.
	ReasonCollectorNotRunning = "collector not running"
)

// PullSource is the Source value recorded on every pull-mode state entry.
const PullSource = "pull"

// PullHTTPError is returned by PullFetcher implementations for
// HTTP-level failures. The collector maps a StatusCode of 401 or 403 to
// ReasonForbidden and any other status to ReasonError.
type PullHTTPError struct {
	StatusCode int
	Message    string
}

// NewPullHTTPError builds a PullHTTPError.
func NewPullHTTPError(statusCode int, message string) *PullHTTPError {
	return &PullHTTPError{StatusCode: statusCode, Message: message}
}

// Error implements error.
func (e *PullHTTPError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return fmt.Sprintf("pull: HTTP status %d", e.StatusCode)
}

// ErrBadPayload marks a fetch whose response body could not be decoded.
// Fetchers should wrap this (e.g. errors.New via fmt.Errorf("%w", ErrBadPayload))
// and the collector maps it to ReasonBadPayload.
var ErrBadPayload = errors.New("pull: response payload could not be decoded")

// ErrNotPermitted marks a fetch the peer rejected on permissions (distinct
// from an HTTP 401/403, which maps to ReasonForbidden). Maps to
// ReasonNotPermitted.
var ErrNotPermitted = errors.New("pull: peer refused the pull request as not permitted")

// classifyPullError maps a fetch failure to a PullState stale reason.
func classifyPullError(err error) string {
	if err == nil {
		return ReasonNone
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return ReasonTimeout
	}
	if errors.Is(err, ErrBadPayload) {
		return ReasonBadPayload
	}
	if errors.Is(err, ErrNotPermitted) {
		return ReasonNotPermitted
	}
	var httpErr *PullHTTPError
	if errors.As(err, &httpErr) {
		switch httpErr.StatusCode {
		case 401, 403:
			return ReasonForbidden
		default:
			return ReasonError
		}
	}
	// Other HTTP or transport errors, and anything else unclassifiable.
	return ReasonError
}

// PullState is the observed state of one pull target.
type PullState struct {
	Name          string
	Source        string // always "pull"
	LastAttemptAt time.Time
	LastSuccessAt time.Time
	Stale         bool
	StaleReason   string // one of the Reason* values
	LastSnapshot  *StatsResponse
}

type workerEntry struct {
	cancel context.CancelFunc
	done   chan struct{}
}

// PullCollector pulls stats from a set of peers on per-target schedules.
type PullCollector struct {
	fetcher   PullFetcher
	newTicker func(time.Duration) Ticker
	now       func() time.Time

	ctx    context.Context
	cancel context.CancelFunc
	stopMu sync.Mutex
	wg     sync.WaitGroup

	mu      sync.Mutex
	states  map[string]*PullState
	workers map[string]workerEntry
}

// SetTargets reconciles the running worker set with targets: it starts one
// goroutine per newly-enabled target and stops goroutines for removed or
// disabled targets. Existing per-target state is preserved.
func (c *PullCollector) SetTargets(targets []PullTarget) {
	c.mu.Lock()
	if c.stopped() {
		c.mu.Unlock()
		return
	}
	desired := make(map[string]PullTarget, len(targets))
	for _, t := range targets {
		if !t.Enabled || t.Name == "" {
			continue
		}
		desired[t.Name] = t
	}
	for name, entry := range c.workers {
		if _, ok := desired[name]; !ok {
			entry.cancel()
			delete(c.workers, name)
		}
	}
	for name, t := range desired {
		if _, ok := c.workers[name]; ok {
			continue
		}
		wctx, wcancel := context.WithCancel(c.ctx)
		entry := workerEntry{cancel: wcancel, done: make(chan struct{})}
		c.workers[name] = entry
		c.wg.Add(1)
		go c.runWorker(wctx, entry.done, t)
	}
	c.mu.Unlock()
}

// workerDone returns a channel that closes once the worker for name has
// fully exited (nil if no such worker was ever started). Exposed for tests
// so removal can be observed deterministically.
func (c *PullCollector) workerDone(name string) <-chan struct{} {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.workers[name]
	if !ok {
		return nil
	}
	return entry.done
}

func (c *PullCollector) stopped() bool {
	c.stopMu.Lock()
	defer c.stopMu.Unlock()
	select {
	case <-c.ctx.Done():
		return true
	default:
		return false
	}
}

// runWorker drives one target: an immediate fetch, then one fetch per tick.
func (c *PullCollector) runWorker(wctx context.Context, done chan struct{}, t PullTarget) {
	defer c.wg.Done()
	defer close(done)
	tick := c.newTicker(t.Interval)
	defer tick.Stop()
	c.fetchOnce(t)
	for {
		select {
		case <-wctx.Done():
			return
		case <-tick.Chan():
			c.fetchOnce(t)
		}
	}
}

// fetchOnce performs a single bounded fetch for t and records the outcome
// in that target's PullState.
func (c *PullCollector) fetchOnce(t PullTarget) {
	st := c.stateFor(t.Name)

	c.mu.Lock()
	st.LastAttemptAt = c.now()
	c.mu.Unlock()

	ctx, cancel := context.WithTimeout(c.ctx, t.Timeout)
	defer cancel()
	snap, err := c.fetcher.Fetch(ctx, t.Name)

	c.mu.Lock()
	defer c.mu.Unlock()
	if err != nil {
		st.Stale = true
		st.StaleReason = classifyPullError(err)
		return
	}
	st.Stale = false
	st.StaleReason = ReasonNone
	st.LastSuccessAt = c.now()
	st.LastSnapshot = snap
}

func (c *PullCollector) stateFor(name string) *PullState {
	c.mu.Lock()
	defer c.mu.Unlock()
	st, ok := c.states[name]
	if !ok {
		st = &PullState{Name: name, Source: PullSource, StaleReason: ReasonNone}
		c.states[name] = st
	}
	return st
}

// Snapshot returns a copy of every pull target's state, sorted by name.
func (c *PullCollector) Snapshot() []PullState {
	c.mu.Lock()
	out := make([]PullState, 0, len(c.states))
	for _, st := range c.states {
		out = append(out, *st)
	}
	c.mu.Unlock()
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Last returns a copy of the most recent state recorded for name.
func (c *PullCollector) Last(name string) (PullState, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	st, ok := c.states[name]
	if !ok {
		return PullState{}, false
	}
	return *st, true
}

// Stop halts every worker and releases resources. It is idempotent: only
// the first call does work.
func (c *PullCollector) Stop() {
	c.stopMu.Lock()
	c.cancel()
	c.stopMu.Unlock()
	c.wg.Wait()
}

// Running reports whether the collector has not been stopped.
func (c *PullCollector) Running() bool { return !c.stopped() }
