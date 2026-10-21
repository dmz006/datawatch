package autonomous

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"
)

// outOfScopeSpec references an absolute path outside the PRD's project dir
// and outside any read/write dir, so LintTaskScope blocks it before spawn
// under both the current and any stricter future scope-linter rules.
const outOfScopeSpec = "Measure peak RSS with /home/other/elsewhere/tool --flag"

// spawnRecorder is a stub SpawnFn that records every SpawnRequest it sees.
type spawnRecorder struct {
	mu   sync.Mutex
	reqs []SpawnRequest
}

func (r *spawnRecorder) spawn(_ context.Context, req SpawnRequest) (SpawnResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.reqs = append(r.reqs, req)
	return SpawnResult{SessionID: "s-" + req.TaskID}, nil
}

func (r *spawnRecorder) forTask(id string) []SpawnRequest {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []SpawnRequest
	for _, q := range r.reqs {
		if q.TaskID == id {
			out = append(out, q)
		}
	}
	return out
}

// blockedPRDFixture drives a PRD into the live BL421 incident shape through
// the real executor: story 1 holds task A (already completed) and task B
// (out-of-scope spec, so the pre-spawn scope lint fails it); story 2 holds a
// still-pending task C, so the story-1 failure halts the PRD as blocked
// rather than failed. Returns the manager, API, PRD and task IDs A, B, C.
func blockedPRDFixture(t *testing.T) (*Manager, *API, *PRD, string, string, string) {
	t.Helper()
	m, api, _, verify := apiFixture(t)
	prd, err := m.CreatePRD("s", "/w/proj", "opencode", "", EffortNormal)
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	if err := m.Store().SetStories(prd.ID, []Story{
		{Title: "S1", Tasks: []Task{
			{Title: "A", Spec: "write docs/a.md", Status: TaskCompleted},
			{Title: "B", Spec: outOfScopeSpec},
		}},
		{Title: "S2", Tasks: []Task{{Title: "C", Spec: "write docs/c.md"}}},
	}); err != nil {
		t.Fatalf("SetStories: %v", err)
	}
	prd, _ = m.Store().GetPRD(prd.ID)
	a := prd.Story[0].Tasks[0].ID
	b := prd.Story[0].Tasks[1].ID
	c := prd.Story[1].Tasks[0].ID

	rec := &spawnRecorder{}
	runToTerminalOrBlocked(t, m, api, prd.ID, rec.spawn, verify)
	got := waitForPRDStatus(t, m, prd.ID, 30*time.Second, PRDBlocked)
	if n := len(rec.reqs); n != 0 {
		t.Fatalf("no task should spawn (A completed, B out of scope, C halted); got %d spawns", n)
	}
	if got.Status != PRDBlocked {
		t.Fatalf("want PRD blocked after story failure halt, got %s", got.Status)
	}
	bt := lookupTask(got, b)
	if bt.Status != TaskFailed && bt.Status != TaskBlocked {
		t.Fatalf("task B should be failed/blocked by scope lint, got %q", bt.Status)
	}
	if !strings.Contains(bt.Error, "scope:") {
		t.Fatalf("task B error should be the scope-lint error, got %q", bt.Error)
	}
	if at := lookupTask(got, a); at.Status != TaskCompleted {
		t.Fatalf("task A must stay completed, got %q", at.Status)
	}
	return m, api, got, a, b, c
}

// TestBlockedTaskEdit_ReproEditAcceptedOnBlockedPRD reproduces BL421: a
// scope-blocked task on a blocked PRD could not have its spec corrected in
// place. Pre-fix result (unmodified EditTaskSpec): FAIL — `prd "..." status
// "blocked" is locked; only needs_review / revisions_asked / cancelled accept
// task edits`. Post-fix the edit is accepted.
func TestBlockedTaskEdit_ReproEditAcceptedOnBlockedPRD(t *testing.T) {
	m, _, prd, _, b, _ := blockedPRDFixture(t)
	if _, err := m.EditTaskSpec(prd.ID, b, "write docs/x.md", "tester"); err != nil {
		t.Fatalf("EditTaskSpec on blocked PRD's stuck task: %v", err)
	}
}

func TestBlockedTaskEdit_EditPersistsAndLeavesCompletedSiblingAlone(t *testing.T) {
	m, _, prd, a, b, _ := blockedPRDFixture(t)
	before := lookupTask(prd, a)
	beforeSpec, beforeStatus := before.Spec, before.Status

	if _, err := m.EditTaskSpec(prd.ID, b, "write docs/x.md", "tester"); err != nil {
		t.Fatalf("EditTaskSpec: %v", err)
	}
	got, _ := m.Store().GetPRD(prd.ID)
	if s := lookupTask(got, b).Spec; s != "write docs/x.md" {
		t.Fatalf("new spec not persisted: %q", s)
	}
	last := got.Decisions[len(got.Decisions)-1]
	if last.Kind != "edit_task" || last.Actor != "tester" || !strings.Contains(last.Note, b) {
		t.Fatalf("want edit_task decision for %s by tester, got %+v", b, last)
	}
	at := lookupTask(got, a)
	if at.Spec != beforeSpec || at.Status != beforeStatus {
		t.Fatalf("completed sibling A changed: spec %q->%q status %q->%q", beforeSpec, at.Spec, beforeStatus, at.Status)
	}
	if got.Status != PRDBlocked {
		t.Fatalf("edit must not change PRD status, got %s", got.Status)
	}
}

func TestBlockedTaskEdit_CompletedTaskImmutable(t *testing.T) {
	for _, st := range []PRDStatus{PRDBlocked, PRDNeedsReview, PRDCancelled} {
		t.Run(string(st), func(t *testing.T) {
			m, _, prd, a, _, _ := blockedPRDFixture(t)
			prd.Status = st
			if err := m.Store().SavePRD(prd); err != nil {
				t.Fatal(err)
			}
			_, err := m.EditTaskSpec(prd.ID, a, "rewrite history", "tester")
			if err == nil || !strings.Contains(err.Error(), "completed task specs are immutable") {
				t.Fatalf("want completed-task immutability error, got %v", err)
			}
			got, _ := m.Store().GetPRD(prd.ID)
			if s := lookupTask(got, a).Spec; s != "write docs/a.md" {
				t.Fatalf("completed task spec changed to %q", s)
			}
		})
	}
}

func TestBlockedTaskEdit_PendingSiblingRejectedWhileBlocked(t *testing.T) {
	m, _, prd, _, _, c := blockedPRDFixture(t)
	if st := lookupTask(prd, c).Status; st != "" && st != TaskPending {
		t.Fatalf("precondition: task C should be pending, got %q", st)
	}
	_, err := m.EditTaskSpec(prd.ID, c, "write docs/other.md", "tester")
	if err == nil || !strings.Contains(err.Error(), "only failed or blocked tasks") {
		t.Fatalf("want pending-sibling rejection on blocked PRD, got %v", err)
	}
	got, _ := m.Store().GetPRD(prd.ID)
	if s := lookupTask(got, c).Spec; s != "write docs/c.md" {
		t.Fatalf("pending sibling spec changed to %q", s)
	}
}

func TestBlockedTaskEdit_EditThenResetRecoversWithoutRerunningCompleted(t *testing.T) {
	m, api, prd, a, b, c := blockedPRDFixture(t)
	const fixed = "write docs/x.md"
	if _, err := m.EditTaskSpec(prd.ID, b, fixed, "tester"); err != nil {
		t.Fatalf("EditTaskSpec: %v", err)
	}
	if _, err := m.ResetTask(prd.ID, b, "tester", false); err != nil {
		t.Fatalf("ResetTask: %v", err)
	}

	rec := &spawnRecorder{}
	verify := func(context.Context, *PRD, *Task) (VerificationResult, error) {
		return VerificationResult{OK: true}, nil
	}
	got := runToTerminalOrBlocked(t, m, api, prd.ID, rec.spawn, verify)

	bReqs := rec.forTask(b)
	if len(bReqs) == 0 {
		t.Fatalf("task B was not spawned after edit+reset; PRD %s, B error %q", got.Status, lookupTask(got, b).Error)
	}
	if !strings.Contains(bReqs[0].Spec, fixed) || strings.Contains(bReqs[0].Spec, "/home/other/elsewhere/tool") {
		t.Fatalf("spawn for B must carry the edited spec, got %q", bReqs[0].Spec)
	}
	if bt := lookupTask(got, b); bt.Status != TaskCompleted || strings.Contains(bt.Error, "scope:") {
		t.Fatalf("task B should complete after recovery, got status %q error %q", bt.Status, bt.Error)
	}
	if n := len(rec.forTask(a)); n != 0 {
		t.Fatalf("completed task A must not re-run, spawned %d times", n)
	}
	if at := lookupTask(got, a); at.Status != TaskCompleted {
		t.Fatalf("task A must stay completed, got %q", at.Status)
	}
	if len(rec.forTask(c)) == 0 {
		t.Fatalf("halted story-2 task C should run once the PRD resumes")
	}
	if got.Status != PRDCompleted {
		t.Fatalf("want PRD completed after recovery, got %s", got.Status)
	}
}
