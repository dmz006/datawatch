// Boot reconcile adoption: a live session that belongs to a stuck task but was
// never linked (or linked to a dead id) is adopted instead of reset.
//
// Run with: env -u TMUX -u TMUX_TMPDIR go test ./internal/autonomous -run TestBootReconcile
package autonomous

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/capacity"
)

// adoptedPoolFixture is a restart with three live tasks of one PRD on the
// pi-prompt backend, whose pool is capped at 2. Every task is in the given
// status and bound to a live session.
func adoptedPoolFixture(t *testing.T, status TaskStatus) (*Manager, *PRD, *capacity.Ledger) {
	t.Helper()
	m, _, _, _ := apiFixture(t)
	led := capacity.New(capacity.Options{PollInterval: 10 * time.Millisecond})
	led.SetLimit("pi-prompt", 2)
	m.SetCapacity(led, func(backend, model string) ([]string, string) { return []string{"host", "pi-prompt"}, "" })
	prd := newPRDWithTasks(t, m, 3)
	alive := map[string]bool{}
	for i := range prd.Story[0].Tasks {
		tk := &prd.Story[0].Tasks[i]
		tk.Status = status
		tk.SessionID = "sess-" + tk.ID
		alive[tk.SessionID] = true
		_ = m.Store().SaveTask(tk)
	}
	m.SetSessionAliveFn(func(id string) bool { return alive[id] })
	prd, _ = m.Store().GetPRD(prd.ID)
	return m, prd, led
}

// wantAdoptedOnce asserts the ledger holds exactly one lease per task of prd,
// keyed by the task ID, and that the pi-prompt pool reads one slot per task.
func wantAdoptedOnce(t *testing.T, prd *PRD, led *capacity.Ledger, step string) {
	t.Helper()
	st := led.Snapshot()
	if len(st.Leases) != 3 {
		t.Fatalf("%s: want 3 leases, got %d: %+v", step, len(st.Leases), st.Leases)
	}
	for _, tk := range prd.Story[0].Tasks {
		if !led.Held(tk.ID) {
			t.Fatalf("%s: task %s holds no lease under its own key: %+v", step, tk.ID, st.Leases)
		}
	}
	for _, p := range st.Pools {
		if p.Name == "pi-prompt" && p.Held != 3 {
			t.Fatalf("%s: pi-prompt held = %d, want 3 (holders %v)", step, p.Held, p.Holders)
		}
	}
}

// Three live tasks adopted over a cap of 2: each holds one lease and is
// counted once, a repeated adoption pass adds nothing, and a new pi-prompt
// admission waits with a reason that names the pool and the holders.
func TestBootResumeAdoptsThreeLiveTasksOverCapOnce(t *testing.T) {
	m, prd, led := adoptedPoolFixture(t, TaskInProgress)

	m.resetInProgressTasksForResume(prd)
	wantAdoptedOnce(t, prd, led, "boot pass")

	m.resetInProgressTasksForResume(prd)
	for i := range prd.Story[0].Tasks {
		m.adoptCapacity(prd, &prd.Story[0].Tasks[i])
	}
	wantAdoptedOnce(t, prd, led, "repeated adoption pass")

	err := led.Acquire(context.Background(), capacity.Request{Holder: "new-task", PRDID: prd.ID, Pools: []string{"host", "pi-prompt"}}, 50*time.Millisecond, nil, nil)
	if !errors.Is(err, capacity.ErrWaitTimeout) || !strings.Contains(err.Error(), "pool pi-prompt full (3/2") {
		t.Fatalf("new admission must wait naming the pool, got %v", err)
	}
	for _, tk := range prd.Story[0].Tasks {
		if !strings.Contains(err.Error(), tk.ID) {
			t.Fatalf("wait reason does not name holder %s: %v", tk.ID, err)
		}
	}
}

// A verify hand-off re-keys each worker lease to verify:<task>. A later
// adoption pass over the same live sessions (a resume of the PRD, or the
// executor's skip-spawn path) must find the hand-off lease and leave the count
// at three instead of adding a second lease under the task key.
func TestAdoptAfterVerifyHandoffKeepsOneLeasePerSession(t *testing.T) {
	m, prd, led := adoptedPoolFixture(t, TaskVerifying)

	m.resetInProgressTasksForResume(prd)
	wantAdoptedOnce(t, prd, led, "boot pass")

	for _, tk := range prd.Story[0].Tasks {
		if !m.HandoffVerifyLease(tk.ID, "pi-prompt", "m") {
			t.Fatalf("verify hand-off of %s failed", tk.ID)
		}
	}
	st := led.Snapshot()
	if len(st.Leases) != 3 {
		t.Fatalf("after hand-off want 3 leases, got %+v", st.Leases)
	}

	m.resetInProgressTasksForResume(prd)
	for i := range prd.Story[0].Tasks {
		m.adoptCapacity(prd, &prd.Story[0].Tasks[i])
	}
	st = led.Snapshot()
	if len(st.Leases) != 3 {
		t.Fatalf("adoption after hand-off added leases: want 3, got %d: %+v", len(st.Leases), st.Leases)
	}
	for _, tk := range prd.Story[0].Tasks {
		if led.Held(tk.ID) {
			t.Fatalf("task %s holds a second lease under its own key beside verify:%s", tk.ID, tk.ID)
		}
		if !led.Held(VerifyHolder(tk.ID)) {
			t.Fatalf("task %s lost its hand-off lease", tk.ID)
		}
	}
}

func adoptReconcileSetup(t *testing.T, sessionID string) (*Manager, *PRD, string) {
	t.Helper()
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	dir := testProjectDir(t)
	prd, err := m.CreatePRD("spec", dir, "", "", "")
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	_ = m.Store().SetStories(prd.ID, []Story{{
		Title: "story",
		Tasks: []Task{{Title: "task", Status: TaskInProgress, SessionID: sessionID}},
	}})
	prd, _ = m.Store().GetPRD(prd.ID)
	prd.Status = PRDRunning
	_ = m.Store().SavePRD(prd)
	prd, _ = m.Store().GetPRD(prd.ID)
	return m, prd, dir
}

func countAdoptDecisions(prd *PRD, sess string) int {
	n := 0
	for _, d := range prd.Decisions {
		if d.Kind == "boot_reconcile" && strings.Contains(d.Note, sess) {
			n++
		}
	}
	return n
}

func TestBootReconcileAdoptsUnlinkedFreshSession(t *testing.T) {
	m, prd, dir := adoptReconcileSetup(t, "")
	task := prd.Story[0].Tasks[0]
	sessions := []SessionInfo{{
		ID: "host-fresh", Task: "autonomous: do work for " + task.ID, ProjectDir: dir,
		CreatedAt: time.Now().Add(-40 * time.Second), Alive: true,
	}}
	m.SetSessionAliveFn(func(id string) bool { return id == "host-fresh" })
	m.SetSessionListerFn(func() []SessionInfo { return sessions })
	m.reconcileStuckTasks()

	got, _ := m.Store().GetPRD(prd.ID)
	tk := got.Story[0].Tasks[0]
	if tk.Status != TaskInProgress || tk.SessionID != "host-fresh" {
		t.Fatalf("not adopted: status=%s session=%q", tk.Status, tk.SessionID)
	}
	if countAdoptDecisions(got, "host-fresh") != 1 {
		t.Fatalf("want one boot_reconcile decision naming the adopted session, got %+v", got.Decisions)
	}
	if len(sessions) != 1 {
		t.Fatalf("session total changed: %d", len(sessions))
	}
}

func TestBootReconcileAdoptsByTaskLinkAndWorktree(t *testing.T) {
	m, prd, dir := adoptReconcileSetup(t, "sess-dead")
	task := prd.Story[0].Tasks[0]
	now := time.Now()
	sessions := []SessionInfo{
		{ID: "linked-old", TaskID: task.ID, PRDID: prd.ID, CreatedAt: now.Add(-10 * time.Minute), Alive: true},
		{ID: "linked-new", TaskID: task.ID, PRDID: prd.ID, CreatedAt: now.Add(-1 * time.Minute), Alive: true},
		{ID: "other-task", TaskID: "someone-else", PRDID: prd.ID, Task: task.ID, ProjectDir: dir, CreatedAt: now, Alive: true},
		{ID: "dead", TaskID: task.ID, PRDID: prd.ID, CreatedAt: now, Alive: false},
	}
	adopted, others := findAdoptableSession(prd, &prd.Story[0].Tasks[0], sessions)
	if adopted == nil || adopted.ID != "linked-new" || len(others) != 1 || others[0].ID != "linked-old" {
		t.Fatalf("adopted=%v others=%v, want linked-new with linked-old left over", adopted, others)
	}
	// worktree fallback: no recorded link, task id in text, same dir
	byText := []SessionInfo{
		{ID: "wrong-dir", Task: task.ID, ProjectDir: dir + "-x", Alive: true},
		{ID: "by-text", Task: "work on " + task.ID, ProjectDir: dir + "/", Alive: true},
	}
	if a, _ := findAdoptableSession(prd, &prd.Story[0].Tasks[0], byText); a == nil || a.ID != "by-text" {
		t.Fatalf("worktree match = %v, want by-text", a)
	}

	m.SetSessionAliveFn(func(id string) bool { return id == "linked-new" || id == "linked-old" })
	m.SetSessionListerFn(func() []SessionInfo { return sessions })
	m.reconcileStuckTasks()
	got, _ := m.Store().GetPRD(prd.ID)
	tk := got.Story[0].Tasks[0]
	if tk.Status != TaskInProgress || tk.SessionID != "linked-new" {
		t.Fatalf("status=%s session=%q, want in-progress on linked-new", tk.Status, tk.SessionID)
	}
}

func TestBootReconcileNoAdoptableSessionSingleReplacement(t *testing.T) {
	m, prd, dir := adoptReconcileSetup(t, "")
	m.SetSessionAliveFn(func(string) bool { return false })
	m.SetSessionListerFn(func() []SessionInfo {
		return []SessionInfo{
			{ID: "unrelated", Task: "something else", ProjectDir: dir, Alive: true},
			{ID: "dead-match", TaskID: prd.Story[0].Tasks[0].ID, Alive: false},
		}
	})
	m.reconcileStuckTasks()
	got, _ := m.Store().GetPRD(prd.ID)
	tk := got.Story[0].Tasks[0]
	if tk.Status != TaskPending || tk.SessionID != "" {
		t.Fatalf("want reset to pending, got status=%s session=%q", tk.Status, tk.SessionID)
	}
}

// claimedPairSetup is adoptReconcileSetup with a second task: task a (the first)
// is unlinked and task b stores boundID as its session. Tests reset or adopt
// for task a while a live worker belongs to task b.
func claimedPairSetup(t *testing.T, boundID string) (*Manager, *PRD, string) {
	t.Helper()
	m, prd, dir := adoptReconcileSetup(t, "")
	_ = m.Store().SetStories(prd.ID, []Story{{
		Title: "story",
		Tasks: []Task{
			{Title: "task a", Status: TaskInProgress},
			{Title: "task b", Status: TaskInProgress, SessionID: boundID},
		},
	}})
	prd, _ = m.Store().GetPRD(prd.ID)
	return m, prd, dir
}

func TestBootReconcileDoesNotAdoptSessionClaimedByAnotherTask(t *testing.T) {
	m, prd, dir := claimedPairSetup(t, "a1b2")
	taskA := prd.Story[0].Tasks[0]
	f := &liveFake{sessions: []SessionInfo{
		// task b's worker under its full lister id; its task text names task a and
		// its worktree matches, so only the claim by task b keeps it from adoption
		{ID: "datawatch-box-a1b2", Task: "autonomous: do work for " + taskA.ID, ProjectDir: dir,
			CreatedAt: time.Now().Add(-40 * time.Second), Alive: true},
	}}
	f.wire(m)
	m.SetSessionAliveFn(shortIDAlive("a1b2"))
	m.reconcileStuckTasks()

	got, _ := m.Store().GetPRD(prd.ID)
	tk := got.Story[0].Tasks[0]
	if tk.SessionID != "" || tk.Status != TaskPending {
		t.Fatalf("task a adopted a claimed session: status=%s session=%q", tk.Status, tk.SessionID)
	}
	if countAdoptDecisions(got, "datawatch-box-a1b2") != 0 {
		t.Fatalf("boot_reconcile decision names the claimed session: %+v", got.Decisions)
	}
	if live := strings.Join(f.liveIDs(), ","); live != "datawatch-box-a1b2" {
		t.Fatalf("live = %q, want task b's worker left running", live)
	}
}
