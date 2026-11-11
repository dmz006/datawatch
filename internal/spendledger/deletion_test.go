package spendledger

import (
	"testing"
	"time"
)

// TestReportDeletionSurvivesSessionRemoval models the session-delete path:
// the session manager only ever upserts, so entries for deleted sessions
// must stay and keep feeding a per-automaton report after a reopen.
func TestReportDeletionSurvivesSessionRemoval(t *testing.T) {
	dir := t.TempDir()
	c := &fakeClock{time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)}
	l := newLedger(t, dir, c)
	start := c.t.Add(-time.Hour)
	for i, r := range []string{RoleWorker, RoleVerification, RoleGuardrail} {
		if err := l.Upsert(Entry{
			SessionID: "s" + r, AutomatonID: "auto9", TaskID: "task9", Role: r,
			StartedAt: start, EndedAt: start.Add(time.Minute), TokensIn: int64(10 * (i + 1)),
		}); err != nil {
			t.Fatal(err)
		}
	}
	// A prune at the same instant (the only removal the ledger has) keeps them.
	if err := l.Prune(c.t); err != nil {
		t.Fatal(err)
	}
	got := newLedger(t, dir, c).Entries()
	if len(got) != 3 {
		t.Fatalf("entries after prune+reopen: %+v", got)
	}
	total, roles := int64(0), map[string]bool{}
	for _, e := range got {
		if e.AutomatonID != "auto9" || e.TaskID != "task9" {
			t.Errorf("attribution lost: %+v", e)
		}
		total += e.TokensIn
		roles[e.Role] = true
	}
	if total != 60 || len(roles) != 3 {
		t.Errorf("report totals wrong: total=%d roles=%v", total, roles)
	}
}

func TestEntriesDeletionSurvivesReupsertOfEndedSession(t *testing.T) {
	c := &fakeClock{time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)}
	l := newLedger(t, t.TempDir(), c)
	e := Entry{SessionID: "s1", AutomatonID: "a", TaskID: "t", Role: RoleWorker, StartedAt: c.t, EndedAt: c.t}
	_ = l.Upsert(e)
	_ = l.Upsert(e)
	if got := l.Entries(); len(got) != 1 || got[0].AutomatonID != "a" || got[0].TaskID != "t" {
		t.Fatalf("got %+v", got)
	}
}
