package autonomous

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/config"
)

const (
	gateBaseSHA  = "1111111111111111111111111111111111111111"
	gateMergeSHA = "2222222222222222222222222222222222222222"
)

// gateGit is the fake driftGit runner of the gate tests: it answers the
// base-commit resolution and serves canned diff, ls-files and log output.
type gateGit struct {
	mu        sync.Mutex
	calls     [][]string
	diff      string
	untracked string
	logOut    string
	// show serves `git show <ref>`: ref ("<sha>:<path>") to content.
	// A ref it does not know fails, as git does for a path absent at the commit.
	show map[string]string
	// noBase makes every base-commit candidate fail to resolve.
	noBase  bool
	diffErr error
}

func (g *gateGit) run(_ context.Context, _ string, args ...string) (string, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.calls = append(g.calls, append([]string(nil), args...))
	if len(args) == 0 {
		return "", nil
	}
	switch args[0] {
	case "rev-parse":
		if g.noBase {
			return "", errors.New("unknown revision")
		}
		// rev-parse --verify <sha>^{commit} echoes the commit it verified.
		return strings.TrimSuffix(args[len(args)-1], "^{commit}") + "\n", nil
	case "merge-base":
		if g.noBase {
			return "", errors.New("no merge base")
		}
		if len(args) > 1 && args[1] == "--is-ancestor" {
			return "", nil
		}
		return gateMergeSHA + "\n", nil
	case "diff":
		return g.diff, g.diffErr
	case "ls-files":
		return g.untracked, nil
	case "log":
		return g.logOut, nil
	case "show":
		if len(args) < 2 {
			return "", errors.New("show takes no ref")
		}
		if c, ok := g.show[args[1]]; ok {
			return c, nil
		}
		if idx := strings.LastIndex(args[1], ":"); idx > 0 {
			return "", fmt.Errorf("fatal: path '%s' does not exist in %s", args[1][idx+1:], args[1][:idx])
		}
		return "", fmt.Errorf("fatal: ambiguous argument '%s'", args[1])
	case "symbolic-ref":
		return "", errors.New("no origin")
	}
	return "", nil
}

func (g *gateGit) callCount() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return len(g.calls)
}

// diffBase returns the revision the gate diffed against.
func (g *gateGit) diffBase() string {
	g.mu.Lock()
	defer g.mu.Unlock()
	for _, c := range g.calls {
		if c[0] == "diff" {
			return c[len(c)-1]
		}
	}
	return ""
}

// gateRig is a Manager with one Automaton and task in direct mode, a real
// question store and a fake git.
type gateRig struct {
	*uncommittedHoldRig
	git *gateGit
}

func newGateRig(t *testing.T) *gateRig {
	t.Helper()
	r := &gateRig{uncommittedHoldRig: newUncommittedHoldRig(t, true), git: &gateGit{}}
	r.m.driftGit = r.git.run
	r.m.SetConfig(withProtected(r.m.Config(), nil, config.ProtectedPathsActionHold))
	r.task.PreTaskSHA = gateBaseSHA
	return r
}

func (r *gateRig) check(t *testing.T) (bool, error) {
	t.Helper()
	return r.m.checkProtectedPaths(context.Background(), r.prd, r.task)
}

func (r *gateRig) questionCount() int { return len(r.store.List(QuestionFilter{})) }

func (r *gateRig) alertTitles() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.alerts...)
}

func TestProtectedPathsGateHoldsWorkerThatEditsProtectedPath(t *testing.T) {
	r := newGateRig(t)
	r.git.diff = "AGENT.md\ncmd/tool/main.go\n"

	held, err := r.check(t)
	if err != nil || !held {
		t.Fatalf("held=%v err=%v; want held", held, err)
	}
	tk, _ := r.m.Store().GetTask(r.task.ID)
	if tk.Status != TaskBlocked {
		t.Errorf("status = %s; want blocked", tk.Status)
	}
	h := tk.ProtectedPathHold
	if h == nil || h.Action != config.ProtectedPathsActionHold || strings.Join(h.Paths, ",") != "AGENT.md" || h.BaseCommit != gateBaseSHA || h.QuestionID == "" {
		t.Fatalf("hold = %+v", h)
	}
	qs := r.store.List(QuestionFilter{})
	if len(qs) != 1 || qs[0].Label != ProtectedPathsQuestionLabel || !strings.Contains(qs[0].Text, "AGENT.md") {
		t.Fatalf("questions = %+v", qs)
	}
	if strings.Contains(qs[0].Text, "main.go") {
		t.Errorf("question lists an unprotected file:\n%s", qs[0].Text)
	}
	if len(r.decisions("protected_paths_held")) != 1 {
		t.Errorf("held decisions = %+v", r.decisions("protected_paths_held"))
	}
}

func TestProtectedPathsGateHoldsInWorktreeModeFromMergeBase(t *testing.T) {
	r := newGateRig(t)
	r.prd.Git.Branch = "automaton/" + r.prd.ID
	r.task.BaseSHA = ""
	r.task.PreTaskSHA = ""
	r.git.diff = ".github/workflows/ci.yml\n"

	held, err := r.check(t)
	if err != nil || !held {
		t.Fatalf("held=%v err=%v; want held in worktree mode", held, err)
	}
	if got := r.git.diffBase(); got != gateMergeSHA {
		t.Errorf("diffed against %q; want the merge-base %q", got, gateMergeSHA)
	}
	tk, _ := r.m.Store().GetTask(r.task.ID)
	if tk.ProtectedPathHold == nil || tk.ProtectedPathHold.BaseCommit != gateMergeSHA {
		t.Fatalf("hold = %+v", tk.ProtectedPathHold)
	}
	if r.questionCount() != 1 {
		t.Errorf("questions = %d; want 1", r.questionCount())
	}
}

func TestProtectedPathsGateHoldsInDirectModeFromTaskBase(t *testing.T) {
	r := newGateRig(t)
	r.prd.Git.Branch = ""
	r.git.diff = "CODEOWNERS\n"

	held, err := r.check(t)
	if err != nil || !held {
		t.Fatalf("held=%v err=%v; want held in direct mode", held, err)
	}
	if got := r.git.diffBase(); got != gateBaseSHA {
		t.Errorf("diffed against %q; want the task base %q", got, gateBaseSHA)
	}
}

func TestProtectedPathsGateCatchesUncommittedRulesFileInjection(t *testing.T) {
	r := newGateRig(t)
	// Nothing committed; the daemon's rules-file injection is only an edit to
	// a tracked file in the working tree, which git diff <base> reports.
	r.git.diff = "AGENT.md\n"
	r.git.logOut = ""

	held, err := r.check(t)
	if err != nil || !held {
		t.Fatalf("held=%v err=%v; want the uncommitted injection caught", held, err)
	}
	tk, _ := r.m.Store().GetTask(r.task.ID)
	if tk.ProtectedPathHold == nil || strings.Join(tk.ProtectedPathHold.Paths, ",") != "AGENT.md" {
		t.Fatalf("hold = %+v", tk.ProtectedPathHold)
	}
}

func TestProtectedPathsGateCatchesUntrackedNewProtectedFile(t *testing.T) {
	r := newGateRig(t)
	r.git.untracked = "CODEOWNERS\nnotes.txt\n"

	held, err := r.check(t)
	if err != nil || !held {
		t.Fatalf("held=%v err=%v; want the untracked file caught", held, err)
	}
	tk, _ := r.m.Store().GetTask(r.task.ID)
	if tk.ProtectedPathHold == nil || strings.Join(tk.ProtectedPathHold.Paths, ",") != "CODEOWNERS" {
		t.Fatalf("hold = %+v", tk.ProtectedPathHold)
	}
}

func TestProtectedPathsGateCountsRenameAndDeleteForBothPaths(t *testing.T) {
	r := newGateRig(t)
	// --no-renames reports a rename as a delete plus an add.
	r.git.diff = "AGENT.md\nnotes/agent-copy.md\n"
	r.m.SetConfig(withProtected(r.m.Config(), []string{"AGENT.md", "notes/"}, ""))

	if held, err := r.check(t); err != nil || !held {
		t.Fatalf("held=%v err=%v", held, err)
	}
	tk, _ := r.m.Store().GetTask(r.task.ID)
	if tk.ProtectedPathHold == nil || strings.Join(tk.ProtectedPathHold.Paths, ",") != "AGENT.md,notes/agent-copy.md" {
		t.Fatalf("hold = %+v", tk.ProtectedPathHold)
	}
	for _, c := range r.git.calls {
		if c[0] == "diff" && (len(c) < 4 || c[1] != "--name-only" || c[2] != "--no-renames") {
			t.Errorf("diff args = %v", c)
		}
	}
}

func TestProtectedPathsGateAuthorisedPathPassesWithDecision(t *testing.T) {
	r := newGateRig(t)
	r.prd.AuthorizedProtectedPaths = []string{"AGENT.md"}
	r.git.diff = "AGENT.md\n"

	for i := 0; i < 2; i++ {
		held, err := r.check(t)
		if err != nil || held {
			t.Fatalf("held=%v err=%v; an authorised path must pass", held, err)
		}
	}
	if r.questionCount() != 0 {
		t.Errorf("questions = %d; want none", r.questionCount())
	}
	tk, _ := r.m.Store().GetTask(r.task.ID)
	if tk.Status == TaskBlocked || tk.ProtectedPathHold != nil {
		t.Errorf("task = %s hold=%+v; want untouched", tk.Status, tk.ProtectedPathHold)
	}
	decs := r.decisions("protected_paths_authorised")
	if len(decs) != 1 || !strings.Contains(decs[0].Note, "AGENT.md") {
		t.Fatalf("authorised decisions = %+v; want one naming AGENT.md", decs)
	}
}

func TestProtectedPathsGateOperatorApprovedPathPasses(t *testing.T) {
	r := newGateRig(t)
	r.task.ApprovedProtectedPaths = []string{"AGENT.md"}
	r.git.diff = "AGENT.md\n"

	held, err := r.check(t)
	if err != nil || held {
		t.Fatalf("held=%v err=%v; an approved path must pass", held, err)
	}
	if r.questionCount() != 0 || len(r.decisions("protected_paths_held")) != 0 {
		t.Errorf("approved path raised questions=%d held=%d", r.questionCount(), len(r.decisions("protected_paths_held")))
	}
}

func TestProtectedPathsGateApprovalCoversOnlyApprovedPaths(t *testing.T) {
	r := newGateRig(t)
	r.task.ApprovedProtectedPaths = []string{"AGENT.md"}
	r.git.diff = "AGENT.md\nCODEOWNERS\n"

	held, err := r.check(t)
	if err != nil || !held {
		t.Fatalf("held=%v err=%v; the unapproved path must still hold", held, err)
	}
	tk, _ := r.m.Store().GetTask(r.task.ID)
	if tk.ProtectedPathHold == nil || strings.Join(tk.ProtectedPathHold.Paths, ",") != "CODEOWNERS" {
		t.Fatalf("hold = %+v", tk.ProtectedPathHold)
	}
}

func TestProtectedPathsGateWarnModeRecordsAndContinues(t *testing.T) {
	r := newGateRig(t)
	r.m.SetConfig(withProtected(r.m.Config(), nil, config.ProtectedPathsActionWarn))
	r.git.diff = "AGENT.md\n"

	held, err := r.check(t)
	if err != nil || held {
		t.Fatalf("held=%v err=%v; warn must continue", held, err)
	}
	if r.questionCount() != 0 {
		t.Errorf("warn raised %d questions", r.questionCount())
	}
	tk, _ := r.m.Store().GetTask(r.task.ID)
	if tk.Status != TaskInProgress || tk.ProtectedPathHold != nil {
		t.Errorf("task = %s hold=%+v; warn must leave it alone", tk.Status, tk.ProtectedPathHold)
	}
	if len(r.decisions("protected_paths_warned")) != 1 {
		t.Errorf("warned decisions = %+v", r.decisions("protected_paths_warned"))
	}
	if a := r.alertTitles(); len(a) != 1 || !strings.HasPrefix(a[0], "warn|Protected paths warning|") {
		t.Errorf("alerts = %v", a)
	}
}

func TestProtectedPathsGateBlockModeBlocksWithoutQuestion(t *testing.T) {
	r := newGateRig(t)
	r.m.SetConfig(withProtected(r.m.Config(), nil, config.ProtectedPathsActionBlock))
	r.git.diff = "AGENT.md\n"

	held, err := r.check(t)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if held {
		t.Errorf("held = true; block parks the task without a hold")
	}
	if r.questionCount() != 0 {
		t.Errorf("block raised %d questions", r.questionCount())
	}
	tk, _ := r.m.Store().GetTask(r.task.ID)
	if tk.Status != TaskBlocked || !strings.HasPrefix(tk.Error, "protected_path_blocked: ") || !strings.Contains(tk.Error, "AGENT.md") {
		t.Errorf("task = %s error=%q", tk.Status, tk.Error)
	}
	if tk.ProtectedPathHold != nil {
		t.Errorf("block recorded a hold: %+v", tk.ProtectedPathHold)
	}
	if !r.m.protectedPathsBlockedTask(r.task) {
		t.Error("protectedPathsBlockedTask = false for a blocked task")
	}
	if len(r.decisions("protected_paths_blocked")) != 1 {
		t.Errorf("blocked decisions = %+v", r.decisions("protected_paths_blocked"))
	}
}

func TestProtectedPathsGateOffModeMakesNoGitCall(t *testing.T) {
	r := newGateRig(t)
	r.m.SetConfig(withProtected(r.m.Config(), nil, config.ProtectedPathsActionOff))
	r.git.diff = "AGENT.md\n"

	held, err := r.check(t)
	if err != nil || held {
		t.Fatalf("held=%v err=%v; off must pass", held, err)
	}
	if n := r.git.callCount(); n != 0 {
		t.Errorf("off made %d git calls; want 0", n)
	}
	if r.questionCount() != 0 || len(r.alertTitles()) != 0 {
		t.Errorf("off recorded something: questions=%d alerts=%v", r.questionCount(), r.alertTitles())
	}
}

func TestProtectedPathsGateHonoursProjectsOwnProtectedNames(t *testing.T) {
	r := newGateRig(t)
	cfg := withProtected(r.m.Config(), []string{"RULES.md", "ops/"}, "")
	cfg.RulesFile = "RULES.md"
	r.m.SetConfig(cfg)
	r.git.diff = "AGENT.md\nRULES.md\n"

	held, err := r.check(t)
	if err != nil || !held {
		t.Fatalf("held=%v err=%v", held, err)
	}
	tk, _ := r.m.Store().GetTask(r.task.ID)
	if tk.ProtectedPathHold == nil || strings.Join(tk.ProtectedPathHold.Paths, ",") != "RULES.md" {
		t.Fatalf("hold = %+v; want RULES.md only (AGENT.md is not protected here)", tk.ProtectedPathHold)
	}

	// With only AGENT.md changed the project passes.
	r2 := newGateRig(t)
	cfg2 := withProtected(r2.m.Config(), []string{"RULES.md", "ops/"}, "")
	cfg2.RulesFile = "RULES.md"
	r2.m.SetConfig(cfg2)
	r2.git.diff = "AGENT.md\n"
	if held, err := r2.check(t); err != nil || held {
		t.Fatalf("held=%v err=%v; AGENT.md is not protected in this project", held, err)
	}
}

func TestProtectedPathsGateDirectoryEntryCatchesNestedFile(t *testing.T) {
	r := newGateRig(t)
	r.m.SetConfig(withProtected(r.m.Config(), []string{"RULES.md", "ops/"}, ""))
	r.git.diff = "ops/deploy/prod/values.yaml\n"

	held, err := r.check(t)
	if err != nil || !held {
		t.Fatalf("held=%v err=%v; a directory entry must catch a nested file", held, err)
	}
	tk, _ := r.m.Store().GetTask(r.task.ID)
	if tk.ProtectedPathHold == nil || strings.Join(tk.ProtectedPathHold.Paths, ",") != "ops/deploy/prod/values.yaml" {
		t.Fatalf("hold = %+v", tk.ProtectedPathHold)
	}
}

func TestProtectedPathsGateChangeOutsideTheListPasses(t *testing.T) {
	r := newGateRig(t)
	r.git.diff = "internal/autonomous/other.go\ndocs/notes.md\n"
	r.git.untracked = "scratch.txt\n"

	held, err := r.check(t)
	if err != nil || held {
		t.Fatalf("held=%v err=%v; an unprotected change must pass", held, err)
	}
	if r.questionCount() != 0 || len(r.alertTitles()) != 0 {
		t.Errorf("recorded something: questions=%d alerts=%v", r.questionCount(), r.alertTitles())
	}
}

func TestProtectedPathsGateUnresolvedBaseLogsRecordsAndContinues(t *testing.T) {
	r := newGateRig(t)
	r.task.PreTaskSHA = ""
	r.git.noBase = true
	r.git.diff = "AGENT.md\n"

	held, err := r.check(t)
	if err != nil || held {
		t.Fatalf("held=%v err=%v; an unresolved base lets the task continue", held, err)
	}
	decs := r.decisions("protected_paths_check_skipped")
	if len(decs) != 1 || !strings.Contains(decs[0].Note, "base commit") || !strings.Contains(decs[0].Note, r.task.ID) {
		t.Fatalf("skipped decisions = %+v", decs)
	}
	if a := r.alertTitles(); len(a) != 1 || !strings.HasPrefix(a[0], "warn|Protected paths check skipped|") {
		t.Errorf("alerts = %v; want one warn alert", a)
	}
	if r.questionCount() != 0 {
		t.Errorf("a skipped check raised %d questions", r.questionCount())
	}
	for _, c := range r.git.calls {
		if c[0] == "diff" || c[0] == "ls-files" {
			t.Errorf("read the changed set with no base: %v", c)
		}
	}
}

func TestProtectedPathsGateUnresolvedBaseWithoutAlertSinkDoesNotPanic(t *testing.T) {
	r := newGateRig(t)
	r.task.PreTaskSHA = ""
	r.git.noBase = true
	r.m.SetAlertSink(nil)

	if held, err := r.check(t); err != nil || held {
		t.Fatalf("held=%v err=%v", held, err)
	}
	if len(r.decisions("protected_paths_check_skipped")) != 1 {
		t.Errorf("skipped decisions = %+v", r.decisions("protected_paths_check_skipped"))
	}
}

func TestProtectedPathsGateUnreadableChangedSetRecordsSkipped(t *testing.T) {
	r := newGateRig(t)
	r.git.diffErr = errors.New("fatal: bad object")

	held, err := r.check(t)
	if err != nil || held {
		t.Fatalf("held=%v err=%v", held, err)
	}
	decs := r.decisions("protected_paths_check_skipped")
	if len(decs) != 1 || !strings.Contains(decs[0].Note, "changed files") {
		t.Fatalf("skipped decisions = %+v", decs)
	}
}

func TestProtectedPathsGateNoDuplicateQuestionOnRepeat(t *testing.T) {
	r := newGateRig(t)
	r.git.diff = "AGENT.md\n"

	for i := 0; i < 3; i++ {
		held, err := r.check(t)
		if err != nil || !held {
			t.Fatalf("check #%d: held=%v err=%v", i+1, held, err)
		}
	}
	if n := r.questionCount(); n != 1 {
		t.Errorf("questions = %d; want 1", n)
	}
	if n := len(r.decisions("protected_paths_held")); n != 1 {
		t.Errorf("held decisions = %d; want 1", n)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.alerts) != 1 {
		t.Errorf("alerts = %v; want 1", r.alerts)
	}
}

func TestProtectedPathsGateOperatorsOwnDirtyFileIsNotAttributedToTheTask(t *testing.T) {
	r := newGateRig(t)
	r.task.PreTaskDirtyFiles = []string{"AGENT.md"}
	r.task.PreTaskUntrackedFiles = []string{"CODEOWNERS"}
	r.git.diff = "AGENT.md\n"
	r.git.untracked = "CODEOWNERS\n"
	r.git.logOut = "cmd/x.go\n"

	held, err := r.check(t)
	if err != nil || held {
		t.Fatalf("held=%v err=%v; the operator's own pre-existing edits are not the task's", held, err)
	}
	if r.questionCount() != 0 {
		t.Errorf("questions = %d; want none", r.questionCount())
	}
}

func TestProtectedPathsGatePreExistingDirtyFileCommittedByTaskIsCaught(t *testing.T) {
	r := newGateRig(t)
	r.task.PreTaskDirtyFiles = []string{"AGENT.md"}
	r.git.diff = "AGENT.md\n"
	r.git.logOut = "AGENT.md\ncmd/x.go\n"

	held, err := r.check(t)
	if err != nil || !held {
		t.Fatalf("held=%v err=%v; a commit that touched the file makes it the task's", held, err)
	}
}

func TestProtectedPathsGateNeverWritesToGit(t *testing.T) {
	r := newGateRig(t)
	r.git.diff = "AGENT.md\n"
	r.task.PreTaskDirtyFiles = []string{"AGENT.md"}
	r.git.logOut = "AGENT.md\n"
	if _, err := r.check(t); err != nil {
		t.Fatal(err)
	}
	readOnly := map[string]bool{"rev-parse": true, "merge-base": true, "diff": true, "ls-files": true, "log": true, "symbolic-ref": true, "show": true}
	for _, c := range r.git.calls {
		if !readOnly[c[0]] {
			t.Errorf("the gate ran a git command that may write: %v", c)
		}
	}
}

// withProtected returns cfg with the protected-paths list and action set; an
// empty list or action leaves the default.
func withProtected(cfg Config, paths []string, action string) Config {
	cfg.Automation.ProtectedPaths = paths
	cfg.Automation.ProtectedPathsAction = action
	return cfg
}

// The executor-level test: Run drives executeOne in worktree mode with the
// fake git and proves the gate runs there.
func TestProtectedPathsGateRunsFromExecuteOneInWorktreeMode(t *testing.T) {
	r := newUncommittedTaskRig(t, uncommittedTaskOpts{dirty: map[string]string{}})
	r.m.SetConfig(withProtected(r.m.Config(), nil, config.ProtectedPathsActionHold))
	g := &gateGit{diff: "AGENT.md\n"}
	r.m.driftGit = func(ctx context.Context, dir string, args ...string) (string, error) {
		if len(args) > 0 && args[0] == "status" {
			return "", nil // the uncommitted-work check sees a clean tree
		}
		return g.run(ctx, dir, args...)
	}
	if r.prd.Git.Branch == "" {
		t.Fatal("rig is not in worktree mode")
	}

	spawn := func(_ context.Context, _ SpawnRequest) (SpawnResult, error) {
		r.spawns++
		// No PreTaskSHA, no BaseSHA: the gate must fall back to the merge-base.
		return SpawnResult{SessionID: "sess-1"}, nil
	}
	verify := func(_ context.Context, _ *PRD, _ *Task) (VerificationResult, error) {
		r.verifies++
		return VerificationResult{OK: true, Summary: "ok", VerifiedAt: time.Now()}, nil
	}
	if err := r.m.Run(context.Background(), r.prd.ID, spawn, verify); err != nil {
		t.Fatalf("Run: %v", err)
	}
	got, _ := r.m.Store().GetPRD(r.prd.ID)
	r.prd = got
	task := &got.Story[0].Tasks[0]

	if task.Status != TaskBlocked {
		t.Fatalf("status = %s; want blocked by the gate (worktree mode)", task.Status)
	}
	if task.ProtectedPathHold == nil || strings.Join(task.ProtectedPathHold.Paths, ",") != "AGENT.md" {
		t.Fatalf("hold = %+v", task.ProtectedPathHold)
	}
	if task.ProtectedPathHold.BaseCommit != gateMergeSHA {
		t.Errorf("base = %q; want the merge-base %q", task.ProtectedPathHold.BaseCommit, gateMergeSHA)
	}
	if r.verifies != 1 || r.spawns != 1 {
		t.Errorf("spawns=%d verifies=%d; a hold must not retry", r.spawns, r.verifies)
	}
	if n := len(task.Attempts); n != 0 {
		t.Errorf("attempts = %d; a hold is not a failed attempt", n)
	}
	if len(r.questions()) != 1 || r.questions()[0].Label != ProtectedPathsQuestionLabel {
		t.Errorf("questions = %+v", r.questions())
	}
	if task.CompletedAt != nil {
		t.Errorf("a held task must not complete")
	}
}
