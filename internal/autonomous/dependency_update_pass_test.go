// dependency_update_pass_test.go — the weekly dependency update pass with an
// injected clock: not due, due once, not due again, disabled, a previous run
// still active, and restart persistence.
package autonomous

import (
	"strings"
	"testing"
	"time"
)

type duHarness struct {
	t      *testing.T
	m      *Manager
	c      *Coordinator
	now    time.Time
	audits []string
}

func newDUHarness(t *testing.T, dir string) *duHarness {
	t.Helper()
	m, err := NewManager(dir, DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	h := &duHarness{t: t, m: m}
	h.c = h.coordinator()
	return h
}

func (h *duHarness) coordinator() *Coordinator {
	c := NewCoordinator(h.m, swFakeStarter{})
	c.dependencyUpdateLoc = time.UTC
	c.now = func() time.Time { return h.now }
	c.auditFn = func(action string, _ map[string]any) { h.audits = append(h.audits, action) }
	return c
}

// tick moves the clock and runs one pass.
func (h *duHarness) tick(at string) {
	h.t.Helper()
	ts, err := time.Parse("2006-01-02 15:04", at)
	if err != nil {
		h.t.Fatal(err)
	}
	h.now = ts
	h.c.dependencyUpdatePass()
}

func (h *duHarness) count(action string) int {
	n := 0
	for _, a := range h.audits {
		if a == action {
			n++
		}
	}
	return n
}

func (h *duHarness) setEnabled(v bool) {
	cfg := h.m.Config()
	cfg.Automation.DependencyUpdate.Enabled = &v
	h.m.mu.Lock()
	h.m.cfg = cfg
	h.m.mu.Unlock()
}

// 2026-11-09 is a Monday; the default schedule is monday 05:00.
func TestDependencyUpdatePassDueOnce(t *testing.T) {
	h := newDUHarness(t, t.TempDir())
	h.tick("2026-11-06 12:00") // first sight: baseline only
	h.tick("2026-11-09 04:00") // not due yet
	if h.count("dependency_update_started") != 0 || h.c.DependencyUpdateRun().At != (time.Time{}) {
		t.Fatal("ran before the schedule")
	}
	h.tick("2026-11-09 05:01") // due
	if h.count("dependency_update_started") != 1 {
		t.Fatalf("want one run, audits=%v", h.audits)
	}
	info := h.c.DependencyUpdateRun()
	if info.At.IsZero() || !strings.Contains(strings.ToLower(info.AutomatonTitle), "dependency") || info.Next.IsZero() {
		t.Fatalf("bad info %+v", info)
	}
	if len(h.m.store.ListPRDs()) != 1 {
		t.Fatal("expected exactly one Automaton")
	}
	h.tick("2026-11-09 05:30") // not due again
	h.tick("2026-11-10 09:00")
	if h.count("dependency_update_started") != 1 || len(h.m.store.ListPRDs()) != 1 {
		t.Fatalf("ran twice: %v", h.audits)
	}
}

func TestDependencyUpdatePassDisabled(t *testing.T) {
	h := newDUHarness(t, t.TempDir())
	h.setEnabled(false)
	h.tick("2026-11-06 12:00")
	h.tick("2026-11-09 06:00")
	if len(h.audits) != 0 || len(h.m.store.ListPRDs()) != 0 {
		t.Fatalf("disabled pass acted: %v", h.audits)
	}
	if !h.c.DependencyUpdateRun().Next.IsZero() {
		t.Fatal("next run reported while disabled")
	}
}

func TestDependencyUpdatePassPreviousRunStillActive(t *testing.T) {
	h := newDUHarness(t, t.TempDir())
	h.tick("2026-11-06 12:00")
	h.tick("2026-11-09 05:01")
	h.tick("2026-11-16 05:01") // next week; previous Automaton is still a draft
	if h.count("dependency_update_started") != 1 || h.count("dependency_update_skipped") != 1 {
		t.Fatalf("audits=%v", h.audits)
	}
	h.tick("2026-11-16 05:05") // still held: no second note
	if h.count("dependency_update_skipped") != 1 {
		t.Fatalf("skip noted repeatedly: %v", h.audits)
	}
	// Finish it: the held run starts on the next poll.
	prd := h.m.store.ListPRDs()[0]
	h.m.store.WithLock(func() { prd.Status = PRDCompleted })
	h.tick("2026-11-16 05:10")
	if h.count("dependency_update_started") != 2 {
		t.Fatalf("held run did not start: %v", h.audits)
	}
}

func TestDependencyUpdatePassRestartPersistence(t *testing.T) {
	dir := t.TempDir()
	h := newDUHarness(t, dir)
	h.tick("2026-11-06 12:00")
	h.tick("2026-11-09 05:01")
	title := h.c.DependencyUpdateRun().AutomatonTitle
	if h.count("dependency_update_started") != 1 {
		t.Fatalf("audits=%v", h.audits)
	}
	// Restart: a new manager and coordinator over the same data directory.
	h2 := newDUHarness(t, dir)
	h2.tick("2026-11-09 05:20")
	if h2.count("dependency_update_started") != 0 || len(h2.m.store.ListPRDs()) != 1 {
		t.Fatalf("ran twice after restart: %v", h2.audits)
	}
	if got := h2.c.DependencyUpdateRun().AutomatonTitle; got != title {
		t.Fatalf("last run lost: %q vs %q", got, title)
	}
}
