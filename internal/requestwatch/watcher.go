package requestwatch

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/dmz006/datawatch/internal/config"
)

// Target is one session whose model request the watcher observes. The daemon
// builds the list; the watcher never decides what is a target beyond the role
// switches and whether a Source exists for the node's kind.
type Target struct {
	// SessionID is the watched session's id.
	SessionID string
	// AutomatonID and TaskID name the Automaton task the session works for.
	AutomatonID string
	TaskID      string
	// Role is RoleWorker or RoleVerifier.
	Role string
	// NodeName, NodeAddress, NodeKind and ObserverPeer describe the compute
	// node the session's model runs on.
	NodeName     string
	NodeAddress  string
	NodeKind     string
	ObserverPeer string
	// Model is the model the session asked the node for.
	Model string
}

// TargetLister lists the sessions to watch right now. An implementation must
// be cheap and must never make a model call.
type TargetLister interface {
	Targets(ctx context.Context) []Target
}

// SourceFor picks the Source that reads nodes of a kind. It returns nil when
// the kind has no source, and the watcher then skips the target. This is the
// extension point for another node kind.
type SourceFor func(kind string) Source

// Callbacks are the optional hooks a Watcher fires. Any of them may be nil.
// They run on the polling goroutine after the tick's state is updated and
// outside the watcher's lock, so a callback may call back into the Reader.
type Callbacks struct {
	// OnNoProgress fires once per episode when a session first passes the
	// no-progress threshold.
	OnNoProgress func(Observation)
	// OnRecovered fires when progress resumes after OnNoProgress.
	OnRecovered func(Observation)
	// OnEnded fires once when a session stops being a target. It receives the
	// last observation of that session.
	OnEnded func(Observation)
}

// Watcher polls the node signal of every target and keeps one Tracker per
// session. It is observation only: it makes no model call and performs no
// remediation. It is safe for concurrent use.
type Watcher struct {
	lister  TargetLister
	sources SourceFor
	cfg     func() config.RequestWatchConfig
	now     func() time.Time
	cb      Callbacks

	// pollMu serialises ticks so a manual Poll and the Start loop never
	// overlap.
	pollMu sync.Mutex

	mu       sync.RWMutex
	trackers map[string]*Tracker
	ids      map[string]Identity
	started  bool
}

// NewWatcher returns a watcher. cfg is read on every tick so a configuration
// change applies with no restart; a nil cfg reads the defaults. now is the
// clock; nil uses the wall clock.
func NewWatcher(lister TargetLister, sources SourceFor, cfg func() config.RequestWatchConfig, now func() time.Time, cb Callbacks) *Watcher {
	if cfg == nil {
		cfg = func() config.RequestWatchConfig { return config.RequestWatchConfig{} }
	}
	if now == nil {
		now = time.Now
	}
	return &Watcher{
		lister:   lister,
		sources:  sources,
		cfg:      cfg,
		now:      now,
		cb:       cb,
		trackers: map[string]*Tracker{},
		ids:      map[string]Identity{},
	}
}

var _ Reader = (*Watcher)(nil)

// Observations returns a copy of every current observation ordered by session
// id. It is never nil.
func (w *Watcher) Observations() []Observation {
	w.mu.RLock()
	out := make([]Observation, 0, len(w.trackers))
	for _, t := range w.trackers {
		out = append(out, t.Observation())
	}
	w.mu.RUnlock()
	sort.Slice(out, func(i, j int) bool { return out[i].SessionID < out[j].SessionID })
	return out
}

// Observation returns the current observation of one session.
func (w *Watcher) Observation(sessionID string) (Observation, bool) {
	w.mu.RLock()
	t, ok := w.trackers[sessionID]
	w.mu.RUnlock()
	if !ok {
		return Observation{}, false
	}
	return t.Observation(), true
}

// Start runs Poll once at once and then every poll_seconds until ctx is done.
// The interval is re-read from the live configuration after every tick. Start
// returns immediately; a second call while the loop runs does nothing.
func (w *Watcher) Start(ctx context.Context) {
	w.mu.Lock()
	if w.started {
		w.mu.Unlock()
		return
	}
	w.started = true
	w.mu.Unlock()

	go func() {
		defer func() {
			w.mu.Lock()
			w.started = false
			w.mu.Unlock()
		}()
		for {
			w.Poll(ctx)
			wait := time.Duration(w.cfg().EffectivePollSeconds()) * time.Second
			timer := time.NewTimer(wait)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
		}
	}()
}

// nodeKey identifies one node read: sessions on the same node and model share
// it.
type nodeKey struct{ node, model string }

// nodeRead is the outcome of one shared node read.
type nodeRead struct {
	sample Sample
	err    error
}

// event is a callback to fire once the tick's state is stored.
type event struct {
	fn  func(Observation)
	obs Observation
}

// Poll runs one tick. With the watch disabled it records nothing and clears
// the state. Otherwise it reads the targets, takes one node read per node and
// model, folds each session's sample into its Tracker and drops the sessions
// that are no longer targets.
func (w *Watcher) Poll(ctx context.Context) {
	w.pollMu.Lock()
	defer w.pollMu.Unlock()

	cfg := w.cfg()
	if !cfg.EffectiveEnabled() {
		w.mu.Lock()
		w.trackers = map[string]*Tracker{}
		w.ids = map[string]Identity{}
		w.mu.Unlock()
		return
	}
	if w.lister == nil || ctx.Err() != nil {
		return
	}

	params := Params{
		NoProgress:        time.Duration(cfg.EffectiveNoProgressMinutes()) * time.Minute,
		UseGPU:            cfg.EffectiveUseGPUSignal(),
		GPUBusyPercent:    float64(cfg.EffectiveGPUBusyPercent()),
		UnreachableCounts: cfg.EffectiveUnreachableCounts(),
	}

	type watched struct {
		target Target
		source Source
	}
	var current []watched
	for _, t := range w.lister.Targets(ctx) {
		if t.SessionID == "" {
			continue
		}
		switch t.Role {
		case RoleWorker:
			if !cfg.EffectiveObserveWorkers() {
				continue
			}
		case RoleVerifier:
			if !cfg.EffectiveObserveVerifiers() {
				continue
			}
		default:
			continue
		}
		if w.sources == nil {
			continue
		}
		src := w.sources(t.NodeKind)
		if src == nil {
			continue
		}
		current = append(current, watched{target: t, source: src})
	}

	now := w.now()
	reads := map[nodeKey]nodeRead{}
	live := make(map[string]bool, len(current))
	var events []event

	for _, c := range current {
		if ctx.Err() != nil {
			return
		}
		t := c.target
		live[t.SessionID] = true

		key := nodeKey{node: t.NodeName, model: t.Model}
		read, done := reads[key]
		if !done {
			s, err := c.source.Sample(ctx, NodeRef{
				Name:         t.NodeName,
				Address:      t.NodeAddress,
				Kind:         t.NodeKind,
				ObserverPeer: t.ObserverPeer,
			}, t.Model)
			read = nodeRead{sample: s, err: err}
			reads[key] = read
		}

		id := Identity{
			SessionID:   t.SessionID,
			AutomatonID: t.AutomatonID,
			TaskID:      t.TaskID,
			Role:        t.Role,
			Node:        t.NodeName,
			Model:       t.Model,
		}
		w.mu.Lock()
		tr, ok := w.trackers[t.SessionID]
		if !ok || w.ids[t.SessionID] != id {
			tr = NewTracker(id)
			w.trackers[t.SessionID] = tr
			w.ids[t.SessionID] = id
		}
		w.mu.Unlock()

		obs, crossed, recovered := tr.Observe(now, read.sample, read.err, params)
		if crossed && w.cb.OnNoProgress != nil {
			events = append(events, event{w.cb.OnNoProgress, obs})
		}
		if recovered && w.cb.OnRecovered != nil {
			events = append(events, event{w.cb.OnRecovered, obs})
		}
	}

	// Drop every session that is no longer a target.
	var ended []Observation
	w.mu.Lock()
	for sid, tr := range w.trackers {
		if live[sid] {
			continue
		}
		ended = append(ended, tr.Observation())
		delete(w.trackers, sid)
		delete(w.ids, sid)
	}
	w.mu.Unlock()
	sort.Slice(ended, func(i, j int) bool { return ended[i].SessionID < ended[j].SessionID })
	if w.cb.OnEnded != nil {
		for _, o := range ended {
			events = append(events, event{w.cb.OnEnded, o})
		}
	}

	for _, e := range events {
		e.fn(e.obs)
	}
}
