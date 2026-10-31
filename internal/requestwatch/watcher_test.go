package requestwatch

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/config"
)

// fakeLister returns whatever targets the test last set.
type fakeLister struct{ targets []Target }

func (f *fakeLister) Targets(context.Context) []Target {
	return append([]Target(nil), f.targets...)
}

// fakeSource answers every read with the sample and error the test last set
// and counts the reads.
type fakeSource struct {
	sample Sample
	err    error
	calls  int
	nodes  []string
}

func (f *fakeSource) Sample(_ context.Context, node NodeRef, _ string) (Sample, error) {
	f.calls++
	f.nodes = append(f.nodes, node.Name)
	return f.sample, f.err
}

// watcherRig bundles a watcher with its fakes and the callback records.
type watcherRig struct {
	w         *Watcher
	lister    *fakeLister
	src       *fakeSource
	clock     *fakeClock
	cfg       config.RequestWatchConfig
	noProg    []Observation
	recovered []Observation
	ended     []Observation
}

func boolPtr(b bool) *bool { return &b }

const testKind = "kind-a"

func workerTarget(id, node, model string) Target {
	return Target{SessionID: id, AutomatonID: "a1", TaskID: "t-" + id, Role: RoleWorker,
		NodeName: node, NodeAddress: node + ":1", NodeKind: testKind, Model: model}
}

func newRig(targets ...Target) *watcherRig {
	r := &watcherRig{
		lister: &fakeLister{targets: targets},
		src:    &fakeSource{sample: Sample{Fingerprint: "a"}},
		clock:  newClock(),
		cfg:    config.RequestWatchConfig{NoProgressMinutes: 10},
	}
	selector := func(kind string) Source {
		if kind == testKind {
			return r.src
		}
		return nil
	}
	r.w = NewWatcher(r.lister, selector,
		func() config.RequestWatchConfig { return r.cfg },
		func() time.Time { return r.clock.now },
		Callbacks{
			OnNoProgress: func(o Observation) { r.noProg = append(r.noProg, o) },
			OnRecovered:  func(o Observation) { r.recovered = append(r.recovered, o) },
			OnEnded:      func(o Observation) { r.ended = append(r.ended, o) },
		})
	return r
}

func (r *watcherRig) poll(advance time.Duration) {
	r.clock.advance(advance)
	r.w.Poll(context.Background())
}

func mustObs(t *testing.T, r *watcherRig, id string) Observation {
	t.Helper()
	o, ok := r.w.Observation(id)
	if !ok {
		t.Fatalf("no observation for %s", id)
	}
	return o
}

func TestWatcherPollGenerating(t *testing.T) {
	r := newRig(workerTarget("s1", "n1", "m1"))
	r.poll(0)
	if o := mustObs(t, r, "s1"); o.State != StateIdle {
		t.Fatalf("after baseline state = %q, want idle", o.State)
	}
	r.src.sample = Sample{Fingerprint: "b"}
	r.poll(time.Minute)
	o := mustObs(t, r, "s1")
	if o.State != StateGenerating || o.OverThreshold {
		t.Fatalf("obs = %+v, want generating", o)
	}
	if o.Summary != "waiting on a model request: generating" {
		t.Fatalf("summary = %q", o.Summary)
	}
	if o.AutomatonID != "a1" || o.TaskID != "t-s1" || o.Role != RoleWorker || o.Node != "n1" || o.Model != "m1" {
		t.Fatalf("identity not carried: %+v", o)
	}
	if len(r.noProg) != 0 {
		t.Fatalf("OnNoProgress fired while generating: %+v", r.noProg)
	}
}

func TestWatcherPollIdleThreshold(t *testing.T) {
	r := newRig(workerTarget("s1", "n1", "m1"))
	r.poll(0)
	r.poll(5 * time.Minute)
	if len(r.noProg) != 0 {
		t.Fatalf("fired before the threshold: %+v", r.noProg)
	}
	r.poll(5 * time.Minute)
	if len(r.noProg) != 1 {
		t.Fatalf("OnNoProgress calls = %d, want 1", len(r.noProg))
	}
	if o := r.noProg[0]; o.SessionID != "s1" || !o.OverThreshold || o.State != StateIdle {
		t.Fatalf("callback obs = %+v", o)
	}
	r.poll(5 * time.Minute)
	r.poll(5 * time.Minute)
	if len(r.noProg) != 1 {
		t.Fatalf("OnNoProgress fired again on a later tick: %d calls", len(r.noProg))
	}
	o := mustObs(t, r, "s1")
	if !o.OverThreshold || o.NoProgressSeconds != 1200 {
		t.Fatalf("obs = %+v, want over threshold at 1200s", o)
	}
	if o.Summary != "waiting on a model request: no progress for 20 minutes" {
		t.Fatalf("summary = %q", o.Summary)
	}

	// Progress after the crossing recovers exactly once.
	r.src.sample = Sample{Fingerprint: "b"}
	r.poll(time.Minute)
	if len(r.recovered) != 1 {
		t.Fatalf("OnRecovered calls = %d, want 1", len(r.recovered))
	}
	r.poll(time.Minute)
	if len(r.recovered) != 1 {
		t.Fatalf("OnRecovered fired again: %d calls", len(r.recovered))
	}
}

func TestWatcherPollUnreachable(t *testing.T) {
	r := newRig(workerTarget("s1", "n1", "m1"))
	r.src.err = errors.New("connection refused")
	r.poll(0)
	o := mustObs(t, r, "s1")
	if o.State != StateUnreachable || o.Error != "connection refused" {
		t.Fatalf("obs = %+v, want unreachable with the error", o)
	}
	r.poll(10 * time.Minute)
	if len(r.noProg) != 1 {
		t.Fatalf("OnNoProgress calls = %d, want 1 for an unreachable node", len(r.noProg))
	}
	o = mustObs(t, r, "s1")
	if o.Summary != "waiting on a model request: node unreachable, no progress for 10 minutes" {
		t.Fatalf("summary = %q", o.Summary)
	}

	// With unreachable_counts off the unreachable time never counts.
	r2 := newRig(workerTarget("s1", "n1", "m1"))
	r2.cfg.UnreachableCounts = boolPtr(false)
	r2.src.err = errors.New("connection refused")
	r2.poll(0)
	r2.poll(30 * time.Minute)
	if len(r2.noProg) != 0 {
		t.Fatalf("unreachable counted although unreachable_counts is false: %+v", r2.noProg)
	}
}

func TestWatcherEndedSessionDropped(t *testing.T) {
	r := newRig(workerTarget("s1", "n1", "m1"), workerTarget("s2", "n1", "m1"))
	r.poll(0)
	if len(r.w.Observations()) != 2 {
		t.Fatalf("observations = %d, want 2", len(r.w.Observations()))
	}
	r.lister.targets = r.lister.targets[1:]
	r.poll(time.Minute)
	if _, ok := r.w.Observation("s1"); ok {
		t.Fatal("s1 is no longer a target but still has an observation")
	}
	if len(r.ended) != 1 || r.ended[0].SessionID != "s1" {
		t.Fatalf("OnEnded calls = %+v, want one for s1", r.ended)
	}
	r.poll(time.Minute)
	r.poll(time.Minute)
	if len(r.ended) != 1 {
		t.Fatalf("OnEnded fired again: %d calls", len(r.ended))
	}
	if got := r.w.Observations(); len(got) != 1 || got[0].SessionID != "s2" {
		t.Fatalf("observations = %+v, want only s2", got)
	}
}

func TestWatcherDisabledRecordsNothing(t *testing.T) {
	r := newRig(workerTarget("s1", "n1", "m1"))
	r.cfg.Enabled = boolPtr(false)
	r.poll(0)
	if r.src.calls != 0 {
		t.Fatalf("disabled watcher read the node %d times", r.src.calls)
	}
	if got := r.w.Observations(); len(got) != 0 {
		t.Fatalf("disabled watcher recorded %+v", got)
	}

	// Turning it off clears state recorded while it was on.
	r.cfg.Enabled = nil
	r.poll(0)
	if len(r.w.Observations()) != 1 {
		t.Fatalf("enabled watcher recorded %d observations, want 1", len(r.w.Observations()))
	}
	r.cfg.Enabled = boolPtr(false)
	r.poll(time.Minute)
	if got := r.w.Observations(); len(got) != 0 {
		t.Fatalf("state not cleared on disable: %+v", got)
	}
	if got := r.w.Observations(); got == nil {
		t.Fatal("Observations must never be nil")
	}
}

func TestWatcherRoleFlags(t *testing.T) {
	verifier := workerTarget("v1", "n1", "m1")
	verifier.Role = RoleVerifier
	r := newRig(workerTarget("w1", "n1", "m1"), verifier)

	r.poll(0)
	if got := r.w.Observations(); len(got) != 2 {
		t.Fatalf("both roles on: %d observations, want 2", len(got))
	}

	r.cfg.ObserveWorkers = boolPtr(false)
	r.poll(time.Minute)
	got := r.w.Observations()
	if len(got) != 1 || got[0].SessionID != "v1" || got[0].Role != RoleVerifier {
		t.Fatalf("observe_workers=false: %+v, want only the verifier", got)
	}

	r.cfg.ObserveWorkers = nil
	r.cfg.ObserveVerifiers = boolPtr(false)
	r.poll(time.Minute)
	got = r.w.Observations()
	if len(got) != 1 || got[0].SessionID != "w1" || got[0].Role != RoleWorker {
		t.Fatalf("observe_verifiers=false: %+v, want only the worker", got)
	}

	r.cfg.ObserveWorkers = boolPtr(false)
	r.poll(time.Minute)
	if got := r.w.Observations(); len(got) != 0 {
		t.Fatalf("both roles off: %+v", got)
	}
}

func TestWatcherSharedNodeRead(t *testing.T) {
	r := newRig(workerTarget("s1", "n1", "m1"), workerTarget("s2", "n1", "m1"))
	r.poll(0)
	if r.src.calls != 1 {
		t.Fatalf("source calls = %d, want 1 for two sessions on one node and model", r.src.calls)
	}
	if len(r.w.Observations()) != 2 {
		t.Fatalf("observations = %d, want 2", len(r.w.Observations()))
	}
	r.poll(time.Minute)
	if r.src.calls != 2 {
		t.Fatalf("source calls after second tick = %d, want 2", r.src.calls)
	}

	// A different model, or a different node, is its own read.
	r.lister.targets = append(r.lister.targets, workerTarget("s3", "n1", "m2"), workerTarget("s4", "n2", "m1"))
	r.poll(time.Minute)
	if r.src.calls != 5 {
		t.Fatalf("source calls = %d, want 5 (2 + one per distinct node and model)", r.src.calls)
	}
}

func TestWatcherConfigChangeAppliesNextTick(t *testing.T) {
	r := newRig(workerTarget("s1", "n1", "m1"))
	r.cfg.NoProgressMinutes = 30
	r.poll(0)
	r.poll(12 * time.Minute)
	if len(r.noProg) != 0 || mustObs(t, r, "s1").OverThreshold {
		t.Fatal("crossed a 30 minute threshold after 12 minutes")
	}

	r.cfg.NoProgressMinutes = 10
	r.poll(time.Minute)
	if len(r.noProg) != 1 || !mustObs(t, r, "s1").OverThreshold {
		t.Fatalf("lowered threshold not applied on the next tick: %d calls", len(r.noProg))
	}

	// The role switch is also read live.
	r.cfg.ObserveWorkers = boolPtr(false)
	r.poll(time.Minute)
	if len(r.w.Observations()) != 0 {
		t.Fatal("observe_workers=false not applied on the next tick")
	}
}

func TestWatcherNoSourceForKind(t *testing.T) {
	other := workerTarget("s2", "n2", "m1")
	other.NodeKind = "kind-without-a-source"
	r := newRig(workerTarget("s1", "n1", "m1"), other)
	r.poll(0)
	got := r.w.Observations()
	if len(got) != 1 || got[0].SessionID != "s1" {
		t.Fatalf("observations = %+v, want only the target whose kind has a source", got)
	}
	if r.src.calls != 1 {
		t.Fatalf("source calls = %d, want 1", r.src.calls)
	}
	if _, ok := r.w.Observation("s2"); ok {
		t.Fatal("a target without a source must be skipped")
	}
	if len(r.ended) != 0 {
		t.Fatalf("a skipped target is not an ended session: %+v", r.ended)
	}
}

func TestWatcherObservationsSortedCopies(t *testing.T) {
	r := newRig(workerTarget("s3", "n1", "m1"), workerTarget("s1", "n1", "m1"), workerTarget("s2", "n1", "m1"))
	r.poll(0)
	got := r.w.Observations()
	if len(got) != 3 || got[0].SessionID != "s1" || got[1].SessionID != "s2" || got[2].SessionID != "s3" {
		t.Fatalf("not sorted by session id: %+v", got)
	}
	got[0].State = StateUnreachable
	if mustObs(t, r, "s1").State == StateUnreachable {
		t.Fatal("Observations returned a view onto the watcher's state, not a copy")
	}
}
