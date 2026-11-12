package autonomous

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

type fakeLeftoverOps struct {
	views      []LeftoverView
	released   []string
	releaseErr error
}

func (f *fakeLeftoverOps) Leftovers(context.Context) []LeftoverView { return f.views }
func (f *fakeLeftoverOps) Release(_ context.Context, kind, ref string) error {
	f.released = append(f.released, kind+":"+ref)
	return f.releaseErr
}

type leftoverHarness struct {
	r      *Repairer
	logs   []string
	alerts []string
}

func newLeftoverHarness(mode string, ops LeftoverReleaseOps) *leftoverHarness {
	h := &leftoverHarness{}
	grace := 0
	cfg := config.HousekeepingRepairsConfig{Leftovers: mode, GraceSeconds: &grace}
	h.r = NewRepairer(RepairerHooks{
		Settings: func() config.HousekeepingRepairsConfig { return cfg },
		Log:      func(l string) { h.logs = append(h.logs, l) },
		Alert:    func(_, title, _ string) { h.alerts = append(h.alerts, title) },
	}, NewLeftoverReleaseRepairs(ops)...)
	return h
}

func lv(kind, ref string) LeftoverView {
	return LeftoverView{Kind: kind, Ref: ref, Automaton: "Demo", Finished: true}
}

func TestLeftoverReleaseNil(t *testing.T) {
	if NewLeftoverReleaseRepairs(nil) != nil {
		t.Fatal("nil ops should give none")
	}
	if got := NewLeftoverReleaseRepairs(&fakeLeftoverOps{}); len(got) != 3 {
		t.Fatalf("want 3, got %d", len(got))
	}
}

func TestLeftoverReleasesEachKind(t *testing.T) {
	for _, kind := range []string{LeftoverLease, LeftoverWorktree, LeftoverTmuxName} {
		ops := &fakeLeftoverOps{views: []LeftoverView{lv(kind, "x1")}}
		h := newLeftoverHarness(config.HousekeepingModeOn, ops)
		recs := h.r.Run(context.Background())
		if len(ops.released) != 1 || ops.released[0] != kind+":x1" {
			t.Fatalf("%s: released = %v", kind, ops.released)
		}
		if len(recs) != 1 || recs[0].Kind != RepairDid || len(h.logs) != 1 || len(h.alerts) != 0 {
			t.Fatalf("%s: recs=%v logs=%v alerts=%v", kind, recs, h.logs, h.alerts)
		}
		if !strings.Contains(h.logs[0], "undo:") || !strings.Contains(recs[0].Done, "no session was touched") {
			t.Fatalf("%s: log line = %v", kind, h.logs)
		}
	}
}

func TestLeftoverRefusals(t *testing.T) {
	cases := map[string]func(*LeftoverView){
		"uncommitted work": func(v *LeftoverView) { v.UncommittedWork = true },
		"unlanded commits": func(v *LeftoverView) { v.UnlandedCommits = true },
		"live session":     func(v *LeftoverView) { v.LiveSession = true },
		"check unanswered": func(v *LeftoverView) { v.CheckError = "git failed" },
	}
	for name, mut := range cases {
		for _, kind := range []string{LeftoverLease, LeftoverWorktree, LeftoverTmuxName} {
			v := lv(kind, "x1")
			mut(&v)
			ops := &fakeLeftoverOps{views: []LeftoverView{v}}
			h := newLeftoverHarness(config.HousekeepingModeOn, ops)
			recs := h.r.Run(context.Background())
			h.r.Run(context.Background()) // second pass must not alert again
			if len(ops.released) != 0 {
				t.Fatalf("%s/%s: released %v", name, kind, ops.released)
			}
			if len(recs) != 1 || recs[0].Kind != RepairSkippedUnsafe || len(h.alerts) != 1 {
				t.Fatalf("%s/%s: recs=%v alerts=%v", name, kind, recs, h.alerts)
			}
			if !strings.Contains(h.logs[0], "undo:") {
				t.Fatalf("%s/%s: log = %v", name, kind, h.logs)
			}
		}
	}
}

func TestLeftoverUnfinishedAutomatonIgnored(t *testing.T) {
	v := lv(LeftoverWorktree, "/w/a")
	v.Finished = false
	ops := &fakeLeftoverOps{views: []LeftoverView{v}}
	h := newLeftoverHarness(config.HousekeepingModeOn, ops)
	if recs := h.r.Run(context.Background()); len(recs) != 0 || len(ops.released) != 0 {
		t.Fatalf("recs=%v released=%v", recs, ops.released)
	}
}

func TestLeftoverReportModeActsOnNothing(t *testing.T) {
	ops := &fakeLeftoverOps{views: []LeftoverView{lv(LeftoverLease, "l1")}}
	h := newLeftoverHarness(config.HousekeepingModeReport, ops)
	recs := h.r.Run(context.Background())
	if len(ops.released) != 0 || len(recs) != 1 || recs[0].Kind != RepairWould {
		t.Fatalf("released=%v recs=%v", ops.released, recs)
	}
}

func TestLeftoverOffModeSkips(t *testing.T) {
	ops := &fakeLeftoverOps{views: []LeftoverView{lv(LeftoverLease, "l1")}}
	h := newLeftoverHarness(config.HousekeepingModeOff, ops)
	if recs := h.r.Run(context.Background()); len(recs) != 0 || len(ops.released) != 0 {
		t.Fatalf("recs=%v released=%v", recs, ops.released)
	}
}

func TestLeftoverReleaseErrorRecorded(t *testing.T) {
	ops := &fakeLeftoverOps{views: []LeftoverView{lv(LeftoverTmuxName, "t1")}, releaseErr: errors.New("boom")}
	h := newLeftoverHarness(config.HousekeepingModeOn, ops)
	recs := h.r.Run(context.Background())
	if len(recs) != 1 || recs[0].Kind != RepairFailed || !strings.Contains(recs[0].Done, "boom") {
		t.Fatalf("recs=%v", recs)
	}
}

// A leftover that turns unsafe between detect and apply is refused on the
// recheck inside Apply.
func TestLeftoverApplyRechecksSafety(t *testing.T) {
	ops := &fakeLeftoverOps{views: []LeftoverView{lv(LeftoverWorktree, "/w/a")}}
	rep := NewLeftoverReleaseRepairs(ops)[1]
	f := rep.Detect(context.Background())[0]
	ops.views[0].LiveSession = true
	if _, err := rep.Apply(context.Background(), f); err == nil || len(ops.released) != 0 {
		t.Fatalf("err=%v released=%v", err, ops.released)
	}
	ops.views = nil
	if _, err := rep.Apply(context.Background(), f); err == nil {
		t.Fatal("want error for a gone leftover")
	}
}
