package autonomous

import (
	"bytes"
	"log"
	"strings"
	"sync"
	"testing"
	"time"
)

type orphanHarness struct {
	m            *Manager
	prd          *PRD
	now          time.Time
	mu           sync.Mutex
	list         []SessionInfo
	killed       []string
	callers      []string
	reasons      []string
	audits       []string
	auditDetails []map[string]any
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
	m.SetSessionKillerFn(func(id, caller, reason string) error {
		h.mu.Lock()
		defer h.mu.Unlock()
		h.killed = append(h.killed, id)
		h.callers = append(h.callers, caller)
		h.reasons = append(h.reasons, reason)
		return nil
	})
	m.SetAuditFn(func(a string, d map[string]any) {
		h.audits = append(h.audits, a)
		h.auditDetails = append(h.auditDetails, d)
	})
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
	if _, ok := h.m.orphanSeen[sessionShortID("sess-orphan")]; !ok {
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
	if _, ok := h.m.orphanSeen[sessionShortID("sess-orphan")]; ok {
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
	if _, ok := h.m.orphanSeen[sessionShortID("sess-late")]; !ok {
		t.Fatal("orphan not tracked")
	}
	prd, _ := h.m.Store().GetPRD(h.prd.ID)
	prd.Story[0].Tasks[0].SessionID = "sess-late"
	_ = h.m.Store().SavePRD(prd)
	h.now = h.now.Add(time.Hour)
	h.m.sweepOrphanSessions()
	if _, ok := h.m.orphanSeen[sessionShortID("sess-late")]; ok || len(h.killed) != 0 {
		t.Fatalf("bound session not cleared/left alone: seen=%v killed=%v", h.m.orphanSeen, h.killed)
	}
}

func TestEndAttributionOrphanSweepPassesNoteAsReason(t *testing.T) {
	h := newOrphanHarness(t)
	h.list = []SessionInfo{h.worker("sess-orphan")}
	h.m.sweepOrphanSessions()
	h.now = h.now.Add(301 * time.Second)
	h.m.sweepOrphanSessions()

	if len(h.killed) != 1 {
		t.Fatalf("killed = %v, want one end", h.killed)
	}
	if h.callers[0] != orphanSweepCaller {
		t.Errorf("caller = %q, want %q", h.callers[0], orphanSweepCaller)
	}
	want := orphanEndNote(h.worker("sess-orphan"), 301*time.Second, 300*time.Second)
	if h.reasons[0] != want {
		t.Errorf("reason = %q\nwant the sweep's note %q", h.reasons[0], want)
	}
	if !strings.Contains(h.reasons[0], "after 5m1s unbound") {
		t.Errorf("reason %q does not name the measured orphan age", h.reasons[0])
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

// bindTaskSession stores sessionID on the harness PRD's only task, the way the
// executor stores the short id it gets back at spawn.
func (h *orphanHarness) bindTaskSession(t *testing.T, sessionID string) {
	t.Helper()
	prd, ok := h.m.Store().GetPRD(h.prd.ID)
	if !ok {
		t.Fatalf("PRD %s not found", h.prd.ID)
	}
	prd.Story[0].Tasks[0].SessionID = sessionID
	if err := h.m.Store().SavePRD(prd); err != nil {
		t.Fatalf("SavePRD: %v", err)
	}
}

// auditDetail returns the details of the first audit entry named action, or nil.
func (h *orphanHarness) auditDetail(action string) map[string]any {
	for i, a := range h.audits {
		if a == action {
			return h.auditDetails[i]
		}
	}
	return nil
}

func TestOrphanSweepKeepsWorkerBoundByShortID(t *testing.T) {
	h := newOrphanHarness(t)
	h.bindTaskSession(t, "8b57")
	w := h.worker("local-node-8b57")
	w.TaskID = h.prd.Story[0].Tasks[0].ID
	h.list = []SessionInfo{w}
	for i := 0; i < 4; i++ {
		h.m.sweepOrphanSessions()
		h.now = h.now.Add(10 * time.Minute) // past the 300s grace several times over
	}
	if len(h.killed) != 0 {
		t.Fatalf("bound worker ended: %v", h.killed)
	}
	if len(h.m.orphanSeen) != 0 {
		t.Fatalf("bound worker tracked as an orphan: %v", h.m.orphanSeen)
	}
	if len(h.audits) != 0 {
		t.Fatalf("bound worker produced audit entries: %v", h.audits)
	}
}

func TestOrphanSweepEndsTrueOrphanByFullID(t *testing.T) {
	h := newOrphanHarness(t)
	h.bindTaskSession(t, "8b57")
	h.list = []SessionInfo{h.worker("local-node-c3d4")}

	h.m.sweepOrphanSessions()
	if len(h.killed) != 0 {
		t.Fatalf("killed %v on first sight", h.killed)
	}
	if h.auditDetail("autonomous_orphan_session_seen") == nil {
		t.Fatal("orphan not reported on first sight")
	}
	seen := h.auditDetail("autonomous_orphan_session_seen")
	if seen["session_id"] != "local-node-c3d4" || seen["session_short_id"] != "c3d4" {
		t.Errorf("seen audit ids = session_id %v, session_short_id %v; want the lister id and its short id", seen["session_id"], seen["session_short_id"])
	}

	h.now = h.now.Add(200 * time.Second) // inside the 300s grace
	h.m.sweepOrphanSessions()
	if len(h.killed) != 0 {
		t.Fatalf("killed %v before grace elapsed", h.killed)
	}

	h.now = h.now.Add(101 * time.Second) // 301s since first sight
	h.m.sweepOrphanSessions()
	if len(h.killed) != 1 || h.killed[0] != "local-node-c3d4" {
		t.Fatalf("killed = %v, want [local-node-c3d4]", h.killed)
	}
	ended := h.auditDetail("autonomous_orphan_session_ended")
	if ended == nil {
		t.Fatal("no ended audit entry")
	}
	if ended["session_id"] != "local-node-c3d4" || ended["session_short_id"] != "c3d4" {
		t.Errorf("ended audit ids = session_id %v, session_short_id %v; want the lister id and its short id", ended["session_id"], ended["session_short_id"])
	}
}

func TestOrphanSweepLogsSessionIDFormMismatchOnce(t *testing.T) {
	h := newOrphanHarness(t)
	h.bindTaskSession(t, "8b57")
	h.list = []SessionInfo{h.worker("local-node-c3d4")}

	prev := log.Writer()
	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(prev)
	for i := 0; i < 3; i++ {
		h.m.sweepOrphanSessions()
		h.now = h.now.Add(time.Minute)
	}
	log.SetOutput(prev)

	var formLines []string
	for _, line := range strings.Split(buf.String(), "\n") {
		if strings.Contains(line, "session id form check") {
			formLines = append(formLines, line)
		}
	}
	if len(formLines) != 1 {
		t.Fatalf("form-check line logged %d times over three passes, want 1:\n%s", len(formLines), buf.String())
	}
	for _, want := range []string{
		"[autonomous] orphan-sweep:",
		"full ids (example local-node-c3d4)",
		"short ids (example 8b57)",
		"by short id",
	} {
		if !strings.Contains(formLines[0], want) {
			t.Errorf("form-check line %q lacks %q", formLines[0], want)
		}
	}
}

func TestOrphanSweepFormCheckSilentWhenFormsMatch(t *testing.T) {
	h := newOrphanHarness(t)
	h.bindTaskSession(t, "8b57")
	h.list = []SessionInfo{h.worker("c3d4")}

	prev := log.Writer()
	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(prev)
	h.m.sweepOrphanSessions()
	log.SetOutput(prev)

	if strings.Contains(buf.String(), "session id form check") {
		t.Fatalf("form check logged although the lister and tasks both use short ids:\n%s", buf.String())
	}
}
