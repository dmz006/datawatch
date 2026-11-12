package autonomous

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/config"
)

const (
	answerBaseSHA = "1111111111111111111111111111111111111111"
	answerHeadSHA = "3333333333333333333333333333333333333333"
	answerRestSHA = "4444444444444444444444444444444444444444"
)

// answerGit is the fake git runner of the answer tests. It records every call,
// reports which paths exist at the base commit for ls-tree, can fail one
// subcommand, and hands everything else to the gate's fake.
type answerGit struct {
	*gateGit
	mu2    sync.Mutex
	log    [][]string
	tree   map[string]bool // paths that exist at the base commit
	head   map[string]bool // paths that exist at HEAD
	failOn string
}

func (g *answerGit) run(ctx context.Context, dir string, args ...string) (string, error) {
	g.mu2.Lock()
	g.log = append(g.log, append([]string(nil), args...))
	failOn, tree := g.failOn, g.tree
	g.mu2.Unlock()
	if len(args) == 0 {
		return "", nil
	}
	if args[0] == failOn {
		return "", errors.New("fake git: " + args[0] + " failed")
	}
	switch args[0] {
	case "ls-tree":
		p := args[len(args)-1]
		if args[2] == "HEAD" {
			tree = g.head
		}
		if tree[p] {
			return p + "\n", nil
		}
		return "", nil
	case "rev-parse":
		if len(args) > 1 && args[1] == "HEAD" {
			return answerHeadSHA + "\n", nil
		}
	case "checkout", "rm":
		return "", nil
	}
	return g.gateGit.run(ctx, dir, args...)
}

func (g *answerGit) calls() []string {
	g.mu2.Lock()
	defer g.mu2.Unlock()
	out := make([]string, len(g.log))
	for i, c := range g.log {
		out[i] = strings.Join(c, " ")
	}
	return out
}

// mutating returns the calls that change a repository.
func (g *answerGit) mutating() []string {
	var out []string
	for _, c := range g.calls() {
		switch strings.Fields(c)[0] {
		case "checkout", "rm", "reset", "commit", "rebase", "revert", "clean", "restore", "add":
			out = append(out, c)
		}
	}
	return out
}

// commitCall is one call to the injected task commit function.
type commitCall struct {
	Dir     string
	Files   []string
	Message string
}

// protectedAnswerRig is a task held for protected paths (fake git, real question
// store) with the store's change hook wired straight to the answer handler.
type protectedAnswerRig struct {
	*uncommittedHoldRig
	git *answerGit

	cmu       sync.Mutex
	commits   []commitCall
	commitErr error
}

// newProtectedAnswerRig holds the task for paths. tree names the paths that exist at
// the base commit.
func newProtectedAnswerRig(t *testing.T, paths []string, tree ...string) *protectedAnswerRig {
	t.Helper()
	r := &protectedAnswerRig{
		uncommittedHoldRig: newUncommittedHoldRig(t, true),
		git:                &answerGit{gateGit: &gateGit{}, tree: map[string]bool{}, head: map[string]bool{}},
	}
	for _, p := range tree {
		r.git.tree[p] = true
	}
	// The task's commit left every held path at HEAD.
	for _, p := range paths {
		r.git.head[p] = true
	}
	r.m.driftGit = r.git.run
	r.m.SetTaskCommit(func(_ context.Context, dir string, files []string, message string) (string, error) {
		r.cmu.Lock()
		defer r.cmu.Unlock()
		r.commits = append(r.commits, commitCall{Dir: dir, Files: append([]string(nil), files...), Message: message})
		if r.commitErr != nil {
			return "", r.commitErr
		}
		return answerRestSHA, nil
	})
	r.m.SetConfig(withProtected(r.m.Config(), nil, config.ProtectedPathsActionHold))
	r.prd.AuthorizedProtectedPaths = []string{"docs/rules.md"}
	r.task.PreTaskSHA = answerBaseSHA
	r.store.SetChangeHook(func(c QuestionChange) { r.m.resolveProtectedPaths(c) })
	if err := r.m.holdForProtectedPaths(r.prd, r.task, paths, answerBaseSHA); err != nil {
		t.Fatalf("holdForProtectedPaths: %v", err)
	}
	if r.task.Status != TaskBlocked || r.task.ProtectedPathHold == nil {
		t.Fatalf("setup: task = %s, hold = %+v; want a held task", r.task.Status, r.task.ProtectedPathHold)
	}
	return r
}

func (r *protectedAnswerRig) commitList() []commitCall {
	r.cmu.Lock()
	defer r.cmu.Unlock()
	return append([]commitCall(nil), r.commits...)
}

// openQuestions returns the open protected-paths questions.
func (r *protectedAnswerRig) openQuestions() []Question {
	var out []Question
	for _, q := range r.store.List(QuestionFilter{}) {
		if q.Label == ProtectedPathsQuestionLabel && q.State == QuestionOpen {
			out = append(out, q)
		}
	}
	return out
}

// answer answers the open question through the store, which runs the handler.
func (r *protectedAnswerRig) answer(t *testing.T, option string) {
	t.Helper()
	qs := r.openQuestions()
	if len(qs) != 1 {
		t.Fatalf("open protected-paths questions = %d; want 1", len(qs))
	}
	if _, err := r.store.Answer(qs[0].ID, Answer{ChosenOptionID: option, Actor: "tester", Surface: "test"}); err != nil {
		t.Fatalf("Answer %s: %v", option, err)
	}
}

// change feeds the handler an answered change directly, so an option or a
// state the store would refuse can be tested.
func (r *protectedAnswerRig) change(label, option string) (string, bool) {
	q := Question{ID: "q-direct", Label: label, PRDID: r.prd.ID, TaskID: r.task.ID}
	if qs := r.openQuestions(); len(qs) == 1 && label == ProtectedPathsQuestionLabel {
		q = qs[0]
	}
	return r.m.resolveProtectedPaths(QuestionChange{
		Kind:     QuestionChangeAnswered,
		Question: q,
		Answer:   &Answer{ChosenOptionID: option, Actor: "tester", Surface: "test"},
	})
}

// reload returns the stored Automaton and the held task.
func (r *protectedAnswerRig) reload(t *testing.T) (*PRD, *Task) {
	t.Helper()
	prd, ok := r.m.Store().GetPRD(r.prd.ID)
	if !ok {
		t.Fatal("Automaton vanished")
	}
	tk, _ := findTaskInPRD(prd, r.task.ID)
	if tk == nil {
		t.Fatal("task vanished")
	}
	return prd, tk
}

func (r *protectedAnswerRig) auditActions() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.acts...)
}

func countAction(actions []string, want string) int {
	n := 0
	for _, a := range actions {
		if a == want {
			n++
		}
	}
	return n
}

func TestProtectedPathsAnswerApproveCompletesTaskAndGateLetsPathsThrough(t *testing.T) {
	r := newProtectedAnswerRig(t, []string{"cmd/ci.yml", "AGENT.md"}, "AGENT.md")
	r.answer(t, ProtectedPathsOptionApprove)

	prd, tk := r.reload(t)
	if tk.Status != TaskCompleted || tk.CompletedAt == nil {
		t.Fatalf("task = %s completed_at=%v; want completed", tk.Status, tk.CompletedAt)
	}
	if tk.PostVerifySHA != answerHeadSHA {
		t.Errorf("PostVerifySHA = %q; want the HEAD sha", tk.PostVerifySHA)
	}
	if tk.ProtectedPathHold != nil || tk.Error != "" {
		t.Errorf("hold = %+v error = %q; want both cleared", tk.ProtectedPathHold, tk.Error)
	}
	if want := []string{"AGENT.md", "cmd/ci.yml"}; !reflect.DeepEqual(tk.ApprovedProtectedPaths, want) {
		t.Errorf("approved paths = %v; want %v (sorted)", tk.ApprovedProtectedPaths, want)
	}
	decs := decisionsOfKind(prd, "protected_paths_approved")
	if len(decs) != 1 || decs[0].Actor != "tester" || !strings.Contains(decs[0].Note, "AGENT.md") || !strings.Contains(decs[0].Note, "cmd/ci.yml") {
		t.Errorf("approved decisions = %+v", decs)
	}
	if countAction(r.auditActions(), "autonomous_protected_paths_answered") != 1 {
		t.Errorf("audit = %v; want one answered event", r.auditActions())
	}
	if len(r.git.mutating()) != 0 || len(r.commitList()) != 0 {
		t.Errorf("approve touched git: %v commits=%v", r.git.mutating(), r.commitList())
	}

	// The gate now lets the same paths through: no hold, no new question.
	r.git.diff = "AGENT.md\ncmd/ci.yml\n"
	held, err := r.m.checkProtectedPaths(context.Background(), prd, tk)
	if err != nil || held {
		t.Fatalf("gate after approval: held=%v err=%v; want it to pass", held, err)
	}
	if n := len(r.store.List(QuestionFilter{})); n != 1 {
		t.Errorf("questions = %d; want only the answered one", n)
	}
}

func TestProtectedPathsAnswerApproveDoesNotTouchAuthorisedPaths(t *testing.T) {
	r := newProtectedAnswerRig(t, []string{"AGENT.md"}, "AGENT.md")
	before := append([]string(nil), r.prd.AuthorizedProtectedPaths...)
	r.answer(t, ProtectedPathsOptionApprove)

	prd, _ := r.reload(t)
	if !reflect.DeepEqual(prd.AuthorizedProtectedPaths, before) {
		t.Errorf("authorised paths = %v; want %v unchanged", prd.AuthorizedProtectedPaths, before)
	}
	// Another task of the Automaton is still not covered by the approval.
	other := &Task{ID: "other", Title: "Other"}
	r.git.diff = "AGENT.md\n"
	held, err := r.m.checkProtectedPaths(context.Background(), prd, other)
	if err != nil || !held {
		t.Errorf("another task: held=%v err=%v; want it held (the approval is per task)", held, err)
	}
}

func TestProtectedPathsAnswerRejectRestoresPathsInNewCommitAndRerunsTask(t *testing.T) {
	r := newProtectedAnswerRig(t, []string{"AGENT.md", ".github/ci.yml"}, "AGENT.md")
	r.task.RetryCount = 1
	r.answer(t, ProtectedPathsOptionReject)

	want := []string{
		"rm -q --ignore-unmatch -- .github/ci.yml",
		"checkout " + answerBaseSHA + " -- AGENT.md",
	}
	if got := r.git.mutating(); !reflect.DeepEqual(got, want) {
		t.Errorf("git commands that change the repository:\n got %v\nwant %v", got, want)
	}
	for _, c := range r.git.calls() {
		if strings.HasPrefix(c, "ls-tree") && !strings.Contains(c, answerBaseSHA) && !strings.Contains(c, " HEAD ") {
			t.Errorf("existence check not against the base commit or HEAD: %s", c)
		}
	}

	commits := r.commitList()
	if len(commits) != 1 {
		t.Fatalf("commits = %+v; want one through the task commit function", commits)
	}
	short := shortID(r.prd.ID)
	if commits[0].Message != "task("+short+"): restore protected paths" {
		t.Errorf("commit message = %q", commits[0].Message)
	}
	if want := []string{".github/ci.yml", "AGENT.md"}; !reflect.DeepEqual(commits[0].Files, want) {
		t.Errorf("committed files = %v; want %v", commits[0].Files, want)
	}
	if commits[0].Dir != r.prd.ProjectDir {
		t.Errorf("commit dir = %q; want the task directory %q", commits[0].Dir, r.prd.ProjectDir)
	}

	prd, tk := r.reload(t)
	if tk.Status != "" && tk.Status != TaskPending {
		t.Errorf("task = %q; want pending for a re-run", tk.Status)
	}
	if tk.ProtectedPathHold != nil {
		t.Errorf("hold = %+v; want cleared", tk.ProtectedPathHold)
	}
	if tk.RetryCount > 1 {
		t.Errorf("RetryCount = %d; the rejection must consume no auto-fix budget", tk.RetryCount)
	}
	if !strings.Contains(tk.PriorAttemptHint, "Do not change those paths") ||
		!strings.Contains(tk.PriorAttemptHint, "AGENT.md") || !strings.Contains(tk.PriorAttemptHint, ".github/ci.yml") {
		t.Errorf("retry hint = %q; want a note not to change the paths", tk.PriorAttemptHint)
	}
	decs := decisionsOfKind(prd, "protected_paths_rejected")
	if len(decs) != 1 || decs[0].Actor != "tester" || strings.Contains(decs[0].Note, "hold stays") {
		t.Errorf("rejected decisions = %+v", decs)
	}
	if countAction(r.auditActions(), "autonomous_protected_paths_answered") != 1 {
		t.Errorf("audit = %v; want one answered event", r.auditActions())
	}
	if len(tk.ApprovedProtectedPaths) != 0 {
		t.Errorf("approved paths = %v; a rejection approves nothing", tk.ApprovedProtectedPaths)
	}
}

func TestProtectedPathsAnswerRejectFailureKeepsHoldAndRaisesOneFreshQuestion(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(r *protectedAnswerRig)
		want  string
	}{
		{"restore fails", func(r *protectedAnswerRig) { r.git.failOn = "checkout" }, "were not restored"},
		{"commit fails", func(r *protectedAnswerRig) { r.commitErr = errors.New("hook said no") }, "could not be committed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newProtectedAnswerRig(t, []string{"AGENT.md"}, "AGENT.md")
			tc.setup(r)
			oldQ := r.task.ProtectedPathHold.QuestionID
			r.answer(t, ProtectedPathsOptionReject)

			prd, tk := r.reload(t)
			if tk.Status != TaskBlocked || tk.ProtectedPathHold == nil {
				t.Fatalf("task = %s hold = %+v; want the hold kept", tk.Status, tk.ProtectedPathHold)
			}
			h := tk.ProtectedPathHold
			if !strings.Contains(h.Reason, tc.want) {
				t.Errorf("hold reason = %q; want it to say %q", h.Reason, tc.want)
			}
			qs := r.openQuestions()
			if len(qs) != 1 || qs[0].ID == oldQ || h.QuestionID != qs[0].ID {
				t.Errorf("open questions = %+v hold question = %q old = %q; want one fresh question the hold names", qs, h.QuestionID, oldQ)
			}
			decs := decisionsOfKind(prd, "protected_paths_rejected")
			if len(decs) != 1 || !strings.Contains(decs[0].Note, "hold stays") {
				t.Errorf("rejected decisions = %+v", decs)
			}
			if len(tk.ApprovedProtectedPaths) != 0 || tk.PriorAttemptHint != "" {
				t.Errorf("failed restore changed the task: approved=%v hint=%q", tk.ApprovedProtectedPaths, tk.PriorAttemptHint)
			}
			if countAction(r.auditActions(), "autonomous_protected_paths_answered") != 1 {
				t.Errorf("audit = %v; want one answered event", r.auditActions())
			}
			warned := false
			r.mu.Lock()
			for _, a := range r.alerts {
				warned = warned || strings.HasPrefix(a, "warn|Protected paths not restored|")
			}
			r.mu.Unlock()
			if !warned {
				t.Errorf("alerts = %v; want a warn alert", r.alerts)
			}
		})
	}
}

func TestProtectedPathsAnswerChangeSpecLeavesTaskBlockedAwaitingEdit(t *testing.T) {
	r := newProtectedAnswerRig(t, []string{"AGENT.md"}, "AGENT.md")
	r.answer(t, ProtectedPathsOptionChangeSpec)

	prd, tk := r.reload(t)
	if tk.Status != TaskBlocked {
		t.Fatalf("task = %s; want blocked", tk.Status)
	}
	if tk.ProtectedPathHold == nil || !tk.ProtectedPathHold.AwaitingSpecChange {
		t.Errorf("hold = %+v; want AwaitingSpecChange", tk.ProtectedPathHold)
	}
	if tk.Error != "protected_path: change the task instructions, then reset the task" {
		t.Errorf("error = %q", tk.Error)
	}
	if n := len(decisionsOfKind(prd, "protected_paths_spec_change_requested")); n != 1 {
		t.Errorf("spec change decisions = %d; want 1", n)
	}
	if len(r.git.mutating()) != 0 || len(r.commitList()) != 0 || len(tk.ApprovedProtectedPaths) != 0 {
		t.Errorf("change_spec touched git or approved paths: %v %v %v", r.git.mutating(), r.commitList(), tk.ApprovedProtectedPaths)
	}
	if countAction(r.auditActions(), "autonomous_protected_paths_answered") != 1 {
		t.Errorf("audit = %v; want one answered event", r.auditActions())
	}

	// The reset the operator does after editing clears the hold.
	if _, err := r.m.ResetTask(prd.ID, tk.ID, "operator", false); err != nil {
		t.Fatalf("ResetTask: %v", err)
	}
	if _, tk = r.reload(t); tk.ProtectedPathHold != nil {
		t.Errorf("hold after reset = %+v; want cleared", tk.ProtectedPathHold)
	}
}

func TestProtectedPathsAnswerForTaskNotHeldIsRecordedNoOp(t *testing.T) {
	for _, option := range []string{ProtectedPathsOptionApprove, ProtectedPathsOptionReject, ProtectedPathsOptionChangeSpec} {
		t.Run(option, func(t *testing.T) {
			r := newProtectedAnswerRig(t, []string{"AGENT.md"}, "AGENT.md")
			// The operator reset the task meanwhile: no longer held.
			if _, err := r.m.ResetTask(r.prd.ID, r.task.ID, "operator", false); err != nil {
				t.Fatalf("ResetTask: %v", err)
			}
			calls := len(r.git.calls())
			prdID, resolved := r.change(ProtectedPathsQuestionLabel, option)
			if prdID != r.prd.ID || resolved {
				t.Fatalf("handler = (%q, %v); want the Automaton id and not resolved", prdID, resolved)
			}
			prd, tk := r.reload(t)
			if tk.Status != "" && tk.Status != TaskPending {
				t.Errorf("task = %q; want it left pending", tk.Status)
			}
			if len(tk.ApprovedProtectedPaths) != 0 || tk.ProtectedPathHold != nil || tk.CompletedAt != nil {
				t.Errorf("no-op changed the task: %+v", tk)
			}
			if n := len(decisionsOfKind(prd, "protected_paths_answer_ignored")); n != 1 {
				t.Errorf("ignored decisions = %d; want 1", n)
			}
			if len(r.git.calls()) != calls || len(r.commitList()) != 0 {
				t.Errorf("no-op ran git: %v commits=%v", r.git.calls()[calls:], r.commitList())
			}
			if countAction(r.auditActions(), "autonomous_protected_paths_answered") != 0 {
				t.Errorf("audit = %v; a no-op is not an answered event", r.auditActions())
			}
		})
	}
}

func TestProtectedPathsAnswerStaleQuestionIsRecordedNoOp(t *testing.T) {
	r := newProtectedAnswerRig(t, []string{"AGENT.md"}, "AGENT.md")
	q := Question{ID: "some-older-question", Label: ProtectedPathsQuestionLabel, PRDID: r.prd.ID, TaskID: r.task.ID}
	prdID, resolved := r.m.resolveProtectedPaths(QuestionChange{
		Kind: QuestionChangeAnswered, Question: q,
		Answer: &Answer{ChosenOptionID: ProtectedPathsOptionApprove, Actor: "tester"},
	})
	if prdID != r.prd.ID || resolved {
		t.Fatalf("handler = (%q, %v)", prdID, resolved)
	}
	prd, tk := r.reload(t)
	if tk.Status != TaskBlocked || tk.ProtectedPathHold == nil || len(tk.ApprovedProtectedPaths) != 0 {
		t.Errorf("stale answer changed the task: %+v", tk)
	}
	if n := len(decisionsOfKind(prd, "protected_paths_answer_ignored")); n != 1 {
		t.Errorf("ignored decisions = %d; want 1", n)
	}
}

func TestProtectedPathsAnswerDifferentQuestionLabelIsIgnored(t *testing.T) {
	r := newProtectedAnswerRig(t, []string{"AGENT.md"}, "AGENT.md")
	decisions := len(r.prd.Decisions)
	prdID, resolved := r.change(UncommittedQuestionLabel, ProtectedPathsOptionApprove)
	if prdID != "" || resolved {
		t.Fatalf("handler = (%q, %v); want it to ignore another label", prdID, resolved)
	}
	prd, tk := r.reload(t)
	if tk.Status != TaskBlocked || tk.ProtectedPathHold == nil || len(prd.Decisions) != decisions {
		t.Errorf("another label changed state: task=%s hold=%+v decisions=%d->%d", tk.Status, tk.ProtectedPathHold, decisions, len(prd.Decisions))
	}
	// Neither does a change that is not an answer.
	if id, _ := r.m.resolveProtectedPaths(QuestionChange{
		Kind:     QuestionChangeWithdrawn,
		Question: Question{ID: "q", Label: ProtectedPathsQuestionLabel, PRDID: r.prd.ID, TaskID: r.task.ID},
	}); id != "" {
		t.Errorf("a withdrawn question was acted on: %q", id)
	}
}

func TestProtectedPathsAnswerUnknownOptionIsRecordedErrorAndChangesNothing(t *testing.T) {
	r := newProtectedAnswerRig(t, []string{"AGENT.md"}, "AGENT.md")
	calls := len(r.git.calls())
	prdID, resolved := r.change(ProtectedPathsQuestionLabel, "bogus")
	if prdID != r.prd.ID || resolved {
		t.Fatalf("handler = (%q, %v)", prdID, resolved)
	}
	prd, tk := r.reload(t)
	if tk.Status != TaskBlocked || tk.ProtectedPathHold == nil || tk.ProtectedPathHold.AwaitingSpecChange ||
		len(tk.ApprovedProtectedPaths) != 0 || tk.CompletedAt != nil {
		t.Errorf("unknown option changed the task: %+v hold=%+v", tk, tk.ProtectedPathHold)
	}
	decs := decisionsOfKind(prd, "protected_paths_answer_invalid")
	if len(decs) != 1 || !strings.Contains(decs[0].Note, "bogus") {
		t.Errorf("invalid decisions = %+v", decs)
	}
	if len(r.git.calls()) != calls || len(r.commitList()) != 0 {
		t.Errorf("unknown option ran git: %v commits=%v", r.git.calls()[calls:], r.commitList())
	}
	if n := len(r.openQuestions()); n != 1 {
		t.Errorf("open questions = %d; the original must stay", n)
	}
}

func TestProtectedPathsAnswerDefaultHoldOptionChangesNothing(t *testing.T) {
	r := newProtectedAnswerRig(t, []string{"AGENT.md"}, "AGENT.md")
	decisions := len(r.prd.Decisions)
	if id, resolved := r.change(ProtectedPathsQuestionLabel, protectedPathsQuestionDefault); id != "" || resolved {
		t.Fatalf("handler = (%q, %v); want the default to keep the hold", id, resolved)
	}
	prd, tk := r.reload(t)
	if tk.Status != TaskBlocked || tk.ProtectedPathHold == nil || len(prd.Decisions) != decisions {
		t.Errorf("default answer changed state")
	}
}

// The answer reaches the handler from the API's question hook, the way the
// daemon wires it.
func TestProtectedPathsAnswerIsRoutedFromTheAPIQuestionHook(t *testing.T) {
	r := newProtectedAnswerRig(t, []string{"AGENT.md"}, "AGENT.md")
	api := NewAPI(r.m)
	drainOnCleanup(t, r.m, api)
	r.store.SetChangeHook(func(c QuestionChange) {
		r.m.RecordQuestionChange(c)
		api.OnQuestionChange(c)
	})
	r.answer(t, ProtectedPathsOptionApprove)
	pollUntil(t, 10*time.Second, "task completed after the approval", func() bool {
		_, tk := r.reload(t)
		return tk.Status == TaskCompleted
	})
	if _, tk := r.reload(t); tk.ProtectedPathHold != nil || !reflect.DeepEqual(tk.ApprovedProtectedPaths, []string{"AGENT.md"}) {
		t.Errorf("task = %+v", tk)
	}
}

// With real git and no injected functions, reject puts an edited path back and
// removes an added one, in a new commit on top of the task's own commit.
func TestProtectedPathsAnswerRejectRealGitRestoresInNewCommit(t *testing.T) {
	repo := t.TempDir()
	run := func(args ...string) string { return snapTestGit(t, repo, args...) }
	write := func(name, body string) {
		t.Helper()
		full := filepath.Join(repo, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	run("init", "-q")
	write("AGENT.md", "rules v1\n")
	write("keep.go", "package keep\n")
	run("add", ".")
	run("commit", "-q", "-m", "base")
	base := run("rev-parse", "HEAD")

	// The task's own commit edits a protected file, adds a protected one and
	// changes an ordinary file.
	write("AGENT.md", "rules v2 by the task\n")
	write(".github/workflows/ci.yml", "name: ci\n")
	write("keep.go", "package keep // edited\n")
	run("add", ".")
	run("commit", "-q", "-m", "task work")
	taskCommit := run("rev-parse", "HEAD")
	// And a further protected file that was never committed.
	write("CLAUDE.md", "untracked rules\n")

	r := newUncommittedHoldRig(t, true)
	r.prd.ProjectDir = repo
	if err := r.m.Store().SavePRD(r.prd); err != nil {
		t.Fatal(err)
	}
	r.store.SetChangeHook(func(c QuestionChange) { r.m.resolveProtectedPaths(c) })
	if err := r.m.holdForProtectedPaths(r.prd, r.task, []string{"AGENT.md", ".github/workflows/ci.yml", "CLAUDE.md"}, base); err != nil {
		t.Fatalf("hold: %v", err)
	}
	var q Question
	for _, c := range r.store.List(QuestionFilter{}) {
		if c.Label == ProtectedPathsQuestionLabel && c.State == QuestionOpen {
			q = c
		}
	}
	if _, err := r.store.Answer(q.ID, Answer{ChosenOptionID: ProtectedPathsOptionReject, Actor: "tester"}); err != nil {
		t.Fatalf("Answer: %v", err)
	}

	prd, _ := r.m.Store().GetPRD(r.prd.ID)
	if tk, _ := findTaskInPRD(prd, r.task.ID); tk.ProtectedPathHold != nil || (tk.Status != "" && tk.Status != TaskPending) {
		t.Fatalf("task = %q hold = %+v; want pending and released (decisions: %+v)", tk.Status, tk.ProtectedPathHold, prd.Decisions)
	}
	if got, _ := os.ReadFile(filepath.Join(repo, "AGENT.md")); string(got) != "rules v1\n" {
		t.Errorf("AGENT.md = %q; want the base content", got)
	}
	for _, gone := range []string{".github/workflows/ci.yml", "CLAUDE.md"} {
		if _, err := os.Stat(filepath.Join(repo, filepath.FromSlash(gone))); !os.IsNotExist(err) {
			t.Errorf("%s still exists (err=%v); want it removed", gone, err)
		}
	}
	if got, _ := os.ReadFile(filepath.Join(repo, "keep.go")); string(got) != "package keep // edited\n" {
		t.Errorf("keep.go = %q; an unlisted path must be left as it was", got)
	}
	if subject := run("log", "-1", "--format=%s"); subject != "task("+shortID(r.prd.ID)+"): restore protected paths" {
		t.Errorf("HEAD subject = %q", subject)
	}
	if parent := run("rev-parse", "HEAD~1"); parent != taskCommit {
		t.Errorf("HEAD~1 = %s; want the task's commit %s (history must not be rewritten)", parent, taskCommit)
	}
	if status := run("status", "--porcelain"); status != "" {
		t.Errorf("worktree not clean after the restore:\n%s", status)
	}
}
