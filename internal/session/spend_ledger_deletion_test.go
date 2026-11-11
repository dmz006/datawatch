package session

import (
	"sync"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/spendledger"
)

// retainingLedger is a fake ledger that keeps the latest entry per
// session and role, like the real one, and has no remove method.
type retainingLedger struct {
	mu sync.Mutex
	m  map[string]spendledger.Entry
}

func (r *retainingLedger) Upsert(e spendledger.Entry) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.m == nil {
		r.m = map[string]spendledger.Entry{}
	}
	r.m[e.SessionID+"|"+e.Role] = e
	return nil
}

func (r *retainingLedger) get(id, role string) (spendledger.Entry, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	e, ok := r.m[id+"|"+role]
	return e, ok
}

func seedAutomatonSessions(t *testing.T, mgr *Manager) []string {
	t.Helper()
	roles := []string{spendledger.RoleWorker, spendledger.RoleVerification, spendledger.RoleGuardrail}
	var ids []string
	for i, role := range roles {
		id := "dl0" + string(rune('1'+i))
		full := "testhost-" + id
		if err := mgr.SaveSession(&Session{
			ID: id, FullID: full, Hostname: "testhost", BackendFamily: "claude-code",
			State: StateRunning, AutomatonID: "auto9", TaskID: "task9", SpendRole: role,
			CreatedAt: time.Now(), UpdatedAt: time.Now(),
		}); err != nil {
			t.Fatal(err)
		}
		if err := mgr.AddUsage(full, 10*(i+1), 5, CostRate{}); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, full)
	}
	return ids
}

func assertAttributionSurvives(t *testing.T, rl *retainingLedger, ids []string) {
	t.Helper()
	roles := []string{spendledger.RoleWorker, spendledger.RoleVerification, spendledger.RoleGuardrail}
	for i, id := range ids {
		e, ok := rl.get(id, roles[i])
		if !ok {
			t.Fatalf("ledger entry for %s missing", id)
		}
		if e.AutomatonID != "auto9" || e.TaskID != "task9" || e.Role != roles[i] || e.TokensIn != int64(10*(i+1)) {
			t.Errorf("attribution lost for %s: %+v", id, e)
		}
	}
}

func TestSpendLedger_DeletionSurvivesSessionDelete(t *testing.T) {
	for _, deleteData := range []bool{false, true} {
		mgr, _ := newLedgerManager(t)
		rl := &retainingLedger{}
		mgr.SetSpendLedger(rl)
		ids := seedAutomatonSessions(t, mgr)
		for _, id := range ids {
			if err := mgr.Delete(id, deleteData); err != nil {
				t.Fatalf("Delete(%s, %v): %v", id, deleteData, err)
			}
			if _, ok := mgr.store.Get(id); ok {
				t.Fatalf("session %s still in store", id)
			}
		}
		assertAttributionSurvives(t, rl, ids)
		// Ended-on-delete entries are finalized, not removed.
		for i, id := range ids {
			roles := []string{spendledger.RoleWorker, spendledger.RoleVerification, spendledger.RoleGuardrail}
			if e, _ := rl.get(id, roles[i]); e.EndedAt.IsZero() {
				t.Errorf("deleted-while-running %s has no EndedAt", id)
			}
		}
	}
}

func TestSpendLedger_DeletionSurvivesCleanupPaths(t *testing.T) {
	mgr, _ := newLedgerManager(t)
	rl := &retainingLedger{}
	mgr.SetSpendLedger(rl)
	ids := seedAutomatonSessions(t, mgr)
	for _, id := range ids {
		if err := mgr.EndSession(id, EndReason{Caller: "test", Reason: "done"}); err != nil {
			t.Fatal(err)
		}
	}
	_ = mgr.ReapFinishedOneShots()
	_ = mgr.PruneSessionFolders(time.Now().Add(365 * 24 * time.Hour))
	_, _ = mgr.ReapOrphanWorkspaces()
	for _, id := range ids {
		if err := mgr.Delete(id, true); err != nil {
			t.Fatal(err)
		}
	}
	_ = mgr.PruneSessionFolders(time.Now().Add(365 * 24 * time.Hour))
	_, _ = mgr.ReapOrphanWorkspaces()
	assertAttributionSurvives(t, rl, ids)
}
