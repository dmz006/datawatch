package autonomous

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

// fakeDiscardCall is one call made to the discard function.
type fakeDiscardCall struct {
	Dir       string
	Tracked   []string
	Untracked []string
}

// uncommittedAnswerRig is a held task (fake git, real question store) with the
// store's change hook wired to the Automaton API the way the daemon wires it,
// plus fakes that record alerts, audit events and discards.
type uncommittedAnswerRig struct {
	*uncommittedTaskRig
	api *API

	mu         sync.Mutex
	alerts     []string
	acts       []string
	discards   []fakeDiscardCall
	discardErr error
}

// wireUncommittedAnswer attaches the API, the change hook and the fakes to m.
func wireUncommittedAnswer(t *testing.T, m *Manager, s *QuestionStore) (*API, *uncommittedAnswerRig) {
	t.Helper()
	api := NewAPI(m)
	drainOnCleanup(t, m, api)
	s.SetChangeHook(func(c QuestionChange) {
		m.RecordQuestionChange(c)
		api.OnQuestionChange(c)
	})
	r := &uncommittedAnswerRig{api: api}
	m.SetAlertSink(func(level, title, body string) string {
		r.mu.Lock()
		defer r.mu.Unlock()
		r.alerts = append(r.alerts, level+"|"+title+"|"+body)
		return "alert-1"
	})
	m.SetAuditFn(func(action string, _ map[string]any) {
		r.mu.Lock()
		defer r.mu.Unlock()
		r.acts = append(r.acts, action)
	})
	m.discardFiles = func(_ context.Context, dir string, tracked, untracked []string) error {
		r.mu.Lock()
		defer r.mu.Unlock()
		r.discards = append(r.discards, fakeDiscardCall{Dir: dir, Tracked: tracked, Untracked: untracked})
		return r.discardErr
	}
	return api, r
}

// newUncommittedAnswerRig runs a task to its hold and returns the rig.
func newUncommittedAnswerRig(t *testing.T, o uncommittedTaskOpts) (*uncommittedAnswerRig, *Task) {
	t.Helper()
	base := newUncommittedTaskRig(t, o)
	_, r := wireUncommittedAnswer(t, base.m, base.store)
	r.uncommittedTaskRig = base
	task := base.run(t, nil)
	if task.Status != TaskBlocked || task.UncommittedWork == nil {
		t.Fatalf("setup: task = %s, hold = %+v; want a held task", task.Status, task.UncommittedWork)
	}
	return r, task
}

// openQuestion returns the open uncommitted-work question.
func openUncommittedQuestion(t *testing.T, s *QuestionStore) Question {
	t.Helper()
	for _, q := range s.List(QuestionFilter{}) {
		if q.Label == UncommittedQuestionLabel && q.State == QuestionOpen {
			return q
		}
	}
	t.Fatal("no open uncommitted-work question")
	return Question{}
}

func answerUncommitted(t *testing.T, s *QuestionStore, q Question, option string) {
	t.Helper()
	if _, err := s.Answer(q.ID, Answer{ChosenOptionID: option, Actor: "tester", Surface: "test"}); err != nil {
		t.Fatalf("Answer %s: %v", option, err)
	}
}

func (r *uncommittedAnswerRig) alertList() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.alerts...)
}

func (r *uncommittedAnswerRig) actions() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.acts...)
}

func (r *uncommittedAnswerRig) discardCalls() []fakeDiscardCall {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]fakeDiscardCall(nil), r.discards...)
}

// reload returns the stored Automaton and its only task.
func reloadUncommitted(t *testing.T, m *Manager, id string) (*PRD, *Task) {
	t.Helper()
	prd, ok := m.Store().GetPRD(id)
	if !ok {
		t.Fatal("Automaton vanished")
	}
	return prd, &prd.Story[0].Tasks[0]
}

func decisionsOfKind(prd *PRD, kind string) []Decision {
	var out []Decision
	for _, d := range prd.Decisions {
		if d.Kind == kind {
			out = append(out, d)
		}
	}
	return out
}

func TestUncommittedAnswerCommitCompletesTask(t *testing.T) {
	// ok/a.go is declared, other/b.go is not: the completion check commits a.go
	// and holds on b.go, which is outside the declared files.
	r, held := newUncommittedAnswerRig(t, uncommittedTaskOpts{
		policy:       config.UncommittedWorkPolicyHold,
		dirty:        dirtyFiles("ok/a.go", "other/b.go"),
		filesPlanned: []string{"ok/a.go"},
		writeSubdir:  "ok",
	})
	if got := strings.Join(held.UncommittedWork.Files, ","); got != "other/b.go" {
		t.Fatalf("hold files = %s; want other/b.go", got)
	}
	if got := strings.Join(held.UncommittedWork.OutsideScope, ","); got != "other/b.go" {
		t.Fatalf("hold outside-scope files = %s; want other/b.go", got)
	}
	before := len(r.git.commitCalls())

	answerUncommitted(t, r.store, openUncommittedQuestion(t, r.store), UncommittedOptionCommit)

	calls := r.git.commitCalls()
	if len(calls) != before+1 {
		t.Fatalf("commit calls = %d; want %d", len(calls), before+1)
	}
	last := calls[len(calls)-1]
	if strings.Join(last.Files, ",") != "other/b.go" {
		t.Errorf("committed files = %v; want exactly the held file, outside the declared files or not", last.Files)
	}
	if want := "task(" + shortID(r.prd.ID) + "): Task 1"; last.Message != want {
		t.Errorf("commit message = %q; want %q", last.Message, want)
	}

	prd, task := reloadUncommitted(t, r.m, r.prd.ID)
	if task.Status != TaskCompleted {
		t.Fatalf("task status = %s; want completed", task.Status)
	}
	if task.UncommittedWork != nil {
		t.Errorf("hold still set: %+v", task.UncommittedWork)
	}
	if task.PostVerifySHA != uncommittedTestNewHead {
		t.Errorf("PostVerifySHA = %q; want HEAD %q", task.PostVerifySHA, uncommittedTestNewHead)
	}
	if task.CompletedAt == nil {
		t.Error("CompletedAt is not set")
	}
	if task.Error != "" {
		t.Errorf("error = %q; want it cleared", task.Error)
	}
	if prd.Status != PRDRunning {
		t.Errorf("Automaton status = %s; want running so the roll-up runs", prd.Status)
	}
	resolved := decisionsOfKind(prd, uncommittedResolvedDecisionKind)
	if len(resolved) != 1 || !strings.Contains(resolved[0].Note, "option=commit") {
		t.Errorf("resolved decisions = %+v; want one naming option=commit", resolved)
	}
	if acts := r.actions(); len(acts) == 0 || acts[len(acts)-1] != uncommittedResolvedAuditAction {
		t.Errorf("audit actions = %v; want the last to be %s", acts, uncommittedResolvedAuditAction)
	}
	if n := len(r.discardCalls()); n != 0 {
		t.Errorf("discard calls = %d; a commit answer discards nothing", n)
	}
}

func TestUncommittedAnswerCommitFailureKeepsHold(t *testing.T) {
	r, held := newUncommittedAnswerRig(t, uncommittedTaskOpts{
		policy:       config.UncommittedWorkPolicyHold,
		dirty:        dirtyFiles("ok/a.go", "other/b.go"),
		filesPlanned: []string{"ok/a.go"},
		writeSubdir:  "ok",
	})
	firstQuestion := held.UncommittedWork.QuestionID
	r.git.mu.Lock()
	r.git.commitErr = errors.New("pre-commit hook rejected the commit")
	r.git.mu.Unlock()

	answerUncommitted(t, r.store, openUncommittedQuestion(t, r.store), UncommittedOptionCommit)

	prd, task := reloadUncommitted(t, r.m, r.prd.ID)
	if task.Status != TaskBlocked {
		t.Fatalf("task status = %s; want blocked", task.Status)
	}
	if task.UncommittedWork == nil {
		t.Fatal("the hold was cleared by a failed commit")
	}
	if task.PostVerifySHA != "" || task.CompletedAt != nil {
		t.Errorf("a held task recorded completion: sha=%q completed=%v", task.PostVerifySHA, task.CompletedAt)
	}
	failed := decisionsOfKind(prd, uncommittedCommitFailedDecisionKind)
	if len(failed) != 1 || !strings.Contains(failed[0].Note, "pre-commit hook rejected") {
		t.Errorf("commit-failed decisions = %+v; want one carrying the error", failed)
	}
	if n := len(decisionsOfKind(prd, uncommittedResolvedDecisionKind)); n != 0 {
		t.Errorf("resolved decisions = %d; a failed commit resolves nothing", n)
	}
	alerts := r.alertList()
	if len(alerts) == 0 || !strings.HasPrefix(alerts[len(alerts)-1], "warn|") {
		t.Errorf("alerts = %v; want a warn alert last", alerts)
	}
	if strings.Contains(strings.Join(alerts, " "), "PRD") {
		t.Errorf("alerts name a PRD: %v", alerts)
	}

	// The operator can answer again: a fresh question is open and the hold
	// names it.
	again := openUncommittedQuestion(t, r.store)
	if again.ID == firstQuestion || task.UncommittedWork.QuestionID != again.ID {
		t.Errorf("hold names %q, open question is %q (first was %q)", task.UncommittedWork.QuestionID, again.ID, firstQuestion)
	}
}

func TestUncommittedAnswerDiscardOnlyListedFiles(t *testing.T) {
	repo := snapTestRepo(t)
	write := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(repo, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	read := func(name string) string {
		t.Helper()
		b, err := os.ReadFile(filepath.Join(repo, name))
		if err != nil {
			return "<missing>"
		}
		return string(b)
	}
	// A second tracked file, committed, so there is a modified sibling.
	write("sibling.txt", "sibling original\n")
	snapTestGit(t, repo, "add", "sibling.txt")
	snapTestGit(t, repo, "commit", "-q", "-m", "sibling")

	write("tracked.txt", "edited by the task\n") // listed, tracked
	write("listed-new.txt", "untracked and listed\n")
	write("sibling.txt", "sibling edited by someone else\n") // dirty, not listed
	write("sibling-new.txt", "untracked, not listed\n")      // dirty, not listed

	m, _ := newFailureEscalationFixture(t, t.TempDir())
	s, err := NewQuestionStore(filepath.Join(t.TempDir(), "questions.jsonl"))
	if err != nil {
		t.Fatalf("NewQuestionStore: %v", err)
	}
	m.SetDecisionGate(s, func() config.AutonomousQuestionsConfig { return config.AutonomousQuestionsConfig{} })
	_, r := wireUncommittedAnswer(t, m, s)
	m.discardFiles = nil // the real git work, so the proof is on disk
	prd := newFailureEscalationPRD(t, m, repo, func(p *PRD) { p.Git = GitWorkflow{Branch: "automaton/" + p.ID} })
	task := &prd.Story[0].Tasks[0]
	m.store.WithLock(func() {
		prd.Status = PRDBlocked
		task.Status = TaskInProgress
	})
	if err := m.holdForUncommittedWork(prd, task, []UncommittedFile{
		{Path: "tracked.txt", Status: " M"},
		{Path: "listed-new.txt", Status: "??"},
	}, UncommittedCauseStillDirty); err != nil {
		t.Fatalf("holdForUncommittedWork: %v", err)
	}

	answerUncommitted(t, s, openUncommittedQuestion(t, s), UncommittedOptionDiscard)

	if got := read("tracked.txt"); got != "original\n" {
		t.Errorf("listed tracked file = %q; want it restored to HEAD", got)
	}
	if got := read("listed-new.txt"); got != "<missing>" {
		t.Errorf("listed untracked file = %q; want it deleted", got)
	}
	if got := read("sibling.txt"); got != "sibling edited by someone else\n" {
		t.Errorf("sibling tracked file = %q; a discard must not touch files outside the hold", got)
	}
	if got := read("sibling-new.txt"); got != "untracked, not listed\n" {
		t.Errorf("sibling untracked file = %q; a discard must not touch files outside the hold", got)
	}

	got, gt := reloadUncommitted(t, m, prd.ID)
	if gt.Status != "" && gt.Status != TaskPending {
		t.Errorf("task status = %q; want it reset to pending", gt.Status)
	}
	if gt.UncommittedWork != nil {
		t.Errorf("hold still set: %+v", gt.UncommittedWork)
	}
	if gt.PriorAttemptCount != 1 {
		t.Errorf("PriorAttemptCount = %d; the reset keeps the attempt history", gt.PriorAttemptCount)
	}
	discarded := decisionsOfKind(got, uncommittedDiscardedDecisionKind)
	if len(discarded) != 1 || !strings.Contains(discarded[0].Note, "tracked.txt") || !strings.Contains(discarded[0].Note, "listed-new.txt") ||
		strings.Contains(discarded[0].Note, "sibling") {
		t.Errorf("discarded decisions = %+v; want one listing exactly the two held files", discarded)
	}
	resolved := decisionsOfKind(got, uncommittedResolvedDecisionKind)
	if len(resolved) != 1 || !strings.Contains(resolved[0].Note, "option=discard") {
		t.Errorf("resolved decisions = %+v; want one naming option=discard", resolved)
	}
	if acts := r.actions(); len(acts) == 0 || acts[len(acts)-1] != uncommittedResolvedAuditAction {
		t.Errorf("audit actions = %v; want the last to be %s", acts, uncommittedResolvedAuditAction)
	}

	// A path that does not resolve inside the directory is refused.
	for _, bad := range []string{"../outside.txt", "/etc/passwd", "a/../../outside.txt", ".git/config", ""} {
		if _, err := resolveInsideDir(repo, bad); err == nil {
			t.Errorf("resolveInsideDir(%q) accepted a path outside the task directory", bad)
		}
	}
	if _, err := resolveInsideDir(repo, "sub/dir/file.txt"); err != nil {
		t.Errorf("resolveInsideDir refused a path inside the directory: %v", err)
	}
}

func TestUncommittedAnswerRetryResetsTask(t *testing.T) {
	r, _ := newUncommittedAnswerRig(t, uncommittedTaskOpts{
		policy:      config.UncommittedWorkPolicyHold,
		dirty:       dirtyFiles("src/a.go", "src/b.go"),
		commitKeeps: true,
	})
	before := len(r.git.commitCalls())

	answerUncommitted(t, r.store, openUncommittedQuestion(t, r.store), UncommittedOptionRetry)

	prd, task := reloadUncommitted(t, r.m, r.prd.ID)
	if task.Status != "" && task.Status != TaskPending {
		t.Fatalf("task status = %q; want pending", task.Status)
	}
	if task.UncommittedWork != nil {
		t.Errorf("hold still set: %+v", task.UncommittedWork)
	}
	if task.PriorAttemptCount != 1 {
		t.Errorf("PriorAttemptCount = %d; the reset keeps the attempt history", task.PriorAttemptCount)
	}
	if prd.Status != PRDRunning {
		t.Errorf("Automaton status = %s; want running so the task runs again", prd.Status)
	}
	// No file was touched: nothing committed, nothing discarded, files dirty.
	if n := len(r.git.commitCalls()); n != before {
		t.Errorf("commit calls %d -> %d; retry commits nothing", before, n)
	}
	if n := len(r.discardCalls()); n != 0 {
		t.Errorf("discard calls = %d; retry discards nothing", n)
	}
	r.git.mu.Lock()
	dirty := len(r.git.dirty)
	r.git.mu.Unlock()
	if dirty != 2 {
		t.Errorf("dirty files = %d; retry leaves the worktree as it was", dirty)
	}
	resolved := decisionsOfKind(prd, uncommittedResolvedDecisionKind)
	if len(resolved) != 1 || !strings.Contains(resolved[0].Note, "option=retry") {
		t.Errorf("resolved decisions = %+v; want one naming option=retry", resolved)
	}
	if acts := r.actions(); len(acts) == 0 || acts[len(acts)-1] != uncommittedResolvedAuditAction {
		t.Errorf("audit actions = %v; want the last to be %s", acts, uncommittedResolvedAuditAction)
	}
}

func TestUncommittedAnswerWorktreeCommitUnblocksAutomaton(t *testing.T) {
	m, _ := newFailureEscalationFixture(t, t.TempDir())
	rig := attachUncommittedRollup(t, m, "Worktree Automaton", config.UncommittedWorkPolicyHold, dirtyFiles("internal/a.go", "docs/b.md"))
	m.SetTaskCommit(rig.git.commit)

	blocked := rig.run(t)
	if blocked.Status != PRDBlocked || blocked.UncommittedWork == nil || blocked.UncommittedWork.Kind != UncommittedHoldKindWorktree {
		t.Fatalf("setup: status = %s, hold = %+v; want a blocked Automaton with a worktree hold", blocked.Status, blocked.UncommittedWork)
	}
	// The change hook goes on once the hold exists, so the answer reaches the API.
	_, r := wireUncommittedAnswer(t, m, rig.store)

	answerUncommitted(t, rig.store, openUncommittedQuestion(t, rig.store), UncommittedOptionCommit)

	calls := rig.git.commitCalls()
	if len(calls) != 1 || strings.Join(calls[0].Files, ",") != "docs/b.md,internal/a.go" {
		t.Fatalf("commit calls = %+v; want one commit of exactly the held files", calls)
	}
	got, ok := m.Store().GetPRD(rig.prd.ID)
	if !ok {
		t.Fatal("Automaton vanished")
	}
	if got.UncommittedWork != nil {
		t.Errorf("hold still set: %+v", got.UncommittedWork)
	}
	if got.Status != PRDRunning || got.BlockedReason != "" {
		t.Errorf("status = %s, blocked reason = %q; want running with no reason", got.Status, got.BlockedReason)
	}
	resolved := decisionsOfKind(got, uncommittedResolvedDecisionKind)
	if len(resolved) != 1 || !strings.Contains(resolved[0].Note, "option=commit") {
		t.Errorf("resolved decisions = %+v; want one naming option=commit", resolved)
	}
	if acts := r.actions(); len(acts) == 0 || acts[len(acts)-1] != uncommittedResolvedAuditAction {
		t.Errorf("audit actions = %v; want the last to be %s", acts, uncommittedResolvedAuditAction)
	}

	// The roll-up runs again, finds the worktree clean and completes.
	if final := rig.run(t); final.Status != PRDCompleted {
		t.Fatalf("status after the roll-up = %s; want completed", final.Status)
	}
}

func TestUncommittedAnswerIgnoresOtherQuestions(t *testing.T) {
	r, held := newUncommittedAnswerRig(t, uncommittedTaskOpts{
		policy:      config.UncommittedWorkPolicyHold,
		dirty:       dirtyFiles("src/a.go"),
		commitKeeps: true,
	})
	prd, task := reloadUncommitted(t, r.m, r.prd.ID)
	before := len(r.git.commitCalls())
	decisionsBefore := len(prd.Decisions)
	hold := *held.UncommittedWork

	files := []UncommittedFile{{Path: "src/a.go", Status: " M"}}
	// A question with another label, for the same task.
	other := uncommittedQuestion(prd, task, UncommittedHoldKindTask, files, UncommittedCauseStillDirty)
	other.Label = "something_else"
	otherQ, err := r.store.Raise(other)
	if err != nil {
		t.Fatalf("Raise: %v", err)
	}
	answerUncommitted(t, r.store, otherQ, UncommittedOptionCommit)

	// A stale uncommitted-work question: right label and task, but not the one
	// the hold names.
	stale, err := r.store.Raise(uncommittedQuestion(prd, task, UncommittedHoldKindTask, files, UncommittedCauseStillDirty))
	if err != nil {
		t.Fatalf("Raise: %v", err)
	}
	if stale.ID == hold.QuestionID {
		t.Fatal("setup: the stale question is the held one")
	}
	answerUncommitted(t, r.store, stale, UncommittedOptionCommit)

	got, gt := reloadUncommitted(t, r.m, r.prd.ID)
	if gt.Status != TaskBlocked || gt.UncommittedWork == nil || gt.UncommittedWork.QuestionID != hold.QuestionID {
		t.Errorf("task = %s, hold = %+v; want the hold untouched", gt.Status, gt.UncommittedWork)
	}
	if n := len(r.git.commitCalls()); n != before {
		t.Errorf("commit calls %d -> %d; a question that is not the hold's changes nothing", before, n)
	}
	if n := len(r.discardCalls()); n != 0 {
		t.Errorf("discard calls = %d", n)
	}
	for _, d := range got.Decisions[decisionsBefore:] {
		if strings.HasPrefix(d.Kind, "uncommitted_work") {
			t.Errorf("unexpected decision %s: %s", d.Kind, d.Note)
		}
	}
	// The held question is still open and answerable.
	if q := openUncommittedQuestion(t, r.store); q.ID != hold.QuestionID {
		t.Errorf("open question = %s; want the held %s", q.ID, hold.QuestionID)
	}
}

func TestUncommittedAnswerWithdrawnKeepsHold(t *testing.T) {
	r, held := newUncommittedAnswerRig(t, uncommittedTaskOpts{
		policy:      config.UncommittedWorkPolicyHold,
		dirty:       dirtyFiles("src/a.go"),
		commitKeeps: true,
	})
	before := len(r.git.commitCalls())
	q := openUncommittedQuestion(t, r.store)
	if _, err := r.store.Withdraw(q.ID, "tester", "no longer needed"); err != nil {
		t.Fatalf("Withdraw: %v", err)
	}
	// Expired and withdrawn changes reach the handler too, and change nothing.
	r.api.onUncommittedWorkAnswered(QuestionChange{Kind: QuestionChangeExpired, Question: q})
	r.api.onUncommittedWorkAnswered(QuestionChange{Kind: QuestionChangeWithdrawn, Question: q})

	prd, task := reloadUncommitted(t, r.m, r.prd.ID)
	if task.Status != TaskBlocked || task.UncommittedWork == nil || task.UncommittedWork.QuestionID != held.UncommittedWork.QuestionID {
		t.Fatalf("task = %s, hold = %+v; want the hold kept", task.Status, task.UncommittedWork)
	}
	if n := len(r.git.commitCalls()); n != before {
		t.Errorf("commit calls %d -> %d; a withdrawn question changes nothing", before, n)
	}
	if n := len(r.discardCalls()); n != 0 {
		t.Errorf("discard calls = %d", n)
	}
	if n := len(decisionsOfKind(prd, uncommittedResolvedDecisionKind)); n != 0 {
		t.Errorf("resolved decisions = %d; a withdrawn question resolves nothing", n)
	}
	for _, a := range r.actions() {
		if a == uncommittedResolvedAuditAction {
			t.Error("a resolved audit event was written for a withdrawn question")
		}
	}
}
