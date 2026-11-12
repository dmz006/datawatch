package requestwatch

import (
	"sync"
	"time"
)

// Identity says which session an Observation belongs to.
type Identity struct {
	SessionID   string
	AutomatonID string
	TaskID      string
	Role        string
	Node        string
	Model       string
}

// Tracker turns the samples of one session into an Observation. It keeps the
// previous fingerprint, the time of the last progress and whether the current
// no-progress episode has already crossed the threshold. It is safe for
// concurrent use. It never reads the wall clock: the caller passes the time.
type Tracker struct {
	mu         sync.Mutex
	id         Identity
	baselined  bool
	lastFP     string
	lastChange time.Time
	crossed    bool
	obs        Observation
}

// NewTracker returns a tracker for one session. Before the first Observe its
// Observation has the state unknown.
func NewTracker(id Identity) *Tracker {
	t := &Tracker{id: id}
	t.obs = Observation{
		SessionID:   id.SessionID,
		AutomatonID: id.AutomatonID,
		TaskID:      id.TaskID,
		Role:        id.Role,
		Node:        id.Node,
		Model:       id.Model,
		State:       StateUnknown,
	}
	t.obs.Summary = Summary(t.obs)
	return t
}

// Observation returns the latest observation.
func (t *Tracker) Observation() Observation {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.obs
}

// gpuBusy reports whether the sample's GPU reading counts as progress.
func gpuBusy(s Sample, p Params) bool {
	return p.UseGPU && s.GPUKnown && p.GPUBusyPercent > 0 && s.GPUPercent >= p.GPUBusyPercent
}

// Observe folds one poll into the tracker. s is the sample and err the error
// from the Source; when err is not nil the sample is ignored and the node
// counts as unreachable. p is read fresh on every call.
//
// Crossed is true exactly once per episode, on the poll where the no-progress
// time first reaches p.NoProgress. Recovered is true on the poll where
// progress resumes after a crossing, which also ends the episode.
func (t *Tracker) Observe(now time.Time, s Sample, err error, p Params) (obs Observation, crossed, recovered bool) {
	t.mu.Lock()
	defer t.mu.Unlock()

	state := StateIdle
	errText := ""
	progress := false

	switch {
	case err != nil:
		state = StateUnreachable
		errText = err.Error()
		if t.lastChange.IsZero() || !p.UnreachableCounts {
			t.lastChange = now
		}
	case !t.baselined:
		t.baselined = true
		t.lastFP = s.Fingerprint
		t.lastChange = now
		if gpuBusy(s, p) {
			state = StateGenerating
		}
	case s.Fingerprint != t.lastFP || gpuBusy(s, p):
		t.lastFP = s.Fingerprint
		t.lastChange = now
		state = StateGenerating
		progress = true
	}

	if progress && t.crossed {
		recovered = true
		t.crossed = false
	}

	age := now.Sub(t.lastChange)
	if age < 0 {
		age = 0
	}
	over := false
	if p.NoProgress > 0 && age >= p.NoProgress {
		over = state == StateIdle || (state == StateUnreachable && p.UnreachableCounts)
	}
	if over && !t.crossed {
		t.crossed = true
		crossed = true
	}

	o := Observation{
		SessionID:         t.id.SessionID,
		AutomatonID:       t.id.AutomatonID,
		TaskID:            t.id.TaskID,
		Role:              t.id.Role,
		Node:              t.id.Node,
		Model:             t.id.Model,
		State:             state,
		LastChange:        formatTime(t.lastChange),
		NoProgressSeconds: int64(age / time.Second),
		LastPoll:          formatTime(now),
		OverThreshold:     over,
		Error:             errText,
	}
	o.Summary = Summary(o)
	t.obs = o
	return o, crossed, recovered
}
