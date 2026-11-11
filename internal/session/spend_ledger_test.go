package session

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/spendledger"
)

type fakeLedger struct {
	mu    sync.Mutex
	calls []spendledger.Entry
}

func (f *fakeLedger) Upsert(e spendledger.Entry) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, e)
	return nil
}

func (f *fakeLedger) last(t *testing.T) spendledger.Entry {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.calls) == 0 {
		t.Fatal("no ledger upserts")
	}
	return f.calls[len(f.calls)-1]
}

func newLedgerManager(t *testing.T) (*Manager, *fakeLedger) {
	t.Helper()
	mgr, err := NewManager("testhost", t.TempDir(), "/bin/echo", 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = mgr.Close() })
	allowTestTemp(mgr)
	mgr.WithFakeTmux()
	fl := &fakeLedger{}
	mgr.SetSpendLedger(fl)
	return mgr, fl
}

func TestSpendLedger_StartStampsEntry(t *testing.T) {
	mgr, fl := newLedgerManager(t)
	sess, err := mgr.Start(context.Background(), "task", "", t.TempDir(), &StartOptions{
		AutomatonID: "auto1", TaskID: "task1", SpendRole: spendledger.RoleWorker,
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	e := fl.last(t)
	if e.SessionID != sess.FullID || e.AutomatonID != "auto1" || e.TaskID != "task1" || e.Role != spendledger.RoleWorker {
		t.Errorf("entry not stamped: %+v", e)
	}
	if !e.EndedAt.IsZero() {
		t.Errorf("running session has EndedAt: %+v", e)
	}
}

func TestSpendLedger_UsageRefreshesAndEndFinalizes(t *testing.T) {
	mgr, fl := newLedgerManager(t)
	_ = mgr.SaveSession(&Session{
		ID: "ll01", FullID: "testhost-ll01", Hostname: "testhost",
		BackendFamily: "claude-code", State: StateRunning,
		AutomatonID: "a", TaskID: "t", SpendRole: spendledger.RoleVerification,
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	})
	if err := mgr.AddUsage("testhost-ll01", 100, 50, CostRate{}); err != nil {
		t.Fatal(err)
	}
	e := fl.last(t)
	if e.TokensIn != 100 || e.TokensOut != 50 || e.AutomatonID != "a" || e.TaskID != "t" || e.Role != spendledger.RoleVerification {
		t.Errorf("usage not refreshed: %+v", e)
	}
	if err := mgr.EndSession("testhost-ll01", EndReason{Caller: "test", Reason: "done"}); err != nil {
		t.Fatal(err)
	}
	e = fl.last(t)
	if e.EndedAt.IsZero() || e.TokensIn != 100 {
		t.Errorf("end not finalized: %+v", e)
	}
}

func TestSpendLedger_DeleteLeavesEntries(t *testing.T) {
	mgr, fl := newLedgerManager(t)
	_ = mgr.SaveSession(&Session{
		ID: "ll02", FullID: "testhost-ll02", Hostname: "testhost",
		State: StateComplete, CreatedAt: time.Now(), UpdatedAt: time.Now(),
	})
	before := len(fl.calls)
	if err := mgr.Delete("testhost-ll02", false); err != nil {
		t.Fatal(err)
	}
	if len(fl.calls) != before {
		t.Errorf("delete touched ledger")
	}
}
