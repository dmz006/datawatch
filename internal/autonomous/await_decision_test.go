// Parking a task on an open blocking worker question:
//
//   - wait reason + predicate + classifier cause
//   - the lease is released through the capacity ledger; the session is not
//     prompted and the task is not re-admitted
//   - tree lock kept while there is unpushed work, released when clean
//   - no retry counted, no failure recorded
//   - a non-blocking question, a session with no task and an answered
//     question never park
//   - a second park of the same question changes nothing
package autonomous

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/capacity"
	"github.com/dmz006/datawatch/internal/config"
)

// awaitDecisionStore wires a decision store on the manager and returns it.
func awaitDecisionStore(t *testing.T, m *Manager) *QuestionStore {
	t.Helper()
	store, err := NewQuestionStore(filepath.Join(t.TempDir(), "questions.jsonl"))
	if err != nil {
		t.Fatalf("NewQuestionStore: %v", err)
	}
	m.SetDecisionGate(store, func() config.AutonomousQuestionsConfig { return config.AutonomousQuestionsConfig{} })
	return store
}

// raiseTaskQuestion raises a task-linked question and returns its id.
func raiseTaskQuestion(t *testing.T, s *QuestionStore, prdID, taskID string, blocking bool) string {
	t.Helper()
	q := sampleQuestion("Which way?")
	q.Context = QuestionContextTask
	q.PRDID = prdID
	q.TaskID = taskID
	q.Blocking = blocking
	return mustRaise(t, s, q)
}

// awaitDecisionTask makes a running task with a session and a held lease.
func awaitDecisionTask(t *testing.T, m *Manager, led *capacity.Ledger, prd *PRD) (*PRD, *Task) {
	t.Helper()
	cur, _ := m.Store().GetPRD(prd.ID)
	tk := &cur.Story[0].Tasks[0]
	tk.Status = TaskInProgress
	tk.SessionID = "worker-sess"
	tk.RetryCount = 2
	_ = m.Store().SaveTask(tk)
	if led != nil {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := led.Acquire(ctx, capacity.Request{Holder: tk.ID, PRDID: prd.ID, Pools: []string{"node:n1"}}, 0, nil, nil); err != nil {
			t.Fatalf("worker lease acquire: %v", err)
		}
	}
	cur, _ = m.Store().GetPRD(prd.ID)
	return cur, tk
}

func TestAwaitDecision_ParksWithReasonAndReleasesLease(t *testing.T) {
	m, _, led := capFixture(t, 1)
	prd := newPRDWithTasks(t, m, 1)
	cur, tk := awaitDecisionTask(t, m, led, prd)
	audit := &testAudit{}
	m.SetAuditFn(audit.fn)
	continued := 0
	m.SetSessionContinueFn(func(string) error { continued++; return nil })

	if !m.ParkForDecision(cur, tk, "q-7") {
		t.Fatal("ParkForDecision did not park")
	}

	got := testTaskAt(t, m, prd.ID, 0)
	if got.Status != TaskWaitingCapacity {
		t.Errorf("status = %q; want waiting_capacity", got.Status)
	}
	if got.WaitReason != "await_decision: q-7" || !IsAwaitDecisionWaitReason(got.WaitReason) {
		t.Errorf("wait reason = %q; want %q", got.WaitReason, "await_decision: q-7")
	}
	if ClassifyWaitReason(got.WaitReason) != WaitCauseAwaitDecision {
		t.Errorf("cause = %q; want %q", ClassifyWaitReason(got.WaitReason), WaitCauseAwaitDecision)
	}
	// Lease released and NOT re-taken: the task is not re-admitted.
	if led.Held(tk.ID) {
		t.Error("parked task still holds its capacity lease")
	}
	// The session is left alone.
	if continued != 0 {
		t.Errorf("session was prompted %d time(s); a decision park must not prompt it", continued)
	}
	if got.SessionID != "worker-sess" {
		t.Errorf("session id = %q; the session must be kept", got.SessionID)
	}
	// Audit: the existing await-release event, naming the pool and the decision.
	details, ok := audit.find("autonomous_idle_wait_release")
	if !ok {
		t.Fatal("autonomous_idle_wait_release audit event not emitted")
	}
	pools, _ := details["released_pools"].([]string)
	if len(pools) != 1 || pools[0] != "node:n1" {
		t.Errorf("released_pools = %v; want [node:n1]", details["released_pools"])
	}
	if cond, _ := details["condition"].(string); cond != "decision q-7" {
		t.Errorf("condition = %q; want %q", cond, "decision q-7")
	}
}

func TestAwaitDecision_RetryCountUnchangedAndNoFailure(t *testing.T) {
	m, _, led := capFixture(t, 1)
	prd := newPRDWithTasks(t, m, 1)
	cur, tk := awaitDecisionTask(t, m, led, prd)

	m.ParkForDecision(cur, tk, "q-1")

	got := testTaskAt(t, m, prd.ID, 0)
	if got.RetryCount != 2 {
		t.Errorf("RetryCount = %d; want 2 unchanged", got.RetryCount)
	}
	if got.Status == TaskFailed {
		t.Error("park recorded a failure")
	}
}

func TestAwaitDecision_ParksWithoutCapacityLedger(t *testing.T) {
	m, _, _, _ := apiFixture(t)
	prd := newPRDWithTasks(t, m, 1)
	cur, tk := awaitDecisionTask(t, m, nil, prd)

	if !m.ParkForDecision(cur, tk, "q-1") {
		t.Fatal("ParkForDecision did not park without a ledger")
	}
	if got := testTaskAt(t, m, prd.ID, 0); got.WaitReason != "await_decision: q-1" {
		t.Errorf("wait reason = %q", got.WaitReason)
	}
}

func TestAwaitDecision_TreeLockKeptWhileUnpushed(t *testing.T) {
	repo := gitRepo(t, false)
	if err := os.WriteFile(filepath.Join(repo, "wip.txt"), []byte("wip\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m, _, _, _ := apiFixture(t)
	prd, _ := m.CreatePRD("s", repo, "opencode", "", EffortNormal)
	_ = m.Store().SetStories(prd.ID, []Story{{Title: "S", Tasks: []Task{{Title: "T", Spec: "do"}}}})
	cur, tk := awaitDecisionTask(t, m, nil, prd)

	rel, err := m.acquireTreeWritersLock(context.Background(), cur, tk)
	if err != nil {
		t.Fatalf("initial tree lock: %v", err)
	}
	treeRelease := rel
	m.attachTreeRelease(tk.ID, &treeRelease)
	t.Cleanup(func() { m.detachTreeRelease(tk.ID) })
	lockPath := testLockPath(t, repo)
	lockState(t, lockPath, tk.ID)

	audit := &testAudit{}
	m.SetAuditFn(audit.fn)
	m.ParkForDecision(cur, tk, "q-1")

	lockState(t, lockPath, tk.ID) // uncommitted work → lock kept
	if treeRelease == nil {
		t.Error("the executor's release closure was cleared although the lock is kept")
	}
	details, _ := audit.find("autonomous_idle_wait_release")
	if kept, _ := details["tree_lock_kept"].(bool); !kept {
		t.Errorf("tree_lock_kept = false; want true (details=%v)", details)
	}
}

func TestAwaitDecision_TreeLockReleasedWhenClean(t *testing.T) {
	repo := gitRepo(t, true)
	m, _, led := capFixture(t, 1)
	prd := newPRDWithTasks(t, m, 1)
	cur, _ := m.Store().GetPRD(prd.ID)
	cur.Story[0].Tasks[0].ProjectDir = repo
	_ = m.Store().SaveTask(&cur.Story[0].Tasks[0])
	cur, tk := awaitDecisionTask(t, m, led, prd)

	rel, err := m.acquireTreeWritersLock(context.Background(), cur, tk)
	if err != nil {
		t.Fatalf("initial tree lock: %v", err)
	}
	treeRelease := rel
	m.attachTreeRelease(tk.ID, &treeRelease)
	t.Cleanup(func() { m.detachTreeRelease(tk.ID) })
	lockPath := testLockPath(t, repo)
	lockState(t, lockPath, tk.ID)

	audit := &testAudit{}
	m.SetAuditFn(audit.fn)
	m.ParkForDecision(cur, tk, "q-1")

	lockState(t, lockPath, "") // pushed + clean → lock released
	if treeRelease != nil {
		t.Error("the executor's release closure must be cleared once the lock is released")
	}
	details, _ := audit.find("autonomous_idle_wait_release")
	if kept, _ := details["tree_lock_kept"].(bool); kept {
		t.Errorf("tree_lock_kept = true; want false (details=%v)", details)
	}
}

func TestAwaitDecision_SecondParkOfSameQuestionChangesNothing(t *testing.T) {
	m, _, led := capFixture(t, 1)
	prd := newPRDWithTasks(t, m, 1)
	cur, tk := awaitDecisionTask(t, m, led, prd)
	audit := &testAudit{}
	m.SetAuditFn(audit.fn)

	if !m.ParkForDecision(cur, tk, "q-1") {
		t.Fatal("first park did not park")
	}
	first := testTaskAt(t, m, prd.ID, 0)
	events := len(audit.events)

	if m.ParkForDecision(cur, tk, "q-1") {
		t.Error("second park of the same question reported a park")
	}
	second := testTaskAt(t, m, prd.ID, 0)
	if second.Status != first.Status || second.WaitReason != first.WaitReason ||
		second.RetryCount != first.RetryCount || !second.UpdatedAt.Equal(first.UpdatedAt) {
		t.Errorf("task changed by a second park:\nfirst  %+v\nsecond %+v", first, second)
	}
	if len(audit.events) != events {
		t.Errorf("second park emitted %d extra audit event(s)", len(audit.events)-events)
	}
}

func TestAwaitDecision_NeverParksWithoutTaskOrQuestion(t *testing.T) {
	m, _, led := capFixture(t, 1)
	prd := newPRDWithTasks(t, m, 1)
	cur, tk := awaitDecisionTask(t, m, led, prd)
	audit := &testAudit{}
	m.SetAuditFn(audit.fn)

	if m.ParkForDecision(cur, nil, "q-1") {
		t.Error("parked a nil task")
	}
	if m.ParkForDecision(nil, tk, "q-1") {
		t.Error("parked with a nil PRD")
	}
	if m.ParkForDecision(cur, tk, "") {
		t.Error("parked with no question id")
	}
	if got := testTaskAt(t, m, prd.ID, 0); got.Status != TaskInProgress || got.WaitReason != "" {
		t.Errorf("task changed: status=%q reason=%q", got.Status, got.WaitReason)
	}
	if !led.Held(tk.ID) {
		t.Error("lease released although nothing parked")
	}
	if len(audit.events) != 0 {
		t.Errorf("audit events emitted: %d", len(audit.events))
	}
}

func TestAwaitDecision_OnlyOpenBlockingQuestionOfTheTaskMatches(t *testing.T) {
	m, _, _ := capFixture(t, 1)
	store := awaitDecisionStore(t, m)

	// No question at all.
	if _, ok := m.OpenBlockingQuestionForTask("t1"); ok {
		t.Fatal("matched with an empty store")
	}

	// A non-blocking question never parks.
	raiseTaskQuestion(t, store, "p1", "t1", false)
	if _, ok := m.OpenBlockingQuestionForTask("t1"); ok {
		t.Error("a non-blocking question matched")
	}

	// A blocking question of another task does not match.
	raiseTaskQuestion(t, store, "p1", "t2", true)
	if _, ok := m.OpenBlockingQuestionForTask("t1"); ok {
		t.Error("another task's blocking question matched")
	}

	// A session with no task (empty id) never matches, even with a blocking
	// PRD-level question that carries no task id.
	pq := sampleQuestion("PRD level?")
	pq.Context = QuestionContextPrd
	pq.PRDID = "p1"
	pq.Blocking = true
	mustRaise(t, store, pq)
	if _, ok := m.OpenBlockingQuestionForTask(""); ok {
		t.Error("an empty task id matched")
	}

	// The task's own blocking question matches.
	id := raiseTaskQuestion(t, store, "p1", "t1", true)
	q, ok := m.OpenBlockingQuestionForTask("t1")
	if !ok || q.ID != id {
		t.Fatalf("OpenBlockingQuestionForTask = (%q, %v); want (%q, true)", q.ID, ok, id)
	}

	// Once withdrawn it is no longer open, so nothing parks on it.
	if _, err := store.Withdraw(id, "operator", "not needed"); err != nil {
		t.Fatalf("Withdraw: %v", err)
	}
	if _, ok := m.OpenBlockingQuestionForTask("t1"); ok {
		t.Error("a withdrawn question matched")
	}
}

func TestAwaitDecision_NoStoreNeverMatches(t *testing.T) {
	m, _, _ := capFixture(t, 1)
	if _, ok := m.OpenBlockingQuestionForTask("t1"); ok {
		t.Error("matched with no decision store wired")
	}
}
