package autonomous

import (
	"fmt"
	"strings"
	"testing"
)

func protectedHoldPaths() []string {
	return []string{"internal/autonomous/rules.go", ".github/workflows/ci.yml", "AGENT.md"}
}

func TestProtectedPathsHoldParksTaskAndRaisesOneQuestion(t *testing.T) {
	r := newUncommittedHoldRig(t, true)
	if err := r.m.holdForProtectedPaths(r.prd, r.task, protectedHoldPaths(), "abc123"); err != nil {
		t.Fatalf("holdForProtectedPaths: %v", err)
	}
	tk, _ := r.m.Store().GetTask(r.task.ID)
	if tk.Status != TaskBlocked {
		t.Errorf("status = %s; want blocked", tk.Status)
	}
	if want := "protected_path: .github/workflows/ci.yml, AGENT.md, internal/autonomous/rules.go"; tk.Error != want {
		t.Errorf("error = %q; want %q", tk.Error, want)
	}
	if tk.RetryCount != 0 || tk.FailureReason != nil || tk.EscalationFailures != 0 || tk.PriorAttemptCount != 0 {
		t.Errorf("a hold must not consume retry budget or record a failed attempt: %+v", tk)
	}
	h := tk.ProtectedPathHold
	if h == nil || h.Action != "hold" || h.BaseCommit != "abc123" || h.Since.IsZero() || h.QuestionID == "" || h.AwaitingSpecChange {
		t.Fatalf("hold = %+v", h)
	}
	if strings.Join(h.Paths, ",") != ".github/workflows/ci.yml,AGENT.md,internal/autonomous/rules.go" {
		t.Errorf("hold paths = %v", h.Paths)
	}

	qs := r.store.List(QuestionFilter{})
	if len(qs) != 1 {
		t.Fatalf("questions = %d; want 1", len(qs))
	}
	q := qs[0]
	if q.ID != h.QuestionID {
		t.Errorf("hold names question %q; raised %q", h.QuestionID, q.ID)
	}
	if q.Label != ProtectedPathsQuestionLabel || q.AskedBy != "autonomous" || q.AskerKind != QuestionAskerSystem {
		t.Errorf("label/asker = %q/%q/%q", q.Label, q.AskedBy, q.AskerKind)
	}
	if q.Context != QuestionContextTask || q.TaskID != r.task.ID || q.PRDID != r.prd.ID {
		t.Errorf("context = %q task=%q prd=%q", q.Context, q.TaskID, q.PRDID)
	}
	if q.Default != "hold" {
		t.Errorf("default = %q; want hold", q.Default)
	}
	if q.Recommendation != nil {
		t.Errorf("recommendation = %+v; want none", q.Recommendation)
	}
	var ids, labels []string
	for _, o := range q.Options {
		ids = append(ids, o.ID)
		labels = append(labels, o.Label)
		if o.Tradeoffs == "" {
			t.Errorf("option %s has no tradeoff", o.ID)
		}
	}
	if got := strings.Join(ids, ","); got != "approve,reject,change_spec" {
		t.Errorf("option ids = %s", got)
	}
	if got := strings.Join(labels, ","); got != "Approve this change,Reject it,Change the spec" {
		t.Errorf("option labels = %s", got)
	}
	wants := []string{"Automaton", "Dogfood Automaton", r.prd.ID, "Task 1", r.task.ID, "protected paths",
		"internal/autonomous/rules.go", ".github/workflows/ci.yml", "AGENT.md"}
	for _, want := range wants {
		if !strings.Contains(q.Text, want) {
			t.Errorf("question text lacks %q:\n%s", want, q.Text)
		}
	}
	if strings.Contains(q.Text, "PRD") {
		t.Errorf("question text says PRD:\n%s", q.Text)
	}
	if !strings.HasSuffix(q.Text, "Nothing changes until you answer.") {
		t.Errorf("question text does not end with the standing line:\n%s", q.Text)
	}

	decs := r.decisions("protected_paths_held")
	if len(decs) != 1 || decs[0].QuestionID != h.QuestionID || !strings.Contains(decs[0].Note, "AGENT.md") {
		t.Errorf("decisions = %+v", decs)
	}
	saved, _ := r.m.Store().GetPRD(r.prd.ID)
	found := false
	for _, d := range saved.Decisions {
		found = found || d.Kind == "protected_paths_held"
	}
	if !found {
		t.Error("the Automaton was saved without the decision")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.acts) != 1 || r.acts[0] != "autonomous_protected_paths_held" {
		t.Fatalf("audit actions = %v", r.acts)
	}
	d := r.audits[0]
	if d["prd_id"] != r.prd.ID || d["task_id"] != r.task.ID || d["base_commit"] != "abc123" {
		t.Errorf("audit details = %v", d)
	}
	if len(r.alerts) != 1 || !strings.HasPrefix(r.alerts[0], "warn|Protected paths held|") {
		t.Errorf("alerts = %v", r.alerts)
	}
}

func TestProtectedPathsHoldSecondCallRaisesNoSecondQuestion(t *testing.T) {
	r := newUncommittedHoldRig(t, true)
	for i := 0; i < 2; i++ {
		if err := r.m.holdForProtectedPaths(r.prd, r.task, protectedHoldPaths(), "abc123"); err != nil {
			t.Fatalf("holdForProtectedPaths #%d: %v", i+1, err)
		}
	}
	if n := len(r.store.List(QuestionFilter{})); n != 1 {
		t.Errorf("raised %d questions; want 1", n)
	}
	if n := len(r.decisions("protected_paths_held")); n != 1 {
		t.Errorf("recorded %d decisions; want 1", n)
	}

	// Once the question is answered the hold may raise a fresh one.
	if _, err := r.store.Answer(r.task.ProtectedPathHold.QuestionID, Answer{ChosenOptionID: "change_spec", Actor: "operator"}); err != nil {
		t.Fatalf("Answer: %v", err)
	}
	if err := r.m.holdForProtectedPaths(r.prd, r.task, protectedHoldPaths(), "abc123"); err != nil {
		t.Fatalf("holdForProtectedPaths after answer: %v", err)
	}
	if n := len(r.store.List(QuestionFilter{})); n != 2 {
		t.Errorf("after an answer there are %d questions; want 2", n)
	}
}

func TestProtectedPathsHoldNoQuestionStoreStillHolds(t *testing.T) {
	r := newUncommittedHoldRig(t, false)
	if err := r.m.holdForProtectedPaths(r.prd, r.task, protectedHoldPaths(), "abc123"); err != nil {
		t.Fatalf("holdForProtectedPaths: %v", err)
	}
	if r.task.Status != TaskBlocked {
		t.Errorf("status = %s; want blocked", r.task.Status)
	}
	if r.task.ProtectedPathHold == nil || r.task.ProtectedPathHold.QuestionID != "" {
		t.Errorf("hold = %+v; want a hold with no question id", r.task.ProtectedPathHold)
	}
	decs := r.decisions("protected_paths_held")
	if len(decs) != 1 || decs[0].QuestionID != "" {
		t.Errorf("decisions = %+v", decs)
	}
}

func TestProtectedPathsHoldWarnChangesNothingOnTheTask(t *testing.T) {
	r := newUncommittedHoldRig(t, true)
	r.m.warnProtectedPaths(r.prd, r.task, protectedHoldPaths(), "abc123")
	if r.task.Status != TaskInProgress || r.task.Error != "" {
		t.Errorf("task = status %s error %q; a warning must not change it", r.task.Status, r.task.Error)
	}
	if r.task.ProtectedPathHold != nil {
		t.Error("a warning must not record a hold")
	}
	if n := len(r.store.List(QuestionFilter{})); n != 0 {
		t.Errorf("a warning raised %d questions", n)
	}
	decs := r.decisions("protected_paths_warned")
	if len(decs) != 1 || !strings.Contains(decs[0].Note, "AGENT.md") {
		t.Errorf("decisions = %+v", decs)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.acts) != 1 || r.acts[0] != "autonomous_protected_paths_warned" {
		t.Errorf("audit actions = %v", r.acts)
	}
	if len(r.alerts) != 1 || !strings.HasPrefix(r.alerts[0], "warn|") {
		t.Errorf("alerts = %v", r.alerts)
	}
}

func TestProtectedPathsHoldBlockSetsErrorAndRaisesNoQuestion(t *testing.T) {
	r := newUncommittedHoldRig(t, true)
	if err := r.m.blockProtectedPaths(r.prd, r.task, []string{"b.go", "a.go"}, "abc123"); err != nil {
		t.Fatalf("blockProtectedPaths: %v", err)
	}
	tk, _ := r.m.Store().GetTask(r.task.ID)
	if tk.Status != TaskBlocked || tk.Error != "protected_path_blocked: a.go, b.go" {
		t.Errorf("task = status %s error %q", tk.Status, tk.Error)
	}
	if tk.ProtectedPathHold != nil {
		t.Errorf("a block records no hold: %+v", tk.ProtectedPathHold)
	}
	if n := len(r.store.List(QuestionFilter{})); n != 0 {
		t.Errorf("a block raised %d questions", n)
	}
	if n := len(r.decisions("protected_paths_blocked")); n != 1 {
		t.Errorf("blocked decisions = %d; want 1", n)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.acts) != 1 || r.acts[0] != "autonomous_protected_paths_blocked" {
		t.Errorf("audit actions = %v", r.acts)
	}
	if len(r.alerts) != 1 || !strings.HasPrefix(r.alerts[0], "warn|") {
		t.Errorf("alerts = %v", r.alerts)
	}
}

func TestProtectedPathsHoldViewCarriesTheHold(t *testing.T) {
	r := newUncommittedHoldRig(t, true)
	if err := r.m.holdForProtectedPaths(r.prd, r.task, protectedHoldPaths(), "abc123"); err != nil {
		t.Fatalf("holdForProtectedPaths: %v", err)
	}
	prd, _ := r.m.Store().GetPRD(r.prd.ID)
	view := BuildTasksView(prd)
	if len(view.Tasks) != 1 {
		t.Fatalf("tasks = %d", len(view.Tasks))
	}
	h := view.Tasks[0].ProtectedPathHold
	if h == nil || h.BaseCommit != "abc123" || len(h.Paths) != 3 || h.QuestionID == "" {
		t.Errorf("view hold = %+v", h)
	}
}

func TestProtectedPathsHoldLongPathListsAreBounded(t *testing.T) {
	r := newUncommittedHoldRig(t, true)
	var paths []string
	for i := 0; i < 25; i++ {
		paths = append(paths, fmt.Sprintf("pkg/f%02d.go", i))
	}
	if err := r.m.holdForProtectedPaths(r.prd, r.task, paths, "abc123"); err != nil {
		t.Fatalf("holdForProtectedPaths: %v", err)
	}
	q := r.store.List(QuestionFilter{})[0]
	if !strings.Contains(q.Text, "and 5 more") || strings.Contains(q.Text, "pkg/f24.go") || !strings.Contains(q.Text, "pkg/f19.go") {
		t.Errorf("question text is not bounded to 20 paths:\n%s", q.Text)
	}
	if want := "protected_path: pkg/f00.go, pkg/f01.go, pkg/f02.go, pkg/f03.go, pkg/f04.go and 20 more"; r.task.Error != want {
		t.Errorf("error = %q; want %q", r.task.Error, want)
	}
	if len(r.task.ProtectedPathHold.Paths) != 25 {
		t.Errorf("the record keeps all %d paths", len(r.task.ProtectedPathHold.Paths))
	}

	// Very long paths stay inside the question size cap and keep the closing line.
	long := []string{strings.Repeat("d/", 2000) + "x.go", strings.Repeat("e/", 2000) + "y.go",
		strings.Repeat("f/", 2000) + "z.go"}
	text := protectedPathsQuestionText(r.prd, r.task, long)
	if len(text) > questionTextMaxBytes {
		t.Errorf("question text is %d bytes; cap is %d", len(text), questionTextMaxBytes)
	}
	if !strings.HasSuffix(text, "Nothing changes until you answer.") {
		t.Error("a capped question text lost its closing line")
	}
}

func TestProtectedPathsHoldResetClearsTheHold(t *testing.T) {
	r := newUncommittedHoldRig(t, true)
	if err := r.m.holdForProtectedPaths(r.prd, r.task, protectedHoldPaths(), "abc123"); err != nil {
		t.Fatalf("holdForProtectedPaths: %v", err)
	}
	if _, err := r.m.ResetTask(r.prd.ID, r.task.ID, "operator", false); err != nil {
		t.Fatalf("ResetTask: %v", err)
	}
	tk, _ := r.m.Store().GetTask(r.task.ID)
	if tk.ProtectedPathHold != nil {
		t.Errorf("hold survived the reset: %+v", tk.ProtectedPathHold)
	}
	if tk.Error != "" {
		t.Errorf("error survived the reset: %q", tk.Error)
	}
}
