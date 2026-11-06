// decision_follow_up_answer_test.go — the optional follow_up field of
// POST /api/questions/{id}/answer. The real question store, decision record
// store and decision record service stand behind the handler, as in the
// daemon; only the template starter is a fake.

package server

import (
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
)

// followUpStarter records the templates the follow-up started.
type followUpStarter struct {
	mu        sync.Mutex
	templates []string
}

func (f *followUpStarter) InstantiateTemplate(templateID string, _ map[string]string, _ string) (any, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.templates = append(f.templates, templateID)
	return nil, nil
}

func (f *followUpStarter) started() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.templates...)
}

type followUpRig struct {
	s       *Server
	questns *autonomous.QuestionStore
	records *autonomous.DecisionRecordStore
	svc     *autonomous.DecisionRecords
	starter *followUpStarter
}

func newFollowUpRig(t *testing.T, followUps []config.AutonomousDecisionFollowUp) *followUpRig {
	t.Helper()
	s, qs := newQuestionsRESTServer(t)
	rs, err := autonomous.NewDecisionRecordStore(filepath.Join(t.TempDir(), "decision_records.jsonl"))
	if err != nil {
		t.Fatalf("NewDecisionRecordStore: %v", err)
	}
	dcfg := config.AutonomousDecisionsConfig{FollowUps: followUps}
	qcfg := config.AutonomousQuestionsConfig{Enabled: true}
	svc := autonomous.NewDecisionRecords(rs, qs,
		func() config.AutonomousDecisionsConfig { return dcfg },
		func() config.AutonomousQuestionsConfig { return qcfg },
		nil)
	svc.ChainQuestionHook()
	starter := &followUpStarter{}
	svc.SetFollowUpTargets(starter, nil)
	s.SetDecisionFollowUps(svc)
	return &followUpRig{s: s, questns: qs, records: rs, svc: svc, starter: starter}
}

// openRecord creates a clear decision record and returns it with its question id.
func (r *followUpRig) openRecord(t *testing.T) autonomous.DecisionRecordView {
	t.Helper()
	v, _, err := r.svc.Create(autonomous.DecisionRecord{
		Title: "Which storage should the service use?",
		Situation: "The service keeps its state in memory and loses it on every restart, " +
			"so the operator has to re-enter the same settings each time it comes back.",
		Deciding: "where the settings are kept",
		Options: []autonomous.DecisionOption{
			{ID: "sqlite", Label: "SQLite", Tradeoff: "one more dependency, survives restarts"},
			{ID: "files", Label: "Plain files", Tradeoff: "no dependency, easy to corrupt by hand"},
		},
		Default: "hold",
	}, autonomous.DecisionSourceREST, "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if v.QuestionID == "" {
		t.Fatal("record has no question")
	}
	return v
}

var oneFollowUp = []config.AutonomousDecisionFollowUp{
	{Name: "kickoff", Kind: config.DecisionFollowUpStartTemplate, Target: "tpl-build"},
}

func TestDecisionFollowUpAnswer_ValidFollowUpStoredAndAnswerRecorded(t *testing.T) {
	r := newFollowUpRig(t, oneFollowUp)
	v := r.openRecord(t)

	rr := doAnswer(t, r.s, v.QuestionID, map[string]any{
		"chosen_option_id": "sqlite", "quote": "SQLite, then start the build", "follow_up": "kickoff",
	})
	if rr.Code != http.StatusCreated {
		t.Fatalf("want 201, got %d body=%s", rr.Code, rr.Body.String())
	}
	rec, _ := r.records.Get(v.ID)
	if rec.State != autonomous.DecisionRecordAnswered || rec.Answer == nil || rec.Answer.ChosenOptionID != "sqlite" {
		t.Fatalf("record state %s answer %+v, want answered with sqlite", rec.State, rec.Answer)
	}
	fu := rec.FollowUp
	if fu == nil || !fu.Requested || fu.Name != "kickoff" || fu.Status != autonomous.DecisionFollowUpDone || fu.Target != "tpl-build" {
		t.Fatalf("follow_up = %+v, want kickoff done on tpl-build", fu)
	}
	if got := r.starter.started(); len(got) != 1 || got[0] != "tpl-build" {
		t.Fatalf("templates started = %v, want [tpl-build]", got)
	}
}

func TestDecisionFollowUpAnswer_UnknownFollowUpIsBadRequestAndNoAnswerStored(t *testing.T) {
	r := newFollowUpRig(t, oneFollowUp)
	v := r.openRecord(t)

	rr := doAnswer(t, r.s, v.QuestionID, map[string]any{"chosen_option_id": "sqlite", "follow_up": "launch-rockets"})
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d body=%s", rr.Code, rr.Body.String())
	}
	if body := rr.Body.String(); !strings.Contains(body, "launch-rockets") || !strings.Contains(body, "kickoff") {
		t.Fatalf("body %s should name the unknown follow-up and the allowed one", body)
	}
	q, _ := r.questns.Get(v.QuestionID)
	if q.State != autonomous.QuestionOpen || r.questns.AnswerCount(v.QuestionID) != 0 {
		t.Fatalf("question state %s with %d answers, want open with none", q.State, r.questns.AnswerCount(v.QuestionID))
	}
	rec, _ := r.records.Get(v.ID)
	if rec.State != autonomous.DecisionRecordOpen || rec.Answer != nil || rec.FollowUp != nil {
		t.Fatalf("record = %+v, want untouched", rec)
	}
	if len(r.starter.started()) != 0 {
		t.Fatalf("nothing may start; got %v", r.starter.started())
	}
}

func TestDecisionFollowUpAnswer_NoFollowUpLeavesOldBehaviourUnchanged(t *testing.T) {
	r := newFollowUpRig(t, oneFollowUp)
	v := r.openRecord(t)

	rr := doAnswer(t, r.s, v.QuestionID, map[string]any{"chosen_option_id": "files", "quote": "files for now"})
	if rr.Code != http.StatusCreated {
		t.Fatalf("want 201, got %d body=%s", rr.Code, rr.Body.String())
	}
	rec, _ := r.records.Get(v.ID)
	if rec.State != autonomous.DecisionRecordAnswered || rec.FollowUp != nil {
		t.Fatalf("state %s follow_up %+v, want answered with no follow-up", rec.State, rec.FollowUp)
	}
	if len(r.starter.started()) != 0 {
		t.Fatalf("no follow_up asked, nothing may start; got %v", r.starter.started())
	}
	// An ordinary question without a decision record answers as before.
	q := raiseQ(t, r.s)
	if rr := doAnswer(t, r.s, q.ID, map[string]any{"chosen_option_id": "yes"}); rr.Code != http.StatusCreated {
		t.Fatalf("ordinary question: want 201, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestDecisionFollowUpAnswer_QuestionWithoutDecisionRecordIsBadRequest(t *testing.T) {
	r := newFollowUpRig(t, oneFollowUp)
	q := raiseQ(t, r.s) // an ordinary question: no decision record owns it

	rr := doAnswer(t, r.s, q.ID, map[string]any{"chosen_option_id": "yes", "follow_up": "kickoff"})
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d body=%s", rr.Code, rr.Body.String())
	}
	got, _ := r.questns.Get(q.ID)
	if got.State != autonomous.QuestionOpen {
		t.Fatalf("question state = %s, want it still open", got.State)
	}
}

func TestDecisionFollowUpAnswer_NotWiredIsServiceUnavailable(t *testing.T) {
	s, _ := newQuestionsRESTServer(t)
	q := raiseQ(t, s)
	rr := doAnswer(t, s, q.ID, map[string]any{"chosen_option_id": "yes", "follow_up": "kickoff"})
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("want 503, got %d body=%s", rr.Code, rr.Body.String())
	}
	got, _ := s.questionStore.Get(q.ID)
	if got.State != autonomous.QuestionOpen {
		t.Fatalf("question state = %s, want it still open", got.State)
	}
}
