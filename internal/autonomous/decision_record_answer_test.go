package autonomous

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/config"
)

// answerEffectsLog counts the calls to applyAnswerEffects.
type answerEffectsLog struct {
	mu    sync.Mutex
	calls []Answer
}

func (l *answerEffectsLog) seen(_ DecisionRecord, _ Question, a Answer) {
	l.mu.Lock()
	l.calls = append(l.calls, a)
	l.mu.Unlock()
}

func (l *answerEffectsLog) count() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.calls)
}

// newAnswerRig is a sync rig whose service chains onto the question store
// change hook and counts the effects.
func newAnswerRig(t *testing.T, chain bool) (*decisionSyncRig, *answerEffectsLog) {
	t.Helper()
	r := newDecisionSyncRig(t)
	log := &answerEffectsLog{}
	r.svc.effectsSeen = log.seen
	if chain {
		r.svc.ChainQuestionHook()
	}
	return r, log
}

// openedRecord creates a clear record and returns its view.
func openedRecord(t *testing.T, r *decisionSyncRig) DecisionRecordView {
	t.Helper()
	v, _, err := r.svc.Create(clearRecord(), DecisionSourceREST, "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if v.QuestionID == "" {
		t.Fatalf("record %s has no question", v.ID)
	}
	return v
}

const multiLineQuote = "Go with SQLite.\r\n\n  Keep the files as a backup;\n\tdo not drop them.  \n\n"

func TestDecisionAnswerApply_StoresAnswerFieldsOnRecord(t *testing.T) {
	r, effects := newAnswerRig(t, true)
	v := openedRecord(t, r)

	ans, err := r.questions.Answer(v.QuestionID, Answer{
		Actor: "alice", Surface: "pwa", ChosenOptionID: "sqlite", Quote: "SQLite, please",
	})
	if err != nil {
		t.Fatalf("Answer: %v", err)
	}
	rec, _ := r.records.Get(v.ID)
	if rec.State != DecisionRecordAnswered {
		t.Fatalf("record state = %s, want answered", rec.State)
	}
	got := rec.Answer
	if got == nil {
		t.Fatal("record has no answer")
	}
	if got.AnswerID != ans.ID || got.ChosenOptionID != "sqlite" || got.Quote != "SQLite, please" ||
		got.Actor != "alice" || got.Surface != "pwa" {
		t.Fatalf("stored answer = %+v, want the one given (%+v)", *got, ans)
	}
	if got.AnsweredAt == nil || !got.AnsweredAt.Equal(*ans.CreatedAt) {
		t.Fatalf("answered_at = %v, want %v", got.AnsweredAt, ans.CreatedAt)
	}
	if effects.count() != 1 {
		t.Fatalf("effects ran %d times, want 1", effects.count())
	}
}

func TestDecisionAnswerApply_MultiLineQuoteKeptByteForByte(t *testing.T) {
	r, _ := newAnswerRig(t, true)
	v := openedRecord(t, r)
	if _, err := r.questions.Answer(v.QuestionID, Answer{Actor: "bob", Surface: "cli", ChosenOptionID: "files", Quote: multiLineQuote}); err != nil {
		t.Fatalf("Answer: %v", err)
	}
	rec, _ := r.records.Get(v.ID)
	if rec.Answer == nil || rec.Answer.Quote != multiLineQuote {
		t.Fatalf("quote = %q, want %q byte for byte", rec.Answer, multiLineQuote)
	}
	// It survives a reload of the store from disk.
	reloaded, err := NewDecisionRecordStore(r.records.path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	again, _ := reloaded.Get(v.ID)
	if again.Answer == nil || again.Answer.Quote != multiLineQuote {
		t.Fatalf("quote after reload = %+v, want %q", again.Answer, multiLineQuote)
	}
	view, _ := r.svc.View(v.ID)
	if view.AnsweredQuote != multiLineQuote {
		t.Fatalf("view quote = %q, want %q", view.AnsweredQuote, multiLineQuote)
	}
}

func TestDecisionAnswerApply_QuoteOnlyAnswerBecomesAnswered(t *testing.T) {
	r, _ := newAnswerRig(t, true)
	v := openedRecord(t, r)
	if _, err := r.questions.Answer(v.QuestionID, Answer{Actor: "carol", Surface: "mcp", Quote: "Neither; ask me next week."}); err != nil {
		t.Fatalf("Answer: %v", err)
	}
	rec, _ := r.records.Get(v.ID)
	if rec.State != DecisionRecordAnswered || rec.Answer == nil || rec.Answer.ChosenOptionID != "" ||
		rec.Answer.Quote != "Neither; ask me next week." {
		t.Fatalf("record = state %s answer %+v, want answered with the quote and no option", rec.State, rec.Answer)
	}
}

func TestDecisionAnswerApply_SameAnswerTwiceChangesNothing(t *testing.T) {
	r, effects := newAnswerRig(t, true)
	v := openedRecord(t, r)
	ans, err := r.questions.Answer(v.QuestionID, Answer{Actor: "alice", Surface: "pwa", ChosenOptionID: "sqlite", Quote: "yes"})
	if err != nil {
		t.Fatalf("Answer: %v", err)
	}
	first, _ := r.records.Get(v.ID)
	before, err := readFileString(r.records.path)
	if err != nil {
		t.Fatal(err)
	}

	// The hook again, the recovery path, and a direct apply: none changes anything.
	q, _ := r.questions.Get(v.QuestionID)
	r.svc.OnQuestionChange(QuestionChange{Kind: QuestionChangeAnswered, Question: q, Answer: &ans})
	rep := r.svc.Reconcile()
	r.svc.mu.Lock()
	applied, err := r.svc.applyAnswerLocked(first, q, ans)
	r.svc.mu.Unlock()
	if err != nil || applied {
		t.Fatalf("applyAnswerLocked again = %v, %v; want false, nil", applied, err)
	}
	r.svc.drainAnswerEffects()

	after, _ := readFileString(r.records.path)
	if after != before {
		t.Fatalf("the record file changed on a repeated apply:\nbefore: %s\nafter:  %s", before, after)
	}
	second, _ := r.records.Get(v.ID)
	if !second.UpdatedAt.Equal(*first.UpdatedAt) || second.Answer.AnswerID != first.Answer.AnswerID {
		t.Fatalf("record changed: %+v -> %+v", first, second)
	}
	if rep.Answered != 0 {
		t.Fatalf("Reconcile reported %d answers applied, want 0 (the record is answered, not open)", rep.Answered)
	}
	if effects.count() != 1 {
		t.Fatalf("effects ran %d times, want exactly 1", effects.count())
	}
}

func TestDecisionAnswerApply_RecoveryAppliesAnswerTheHookMissed(t *testing.T) {
	// No hook is chained: the answer lands, and only Reconcile can apply it.
	r, effects := newAnswerRig(t, false)
	v := openedRecord(t, r)
	ans, err := r.questions.Answer(v.QuestionID, Answer{Actor: "alice", Surface: "comm", ChosenOptionID: "files", Quote: "keep the files"})
	if err != nil {
		t.Fatalf("Answer: %v", err)
	}
	if rec, _ := r.records.Get(v.ID); rec.State != DecisionRecordOpen || rec.Answer != nil {
		t.Fatalf("record = %+v before recovery, want open with no answer", rec)
	}

	var released []QuestionChange
	r.svc.SetAnswerRelease(func(c QuestionChange) { released = append(released, c) })
	rep := r.svc.Reconcile()
	if rep.Answered != 1 || !rep.Changed() || len(rep.Errors) != 0 {
		t.Fatalf("report = %+v, want one answer applied and no errors", rep)
	}
	rec, _ := r.records.Get(v.ID)
	if rec.State != DecisionRecordAnswered || rec.Answer == nil || rec.Answer.AnswerID != ans.ID ||
		rec.Answer.Quote != "keep the files" || rec.Answer.Actor != "alice" || rec.Answer.Surface != "comm" ||
		rec.Answer.ChosenOptionID != "files" {
		t.Fatalf("record = %+v, want the missed answer applied", rec)
	}
	if effects.count() != 1 || len(released) != 1 || released[0].Question.ID != v.QuestionID {
		t.Fatalf("effects = %d, releases = %d; want one of each for the question", effects.count(), len(released))
	}

	// A second poll has nothing left to do.
	if rep := r.svc.Reconcile(); rep.Changed() {
		t.Fatalf("second Reconcile changed things: %+v", rep)
	}
	if effects.count() != 1 {
		t.Fatalf("effects ran %d times after the second poll, want 1", effects.count())
	}
}

func TestDecisionAnswerApply_ChainKeepsTheExistingHook(t *testing.T) {
	r := newDecisionSyncRig(t)
	var existing int
	r.questions.SetChangeHook(func(c QuestionChange) {
		if c.Kind == QuestionChangeAnswered {
			existing++
		}
	})
	effects := &answerEffectsLog{}
	r.svc.effectsSeen = effects.seen
	r.svc.ChainQuestionHook()

	v := openedRecord(t, r)
	if _, err := r.questions.Answer(v.QuestionID, Answer{Actor: "alice", Surface: "pwa", ChosenOptionID: "sqlite"}); err != nil {
		t.Fatalf("Answer: %v", err)
	}
	if existing != 1 {
		t.Fatalf("existing hook saw %d answers, want 1", existing)
	}
	if rec, _ := r.records.Get(v.ID); rec.State != DecisionRecordAnswered {
		t.Fatalf("record state = %s, want answered", rec.State)
	}
	if effects.count() != 1 {
		t.Fatalf("effects ran %d times, want 1", effects.count())
	}
}

func TestDecisionAnswerApply_OrdinaryQuestionAndSwitchOffAreLeftAlone(t *testing.T) {
	r, effects := newAnswerRig(t, true)

	// A question no record owns: nothing happens and nothing fails.
	id := mustRaise(t, r.questions, sampleQuestion("An ordinary question?"))
	if _, err := r.questions.Answer(id, Answer{Actor: "alice", Surface: "pwa", ChosenOptionID: "first"}); err != nil {
		t.Fatalf("Answer: %v", err)
	}
	if effects.count() != 0 {
		t.Fatalf("effects ran for an ordinary question")
	}

	// With the decisions switch off the hook defers; Reconcile applies it
	// once the switch is back on.
	v := openedRecord(t, r)
	off := false
	r.dcfg.Enabled = &off
	if _, err := r.questions.Answer(v.QuestionID, Answer{Actor: "alice", Surface: "pwa", ChosenOptionID: "sqlite"}); err != nil {
		t.Fatalf("Answer: %v", err)
	}
	if rec, _ := r.records.Get(v.ID); rec.State != DecisionRecordOpen {
		t.Fatalf("record state = %s with the switch off, want open", rec.State)
	}
	r.dcfg.Enabled = nil
	if rep := r.svc.Reconcile(); rep.Answered != 1 {
		t.Fatalf("report = %+v, want the answer applied after the switch came back", rep)
	}
	if rec, _ := r.records.Get(v.ID); rec.State != DecisionRecordAnswered {
		t.Fatalf("record state = %s, want answered", rec.State)
	}
}

// answerGateRig runs an Automaton against the fakes of the decision gate
// tests, with a held task whose spec names the question of a decision
// record. The change hook carries the service only: the wake that starts the
// task comes from the service's release, not from a hook that already calls
// the API.
type answerGateRig struct {
	*decisionGateRig
	svc     *DecisionRecords
	records *DecisionRecordStore
	view    DecisionRecordView
	effects *answerEffectsLog
}

func newAnswerGateRig(t *testing.T) *answerGateRig {
	t.Helper()
	m, api := newFailureEscalationFixture(t, t.TempDir())
	qs, err := NewQuestionStore(filepath.Join(t.TempDir(), "questions.jsonl"))
	if err != nil {
		t.Fatalf("NewQuestionStore: %v", err)
	}
	rs, err := NewDecisionRecordStore(filepath.Join(t.TempDir(), "decision_records.jsonl"))
	if err != nil {
		t.Fatalf("NewDecisionRecordStore: %v", err)
	}
	qcfg := config.AutonomousQuestionsConfig{Enabled: true, DecisionGateMode: config.DecisionGateModeOn}
	svc := NewDecisionRecords(rs, qs,
		func() config.AutonomousDecisionsConfig { return config.AutonomousDecisionsConfig{} },
		func() config.AutonomousQuestionsConfig { return qcfg },
		nil)
	effects := &answerEffectsLog{}
	svc.effectsSeen = effects.seen
	svc.SetAnswerRelease(api.OnQuestionChange)
	qs.SetChangeHook(m.RecordQuestionChange)
	svc.ChainQuestionHook()
	m.SetDecisionGate(qs, func() config.AutonomousQuestionsConfig { return qcfg })
	m.SetAlertSink(func(_, _, _ string) string { return "" })

	view, _, err := svc.Create(clearRecord(), DecisionSourceREST, "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	g := &decisionGateRig{m: m, api: api, store: qs, decision: view.QuestionID}
	task := func(id, title, spec string, deps ...string) Task {
		return Task{ID: id, Title: title, Spec: spec, ProjectDir: t.TempDir(), DependsOn: deps}
	}
	tasks := []Task{
		task(gateFreeID, "Free task", "do the free task"),
		task(gateHeldID, "Held task", "do the held task once "+view.QuestionID+" is answered"),
		task(gateFollowerID, "Follower task", "do the follower task", "Held task"),
	}
	g.prdID = newHoldPRD(t, m, tasks, func(p *PRD) { p.MaxConcurrentTasks = 2 }).ID
	api.SetExecutors(g.spawns.spawn, func(context.Context, *PRD, *Task) (VerificationResult, error) {
		return VerificationResult{OK: true, Summary: "ok"}, nil
	})
	return &answerGateRig{decisionGateRig: g, svc: svc, records: rs, view: view, effects: effects}
}

func TestDecisionAnswerApply_AnswerMovesGatedTaskFromWaitingToReleased(t *testing.T) {
	r := newAnswerGateRig(t)
	r.startAndWaitParked(t)
	if r.spawned(gateHeldID) {
		t.Fatal("the held task started before its decision was answered")
	}
	if n := r.runDecisions(t); n != 1 {
		t.Fatalf("run decisions before the answer = %d, want 1", n)
	}

	begun := time.Now()
	if _, err := r.store.Answer(r.view.QuestionID, Answer{Actor: "alice", Surface: "pwa", ChosenOptionID: "sqlite", Quote: "SQLite"}); err != nil {
		t.Fatalf("Answer: %v", err)
	}
	// The poll is thirty seconds; starting well inside that proves the wake.
	pollUntil(t, gateAnswerStart, "held task started", func() bool { return r.spawned(gateHeldID) })
	if took := time.Since(begun); took >= gateAnswerStart {
		t.Errorf("task started %s after the answer, want under %s", took, gateAnswerStart)
	}
	pollUntil(t, gateWait, "Automaton completed", func() bool { return r.prdStatus(t) == PRDCompleted })
	if got := r.status(t, gateHeldID); got != TaskCompleted {
		t.Errorf("held task status = %s, want completed", got)
	}
	if reason := r.task(t, gateHeldID).WaitReason; reason != "" {
		t.Errorf("wait reason after release = %q, want empty", reason)
	}
	if n := r.runDecisions(t); n != 1 {
		t.Errorf("run decisions after the answer = %d, want 1 (the waiting run, no second one)", n)
	}

	rec, _ := r.records.Get(r.view.ID)
	if rec.State != DecisionRecordAnswered || rec.Answer == nil || rec.Answer.Quote != "SQLite" {
		t.Errorf("record = state %s answer %+v, want answered with the quote", rec.State, rec.Answer)
	}
	if r.effects.count() != 1 {
		t.Errorf("effects ran %d times, want 1", r.effects.count())
	}
}

func TestDecisionAnswerApply_WithdrawnQuestionDoesNotReleaseTask(t *testing.T) {
	r := newAnswerGateRig(t)
	r.startAndWaitParked(t)

	if _, err := r.svc.Withdraw(r.view.ID, "operator", "no longer needed"); err != nil {
		t.Fatalf("Withdraw: %v", err)
	}
	// A withdrawal does not wake the executor; nudge it so the scan runs.
	r.api.reviewWake.signal(r.prdID)
	want := "operator decision " + r.view.QuestionID + " was withdrawn without an answer"
	pollUntil(t, gateWait, "wait reason names the withdrawal", func() bool {
		return r.task(t, gateHeldID).WaitReason == want
	})
	if got := r.status(t, gateHeldID); got != TaskWaitingDecision {
		t.Errorf("held task status = %s, want waiting_decision", got)
	}
	if r.spawned(gateHeldID) {
		t.Error("the held task started after its decision was withdrawn")
	}

	// Neither the hook nor the recovery path turns a withdrawn record into an
	// answered one.
	q, _ := r.store.Get(r.view.QuestionID)
	r.svc.OnQuestionChange(QuestionChange{Kind: QuestionChangeAnswered, Question: q, Answer: &Answer{ID: "a-forged", Quote: "late"}})
	r.svc.Reconcile()
	rec, _ := r.records.Get(r.view.ID)
	if rec.State != DecisionRecordWithdrawn || rec.Answer != nil {
		t.Errorf("record = state %s answer %+v, want withdrawn with no answer", rec.State, rec.Answer)
	}
	if r.effects.count() != 0 {
		t.Errorf("effects ran %d times for a withdrawn record, want 0", r.effects.count())
	}
}

func TestDecisionAnswerApply_ReleaseRunsOutsideTheServiceLock(t *testing.T) {
	// A release that calls back into the service must not deadlock.
	r, _ := newAnswerRig(t, true)
	var called atomic.Int32
	r.svc.SetAnswerRelease(func(c QuestionChange) {
		called.Add(1)
		if _, ok := r.svc.View(c.Question.DecisionRecordID); !ok {
			t.Errorf("release could not read the record")
		}
		r.svc.Reconcile()
	})
	v := openedRecord(t, r)
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = r.questions.Answer(v.QuestionID, Answer{Actor: "alice", Surface: "pwa", ChosenOptionID: "sqlite"})
	}()
	select {
	case <-done:
	case <-time.After(gateWait):
		t.Fatal("answering deadlocked with a release that re-enters the service")
	}
	if called.Load() != 1 {
		t.Fatalf("release ran %d times, want 1", called.Load())
	}
}

func readFileString(path string) (string, error) {
	b, err := os.ReadFile(path)
	return strings.TrimSpace(string(b)), err
}
