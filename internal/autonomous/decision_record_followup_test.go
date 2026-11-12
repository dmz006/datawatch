package autonomous

import (
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

// fakeFollowUpTargets stands in for the template instantiate call and the
// Automaton approve and run calls.
type fakeFollowUpTargets struct {
	mu        sync.Mutex
	templates []string
	approved  []string
	ran       []string
	status    map[string]string
	startErr  error
}

func (f *fakeFollowUpTargets) InstantiateTemplate(templateID string, _ map[string]string, _ string) (any, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.templates = append(f.templates, templateID)
	if f.startErr != nil {
		return nil, f.startErr
	}
	return &PRD{ID: "new-automaton-1"}, nil
}

func (f *fakeFollowUpTargets) AutomatonStatus(id string) (string, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	st, ok := f.status[id]
	return st, ok
}

func (f *fakeFollowUpTargets) Approve(id, _, _ string) (any, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.approved = append(f.approved, id)
	return nil, nil
}

func (f *fakeFollowUpTargets) Run(id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ran = append(f.ran, id)
	return nil
}

func (f *fakeFollowUpTargets) calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.templates) + len(f.approved) + len(f.ran)
}

// followUpRig is an answer rig with two configured follow-ups and the fake
// wired in.
func followUpRig(t *testing.T) (*decisionSyncRig, *fakeFollowUpTargets) {
	t.Helper()
	r, _ := newAnswerRig(t, true)
	r.dcfg.FollowUps = []config.AutonomousDecisionFollowUp{
		{Name: "kickoff", Kind: config.DecisionFollowUpStartTemplate, Target: "tpl-build"},
		{Name: "release", Kind: config.DecisionFollowUpOpenAutomaton, Target: "held-1"},
	}
	fake := &fakeFollowUpTargets{status: map[string]string{"held-1": "needs_review"}}
	r.svc.SetFollowUpTargets(fake, fake)
	return r, fake
}

func answerRecord(t *testing.T, r *decisionSyncRig, questionID string) {
	t.Helper()
	if _, err := r.questions.Answer(questionID, Answer{Actor: "alice", Surface: "pwa", ChosenOptionID: "sqlite", Quote: "SQLite, please"}); err != nil {
		t.Fatalf("Answer: %v", err)
	}
}

func TestDecisionFollowUp_StartTemplateRunsThroughFakeAndRecordsDone(t *testing.T) {
	r, fake := followUpRig(t)
	v := openedRecord(t, r)
	if err := r.svc.RequestFollowUp(v.QuestionID, "kickoff"); err != nil {
		t.Fatalf("RequestFollowUp: %v", err)
	}
	if fake.calls() != 0 {
		t.Fatalf("requesting must not run anything; %d calls", fake.calls())
	}
	answerRecord(t, r, v.QuestionID)

	rec, _ := r.records.Get(v.ID)
	fu := rec.FollowUp
	if fu == nil || fu.Status != DecisionFollowUpDone || fu.Kind != config.DecisionFollowUpStartTemplate || fu.Target != "tpl-build" || fu.Name != "kickoff" {
		t.Fatalf("follow_up = %+v, want kickoff done as start_template tpl-build", fu)
	}
	if !strings.Contains(fu.Result, "new-automaton-1") || strings.ContainsAny(fu.Result, "\r\n") || fu.At == nil {
		t.Fatalf("result %q at %v, want a one-line result naming the new Automaton and a time", fu.Result, fu.At)
	}
	if len(fake.templates) != 1 || fake.templates[0] != "tpl-build" {
		t.Fatalf("templates started = %v, want [tpl-build]", fake.templates)
	}
}

func TestDecisionFollowUp_OpenAutomatonOnHeldAutomatonRecordsDone(t *testing.T) {
	r, fake := followUpRig(t)
	v := openedRecord(t, r)
	if err := r.svc.RequestFollowUp(v.QuestionID, "release"); err != nil {
		t.Fatalf("RequestFollowUp: %v", err)
	}
	answerRecord(t, r, v.QuestionID)

	rec, _ := r.records.Get(v.ID)
	if rec.FollowUp == nil || rec.FollowUp.Status != DecisionFollowUpDone || rec.FollowUp.Kind != config.DecisionFollowUpOpenAutomaton {
		t.Fatalf("follow_up = %+v, want open_automaton done", rec.FollowUp)
	}
	if len(fake.approved) != 1 || fake.approved[0] != "held-1" || len(fake.ran) != 1 || fake.ran[0] != "held-1" {
		t.Fatalf("approved %v ran %v, want held-1 approved then run once", fake.approved, fake.ran)
	}
}

func TestDecisionFollowUp_OpenAutomatonOnRunningAutomatonRecordsSkipped(t *testing.T) {
	r, fake := followUpRig(t)
	fake.status["held-1"] = "running"
	v := openedRecord(t, r)
	if err := r.svc.RequestFollowUp(v.QuestionID, "release"); err != nil {
		t.Fatalf("RequestFollowUp: %v", err)
	}
	answerRecord(t, r, v.QuestionID)

	rec, _ := r.records.Get(v.ID)
	if rec.FollowUp == nil || rec.FollowUp.Status != DecisionFollowUpSkipped || !strings.Contains(rec.FollowUp.Result, "running") {
		t.Fatalf("follow_up = %+v, want skipped with the running state in the result", rec.FollowUp)
	}
	if fake.calls() != 0 {
		t.Fatalf("a running Automaton must not be approved or run; %d calls", fake.calls())
	}
}

func TestDecisionFollowUp_EmptyListRunsNothingAndRefusesRequest(t *testing.T) {
	r, fake := followUpRig(t)
	r.dcfg.FollowUps = nil
	v := openedRecord(t, r)

	err := r.svc.RequestFollowUp(v.QuestionID, "kickoff")
	var unknown *ErrUnknownFollowUp
	if !errors.As(err, &unknown) || !strings.Contains(err.Error(), "no follow-ups are configured") {
		t.Fatalf("err = %v, want *ErrUnknownFollowUp saying none are configured", err)
	}
	answerRecord(t, r, v.QuestionID)

	rec, _ := r.records.Get(v.ID)
	if rec.FollowUp != nil {
		t.Fatalf("follow_up = %+v, want none stored", rec.FollowUp)
	}
	if fake.calls() != 0 {
		t.Fatalf("nothing may run with an empty list; %d calls", fake.calls())
	}
}

func TestDecisionFollowUp_UnknownNameRefusedNamingAllowedNames(t *testing.T) {
	r, fake := followUpRig(t)
	v := openedRecord(t, r)

	err := r.svc.RequestFollowUp(v.QuestionID, "launch-rockets")
	var unknown *ErrUnknownFollowUp
	if !errors.As(err, &unknown) {
		t.Fatalf("err = %v, want *ErrUnknownFollowUp", err)
	}
	for _, want := range []string{"launch-rockets", "kickoff", "release"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("message %q should name %q", err.Error(), want)
		}
	}
	rec, _ := r.records.Get(v.ID)
	if rec.FollowUp != nil || fake.calls() != 0 {
		t.Fatalf("a refused request must store and run nothing (follow_up %+v, %d calls)", rec.FollowUp, fake.calls())
	}
}

func TestDecisionFollowUp_SecondApplyDoesNotRunTwice(t *testing.T) {
	r, fake := followUpRig(t)
	v := openedRecord(t, r)
	if err := r.svc.RequestFollowUp(v.QuestionID, "kickoff"); err != nil {
		t.Fatalf("RequestFollowUp: %v", err)
	}
	answerRecord(t, r, v.QuestionID)

	rec, _ := r.records.Get(v.ID)
	q, _ := r.questions.Get(v.QuestionID)
	a, _ := r.questions.CurrentAnswer(v.QuestionID)
	r.svc.runFollowUp(rec, q, a) // the effects run again for the same record
	r.svc.runFollowUp(rec, q, a)
	r.svc.Reconcile()

	if len(fake.templates) != 1 {
		t.Fatalf("the template was started %d times, want exactly 1", len(fake.templates))
	}
}

func TestDecisionFollowUp_RemovedFromConfigBeforeAnswerRecordsSkipped(t *testing.T) {
	r, fake := followUpRig(t)
	v := openedRecord(t, r)
	if err := r.svc.RequestFollowUp(v.QuestionID, "kickoff"); err != nil {
		t.Fatalf("RequestFollowUp: %v", err)
	}
	r.dcfg.FollowUps = nil // the operator removed it before answering
	answerRecord(t, r, v.QuestionID)

	rec, _ := r.records.Get(v.ID)
	if rec.FollowUp == nil || rec.FollowUp.Status != DecisionFollowUpSkipped || !strings.Contains(rec.FollowUp.Result, "no longer") {
		t.Fatalf("follow_up = %+v, want skipped saying it is no longer configured", rec.FollowUp)
	}
	if fake.calls() != 0 {
		t.Fatalf("a removed follow-up must not run; %d calls", fake.calls())
	}
}

func TestDecisionFollowUp_StarterErrorRecordsError(t *testing.T) {
	r, fake := followUpRig(t)
	fake.startErr = errors.New("template tpl-build not found")
	v := openedRecord(t, r)
	if err := r.svc.RequestFollowUp(v.QuestionID, "kickoff"); err != nil {
		t.Fatalf("RequestFollowUp: %v", err)
	}
	answerRecord(t, r, v.QuestionID)

	rec, _ := r.records.Get(v.ID)
	if rec.FollowUp == nil || rec.FollowUp.Status != DecisionFollowUpError || !strings.Contains(rec.FollowUp.Result, "not found") {
		t.Fatalf("follow_up = %+v, want error carrying the cause", rec.FollowUp)
	}
	if rec.State != DecisionRecordAnswered {
		t.Fatalf("a failed follow-up must not undo the answer; state = %s", rec.State)
	}
}

func TestDecisionFollowUp_NoRequestRunsNothing(t *testing.T) {
	r, fake := followUpRig(t)
	v := openedRecord(t, r)
	answerRecord(t, r, v.QuestionID)

	rec, _ := r.records.Get(v.ID)
	if rec.FollowUp != nil || fake.calls() != 0 {
		t.Fatalf("no request: follow_up %+v, %d calls, want none", rec.FollowUp, fake.calls())
	}
}

func TestDecisionFollowUp_RequestForQuestionWithoutRecordIsRefused(t *testing.T) {
	r, _ := followUpRig(t)
	err := r.svc.RequestFollowUp("q-does-not-exist", "kickoff")
	if !errors.Is(err, ErrNoDecisionRecord) {
		t.Fatalf("err = %v, want ErrNoDecisionRecord", err)
	}
}
