package autonomous

import (
	"context"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

type viewFakeRepair struct {
	group string
	finds []RepairFinding
}

func (f viewFakeRepair) Group() string                          { return f.group }
func (f viewFakeRepair) Detect(context.Context) []RepairFinding { return f.finds }
func (f viewFakeRepair) Apply(context.Context, RepairFinding) (string, error) {
	return "reset the task", nil
}
func (f viewFakeRepair) UndoHint(RepairFinding) string { return "set the task back" }

func TestHousekeepingRepairsView_NilReadsReport(t *testing.T) {
	var r *Repairer
	v := r.View()
	if len(v.Groups) != 4 {
		t.Fatalf("groups = %d, want 4", len(v.Groups))
	}
	for _, g := range v.Groups {
		if g.Mode != config.HousekeepingModeReport {
			t.Errorf("%s mode = %q, want report", g.Group, g.Mode)
		}
		if g.Would == nil || g.Did == nil || g.Failed == nil || g.SkippedUnsafe == nil {
			t.Errorf("%s lists must be empty, not nil", g.Group)
		}
	}
}

func TestHousekeepingRepairsView_SplitsWouldAndDid(t *testing.T) {
	zero := 0
	set := config.HousekeepingRepairsConfig{TaskChain: "on", LaneFill: "report", GraceSeconds: &zero}
	r := NewRepairer(RepairerHooks{Settings: func() config.HousekeepingRepairsConfig { return set }},
		viewFakeRepair{group: RepairGroupTaskChain, finds: []RepairFinding{{Subject: "t1", Summary: "stuck"}}},
		viewFakeRepair{group: RepairGroupLaneFill, finds: []RepairFinding{{Subject: "l1", Summary: "idle"}, {Subject: "l2", Summary: "odd", Unsafe: true, UnsafeReason: "needs a person"}}},
	)
	r.Run(context.Background())
	v := r.View()
	by := map[string]HousekeepingRepairGroupView{}
	for _, g := range v.Groups {
		by[g.Group] = g
	}
	if g := by[RepairGroupTaskChain]; g.Mode != "on" || len(g.Did) != 1 || len(g.Would) != 0 || g.Did[0].Undo == "" {
		t.Errorf("task_chain = %+v", g)
	}
	if g := by[RepairGroupLaneFill]; g.Mode != "report" || len(g.Would) != 1 || len(g.Did) != 0 || len(g.SkippedUnsafe) != 1 {
		t.Errorf("lane_fill = %+v", g)
	}
	var b strings.Builder
	RenderHousekeepingRepairs(&b, &v)
	for _, frag := range []string{"task_chain: mode on", "did 1", "would do 1", "skipped as unsafe 1", "undo: set the task back"} {
		if !strings.Contains(b.String(), frag) {
			t.Errorf("render lacks %q:\n%s", frag, b.String())
		}
	}
}

func TestManagerHousekeepingRepairsView(t *testing.T) {
	m := &Manager{}
	if got := m.HousekeepingRepairsView(); len(got.Groups) != 4 {
		t.Errorf("no repairer: groups = %d", len(got.Groups))
	}
	m.SetHousekeepingRepairer(NewRepairer(RepairerHooks{}))
	if got := m.HousekeepingRepairsView(); len(got.Groups) != 4 {
		t.Errorf("repairer: groups = %d", len(got.Groups))
	}
}
