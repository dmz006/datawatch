package autonomous

import (
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

// uncommittedHoldRig is a Manager with one Automaton and one task, a real
// question store and fakes for the alert and audit hooks.
type uncommittedHoldRig struct {
	m     *Manager
	prd   *PRD
	task  *Task
	store *QuestionStore

	mu     sync.Mutex
	audits []map[string]any
	acts   []string
	alerts []string
}

func newUncommittedHoldRig(t *testing.T, withStore bool) *uncommittedHoldRig {
	t.Helper()
	m, _ := newFailureEscalationFixture(t, t.TempDir())
	prd, err := m.CreatePRD("test spec", t.TempDir(), "opencode", "", EffortNormal)
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
	task.Status = TaskInProgress
	if err := m.Store().SaveTask(task); err != nil {
		t.Fatalf("SaveTask: %v", err)
	}
	r := &uncommittedHoldRig{m: m, prd: prd, task: task}
	if withStore {
		s, err := NewQuestionStore(filepath.Join(t.TempDir(), "questions.jsonl"))
		if err != nil {
			t.Fatalf("NewQuestionStore: %v", err)
		}
		m.SetDecisionGate(s, func() config.AutonomousQuestionsConfig { return config.AutonomousQuestionsConfig{} })
		r.store = s
	}
	m.SetAuditFn(func(action string, details map[string]any) {
		r.mu.Lock()
		defer r.mu.Unlock()
		r.acts = append(r.acts, action)
		r.audits = append(r.audits, details)
	})
	m.SetAlertSink(func(level, title, body string) string {
		r.mu.Lock()
		defer r.mu.Unlock()
		r.alerts = append(r.alerts, level+"|"+title+"|"+body)
		return "alert-1"
	})
	return r
}

func (r *uncommittedHoldRig) decisions(kind string) []Decision {
	var out []Decision
	for _, d := range r.prd.Decisions {
		if d.Kind == kind {
			out = append(out, d)
		}
	}
	return out
}

func uncommittedFiles(outside ...string) []UncommittedFile {
	files := []UncommittedFile{
		{Path: "internal/autonomous/a.go", Status: " M"},
		{Path: "docs/b.md", Status: "??"},
	}
	for _, p := range outside {
		files = append(files, UncommittedFile{Path: p, Status: " M", OutsideScope: true})
	}
	return files
}

func TestUncommittedHoldRaisesQuestionWithThreeOptions(t *testing.T) {
	r := newUncommittedHoldRig(t, true)
	if err := r.m.holdForUncommittedWork(r.prd, r.task, uncommittedFiles("README.md"), UncommittedCauseStillDirty); err != nil {
		t.Fatalf("holdForUncommittedWork: %v", err)
	}
	qs := r.store.List(QuestionFilter{})
	if len(qs) != 1 {
		t.Fatalf("questions = %d; want 1", len(qs))
	}
	q := qs[0]
	if q.Label != "uncommitted_work" || q.AskedBy != "autonomous" || q.AskerKind != QuestionAskerSystem {
		t.Errorf("label/asker = %q/%q/%q", q.Label, q.AskedBy, q.AskerKind)
	}
	if q.Context != QuestionContextTask || q.TaskID != r.task.ID || q.PRDID != r.prd.ID {
		t.Errorf("context = %q task=%q prd=%q", q.Context, q.TaskID, q.PRDID)
	}
	if q.Default != "hold" {
		t.Errorf("default = %q; want hold", q.Default)
	}
	var ids, labels []string
	for _, o := range q.Options {
		ids = append(ids, o.ID)
		labels = append(labels, o.Label)
	}
	if got := strings.Join(ids, ","); got != "commit,discard,retry" {
		t.Errorf("option ids = %s", got)
	}
	if got := strings.Join(labels, ","); got != "Commit them,Discard them,Retry the task" {
		t.Errorf("option labels = %s", got)
	}
	for _, want := range []string{"Automaton", "Dogfood Automaton", r.prd.ID, "Task 1", "internal/autonomous/a.go", "docs/b.md", "README.md (outside the task's declared files)", UncommittedCauseStillDirty} {
		if !strings.Contains(q.Text, want) {
			t.Errorf("question text lacks %q:\n%s", want, q.Text)
		}
	}
	if strings.Contains(q.Text, "PRD") {
		t.Errorf("question text says PRD:\n%s", q.Text)
	}
	if q.Recommendation == nil || q.Recommendation.OptionID != "retry" {
		t.Errorf("recommendation = %+v; want retry with a file outside the scope", q.Recommendation)
	}
	if r.task.UncommittedWork == nil || r.task.UncommittedWork.QuestionID != q.ID {
		t.Errorf("task hold does not name question %s: %+v", q.ID, r.task.UncommittedWork)
	}
}

func TestUncommittedHoldQuestionBoundsFilesAndRecommendsCommit(t *testing.T) {
	r := newUncommittedHoldRig(t, true)
	var files []UncommittedFile
	for i := 0; i < 25; i++ {
		files = append(files, UncommittedFile{Path: fmt.Sprintf("pkg/f%02d.go", i), Status: "??"})
	}
	if err := r.m.holdForUncommittedWork(r.prd, r.task, files, UncommittedCauseCommitFailed); err != nil {
		t.Fatalf("holdForUncommittedWork: %v", err)
	}
	q := r.store.List(QuestionFilter{})[0]
	if !strings.Contains(q.Text, "and 5 more") || strings.Contains(q.Text, "pkg/f24.go") {
		t.Errorf("question text is not bounded to 20 files:\n%s", q.Text)
	}
	if q.Recommendation == nil || q.Recommendation.OptionID != "commit" {
		t.Errorf("recommendation = %+v; want commit with every file in scope", q.Recommendation)
	}
}

func TestUncommittedHoldRecordsFilesAndReason(t *testing.T) {
	r := newUncommittedHoldRig(t, true)
	files := uncommittedFiles("README.md")
	if err := r.m.holdForUncommittedWork(r.prd, r.task, files, UncommittedCauseCommitFailed); err != nil {
		t.Fatalf("holdForUncommittedWork: %v", err)
	}
	tk, _ := r.m.Store().GetTask(r.task.ID)
	if tk.Status != TaskBlocked {
		t.Errorf("status = %s; want blocked", tk.Status)
	}
	if !strings.HasPrefix(tk.Error, "uncommitted_work: ") || !strings.Contains(tk.Error, "README.md") {
		t.Errorf("error = %q", tk.Error)
	}
	fr := tk.FailureReason
	if fr == nil || fr.Category != "git" || fr.Source != "uncommitted-work-hold" || !fr.Retryable || fr.Detail == "" || fr.OccurredAt.IsZero() {
		t.Errorf("failure reason = %+v", fr)
	}
	h := tk.UncommittedWork
	if h == nil || h.Kind != "task" || h.Reason != UncommittedCauseCommitFailed || h.HeldAt.IsZero() || h.QuestionID == "" {
		t.Fatalf("hold = %+v", h)
	}
	if strings.Join(h.Files, ",") != "README.md,docs/b.md,internal/autonomous/a.go" {
		t.Errorf("hold files = %v", h.Files)
	}
	if strings.Join(h.OutsideScope, ",") != "README.md" {
		t.Errorf("hold outside scope = %v", h.OutsideScope)
	}
	decs := r.decisions("uncommitted_work_held")
	if len(decs) != 1 || decs[0].QuestionID != h.QuestionID || !strings.Contains(decs[0].Note, "README.md") {
		t.Errorf("decisions = %+v", decs)
	}
	saved, _ := r.m.Store().GetPRD(r.prd.ID)
	found := false
	for _, d := range saved.Decisions {
		found = found || d.Kind == "uncommitted_work_held"
	}
	if !found {
		t.Error("the Automaton was saved without the decision")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.acts) != 1 || r.acts[0] != "autonomous_uncommitted_work_held" {
		t.Fatalf("audit actions = %v", r.acts)
	}
	d := r.audits[0]
	if d["prd_id"] != r.prd.ID || d["task_id"] != r.task.ID || d["cause"] != UncommittedCauseCommitFailed {
		t.Errorf("audit details = %v", d)
	}
	if got := d["files"].([]string); len(got) != 3 {
		t.Errorf("audit files = %v", got)
	}
	if got := d["outside_scope"].([]string); len(got) != 1 || got[0] != "README.md" {
		t.Errorf("audit outside_scope = %v", got)
	}
	if len(r.alerts) != 1 || !strings.HasPrefix(r.alerts[0], "warn|Uncommitted work held|") {
		t.Errorf("alerts = %v", r.alerts)
	}
}

func TestUncommittedHoldIsIdempotent(t *testing.T) {
	r := newUncommittedHoldRig(t, true)
	files := uncommittedFiles()
	for i := 0; i < 2; i++ {
		if err := r.m.holdForUncommittedWork(r.prd, r.task, files, UncommittedCauseStillDirty); err != nil {
			t.Fatalf("holdForUncommittedWork #%d: %v", i+1, err)
		}
	}
	if n := len(r.store.List(QuestionFilter{})); n != 1 {
		t.Errorf("task hold raised %d questions; want 1", n)
	}
	if n := len(r.decisions("uncommitted_work_held")); n != 1 {
		t.Errorf("task hold recorded %d decisions; want 1", n)
	}

	for i := 0; i < 2; i++ {
		r.m.holdAutomatonForUncommittedWork(r.prd, files, UncommittedCauseStillDirty)
	}
	if n := len(r.store.List(QuestionFilter{})); n != 2 {
		t.Errorf("after the Automaton hold there are %d questions; want 2", n)
	}
	if r.prd.UncommittedWork == nil || r.prd.UncommittedWork.Kind != "worktree" {
		t.Fatalf("Automaton hold = %+v", r.prd.UncommittedWork)
	}
	var wt Question
	for _, q := range r.store.List(QuestionFilter{}) {
		if q.ID == r.prd.UncommittedWork.QuestionID {
			wt = q
		}
	}
	if wt.Context != QuestionContextPrd || wt.TaskID != "" || wt.Options[2].Label != "Check again" {
		t.Errorf("worktree question = context %q task %q retry label %q", wt.Context, wt.TaskID, wt.Options[2].Label)
	}
	if r.prd.Status != PRDRunning {
		t.Errorf("Automaton status = %s; the worktree hold must not change it", r.prd.Status)
	}

	// Once the question is answered the hold may raise a fresh one.
	if _, err := r.store.Answer(r.task.UncommittedWork.QuestionID, Answer{ChosenOptionID: "retry", Actor: "operator"}); err != nil {
		t.Fatalf("Answer: %v", err)
	}
	if err := r.m.holdForUncommittedWork(r.prd, r.task, files, UncommittedCauseStillDirty); err != nil {
		t.Fatalf("holdForUncommittedWork after answer: %v", err)
	}
	if n := len(r.store.List(QuestionFilter{})); n != 3 {
		t.Errorf("after an answer there are %d questions; want 3", n)
	}
}

func TestUncommittedWarnRecordsDecisionOnly(t *testing.T) {
	r := newUncommittedHoldRig(t, true)
	r.m.warnUncommittedWork(r.prd, r.task, uncommittedFiles("README.md"), UncommittedCauseStillDirty)
	r.m.warnUncommittedWork(r.prd, nil, uncommittedFiles(), UncommittedCauseCommitFailed)
	if r.task.Status != TaskInProgress {
		t.Errorf("task status = %s; a warning must not change it", r.task.Status)
	}
	if r.task.UncommittedWork != nil || r.prd.UncommittedWork != nil {
		t.Error("a warning must not record a hold")
	}
	if n := len(r.store.List(QuestionFilter{})); n != 0 {
		t.Errorf("a warning raised %d questions", n)
	}
	if n := len(r.decisions("uncommitted_work_warned")); n != 2 {
		t.Errorf("warned decisions = %d; want 2", n)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.acts) != 2 || r.acts[0] != "autonomous_uncommitted_work_warned" {
		t.Errorf("audit actions = %v", r.acts)
	}
	if _, ok := r.audits[1]["task_id"]; ok {
		t.Errorf("the Automaton-wide warning carries a task id: %v", r.audits[1])
	}
	if len(r.alerts) != 2 || !strings.HasPrefix(r.alerts[0], "warn|") {
		t.Errorf("alerts = %v", r.alerts)
	}
}

func TestUncommittedHoldNoQuestionStoreStillHolds(t *testing.T) {
	r := newUncommittedHoldRig(t, false)
	if err := r.m.holdForUncommittedWork(r.prd, r.task, uncommittedFiles(), UncommittedCauseCommitFailed); err != nil {
		t.Fatalf("holdForUncommittedWork: %v", err)
	}
	if r.task.Status != TaskBlocked {
		t.Errorf("status = %s; want blocked", r.task.Status)
	}
	if r.task.UncommittedWork == nil || r.task.UncommittedWork.QuestionID != "" {
		t.Errorf("hold = %+v; want a hold with no question id", r.task.UncommittedWork)
	}
	decs := r.decisions("uncommitted_work_held")
	if len(decs) != 1 || decs[0].QuestionID != "" {
		t.Errorf("decisions = %+v", decs)
	}
	r.m.holdAutomatonForUncommittedWork(r.prd, uncommittedFiles(), UncommittedCauseStillDirty)
	if r.prd.UncommittedWork == nil || r.prd.UncommittedWork.QuestionID != "" {
		t.Errorf("Automaton hold = %+v", r.prd.UncommittedWork)
	}
}
