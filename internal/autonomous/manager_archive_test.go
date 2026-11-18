package autonomous

import (
	"testing"
	"time"
)

func newArchiveManager(t *testing.T) *Manager {
	t.Helper()
	m, err := NewManager(t.TempDir(), Config{}, nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	return m
}

func archivedCount(m *Manager) int {
	n := 0
	for _, p := range m.store.ListPRDs() {
		if p.Archive != nil {
			n++
		}
	}
	return n
}

func TestArchivePassHonoursSettingsAndSessionCheck(t *testing.T) {
	m := newArchiveManager(t)
	// The fixture is dated against the real clock: the pass uses time.Now.
	fixtureNow := archiveTestNow
	_ = fixtureNow
	id := fatArchiveFixture(t, m.store, PRDCompleted, 0)
	m.store.WithLock(func() {
		m.store.prds[id].UpdatedAt = time.Now().Add(-90 * 24 * time.Hour)
		m.store.rec.lastFlush = time.Time{}
	})

	// Switched off: nothing happens.
	m.SetArchiveSettings(func() (bool, int) { return false, 30 })
	m.SetSessionAliveFn(func(string) bool { return false })
	m.runArchivePass()
	if archivedCount(m) != 0 {
		t.Fatal("pass archived while autonomous.archive.enabled is false")
	}
	// On, but no live-session check wired: the pass must not guess.
	m2 := newArchiveManager(t)
	id2 := fatArchiveFixture(t, m2.store, PRDCompleted, 0)
	m2.store.WithLock(func() { m2.store.prds[id2].UpdatedAt = time.Now().Add(-90 * 24 * time.Hour) })
	m2.SetArchiveSettings(func() (bool, int) { return true, 30 })
	m2.runArchivePass()
	if archivedCount(m2) != 0 {
		t.Fatal("pass archived without a session check")
	}
	if _, err := m2.RunAutomatonArchive(); err == nil {
		t.Fatal("manual run must refuse without a session check")
	}
	// A retention longer than the age keeps it.
	m.SetArchiveSettings(func() (bool, int) { return true, 365 })
	m.runArchivePass()
	if archivedCount(m) != 0 {
		t.Fatal("pass archived an Automaton newer than the retention")
	}
	// A hot write holds the pass back.
	m.SetArchiveSettings(func() (bool, int) { return true, 30 })
	m.store.WithLock(func() { m.store.rec.lastFlush = time.Now() })
	m.runArchivePass()
	if archivedCount(m) != 0 {
		t.Fatal("pass archived while an Automaton write was hot")
	}
	// Quiet, enabled, retention met: it archives.
	m.store.WithLock(func() { m.store.rec.lastFlush = time.Now().Add(-time.Hour) })
	m.runArchivePass()
	if archivedCount(m) != 1 {
		t.Fatal("pass did not archive an eligible Automaton")
	}
	// The dry run through the manager reports nothing more to do.
	if plan := m.PlanAutomatonArchive(); len(plan.Eligible) != 0 || plan.AlreadyArchive != 1 {
		t.Fatalf("plan after the pass: %+v", plan)
	}
	if _, err := m.ReadAutomatonArchive(id); err != nil {
		t.Fatalf("ReadAutomatonArchive: %v", err)
	}
}

func TestArchivePassRefusesOpenSession(t *testing.T) {
	m := newArchiveManager(t)
	id := fatArchiveFixture(t, m.store, PRDCompleted, 0)
	m.store.WithLock(func() { m.store.prds[id].UpdatedAt = time.Now().Add(-90 * 24 * time.Hour) })
	m.SetArchiveSettings(func() (bool, int) { return true, 30 })
	m.SetSessionAliveFn(func(sid string) bool { return sid == "w-1-2" })
	m.runArchivePass()
	if archivedCount(m) != 0 {
		t.Fatal("pass archived an Automaton that has an open session")
	}
	if plan := m.PlanAutomatonArchive(); len(plan.Skipped) != 1 {
		t.Fatalf("plan should name the skipped Automaton: %+v", plan)
	}
}

func TestArchiveRetentionDefaults(t *testing.T) {
	m := newArchiveManager(t)
	if on, ret := m.archiveRetention(); !on || ret != 30*24*time.Hour {
		t.Fatalf("defaults: %v %v", on, ret)
	}
	m.SetArchiveSettings(func() (bool, int) { return true, 0 })
	if _, ret := m.archiveRetention(); ret != 30*24*time.Hour {
		t.Fatalf("zero retention must mean the default, got %v", ret)
	}
}
