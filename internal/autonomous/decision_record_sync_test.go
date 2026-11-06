package autonomous

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/config"
)

// decisionSyncRig holds the real stores under t.TempDir() and the switches
// the service reads live.
type decisionSyncRig struct {
	svc       *DecisionRecords
	records   *DecisionRecordStore
	questions *QuestionStore
	dcfg      config.AutonomousDecisionsConfig
	qcfg      config.AutonomousQuestionsConfig
	advance   func(time.Duration)
}

func newDecisionSyncRig(t *testing.T) *decisionSyncRig {
	t.Helper()
	_, qs, _, advance := newQuestionStoreTest(t)
	rs, err := NewDecisionRecordStore(filepath.Join(t.TempDir(), "decision_records.jsonl"))
	if err != nil {
		t.Fatalf("NewDecisionRecordStore: %v", err)
	}
	wireDecisionStoreTest(rs)
	ts := time.Date(2026, 10, 31, 9, 0, 0, 0, time.UTC)
	rs.now = func() time.Time { return ts }
	r := &decisionSyncRig{records: rs, questions: qs, advance: func(d time.Duration) { ts = ts.Add(d); advance(d) }}
	r.qcfg.Enabled = true
	r.svc = NewDecisionRecords(rs, qs,
		func() config.AutonomousDecisionsConfig { return r.dcfg },
		func() config.AutonomousQuestionsConfig { return r.qcfg },
		func() time.Time { return ts })
	return r
}

// clearRecord is a record that passes the default clarity rules.
func clearRecord() DecisionRecord {
	r := validDecisionRecord()
	r.Situation = "The service keeps its state in memory and loses it on every restart, " +
		"so the operator has to re-enter the same settings each time it comes back."
	return r
}

// unclearRecord has no trade-offs and a very short body.
func unclearRecord() DecisionRecord {
	return DecisionRecord{
		Title:     "Pick one",
		Situation: "Short.",
		Deciding:  "Which one.",
		Options:   []DecisionOption{{ID: "a", Label: "Option A"}, {ID: "b", Label: "Option B"}},
		Default:   "hold",
	}
}

func setClarityMode(r *decisionSyncRig, mode string) { r.dcfg.ClarityMode = &mode }

func (r *decisionSyncRig) openQuestions(t *testing.T, recordID string) []Question {
	t.Helper()
	var out []Question
	for _, q := range r.questions.List(QuestionFilter{Limit: maxQuestionListLimit}) {
		if q.DecisionRecordID == recordID {
			out = append(out, q)
		}
	}
	return out
}

func TestDecisionSync_CreateRaisesOneQuestionBuiltFromRecord(t *testing.T) {
	r := newDecisionSyncRig(t)
	view, findings, err := r.svc.Create(clearRecord(), DecisionSourceREST, "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("findings = %+v, want none for a clear record", findings)
	}
	if view.State != DecisionRecordOpen || view.QuestionID == "" {
		t.Fatalf("view = %+v, want an open record linked to its question", view)
	}
	qs := r.openQuestions(t, view.ID)
	if len(qs) != 1 {
		t.Fatalf("%d questions for the record, want exactly 1", len(qs))
	}
	q := qs[0]
	rec, _ := r.records.Get(view.ID)
	want := BuildQuestionFromDecision(rec)
	if q.ID != view.QuestionID || q.Text != want.Text || q.Label != want.Label || q.Default != want.Default ||
		q.Context != want.Context || q.PRDID != want.PRDID || len(q.Options) != len(want.Options) {
		t.Fatalf("question %+v is not the one built from the record %+v", q, want)
	}
	if !strings.HasPrefix(q.Text, rec.Title+"\n\n") || !strings.Contains(q.Text, "\n\nDeciding: "+rec.Deciding) {
		t.Fatalf("question text %q does not follow the record layout", q.Text)
	}
	if q.State != QuestionOpen || view.QuestionState != QuestionOpen {
		t.Fatalf("question state = %s / view %s, want open", q.State, view.QuestionState)
	}
}

func TestDecisionSync_DoubleReconcileKeepsOneQuestion(t *testing.T) {
	r := newDecisionSyncRig(t)
	view, _, err := r.svc.Create(clearRecord(), DecisionSourceMCP, "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	for i := 0; i < 3; i++ {
		rep := r.svc.Reconcile()
		if rep.Changed() || len(rep.Errors) > 0 {
			t.Fatalf("reconcile %d changed or failed on an in-step record: %+v", i, rep)
		}
	}
	if n := len(r.openQuestions(t, view.ID)); n != 1 {
		t.Fatalf("%d questions after repeated reconcile, want 1", n)
	}
}

func TestDecisionSync_CrashBetweenRaiseAndLinkLeavesOneQuestion(t *testing.T) {
	r := newDecisionSyncRig(t)
	// The record is stored and its question raised, but the daemon stopped
	// before SetQuestionID ran.
	rec, err := r.records.Create(clearRecord())
	if err != nil {
		t.Fatalf("records.Create: %v", err)
	}
	raised, ok, err := r.questions.RaiseUnlessDecisionRecord(BuildQuestionFromDecision(rec))
	if err != nil || !ok {
		t.Fatalf("raise: %v ok=%v", err, ok)
	}
	for i := 0; i < 2; i++ {
		rep := r.svc.Reconcile()
		if len(rep.Errors) > 0 {
			t.Fatalf("reconcile errors: %v", rep.Errors)
		}
		if i == 0 && rep.Linked != 1 {
			t.Fatalf("first reconcile report = %+v, want the existing question linked", rep)
		}
	}
	if n := len(r.openQuestions(t, rec.ID)); n != 1 {
		t.Fatalf("%d questions for the record after the crash repair, want 1", n)
	}
	got, _ := r.records.Get(rec.ID)
	if got.QuestionID != raised.ID {
		t.Fatalf("record question id = %q, want the raised %q", got.QuestionID, raised.ID)
	}
}

func TestDecisionSync_ReconcileRaisesMissingQuestionAndRepairsText(t *testing.T) {
	r := newDecisionSyncRig(t)
	// An open record with no question at all.
	rec, err := r.records.Create(clearRecord())
	if err != nil {
		t.Fatalf("records.Create: %v", err)
	}
	if rep := r.svc.Reconcile(); rep.Raised != 1 {
		t.Fatalf("report = %+v, want one question raised", rep)
	}
	linked, _ := r.records.Get(rec.ID)
	if linked.QuestionID == "" {
		t.Fatal("record is not linked after the raise")
	}
	// The question text drifts away from the record.
	drift := "Something else entirely"
	if _, err := r.questions.Update(linked.QuestionID, QuestionPatch{Text: &drift}, "someone"); err != nil {
		t.Fatalf("questions.Update: %v", err)
	}
	if rep := r.svc.Reconcile(); rep.Updated != 1 {
		t.Fatalf("report = %+v, want the drifted question updated", rep)
	}
	q, _ := r.questions.Get(linked.QuestionID)
	if want := BuildQuestionFromDecision(linked).Text; q.Text != want {
		t.Fatalf("question text = %q, want %q", q.Text, want)
	}
	if n := len(r.openQuestions(t, rec.ID)); n != 1 {
		t.Fatalf("%d questions, want 1", n)
	}
}

func TestDecisionSync_EditOfOpenRecordUpdatesSameQuestion(t *testing.T) {
	r := newDecisionSyncRig(t)
	view, _, err := r.svc.Create(clearRecord(), DecisionSourceCLI, "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	edit := clearRecord()
	edit.Title = "Pick the storage engine for the service"
	edit.Options = append(edit.Options, DecisionOption{ID: "pg", Label: "Use Postgres", Tradeoff: "strong, needs a server"})
	got, _, err := r.svc.Update(view.ID, edit)
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if got.QuestionID != view.QuestionID {
		t.Fatalf("question id changed from %s to %s", view.QuestionID, got.QuestionID)
	}
	q, _ := r.questions.Get(view.QuestionID)
	if !strings.HasPrefix(q.Text, edit.Title+"\n\n") || len(q.Options) != 3 {
		t.Fatalf("question not updated: text %q, %d options", q.Text, len(q.Options))
	}
	if n := len(r.openQuestions(t, view.ID)); n != 1 {
		t.Fatalf("%d questions after the edit, want 1", n)
	}
	if got.ContentSHA256 == view.ContentSHA256 {
		t.Fatal("content hash did not change with the content")
	}
}

func TestDecisionSync_EditOfAnsweredRecordRefused(t *testing.T) {
	r := newDecisionSyncRig(t)
	view, _, err := r.svc.Create(clearRecord(), DecisionSourceREST, "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	ans, err := r.questions.Answer(view.QuestionID, Answer{Actor: "dmz", Surface: "pwa", ChosenOptionID: "sqlite", Quote: "go with sqlite"})
	if err != nil {
		t.Fatalf("Answer: %v", err)
	}

	// The question is answered first; the record is not yet marked.
	edit := clearRecord()
	edit.Title = "A different title"
	if _, _, err := r.svc.Update(view.ID, edit); !errors.Is(err, ErrDecisionRecordClosed) {
		t.Fatalf("edit after the question was answered: err = %v, want ErrDecisionRecordClosed", err)
	}

	// Now the record is answered too.
	when := ans.CreatedAt
	if _, err := r.records.RecordAnswer(view.ID, DecisionAnswer{AnswerID: ans.ID, ChosenOptionID: "sqlite", Quote: "go with sqlite", Actor: "dmz", Surface: "pwa", AnsweredAt: when}); err != nil {
		t.Fatalf("RecordAnswer: %v", err)
	}
	if _, _, err := r.svc.Update(view.ID, edit); !errors.Is(err, ErrDecisionRecordClosed) {
		t.Fatalf("edit of an answered record: err = %v, want ErrDecisionRecordClosed", err)
	}
	q, _ := r.questions.Get(view.QuestionID)
	if strings.HasPrefix(q.Text, "A different title") {
		t.Fatal("the answered question was changed")
	}

	// The view joins the answer from the question store.
	v, ok := r.svc.View(view.ID)
	if !ok || v.QuestionState != QuestionAnswered || v.AnsweredBy != "dmz" || v.AnsweredSurface != "pwa" ||
		v.AnsweredQuote != "go with sqlite" || v.AnsweredAt == nil {
		t.Fatalf("view = %+v, want the answer joined from the question store", v)
	}
}

func TestDecisionSync_WithdrawWithdrawsOpenQuestion(t *testing.T) {
	r := newDecisionSyncRig(t)
	view, _, err := r.svc.Create(clearRecord(), DecisionSourceREST, "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	got, err := r.svc.Withdraw(view.ID, "dmz", "no longer needed")
	if err != nil {
		t.Fatalf("Withdraw: %v", err)
	}
	if got.State != DecisionRecordWithdrawn || got.QuestionState != QuestionWithdrawn {
		t.Fatalf("view = state %s / question %s, want both withdrawn", got.State, got.QuestionState)
	}
	q, _ := r.questions.Get(view.QuestionID)
	if q.State != QuestionWithdrawn {
		t.Fatalf("question state = %s, want withdrawn", q.State)
	}
	// Reconcile leaves a withdrawn record alone and raises nothing.
	if rep := r.svc.Reconcile(); rep.Changed() {
		t.Fatalf("reconcile changed a withdrawn record: %+v", rep)
	}
	if n := len(r.openQuestions(t, view.ID)); n != 1 {
		t.Fatalf("%d questions, want the one withdrawn question only", n)
	}
	if _, err := r.svc.Withdraw("dr-nope", "dmz", ""); !errors.Is(err, ErrDecisionRecordNotFound) {
		t.Fatalf("withdraw of an unknown record: err = %v, want ErrDecisionRecordNotFound", err)
	}
}

func TestDecisionSync_QuestionWithdrawnOutsideMarksRecordWithdrawn(t *testing.T) {
	r := newDecisionSyncRig(t)
	view, _, err := r.svc.Create(clearRecord(), DecisionSourceREST, "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := r.questions.Withdraw(view.QuestionID, "operator", "asked in the queue"); err != nil {
		t.Fatalf("questions.Withdraw: %v", err)
	}
	rep := r.svc.Reconcile()
	if rep.Withdrawn != 1 {
		t.Fatalf("report = %+v, want one record marked withdrawn", rep)
	}
	got, _ := r.records.Get(view.ID)
	if got.State != DecisionRecordWithdrawn {
		t.Fatalf("record state = %s, want withdrawn", got.State)
	}
	if rep := r.svc.Reconcile(); rep.Changed() {
		t.Fatalf("second reconcile changed something: %+v", rep)
	}
}

func TestDecisionSync_RefuseModeStoresNothingAndReturnsFindings(t *testing.T) {
	r := newDecisionSyncRig(t)
	setClarityMode(r, QualityModeRefuse)
	_, _, err := r.svc.Create(unclearRecord(), DecisionSourceREST, "")
	var refused *ErrDecisionClarityRefused
	if !errors.As(err, &refused) {
		t.Fatalf("err = %v, want ErrDecisionClarityRefused", err)
	}
	if len(refused.Findings) == 0 {
		t.Fatal("the refusal carries no findings")
	}
	if n := len(r.records.List(DecisionRecordFilter{})); n != 0 {
		t.Fatalf("%d records stored, want none", n)
	}
	if n := len(r.questions.List(QuestionFilter{})); n != 0 {
		t.Fatalf("%d questions raised, want none", n)
	}

	// An edit that turns a clear record unclear is refused the same way and
	// leaves the record as it was.
	ok, _, err := r.svc.Create(clearRecord(), DecisionSourceREST, "")
	if err != nil {
		t.Fatalf("Create of a clear record in refuse mode: %v", err)
	}
	if _, _, err := r.svc.Update(ok.ID, unclearRecord()); !errors.As(err, &refused) {
		t.Fatalf("unclear edit: err = %v, want ErrDecisionClarityRefused", err)
	}
	after, _ := r.records.Get(ok.ID)
	if after.ContentSHA256 != ok.ContentSHA256 {
		t.Fatal("a refused edit changed the stored record")
	}
}

func TestDecisionSync_WarnModeStoresRecordAndQuestionWithFindings(t *testing.T) {
	r := newDecisionSyncRig(t) // default clarity mode is warn
	view, findings, err := r.svc.Create(unclearRecord(), DecisionSourceREST, "")
	if err != nil {
		t.Fatalf("Create in warn mode: %v", err)
	}
	if len(findings) == 0 {
		t.Fatal("warn mode returned no findings for an unclear record")
	}
	if view.ID == "" || view.QuestionID == "" {
		t.Fatalf("view = %+v, want the record stored and linked", view)
	}
	if n := len(r.openQuestions(t, view.ID)); n != 1 {
		t.Fatalf("%d questions, want 1", n)
	}

	// Mode off finds nothing.
	setClarityMode(r, QualityModeOff)
	_, findings, err = r.svc.Create(unclearRecord(), DecisionSourceREST, "")
	if err != nil || len(findings) != 0 {
		t.Fatalf("mode off: findings %+v err %v, want none", findings, err)
	}
}

func TestDecisionSync_SwitchesOffReturnUnavailable(t *testing.T) {
	r := newDecisionSyncRig(t)
	off := false
	r.dcfg.Enabled = &off
	r.qcfg.Enabled = false

	_, _, err := r.svc.Create(clearRecord(), DecisionSourceREST, "")
	var un *ErrDecisionRecordsUnavailable
	if !errors.As(err, &un) {
		t.Fatalf("err = %v, want ErrDecisionRecordsUnavailable", err)
	}
	if len(un.Switches) != 2 || !strings.Contains(err.Error(), "autonomous.decisions.enabled") ||
		!strings.Contains(err.Error(), "autonomous.questions.enabled") {
		t.Fatalf("error %q does not name both switches", err)
	}

	// One switch at a time is named alone.
	on := true
	r.dcfg.Enabled = &on
	if _, _, err := r.svc.Create(clearRecord(), DecisionSourceREST, ""); !errors.As(err, &un) ||
		len(un.Switches) != 1 || un.Switches[0] != "autonomous.questions.enabled" {
		t.Fatalf("questions off: err = %v, want only the questions switch", err)
	}
	r.qcfg.Enabled = true
	r.dcfg.Enabled = &off
	if _, _, err := r.svc.Create(clearRecord(), DecisionSourceREST, ""); !errors.As(err, &un) ||
		len(un.Switches) != 1 || un.Switches[0] != "autonomous.decisions.enabled" {
		t.Fatalf("decisions off: err = %v, want only the decisions switch", err)
	}

	// Reconcile does nothing while a switch is off, even for a stored record.
	if _, err := r.records.Create(clearRecord()); err != nil {
		t.Fatalf("records.Create: %v", err)
	}
	if rep := r.svc.Reconcile(); rep.Changed() || len(r.questions.List(QuestionFilter{})) != 0 {
		t.Fatalf("reconcile acted while a switch was off: %+v", rep)
	}
	if n := len(r.records.List(DecisionRecordFilter{})); n != 1 {
		t.Fatalf("%d records, want only the one stored directly", n)
	}
}

func TestDecisionSync_ManagerTickReconciles(t *testing.T) {
	r := newDecisionSyncRig(t)
	rec, err := r.records.Create(clearRecord())
	if err != nil {
		t.Fatalf("records.Create: %v", err)
	}
	m := &Manager{}
	m.DecisionRecordTick(context.Background()) // nothing attached: no panic
	m.SetDecisionRecords(r.svc)
	m.DecisionRecordTick(context.Background())
	got, _ := r.records.Get(rec.ID)
	if got.QuestionID == "" {
		t.Fatal("the tick did not raise the missing question")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	m.DecisionRecordTick(ctx) // cancelled: returns without error
}
