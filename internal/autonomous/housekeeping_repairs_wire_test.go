package autonomous

import (
	"context"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

type wireLaneOps struct{ started int }

func (*wireLaneOps) Lanes(context.Context) []LaneFillLaneView {
	return []LaneFillLaneView{{Name: "gpu", Idle: true, Candidates: []LaneFillCandidateView{{ID: "a1", Eligible: true}}}}
}
func (*wireLaneOps) CapacityFull(context.Context) bool { return false }
func (*wireLaneOps) UsagePaused(context.Context) bool  { return false }
func (o *wireLaneOps) Start(context.Context, string) error {
	o.started++
	return nil
}

// TestSupervisorLoopPass_RunsHousekeepingRepairer proves the idle tick's
// supervisor loop pass also runs the registered Repairer.
func TestSupervisorLoopPass_RunsHousekeepingRepairer(t *testing.T) {
	h, steps, _ := newStepsRig(t, SupervisorLoopHooks{})
	h.c.SetSupervisorLoopSteps(steps)
	ops := &wireLaneOps{}
	zero := 0
	set := config.HousekeepingRepairsConfig{LaneFill: config.HousekeepingModeOn, GraceSeconds: &zero}
	rep := NewHousekeepingRepairer(HousekeepingWiring{
		Settings: func() config.HousekeepingRepairsConfig { return set }, LaneFill: ops,
	})
	h.c.SetHousekeepingRepairer(rep)
	h.c.supervisorLoopPass(context.Background())
	if ops.started != 1 {
		t.Fatalf("the idle tick pass started %d Automata, want 1", ops.started)
	}
	// A nil Coordinator or no Repairer is a no-op.
	var nilC *Coordinator
	nilC.SetHousekeepingRepairer(rep)
	if nilC.HousekeepingPass(context.Background()) != nil {
		t.Fatal("nil Coordinator pass returned records")
	}
}
