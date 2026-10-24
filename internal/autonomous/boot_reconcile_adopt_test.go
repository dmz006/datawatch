// Boot reconcile adoption: a live session that belongs to a stuck task but was
// never linked (or linked to a dead id) is adopted instead of reset.
//
// Run with: env -u TMUX -u TMUX_TMPDIR go test ./internal/autonomous -run TestBootReconcile
package autonomous

import (
	"strings"
	"testing"
	"time"
)

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
