package autonomous

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

// uncommittedRollupRig is a Manager with one Automaton on a work branch whose
// only task is already completed, so Run goes straight to the roll-up, a fake
// git that answers every call through gitCalls, a real question store and
// fakes for the alert and audit hooks.
type uncommittedRollupRig struct {
	m     *Manager
	prd   *PRD
	git   *fakeTaskGit
	store *QuestionStore

	mu       sync.Mutex
	gitCalls int
	acts     []string
	alerts   []string
}

// attachUncommittedRollup seeds the Automaton and wires the fakes into m, and
// leaves every other setting of its config as it was.
func attachUncommittedRollup(t *testing.T, m *Manager, title, policy string, dirty map[string]string) *uncommittedRollupRig {
	t.Helper()
	dir := t.TempDir()
	cfg := m.Config()
	cfg.Automation.UncommittedWorkPolicy = policy
	m.SetConfig(cfg)

	prd, err := m.CreatePRD("test spec", dir, "opencode", "", EffortNormal)
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	if err := m.Store().SetStories(prd.ID, []Story{
		{Title: "Story", Tasks: []Task{{Title: "Task 1", Spec: "do task 1"}}},
	}); err != nil {
		t.Fatalf("SetStories: %v", err)
	}
	prd, _ = m.Store().GetPRD(prd.ID)
	prd.Title = title
	prd.Git = GitWorkflow{Branch: "automaton/" + prd.ID}
	prd.Status = PRDRunning
	prd.Story[0].Tasks[0].Status = TaskCompleted
	if err := m.Store().SavePRD(prd); err != nil {
		t.Fatalf("SavePRD: %v", err)
	}

	r := &uncommittedRollupRig{m: m, prd: prd, git: newFakeTaskGit(dirty)}
	m.driftGit = func(ctx context.Context, d string, args ...string) (string, error) {
		r.mu.Lock()
		r.gitCalls++
		r.mu.Unlock()
		return r.git.run(ctx, d, args...)
	}
	s, err := NewQuestionStore(filepath.Join(t.TempDir(), "questions.jsonl"))
	if err != nil {
		t.Fatalf("NewQuestionStore: %v", err)
	}
	m.SetDecisionGate(s, func() config.AutonomousQuestionsConfig { return config.AutonomousQuestionsConfig{} })
	r.store = s
	m.SetAuditFn(func(action string, _ map[string]any) {
		r.mu.Lock()
		defer r.mu.Unlock()
		r.acts = append(r.acts, action)
	})
	m.SetAlertSink(func(level, alertTitle, body string) string {
		r.mu.Lock()
		defer r.mu.Unlock()
		r.alerts = append(r.alerts, level+"|"+alertTitle+"|"+body)
		return "alert-1"
	})
	return r
}

func newUncommittedRollupRig(t *testing.T, policy string, dirty map[string]string) *uncommittedRollupRig {
	t.Helper()
	m, _ := newFailureEscalationFixture(t, t.TempDir())
	return attachUncommittedRollup(t, m, "Rollup Automaton", policy, dirty)
}

// run drives the Automaton once and reloads it.
func (r *uncommittedRollupRig) run(t *testing.T) *PRD {
	t.Helper()
	if err := r.m.Run(context.Background(), r.prd.ID, fakeSpawn, fakeVerify); err != nil {
		t.Fatalf("Run: %v", err)
	}
	got, ok := r.m.Store().GetPRD(r.prd.ID)
	if !ok {
		t.Fatal("Automaton vanished")
	}
	r.prd = got
	return got
}

func (r *uncommittedRollupRig) calls() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.gitCalls
}

func (r *uncommittedRollupRig) actions() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.acts...)
}

func (r *uncommittedRollupRig) decisions(kind string) []Decision {
	var out []Decision
	for _, d := range r.prd.Decisions {
		if d.Kind == kind {
			out = append(out, d)
		}
	}
	return out
}

func TestUncommittedRollupDirtyWorktreeBlocksCompletion(t *testing.T) {
	r := newUncommittedRollupRig(t, config.UncommittedWorkPolicyHold, dirtyFiles("internal/a.go", "docs/b.md"))

	got := r.run(t)
	if got.Status != PRDBlocked {
		t.Fatalf("status = %s; want blocked", got.Status)
	}
	if got.BlockedReason == "" {
		t.Error("blocked reason is empty")
	}
	if got.UncommittedWork == nil || got.UncommittedWork.Kind != UncommittedHoldKindWorktree {
		t.Fatalf("hold = %+v; want a worktree hold", got.UncommittedWork)
	}
	if calls := r.git.commitCalls(); len(calls) != 0 {
		t.Errorf("the roll-up committed: %+v", calls)
	}
	if len(r.store.List(QuestionFilter{})) != 1 {
		t.Errorf("questions = %d; want 1", len(r.store.List(QuestionFilter{})))
	}

	// The operator cleans the worktree and the hold is cleared; running the
	// Automaton again must pass through the check again and complete.
	before := r.git.statusCount()
	r.git.mu.Lock()
	r.git.dirty = map[string]string{}
	r.git.mu.Unlock()
	r.m.store.WithLock(func() {
		got.UncommittedWork = nil
		got.Status = PRDRunning
	})
	if err := r.m.Store().SavePRD(got); err != nil {
		t.Fatalf("SavePRD: %v", err)
	}
	got = r.run(t)
	if r.git.statusCount() <= before {
		t.Errorf("status calls %d -> %d; the second run skipped the check", before, r.git.statusCount())
	}
	if got.Status != PRDCompleted {
		t.Fatalf("status after the clean re-run = %s; want completed", got.Status)
	}
}

func TestUncommittedRollupCleanWorktreeCompletes(t *testing.T) {
	r := newUncommittedRollupRig(t, config.UncommittedWorkPolicyHold, nil)

	got := r.run(t)
	if got.Status != PRDCompleted {
		t.Fatalf("status = %s; want completed", got.Status)
	}
	if r.git.statusCount() == 0 {
		t.Error("the worktree was never read")
	}
	if got.UncommittedWork != nil || len(r.store.List(QuestionFilter{})) != 0 {
		t.Errorf("a clean worktree raised a hold or question: hold=%+v", got.UncommittedWork)
	}
}

func TestUncommittedRollupWarnCompletes(t *testing.T) {
	r := newUncommittedRollupRig(t, config.UncommittedWorkPolicyWarn, dirtyFiles("internal/a.go"))

	got := r.run(t)
	if got.Status != PRDCompleted {
		t.Fatalf("status = %s; want completed", got.Status)
	}
	if n := len(r.decisions(uncommittedWarnedDecisionKind)); n != 1 {
		t.Errorf("warned decisions = %d; want 1", n)
	}
	if got.UncommittedWork != nil || len(r.store.List(QuestionFilter{})) != 0 {
		t.Errorf("warn raised a hold or question: hold=%+v", got.UncommittedWork)
	}
	if acts := r.actions(); len(acts) != 1 || acts[0] != uncommittedWarnedAuditAction {
		t.Errorf("audit actions = %v; want [%s]", acts, uncommittedWarnedAuditAction)
	}
	if calls := r.git.commitCalls(); len(calls) != 0 {
		t.Errorf("the roll-up committed: %+v", calls)
	}
}

func TestUncommittedRollupOffSkips(t *testing.T) {
	r := newUncommittedRollupRig(t, config.UncommittedWorkPolicyOff, dirtyFiles("internal/a.go"))

	got := r.run(t)
	if got.Status != PRDCompleted {
		t.Fatalf("status = %s; want completed", got.Status)
	}
	if n := r.calls(); n != 0 {
		t.Errorf("git calls = %d; want 0 with the policy off", n)
	}
	if got.UncommittedWork != nil || len(r.decisions(uncommittedWarnedDecisionKind)) != 0 {
		t.Errorf("policy off recorded something: hold=%+v", got.UncommittedWork)
	}
}

func TestUncommittedRollupListsFiles(t *testing.T) {
	paths := []string{"cmd/tool/main.go", "docs/notes.md", "internal/a.go"}
	r := newUncommittedRollupRig(t, config.UncommittedWorkPolicyHold, dirtyFiles(paths...))

	got := r.run(t)
	if got.Status != PRDBlocked {
		t.Fatalf("status = %s; want blocked", got.Status)
	}
	held := r.decisions(uncommittedHeldDecisionKind)
	if len(held) != 1 {
		t.Fatalf("held decisions = %d; want 1", len(held))
	}
	qs := r.store.List(QuestionFilter{})
	if len(qs) != 1 {
		t.Fatalf("questions = %d; want 1", len(qs))
	}
	for _, p := range paths {
		if !strings.Contains(got.BlockedReason, p) {
			t.Errorf("blocked reason lacks %q: %s", p, got.BlockedReason)
		}
		if got.UncommittedWork == nil || !containsString(got.UncommittedWork.Files, p) {
			t.Errorf("hold record lacks %q: %+v", p, got.UncommittedWork)
		}
		if !strings.Contains(held[0].Note, p) {
			t.Errorf("decision lacks %q: %s", p, held[0].Note)
		}
		if !strings.Contains(qs[0].Text, p) {
			t.Errorf("question lacks %q:\n%s", p, qs[0].Text)
		}
	}
	for _, text := range []string{got.BlockedReason, held[0].Note, qs[0].Text} {
		if strings.Contains(text, "PRD") {
			t.Errorf("operator text says PRD: %s", text)
		}
	}
	if qs[0].PRDID != got.ID || qs[0].TaskID != "" {
		t.Errorf("question scope = prd %q task %q; want the Automaton alone", qs[0].PRDID, qs[0].TaskID)
	}
}

func TestUncommittedRollupBlockedAutomatonNotMergeReady(t *testing.T) {
	th, _ := newMergePendingHarness(t, config.MergePendingModeOn, 10)
	// MergeStatus reads through the Manager's coordinator, which the harness
	// does not start.
	th.h.m.mu.Lock()
	th.h.m.coordinator = th.h.c
	th.h.m.mu.Unlock()
	// alpha is completed on its branch and shows up; the dirty Automaton
	// must not.
	r := attachUncommittedRollup(t, th.h.m, "Dirty Automaton", config.UncommittedWorkPolicyHold, dirtyFiles("internal/a.go"))

	got := r.run(t)
	if got.Status != PRDBlocked {
		t.Fatalf("status = %s; want blocked", got.Status)
	}
	th.h.c.triagePass()

	ms := th.h.m.MergeStatus()
	var listed []string
	for _, e := range ms.Automata {
		listed = append(listed, e.AutomatonID)
		if e.AutomatonID == got.ID {
			t.Errorf("blocked Automaton %s is listed as merge ready: %+v", got.ID, e)
		}
	}
	if len(listed) != 1 {
		t.Errorf("merge status lists %v; want only the completed control Automaton", listed)
	}
}

func containsString(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
