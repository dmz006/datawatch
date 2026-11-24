package autonomous

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/capacity"
)

type countingAdvisor struct{ plans int }

func (a *countingAdvisor) Plan(_, _, _ string) PlacementPlan { a.plans++; return PlacementPlan{} }
func (a *countingAdvisor) AskTimeout() time.Duration         { return 0 }

func cancelledTask(t *testing.T, m *Manager) (*PRD, *Task) {
	t.Helper()
	prd := newPRDWithTasks(t, m, 1)
	got, _ := m.Store().GetPRD(prd.ID)
	tk := &got.Story[0].Tasks[0]
	m.Store().MutateTask(tk.ID, func(c *Task) { c.Status = TaskCancelled })
	return got, tk
}

// A cancelled task never takes a lease, even on a pool with room: the ledger
// only checks the cancel flag while a request waits, so a free pool granted
// the cancelled worker at once and it went on to spawn.
func TestAdmit_CancelledTaskTakesNoLeaseOnFreePool(t *testing.T) {
	m, _, led := capFixture(t, 3)
	prd, tk := cancelledTask(t, m)
	err := m.admit(context.Background(), prd, tk, "opencode", "", capacity.RoleWorker)
	if !errors.Is(err, errCancelledWhileWaiting) {
		t.Fatalf("admit err = %v, want errCancelledWhileWaiting", err)
	}
	if led.Held(tk.ID) {
		t.Fatal("a cancelled task must not hold a lease")
	}
}

// A cancelled task never reaches the placement gate, so it cannot ask the
// planner, load a model on a node or hold the operator in a question.
func TestAdmit_CancelledTaskSkipsPlacement(t *testing.T) {
	m, _, led := capFixture(t, 3)
	adv := &countingAdvisor{}
	m.SetCapacity(led, m.CapacityKeys(), adv)
	prd, tk := cancelledTask(t, m)
	if err := m.admit(context.Background(), prd, tk, "opencode", "", capacity.RoleWorker); !errors.Is(err, errCancelledWhileWaiting) {
		t.Fatalf("admit err = %v, want errCancelledWhileWaiting", err)
	}
	if adv.plans != 0 {
		t.Fatalf("placement planner asked %d times for a cancelled task, want 0", adv.plans)
	}
}
