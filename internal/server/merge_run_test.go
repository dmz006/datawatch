package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/autonomous"
)

// mergeRunSpy is a Manager double for the run route: it records the runs the
// Runner reports, as the real Manager does.
type mergeRunSpy struct {
	fakeOrchAutonomous
	repo string
	runs []autonomous.MergePipelineRun
}

func (m *mergeRunSpy) RecordMergeRun(r autonomous.MergePipelineRun)  { m.runs = append(m.runs, r) }
func (m *mergeRunSpy) MergeBaseRepo() string                         { return m.repo }
func (m *mergeRunSpy) MergeQuestionStore() *autonomous.QuestionStore { return nil }

func postMergeRun(s *Server, method, body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	s.handleAutonomousMergeRun(rec, httptest.NewRequest(method, "/api/autonomous/merge/run", strings.NewReader(body)))
	return rec
}

func TestMergeRunRoute_OnlyPost(t *testing.T) {
	s := &Server{autonomousMgr: &mergeRunSpy{repo: t.TempDir()}}
	if rec := postMergeRun(s, http.MethodGet, ""); rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET = %d, want 405", rec.Code)
	}
}

func TestMergeRunRoute_NeedsBaseRepo(t *testing.T) {
	s := &Server{autonomousMgr: &mergeRunSpy{}}
	if rec := postMergeRun(s, http.MethodPost, `{"title":"t","branches":["b"]}`); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("no base repo = %d, want 503", rec.Code)
	}
}

// The route reaches the Runner: a request the Runner refuses is still
// reported to the recorder, so it shows in pipeline_runs.
func TestMergeRunRoute_CallsRunnerAndRecords(t *testing.T) {
	spy := &mergeRunSpy{repo: t.TempDir()}
	s := &Server{autonomousMgr: spy}
	rec := postMergeRun(s, http.MethodPost, `{"title":"","branches":["b"]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}
	var out struct {
		Run   autonomous.MergePipelineRun `json:"run"`
		Error string                      `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.Error == "" || out.Run.Result != autonomous.MergeRunFailed || out.Run.ID == "" {
		t.Fatalf("unexpected response: %+v", out)
	}
	if len(spy.runs) != 1 || spy.runs[0].ID != out.Run.ID {
		t.Fatalf("recorder saw %d runs, want the one returned", len(spy.runs))
	}
}

func TestMergeRunRoute_BadBody(t *testing.T) {
	s := &Server{autonomousMgr: &mergeRunSpy{repo: t.TempDir()}}
	if rec := postMergeRun(s, http.MethodPost, `{`); rec.Code != http.StatusBadRequest {
		t.Fatalf("bad body = %d, want 400", rec.Code)
	}
}
