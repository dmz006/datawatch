// A planner that fails after the operator closed its Automaton must not write
// anything over the closed record: no draft rollback, no failed status and no
// further planner.
package autonomous

import (
	"errors"
	"testing"
)

// pgcCancel closes the stored record the way a cancel does. With swap the store
// holds a different object afterwards, so the planner's own pointer is stale.
func pgcCancel(t *testing.T, m *Manager, id string, swap bool) {
	t.Helper()
	if swap {
		cur, ok := m.Store().SnapshotPRD(id)
		if !ok {
			t.Error("record gone")
			return
		}
		cur.Status = PRDCancelled
		if err := m.Store().SavePRD(cur); err != nil {
			t.Errorf("SavePRD: %v", err)
		}
		return
	}
	cur, _ := m.Store().GetPRD(id)
	m.Store().WithLock(func() { cur.Status = PRDCancelled })
	if err := m.Store().SavePRD(cur); err != nil {
		t.Errorf("SavePRD: %v", err)
	}
}

func TestPlanningGiveUpLeavesCancelledRecordCancelled(t *testing.T) {
	failures := map[string]struct {
		err    error
		budget int
	}{
		"planner error, no retry budget":   {errors.New("planner backend failed"), 0},
		"planner error, retry budget left": {errors.New("planner backend failed"), 3},
		"sessions ending without a plan":   {noOutputDecomposeErr(), 0},
	}
	for name, f := range failures {
		for _, swap := range []bool{false, true} {
			sub := name + "/shared record"
			if swap {
				sub = name + "/replaced record"
			}
			t.Run(sub, func(t *testing.T) {
				m, _ := newFailureEscalationFixture(t, t.TempDir())
				cfg := m.Config()
				cfg.RetryPolicy = map[string]int{"infra": f.budget}
				m.SetConfig(cfg)
				prd, err := m.CreatePRD("spec", t.TempDir(), "opencode", "", EffortNormal)
				if err != nil {
					t.Fatalf("CreatePRD: %v", err)
				}
				id := prd.ID
				calls := 0
				m.decompose = func(DecomposeRequest) (string, error) {
					calls++
					// The no-output family stops on its second session, so the
					// cancel lands during that one.
					if !isOneShotNoOutput(f.err) || calls == decomposeSessionStopThreshold {
						pgcCancel(t, m, id, swap)
					}
					if calls > 5 {
						return "", errDecomposeCancelled
					}
					return "", f.err
				}

				if _, err := m.Decompose(id); err == nil {
					t.Error("Decompose must still report the failure")
				}
				got, _ := m.Store().SnapshotPRD(id)
				if got.Status != PRDCancelled {
					t.Errorf("status = %s; the cancel must survive the give-up", got.Status)
				}
				want := 1
				if isOneShotNoOutput(f.err) {
					want = decomposeSessionStopThreshold
				}
				if calls != want {
					t.Errorf("planner ran %d times; want %d (none after the cancel)", calls, want)
				}
			})
		}
	}
}

// The shared give-up tail also leaves a record with a closed outcome alone.
func TestPlanningGiveUpLeavesClosedOutcomeRecordAlone(t *testing.T) {
	m, _ := newFailureEscalationFixture(t, t.TempDir())
	prd, err := m.CreatePRD("spec", t.TempDir(), "opencode", "", EffortNormal)
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	m.Store().WithLock(func() {
		prd.Status = PRDPlanning
		prd.ClosedOutcome = OutcomeDelivered
	})
	m.failDecomposeAttempt(prd, "decompose-llm", errors.New("planner backend failed"))
	got, _ := m.Store().SnapshotPRD(prd.ID)
	if got.Status != PRDPlanning || got.LastDecomposeFailure != nil {
		t.Errorf("status = %s failure = %+v; a closed record must not be written", got.Status, got.LastDecomposeFailure)
	}
}
