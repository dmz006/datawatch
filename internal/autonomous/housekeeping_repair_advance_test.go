package autonomous

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/config"
)

type fakeAdvanceOps struct {
	autos      []AdvanceAutomaton
	advanced   []string
	repaired   []string
	findingsIn [][]string
	advErr     error
	repErr     error
}

func (f *fakeAdvanceOps) Automata(context.Context) []AdvanceAutomaton { return f.autos }
func (f *fakeAdvanceOps) Advance(_ context.Context, id string) error {
	f.advanced = append(f.advanced, id)
	return f.advErr
}
func (f *fakeAdvanceOps) RepairJudge(_ context.Context, id string, fs []string) error {
	f.repaired = append(f.repaired, id)
	f.findingsIn = append(f.findingsIn, fs)
	return f.repErr
}

type advHarness struct {
	r      *Repairer
	ops    *fakeAdvanceOps
	logs   []string
	alerts []string
}

func newAdvHarness(mode string, ops *fakeAdvanceOps) *advHarness {
	zero := 0
	h := &advHarness{ops: ops}
	cfg := config.HousekeepingRepairsConfig{AdvanceJudge: mode, GraceSeconds: &zero}
	h.r = NewRepairer(RepairerHooks{
		Settings: func() config.HousekeepingRepairsConfig { return cfg },
		Log:      func(l string) { h.logs = append(h.logs, l) },
		Alert:    func(_, title, _ string) { h.alerts = append(h.alerts, title) },
		Now:      func() time.Time { return time.Unix(1000, 0) },
	}, NewAdvanceJudgeRepairs(ops)...)
	return h
}

func runningDone() AdvanceAutomaton {
	return AdvanceAutomaton{ID: "a1", Title: "Thing", Running: true, TaskStatuses: []TaskStatus{TaskCompleted, TaskFailed}}
}

func blocked(findings ...string) AdvanceAutomaton {
	return AdvanceAutomaton{ID: "b1", Title: "Blocked", JudgeBlocked: true,
		TaskStatuses: []TaskStatus{TaskCompleted}, Findings: findings}
}

func TestNewAdvanceJudgeRepairsNil(t *testing.T) {
	if NewAdvanceJudgeRepairs(nil) != nil {
		t.Fatal("nil ops should give none")
	}
	if got := NewAdvanceJudgeRepairs(&fakeAdvanceOps{}); len(got) != 3 {
		t.Fatalf("want 3, got %d", len(got))
	}
}

func TestAdvanceRunningAllTerminal(t *testing.T) {
	ops := &fakeAdvanceOps{autos: []AdvanceAutomaton{runningDone()}}
	h := newAdvHarness(config.HousekeepingModeOn, ops)
	recs := h.r.Run(context.Background())
	if len(ops.advanced) != 1 || ops.advanced[0] != "a1" {
		t.Fatalf("advanced = %v", ops.advanced)
	}
	if len(recs) != 1 || recs[0].Kind != RepairDid || len(h.logs) != 1 {
		t.Fatalf("recs=%v logs=%v", recs, h.logs)
	}
	if !strings.Contains(h.logs[0], "found:") || !strings.Contains(h.logs[0], "undo:") {
		t.Fatalf("log line lacks found/undo: %s", h.logs[0])
	}
}

func TestAdvanceReportModeActsOnNothing(t *testing.T) {
	ops := &fakeAdvanceOps{autos: []AdvanceAutomaton{runningDone(), blocked("x")}}
	h := newAdvHarness(config.HousekeepingModeReport, ops)
	recs := h.r.Run(context.Background())
	if len(ops.advanced)+len(ops.repaired) != 0 {
		t.Fatal("report mode acted")
	}
	for _, r := range recs {
		if r.Kind != RepairWould {
			t.Fatalf("kind = %s", r.Kind)
		}
	}
	if len(recs) != 2 {
		t.Fatalf("want 2 records, got %d", len(recs))
	}
}

func TestAdvanceOffDoesNothing(t *testing.T) {
	ops := &fakeAdvanceOps{autos: []AdvanceAutomaton{runningDone()}}
	h := newAdvHarness(config.HousekeepingModeOff, ops)
	if recs := h.r.Run(context.Background()); len(recs) != 0 || len(ops.advanced) != 0 {
		t.Fatal("off mode acted")
	}
}

func TestAdvanceRefusesNonTerminalTask(t *testing.T) {
	a := runningDone()
	a.TaskStatuses = append(a.TaskStatuses, TaskInProgress)
	ops := &fakeAdvanceOps{autos: []AdvanceAutomaton{a}}
	h := newAdvHarness(config.HousekeepingModeOn, ops)
	recs := h.r.Run(context.Background())
	h.r.Run(context.Background())
	if len(ops.advanced) != 0 || len(recs) != 1 || recs[0].Kind != RepairSkippedUnsafe {
		t.Fatalf("advanced=%v recs=%v", ops.advanced, recs)
	}
	if len(h.alerts) != 1 {
		t.Fatalf("want exactly one alert across two passes, got %d", len(h.alerts))
	}
}

func TestAdvanceSkipsLiveExecutor(t *testing.T) {
	a := runningDone()
	a.LiveExecutor = true
	ops := &fakeAdvanceOps{autos: []AdvanceAutomaton{a}}
	h := newAdvHarness(config.HousekeepingModeOn, ops)
	if recs := h.r.Run(context.Background()); len(recs) != 0 {
		t.Fatalf("recs=%v", recs)
	}
}

func TestAdvanceErrorIsRecordedFailed(t *testing.T) {
	ops := &fakeAdvanceOps{autos: []AdvanceAutomaton{runningDone()}, advErr: errors.New("boom")}
	h := newAdvHarness(config.HousekeepingModeOn, ops)
	recs := h.r.Run(context.Background())
	if len(recs) != 1 || recs[0].Kind != RepairFailed {
		t.Fatalf("recs=%v", recs)
	}
}

func TestJudgeRepairMadeOnceWithFindings(t *testing.T) {
	ops := &fakeAdvanceOps{autos: []AdvanceAutomaton{blocked("missing test", "doc stale")}}
	h := newAdvHarness(config.HousekeepingModeOn, ops)
	h.r.Run(context.Background())
	if len(ops.repaired) != 1 || len(ops.findingsIn[0]) != 2 || ops.findingsIn[0][0] != "missing test" {
		t.Fatalf("repaired=%v findings=%v", ops.repaired, ops.findingsIn)
	}
	// Second pass: recorded in the repair itself, so refused and alerted once.
	recs := h.r.Run(context.Background())
	h.r.Run(context.Background())
	if len(ops.repaired) != 1 {
		t.Fatalf("repair repeated: %v", ops.repaired)
	}
	if len(recs) != 1 || recs[0].Kind != RepairSkippedUnsafe || len(h.alerts) != 1 {
		t.Fatalf("recs=%v alerts=%v", recs, h.alerts)
	}
}

func TestJudgeRepairRefusedWhenAlreadyRecorded(t *testing.T) {
	a := blocked("x")
	a.RepairMade = true
	ops := &fakeAdvanceOps{autos: []AdvanceAutomaton{a}}
	h := newAdvHarness(config.HousekeepingModeOn, ops)
	recs := h.r.Run(context.Background())
	if len(ops.repaired) != 0 || len(recs) != 1 || recs[0].Kind != RepairSkippedUnsafe || len(h.alerts) != 1 {
		t.Fatalf("repaired=%v recs=%v alerts=%v", ops.repaired, recs, h.alerts)
	}
}

func TestJudgeRepairRefusedOnEmptyFindings(t *testing.T) {
	for _, fs := range [][]string{nil, {"", "  "}} {
		ops := &fakeAdvanceOps{autos: []AdvanceAutomaton{blocked(fs...)}}
		h := newAdvHarness(config.HousekeepingModeOn, ops)
		recs := h.r.Run(context.Background())
		if len(ops.repaired) != 0 || len(recs) != 1 || recs[0].Kind != RepairSkippedUnsafe || len(h.alerts) != 1 {
			t.Fatalf("findings %q: repaired=%v recs=%v", fs, ops.repaired, recs)
		}
	}
}

func TestJudgeRepairRefusedOnNonTerminalTask(t *testing.T) {
	a := blocked("x")
	a.TaskStatuses = append(a.TaskStatuses, TaskQueued)
	ops := &fakeAdvanceOps{autos: []AdvanceAutomaton{a}}
	h := newAdvHarness(config.HousekeepingModeOn, ops)
	recs := h.r.Run(context.Background())
	if len(ops.repaired) != 0 || len(recs) != 1 || recs[0].Kind != RepairSkippedUnsafe {
		t.Fatalf("repaired=%v recs=%v", ops.repaired, recs)
	}
}

func TestJudgeExistingRepairTaskLeftAlone(t *testing.T) {
	a := blocked("x")
	a.HasRepairTask = true
	ops := &fakeAdvanceOps{autos: []AdvanceAutomaton{a}}
	h := newAdvHarness(config.HousekeepingModeOn, ops)
	if recs := h.r.Run(context.Background()); len(recs) != 0 || len(ops.repaired) != 0 {
		t.Fatalf("recs=%v", recs)
	}
}

func TestMergeStatesReportedNeverMerged(t *testing.T) {
	wait := AdvanceAutomaton{ID: "w", Title: "W", MergeState: MergeStateWait, Running: true, TaskStatuses: []TaskStatus{TaskCompleted}}
	ready := AdvanceAutomaton{ID: "r", Title: "R", MergeState: MergeStateReady, Running: true, TaskStatuses: []TaskStatus{TaskCompleted}}
	ops := &fakeAdvanceOps{autos: []AdvanceAutomaton{wait, ready}}
	h := newAdvHarness(config.HousekeepingModeOn, ops)
	recs := h.r.Run(context.Background())
	if len(recs) != 2 {
		t.Fatalf("want 2 records, got %v", recs)
	}
	for _, r := range recs {
		if !strings.Contains(r.Done, "reported only") {
			t.Fatalf("done = %q", r.Done)
		}
	}
	if len(ops.advanced) != 0 || len(ops.repaired) != 0 {
		t.Fatal("a merge-state Automaton was acted on")
	}
}
