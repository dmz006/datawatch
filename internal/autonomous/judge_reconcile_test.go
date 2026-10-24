package autonomous

import (
	"sort"
	"strings"
	"testing"
	"time"
)

// fakeJudgeHost is the JudgeHost the judge-reconcile tests drive the manager
// through.
type fakeJudgeHost struct {
	live   []JudgeSession
	ended  map[string]string // sessionID -> reason
	bound  map[string]string // holder -> sessionID
	endErr error
}

func (h *fakeJudgeHost) LiveJudges() []JudgeSession {
	out := make([]JudgeSession, len(h.live))
	copy(out, h.live)
	return out
}

func (h *fakeJudgeHost) End(sessionID, reason string) error {
	if h.endErr != nil {
		return h.endErr
	}
	if h.ended == nil {
		h.ended = map[string]string{}
	}
	h.ended[sessionID] = reason
	out := h.live[:0]
	for _, s := range h.live {
		if s.ID != sessionID {
			out = append(out, s)
		}
	}
	h.live = out
	return nil
}

func (h *fakeJudgeHost) BindLease(holder, sessionID string) {
	if h.bound == nil {
		h.bound = map[string]string{}
	}
	h.bound[holder] = sessionID
}

func (h *fakeJudgeHost) endedIDs() []string {
	ids := make([]string, 0, len(h.ended))
	for id := range h.ended {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// judgeUnit is one test unit: a title label for its task and the status the
// reconcile should see for it.
type judgeUnit struct {
	id     string
	status TaskStatus
}

// judgeFixture returns a running PRD with one task per unit (the same shape
// the verifier tests use) plus a manager.
func judgeFixture(t *testing.T, units []judgeUnit) (*Manager, *PRD) {
	t.Helper()
	stories := []Story{{Tasks: make([]Task, 0, len(units))}}
	for _, u := range units {
		stories[0].Tasks = append(stories[0].Tasks, Task{Title: "Task " + u.id, Status: u.status})
	}
	m, prd := verifyingBootManager(t, stories)
	return m, prd
}

func TestJudgeRestartEndsFinishedAndGoneUnits(t *testing.T) {
	// One unit whose task is done, one unit that no longer exists at all.
	m, prd := judgeFixture(t, []judgeUnit{{id: "x", status: TaskCompleted}})
	h := &fakeJudgeHost{live: []JudgeSession{
		{ID: "j-done", UnitID: prd.Story[0].Tasks[0].ID, Started: time.Now()},
		{ID: "j-gone", UnitID: "ffffffffffffffff", Started: time.Now()},
	}}

	acts := m.ReconcileJudges(h)
	if len(acts) != 2 {
		t.Fatalf("actions = %v, want one end per session", acts)
	}
	for _, a := range acts {
		if a.Kind != judgeActionEnd || a.Reason == "" {
			t.Errorf("action %v: want end with a reason", a)
		}
	}
	if got := h.endedIDs(); len(got) != 2 || got[0] != "j-done" || got[1] != "j-gone" {
		t.Errorf("ended = %v, want [j-done j-gone]", got)
	}
	for _, r := range h.ended {
		if !strings.Contains(r, "stale") {
			t.Errorf("end reason %q should say what is stale", r)
		}
	}
	if len(h.bound) != 0 {
		t.Errorf("no lease should be bound for a stale judge: %v", h.bound)
	}

	got, _ := m.store.GetPRD(prd.ID)
	if d := lastDecision(got, judgeReconcileKind); d == nil {
		t.Error("the reconcile left no decision on the PRD")
	}
	if len(h.live) != 0 {
		t.Errorf("host still lists ended sessions: %v", h.live)
	}
}

func TestJudgeRestartEndsUnneededPRDJudges(t *testing.T) {
	// PRD-level judges whose PRD is no longer running are stale.
	m, prd := judgeFixture(t, []judgeUnit{{id: "x", status: TaskVerifying}})
	prd.Status = PRDCompleted
	if err := m.store.SavePRD(prd); err != nil {
		t.Fatal(err)
	}
	h := &fakeJudgeHost{live: []JudgeSession{
		{ID: "j-prd", UnitID: prd.ID, Started: time.Now()},
	}}

	acts := m.ReconcileJudges(h)
	if len(acts) != 1 || acts[0].Kind != judgeActionEnd || acts[0].UnitID != prd.ID {
		t.Fatalf("actions = %v, want one end for the PRD unit", acts)
	}
	if _, ok := h.ended["j-prd"]; !ok {
		t.Errorf("ended = %v, want j-prd", h.ended)
	}
	if len(h.bound) != 0 {
		t.Errorf("no lease should be bound: %v", h.bound)
	}
}

func TestJudgeRestartEndsDuplicatesAndKeepsNewest(t *testing.T) {
	m, prd := judgeFixture(t, []judgeUnit{{id: "x", status: TaskVerifying}})
	taskID := prd.Story[0].Tasks[0].ID
	now := time.Now()
	h := &fakeJudgeHost{live: []JudgeSession{
		{ID: "old-1", UnitID: taskID, Started: now.Add(-3 * time.Hour)},
		{ID: "new", UnitID: taskID, Started: now},
		{ID: "old-2", UnitID: taskID, Started: now.Add(-time.Hour)},
	}}

	acts := m.ReconcileJudges(h)
	var ends, adopts int
	for _, a := range acts {
		switch a.Kind {
		case judgeActionEnd:
			ends++
		case judgeActionAdopt:
			adopts++
			if a.SessionID != "new" {
				t.Errorf("adopted session = %q, want the newest new", a.SessionID)
			}
		default:
			t.Errorf("unknown action kind %q", a.Kind)
		}
	}
	if ends != 2 {
		t.Errorf("ended = %d, want two duplicates", ends)
	}
	if got := h.endedIDs(); got[0] != "old-1" || got[1] != "old-2" {
		t.Errorf("ended = %v, want the two oldest", got)
	}
	if h.bound[JudgeHolder(taskID)] != "new" {
		t.Errorf("lease = %v, want the newest judge at judge:%s", h.bound, taskID)
	}
	if len(h.live) != 1 || h.live[0].ID != "new" {
		t.Errorf("live = %v, want only the newest", h.live)
	}
}

func TestJudgeRestartAdoptsSingleJudge(t *testing.T) {
	m, prd := judgeFixture(t, []judgeUnit{{id: "x", status: TaskVerifying}})
	taskID := prd.Story[0].Tasks[0].ID
	h := &fakeJudgeHost{live: []JudgeSession{
		{ID: "only", UnitID: taskID, Started: time.Now()},
	}}

	type auditEntry struct{ action string }
	var audits []auditEntry
	m.SetAuditFn(func(action string, d map[string]any) {
		if action == judgeReconcileAction {
			audits = append(audits, auditEntry{action: d["action"].(string)})
		}
	})

	acts := m.ReconcileJudges(h)
	if len(acts) != 1 || acts[0].Kind != judgeActionAdopt || acts[0].SessionID != "only" {
		t.Fatalf("actions = %v, want one adopt of only", acts)
	}
	if h.bound[JudgeHolder(taskID)] != "only" {
		t.Errorf("lease = %v, want judge:%s -> only", h.bound, taskID)
	}
	if len(audits) != 1 || audits[0].action != judgeActionAdopt {
		t.Errorf("audits = %v, want one autonomous_judge_reconcile adopt entry", audits)
	}
	got, _ := m.store.GetPRD(prd.ID)
	if d := lastDecision(got, judgeReconcileKind); d == nil || d.Actor != "autonomous" {
		t.Errorf("PRD decision = %+v, want an autonomous judge_reconcile entry", d)
	}
}

func TestJudgeRestartNilHostSkips(t *testing.T) {
	m, _ := judgeFixture(t, []judgeUnit{{id: "x", status: TaskVerifying}})
	if acts := m.ReconcileJudges(nil); acts != nil {
		t.Errorf("actions = %v, want none with a nil host", acts)
	}
	// SetJudgeHost with nil keeps the reconcile a no-op too.
	m.SetJudgeHost(nil)
	if acts := m.ReconcileJudges(nil); acts != nil {
		t.Errorf("actions = %v, want none after SetJudgeHost(nil)", acts)
	}
}

func TestJudgeRestartIdempotent(t *testing.T) {
	m, prd := judgeFixture(t, []judgeUnit{
		{id: "x", status: TaskVerifying},
		{id: "y", status: TaskCompleted},
	})
	xID, yID := prd.Story[0].Tasks[0].ID, prd.Story[0].Tasks[1].ID
	now := time.Now()
	h := &fakeJudgeHost{live: []JudgeSession{
		{ID: "x1", UnitID: xID, Started: now.Add(-time.Hour)},
		{ID: "x2", UnitID: xID, Started: now.Add(-2 * time.Hour)},
		{ID: "y1", UnitID: yID, Started: now},
	}}

	first := m.ReconcileJudges(h)
	if len(first) != 3 {
		t.Fatalf("first pass = %v, want 3 actions (end y1, end x2, adopt x1)", first)
	}
	var keep string
	for _, a := range first {
		if a.Kind == judgeActionAdopt {
			keep = a.SessionID
		}
	}
	if keep != "x1" {
		t.Errorf("adopted = %q, want the newest x1", keep)
	}

	// A second run sees nothing left to do and records no new decisions.
	got, _ := m.store.GetPRD(prd.ID)
	decisionsAfterFirst := len(got.Decisions)
	second := m.ReconcileJudges(h)
	if len(second) != 0 {
		t.Fatalf("second pass = %v, want no actions", second)
	}
	got, _ = m.store.GetPRD(prd.ID)
	if n := len(got.Decisions); n != decisionsAfterFirst {
		t.Errorf("decisions %d -> %d after a no-op second pass, want unchanged", decisionsAfterFirst, n)
	}
	if h.bound[JudgeHolder(xID)] != keep {
		t.Errorf("lease = %v, want unchanged (judge:%s -> %s)", h.bound, xID, keep)
	}
}

// TestJudgePlanPureTieBreaksByID exercises the pure planner's deterministic
// keep choice when two judges for one unit started at the same instant.
func TestJudgePlanPureTieBreaksByID(t *testing.T) {
	at := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	lookup := func(unitID string) (JudgeUnitInfo, bool) {
		return JudgeUnitInfo{PRDID: "prd-1", NeedsJudge: true}, true
	}
	sessions := []JudgeSession{
		{ID: "aa", UnitID: "unit-1", Started: at},
		{ID: "zz", UnitID: "unit-1", Started: at},
	}
	acts := PlanJudgeReconcile(sessions, lookup)
	if len(acts) != 2 {
		t.Fatalf("actions = %v, want one end + one adopt", acts)
	}
	var ended, adopted string
	for _, a := range acts {
		switch a.Kind {
		case judgeActionEnd:
			ended = a.SessionID
		case judgeActionAdopt:
			adopted = a.SessionID
		}
	}
	if ended != "aa" || adopted != "zz" {
		t.Errorf("ended/adopted = %q/%q, want the larger id zz kept deterministically", ended, adopted)
	}
}
