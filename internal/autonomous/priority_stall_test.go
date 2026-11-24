package autonomous

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"
)

// fakeStallSource is a read-only decision source. It has no mutating
// method at all; calls counts every read so a test can see nothing else
// was asked of it.
type fakeStallSource struct {
	refusal map[string][2]string
	full    map[string]bool
	merge   map[string]bool
	judge   map[string]bool
	calls   int
}

func newFakeStallSource() *fakeStallSource {
	return &fakeStallSource{refusal: map[string][2]string{}, full: map[string]bool{}, merge: map[string]bool{}, judge: map[string]bool{}}
}
func (f *fakeStallSource) Refusal(p *PRD) (string, string) {
	f.calls++
	r := f.refusal[p.ID]
	return r[0], r[1]
}
func (f *fakeStallSource) LaneFull(p *PRD) bool      { f.calls++; return f.full[p.ID] }
func (f *fakeStallSource) MergePending(p *PRD) bool  { f.calls++; return f.merge[p.ID] }
func (f *fakeStallSource) JudgeTimedOut(p *PRD) bool { f.calls++; return f.judge[p.ID] }

type fakeWakeSink struct{ events []PriorityStall }

func (f *fakeWakeSink) Wake(s PriorityStall) { f.events = append(f.events, s) }

var stallNow = time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

func stallPRD(id string, prio, ageMin int) *PRD {
	return &PRD{ID: id, Priority: prio, Status: PRDNeedsReview, CreatedAt: stallNow.Add(-time.Duration(ageMin) * time.Minute)}
}

func stallCfg() SupervisorWatchConfig { return SupervisorWatchConfig{} }

func TestStallBlockerMapping(t *testing.T) {
	cases := []struct {
		name   string
		setup  func(f *fakeStallSource)
		code   string
		action string
	}{
		{"lane full", func(f *fakeStallSource) { f.full["a"] = true }, StallBlockerLaneFull, "wait for a lane or free one"},
		{"coverage", func(f *fakeStallSource) { f.refusal["a"] = [2]string{LaneFillReasonPlanCoverageBlocked, "x"} }, StallBlockerPlanCoverage, "acknowledge the coverage failure or re-decompose the Automaton"},
		{"held", func(f *fakeStallSource) { f.refusal["a"] = [2]string{LaneFillReasonHeld, "x"} }, StallBlockerHeld, "release the hold"},
		{"operator hold", func(f *fakeStallSource) { f.refusal["a"] = [2]string{LaneFillReasonOperatorHold, "x"} }, StallBlockerHeld, "release the hold"},
		{"stale", func(f *fakeStallSource) { f.refusal["a"] = [2]string{LaneFillReasonStale, "x"} }, StallBlockerStale, "refresh the Automaton or re-decompose it"},
		{"proposal", func(f *fakeStallSource) { f.refusal["a"] = [2]string{LaneFillReasonProposalNotConfirmed, "x"} }, StallBlockerProposalNotConfirmed, "confirm the proposal"},
		{"judge", func(f *fakeStallSource) { f.judge["a"] = true }, StallBlockerJudgeTimedOut, "re-run the judge"},
		{"merge", func(f *fakeStallSource) { f.merge["a"] = true }, StallBlockerMergePending, "merge the branch"},
		{"other", func(f *fakeStallSource) { f.refusal["a"] = [2]string{LaneFillReasonWorktreeInvalid, "worktree gone"} }, StallBlockerOther, "clear the reason named in the text"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			src := newFakeStallSource()
			tc.setup(src)
			d := NewStallDetector(src, &fakeWakeSink{})
			got := d.Scan([]*PRD{stallPRD("a", 90, 100)}, stallCfg(), stallNow)
			if len(got) != 1 {
				t.Fatalf("want 1 stall, got %d", len(got))
			}
			s := got[0]
			if s.BlockerCode != tc.code || s.Action != tc.action || s.BlockerText == "" {
				t.Fatalf("got %+v", s)
			}
			if s.AutomatonID != "a" || s.Priority != 90 || s.WaitingMinutes != 100 {
				t.Fatalf("record fields wrong: %+v", s)
			}
		})
	}
}

func TestStallSkipsBelowFloorAndThreshold(t *testing.T) {
	src := newFakeStallSource()
	src.full["low"], src.full["young"], src.full["ok"] = true, true, true
	sink := &fakeWakeSink{}
	d := NewStallDetector(src, sink)
	cfg := stallCfg()
	floor, thr := cfg.EffectivePriorityFloor(), cfg.EffectiveStallThresholdMinutes()
	prds := []*PRD{
		stallPRD("low", floor-1, thr+50),
		stallPRD("young", floor+5, thr), // age == threshold is not over it
		stallPRD("ok", floor, thr+1),
	}
	got := d.Scan(prds, cfg, stallNow)
	if len(got) != 1 || got[0].AutomatonID != "ok" {
		t.Fatalf("got %+v", got)
	}
	if len(sink.events) != 1 {
		t.Fatalf("wakes %d", len(sink.events))
	}
}

func TestStallNoBlockerNoStall(t *testing.T) {
	d := NewStallDetector(newFakeStallSource(), &fakeWakeSink{})
	if got := d.Scan([]*PRD{stallPRD("a", 99, 500)}, stallCfg(), stallNow); len(got) != 0 {
		t.Fatalf("got %+v", got)
	}
}

func TestStallOneWakePerBlockerAndRewake(t *testing.T) {
	src := newFakeStallSource()
	src.full["a"] = true
	sink := &fakeWakeSink{}
	d := NewStallDetector(src, sink)
	p := []*PRD{stallPRD("a", 90, 100)}
	for i := 0; i < 50; i++ { // many ticks, no storm
		d.Scan(p, stallCfg(), stallNow.Add(time.Duration(i)*time.Minute))
	}
	if len(sink.events) != 1 {
		t.Fatalf("want 1 wake after 50 ticks, got %d", len(sink.events))
	}
	src.full["a"] = false
	src.merge["a"] = true
	for i := 0; i < 20; i++ {
		d.Scan(p, stallCfg(), stallNow)
	}
	if len(sink.events) != 2 || sink.events[1].BlockerCode != StallBlockerMergePending {
		t.Fatalf("want re-wake on change, got %+v", sink.events)
	}
}

func TestStallStateClearedWhenStallEnds(t *testing.T) {
	src := newFakeStallSource()
	src.full["a"] = true
	sink := &fakeWakeSink{}
	d := NewStallDetector(src, sink)
	p := []*PRD{stallPRD("a", 90, 100)}
	d.Scan(p, stallCfg(), stallNow)
	src.full["a"] = false
	d.Scan(p, stallCfg(), stallNow) // no longer stalling
	if len(d.woken) != 0 {
		t.Fatalf("state not cleared: %v", d.woken)
	}
	src.full["a"] = true
	d.Scan(p, stallCfg(), stallNow)
	if len(sink.events) != 2 {
		t.Fatalf("same blocker after a stall ended must wake again, got %d", len(sink.events))
	}
	// Automaton leaves the list entirely.
	d.Scan(nil, stallCfg(), stallNow)
	if len(d.woken) != 0 {
		t.Fatalf("state not cleared for removed Automaton")
	}
}

// The fake exposes only read methods; this test asserts the detector asks
// the source nothing but those reads, and that a scan leaves the Automaton
// untouched.
func TestStallDetectorOnlyReads(t *testing.T) {
	src := newFakeStallSource()
	src.full["a"] = true
	p := stallPRD("a", 90, 100)
	before := *p
	d := NewStallDetector(src, &fakeWakeSink{})
	d.Scan([]*PRD{p}, stallCfg(), stallNow)
	if src.calls == 0 {
		t.Fatal("source never read")
	}
	if !reflect.DeepEqual(*p, before) {
		t.Fatalf("Automaton mutated: %+v vs %+v", *p, before)
	}
	var _ LaneFillDecisionSource = src // no approve/run/acknowledge method exists on the interface
}

func TestStallWakeSettingDefaultsOn(t *testing.T) {
	var c SupervisorWatchConfig
	if !c.EffectiveStallWake() {
		t.Fatal("unset stall_wake must read on")
	}
	off := false
	c.StallWake = &off
	if c.EffectiveStallWake() {
		t.Fatal("explicit false must read off")
	}
	if !DefaultSupervisorWatchConfig().EffectiveStallWake() {
		t.Fatal("default config must have the wake on")
	}
}

func TestStallWakeDisabledSkipsSinkButReturnsStalls(t *testing.T) {
	src := newFakeStallSource()
	src.full["a"] = true
	sink := &fakeWakeSink{}
	d := NewStallDetector(src, sink)
	off := false
	cfg := SupervisorWatchConfig{StallWake: &off}
	now := time.Now()
	p := workTestQueuePRD("a", PRDApproved, 90, now.Add(-45*time.Minute))
	if got := d.Scan([]*PRD{p}, cfg, now); len(got) != 1 {
		t.Fatalf("want 1 stall, got %d", len(got))
	}
	if len(sink.events) != 0 {
		t.Fatalf("wake off must not wake, got %d", len(sink.events))
	}
}

func TestLifecycleWakeSinkPayload(t *testing.T) {
	var got []LifecycleEvent
	sink := LifecycleWakeSink{Emit: func(e LifecycleEvent) { got = append(got, e) }}
	sink.Wake(PriorityStall{AutomatonID: "a", Priority: 90, WaitingMinutes: 45,
		BlockerCode: StallBlockerLaneFull, BlockerText: "every lane is busy", Action: "wait for a lane or free one"})
	if len(got) != 1 || got[0].Kind != KindPriorityStall || got[0].PRDID != "a" || got[0].NewStatus != StallBlockerLaneFull {
		t.Fatalf("bad event: %+v", got)
	}
	var st PriorityStall
	if err := json.Unmarshal([]byte(got[0].Details), &st); err != nil {
		t.Fatal(err)
	}
	if st.Priority != 90 || st.WaitingMinutes != 45 || st.BlockerText != "every lane is busy" || st.Action != "wait for a lane or free one" {
		t.Fatalf("bad payload: %+v", st)
	}
	if !IsKnownEventKind(KindPriorityStall) {
		t.Fatal("priority_stall must be a known kind")
	}
}

func TestPriorityStallFindingCarriesBlockerAndAction(t *testing.T) {
	now := time.Now()
	src := newFakeStallSource()
	src.full["a"] = true
	p := workTestQueuePRD("a", PRDApproved, 90, now.Add(-45*time.Minute))
	w := NewSupervisorWatchWork(&fakeSupervisorWatchPRDLister{prds: []*PRD{p}}, nil, nil)
	w.Stall = src
	got := w.CheckPriorityStall(now, SupervisorWatchConfig{})
	if len(got) != 1 {
		t.Fatalf("want 1 finding, got %d", len(got))
	}
	want := SupervisorWatchPriorityStallText("a", 90, 45, 30, 80, "every lane is busy", "wait for a lane or free one")
	if got[0].Text != want || !strings.Contains(want, "every lane is busy") || !strings.Contains(want, "wait for a lane or free one") {
		t.Fatalf("text = %q, want %q", got[0].Text, want)
	}
}
