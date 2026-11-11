package spendledger

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

type fakeClock struct{ t time.Time }

func (c *fakeClock) Now() time.Time { return c.t }

func newLedger(t *testing.T, dir string, c *fakeClock) *Ledger {
	t.Helper()
	l, err := Open(dir, c.Now)
	if err != nil {
		t.Fatal(err)
	}
	return l
}

func TestUpsertReplacesSameSessionRole(t *testing.T) {
	c := &fakeClock{time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)}
	l := newLedger(t, t.TempDir(), c)
	e := Entry{SessionID: "s1", Role: RoleWorker, StartedAt: c.t, TokensIn: 1}
	if err := l.Upsert(e); err != nil {
		t.Fatal(err)
	}
	e.TokensIn = 5
	if err := l.Upsert(e); err != nil {
		t.Fatal(err)
	}
	if err := l.Upsert(Entry{SessionID: "s1", Role: RoleVerification, StartedAt: c.t}); err != nil {
		t.Fatal(err)
	}
	got := l.Entries()
	if len(got) != 2 {
		t.Fatalf("want 2 entries, got %d", len(got))
	}
	for _, g := range got {
		if g.Role == RoleWorker && g.TokensIn != 5 {
			t.Fatalf("worker not updated: %+v", g)
		}
	}
	if err := l.Upsert(Entry{}); err == nil {
		t.Fatal("empty session id must fail")
	}
}

func TestReloadFromDisk(t *testing.T) {
	dir := t.TempDir()
	c := &fakeClock{time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)}
	l := newLedger(t, dir, c)
	want := Entry{SessionID: "s1", AutomatonID: "a", TaskID: "t", Role: RoleDecompose, Backend: "claude-code",
		StartedAt: c.t, EndedAt: c.t.Add(time.Minute), TokensIn: 1, TokensOut: 2, CacheRead: 3,
		CacheCreation: 4, ToolCalls: 5, ActiveSeconds: 6.5}
	if err := l.Upsert(want); err != nil {
		t.Fatal(err)
	}
	got := newLedger(t, dir, c).Entries()
	if len(got) != 1 || got[0] != want {
		t.Fatalf("reload mismatch: %+v", got)
	}
}

func TestPruneDropsOldEntries(t *testing.T) {
	dir := t.TempDir()
	c := &fakeClock{time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)}
	l := newLedger(t, dir, c)
	old := c.t.Add(-40 * 24 * time.Hour)
	_ = l.Upsert(Entry{SessionID: "old", Role: RoleWorker, StartedAt: old, EndedAt: old.Add(time.Hour)})
	_ = l.Upsert(Entry{SessionID: "new", Role: RoleWorker, StartedAt: c.t.Add(-time.Hour)})
	if err := l.Prune(c.t); err != nil {
		t.Fatal(err)
	}
	got := newLedger(t, dir, c).Entries()
	if len(got) != 1 || got[0].SessionID != "new" {
		t.Fatalf("prune result: %+v", got)
	}
}

func TestHorizon(t *testing.T) {
	dir := t.TempDir()
	c := &fakeClock{time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)}
	l := newLedger(t, dir, c)
	if !l.Horizon().Equal(c.t) {
		t.Fatalf("fresh horizon should be creation time, got %v", l.Horizon())
	}
	later := c.t.Add(60 * 24 * time.Hour)
	if err := l.Prune(later); err != nil {
		t.Fatal(err)
	}
	want := later.Add(-Retention)
	if !l.Horizon().Equal(want) {
		t.Fatalf("horizon %v want %v", l.Horizon(), want)
	}
	if !newLedger(t, dir, c).Horizon().Equal(want) {
		t.Fatal("horizon not persisted")
	}
}

func TestUpsertRejectsUnknownRole(t *testing.T) {
	c := &fakeClock{time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)}
	l := newLedger(t, t.TempDir(), c)
	if err := l.Upsert(Entry{SessionID: "s", Role: "bogus"}); err == nil {
		t.Fatal("unknown role must fail")
	}
	if len(l.Entries()) != 0 {
		t.Fatal("rejected entry was stored")
	}
}

func TestFreshHorizonSurvivesReopen(t *testing.T) {
	dir := t.TempDir()
	c := &fakeClock{time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)}
	first := newLedger(t, dir, c)
	c.t = c.t.Add(48 * time.Hour)
	if got := newLedger(t, dir, c).Horizon(); !got.Equal(first.Horizon()) {
		t.Fatalf("horizon moved on reopen: %v vs %v", got, first.Horizon())
	}
}

func TestOpenCorruptFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "spend_ledger.json"), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	c := &fakeClock{time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)}
	if _, err := Open(dir, c.Now); err == nil {
		t.Fatal("corrupt file must fail to open")
	}
}

func TestFailedSaveLeavesMemoryUnchanged(t *testing.T) {
	base := t.TempDir()
	dir := filepath.Join(base, "data")
	c := &fakeClock{time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)}
	l := newLedger(t, dir, c)
	old := c.t.Add(-40 * 24 * time.Hour)
	if err := l.Upsert(Entry{SessionID: "old", Role: RoleWorker, StartedAt: old}); err != nil {
		t.Fatal(err)
	}
	horizon := l.Horizon()
	// Replace the data dir with a file so every later save fails.
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dir, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := l.Upsert(Entry{SessionID: "new", Role: RoleWorker, StartedAt: c.t}); err == nil {
		t.Fatal("upsert should fail")
	}
	if err := l.Prune(c.t); err == nil {
		t.Fatal("prune should fail")
	}
	got := l.Entries()
	if len(got) != 1 || got[0].SessionID != "old" {
		t.Fatalf("memory changed after failed save: %+v", got)
	}
	if !l.Horizon().Equal(horizon) {
		t.Fatal("horizon changed after failed save")
	}
}
