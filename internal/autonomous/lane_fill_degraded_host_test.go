package autonomous

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// fakeHostHolders reports a fixed set of pool hosts as holding work.
type fakeHostHolders struct{ held map[string]bool }

func (f *fakeHostHolders) HeldHosts(ctx context.Context) map[string]bool { return f.held }

// degradedRig is an evidence source over two fake nodes: node-a is readable,
// node-b is unreachable. The clock and the threshold are set by the test.
type degradedRig struct {
	now       time.Time
	downAfter int
	fetch     *fakeModelFetcher
	holders   *fakeHostHolders
	src       PoolEvidenceSource
}

func newDegradedRig(t *testing.T, downAfter int) *degradedRig {
	t.Helper()
	r := &degradedRig{
		now:       time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC),
		downAfter: downAfter,
		holders:   &fakeHostHolders{held: map[string]bool{}},
		fetch: &fakeModelFetcher{
			models: map[string][]string{"http://a": {"model-one"}},
			errs:   map[string]error{"http://b": errors.New("connection refused")},
		},
	}
	hosts := &fakeHostLister{hosts: []PoolHost{
		{Name: "node-a", Endpoint: "http://a"},
		{Name: "node-b", Endpoint: "http://b"},
	}}
	r.src = NewPoolEvidenceSource(&fakeSessionLister{}, hosts, r.fetch,
		PoolEvidenceClock(func() time.Time { return r.now }),
		PoolEvidenceHostDownAfter(func() int { return r.downAfter }),
		PoolEvidenceHolders(r.holders))
	return r
}

func (r *degradedRig) check(t *testing.T) (PoolEvidence, error) {
	t.Helper()
	return r.src.Check(context.Background())
}

func (r *degradedRig) firstFailOf(host string) (time.Time, bool) {
	s := r.src.(*poolEvidenceSource)
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.firstFail[host]
	return v, ok
}

// pastDownAfter primes the first failure and moves the clock past the
// threshold, so the next read sees node-b as down.
func (r *degradedRig) pastDownAfter(t *testing.T) {
	t.Helper()
	r.check(t) //nolint:errcheck // first failure starts the clock
	r.now = r.now.Add(time.Duration(r.downAfter)*time.Minute + time.Second)
}

func gpuLanesAllBlocked(t *testing.T, lanes []Lane) {
	t.Helper()
	n := 0
	for _, l := range lanes {
		if l.Type != LaneTypeGPU {
			continue
		}
		n++
		if !strings.Contains(l.Blocker, "pool evidence unavailable") {
			t.Errorf("gpu lane %s blocker = %q, want pool evidence unavailable", l.ID, l.Blocker)
		}
	}
	if n == 0 {
		t.Fatal("no gpu lanes in the model")
	}
}

func TestLaneFillUnreadableHostNoHolder(t *testing.T) {
	r := newDegradedRig(t, 10)
	r.pastDownAfter(t)
	firstFail := r.now.Add(-(10*time.Minute + time.Second))

	ev, err := r.check(t)
	if err != nil {
		t.Fatalf("degraded host gave an error: %v", err)
	}
	if len(ev.LoadedModels) != 1 || ev.LoadedModels[0] != (LoadedModel{Host: "node-a", Model: "model-one"}) {
		t.Fatalf("loaded models = %+v, want the readable host's model", ev.LoadedModels)
	}
	if len(ev.Degraded) != 1 {
		t.Fatalf("degraded = %+v, want one entry", ev.Degraded)
	}
	d := ev.Degraded[0]
	if d.Host != "node-b" || !strings.Contains(d.Error, "node-b") || !strings.Contains(d.Error, "connection refused") {
		t.Errorf("degraded entry = %+v, want it to name the host and the error", d)
	}
	if !d.Since.Equal(firstFail) {
		t.Errorf("degraded since = %v, want %v", d.Since, firstFail)
	}

	// Through the lane builder and the planner: the GPU lane is idle.
	cfg := laneFillCoordConfig("shadow").LaneFill
	model := BuildLanes(cfg, nil, LaneEvidence{GPUBusy: ev.ActiveGPUSessions})
	for _, l := range model.Lanes {
		if l.Type == LaneTypeGPU && (l.Occupant != nil || l.Blocker != "") {
			t.Fatalf("gpu lane %s = %+v, want idle and not blocked", l.ID, l)
		}
	}
	decisions := PlanLaneFill(LaneFillInput{Config: cfg, Lanes: model.Lanes, Now: r.now})
	found := false
	for _, dec := range decisions {
		if dec.LaneType != LaneTypeGPU {
			continue
		}
		if dec.Action == LaneFillActionBlocked {
			t.Errorf("gpu lane judged blocked: %+v", dec)
		}
		if dec.Action == LaneFillActionIdle {
			found = true
		}
	}
	if !found {
		t.Errorf("no idle decision for a gpu lane in %+v", decisions)
	}
}

func TestLaneFillHoldingHostStillBlocks(t *testing.T) {
	cases := map[string]func(r *degradedRig){
		// A lease, a placement and a lane node all reach the source through
		// the holders; each is one way node-b ends up in the held set.
		"lease":     func(r *degradedRig) { r.holders.held["node-b"] = true },
		"placement": func(r *degradedRig) { r.holders.held["node-b"] = true },
		"lane node": func(r *degradedRig) { r.holders.held["node-b"] = true },
	}
	cfg := laneFillCoordConfig("shadow").LaneFill
	for name, hold := range cases {
		t.Run(name, func(t *testing.T) {
			r := newDegradedRig(t, 10)
			hold(r)
			r.pastDownAfter(t)

			ev, err := r.check(t)
			if err == nil || !strings.Contains(err.Error(), "node-b") {
				t.Fatalf("err = %v, want a blocking error naming node-b", err)
			}
			if len(ev.Degraded) != 0 {
				t.Errorf("a holding host was listed as degraded: %+v", ev.Degraded)
			}
			model := BuildLanes(cfg, nil, LaneEvidence{GPUBusy: ev.ActiveGPUSessions, Err: err.Error()})
			gpuLanesAllBlocked(t, model.Lanes)
		})
	}
}

func TestLaneFillDownAfterBoundary(t *testing.T) {
	r := newDegradedRig(t, 10)
	step := func(d time.Duration) (PoolEvidence, error) {
		r.now = r.now.Add(d)
		return r.check(t)
	}

	// The first failure starts the clock and blocks.
	start := r.now
	if _, err := r.check(t); err == nil {
		t.Fatal("first failure did not block")
	}
	if got, ok := r.firstFailOf("node-b"); !ok || !got.Equal(start) {
		t.Fatalf("first failure = %v, %v, want %v", got, ok, start)
	}

	// The 5-second cache must not hide the crossing: a poll inside the
	// window returns the cached blocking result, one after it re-reads.
	if _, err := step(time.Second); err == nil {
		t.Fatal("cached read did not block")
	}

	// Exactly one minute under the threshold: still blocking.
	r.now = start.Add(9 * time.Minute)
	if _, err := r.check(t); err == nil {
		t.Fatal("9 minutes of failure with a 10 minute threshold did not block")
	}
	if got, _ := r.firstFailOf("node-b"); !got.Equal(start) {
		t.Fatalf("first failure moved to %v across polls, want %v", got, start)
	}

	// Six seconds under the threshold (past the cache window): still blocking.
	r.now = start.Add(10*time.Minute - 6*time.Second)
	if _, err := r.check(t); err == nil {
		t.Fatal("just under the threshold did not block")
	}

	// Exactly at the threshold: degraded.
	r.now = start.Add(10 * time.Minute)
	ev, err := r.check(t)
	if err != nil || len(ev.Degraded) != 1 || !ev.Degraded[0].Since.Equal(start) {
		t.Fatalf("at the threshold: ev = %+v, err = %v, want degraded since %v", ev.Degraded, err, start)
	}

	// Over the threshold: still degraded.
	r.now = start.Add(30 * time.Minute)
	ev, err = r.check(t)
	if err != nil || len(ev.Degraded) != 1 {
		t.Fatalf("over the threshold: ev = %+v, err = %v, want degraded", ev.Degraded, err)
	}

	// A threshold of 0 always blocks, however long the host has been down.
	r.downAfter = 0
	r.now = start.Add(time.Hour)
	ev, err = r.check(t)
	if err == nil || len(ev.Degraded) != 0 {
		t.Fatalf("threshold 0: ev = %+v, err = %v, want a blocking error", ev.Degraded, err)
	}
}

func TestLaneFillRecoveredHostClearsWarning(t *testing.T) {
	r := newDegradedRig(t, 5)
	r.pastDownAfter(t)
	if ev, err := r.check(t); err != nil || len(ev.Degraded) != 1 {
		t.Fatalf("setup: ev = %+v, err = %v, want degraded", ev.Degraded, err)
	}

	// The host answers again.
	r.fetch.mu.Lock()
	delete(r.fetch.errs, "http://b")
	r.fetch.models["http://b"] = []string{"model-two"}
	r.fetch.mu.Unlock()
	r.now = r.now.Add(time.Minute)
	ev, err := r.check(t)
	if err != nil {
		t.Fatalf("recovered host gave an error: %v", err)
	}
	if len(ev.Degraded) != 0 {
		t.Fatalf("degraded = %+v, want empty after recovery", ev.Degraded)
	}
	if len(ev.LoadedModels) != 2 {
		t.Errorf("loaded models = %+v, want both hosts", ev.LoadedModels)
	}
	if _, ok := r.firstFailOf("node-b"); ok {
		t.Error("first-failure time kept after a successful read")
	}

	// A later failure starts a new clock and blocks again until it ages.
	r.fetch.mu.Lock()
	r.fetch.errs["http://b"] = errors.New("connection refused")
	r.fetch.mu.Unlock()
	r.now = r.now.Add(time.Hour)
	again := r.now
	if _, err := r.check(t); err == nil {
		t.Fatal("a fresh failure after recovery did not block")
	}
	if got, ok := r.firstFailOf("node-b"); !ok || !got.Equal(again) {
		t.Fatalf("new first failure = %v, %v, want %v", got, ok, again)
	}
}

func TestLaneFillDegradedHostLeavesPoolWithoutKeepingClock(t *testing.T) {
	r := newDegradedRig(t, 5)
	r.check(t) //nolint:errcheck
	s := r.src.(*poolEvidenceSource)
	s.hosts = &fakeHostLister{hosts: []PoolHost{{Name: "node-a", Endpoint: "http://a"}}}
	r.now = r.now.Add(time.Minute)
	if _, err := r.check(t); err != nil {
		t.Fatal(err)
	}
	if _, ok := r.firstFailOf("node-b"); ok {
		t.Error("a host that left the pool kept its first-failure time")
	}
}

// newDegradedHarness is the coordinator harness over the real source with
// node-b unreachable.
func newDegradedHarness(t *testing.T, held bool) (*laneFillHarness, *degradedRig) {
	t.Helper()
	h := newLaneFillHarness(t, "shadow")
	r := newDegradedRig(t, 10)
	r.now = h.now
	r.holders.held["node-b"] = held
	r.src = NewPoolEvidenceSource(&fakeSessionLister{},
		&fakeHostLister{hosts: []PoolHost{{Name: "node-a", Endpoint: "http://a"}, {Name: "node-b", Endpoint: "http://b"}}}, r.fetch,
		PoolEvidenceClock(func() time.Time { return h.now }),
		PoolEvidenceHostDownAfter(func() int { return r.downAfter }),
		PoolEvidenceHolders(r.holders))
	h.c.SetLaneFillEvidenceSource(r.src)
	h.c.SetReady()
	return h, r
}

func TestLaneFillCoordinatorShowsDegradedWithoutError(t *testing.T) {
	h, _ := newDegradedHarness(t, false)
	h.poll() // starts the clock; node-b still blocks
	h.now = h.now.Add(11 * time.Minute)
	h.poll()

	st := h.status()
	if st.Evidence.Error != "" {
		t.Fatalf("evidence error = %q, want none for a degraded host", st.Evidence.Error)
	}
	if len(st.Evidence.Degraded) != 1 || st.Evidence.Degraded[0].Host != "node-b" ||
		!strings.Contains(st.Evidence.Degraded[0].Error, "connection refused") {
		t.Fatalf("evidence.degraded = %+v", st.Evidence.Degraded)
	}
	for _, l := range st.Lanes {
		if l.Type == LaneTypeGPU && strings.Contains(l.Blocker, "pool evidence unavailable") {
			t.Errorf("gpu lane blocked by a degraded host: %+v", l)
		}
	}
	if len(st.Evidence.LoadedModels) != 1 || st.Evidence.LoadedModels[0].Host != "node-a" {
		t.Errorf("loaded models = %+v, want the readable host's", st.Evidence.LoadedModels)
	}
}

func TestLaneFillCoordinatorEmptyDegradedWhenHealthy(t *testing.T) {
	h := newLaneFillHarness(t, "shadow")
	h.c.SetReady()
	h.poll()
	if d := h.status().Evidence.Degraded; d == nil || len(d) != 0 {
		t.Fatalf("degraded = %#v, want an empty non-nil list", d)
	}
}

func TestLaneFillCoordinatorBlockingFaultKeepsDegradedSeparate(t *testing.T) {
	h := newLaneFillHarness(t, "shadow")
	since := h.now.Add(-time.Hour)
	h.c.SetLaneFillEvidenceSource(&laneFillEvidenceFake{
		ev: PoolEvidence{
			CheckedAt: h.now,
			Degraded:  []PoolHostDegraded{{Host: "node-b", Error: "pool host node-b unreadable: refused", Since: since}},
		},
		err: errors.New("pool host node-c unreadable (loaded models): timeout"),
	})
	h.c.SetReady()
	h.poll()

	st := h.status()
	if !strings.Contains(st.Evidence.Error, "node-c") || strings.Contains(st.Evidence.Error, "node-b") {
		t.Errorf("evidence error = %q, want only the blocking host", st.Evidence.Error)
	}
	if len(st.Evidence.Degraded) != 1 || st.Evidence.Degraded[0].Host != "node-b" || !st.Evidence.Degraded[0].Since.Equal(since) {
		t.Fatalf("evidence.degraded = %+v", st.Evidence.Degraded)
	}
	for _, l := range st.Lanes {
		if l.Type == LaneTypeGPU {
			if !strings.Contains(l.Blocker, "node-c") || strings.Contains(l.Blocker, "node-b") {
				t.Errorf("gpu lane blocker = %q, want the blocking fault only", l.Blocker)
			}
		}
	}
}

func TestLaneFillCoordinatorHoldingHostStillBlocks(t *testing.T) {
	h, _ := newDegradedHarness(t, true)
	h.poll()
	h.now = h.now.Add(11 * time.Minute)
	h.poll()
	st := h.status()
	if !strings.Contains(st.Evidence.Error, "node-b") || len(st.Evidence.Degraded) != 0 {
		t.Fatalf("evidence = %+v, want a blocking error and no degraded entry", st.Evidence)
	}
}
