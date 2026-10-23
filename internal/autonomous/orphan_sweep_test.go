package autonomous

import (
	"sync"
	"testing"
	"time"
)

type orphanHarness struct {
	m      *Manager
	prd    *PRD
	now    time.Time
	mu     sync.Mutex
	list   []SessionInfo
	killed []string
	audits []string
}

func newOrphanHarness(t *testing.T) *orphanHarness {
	t.Helper()
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	prd, err := m.CreatePRD("spec", "/projorphan", "", "", "")
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	_ = m.Store().SetStories(prd.ID, []Story{{
		Title: "story",
		Tasks: []Task{{Title: "task", Status: TaskInProgress, SessionID: "sess-bound"}},
	}})
	prd, _ = m.Store().GetPRD(prd.ID)
	h := &orphanHarness{m: m, prd: prd, now: time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)}
	m.orphanNow = func() time.Time { return h.now }
	m.SetSessionListerFn(func() []SessionInfo {
		h.mu.Lock()
		defer h.mu.Unlock()
		return append([]SessionInfo(nil), h.list...)
	})
	m.SetSessionKillerFn(func(id string) error {
		h.mu.Lock()
		defer h.mu.Unlock()
		h.killed = append(h.killed, id)
		return nil
	})
	m.SetAuditFn(func(a string, _ map[string]any) { h.audits = append(h.audits, a) })
	return h
}

func (h *orphanHarness) worker(id string) SessionInfo {
	return SessionInfo{ID: id, Name: "autonomous:do the thing", PRDID: h.prd.ID, TaskID: "other-task", Alive: true}
}

func TestOrphanSweepReportsBeforeGrace(t *testing.T) {
	h := newOrphanHarness(t)
	h.list = []SessionInfo{h.worker("sess-orphan")}
	h.m.sweepOrphanSessions()
	h.now = h.now.Add(2 * time.Minute) // within the 300s default grace
	h.m.sweepOrphanSessions()
	if len(h.killed) != 0 {
		t.Fatalf("killed %v before grace elapsed", h.killed)
	}
	if _, ok := h.m.orphanSeen["sess-orphan"]; !ok {
		t.Fatal("first-seen time not tracked")
	}
	got, _ := h.m.Store().GetPRD(h.prd.ID)
	if lastDecision(got, "orphan_session_seen") == nil {
		t.Fatal("no orphan_session_seen decision recorded")
	}
}

func TestOrphanSweepEndsOrphanAfterGrace(t *testing.T) {
	h := newOrphanHarness(t)
	h.list = []SessionInfo{h.worker("sess-orphan")}
	h.m.sweepOrphanSessions()
	h.now = h.now.Add(301 * time.Second)
	h.m.sweepOrphanSessions()
	if len(h.killed) != 1 || h.killed[0] != "sess-orphan" {
		t.Fatalf("killed = %v, want [sess-orphan]", h.killed)
	}
	if _, ok := h.m.orphanSeen["sess-orphan"]; ok {
		t.Fatal("first-seen entry not cleared after ending")
	}
}

func TestOrphanSweepLeavesBoundSession(t *testing.T) {
	h := newOrphanHarness(t)
	b := h.worker("sess-bound")
	b.TaskID = h.prd.Story[0].Tasks[0].ID
	h.list = []SessionInfo{b}
	h.m.sweepOrphanSessions()
	h.now = h.now.Add(time.Hour)
	h.m.sweepOrphanSessions()
	if len(h.killed) != 0 {
		t.Fatalf("bound session ended: %v", h.killed)
	}

	// Becoming bound clears the first-seen time.
	h.list = []SessionInfo{h.worker("sess-late")}
	h.m.sweepOrphanSessions()
	if _, ok := h.m.orphanSeen["sess-late"]; !ok {
		t.Fatal("orphan not tracked")
	}
	prd, _ := h.m.Store().GetPRD(h.prd.ID)
	prd.Story[0].Tasks[0].SessionID = "sess-late"
	_ = h.m.Store().SavePRD(prd)
	h.now = h.now.Add(time.Hour)
	h.m.sweepOrphanSessions()
	if _, ok := h.m.orphanSeen["sess-late"]; ok || len(h.killed) != 0 {
		t.Fatalf("bound session not cleared/left alone: seen=%v killed=%v", h.m.orphanSeen, h.killed)
	}
}

func TestOrphanSweepWritesAuditEntry(t *testing.T) {
	h := newOrphanHarness(t)
	h.list = []SessionInfo{h.worker("sess-orphan")}
	h.m.sweepOrphanSessions()
	h.now = h.now.Add(10 * time.Minute)
	h.m.sweepOrphanSessions()
	want := map[string]bool{"autonomous_orphan_session_seen": false, "autonomous_orphan_session_ended": false}
	for _, a := range h.audits {
		if _, ok := want[a]; ok {
			want[a] = true
		}
	}
	for a, ok := range want {
		if !ok {
			t.Errorf("audit entry %s missing (got %v)", a, h.audits)
		}
	}
	got, _ := h.m.Store().GetPRD(h.prd.ID)
	if lastDecision(got, "orphan_session_ended") == nil {
		t.Fatal("no orphan_session_ended decision recorded")
	}
}

func TestOrphanSweepIgnoresVerifierDecomposeAndJudgeSessions(t *testing.T) {
	h := newOrphanHarness(t)
	for _, name := range []string{
		"autonomous:verify:task-1", "autonomous:decompose", "autonomous:guardrail:unit-1",
		"autonomous:council", "autonomous:eval:suite",
	} {
		s := h.worker("sess-" + name)
		s.Name = name
		h.list = append(h.list, s)
	}
	// Linked-less planning/judge sessions too.
	h.list = append(h.list, SessionInfo{ID: "sess-nolink", Name: "autonomous:decompose", Alive: true})
	h.m.sweepOrphanSessions()
	h.now = h.now.Add(24 * time.Hour)
	h.m.sweepOrphanSessions()
	if len(h.killed) != 0 {
		t.Fatalf("non-worker sessions ended: %v", h.killed)
	}
}
