package autonomous

import "testing"

// Restart adoption takes only a task's worker. Verifier, judge and planner
// sessions carry the same task id and run in the same worktree.
func TestRestartAdoption_TakesOnlyWorkerSessions(t *testing.T) {
	dir := t.TempDir()
	prd := &PRD{ID: "p1", ProjectDir: dir, Story: []Story{{ID: "s1", Tasks: []Task{{ID: "t1", PRDID: "p1"}}}}}
	task := &prd.Story[0].Tasks[0]
	sess := func(id, name, taskID, text string) SessionInfo {
		return SessionInfo{ID: id, Name: name, PRDID: "p1", TaskID: taskID, Task: text, ProjectDir: dir, TmuxSession: "tm-" + id, Alive: true}
	}
	others := []SessionInfo{
		sess("v1", "autonomous:verify:t1", "t1", "verify t1"),
		sess("g1", "autonomous:guardrail:t1", "t1", "judge t1"),
		sess("d1", "autonomous:decompose", "t1", "plan t1"),
		// matched by task text and directory, with no task id
		sess("v2", "autonomous:verify:t1", "", "write verify-t1.json"),
	}
	for _, o := range others {
		if got, rest := findAdoptableSession(prd, task, []SessionInfo{o}); got != nil || len(rest) != 0 {
			t.Errorf("%s (%s) was adopted at boot reconcile", o.ID, o.Name)
		}
		if ownsWorktreeAsWorker(prd, task, o, dir) {
			t.Errorf("%s (%s) was adoptable before spawn", o.ID, o.Name)
		}
	}
	worker := sess("w1", "autonomous:task:Parse the input", "t1", "do the task")
	got, rest := findAdoptableSession(prd, task, append(others, worker))
	if got == nil || got.ID != "w1" || len(rest) != 0 {
		t.Errorf("adopted %+v, others %+v; want only the worker", got, rest)
	}
	if !ownsWorktreeAsWorker(prd, task, worker, dir) {
		t.Error("the worker is not adoptable before spawn")
	}
}

// The executor's before-spawn adoption leaves a lone live verifier alone.
func TestAdoptLiveWorkerBeforeSpawn_IgnoresAVerifier(t *testing.T) {
	m, _, _, _ := apiFixture(t)
	dir := t.TempDir()
	prd, err := m.CreatePRD("spec", dir, "opencode", "", EffortNormal)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Store().SetStories(prd.ID, []Story{{Title: "S", Tasks: []Task{{Title: "one", Spec: "x"}}}}); err != nil {
		t.Fatal(err)
	}
	fresh, _ := m.Store().GetPRD(prd.ID)
	task := &fresh.Story[0].Tasks[0]
	m.SetSessionListerFn(func() []SessionInfo {
		return []SessionInfo{{ID: "v1", Name: "autonomous:verify:" + task.ID, PRDID: fresh.ID, TaskID: task.ID,
			ProjectDir: dir, TmuxSession: "tm-v1", Alive: true}}
	})
	if id := m.adoptLiveWorkerBeforeSpawn(fresh, task, 0, dir); id != "" || task.SessionID != "" || task.AdoptedSession {
		t.Errorf("adopted %q (session %q)", id, task.SessionID)
	}
}
