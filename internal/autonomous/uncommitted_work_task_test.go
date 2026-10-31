package autonomous

import (
	"context"
	"errors"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/config"
)

const (
	uncommittedTestHead    = "aaaaaaa1111111"
	uncommittedTestNewHead = "bbbbbbb2222222"
)

// fakeTaskCommitCall is one call the executor made to the task commit.
type fakeTaskCommitCall struct {
	Dir     string
	Files   []string
	Message string
}

// fakeTaskGit stands in for git: a canned `git status` whose dirty set shrinks
// when the fake commit runs, and a HEAD that moves with it.
type fakeTaskGit struct {
	mu          sync.Mutex
	dirty       map[string]string // path -> two-letter porcelain status
	head        string
	statusCalls int
	commits     []fakeTaskCommitCall
	commitErr   error
	commitKeeps bool // the commit succeeds but leaves the files dirty
}

func newFakeTaskGit(dirty map[string]string) *fakeTaskGit {
	return &fakeTaskGit{dirty: dirty, head: uncommittedTestHead}
}

// run is the driftGitRunner: status prints the dirty set, rev-parse the HEAD.
func (g *fakeTaskGit) run(_ context.Context, _ string, args ...string) (string, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if len(args) == 0 {
		return "", nil
	}
	switch args[0] {
	case "status":
		g.statusCalls++
		paths := make([]string, 0, len(g.dirty))
		for p := range g.dirty {
			paths = append(paths, p)
		}
		sort.Strings(paths)
		var b strings.Builder
		for _, p := range paths {
			b.WriteString(g.dirty[p] + " " + p + "\x00")
		}
		return b.String(), nil
	case "rev-parse":
		return g.head + "\n", nil
	}
	return "", nil
}

// commit is the TaskCommitFn: it records the call, then cleans exactly the
// files it was given and moves HEAD.
func (g *fakeTaskGit) commit(_ context.Context, dir string, files []string, message string) (string, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.commits = append(g.commits, fakeTaskCommitCall{Dir: dir, Files: append([]string(nil), files...), Message: message})
	if g.commitErr != nil {
		return "", g.commitErr
	}
	if !g.commitKeeps {
		for _, f := range files {
			delete(g.dirty, f)
		}
	}
	g.head = uncommittedTestNewHead
	return g.head, nil
}

func (g *fakeTaskGit) statusCount() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.statusCalls
}

func (g *fakeTaskGit) commitCalls() []fakeTaskCommitCall {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]fakeTaskCommitCall(nil), g.commits...)
}

// uncommittedTaskRig is a Manager with one Automaton that commits, a fake git
// and a real question store.
type uncommittedTaskRig struct {
	m        *Manager
	prd      *PRD
	git      *fakeTaskGit
	store    *QuestionStore
	dir      string
	spawns   int
	verifies int
}

type uncommittedTaskOpts struct {
	policy       string // "" leaves the default (hold)
	scope        string
	dirty        map[string]string
	commitErr    error
	commitKeeps  bool
	noGit        bool // the Automaton has no git settings
	writeSubdir  string
	filesPlanned []string
	preDirty     []string
}

func newUncommittedTaskRig(t *testing.T, o uncommittedTaskOpts) *uncommittedTaskRig {
	t.Helper()
	dir := t.TempDir()
	m, _ := newFailureEscalationFixture(t, t.TempDir())
	cfg := m.Config()
	cfg.Automation.UncommittedWorkPolicy = o.policy
	cfg.Automation.UncommittedWorkScope = o.scope
	m.SetConfig(cfg)

	prd := newFailureEscalationPRD(t, m, dir, func(p *PRD) {
		if !o.noGit {
			p.Git = GitWorkflow{Branch: "automaton/" + p.ID}
		}
		if o.writeSubdir != "" {
			p.WriteDirs = []string{filepath.Join(dir, o.writeSubdir)}
		}
		p.Story[0].Tasks[0].FilesPlanned = o.filesPlanned
	})

	g := newFakeTaskGit(o.dirty)
	g.commitErr = o.commitErr
	g.commitKeeps = o.commitKeeps
	m.driftGit = g.run
	m.SetTaskCommit(g.commit)

	s, err := NewQuestionStore(filepath.Join(t.TempDir(), "questions.jsonl"))
	if err != nil {
		t.Fatalf("NewQuestionStore: %v", err)
	}
	m.SetDecisionGate(s, func() config.AutonomousQuestionsConfig { return config.AutonomousQuestionsConfig{} })
	return &uncommittedTaskRig{m: m, prd: prd, git: g, store: s, dir: dir}
}

// run drives the Automaton once with a worker that spawns cleanly and a
// verifier that passes, and returns the stored task.
func (r *uncommittedTaskRig) run(t *testing.T, preDirty []string) *Task {
	t.Helper()
	spawn := func(_ context.Context, _ SpawnRequest) (SpawnResult, error) {
		r.spawns++
		return SpawnResult{SessionID: "sess-1", PreTaskSHA: uncommittedTestHead, PreTaskDirtyFiles: preDirty}, nil
	}
	verify := func(_ context.Context, _ *PRD, _ *Task) (VerificationResult, error) {
		r.verifies++
		return VerificationResult{OK: true, Summary: "ok", VerifiedAt: time.Now()}, nil
	}
	if err := r.m.Run(context.Background(), r.prd.ID, spawn, verify); err != nil {
		t.Fatalf("Run: %v", err)
	}
	got, ok := r.m.Store().GetPRD(r.prd.ID)
	if !ok {
		t.Fatal("Automaton vanished")
	}
	r.prd = got
	return &got.Story[0].Tasks[0]
}

func (r *uncommittedTaskRig) questions() []Question {
	return r.store.List(QuestionFilter{})
}

func (r *uncommittedTaskRig) decisions(kind string) []Decision {
	var out []Decision
	for _, d := range r.prd.Decisions {
		if d.Kind == kind {
			out = append(out, d)
		}
	}
	return out
}

func dirtyFiles(paths ...string) map[string]string {
	out := map[string]string{}
	for _, p := range paths {
		out[p] = " M"
	}
	return out
}

func TestUncommittedTaskDirtyAfterVerificationIsHeld(t *testing.T) {
	r := newUncommittedTaskRig(t, uncommittedTaskOpts{
		policy:      config.UncommittedWorkPolicyHold,
		dirty:       dirtyFiles("src/a.go", "src/b.go"),
		commitKeeps: true,
	})
	task := r.run(t, nil)

	if task.Status != TaskBlocked {
		t.Fatalf("status = %s; want blocked", task.Status)
	}
	if task.PostVerifySHA != "" {
		t.Errorf("PostVerifySHA = %q; a held task records none", task.PostVerifySHA)
	}
	if task.CompletedAt != nil {
		t.Errorf("CompletedAt = %v; a held task has not completed", task.CompletedAt)
	}
	if task.UncommittedWork == nil {
		t.Fatal("no uncommitted-work hold recorded on the task")
	}
	if task.UncommittedWork.Reason != UncommittedCauseStillDirty {
		t.Errorf("reason = %q; want %q", task.UncommittedWork.Reason, UncommittedCauseStillDirty)
	}
	if got := strings.Join(task.UncommittedWork.Files, ","); got != "src/a.go,src/b.go" {
		t.Errorf("hold files = %s", got)
	}
	if !strings.HasPrefix(task.Error, "uncommitted_work: ") || !strings.Contains(task.Error, "src/a.go") {
		t.Errorf("error = %q", task.Error)
	}
	qs := r.questions()
	if len(qs) != 1 || qs[0].Label != UncommittedQuestionLabel || qs[0].TaskID != task.ID {
		t.Fatalf("questions = %+v; want one uncommitted_work question for the task", qs)
	}
	if task.UncommittedWork.QuestionID != qs[0].ID {
		t.Errorf("hold names question %q; raised %q", task.UncommittedWork.QuestionID, qs[0].ID)
	}
	// Held, not failed: one spawn, one verification, no failed attempt.
	if r.spawns != 1 || r.verifies != 1 {
		t.Errorf("spawns=%d verifies=%d; a held task must not retry", r.spawns, r.verifies)
	}
	if n := len(task.Attempts); n != 0 {
		t.Errorf("attempts recorded = %d; a held task is not a failed attempt", n)
	}
	calls := r.git.commitCalls()
	if len(calls) != 1 {
		t.Fatalf("commit calls = %d; want the one rescue attempt", len(calls))
	}
	if want := "task(" + shortID(r.prd.ID) + "): Task 1"; calls[0].Message != want {
		t.Errorf("commit message = %q; want %q", calls[0].Message, want)
	}
	if strings.Join(calls[0].Files, ",") != "src/a.go,src/b.go" {
		t.Errorf("committed files = %v", calls[0].Files)
	}
}

func TestUncommittedTaskCleanCompletes(t *testing.T) {
	r := newUncommittedTaskRig(t, uncommittedTaskOpts{dirty: map[string]string{}})
	task := r.run(t, nil)

	if task.Status != TaskCompleted {
		t.Fatalf("status = %s; want completed", task.Status)
	}
	if task.PostVerifySHA != uncommittedTestHead {
		t.Errorf("PostVerifySHA = %q; want %q", task.PostVerifySHA, uncommittedTestHead)
	}
	if n := len(r.git.commitCalls()); n != 0 {
		t.Errorf("commit calls = %d; a clean tree needs no commit", n)
	}
	if qs := r.questions(); len(qs) != 0 {
		t.Errorf("questions = %d; a clean tree raises none", len(qs))
	}
	if r.git.statusCount() == 0 {
		t.Error("the check never read the git status")
	}
}

func TestUncommittedTaskCommitFailureHolds(t *testing.T) {
	r := newUncommittedTaskRig(t, uncommittedTaskOpts{
		dirty:     dirtyFiles("src/a.go"),
		commitErr: errors.New("pre-commit hook rejected the commit"),
	})
	task := r.run(t, nil)

	if task.Status != TaskBlocked {
		t.Fatalf("status = %s; want blocked", task.Status)
	}
	if task.UncommittedWork == nil {
		t.Fatal("no uncommitted-work hold recorded")
	}
	reason := task.UncommittedWork.Reason
	if !strings.Contains(reason, UncommittedCauseCommitFailed) || !strings.Contains(reason, "pre-commit hook rejected the commit") {
		t.Errorf("reason = %q; want the commit error in it", reason)
	}
	if got := strings.Join(task.UncommittedWork.Files, ","); got != "src/a.go" {
		t.Errorf("hold files = %s", got)
	}
	if task.PostVerifySHA != "" {
		t.Errorf("PostVerifySHA = %q; want unset", task.PostVerifySHA)
	}
	qs := r.questions()
	if len(qs) != 1 || !strings.Contains(qs[0].Text, "pre-commit hook rejected the commit") {
		t.Errorf("questions = %+v; want one that names the commit error", qs)
	}
}

func TestUncommittedTaskWarnModeCompletesWithDecision(t *testing.T) {
	r := newUncommittedTaskRig(t, uncommittedTaskOpts{
		policy:      config.UncommittedWorkPolicyWarn,
		dirty:       dirtyFiles("src/a.go"),
		commitKeeps: true,
	})
	task := r.run(t, nil)

	if task.Status != TaskCompleted {
		t.Fatalf("status = %s; warn mode lets the task complete", task.Status)
	}
	if task.PostVerifySHA == "" {
		t.Error("PostVerifySHA unset; a completed task pins its commit")
	}
	if qs := r.questions(); len(qs) != 0 {
		t.Errorf("questions = %d; warn mode asks nothing", len(qs))
	}
	ds := r.decisions(uncommittedWarnedDecisionKind)
	if len(ds) != 1 || !strings.Contains(ds[0].Note, "src/a.go") {
		t.Fatalf("warned decisions = %+v; want one naming src/a.go", ds)
	}
	if len(r.decisions(uncommittedHeldDecisionKind)) != 0 {
		t.Error("warn mode recorded a held decision")
	}
}

func TestUncommittedTaskOffModeSkipsCheck(t *testing.T) {
	r := newUncommittedTaskRig(t, uncommittedTaskOpts{
		policy: config.UncommittedWorkPolicyOff,
		dirty:  dirtyFiles("src/a.go"),
	})
	task := r.run(t, nil)

	if task.Status != TaskCompleted {
		t.Fatalf("status = %s; off mode behaves as before", task.Status)
	}
	// The executor's other git reads (drift, post-verify SHA) never run
	// `git status`, so a zero count proves the check made no git call.
	if n := r.git.statusCount(); n != 0 {
		t.Errorf("git status calls = %d; off mode makes none", n)
	}
	if n := len(r.git.commitCalls()); n != 0 {
		t.Errorf("commit calls = %d; off mode makes none", n)
	}
	if qs := r.questions(); len(qs) != 0 {
		t.Errorf("questions = %d", len(qs))
	}
}

func TestUncommittedTaskOutsideDeclaredScopeIsHeldAndListed(t *testing.T) {
	r := newUncommittedTaskRig(t, uncommittedTaskOpts{
		dirty:        dirtyFiles("src/a.go", "other/stray.go"),
		writeSubdir:  "src",
		filesPlanned: []string{"src/a.go"},
	})
	task := r.run(t, nil)

	calls := r.git.commitCalls()
	if len(calls) != 1 || strings.Join(calls[0].Files, ",") != "src/a.go" {
		t.Fatalf("commit calls = %+v; only the declared file may be committed", calls)
	}
	if task.Status != TaskBlocked {
		t.Fatalf("status = %s; want blocked", task.Status)
	}
	h := task.UncommittedWork
	if h == nil {
		t.Fatal("no hold recorded")
	}
	if strings.Join(h.Files, ",") != "other/stray.go" {
		t.Errorf("hold files = %v; want the stray file only", h.Files)
	}
	if strings.Join(h.OutsideScope, ",") != "other/stray.go" {
		t.Errorf("outside-scope files = %v", h.OutsideScope)
	}
	qs := r.questions()
	if len(qs) != 1 || !strings.Contains(qs[0].Text, "other/stray.go (outside the task's declared files)") {
		t.Fatalf("questions = %+v; want the stray file listed as outside the declared files", qs)
	}
	if _, still := r.git.dirty["other/stray.go"]; !still {
		t.Error("the file outside the declared files was committed")
	}
}

func TestUncommittedTaskCommitRescuesDirtyTask(t *testing.T) {
	r := newUncommittedTaskRig(t, uncommittedTaskOpts{
		dirty: dirtyFiles("src/a.go"),
	})
	task := r.run(t, nil)

	if task.Status != TaskCompleted {
		t.Fatalf("status = %s; want completed after the rescue commit", task.Status)
	}
	if task.PostVerifySHA != uncommittedTestNewHead {
		t.Errorf("PostVerifySHA = %q; want the rescue commit %q", task.PostVerifySHA, uncommittedTestNewHead)
	}
	calls := r.git.commitCalls()
	if len(calls) != 1 {
		t.Fatalf("commit calls = %d; want 1", len(calls))
	}
	if calls[0].Dir != r.dir {
		t.Errorf("commit dir = %q; want %q", calls[0].Dir, r.dir)
	}
	if qs := r.questions(); len(qs) != 0 {
		t.Errorf("questions = %d; a rescued task raises none", len(qs))
	}
	if task.UncommittedWork != nil {
		t.Errorf("hold recorded on a rescued task: %+v", task.UncommittedWork)
	}
}

func TestUncommittedTaskScopeWorktreeCatchesOtherFiles(t *testing.T) {
	dirty := func() map[string]string { return dirtyFiles("src/a.go", "pre.txt") }

	// task_files: pre.txt was dirty before the task started, so it is not the
	// task's to commit and does not hold it.
	tf := newUncommittedTaskRig(t, uncommittedTaskOpts{
		scope:       config.UncommittedWorkScopeTaskFiles,
		dirty:       dirty(),
		writeSubdir: "src",
	})
	task := tf.run(t, []string{"pre.txt"})
	if task.Status != TaskCompleted {
		t.Fatalf("task_files: status = %s; want completed (error %q)", task.Status, task.Error)
	}
	calls := tf.git.commitCalls()
	if len(calls) != 1 || strings.Join(calls[0].Files, ",") != "src/a.go" {
		t.Errorf("task_files: commit calls = %+v; want only src/a.go", calls)
	}

	// worktree: every dirty file counts, and pre.txt lies outside the
	// declared files, so it is listed and holds the task.
	wt := newUncommittedTaskRig(t, uncommittedTaskOpts{
		scope:       config.UncommittedWorkScopeWorktree,
		dirty:       dirty(),
		writeSubdir: "src",
	})
	task = wt.run(t, []string{"pre.txt"})
	if task.Status != TaskBlocked {
		t.Fatalf("worktree: status = %s; want blocked", task.Status)
	}
	if task.UncommittedWork == nil || strings.Join(task.UncommittedWork.Files, ",") != "pre.txt" {
		t.Errorf("worktree: hold = %+v; want pre.txt", task.UncommittedWork)
	}
	calls = wt.git.commitCalls()
	if len(calls) != 1 || strings.Join(calls[0].Files, ",") != "src/a.go" {
		t.Errorf("worktree: commit calls = %+v; want only src/a.go", calls)
	}
}

func TestUncommittedTaskSkippedWhenAutomatonDoesNotCommit(t *testing.T) {
	r := newUncommittedTaskRig(t, uncommittedTaskOpts{
		dirty: dirtyFiles("src/a.go"),
		noGit: true,
	})
	task := r.run(t, nil)

	if task.Status != TaskCompleted {
		t.Fatalf("status = %s; an Automaton that does not commit is not checked", task.Status)
	}
	if n := r.git.statusCount(); n != 0 {
		t.Errorf("git status calls = %d; want 0", n)
	}
	if n := len(r.git.commitCalls()); n != 0 {
		t.Errorf("commit calls = %d; want 0", n)
	}
	if qs := r.questions(); len(qs) != 0 {
		t.Errorf("questions = %d", len(qs))
	}
}
