// Design §6.ii — "await external" tests:
//
//   - marker line parsing (fresh vs stale vs none)
//   - wait-reason format + IsAwaitExternalWaitReason
//   - cycle: lease released, wait reason kept as await_external, audit
//     emitted, re-admitted when the pool frees, NO retry counted
//   - tree-lock keep policy: kept while uncommitted/unpushed, released
//     once pushed + clean, re-claimed on re-admission
//   - boot resume: parked task with a LIVE session is adopted + resumed;
//     a parked task whose session is gone is re-queued
package autonomous

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/capacity"
)

// --- pure parsing ---

func TestAwaitExternal_MarkerParsing(t *testing.T) {
	cases := []struct {
		name      string
		out       string
		wantCond  string
		wantFresh bool
	}{
		{"none", "did some work\nDATAWATCH_COMPLETE: ok", "", false},
		{"fresh", "checking CI\nDATAWATCH_AWAIT_EXTERNAL: CI run #42 on main", "CI run #42 on main", true},
		{"trimmed", "  DATAWATCH_AWAIT_EXTERNAL:   deploy #7  \n", "deploy #7", true},
		{"stale-after-complete", "DATAWATCH_AWAIT_EXTERNAL: CI run #1\nDATAWATCH_COMPLETE: done", "CI run #1", false},
		{"latest-marker-wins", "DATAWATCH_AWAIT_EXTERNAL: first\nDATAWATCH_AWAIT_EXTERNAL: second", "second", true},
		{"empty-condition", "DATAWATCH_AWAIT_EXTERNAL:", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, cond, fresh := ExtractAwaitExternalCondition(tc.out)
			if cond != tc.wantCond || fresh != tc.wantFresh {
				t.Fatalf("ExtractAwaitExternalCondition(%q) = (%q, %v); want (%q, %v)", tc.out, cond, fresh, tc.wantCond, tc.wantFresh)
			}
		})
	}
}

func TestAwaitExternal_WaitReasonFormat(t *testing.T) {
	if got := AwaitExternalWaitReasonFor("CI run #42"); got != "await_external: CI run #42" {
		t.Fatalf("reason format = %q", got)
	}
	if got := AwaitExternalWaitReasonFor("  "); got != "await_external" {
		t.Fatalf("empty condition reason = %q", got)
	}
	long := strings.Repeat("x", 500)
	got := AwaitExternalWaitReasonFor(long)
	if len(got) > len("await_external: ")+maxAwaitExternalConditionChars+3 {
		t.Fatalf("condition not bounded: %d chars", len(got))
	}

	predicates := []struct {
		reason string
		want   bool
	}{
		{"await_external", true},
		{"await_external: CI run #42", true},
		{"await_external — node:n1 at 1/1 (1 waiting)", true},
		{"  await_external: trimmed  ", true},
		{"node:n1 at 1/1 (1 waiting)", false},
		{"tree lock: t1", false},
		{"", false},
	}
	for _, p := range predicates {
		if got := IsAwaitExternalWaitReason(p.reason); got != p.want {
			t.Errorf("IsAwaitExternalWaitReason(%q) = %v; want %v", p.reason, got, p.want)
		}
	}
}

// --- cycle: release / wait / re-admit ---

// testAudit captures audit events.
type testAudit struct {
	mu     sync.Mutex
	events []struct {
		action  string
		details map[string]any
	}
}

func (a *testAudit) fn(action string, details map[string]any) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.events = append(a.events, struct {
		action  string
		details map[string]any
	}{action, details})
}

func (a *testAudit) find(action string) (map[string]any, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for i := len(a.events) - 1; i >= 0; i-- {
		if a.events[i].action == action {
			return a.events[i].details, true
		}
	}
	return nil, false
}

// testTaskSnapshot fetches one task from the store into a plain value.
func testTaskAt(t *testing.T, m *Manager, prdID string, idx int) Task {
	t.Helper()
	cur, ok := m.Store().SnapshotPRD(prdID)
	if !ok {
		t.Fatalf("prd %s vanished", prdID)
	}
	if idx >= len(cur.Story[0].Tasks) {
		t.Fatalf("task %d missing from prd", idx)
	}
	return cur.Story[0].Tasks[idx]
}

// TestAwaitExternal_LiveLeaseReleasedOnCycle is the "leases released on
// await-external" requirement: the worker's capacity lease (the Active
// inference slot) is found, named in the audit event, actually released,
// and re-taken only when re-admitting — without counting a retry.
func TestAwaitExternal_LiveLeaseReleasedOnCycle(t *testing.T) {
	m, _, led := capFixture(t, 1)
	prd := newPRDWithTasks(t, m, 1)
	cur, _ := m.Store().GetPRD(prd.ID)
	tk := &cur.Story[0].Tasks[0]
	tk.Status = TaskInProgress
	tk.SessionID = "worker-sess"
	tk.RetryCount = 0
	_ = m.Store().SaveTask(tk)

	// The worker's lease; it is the only holder on the pool.
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := led.Acquire(ctx, capacity.Request{Holder: tk.ID, PRDID: prd.ID, Pools: []string{"node:n1"}}, 0, nil, nil); err != nil {
		t.Fatalf("worker lease acquire: %v", err)
	}
	if !led.Held(tk.ID) {
		t.Fatal("precondition: worker must hold its lease")
	}

	audit := &testAudit{}
	m.SetAuditFn(audit.fn)

	if err := m.AwaitExternal(context.Background(), cur, tk, "CI run #42"); err != nil {
		t.Fatalf("AwaitExternal: %v", err)
	}

	// The audit event must name the pool the released lease was on.
	details, ok := audit.find("autonomous_idle_wait_release")
	if !ok {
		t.Fatal("autonomous_idle_wait_release audit event not emitted")
	}
	pools, _ := details["released_pools"].([]string)
	if len(pools) != 1 || pools[0] != "node:n1" {
		t.Fatalf("released_pools = %v; want [node:n1]", details["released_pools"])
	}
	if prdID, _ := details["prd"].(string); prdID != prd.ID {
		t.Errorf("audit prd = %q; want %q", prdID, prd.ID)
	}
	if task, _ := details["task"].(string); task != tk.ID {
		t.Errorf("audit task = %q; want %q", task, tk.ID)
	}
	// Re-admitted and releasable: the lease is held again after the cycle.
	if !led.Held(tk.ID) {
		t.Fatal("re-admission did not re-take the lease")
	}
	got := testTaskAt(t, m, prd.ID, 0)
	if got.RetryCount != 0 {
		t.Errorf("cycle counted a retry: RetryCount = %d", got.RetryCount)
	}
}

// TestAwaitExternal_ReAdmitWaitsOnSaturatedPool is the "re-admission waits
// (it does not fail) on a saturated pool" requirement. The pool is held by
// a foreign task, so the cycle cannot re-admit immediately: the task must
// sit in waiting capacity with the await_external reason — and re-admit
// only when the pool frees.
func TestAwaitExternal_ReAdmitWaitsOnSaturatedPool(t *testing.T) {
	m, _, led := capFixture(t, 1)
	// Saturate the pool with a foreign holder.
	octx, ocancel := context.WithTimeout(context.Background(), time.Second)
	other := "other-hold"
	if err := led.Acquire(octx, capacity.Request{Holder: other, PRDID: "other", Pools: []string{"node:n1"}}, 0, nil, nil); err != nil {
		ocancel()
		t.Fatalf("saturate pool: %v", err)
	}
	defer func() {
		ocancel()
		led.Release(other)
	}()

	prd := newPRDWithTasks(t, m, 1)
	cur, _ := m.Store().GetPRD(prd.ID)
	tk := &cur.Story[0].Tasks[0]
	tk.Status = TaskInProgress
	tk.SessionID = "worker-sess"
	tk.RetryCount = 0
	_ = m.Store().SaveTask(tk)

	audit := &testAudit{}
	m.SetAuditFn(audit.fn)

	errCh := make(chan error, 1)
	go func() {
		errCh <- m.AwaitExternal(context.Background(), cur, tk, "CI run #42")
	}()

	// While the pool is saturated: parked, await_external reason, and no
	// lease for this task.
	pollUntil(t, 3*time.Second, "wait reason shows await_external", func() bool {
		got := testTaskAt(t, m, prd.ID, 0)
		return got.Status == TaskWaitingCapacity && IsAwaitExternalWaitReason(got.WaitReason) &&
			strings.Contains(got.WaitReason, "CI run #42")
	})
	if led.Held(tk.ID) {
		t.Fatal("parked task holds a capacity lease on a saturated pool")
	}
	// The cycle must NOT have given up while waiting.
	select {
	case err := <-errCh:
		t.Fatalf("cycle finished early on a saturated pool: %v", err)
	default:
	}

	// Free the pool — the cycle must re-admit without counting a retry or a
	// failure.
	led.Release(other)
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("AwaitExternal: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cycle did not finish after the pool freed")
	}

	got := testTaskAt(t, m, prd.ID, 0)
	if got.RetryCount != 0 {
		t.Fatalf("re-admission counted a retry: RetryCount = %d", got.RetryCount)
	}
	if got.Status != TaskInProgress || got.WaitReason != "" {
		t.Fatalf("after re-admission: status=%q reason=%q; want in-progress, no reason", got.Status, got.WaitReason)
	}
	if led.LeaseOf(tk.ID) == nil {
		t.Fatal("re-admission did not re-take the lease")
	}
	if _, ok := audit.find("autonomous_idle_wait_release"); !ok {
		t.Fatal("audit event missing")
	}
}

// --- tree-lock keep policy ---

// gitRepo makes a local repo with one commit; withRemote adds a bare
// origin and pushes the commit so HEAD is reachable from a
// remote-tracking ref.
func gitRepo(t *testing.T, withRemote bool) string {
	t.Helper()
	run := func(dir string, args ...string) string {
		t.Helper()
		out, err := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@t"}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
		return strings.TrimSpace(string(out))
	}
	dir := t.TempDir()
	run(dir, "init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run(dir, "add", "a.txt")
	run(dir, "commit", "-q", "-m", "one")
	if withRemote {
		bare := t.TempDir()
		run(bare, "init", "-q", "--bare", "-b", "main")
		run(dir, "remote", "add", "origin", bare)
		run(dir, "push", "-q", "origin", "main")
	}
	return dir
}

func lockState(t *testing.T, lockPath, wantHolder string) {
	t.Helper()
	owner := ""
	if rec, err := readLockRecord(lockPath); err == nil {
		owner = rec.HolderTaskID
	}
	if owner != wantHolder {
		t.Fatalf("lock holder = %q; want %q", owner, wantHolder)
	}
}

// TestAwaitExternal_TreeLockKeptWhileUnpushed: the tree has uncommitted
// changes (nothing pushed) — the writer lock must survive the cycle.
func TestAwaitExternal_TreeLockKeptWhileUnpushed(t *testing.T) {
	repo := gitRepo(t, false)
	// Dirty the tree.
	if err := os.WriteFile(filepath.Join(repo, "wip.txt"), []byte("wip\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	m, _, _, _ := apiFixture(t)
	prd, _ := m.CreatePRD("s", repo, "opencode", "", EffortNormal)
	_ = m.Store().SetStories(prd.ID, []Story{{Title: "S", Tasks: []Task{{Title: "T", Spec: "do"}}}})
	cur, _ := m.Store().GetPRD(prd.ID)
	tk := &cur.Story[0].Tasks[0]
	tk.Status = TaskInProgress
	tk.SessionID = "worker-sess"
	_ = m.Store().SaveTask(tk)
	cur, _ = m.Store().GetPRD(prd.ID)

	// The executor's tree lock, registered the same way executor.go does.
	rel, err := m.acquireTreeWritersLock(context.Background(), cur, tk)
	if err != nil {
		t.Fatalf("initial tree lock: %v", err)
	}
	var treeRelease = rel
	m.attachTreeRelease(tk.ID, &treeRelease)
	t.Cleanup(func() { m.detachTreeRelease(tk.ID) })

	lockPath := testLockPath(t, repo)
	lockState(t, lockPath, tk.ID)

	audit := &testAudit{}
	m.SetAuditFn(audit.fn)
	continued := make(chan string, 1)
	m.SetSessionContinueFn(func(sid string) error { continued <- sid; return nil })

	if err := m.AwaitExternal(context.Background(), cur, tk, "CI run #9"); err != nil {
		t.Fatalf("AwaitExternal: %v", err)
	}

	// Unpushed/uncommitted → lock survived.
	lockState(t, lockPath, tk.ID)
	details, ok := audit.find("autonomous_idle_wait_release")
	if !ok {
		t.Fatal("audit event missing")
	}
	if kept, _ := details["tree_lock_kept"].(bool); !kept {
		t.Errorf("tree_lock_kept = false; the tree has uncommitted work so the lock must be kept (details=%v)", details)
	}
	select {
	case sid := <-continued:
		if sid != "worker-sess" {
			t.Errorf("continue prompt to %q; want %q", sid, "worker-sess")
		}
	default:
		t.Error("parked session was not prompted to continue")
	}
}

// TestAwaitExternal_TreeLockReleasedWhenPushedAndClean: with everything
// pushed and the tree clean the lock is released while the cycle waits,
// and re-claimed by the same task on re-admission.
func TestAwaitExternal_TreeLockReleasedWhenPushedAndClean(t *testing.T) {
	repo := gitRepo(t, true)

	m, _, led := capFixture(t, 1)
	octx, ocancel := context.WithTimeout(context.Background(), time.Second)
	if err := led.Acquire(octx, capacity.Request{Holder: "other-hold", PRDID: "other", Pools: []string{"node:n1"}}, 0, nil, nil); err != nil {
		ocancel()
		t.Fatalf("saturate: %v", err)
	}
	defer func() {
		ocancel()
		led.Release("other-hold")
	}()

	prd := newPRDWithTasks(t, m, 1)
	// Point this task at the (pushed+clean) repo.
	cur, _ := m.Store().GetPRD(prd.ID)
	tk := &cur.Story[0].Tasks[0]
	tk.ProjectDir = repo
	tk.Status = TaskInProgress
	tk.SessionID = "worker-sess"
	_ = m.Store().SaveTask(tk)
	cur, _ = m.Store().GetPRD(prd.ID)

	rel, err := m.acquireTreeWritersLock(context.Background(), cur, tk)
	if err != nil {
		t.Fatalf("initial tree lock: %v", err)
	}
	var treeRelease = rel
	m.attachTreeRelease(tk.ID, &treeRelease)
	t.Cleanup(func() { m.detachTreeRelease(tk.ID) })

	lockPath := testLockPath(t, repo)

	audit := &testAudit{}
	m.SetAuditFn(audit.fn)

	errCh := make(chan error, 1)
	go func() {
		errCh <- m.AwaitExternal(context.Background(), cur, tk, "CI run #9")
	}()

	// While parked, the status shows the await_external reason. (The lock
	// release itself is asserted after the cycle: pushed+clean means it was
	// released *before* the wait, and the audit event records it.)
	pollUntil(t, 3*time.Second, "wait reason shows await_external", func() bool {
		got := testTaskAt(t, m, prd.ID, 0)
		return got.Status == TaskWaitingCapacity && IsAwaitExternalWaitReason(got.WaitReason)
	})

	led.Release("other-hold")
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("AwaitExternal: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cycle did not finish after the pool freed")
	}

	// Re-claimed by the same task.
	lockState(t, lockPath, tk.ID)
	details, ok := audit.find("autonomous_idle_wait_release")
	if !ok {
		t.Fatal("audit event missing")
	}
	if kept, _ := details["tree_lock_kept"].(bool); kept {
		t.Errorf("tree_lock_kept = true; pushed+clean tree should release the lock (details=%v)", details)
	}
}

// --- boot resume (design 6.ii: "a task found in the awaiting state after a
// daemon restart is re-admitted, not lost") ---

func TestAwaitExternal_BootResumeReAdoptsParkedTask(t *testing.T) {
	m, _, led := capFixture(t, 2)
	prd := newPRDWithTasks(t, m, 1)
	cur, _ := m.Store().GetPRD(prd.ID)
	tk := &cur.Story[0].Tasks[0]
	tk.Status = TaskWaitingCapacity
	tk.WaitReason = "await_external: CI run #42"
	tk.SessionID = "parked-sess"
	_ = m.Store().SaveTask(tk)
	cur, _ = m.Store().GetPRD(prd.ID)

	continued := make(chan string, 4)
	m.SetSessionContinueFn(func(sid string) error { continued <- sid; return nil })
	m.SetSessionAliveFn(func(id string) bool { return id == "parked-sess" })

	m.resetInProgressTasksForResume(cur)

	got := testTaskAt(t, m, prd.ID, 0)
	if got.Status != TaskInProgress {
		t.Fatalf("parked task with live session: status = %q; want in-progress (re-admitted, not lost)", got.Status)
	}
	if !led.Held(tk.ID) {
		t.Fatal("re-admitted task holds no capacity lease at boot")
	}
	select {
	case sid := <-continued:
		if sid != "parked-sess" {
			t.Errorf("continue prompt to %q; want parked-sess", sid)
		}
	default:
		t.Error("parked live session was not resumed at boot")
	}
	if !got.AdoptedSession {
		t.Error("adopted live task not marked AdoptedSession")
	}
}

func TestAwaitExternal_BootResumeDeadSessionRequeued(t *testing.T) {
	m, _, led := capFixture(t, 2)
	prd := newPRDWithTasks(t, m, 1)
	cur, _ := m.Store().GetPRD(prd.ID)
	tk := &cur.Story[0].Tasks[0]
	tk.Status = TaskWaitingCapacity
	tk.WaitReason = "await_external: CI run #42"
	tk.SessionID = "dead-sess"
	_ = m.Store().SaveTask(tk)
	cur, _ = m.Store().GetPRD(prd.ID)

	continued := make(chan string, 4)
	m.SetSessionContinueFn(func(sid string) error { continued <- sid; return nil })
	m.SetSessionAliveFn(func(id string) bool { return false }) // the session is gone

	m.resetInProgressTasksForResume(cur)

	got := testTaskAt(t, m, prd.ID, 0)
	if got.Status != TaskPending {
		t.Fatalf("dead-session parked task must be re-queued (pending), got %q", got.Status)
	}
	if led.Held(tk.ID) {
		t.Error("dead-session task must not hold a boot lease")
	}
	select {
	case <-continued:
		t.Error("dead session must not be prompted to continue")
	default:
	}
}
