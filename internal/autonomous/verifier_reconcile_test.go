package autonomous

import (
	"sort"
	"strings"
	"testing"
	"time"
)

type fakeVerifierHost struct {
	live   []VerifierSession
	ended  map[string]string
	bound  map[string]string
	endErr error
}

func (h *fakeVerifierHost) LiveVerifiers() []VerifierSession {
	return append([]VerifierSession(nil), h.live...)
}

func (h *fakeVerifierHost) End(id, reason string) error {
	if h.endErr != nil {
		return h.endErr
	}
	if h.ended == nil {
		h.ended = map[string]string{}
	}
	h.ended[id] = reason
	kept := h.live[:0:0]
	for _, s := range h.live {
		if s.ID != id {
			kept = append(kept, s)
		}
	}
	h.live = kept
	return nil
}

func (h *fakeVerifierHost) BindLease(holder, sessionID string) {
	if h.bound == nil {
		h.bound = map[string]string{}
	}
	h.bound[holder] = sessionID
}

func (h *fakeVerifierHost) endedIDs() []string {
	var ids []string
	for id := range h.ended {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func reconcileFixture(t *testing.T, statuses ...TaskStatus) (*Manager, *PRD, []string) {
	t.Helper()
	var tasks []Task
	for i, st := range statuses {
		tasks = append(tasks, Task{Title: "task" + string(rune('a'+i)), Status: st})
	}
	m, prd := verifyingBootManager(t, []Story{{Title: "story", Tasks: tasks}})
	var ids []string
	for _, tk := range prd.Story[0].Tasks {
		ids = append(ids, tk.ID)
	}
	return m, prd, ids
}

func TestVerifierRestartReconcileDuplicatesKeepNewest(t *testing.T) {
	m, _, ids := reconcileFixture(t, TaskVerifying)
	now := time.Now()
	h := &fakeVerifierHost{live: []VerifierSession{
		{ID: "old1", TaskID: ids[0], Started: now.Add(-3 * time.Hour)},
		{ID: "new", TaskID: ids[0], Started: now},
		{ID: "old2", TaskID: ids[0], Started: now.Add(-time.Hour)},
	}}
	m.ReconcileVerifiers(h)
	if got := strings.Join(h.endedIDs(), ","); got != "old1,old2" {
		t.Fatalf("ended = %q, want old1,old2", got)
	}
	if len(h.live) != 1 || h.live[0].ID != "new" {
		t.Fatalf("live = %+v, want only the newest", h.live)
	}
	if h.bound[VerifyHolder(ids[0])] != "new" {
		t.Fatalf("lease not bound to newest: %v", h.bound)
	}
	for id, reason := range h.ended {
		if !strings.Contains(reason, "duplicate verifier") {
			t.Errorf("session %s reason %q not clear", id, reason)
		}
	}
}

func TestVerifierRestartReconcileEndsVerifierOfFinishedTask(t *testing.T) {
	m, prd, ids := reconcileFixture(t, TaskCompleted, TaskPending, TaskVerifying)
	h := &fakeVerifierHost{live: []VerifierSession{
		{ID: "v-done", TaskID: ids[0], Started: time.Now()},
		{ID: "v-pend", TaskID: ids[1], Started: time.Now()},
		{ID: "v-gone", TaskID: "no-such-task", Started: time.Now()},
	}}
	m.ReconcileVerifiers(h)
	if got := strings.Join(h.endedIDs(), ","); got != "v-done,v-gone,v-pend" {
		t.Fatalf("ended = %q", got)
	}
	if len(h.bound) != 0 {
		t.Fatalf("nothing should be adopted, bound=%v", h.bound)
	}
	got, _ := m.Store().GetPRD(prd.ID)
	found := false
	for _, d := range got.Decisions {
		if d.Kind == verifierReconcileKind && strings.Contains(d.Note, "v-done") {
			found = true
		}
	}
	if !found {
		t.Fatalf("no reconcile decision recorded: %+v", got.Decisions)
	}
}

func TestVerifierRestartReconcileAdoptsSingleVerifier(t *testing.T) {
	m, prd, ids := reconcileFixture(t, TaskVerifying)
	var audits []map[string]any
	m.SetAuditFn(func(action string, d map[string]any) {
		if action == verifierReconcileAction {
			audits = append(audits, d)
		}
	})
	h := &fakeVerifierHost{live: []VerifierSession{{ID: "only", TaskID: ids[0], Started: time.Now()}}}
	acts := m.ReconcileVerifiers(h)
	if len(acts) != 1 || acts[0].Kind != verifierActionAdopt {
		t.Fatalf("actions = %+v", acts)
	}
	if len(h.ended) != 0 {
		t.Fatalf("must not end the only verifier: %v", h.ended)
	}
	if h.bound[VerifyHolder(ids[0])] != "only" {
		t.Fatalf("lease not re-bound: %v", h.bound)
	}
	tk, _ := m.Store().TaskSnapshot(ids[0])
	if tk.VerifierSessionID != "only" {
		t.Fatalf("verifier session not recorded: %q", tk.VerifierSessionID)
	}
	if len(audits) != 1 {
		t.Fatalf("audits = %d, want 1", len(audits))
	}
	got, _ := m.Store().GetPRD(prd.ID)
	if lastDecision(got, verifierReconcileKind) == nil {
		t.Fatal("no task decision recorded")
	}
}

func TestVerifierRestartReconcileLeavesUnrelatedAlone(t *testing.T) {
	m, _, ids := reconcileFixture(t, TaskVerifying, TaskVerifying)
	now := time.Now()
	// Only verifier sessions are ever listed; a worker session never appears.
	h := &fakeVerifierHost{live: []VerifierSession{
		{ID: "a-old", TaskID: ids[0], Started: now.Add(-time.Hour)},
		{ID: "a-new", TaskID: ids[0], Started: now},
		{ID: "b-only", TaskID: ids[1], Started: now.Add(-2 * time.Hour)},
		{ID: "no-task", TaskID: "", Started: now},
	}}
	m.ReconcileVerifiers(h)
	if got := strings.Join(h.endedIDs(), ","); got != "a-old" {
		t.Fatalf("ended = %q, want only a-old", got)
	}
	if h.bound[VerifyHolder(ids[1])] != "b-only" || h.bound[VerifyHolder(ids[0])] != "a-new" {
		t.Fatalf("bound = %v", h.bound)
	}
}

func TestVerifierRestartReconcileIdempotent(t *testing.T) {
	m, prd, ids := reconcileFixture(t, TaskVerifying, TaskCompleted)
	now := time.Now()
	h := &fakeVerifierHost{live: []VerifierSession{
		{ID: "x1", TaskID: ids[0], Started: now.Add(-time.Hour)},
		{ID: "x2", TaskID: ids[0], Started: now},
		{ID: "y1", TaskID: ids[1], Started: now},
	}}
	first := m.ReconcileVerifiers(h)
	got1, _ := m.Store().GetPRD(prd.ID)
	n1 := len(got1.Decisions)
	second := m.ReconcileVerifiers(h)
	got2, _ := m.Store().GetPRD(prd.ID)
	if len(first) != 3 || len(second) != 0 {
		t.Fatalf("first=%d second=%d, want 3 and 0", len(first), len(second))
	}
	if len(got2.Decisions) != n1 {
		t.Fatalf("second run added decisions: %d -> %d", n1, len(got2.Decisions))
	}
}
