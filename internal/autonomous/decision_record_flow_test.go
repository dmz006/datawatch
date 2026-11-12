package autonomous

// End-to-end tests of the decision record path, driven through the
// DecisionRecords service with real stores under t.TempDir(), a fake clock, a
// fake document root and a fake follow-up runner. Each test pins one path, so
// a regression in that path fails that named test and no other. The rigs and
// fixtures come from the other decision record tests of the package
// (decision_record_sync_test.go, decision_record_answer_test.go,
// decision_record_docline_test.go, decision_record_followup_test.go,
// decision_record_folder_test.go and decision_gate_executor_test.go).

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/config"
)

// flowStoreStamp is the time the rig's decision record store reads from the
// fake clock before anything advances it.
var flowStoreStamp = time.Date(2026, 10, 31, 9, 0, 0, 0, time.UTC)

// flowTrackerLike builds an identifier that looks like an internal tracker
// number without writing one in the source.
func flowTrackerLike() string { return "B" + "L" + "123" }

// flowExpect reports one field that differs.
func flowExpect(t *testing.T, field string, got, want any) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%s = %#v, want %#v", field, got, want)
	}
}

func flowQuestion(t *testing.T, r *decisionSyncRig, id string) Question {
	t.Helper()
	q, ok := r.questions.Get(id)
	if !ok {
		t.Fatalf("question %q is not in the question store", id)
	}
	return q
}

func flowRecord(t *testing.T, r *decisionSyncRig, id string) DecisionRecord {
	t.Helper()
	rec, ok := r.records.Get(id)
	if !ok {
		t.Fatalf("record %q is not in the record store", id)
	}
	return rec
}

// flowNothingStored fails when the rig holds any record or question.
func flowNothingStored(t *testing.T, r *decisionSyncRig) {
	t.Helper()
	if n := len(r.records.List(DecisionRecordFilter{Limit: maxDecisionListLimit})); n != 0 {
		t.Fatalf("%d records stored, want none", n)
	}
	if n := len(r.questions.List(QuestionFilter{Limit: maxQuestionListLimit})); n != 0 {
		t.Fatalf("%d questions raised, want none", n)
	}
}

func flowReadFile(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// flowOneQuestion fails unless the record owns exactly one question and
// returns it.
func flowOneQuestion(t *testing.T, r *decisionSyncRig, recordID string) Question {
	t.Helper()
	qs := r.openQuestions(t, recordID)
	if len(qs) != 1 {
		t.Fatalf("%d questions for record %s, want exactly 1", len(qs), recordID)
	}
	return qs[0]
}

func flowCodes(fs []QualityFinding) map[string]bool {
	out := map[string]bool{}
	for _, f := range fs {
		out[f.Code] = true
	}
	return out
}

// ---------------------------------------------------------------------------
// Build

func TestDecisionRecordFlow_Build(t *testing.T) {
	r := newDecisionSyncRig(t)
	in := clearRecord()
	in.Label = "storage"
	in.Default = "files"
	view, findings, err := r.svc.Create(in, DecisionSourceMCP, "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("findings = %v, want none for a clear record", qualityCodes(findings))
	}

	rec := flowRecord(t, r, view.ID)
	if !regexp.MustCompile(`^dr-[0-9a-f]{20}$`).MatchString(rec.ID) {
		t.Errorf("record id %q is not dr- plus 20 lowercase hex characters", rec.ID)
	}
	flowExpect(t, "record.state", rec.State, DecisionRecordOpen)
	flowExpect(t, "record.source", rec.Source, DecisionSourceMCP)
	flowExpect(t, "record.source_ref", rec.SourceRef, "")
	flowExpect(t, "record.label", rec.Label, "storage")
	flowExpect(t, "record.links", rec.Links, in.Links)
	flowExpect(t, "record.options", rec.Options, in.Options)
	flowExpect(t, "record.recommendation", rec.Recommendation, in.Recommendation)
	flowExpect(t, "record.default", rec.Default, "files")
	if !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(rec.ContentSHA256) || rec.ContentSHA256 != rec.ContentHash() {
		t.Errorf("content hash %q is not the sha256 of the content fields", rec.ContentSHA256)
	}
	if rec.CreatedAt == nil || !rec.CreatedAt.Equal(flowStoreStamp) {
		t.Errorf("created_at = %v, want the fake clock %v", rec.CreatedAt, flowStoreStamp)
	}
	if rec.Answer != nil || rec.DocChange != nil || rec.FollowUp != nil {
		t.Errorf("a new record carries an answer, document change or follow-up: %+v", rec)
	}

	q := flowOneQuestion(t, r, rec.ID)
	flowExpect(t, "record.question_id", rec.QuestionID, q.ID)
	flowExpect(t, "view.question_state", view.QuestionState, QuestionOpen)
	flowExpect(t, "question.state", q.State, QuestionOpen)
	flowExpect(t, "question.text", q.Text,
		in.Title+"\n\n"+in.Situation+"\n\nDeciding: "+in.Deciding)
	flowExpect(t, "question.label", q.Label, "storage")
	flowExpect(t, "question.asked_by", q.AskedBy, "datawatch")
	flowExpect(t, "question.asker_kind", q.AskerKind, QuestionAskerSystem)
	flowExpect(t, "question.default", q.Default, "files")
	flowExpect(t, "question.blocking", q.Blocking, false)
	flowExpect(t, "question.source", q.Source, QuestionSourceDecisionRecord)
	flowExpect(t, "question.decision_record_id", q.DecisionRecordID, rec.ID)
	flowExpect(t, "question.option count", len(q.Options), 2)
	if len(q.Options) == 2 {
		flowExpect(t, "question.options[0]", q.Options[0],
			QuestionOption{ID: "sqlite", Label: "Use SQLite", Tradeoffs: "simple, one writer at a time"})
		flowExpect(t, "question.options[1]", q.Options[1],
			QuestionOption{ID: "files", Label: "Keep append-only files", Tradeoffs: "no new dependency, slow queries"})
	}
	flowExpect(t, "question.recommendation", q.Recommendation,
		&QuestionRecommendation{OptionID: "sqlite", Rationale: "queries stay fast"})
	// The Automaton link wins; the other links stay on the record only.
	flowExpect(t, "question.context", q.Context, QuestionContextPrd)
	flowExpect(t, "question.prd_id", q.PRDID, "ee660001")
	flowExpect(t, "question.registry_row_key", q.RegistryRowKey, "")
	flowExpect(t, "question.proposal_path", q.ProposalPath, "")

	t.Run("context_follows_the_links", func(t *testing.T) {
		cases := []struct {
			name    string
			links   DecisionLinks
			context string
			prd     string
			row     string
			doc     string
		}{
			{"registry row beats document", DecisionLinks{Doc: "docs/plans/storage.md", RegistryRow: "row-12"}, QuestionContextRegistryRow, "", "row-12", ""},
			{"document alone", DecisionLinks{Doc: "docs/plans/storage.md"}, QuestionContextProposal, "", "", "docs/plans/storage.md"},
			{"no links", DecisionLinks{}, QuestionContextNone, "", "", ""},
		}
		for _, c := range cases {
			rec := clearRecord()
			rec.Links = c.links
			v, _, err := r.svc.Create(rec, DecisionSourceREST, "")
			if err != nil {
				t.Fatalf("%s: Create: %v", c.name, err)
			}
			got := flowQuestion(t, r, v.QuestionID)
			flowExpect(t, c.name+": context", got.Context, c.context)
			flowExpect(t, c.name+": prd_id", got.PRDID, c.prd)
			flowExpect(t, c.name+": registry_row_key", got.RegistryRowKey, c.row)
			flowExpect(t, c.name+": proposal_path", got.ProposalPath, c.doc)
			flowExpect(t, c.name+": links stay on the record", flowRecord(t, r, v.ID).Links, c.links)
		}
	})

	t.Run("blank_label_becomes_decision", func(t *testing.T) {
		v, _, err := r.svc.Create(clearRecord(), DecisionSourceREST, "")
		if err != nil {
			t.Fatalf("Create: %v", err)
		}
		flowExpect(t, "record.label", flowRecord(t, r, v.ID).Label, DecisionDefaultLabel)
		flowExpect(t, "question.label", flowQuestion(t, r, v.QuestionID).Label, DecisionDefaultLabel)
	})
}

// ---------------------------------------------------------------------------
// Validate

func TestDecisionRecordFlow_Validate(t *testing.T) {
	many := func(n int) []DecisionOption {
		var out []DecisionOption
		for i := 0; i < n; i++ {
			id := string(rune('a' + i))
			out = append(out, DecisionOption{ID: id, Label: "option " + id, Tradeoff: "a trade-off"})
		}
		return out
	}
	cases := []struct {
		name   string
		mutate func(*DecisionRecord)
		want   string
	}{
		{"missing title", func(r *DecisionRecord) { r.Title = "" }, "title is required"},
		{"blank situation", func(r *DecisionRecord) { r.Situation = "  \n " }, "situation is required"},
		{"missing deciding", func(r *DecisionRecord) { r.Deciding = "" }, "deciding is required"},
		{"one option", func(r *DecisionRecord) { r.Options = r.Options[:1]; r.Recommendation = nil }, "at least 2 options"},
		{"too many options", func(r *DecisionRecord) { r.Options = many(21); r.Recommendation = nil }, "21 options exceed the cap of 20"},
		{"repeated option id", func(r *DecisionRecord) { r.Options[1].ID = "sqlite" }, `option id "sqlite" is repeated`},
		{"empty option id", func(r *DecisionRecord) { r.Options[1].ID = "" }, "option 2 has an empty id"},
		{"empty option label", func(r *DecisionRecord) { r.Options[0].Label = " " }, "option 1 has an empty label"},
		{"recommendation names unknown option", func(r *DecisionRecord) { r.Recommendation.OptionID = "mongo" }, `recommendation names option "mongo"`},
		{"recommendation without reason", func(r *DecisionRecord) { r.Recommendation.Reason = "" }, "recommendation needs a reason"},
		{"missing default", func(r *DecisionRecord) { r.Default = "" }, "default is required"},
		{"default is not an option", func(r *DecisionRecord) { r.Default = "mongo" }, `default "mongo" is neither an option nor hold`},
		{"absolute document path", func(r *DecisionRecord) { r.Links.Doc = "/etc/passwd" }, "must be relative"},
		{"document path with parent segment", func(r *DecisionRecord) { r.Links.Doc = "docs/../../x.md" }, "'..'"},
		{"label too long", func(r *DecisionRecord) { r.Label = strings.Repeat("l", 65) }, "label is 65 characters; the cap is 64"},
		{"label with control characters", func(r *DecisionRecord) { r.Label = "bad\x07label" }, "label contains control characters"},
	}
	r := newDecisionSyncRig(t)
	for _, c := range cases {
		rec := clearRecord()
		c.mutate(&rec)
		_, _, err := r.svc.Create(rec, DecisionSourceREST, "")
		if !errors.Is(err, ErrInvalidDecisionRecord) {
			t.Errorf("%s: err = %v, want ErrInvalidDecisionRecord", c.name, err)
			continue
		}
		if !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: error %q does not name the problem %q", c.name, err, c.want)
		}
	}

	sources := []struct {
		name, source, ref, want string
	}{
		{"unknown source", "carrier-pigeon", "", "source is"},
		{"file source without a file name", DecisionSourceFile, "", "requires source_ref"},
		{"source_ref without a source", "", "storage.md", "source_ref requires source"},
	}
	for _, c := range sources {
		_, _, err := r.svc.Create(clearRecord(), c.source, c.ref)
		if !errors.Is(err, ErrInvalidDecisionRecord) || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err = %v, want ErrInvalidDecisionRecord naming %q", c.name, err, c.want)
		}
	}

	// Every problem of one record is named in one answer.
	_, _, err := r.svc.Create(DecisionRecord{Options: []DecisionOption{{ID: "a", Label: "A"}}}, DecisionSourceREST, "")
	if !errors.Is(err, ErrInvalidDecisionRecord) {
		t.Fatalf("empty record: err = %v, want ErrInvalidDecisionRecord", err)
	}
	for _, want := range []string{"title is required", "situation is required", "deciding is required", "at least 2 options", "default is required"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("empty record: error %q does not name %q", err, want)
		}
	}

	// A refused record leaves nothing behind: no record, no question.
	flowNothingStored(t, r)
}

// ---------------------------------------------------------------------------
// Clarity

func TestDecisionRecordFlow_ClarityWarn(t *testing.T) {
	r := newDecisionSyncRig(t) // the default clarity mode is warn

	t.Run("clear_record_has_no_findings", func(t *testing.T) {
		_, findings, err := r.svc.Create(clearRecord(), DecisionSourceREST, "")
		if err != nil || len(findings) != 0 {
			t.Fatalf("findings = %v, err = %v; want none", qualityCodes(findings), err)
		}
	})

	t.Run("unclear_record_is_stored_with_findings", func(t *testing.T) {
		view, findings, err := r.svc.Create(unclearRecord(), DecisionSourceREST, "")
		if err != nil {
			t.Fatalf("warn mode refused the record: %v", err)
		}
		codes := flowCodes(findings)
		if !codes[QualityCodeTextTooShort] || !codes[QualityCodeNoTradeoffs] {
			t.Fatalf("findings = %v, want text_too_short and no_tradeoffs", qualityCodes(findings))
		}
		for _, f := range findings {
			if strings.TrimSpace(f.Message) == "" {
				t.Errorf("finding %s has no message", f.Code)
			}
		}
		if view.State != DecisionRecordOpen || view.QuestionID == "" {
			t.Fatalf("view = %+v, want an open record linked to its question", view)
		}
		q := flowOneQuestion(t, r, view.ID)
		flowExpect(t, "question.text", q.Text, BuildQuestionFromDecision(flowRecord(t, r, view.ID)).Text)
	})

	t.Run("label_and_tracker_identifier_are_reported", func(t *testing.T) {
		rec := clearRecord()
		rec.Label = "dr-0123456789abcdef0123"
		rec.Title = "Pick the storage engine for " + flowTrackerLike()
		view, findings, err := r.svc.Create(rec, DecisionSourceREST, "")
		if err != nil {
			t.Fatalf("Create: %v", err)
		}
		codes := flowCodes(findings)
		if !codes[QualityCodeLabelNotHuman] || !codes[QualityCodeTrackerID] {
			t.Fatalf("findings = %v, want label_not_human and tracker_id", qualityCodes(findings))
		}
		flowOneQuestion(t, r, view.ID)
	})

	t.Run("an_edit_reports_findings_and_is_applied", func(t *testing.T) {
		view, _, err := r.svc.Create(clearRecord(), DecisionSourceREST, "")
		if err != nil {
			t.Fatalf("Create: %v", err)
		}
		got, findings, err := r.svc.Update(view.ID, unclearRecord())
		if err != nil {
			t.Fatalf("Update in warn mode: %v", err)
		}
		if !flowCodes(findings)[QualityCodeTextTooShort] {
			t.Fatalf("findings = %v, want text_too_short", qualityCodes(findings))
		}
		q := flowQuestion(t, r, got.QuestionID)
		if !strings.HasPrefix(q.Text, "Pick one\n\n") {
			t.Fatalf("question text = %q, want the edit applied", q.Text)
		}
	})

	t.Run("each_check_can_be_turned_off", func(t *testing.T) {
		no := false
		r.dcfg.ClarityRequireTradeoffs = &no
		_, findings, err := r.svc.Create(unclearRecord(), DecisionSourceREST, "")
		if err != nil || flowCodes(findings)[QualityCodeNoTradeoffs] || !flowCodes(findings)[QualityCodeTextTooShort] {
			t.Fatalf("tradeoff check off: findings = %v, err = %v", qualityCodes(findings), err)
		}
		zero := 0
		r.dcfg.ClarityMinTextChars = &zero
		_, findings, err = r.svc.Create(unclearRecord(), DecisionSourceREST, "")
		if err != nil || len(findings) != 0 {
			t.Fatalf("length check off: findings = %v, err = %v", qualityCodes(findings), err)
		}
		r.dcfg = config.AutonomousDecisionsConfig{}
		setClarityMode(r, QualityModeOff)
		_, findings, err = r.svc.Create(unclearRecord(), DecisionSourceREST, "")
		if err != nil || len(findings) != 0 {
			t.Fatalf("mode off: findings = %v, err = %v", qualityCodes(findings), err)
		}
	})

	t.Run("custom_tracker_pattern", func(t *testing.T) {
		r.dcfg = config.AutonomousDecisionsConfig{}
		pat := `\bTICKET-\d+\b`
		r.dcfg.ClarityTrackerIDPattern = &pat
		rec := clearRecord()
		rec.Title = "Pick the storage engine for TICKET-77"
		_, findings, err := r.svc.Create(rec, DecisionSourceREST, "")
		if err != nil || !flowCodes(findings)[QualityCodeTrackerID] {
			t.Fatalf("findings = %v, err = %v; want tracker_id from the configured pattern", qualityCodes(findings), err)
		}
	})
}

func TestDecisionRecordFlow_ClarityRefuse(t *testing.T) {
	r := newDecisionSyncRig(t)
	setClarityMode(r, QualityModeRefuse)

	refusal := func(t *testing.T, err error) *ErrDecisionClarityRefused {
		t.Helper()
		var refused *ErrDecisionClarityRefused
		if !errors.As(err, &refused) {
			t.Fatalf("err = %v, want ErrDecisionClarityRefused", err)
		}
		if len(refused.Findings) == 0 || !strings.Contains(err.Error(), "refused as unclear") {
			t.Fatalf("refusal %q carries %d findings", err, len(refused.Findings))
		}
		return refused
	}

	t.Run("unclear_record_stores_nothing", func(t *testing.T) {
		_, _, err := r.svc.Create(unclearRecord(), DecisionSourceREST, "")
		codes := flowCodes(refusal(t, err).Findings)
		if !codes[QualityCodeTextTooShort] || !codes[QualityCodeNoTradeoffs] {
			t.Fatalf("findings = %v, want text_too_short and no_tradeoffs", codes)
		}
		flowNothingStored(t, r)
	})

	t.Run("label_that_is_a_raw_id_is_refused", func(t *testing.T) {
		rec := clearRecord()
		rec.Label = "dr-0123456789abcdef0123"
		_, _, err := r.svc.Create(rec, DecisionSourceREST, "")
		if !flowCodes(refusal(t, err).Findings)[QualityCodeLabelNotHuman] {
			t.Fatalf("err = %v, want label_not_human", err)
		}
		flowNothingStored(t, r)
	})

	t.Run("tracker_identifier_is_refused", func(t *testing.T) {
		rec := clearRecord()
		rec.Options[0].Tradeoff = "simple, see " + flowTrackerLike()
		_, _, err := r.svc.Create(rec, DecisionSourceREST, "")
		if !flowCodes(refusal(t, err).Findings)[QualityCodeTrackerID] {
			t.Fatalf("err = %v, want tracker_id", err)
		}
		flowNothingStored(t, r)
	})

	t.Run("clear_record_is_accepted_and_an_unclear_edit_is_refused", func(t *testing.T) {
		view, findings, err := r.svc.Create(clearRecord(), DecisionSourceREST, "")
		if err != nil || len(findings) != 0 {
			t.Fatalf("clear record: findings = %v, err = %v", qualityCodes(findings), err)
		}
		before := flowQuestion(t, r, view.QuestionID)
		_, _, err = r.svc.Update(view.ID, unclearRecord())
		_ = refusal(t, err)
		after := flowRecord(t, r, view.ID)
		if after.ContentSHA256 != view.ContentSHA256 {
			t.Error("a refused edit changed the stored record")
		}
		flowExpect(t, "question after the refused edit", flowQuestion(t, r, view.QuestionID), before)
	})

	t.Run("switching_to_warn_lets_the_same_record_through", func(t *testing.T) {
		setClarityMode(r, QualityModeWarn)
		defer setClarityMode(r, QualityModeRefuse)
		if _, findings, err := r.svc.Create(unclearRecord(), DecisionSourceREST, ""); err != nil || len(findings) == 0 {
			t.Fatalf("warn mode: findings = %v, err = %v", qualityCodes(findings), err)
		}
	})
}

// ---------------------------------------------------------------------------
// One question per record

func TestDecisionRecordFlow_OneQuestionPerRecord(t *testing.T) {
	t.Run("create_then_repeated_reconcile", func(t *testing.T) {
		r := newDecisionSyncRig(t)
		view, _, err := r.svc.Create(clearRecord(), DecisionSourceREST, "")
		if err != nil {
			t.Fatalf("Create: %v", err)
		}
		for i := 0; i < 3; i++ {
			if rep := r.svc.Reconcile(); rep.Changed() || len(rep.Errors) > 0 {
				t.Fatalf("reconcile %d changed or failed an in-step record: %+v", i, rep)
			}
		}
		flowOneQuestion(t, r, view.ID)
	})

	t.Run("double_create_path_leaves_one_question", func(t *testing.T) {
		r := newDecisionSyncRig(t)
		// Two ways to raise the question at once: Reconcile for a record that
		// was stored without one, and the service Create of another record.
		stored, err := r.records.Create(clearRecord())
		if err != nil {
			t.Fatalf("records.Create: %v", err)
		}
		var wg sync.WaitGroup
		for i := 0; i < 8; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				r.svc.Reconcile()
			}()
		}
		var created DecisionRecordView
		var createErr error
		wg.Add(1)
		go func() {
			defer wg.Done()
			created, _, createErr = r.svc.Create(clearRecord(), DecisionSourceREST, "")
		}()
		wg.Wait()
		if createErr != nil {
			t.Fatalf("Create: %v", createErr)
		}
		r.svc.Reconcile()
		flowOneQuestion(t, r, stored.ID)
		flowOneQuestion(t, r, created.ID)
		if stored.ID == created.ID {
			t.Fatal("two records share one id")
		}

		// The question store dedupe is the same answer given directly.
		rec := flowRecord(t, r, stored.ID)
		got, raised, err := r.questions.RaiseUnlessDecisionRecord(BuildQuestionFromDecision(rec))
		if err != nil || raised || got.ID != rec.QuestionID {
			t.Fatalf("second raise = %q raised=%v err=%v, want the existing %q", got.ID, raised, err, rec.QuestionID)
		}
		flowOneQuestion(t, r, stored.ID)
	})

	t.Run("restart_replay_after_a_crash_between_raise_and_link", func(t *testing.T) {
		r := newDecisionSyncRig(t)
		rec, err := r.records.Create(clearRecord())
		if err != nil {
			t.Fatalf("records.Create: %v", err)
		}
		raised, ok, err := r.questions.RaiseUnlessDecisionRecord(BuildQuestionFromDecision(rec))
		if err != nil || !ok {
			t.Fatalf("raise: %v ok=%v", err, ok)
		}

		// The daemon restarts: both stores are read back from their files.
		restart := func() *DecisionRecords {
			rs, err := NewDecisionRecordStore(r.records.path)
			if err != nil {
				t.Fatalf("reload records: %v", err)
			}
			qs, err := NewQuestionStore(r.questions.path)
			if err != nil {
				t.Fatalf("reload questions: %v", err)
			}
			return NewDecisionRecords(rs, qs,
				func() config.AutonomousDecisionsConfig { return r.dcfg },
				func() config.AutonomousQuestionsConfig { return r.qcfg }, nil)
		}
		svc := restart()
		rep := svc.Reconcile()
		if rep.Linked != 1 || rep.Raised != 0 || len(rep.Errors) > 0 {
			t.Fatalf("first reconcile after the restart = %+v, want the existing question linked and none raised", rep)
		}
		for i := 0; i < 2; i++ {
			svc = restart()
			if rep := svc.Reconcile(); rep.Changed() || len(rep.Errors) > 0 {
				t.Fatalf("replay %d changed or failed: %+v", i, rep)
			}
		}
		fresh, _ := svc.records.Get(rec.ID)
		if fresh.QuestionID != raised.ID {
			t.Fatalf("record question id = %q, want %q", fresh.QuestionID, raised.ID)
		}
		n := 0
		for _, q := range svc.questions.List(QuestionFilter{Limit: maxQuestionListLimit}) {
			if q.DecisionRecordID == rec.ID {
				n++
			}
		}
		if n != 1 {
			t.Fatalf("%d questions after the restarts, want 1", n)
		}
	})

	t.Run("same_content_twice_is_two_records_with_one_question_each", func(t *testing.T) {
		r := newDecisionSyncRig(t)
		a, _, errA := r.svc.Create(clearRecord(), DecisionSourceREST, "")
		b, _, errB := r.svc.Create(clearRecord(), DecisionSourceREST, "")
		if errA != nil || errB != nil {
			t.Fatalf("Create: %v / %v", errA, errB)
		}
		if a.ID == b.ID || a.QuestionID == b.QuestionID {
			t.Fatalf("records %s/%s share a question %s/%s", a.ID, b.ID, a.QuestionID, b.QuestionID)
		}
		flowOneQuestion(t, r, a.ID)
		flowOneQuestion(t, r, b.ID)
	})
}

// ---------------------------------------------------------------------------
// In step

func TestDecisionRecordFlow_InStep(t *testing.T) {
	t.Run("edit_updates_the_open_question", func(t *testing.T) {
		r := newDecisionSyncRig(t)
		view, _, err := r.svc.Create(clearRecord(), DecisionSourceREST, "")
		if err != nil {
			t.Fatalf("Create: %v", err)
		}
		edit := clearRecord()
		edit.Title = "Pick the storage engine for the service"
		edit.Options = append(edit.Options, DecisionOption{ID: "pg", Label: "Use Postgres", Tradeoff: "strong, needs a server"})
		edit.Recommendation = &DecisionRecommendation{OptionID: "pg", Reason: "it scales"}
		edit.Default = "pg"
		edit.Links = DecisionLinks{RegistryRow: "row-99"} // the Automaton and document links are removed
		got, _, err := r.svc.Update(view.ID, edit)
		if err != nil {
			t.Fatalf("Update: %v", err)
		}
		flowExpect(t, "question id", got.QuestionID, view.QuestionID)
		if got.ContentSHA256 == view.ContentSHA256 {
			t.Error("the content hash did not change with the content")
		}
		q := flowOneQuestion(t, r, view.ID)
		flowExpect(t, "question.id", q.ID, view.QuestionID)
		flowExpect(t, "question.state", q.State, QuestionOpen)
		flowExpect(t, "question.text", q.Text, BuildQuestionFromDecision(flowRecord(t, r, view.ID)).Text)
		if !strings.HasPrefix(q.Text, edit.Title+"\n\n") {
			t.Errorf("question text %q does not start with the edited title", q.Text)
		}
		flowExpect(t, "question.option count", len(q.Options), 3)
		flowExpect(t, "question.default", q.Default, "pg")
		flowExpect(t, "question.recommendation", q.Recommendation, &QuestionRecommendation{OptionID: "pg", Rationale: "it scales"})
		flowExpect(t, "question.context", q.Context, QuestionContextRegistryRow)
		flowExpect(t, "question.registry_row_key", q.RegistryRowKey, "row-99")
		flowExpect(t, "question.prd_id cleared with the link", q.PRDID, "")
		if rep := r.svc.Reconcile(); rep.Changed() {
			t.Errorf("reconcile found the edited question out of step: %+v", rep)
		}
	})

	t.Run("withdrawn_record_withdraws_the_question", func(t *testing.T) {
		r := newDecisionSyncRig(t)
		view, _, err := r.svc.Create(clearRecord(), DecisionSourceREST, "")
		if err != nil {
			t.Fatalf("Create: %v", err)
		}
		got, err := r.svc.Withdraw(view.ID, "dmz", "no longer needed")
		if err != nil {
			t.Fatalf("Withdraw: %v", err)
		}
		flowExpect(t, "record state", got.State, DecisionRecordWithdrawn)
		flowExpect(t, "view question state", got.QuestionState, QuestionWithdrawn)
		flowExpect(t, "question state", flowQuestion(t, r, view.QuestionID).State, QuestionWithdrawn)
		if _, err := r.questions.Answer(view.QuestionID, Answer{Actor: "dmz", Surface: "cli", ChosenOptionID: "sqlite"}); !errors.Is(err, ErrQuestionNotOpen) {
			t.Errorf("answering the withdrawn question: err = %v, want ErrQuestionNotOpen", err)
		}
		if rep := r.svc.Reconcile(); rep.Changed() {
			t.Errorf("reconcile changed a withdrawn record: %+v", rep)
		}
		if _, err := r.svc.Withdraw(view.ID, "dmz", "again"); err != nil {
			t.Errorf("withdrawing a withdrawn record: %v, want no change and no error", err)
		}
		flowOneQuestion(t, r, view.ID)
	})

	t.Run("question_withdrawn_outside_marks_the_record_withdrawn", func(t *testing.T) {
		r := newDecisionSyncRig(t)
		view, _, err := r.svc.Create(clearRecord(), DecisionSourceREST, "")
		if err != nil {
			t.Fatalf("Create: %v", err)
		}
		if _, err := r.questions.Withdraw(view.QuestionID, "operator", "asked in the queue"); err != nil {
			t.Fatalf("questions.Withdraw: %v", err)
		}
		if rep := r.svc.Reconcile(); rep.Withdrawn != 1 || len(rep.Errors) > 0 {
			t.Fatalf("report = %+v, want one record marked withdrawn", rep)
		}
		flowExpect(t, "record state", flowRecord(t, r, view.ID).State, DecisionRecordWithdrawn)
		if rep := r.svc.Reconcile(); rep.Changed() {
			t.Errorf("second reconcile changed something: %+v", rep)
		}

		// An edit that finds the question withdrawn brings the record in step too.
		other, _, err := r.svc.Create(clearRecord(), DecisionSourceREST, "")
		if err != nil {
			t.Fatalf("Create: %v", err)
		}
		if _, err := r.questions.Withdraw(other.QuestionID, "operator", "asked in the queue"); err != nil {
			t.Fatalf("questions.Withdraw: %v", err)
		}
		if _, _, err := r.svc.Update(other.ID, clearRecord()); !errors.Is(err, ErrDecisionRecordClosed) {
			t.Fatalf("edit of a record whose question was withdrawn: err = %v, want ErrDecisionRecordClosed", err)
		}
		flowExpect(t, "record state after the refused edit", flowRecord(t, r, other.ID).State, DecisionRecordWithdrawn)
	})
}

// ---------------------------------------------------------------------------
// Answer

func TestDecisionRecordFlow_AnswerReleasesGate(t *testing.T) {
	r := newAnswerGateRig(t)
	r.startAndWaitParked(t)
	if r.spawned(gateHeldID) {
		t.Fatal("the held task started before its decision was answered")
	}
	if want := "operator decision " + r.view.QuestionID + " is waiting for an answer"; !strings.Contains(r.task(t, gateHeldID).WaitReason, want) {
		t.Fatalf("wait reason = %q, want it to hold on %q", r.task(t, gateHeldID).WaitReason, want)
	}

	const words = "Keep the files for now;\nwe can revisit SQLite next quarter."
	begun := time.Now()
	ans, err := r.store.Answer(r.view.QuestionID, Answer{Actor: "alice", Surface: "pwa", ChosenOptionID: "files", Quote: words})
	if err != nil {
		t.Fatalf("Answer: %v", err)
	}
	// The poll is thirty seconds; starting well inside that proves the release.
	pollUntil(t, gateAnswerStart, "held task started", func() bool { return r.spawned(gateHeldID) })
	if took := time.Since(begun); took >= gateAnswerStart {
		t.Errorf("task started %s after the answer, want under %s", took, gateAnswerStart)
	}
	pollUntil(t, gateWait, "Automaton completed", func() bool { return r.prdStatus(t) == PRDCompleted })
	flowExpect(t, "held task status", r.status(t, gateHeldID), TaskCompleted)
	flowExpect(t, "follower task status", r.status(t, gateFollowerID), TaskCompleted)
	flowExpect(t, "wait reason after the release", r.task(t, gateHeldID).WaitReason, "")

	rec, ok := r.records.Get(r.view.ID)
	if !ok {
		t.Fatal("record vanished")
	}
	flowExpect(t, "record state", rec.State, DecisionRecordAnswered)
	if rec.Answer == nil {
		t.Fatal("the record holds no answer")
	}
	flowExpect(t, "answer id", rec.Answer.AnswerID, ans.ID)
	flowExpect(t, "chosen option", rec.Answer.ChosenOptionID, "files")
	flowExpect(t, "answer words", rec.Answer.Quote, words)
	flowExpect(t, "actor", rec.Answer.Actor, "alice")
	flowExpect(t, "surface", rec.Answer.Surface, "pwa")
	if rec.Answer.AnsweredAt == nil || ans.CreatedAt == nil || !rec.Answer.AnsweredAt.Equal(*ans.CreatedAt) {
		t.Errorf("answered_at = %v, want %v", rec.Answer.AnsweredAt, ans.CreatedAt)
	}
	view, _ := r.svc.View(rec.ID)
	flowExpect(t, "view question state", view.QuestionState, QuestionAnswered)
	flowExpect(t, "view answered by", view.AnsweredBy, "alice")
	flowExpect(t, "view answered quote", view.AnsweredQuote, words)
	if n := r.runDecisions(t); n != 1 {
		t.Errorf("run decisions = %d, want 1 (the waiting run, no second one)", n)
	}
	if r.effects.count() != 1 {
		t.Errorf("answer effects ran %d times, want 1", r.effects.count())
	}
}

// ---------------------------------------------------------------------------
// Document status line

const flowDocBody = "# Storage\n\nSome text.\nConfirmed: pending\n\nMore text.\n"

func TestDecisionRecordFlow_DocLineReport(t *testing.T) {
	d := newDocLineRig(t, "report", flowDocBody)
	before := flowReadFile(t, d.path)
	infoBefore, err := os.Stat(d.path)
	if err != nil {
		t.Fatal(err)
	}
	d.advance(48 * time.Hour) // the fake clock: the answer is written on 2026-10-27

	rec := d.answer(t, "Keep the files; do not drop them.")

	if after := flowReadFile(t, d.path); !bytes.Equal(before, after) {
		t.Fatalf("report mode changed the file:\n%q", after)
	}
	if infoAfter, _ := os.Stat(d.path); !infoAfter.ModTime().Equal(infoBefore.ModTime()) {
		t.Error("report mode touched the file's modification time")
	}
	entries, _ := os.ReadDir(filepath.Dir(d.path))
	flowExpect(t, "directory entries", len(entries), 1)

	c := rec.DocChange
	if c == nil {
		t.Fatal("no document change is shown on the record")
	}
	wantAfter := `Confirmed: sqlite - "Keep the files; do not drop them." (alice, 2026-10-27)`
	flowExpect(t, "status", c.Status, DecisionDocReported)
	flowExpect(t, "mode", c.Mode, "report")
	flowExpect(t, "path", c.Path, docLineRel)
	flowExpect(t, "line", c.Line, 4)
	flowExpect(t, "before", c.Before, "Confirmed: pending")
	flowExpect(t, "after", c.After, wantAfter)
	flowExpect(t, "diff", c.Diff, "--- "+docLineRel+"\n+++ "+docLineRel+"\n@@ -4 +4 @@\n-Confirmed: pending\n+"+wantAfter+"\n")
	if c.At == nil || !c.At.Equal(time.Date(2026, 11, 2, 9, 0, 0, 0, time.UTC)) {
		t.Errorf("at = %v, want the fake clock 2026-11-02T09:00:00Z", c.At)
	}

	// The change is what the operator reads back from the service.
	v, ok := d.svc.View(rec.ID)
	if !ok || !reflect.DeepEqual(v.DocChange, c) {
		t.Errorf("view doc_change = %+v, want %+v", v.DocChange, c)
	}

	t.Run("report_is_the_default_mode", func(t *testing.T) {
		d := newDocLineRig(t, "report", flowDocBody)
		d.dcfg.DocStatusMode = nil
		rec := d.answer(t, "yes")
		if rec.DocChange == nil || rec.DocChange.Status != DecisionDocReported || rec.DocChange.Mode != "report" {
			t.Fatalf("change = %+v, want reported in report mode", rec.DocChange)
		}
		if got := flowReadFile(t, d.path); string(got) != flowDocBody {
			t.Fatalf("the default mode changed the file: %q", got)
		}
	})
}

func TestDecisionRecordFlow_DocLineOn(t *testing.T) {
	t.Run("exactly_the_status_line_is_rewritten", func(t *testing.T) {
		d := newDocLineRig(t, "on", flowDocBody)
		d.advance(48 * time.Hour)
		rec := d.answer(t, "Keep the files; do not drop them.")

		c := rec.DocChange
		if c == nil {
			t.Fatal("no document change recorded")
		}
		wantLine := `Confirmed: sqlite - "Keep the files; do not drop them." (alice, 2026-10-27)`
		flowExpect(t, "status", c.Status, DecisionDocApplied)
		flowExpect(t, "mode", c.Mode, "on")
		flowExpect(t, "line", c.Line, 4)
		flowExpect(t, "before", c.Before, "Confirmed: pending")
		flowExpect(t, "after", c.After, wantLine)
		want := "# Storage\n\nSome text.\n" + wantLine + "\n\nMore text.\n"
		if got := string(flowReadFile(t, d.path)); got != want {
			t.Fatalf("file = %q, want %q", got, want)
		}
		info, err := os.Stat(d.path)
		if err != nil {
			t.Fatal(err)
		}
		flowExpect(t, "permission bits", info.Mode().Perm(), os.FileMode(0o640))
		entries, _ := os.ReadDir(filepath.Dir(d.path))
		flowExpect(t, "directory entries (no temp file left)", len(entries), 1)

		// Replaying the answer path never rewrites the file again.
		stamp := info.ModTime()
		q := flowQuestion(t, d.decisionSyncRig, rec.QuestionID)
		ans, _ := d.questions.CurrentAnswer(rec.QuestionID)
		d.svc.OnQuestionChange(QuestionChange{Kind: QuestionChangeAnswered, Question: q, Answer: &ans})
		d.svc.Reconcile()
		if got := string(flowReadFile(t, d.path)); got != want {
			t.Fatalf("file changed on a replay: %q", got)
		}
		if again, _ := os.Stat(d.path); !again.ModTime().Equal(stamp) {
			t.Error("a replay rewrote the file")
		}
	})

	t.Run("a_multi_line_answer_stays_on_one_line", func(t *testing.T) {
		d := newDocLineRig(t, "on", flowDocBody)
		rec := d.answer(t, multiLineQuote)
		if rec.DocChange == nil || rec.DocChange.Status != DecisionDocApplied {
			t.Fatalf("change = %+v, want applied", rec.DocChange)
		}
		if !strings.Contains(rec.DocChange.After, `"Go with SQLite. Keep the files as a backup; do not drop them."`) {
			t.Fatalf("after = %q, want the quoted words on one line", rec.DocChange.After)
		}
		got := string(flowReadFile(t, d.path))
		if strings.Count(got, "\n") != strings.Count(flowDocBody, "\n") {
			t.Fatalf("the rewrite changed the line count: %q", got)
		}
		if rec.Answer.Quote != multiLineQuote {
			t.Errorf("the record's answer words changed: %q", rec.Answer.Quote)
		}
	})

	t.Run("line_endings_are_kept", func(t *testing.T) {
		d := newDocLineRig(t, "on", "# Storage\r\nConfirmed: pending\r\nlast line without ending")
		rec := d.answer(t, "go")
		if rec.DocChange == nil || rec.DocChange.Status != DecisionDocApplied {
			t.Fatalf("change = %+v, want applied", rec.DocChange)
		}
		want := "# Storage\r\n" + rec.DocChange.After + "\r\nlast line without ending"
		if got := string(flowReadFile(t, d.path)); got != want {
			t.Fatalf("file = %q, want %q", got, want)
		}
	})

	t.Run("custom_pattern_and_template", func(t *testing.T) {
		d := newDocLineRig(t, "on", "Confirmed: not this one\nStatus: open\n")
		pat, tmpl := `^Status:.*$`, "Status: {choice_label} [{choice}] by {actor} ({decision_id})"
		d.dcfg.DocStatusPattern, d.dcfg.DocStatusTemplate = &pat, &tmpl
		rec := d.answer(t, "go")
		want := "Status: Use SQLite [sqlite] by alice (" + rec.ID + ")"
		if got := string(flowReadFile(t, d.path)); got != "Confirmed: not this one\n"+want+"\n" {
			t.Fatalf("file = %q", got)
		}
	})
}

func TestDecisionRecordFlow_DocLineOff(t *testing.T) {
	d := newDocLineRig(t, "off", flowDocBody)
	rec := d.answer(t, "yes")
	if rec.DocChange != nil {
		t.Fatalf("off mode recorded %+v", *rec.DocChange)
	}
	if got := string(flowReadFile(t, d.path)); got != flowDocBody {
		t.Fatalf("off mode changed the file: %q", got)
	}
	flowExpect(t, "record state", rec.State, DecisionRecordAnswered)
	if v, _ := d.svc.View(rec.ID); v.DocChange != nil {
		t.Errorf("the view shows a document change in off mode: %+v", *v.DocChange)
	}

	t.Run("off_does_not_even_look_for_the_document", func(t *testing.T) {
		d := newDocLineRig(t, "off", flowDocBody)
		if err := os.Remove(d.path); err != nil {
			t.Fatal(err)
		}
		if rec := d.answer(t, "yes"); rec.DocChange != nil || rec.State != DecisionRecordAnswered {
			t.Fatalf("record = state %s change %+v, want answered with no change recorded", rec.State, rec.DocChange)
		}
	})

	t.Run("a_record_without_a_document_link_records_nothing_in_any_mode", func(t *testing.T) {
		d := newDocLineRig(t, "on", flowDocBody)
		in := clearRecord()
		in.Links.Doc = ""
		v, _, err := d.svc.Create(in, DecisionSourceREST, "")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := d.questions.Answer(v.QuestionID, Answer{Actor: "alice", Surface: "pwa", ChosenOptionID: "sqlite", Quote: "go"}); err != nil {
			t.Fatal(err)
		}
		if got := flowRecord(t, d.decisionSyncRig, v.ID); got.DocChange != nil {
			t.Fatalf("recorded %+v for a record without a document", *got.DocChange)
		}
		if string(flowReadFile(t, d.path)) != flowDocBody {
			t.Fatal("the file changed")
		}
	})
}

// ---------------------------------------------------------------------------
// Follow-up

func TestDecisionRecordFlow_FollowUpNoneConfigured(t *testing.T) {
	r, _ := newAnswerRig(t, true)
	fake := &fakeFollowUpTargets{status: map[string]string{"held-1": "needs_review"}}
	r.svc.SetFollowUpTargets(fake, fake) // wired, but nothing is configured
	v := openedRecord(t, r)

	for _, list := range [][]config.AutonomousDecisionFollowUp{nil, {}} {
		r.dcfg.FollowUps = list
		err := r.svc.RequestFollowUp(v.QuestionID, "kickoff")
		var unknown *ErrUnknownFollowUp
		if !errors.As(err, &unknown) || !strings.Contains(err.Error(), "no follow-ups are configured") {
			t.Fatalf("request with %d configured: err = %v, want *ErrUnknownFollowUp saying none are configured", len(list), err)
		}
	}
	answerRecord(t, r, v.QuestionID)
	r.svc.Reconcile()

	rec := flowRecord(t, r, v.ID)
	flowExpect(t, "record state", rec.State, DecisionRecordAnswered)
	if rec.FollowUp != nil {
		t.Errorf("follow_up = %+v, want none stored", *rec.FollowUp)
	}
	if fake.calls() != 0 {
		t.Errorf("%d follow-up calls ran with nothing configured, want 0", fake.calls())
	}
}

func TestDecisionRecordFlow_FollowUpConfigured(t *testing.T) {
	t.Run("start_template_runs_once", func(t *testing.T) {
		r, fake := followUpRig(t)
		r.advance(time.Hour)
		v := openedRecord(t, r)
		if err := r.svc.RequestFollowUp(v.QuestionID, "kickoff"); err != nil {
			t.Fatalf("RequestFollowUp: %v", err)
		}
		pending := flowRecord(t, r, v.ID).FollowUp
		if pending == nil || !pending.Requested || pending.Name != "kickoff" || pending.Status != "" {
			t.Fatalf("follow_up before the answer = %+v, want requested and not run", pending)
		}
		if fake.calls() != 0 {
			t.Fatalf("asking for a follow-up ran %d calls, want 0", fake.calls())
		}

		answerRecord(t, r, v.QuestionID)

		fu := flowRecord(t, r, v.ID).FollowUp
		if fu == nil {
			t.Fatal("no follow-up outcome on the record")
		}
		flowExpect(t, "name", fu.Name, "kickoff")
		flowExpect(t, "kind", fu.Kind, config.DecisionFollowUpStartTemplate)
		flowExpect(t, "target", fu.Target, "tpl-build")
		flowExpect(t, "status", fu.Status, DecisionFollowUpDone)
		if !strings.Contains(fu.Result, "new-automaton-1") || strings.ContainsAny(fu.Result, "\r\n") {
			t.Errorf("result %q, want one line naming the new Automaton", fu.Result)
		}
		if fu.At == nil || !fu.At.Equal(time.Date(2026, 10, 31, 10, 0, 0, 0, time.UTC)) {
			t.Errorf("at = %v, want the fake clock 2026-10-31T10:00:00Z", fu.At)
		}

		// Every way of replaying the answer leaves it at one run.
		rec := flowRecord(t, r, v.ID)
		q := flowQuestion(t, r, v.QuestionID)
		a, _ := r.questions.CurrentAnswer(v.QuestionID)
		r.svc.OnQuestionChange(QuestionChange{Kind: QuestionChangeAnswered, Question: q, Answer: &a})
		r.svc.runFollowUp(rec, q, a)
		r.svc.Reconcile()
		flowExpect(t, "templates started", fake.templates, []string{"tpl-build"})
	})

	t.Run("open_automaton_runs_once", func(t *testing.T) {
		r, fake := followUpRig(t)
		v := openedRecord(t, r)
		if err := r.svc.RequestFollowUp(v.QuestionID, "release"); err != nil {
			t.Fatalf("RequestFollowUp: %v", err)
		}
		answerRecord(t, r, v.QuestionID)
		r.svc.Reconcile()

		fu := flowRecord(t, r, v.ID).FollowUp
		if fu == nil || fu.Status != DecisionFollowUpDone || fu.Kind != config.DecisionFollowUpOpenAutomaton || fu.Target != "held-1" {
			t.Fatalf("follow_up = %+v, want open_automaton held-1 done", fu)
		}
		flowExpect(t, "approved", fake.approved, []string{"held-1"})
		flowExpect(t, "ran", fake.ran, []string{"held-1"})
	})

	t.Run("a_name_that_is_not_configured_is_refused", func(t *testing.T) {
		r, fake := followUpRig(t)
		v := openedRecord(t, r)
		err := r.svc.RequestFollowUp(v.QuestionID, "launch-rockets")
		var unknown *ErrUnknownFollowUp
		if !errors.As(err, &unknown) || !strings.Contains(err.Error(), "kickoff") || !strings.Contains(err.Error(), "release") {
			t.Fatalf("err = %v, want *ErrUnknownFollowUp naming the allowed names", err)
		}
		answerRecord(t, r, v.QuestionID)
		if fu := flowRecord(t, r, v.ID).FollowUp; fu != nil || fake.calls() != 0 {
			t.Fatalf("follow_up %+v, %d calls; want none after a refused request", fu, fake.calls())
		}
	})
}

// ---------------------------------------------------------------------------
// Folder watch

func TestDecisionRecordFlow_FolderDefaultFormat(t *testing.T) {
	r, dir := folderRig(t)
	writeFolderFile(t, dir, "storage.md", folderFileDefault)
	writeFolderFile(t, dir, "notes.txt", "not matched by the default glob")

	st := r.svc.ScanFolder()
	if !st.Enabled || st.Path != dir || st.FilesSeen != 1 || len(st.Errors) != 0 || st.LastScan == nil {
		t.Fatalf("status = %+v, want enabled, 1 file seen, no errors and a scan time", st)
	}
	recs := fileRecords(r)
	if len(recs) != 1 {
		t.Fatalf("%d file records, want 1", len(recs))
	}
	rec := recs[0]
	flowExpect(t, "source", rec.Source, DecisionSourceFile)
	flowExpect(t, "source_ref", rec.SourceRef, "storage.md")
	flowExpect(t, "state", rec.State, DecisionRecordOpen)
	flowExpect(t, "label", rec.Label, "storage")
	flowExpect(t, "title", rec.Title, "Pick the storage engine")
	flowExpect(t, "situation", rec.Situation, "The service keeps its state in memory and loses it on every restart, "+
		"so the operator has to re-enter the same settings each time it comes back.")
	flowExpect(t, "deciding", rec.Deciding, "Which storage engine the service uses from now on.")
	flowExpect(t, "options", rec.Options, []DecisionOption{
		{ID: "sqlite", Label: "Use SQLite", Tradeoff: "simple, one writer at a time"},
		{ID: "files", Label: "Keep append-only files", Tradeoff: "no new dependency, slow queries"},
	})
	flowExpect(t, "recommendation", rec.Recommendation, &DecisionRecommendation{OptionID: "sqlite", Reason: "queries stay fast"})
	flowExpect(t, "default", rec.Default, "hold")
	flowExpect(t, "links", rec.Links, DecisionLinks{Doc: "docs/plans/storage.md", Automaton: "ee660001", RegistryRow: "row-12"})
	flowExpect(t, "content hash", rec.ContentSHA256, rec.ContentHash())
	q := flowOneQuestion(t, r, rec.ID)
	flowExpect(t, "question.id", q.ID, rec.QuestionID)
	flowExpect(t, "question.text", q.Text, BuildQuestionFromDecision(rec).Text)
	flowExpect(t, "question.state", q.State, QuestionOpen)

	// An unchanged file changes nothing.
	r.advance(time.Hour)
	r.svc.ScanFolder()
	same := flowRecord(t, r, rec.ID)
	if !same.UpdatedAt.Equal(*rec.UpdatedAt) {
		t.Error("an unchanged file rewrote its record")
	}
	flowExpect(t, "question after an unchanged scan", flowQuestion(t, r, q.ID), q)

	// An edit updates the same record and the same open question.
	writeFolderFile(t, dir, "storage.md", strings.Replace(folderFileDefault, "Pick the storage engine", "Choose the storage engine", 1))
	if st := r.svc.ScanFolder(); len(st.Errors) != 0 {
		t.Fatalf("edit scan errors: %+v", st.Errors)
	}
	edited := flowRecord(t, r, rec.ID)
	flowExpect(t, "edited title", edited.Title, "Choose the storage engine")
	got := flowOneQuestion(t, r, rec.ID)
	flowExpect(t, "edited question id", got.ID, q.ID)
	if !strings.HasPrefix(got.Text, "Choose the storage engine\n\n") {
		t.Errorf("question text = %q, want the edited title", got.Text)
	}

	// Deleting the file withdraws the open record and its question.
	if err := os.Remove(filepath.Join(dir, "storage.md")); err != nil {
		t.Fatal(err)
	}
	r.svc.ScanFolder()
	flowExpect(t, "record after delete", flowRecord(t, r, rec.ID).State, DecisionRecordWithdrawn)
	flowExpect(t, "question after delete", flowQuestion(t, r, q.ID).State, QuestionWithdrawn)
}

func TestDecisionRecordFlow_FolderDifferentFrontMatter(t *testing.T) {
	// The reference: the default format.
	ref, refDir := folderRig(t)
	writeFolderFile(t, refDir, "storage.md", folderFileDefault)
	if st := ref.svc.ScanFolder(); len(st.Errors) != 0 {
		t.Fatalf("default format: errors %+v", st.Errors)
	}
	want := fileRecords(ref)
	if len(want) != 1 {
		t.Fatalf("default format: %d records, want 1", len(want))
	}

	// The same decision in a file with another delimiter, separators, renamed
	// keys and another file name pattern.
	alt, altDir := folderRig(t)
	alt.dcfg = plusConfig()
	alt.dcfg.Folder = altDir
	glob := "*.dec"
	alt.dcfg.FolderGlob = &glob
	writeFolderFile(t, altDir, "storage.dec", folderFilePlus)
	writeFolderFile(t, altDir, "storage.md", "ignored: the glob does not match this file\n")
	st := alt.svc.ScanFolder()
	if len(st.Errors) != 0 || st.FilesSeen != 1 {
		t.Fatalf("other format: status = %+v, want 1 file seen and no errors", st)
	}
	got := fileRecords(alt)
	if len(got) != 1 {
		t.Fatalf("other format: %d records, want 1", len(got))
	}

	a, b := want[0], got[0]
	flowExpect(t, "source_ref", b.SourceRef, "storage.dec")
	flowExpect(t, "content hash", b.ContentSHA256, a.ContentSHA256)
	flowExpect(t, "label", b.Label, a.Label)
	flowExpect(t, "title", b.Title, a.Title)
	flowExpect(t, "situation", b.Situation, a.Situation)
	flowExpect(t, "deciding", b.Deciding, a.Deciding)
	flowExpect(t, "options", b.Options, a.Options)
	flowExpect(t, "recommendation", b.Recommendation, a.Recommendation)
	flowExpect(t, "default", b.Default, a.Default)
	flowExpect(t, "links", b.Links, a.Links)

	qa, qb := flowOneQuestion(t, ref, a.ID), flowOneQuestion(t, alt, b.ID)
	flowExpect(t, "question.text", qb.Text, qa.Text)
	flowExpect(t, "question.label", qb.Label, qa.Label)
	flowExpect(t, "question.options", qb.Options, qa.Options)
	flowExpect(t, "question.recommendation", qb.Recommendation, qa.Recommendation)
	flowExpect(t, "question.default", qb.Default, qa.Default)
	flowExpect(t, "question.context", qb.Context, qa.Context)
	flowExpect(t, "question.prd_id", qb.PRDID, qa.PRDID)

	// The format is the configuration's: the same file read with the default
	// pattern is reported, not guessed at.
	plain, plainDir := folderRig(t)
	writeFolderFile(t, plainDir, "storage.md", folderFilePlus)
	pst := plain.svc.ScanFolder()
	if msg, ok := folderErrorFor(pst, "storage.md"); !ok || !strings.Contains(msg, `"---"`) {
		t.Errorf("default pattern on the other format: errors = %+v, want a message naming the expected delimiter", pst.Errors)
	}
	if n := len(fileRecords(plain)); n != 0 {
		t.Errorf("%d records from a file in another format under the default pattern, want 0", n)
	}
}

// ---------------------------------------------------------------------------
// Failure modes

func TestDecisionRecordFlow_FailureModes(t *testing.T) {
	cases := []struct {
		name string
		run  func(t *testing.T)
	}{
		{"record_edit_after_answer", flowFailEditAfterAnswer},
		{"question_channel_off", func(t *testing.T) { flowFailSwitchOff(t, false) }},
		{"decisions_switch_off", func(t *testing.T) { flowFailSwitchOff(t, true) }},
		{"answer_names_an_unknown_option", flowFailUnknownOption},
		{"doc_path_escapes_doc_root", flowFailDocPathEscapes},
		{"doc_file_changed_during_the_rewrite", flowFailDocChangedDuringRewrite},
		{"ambiguous_status_line", flowFailAmbiguousStatusLine},
		{"unparsable_folder_file", flowFailUnparsableFolderFile},
		{"duplicate_folder_source", flowFailDuplicateFolderSource},
		{"secret_looking_answer_is_refused_by_the_question_store", flowFailSecretAnswer},
	}
	for _, c := range cases {
		t.Run(c.name, c.run)
	}
}

func flowFailEditAfterAnswer(t *testing.T) {
	// The answer is applied to the record the moment it lands.
	r, _ := newAnswerRig(t, true)
	v := openedRecord(t, r)
	answerRecord(t, r, v.QuestionID)
	flowExpect(t, "record state", flowRecord(t, r, v.ID).State, DecisionRecordAnswered)
	edit := clearRecord()
	edit.Title = "A different title"
	_, _, err := r.svc.Update(v.ID, edit)
	if !errors.Is(err, ErrDecisionRecordClosed) {
		t.Fatalf("edit of an answered record: err = %v, want ErrDecisionRecordClosed", err)
	}
	if _, err := r.svc.Withdraw(v.ID, "dmz", "late"); !errors.Is(err, ErrDecisionRecordClosed) {
		t.Fatalf("withdraw of an answered record: err = %v, want ErrDecisionRecordClosed", err)
	}
	flowExpect(t, "content hash", flowRecord(t, r, v.ID).ContentSHA256, v.ContentSHA256)
	if q := flowQuestion(t, r, v.QuestionID); strings.HasPrefix(q.Text, "A different title") || q.State != QuestionAnswered {
		t.Fatalf("the answered question changed: state %s text %q", q.State, q.Text)
	}

	// The question is answered but the record has not caught up yet (no hook).
	late, _ := newAnswerRig(t, false)
	lv := openedRecord(t, late)
	answerRecord(t, late, lv.QuestionID)
	if _, _, err := late.svc.Update(lv.ID, edit); !errors.Is(err, ErrDecisionRecordClosed) {
		t.Fatalf("edit of a record whose question is answered: err = %v, want ErrDecisionRecordClosed", err)
	}
	if q := flowQuestion(t, late, lv.QuestionID); strings.HasPrefix(q.Text, "A different title") {
		t.Fatal("the answered question was changed by the refused edit")
	}
}

func flowFailSwitchOff(t *testing.T, decisions bool) {
	r := newDecisionSyncRig(t)
	open, _, err := r.svc.Create(clearRecord(), DecisionSourceREST, "")
	if err != nil {
		t.Fatalf("Create with both switches on: %v", err)
	}
	before := flowQuestion(t, r, open.QuestionID)

	switchName := questionsSwitchName
	if decisions {
		off := false
		r.dcfg.Enabled = &off
		switchName = decisionsSwitchName
	} else {
		r.qcfg.Enabled = false
	}
	unavailable := func(what string, err error) {
		t.Helper()
		var un *ErrDecisionRecordsUnavailable
		if !errors.As(err, &un) || len(un.Switches) != 1 || un.Switches[0] != switchName || !strings.Contains(err.Error(), switchName) {
			t.Fatalf("%s: err = %v, want ErrDecisionRecordsUnavailable naming only %s", what, err, switchName)
		}
	}
	_, _, err = r.svc.Create(clearRecord(), DecisionSourceREST, "")
	unavailable("Create", err)
	_, _, err = r.svc.Update(open.ID, clearRecord())
	unavailable("Update", err)
	unavailable("RequestFollowUp", r.svc.RequestFollowUp(open.QuestionID, "kickoff"))

	// Nothing else acts while the switch is off.
	stored, err := r.records.Create(clearRecord())
	if err != nil {
		t.Fatalf("records.Create: %v", err)
	}
	if rep := r.svc.Reconcile(); rep.Changed() {
		t.Fatalf("reconcile acted while %s is off: %+v", switchName, rep)
	}
	if got := flowRecord(t, r, stored.ID); got.QuestionID != "" {
		t.Fatalf("a question was raised while %s is off", switchName)
	}
	flowExpect(t, "the open question", flowQuestion(t, r, open.QuestionID), before)
	r.dcfg.Folder = t.TempDir()
	writeFolderFile(t, r.dcfg.Folder, "storage.md", folderFileDefault)
	if st := r.svc.ScanFolder(); st.Enabled || len(fileRecords(r)) != 0 {
		t.Fatalf("the folder was scanned while %s is off: %+v", switchName, st)
	}

	// The switch is read live: turning it back on needs no restart.
	r.dcfg.Enabled, r.qcfg.Enabled = nil, true
	if rep := r.svc.Reconcile(); rep.Raised != 1 {
		t.Fatalf("reconcile after the switch came back = %+v, want the missing question raised", rep)
	}
	if _, _, err := r.svc.Create(clearRecord(), DecisionSourceREST, ""); err != nil {
		t.Fatalf("Create after the switch came back: %v", err)
	}
}

func flowFailUnknownOption(t *testing.T) {
	r, effects := newAnswerRig(t, true)
	v := openedRecord(t, r)
	_, err := r.questions.Answer(v.QuestionID, Answer{Actor: "alice", Surface: "pwa", ChosenOptionID: "mongo", Quote: "Mongo, please"})
	if !errors.Is(err, ErrUnknownOption) || !strings.Contains(err.Error(), "mongo") {
		t.Fatalf("err = %v, want ErrUnknownOption naming mongo", err)
	}
	rec := flowRecord(t, r, v.ID)
	if rec.State != DecisionRecordOpen || rec.Answer != nil {
		t.Fatalf("record = state %s answer %+v, want open with no answer", rec.State, rec.Answer)
	}
	if _, ok := r.questions.CurrentAnswer(v.QuestionID); ok || flowQuestion(t, r, v.QuestionID).State != QuestionOpen {
		t.Fatal("the refused answer was stored")
	}
	if effects.count() != 0 {
		t.Fatalf("answer effects ran %d times for a refused answer", effects.count())
	}
	// The question is still answerable with a real option.
	if _, err := r.questions.Answer(v.QuestionID, Answer{Actor: "alice", Surface: "pwa", ChosenOptionID: "sqlite"}); err != nil {
		t.Fatalf("a real option after the refusal: %v", err)
	}
	flowExpect(t, "record state", flowRecord(t, r, v.ID).State, DecisionRecordAnswered)
}

func flowFailDocPathEscapes(t *testing.T) {
	d := newDocLineRig(t, "on", flowDocBody)
	outside := filepath.Join(t.TempDir(), "outside.md")
	const outsideBody = "Confirmed: outside the document folder\n"
	if err := os.WriteFile(outside, []byte(outsideBody), 0o644); err != nil {
		t.Fatal(err)
	}
	// The record links a path that is fine as a path, but the file there is a
	// symbolic link that leaves doc_root.
	if err := os.Remove(d.path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, d.path); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	rec := d.answer(t, "go")
	flowExpect(t, "record state", rec.State, DecisionRecordAnswered)
	c := rec.DocChange
	if c == nil || c.Status != DecisionDocError || !strings.Contains(c.Diff, "leaves the document folder") {
		t.Fatalf("change = %+v, want an error saying the path leaves the document folder", c)
	}
	if strings.Contains(c.Diff, outside) || strings.Contains(c.Diff, d.root) {
		t.Errorf("the message %q carries a real path", c.Diff)
	}
	if got := string(flowReadFile(t, outside)); got != outsideBody {
		t.Fatalf("the file outside doc_root was changed: %q", got)
	}
}

func flowFailDocChangedDuringRewrite(t *testing.T) {
	d := newDocLineRig(t, "on", flowDocBody)
	other := flowDocBody + "Added by someone else.\n"
	docLineBeforeWrite = func() { _ = os.WriteFile(d.path, []byte(other), 0o640) }
	t.Cleanup(func() { docLineBeforeWrite = nil })
	rec := d.answer(t, "go")
	flowExpect(t, "record state", rec.State, DecisionRecordAnswered)
	c := rec.DocChange
	if c == nil || c.Status != DecisionDocError || !strings.Contains(c.Diff, "changed while") {
		t.Fatalf("change = %+v, want an error saying the document changed while the change was prepared", c)
	}
	if got := string(flowReadFile(t, d.path)); got != other {
		t.Fatalf("file = %q, want the other writer's content kept", got)
	}
	entries, _ := os.ReadDir(filepath.Dir(d.path))
	flowExpect(t, "directory entries (no temp file left)", len(entries), 1)
}

func flowFailAmbiguousStatusLine(t *testing.T) {
	const body = "Confirmed: one\ntext\nConfirmed: two\n"
	d := newDocLineRig(t, "on", body)
	rec := d.answer(t, "go")
	flowExpect(t, "record state", rec.State, DecisionRecordAnswered)
	c := rec.DocChange
	if c == nil || c.Status != DecisionDocAmbiguous || !strings.Contains(c.Diff, "1, 3") {
		t.Fatalf("change = %+v, want ambiguous naming lines 1 and 3", c)
	}
	if got := string(flowReadFile(t, d.path)); got != body {
		t.Fatalf("file changed: %q", got)
	}
}

func flowFailUnparsableFolderFile(t *testing.T) {
	r, dir := folderRig(t)
	writeFolderFile(t, dir, "a-broken.md", "---\ntitle: Only a title\n---\n")
	writeFolderFile(t, dir, "b-good.md", folderFileDefault)
	writeFolderFile(t, dir, "c-prose.md", "just some prose, no front matter\n")

	for i := 0; i < 2; i++ { // a broken file stays reported and never creates anything
		st := r.svc.ScanFolder()
		flowExpect(t, "files seen", st.FilesSeen, 3)
		msg, ok := folderErrorFor(st, "a-broken.md")
		if !ok || !strings.Contains(msg, `"situation"`) || strings.Contains(msg, dir) {
			t.Fatalf("scan %d: a-broken.md error = %q (reported %v), want a plain message naming the missing field and no path", i, msg, ok)
		}
		if _, ok := folderErrorFor(st, "c-prose.md"); !ok {
			t.Fatalf("scan %d: c-prose.md is not reported: %+v", i, st.Errors)
		}
		if _, ok := folderErrorFor(st, "b-good.md"); ok {
			t.Fatalf("scan %d: the good file is reported as an error: %+v", i, st.Errors)
		}
	}
	recs := fileRecords(r)
	if len(recs) != 1 || recs[0].SourceRef != "b-good.md" {
		t.Fatalf("records = %+v, want only the good file's record", recs)
	}
	flowOneQuestion(t, r, recs[0].ID)
	if n := len(r.questions.List(QuestionFilter{Limit: maxQuestionListLimit})); n != 1 {
		t.Fatalf("%d questions, want only the good file's", n)
	}

	// Fixing the file clears its error and creates its record.
	writeFolderFile(t, dir, "a-broken.md", strings.Replace(folderFileDefault, "Pick the storage engine", "Pick another engine", 1))
	st := r.svc.ScanFolder()
	if _, ok := folderErrorFor(st, "a-broken.md"); ok {
		t.Fatalf("a-broken.md is still reported after the fix: %+v", st.Errors)
	}
	flowExpect(t, "records after the fix", len(fileRecords(r)), 2)
}

func flowFailDuplicateFolderSource(t *testing.T) {
	r, dir := folderRig(t)
	writeFolderFile(t, dir, "storage.md", folderFileDefault)
	r.svc.ScanFolder()
	recs := fileRecords(r)
	if len(recs) != 1 {
		t.Fatalf("%d file records, want 1", len(recs))
	}

	// A second record for the same file name, from any path, is refused.
	in := clearRecord()
	in.Title = "Another decision under the same file name"
	_, _, err := r.svc.Create(in, DecisionSourceFile, "storage.md")
	if !errors.Is(err, ErrDecisionRecordDuplicate) || !strings.Contains(err.Error(), "storage.md") {
		t.Fatalf("err = %v, want ErrDecisionRecordDuplicate naming storage.md", err)
	}
	if n := len(fileRecords(r)); n != 1 {
		t.Fatalf("%d file records after the duplicate, want 1", n)
	}
	if n := len(r.questions.List(QuestionFilter{Limit: maxQuestionListLimit})); n != 1 {
		t.Fatalf("%d questions after the duplicate, want 1", n)
	}

	// Scanning again finds the record by its source and keeps it single.
	if st := r.svc.ScanFolder(); len(st.Errors) != 0 {
		t.Fatalf("rescan errors: %+v", st.Errors)
	}
	flowExpect(t, "records after a rescan", len(fileRecords(r)), 1)
	flowOneQuestion(t, r, recs[0].ID)

	// The same holds when the first record is withdrawn: its name stays taken.
	if _, err := r.svc.Withdraw(recs[0].ID, "dmz", "not needed"); err != nil {
		t.Fatalf("Withdraw: %v", err)
	}
	if _, _, err := r.svc.Create(in, DecisionSourceFile, "storage.md"); !errors.Is(err, ErrDecisionRecordDuplicate) {
		t.Fatalf("after withdrawing: err = %v, want ErrDecisionRecordDuplicate", err)
	}
}

func flowFailSecretAnswer(t *testing.T) {
	r, effects := newAnswerRig(t, true)
	v := openedRecord(t, r)
	_, err := r.questions.Answer(v.QuestionID, Answer{Actor: "alice", Surface: "pwa", ChosenOptionID: "sqlite", Quote: testSecretAnswer})
	if !errors.Is(err, ErrSecretInAnswer) {
		t.Fatalf("err = %v, want ErrSecretInAnswer", err)
	}
	if strings.Contains(err.Error(), testSecretAnswer) {
		t.Fatal("the refusal carries the secret text")
	}
	rec := flowRecord(t, r, v.ID)
	if rec.State != DecisionRecordOpen || rec.Answer != nil {
		t.Fatalf("record = state %s answer %+v, want open with no answer", rec.State, rec.Answer)
	}
	if flowQuestion(t, r, v.QuestionID).State != QuestionOpen {
		t.Fatal("the question left the open state")
	}
	for name, path := range map[string]string{"question store": r.questions.path, "record store": r.records.path} {
		if strings.Contains(string(flowReadFile(t, path)), "hunter2xy12") {
			t.Errorf("the %s file contains the secret", name)
		}
	}
	if rep := r.svc.Reconcile(); rep.Changed() {
		t.Fatalf("reconcile applied a refused answer: %+v", rep)
	}
	if effects.count() != 0 {
		t.Fatalf("answer effects ran %d times for a refused answer", effects.count())
	}
	// A plain answer afterwards goes through.
	if _, err := r.questions.Answer(v.QuestionID, Answer{Actor: "alice", Surface: "pwa", ChosenOptionID: "sqlite", Quote: "SQLite, please"}); err != nil {
		t.Fatalf("a plain answer after the refusal: %v", err)
	}
	flowExpect(t, "record state", flowRecord(t, r, v.ID).State, DecisionRecordAnswered)
}
