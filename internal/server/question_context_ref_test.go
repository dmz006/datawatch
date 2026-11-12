// question_context_ref_test.go — the computed context_ref on question reads:
// GET /api/questions/{id} and the list rows carry it, a proposal carries the
// in-app documents viewer link, and the stored record never does.

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/autonomous"
)

// namedAutonomous answers the optional name lookups; the embedded interface
// is nil, so any other manager call would panic and fail the test loudly.
type namedAutonomous struct {
	AutonomousAPI
}

func (namedAutonomous) AutomatonName(id string) (string, bool) {
	if id == "a1b2c3d4" {
		return "Nightly cleanup", true
	}
	return "", false
}

func (namedAutonomous) TaskName(id string) (string, bool) {
	if id == "t-1" {
		return "Wire the route", true
	}
	return "", false
}

// contextRefRaise stores a question of the given context directly.
func contextRefRaise(t *testing.T, store *autonomous.QuestionStore, q autonomous.Question) string {
	t.Helper()
	q.Label = "decision"
	q.AskedBy = "ctx-session"
	q.AskerKind = autonomous.QuestionAskerOperator
	q.Text = "Should the retry backoff be capped at five minutes?"
	q.Options = []autonomous.QuestionOption{{ID: "yes", Label: "Yes"}, {ID: "no", Label: "No"}}
	q.Default = "yes"
	raised, err := store.Raise(q)
	if err != nil {
		t.Fatalf("Raise: %v", err)
	}
	return raised.ID
}

type contextRefBody struct {
	Question struct {
		ID         string                `json:"id"`
		ContextRef autonomous.ContextRef `json:"context_ref"`
	} `json:"question"`
}

func contextRefGet(t *testing.T, s *Server, id string) (contextRefBody, string) {
	t.Helper()
	rec := httptest.NewRecorder()
	s.handleQuestions(rec, httptest.NewRequest(http.MethodGet, "/api/questions/"+id, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET: want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var body contextRefBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v: %s", err, rec.Body.String())
	}
	return body, rec.Body.String()
}

func TestQuestionContextRefInGetCarriesViewerLink(t *testing.T) {
	s, store := polledServer(t, true)
	old := questionDocsFileExists
	questionDocsFileExists = func(p string) bool { return p == "docs/plans/plan.md" }
	t.Cleanup(func() { questionDocsFileExists = old })

	id := contextRefRaise(t, store, autonomous.Question{
		Context: autonomous.QuestionContextProposal, ProposalPath: "docs/plans/plan.md",
	})
	body, raw := contextRefGet(t, s, id)
	want := autonomous.ContextRef{
		Kind:      "proposal",
		Title:     "plan.md",
		DocPath:   "docs/plans/plan.md",
		ViewerURL: "/diagrams.html#docs/plans/plan.md",
	}
	if body.Question.ContextRef != want {
		t.Fatalf("context_ref = %+v, want %+v\n%s", body.Question.ContextRef, want, raw)
	}
}

func TestQuestionContextRefNotInStoredRecord(t *testing.T) {
	s, store := polledServer(t, true)
	id := contextRefRaise(t, store, autonomous.Question{
		Context: autonomous.QuestionContextRegistryRow, RegistryRowKey: "row 12",
	})
	body, _ := contextRefGet(t, s, id)
	if body.Question.ContextRef.Kind != "registry_row" || body.Question.ContextRef.Title != "row 12" {
		t.Fatalf("context_ref = %+v", body.Question.ContextRef)
	}
	stored, ok := store.Get(id)
	if !ok {
		t.Fatal("stored question missing")
	}
	b, err := json.Marshal(stored)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "context_ref") {
		t.Fatalf("stored record carries context_ref: %s", b)
	}
}

func TestQuestionContextRefDocOutsideDocsTreeHasNoLink(t *testing.T) {
	s, store := polledServer(t, true)
	// The real embedded tree: the file is not under docs/, so no link.
	id := contextRefRaise(t, store, autonomous.Question{
		Context: autonomous.QuestionContextProposal, ProposalPath: "internal/server/questions.go",
	})
	body, raw := contextRefGet(t, s, id)
	if body.Question.ContextRef.Kind != "proposal" || body.Question.ContextRef.ViewerURL != "" {
		t.Fatalf("context_ref = %+v\n%s", body.Question.ContextRef, raw)
	}
	if body.Question.ContextRef.DocPath != "internal/server/questions.go" {
		t.Fatalf("doc_path = %q", body.Question.ContextRef.DocPath)
	}
}

func TestQuestionContextRefNames(t *testing.T) {
	s, store := polledServer(t, true)
	s.autonomousMgr = namedAutonomous{}

	autoID := contextRefRaise(t, store, autonomous.Question{Context: autonomous.QuestionContextPrd, PRDID: "a1b2c3d4"})
	body, _ := contextRefGet(t, s, autoID)
	if got := body.Question.ContextRef; got.Kind != "automaton" || got.Title != "Nightly cleanup" || got.ID != "a1b2c3d4" {
		t.Fatalf("automaton context_ref = %+v", got)
	}

	taskID := contextRefRaise(t, store, autonomous.Question{Context: autonomous.QuestionContextTask, TaskID: "t-9"})
	body, _ = contextRefGet(t, s, taskID)
	if got := body.Question.ContextRef; got.Kind != "task" || got.Title != "t-9" {
		t.Fatalf("unresolved task context_ref = %+v, want the id as title", got)
	}
}

func TestQuestionContextRefMissingLinkIsNone(t *testing.T) {
	s, store := polledServer(t, true)
	id := contextRefRaise(t, store, autonomous.Question{Context: autonomous.QuestionContextNone})
	body, raw := contextRefGet(t, s, id)
	if body.Question.ContextRef.Kind != "none" {
		t.Fatalf("context_ref = %+v\n%s", body.Question.ContextRef, raw)
	}
}

func TestQuestionContextRefInListRows(t *testing.T) {
	s, store := polledServer(t, true)
	old := questionDocsFileExists
	questionDocsFileExists = func(string) bool { return true }
	t.Cleanup(func() { questionDocsFileExists = old })

	contextRefRaise(t, store, autonomous.Question{
		Context: autonomous.QuestionContextProposal, ProposalPath: "docs/plans/plan.md",
	})
	rec := httptest.NewRecorder()
	s.handleQuestions(rec, httptest.NewRequest(http.MethodGet, "/api/questions", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("list: want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var list struct {
		Count int `json:"count"`
		Rows  []struct {
			ID         string                `json:"id"`
			ContextRef autonomous.ContextRef `json:"context_ref"`
		} `json:"questions"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if list.Count != 1 || len(list.Rows) != 1 {
		t.Fatalf("list = %s", rec.Body.String())
	}
	if got := list.Rows[0].ContextRef; got.Kind != "proposal" || got.ViewerURL != "/diagrams.html#docs/plans/plan.md" {
		t.Fatalf("row context_ref = %+v", got)
	}
}
