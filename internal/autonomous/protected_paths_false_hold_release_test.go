package autonomous

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

// fpfRig builds one Automaton with one pending task in a git repository
// (the task's project directory), plus a real question store, for the
// false-positive protected-paths hold release tests.
func fpfRig(t *testing.T) (*Manager, *PRD, *Task, *QuestionStore, string) {
	t.Helper()
	repo := uwRepo(t)
	m, _ := newFailureEscalationFixture(t, t.TempDir())
	prd, err := m.CreatePRD("test spec", repo, "opencode", "", EffortNormal)
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	if err := m.Store().SetStories(prd.ID, []Story{
		{Title: "Story", Tasks: []Task{{Title: "Task 1", Spec: "do task 1"}}},
	}); err != nil {
		t.Fatalf("SetStories: %v", err)
	}
	prd, _ = m.Store().GetPRD(prd.ID)
	prd.Title = "Dogfood Automaton"
	prd.Status = PRDRunning
	if err := m.Store().SavePRD(prd); err != nil {
		t.Fatalf("SavePRD: %v", err)
	}
	task := &prd.Story[0].Tasks[0]
	s, err := NewQuestionStore(filepath.Join(t.TempDir(), "questions.jsonl"))
	if err != nil {
		t.Fatalf("NewQuestionStore: %v", err)
	}
	m.SetDecisionGate(s, func() config.AutonomousQuestionsConfig { return config.AutonomousQuestionsConfig{} })
	return m, prd, task, s, repo
}

// fpfHold commits AGENT.md, applies the working-tree diff content, and
// holds the task on it with the committed base sha. It returns the hold's
// question id (or "") and the base sha.
func fpfHold(t *testing.T, m *Manager, prd *PRD, task *Task, repo, content string) (string, string) {
	t.Helper()
	uwWrite(t, repo, "AGENT.md", dscBaseRules)
	uwGit(t, repo, "add", "AGENT.md")
	uwGit(t, repo, "commit", "-q", "-m", "rules")
	base := uwGit(t, repo, "rev-parse", "HEAD")
	uwWrite(t, repo, "AGENT.md", content)

	task.Status = TaskInProgress
	if err := m.Store().SaveTask(task); err != nil {
		t.Fatalf("SaveTask: %v", err)
	}
	if err := m.holdForProtectedPaths(prd, task, []string{"AGENT.md"}, base); err != nil {
		t.Fatalf("holdForProtectedPaths: %v", err)
	}
	tk, _ := m.Store().GetTask(task.ID)
	if tk.Status != TaskBlocked || tk.ProtectedPathHold == nil {
		t.Fatalf("task not held: status %s hold %+v", tk.Status, tk.ProtectedPathHold)
	}
	return tk.ProtectedPathHold.QuestionID, base
}

func fpfDecisionKinds(t *testing.T, m *Manager, prd *PRD) []string {
	t.Helper()
	saved, ok := m.Store().GetPRD(prd.ID)
	if !ok {
		t.Fatal("PRD gone")
	}
	kinds := []string{}
	for _, d := range saved.Decisions {
		kinds = append(kinds, d.Kind)
	}
	return kinds
}

func TestReleaseFalseProtectedPathHoldsReleasesDaemonOnlyHold(t *testing.T) {
	m, prd, task, store, repo := fpfRig(t)
	questionID, _ := fpfHold(t, m, prd, task, repo, dscDaemonOnlyRules())
	if questionID == "" {
		t.Fatal("the hold raised no question")
	}

	got := m.ReleaseFalseProtectedPathHolds(context.Background())
	if !sameSet(got, []string{task.ID}) {
		t.Fatalf("released = %v; want %v", got, task.ID)
	}

	tk, _ := m.Store().GetTask(task.ID)
	if normTaskStatus(tk.Status) != TaskPending {
		t.Errorf("status = %q; want pending (back in the queue)", tk.Status)
	}
	if tk.ProtectedPathHold != nil {
		t.Errorf("hold survived the release: %+v", tk.ProtectedPathHold)
	}
	if tk.Error != "" {
		t.Errorf("error survived the release: %q", tk.Error)
	}

	qs := store.List(QuestionFilter{})
	if len(qs) != 1 {
		t.Fatalf("questions = %d; want exactly the one withdrawn hold question", len(qs))
	}
	if qs[0].ID != questionID || qs[0].State != QuestionWithdrawn {
		t.Errorf("question = %q state %q; want %q withdrawn", qs[0].ID, qs[0].State, questionID)
	}

	kinds := fpfDecisionKinds(t, m, prd)
	released, reset := 0, 0
	for _, d := range kinds {
		switch d {
		case protectedPathsFalsePositiveDecisionKind:
			released++
		case "reset_task":
			reset++
		}
	}
	if released != 1 {
		t.Errorf("decisions = %v; want exactly one %s", kinds, protectedPathsFalsePositiveDecisionKind)
	}
	if reset != 1 {
		t.Errorf("decisions = %v; want the reset_task decision too", kinds)
	}
	saved, _ := m.Store().GetPRD(prd.ID)
	var note string
	for _, d := range saved.Decisions {
		if d.Kind == protectedPathsFalsePositiveDecisionKind {
			note = d.Note
		}
	}
	if !strings.Contains(note, task.ID) || !strings.Contains(note, "AGENT.md") {
		t.Errorf("decision note = %q; must name the task and the paths", note)
	}
}

func TestReleaseFalseProtectedPathHoldsKeepsRealEdit(t *testing.T) {
	m, prd, task, store, repo := fpfRig(t)
	questionID, _ := fpfHold(t, m, prd, task, repo, dscRealEditRules())

	got := m.ReleaseFalseProtectedPathHolds(context.Background())
	if len(got) != 0 {
		t.Fatalf("released = %v; a real edit keeps the hold", got)
	}
	tk, _ := m.Store().GetTask(task.ID)
	if tk.Status != TaskBlocked {
		t.Errorf("status = %s; want blocked", tk.Status)
	}
	if tk.ProtectedPathHold == nil || tk.ProtectedPathHold.QuestionID != questionID {
		t.Errorf("hold = %+v; the real edit must keep the hold and its question", tk.ProtectedPathHold)
	}
	qs := store.List(QuestionFilter{})
	if len(qs) != 1 || qs[0].State != QuestionOpen {
		t.Errorf("questions = %+v; the hold question must stay open", qs)
	}
	for _, d := range fpfDecisionKinds(t, m, prd) {
		if d == protectedPathsFalsePositiveDecisionKind {
			t.Errorf("decisions = %v; no false-positive release for a real edit", d)
		}
	}
}

func TestReleaseFalseProtectedPathHoldsLeavesNonHeldTasksAlone(t *testing.T) {
	m, prd, task, store, _ := fpfRig(t)
	snap, _ := m.Store().SnapshotPRD(prd.ID)
	before := len(snap.Decisions)

	got := m.ReleaseFalseProtectedPathHolds(context.Background())
	if len(got) != 0 {
		t.Fatalf("released = %v; nothing was held", got)
	}
	tk, _ := m.Store().GetTask(task.ID)
	if normTaskStatus(tk.Status) != TaskPending || tk.ProtectedPathHold != nil || tk.Error != "" {
		t.Errorf("task = status %q hold %+v error %q; a non-held task must be untouched", tk.Status, tk.ProtectedPathHold, tk.Error)
	}
	if n := len(store.List(QuestionFilter{})); n != 0 {
		t.Errorf("questions = %d; a non-held task raises none", n)
	}
	snap2, _ := m.Store().SnapshotPRD(prd.ID)
	if n := len(snap2.Decisions); n != before {
		t.Errorf("decisions = %d; want %d (unchanged)", n, before)
	}
}

func TestReleaseFalseProtectedPathHoldsIdempotent(t *testing.T) {
	m, prd, task, store, repo := fpfRig(t)
	if got := m.ReleaseFalseProtectedPathHolds(context.Background()); len(got) != 0 {
		t.Fatalf("released = %v; nothing is held yet", got)
	}
	questionID, _ := fpfHold(t, m, prd, task, repo, dscDaemonOnlyRules())
	if got := m.ReleaseFalseProtectedPathHolds(context.Background()); !sameSet(got, []string{task.ID}) {
		t.Fatalf("first sweep released = %v; want %v", got, task.ID)
	}
	// Second sweep: nothing left to release, no errors, no new decisions.
	if got := m.ReleaseFalseProtectedPathHolds(context.Background()); len(got) != 0 {
		t.Fatalf("second sweep released = %v; the sweep must be idempotent", got)
	}
	if got := m.ReleaseFalseProtectedPathHolds(context.Background()); len(got) != 0 {
		t.Fatalf("third sweep released = %v; the sweep must be idempotent", got)
	}
	tk, _ := m.Store().GetTask(task.ID)
	if normTaskStatus(tk.Status) != TaskPending || tk.ProtectedPathHold != nil {
		t.Errorf("task = status %q hold %+v; the second sweep must not change it", tk.Status, tk.ProtectedPathHold)
	}
	qs := store.List(QuestionFilter{})
	if len(qs) != 1 || qs[0].ID != questionID || qs[0].State != QuestionWithdrawn {
		t.Errorf("questions = %+v; the withdrawn question must stay withdrawn", qs)
	}
	saved, _ := m.Store().GetPRD(prd.ID)
	released := 0
	for _, d := range saved.Decisions {
		if d.Kind == protectedPathsFalsePositiveDecisionKind {
			released++
		}
	}
	if released != 1 {
		t.Errorf("release decisions = %d; want exactly one after repeated sweeps", released)
	}
}

func TestReleaseFalseProtectedPathHoldsLeavesBlockActionHoldAlone(t *testing.T) {
	// A protected-paths block (action=block) writes the
	// "protected_path_blocked:" error with no hold record and no stored base
	// commit: the re-check cannot run, and the task stays held.
	m, prd, task, store, repo := fpfRig(t)
	uwWrite(t, repo, "AGENT.md", dscBaseRules)
	uwGit(t, repo, "add", "AGENT.md")
	uwGit(t, repo, "commit", "-q", "-m", "rules")
	task.Status = TaskInProgress
	if err := m.Store().SaveTask(task); err != nil {
		t.Fatalf("SaveTask: %v", err)
	}
	if err := m.blockProtectedPaths(prd, task, []string{"AGENT.md"}, uwGit(t, repo, "rev-parse", "HEAD")); err != nil {
		t.Fatalf("blockProtectedPaths: %v", err)
	}
	tk, _ := m.Store().GetTask(task.ID)
	if tk.Status != TaskBlocked || !strings.HasPrefix(tk.Error, "protected_path_blocked: ") {
		t.Fatalf("task = status %s error %q", tk.Status, tk.Error)
	}

	got := m.ReleaseFalseProtectedPathHolds(context.Background())
	if len(got) != 0 {
		t.Fatalf("released = %v; a block-action hold stores no base commit", got)
	}
	tk, _ = m.Store().GetTask(task.ID)
	if tk.Status != TaskBlocked || tk.Error == "" {
		t.Errorf("task = status %s error %q; the block must stay", tk.Status, tk.Error)
	}
	if n := len(store.List(QuestionFilter{})); n != 0 {
		t.Errorf("questions = %d; a block raises none and the sweep withdraws none", n)
	}
}
