package autonomous

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/config"
)

type fakeRepair struct {
	group    string
	findings []RepairFinding
	applied  []string
	err      error
}

func (f *fakeRepair) Group() string                          { return f.group }
func (f *fakeRepair) Detect(context.Context) []RepairFinding { return f.findings }
func (f *fakeRepair) Apply(_ context.Context, fi RepairFinding) (string, error) {
	f.applied = append(f.applied, fi.Subject)
	return "retried " + fi.Subject, f.err
}
func (f *fakeRepair) UndoHint(RepairFinding) string { return "reset it by hand" }

type repairHarness struct {
	r      *Repairer
	logs   []string
	alerts []string
	cfg    config.HousekeepingRepairsConfig
	now    time.Time
}

func newHarness(t *testing.T, mode string, reps ...Repair) *repairHarness {
	t.Helper()
	zero := 0
	h := &repairHarness{now: time.Unix(1000, 0), cfg: config.HousekeepingRepairsConfig{
		TaskChain: mode, AdvanceJudge: mode, LaneFill: mode, Leftovers: mode, GraceSeconds: &zero}}
	h.r = NewRepairer(RepairerHooks{
		Settings: func() config.HousekeepingRepairsConfig { return h.cfg },
		Log:      func(l string) { h.logs = append(h.logs, l) },
		Alert:    func(_, title, _ string) { h.alerts = append(h.alerts, title) },
		Now:      func() time.Time { return h.now },
	}, reps...)
	return h
}

func TestRepairModeOffDoesNothing(t *testing.T) {
	f := &fakeRepair{group: RepairGroupTaskChain, findings: []RepairFinding{{Subject: "a", Summary: "stuck"}}}
	h := newHarness(t, "off", f)
	if got := h.r.Run(context.Background()); len(got) != 0 {
		t.Fatalf("off recorded %v", got)
	}
	if len(f.applied) != 0 || len(h.logs) != 0 || len(h.r.Records()) != 0 {
		t.Fatal("off acted")
	}
}

func TestRepairDefaultsToReport(t *testing.T) {
	f := &fakeRepair{group: RepairGroupLaneFill, findings: []RepairFinding{{Subject: "lanes", Summary: "x"}}}
	h := newHarness(t, "", f)
	got := h.r.Run(context.Background())
	if len(got) != 1 || got[0].Kind != RepairWould || len(f.applied) != 0 {
		t.Fatalf("default not report: %+v", got)
	}
}

func TestRepairReportRecordsOnly(t *testing.T) {
	f := &fakeRepair{group: RepairGroupAdvanceJudge, findings: []RepairFinding{{Subject: "t1", Summary: "held"}}}
	h := newHarness(t, "report", f)
	got := h.r.Run(context.Background())
	if len(got) != 1 || got[0].Kind != RepairWould {
		t.Fatalf("got %+v", got)
	}
	if len(f.applied) != 0 {
		t.Fatal("report applied")
	}
	if len(h.logs) != 1 {
		t.Fatalf("logs %v", h.logs)
	}
}

func TestRepairOnApplies(t *testing.T) {
	f := &fakeRepair{group: RepairGroupLeftovers, findings: []RepairFinding{{Subject: "disk", Summary: "full"}}}
	h := newHarness(t, "on", f)
	got := h.r.Run(context.Background())
	if len(got) != 1 || got[0].Kind != RepairDid || got[0].Done != "retried disk" {
		t.Fatalf("got %+v", got)
	}
	if len(f.applied) != 1 {
		t.Fatal("not applied")
	}
}

func TestRepairApplyErrorRecorded(t *testing.T) {
	f := &fakeRepair{group: RepairGroupTaskChain, findings: []RepairFinding{{Subject: "a", Summary: "s"}}, err: errors.New("boom")}
	h := newHarness(t, "on", f)
	got := h.r.Run(context.Background())
	if got[0].Kind != RepairFailed || !strings.Contains(got[0].Done, "boom") {
		t.Fatalf("got %+v", got)
	}
}

func TestRepairUnsafeReportedNotRepairedAlertsOnce(t *testing.T) {
	f := &fakeRepair{group: RepairGroupTaskChain, findings: []RepairFinding{{Subject: "a", Summary: "s", Unsafe: true, UnsafeReason: "has uncommitted work"}}}
	h := newHarness(t, "on", f)
	for i := 0; i < 3; i++ {
		h.r.Run(context.Background())
	}
	if len(f.applied) != 0 {
		t.Fatal("unsafe repaired")
	}
	if len(h.alerts) != 1 {
		t.Fatalf("want one alert, got %v", h.alerts)
	}
	if len(h.logs) != 3 {
		t.Fatalf("every pass logs: %v", h.logs)
	}
	// Once the subject clears and returns, it alerts again.
	f.findings = nil
	h.r.Run(context.Background())
	f.findings = []RepairFinding{{Subject: "a", Summary: "s", Unsafe: true, UnsafeReason: "r"}}
	h.r.Run(context.Background())
	if len(h.alerts) != 2 {
		t.Fatalf("want re-alert after clear, got %v", h.alerts)
	}
}

func TestRepairUnsafeAlertsPerSubject(t *testing.T) {
	f := &fakeRepair{group: RepairGroupTaskChain, findings: []RepairFinding{
		{Subject: "a", Summary: "s", Unsafe: true, UnsafeReason: "r"},
		{Subject: "b", Summary: "s", Unsafe: true, UnsafeReason: "r"}}}
	h := newHarness(t, "report", f)
	h.r.Run(context.Background())
	h.r.Run(context.Background())
	if len(h.alerts) != 2 {
		t.Fatalf("alerts %v", h.alerts)
	}
}

func TestRepairDecisionLogLineContent(t *testing.T) {
	f := &fakeRepair{group: RepairGroupTaskChain, findings: []RepairFinding{{Subject: "automation x", Summary: "next step waiting"}}}
	h := newHarness(t, "on", f)
	h.r.Run(context.Background())
	line := h.logs[0]
	for _, want := range []string{"task_chain", "automation x", "found: next step waiting", "done: retried automation x", "undo: reset it by hand"} {
		if !strings.Contains(line, want) {
			t.Fatalf("line %q lacks %q", line, want)
		}
	}
}

func TestRepairGraceDelaysAction(t *testing.T) {
	f := &fakeRepair{group: RepairGroupTaskChain, findings: []RepairFinding{{Subject: "a", Summary: "s"}}}
	h := newHarness(t, "on", f)
	g := 60
	h.cfg.GraceSeconds = &g
	if got := h.r.Run(context.Background()); len(got) != 0 {
		t.Fatal("acted inside grace")
	}
	h.now = h.now.Add(61 * time.Second)
	if got := h.r.Run(context.Background()); len(got) != 1 || got[0].Kind != RepairDid {
		t.Fatalf("after grace %+v", got)
	}
}

func TestRepairRecordBoundedAndPersisted(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "repairs.json")
	f := &fakeRepair{group: RepairGroupTaskChain}
	for i := 0; i < housekeepingRecordCap+20; i++ {
		f.findings = append(f.findings, RepairFinding{Subject: string(rune('a'+i%26)) + string(rune('A'+i/26)), Summary: "s"})
	}
	zero := 0
	cfg := config.HousekeepingRepairsConfig{GraceSeconds: &zero}
	r := NewRepairer(RepairerHooks{Settings: func() config.HousekeepingRepairsConfig { return cfg }, StorePath: path}, f)
	r.Run(context.Background())
	if n := len(r.Records()); n != housekeepingRecordCap {
		t.Fatalf("records %d", n)
	}
	r2 := NewRepairer(RepairerHooks{StorePath: path})
	if n := len(r2.Records()); n != housekeepingRecordCap {
		t.Fatalf("reloaded %d", n)
	}
}

func TestRepairNilSafeAndNoHooks(t *testing.T) {
	var r *Repairer
	if r.Run(context.Background()) != nil || r.Records() != nil {
		t.Fatal("nil repairer")
	}
	f := &fakeRepair{group: RepairGroupTaskChain, findings: []RepairFinding{{Subject: "a", Summary: "s", Unsafe: true, UnsafeReason: "r"}}}
	zero := 0
	_ = zero
	r2 := NewRepairer(RepairerHooks{}, f)
	r2.hooks.Settings = nil
	// default grace is 300s, so nothing is recorded on the first pass.
	if got := r2.Run(context.Background()); len(got) != 0 {
		t.Fatalf("default grace ignored: %v", got)
	}
}

func TestNewSupervisorStepRepairsNil(t *testing.T) {
	if NewSupervisorStepRepairs(nil) != nil {
		t.Fatal("nil steps")
	}
}
