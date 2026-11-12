package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
)

// mergeRunSpy is a Manager double for the run route: it records the runs the
// Runner reports, as the real Manager does, and captures the triage notes
// and alert-sink calls the route makes.
type mergeRunSpy struct {
	fakeOrchAutonomous
	repo    string
	runs    []autonomous.MergePipelineRun
	notes   []mergeRunSpyNote
	alerts  []mergeRunSpyAlert
	SinkOff bool // no alert sink wired, as a daemon before its sink setup
}

type mergeRunSpyNote struct{ prdID, source, text string }
type mergeRunSpyAlert struct{ level, title, body string }

func (m *mergeRunSpy) RecordMergeRun(r autonomous.MergePipelineRun)  { m.runs = append(m.runs, r) }
func (m *mergeRunSpy) MergeBaseRepo() string                         { return m.repo }
func (m *mergeRunSpy) MergeQuestionStore() *autonomous.QuestionStore { return nil }
func (m *mergeRunSpy) AppendTriageNote(prdID, source, text string) error {
	m.notes = append(m.notes, mergeRunSpyNote{prdID, source, text})
	return nil
}
func (m *mergeRunSpy) AlertSink() func(level, title, body string) string {
	if m.SinkOff {
		return nil
	}
	return func(level, title, body string) string {
		m.alerts = append(m.alerts, mergeRunSpyAlert{level, title, body})
		return "alert-1"
	}
}

func postMergeRun(s *Server, method, body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	s.handleAutonomousMergeRun(rec, httptest.NewRequest(method, "/api/autonomous/merge/run", strings.NewReader(body)))
	return rec
}

func TestMergeRunRoute_OnlyPost(t *testing.T) {
	s := &Server{autonomousMgr: &mergeRunSpy{repo: t.TempDir()}, dataDir: t.TempDir()}
	if rec := postMergeRun(s, http.MethodGet, ""); rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET = %d, want 405", rec.Code)
	}
}

func TestMergeRunRoute_NeedsBaseRepo(t *testing.T) {
	s := &Server{autonomousMgr: &mergeRunSpy{}, dataDir: t.TempDir()}
	if rec := postMergeRun(s, http.MethodPost, `{"title":"t","branches":["b"]}`); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("no base repo = %d, want 503", rec.Code)
	}
}

// The route reaches the Runner: a request the Runner refuses is still
// reported to the recorder, so it shows in pipeline_runs.
func TestMergeRunRoute_CallsRunnerAndRecords(t *testing.T) {
	spy := &mergeRunSpy{repo: t.TempDir()}
	s := &Server{autonomousMgr: spy, dataDir: t.TempDir()}
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
	s := &Server{autonomousMgr: &mergeRunSpy{repo: t.TempDir()}, dataDir: t.TempDir()}
	if rec := postMergeRun(s, http.MethodPost, `{`); rec.Code != http.StatusBadRequest {
		t.Fatalf("bad body = %d, want 400", rec.Code)
	}
}

// lockedMergeRunSpy is a run-route double whose Manager also supplies the
// merge lock store, or fails to open it.
type lockedMergeRunSpy struct {
	mergeRunSpy
	locks   *autonomous.MergeLocks
	lockErr error
}

func (m *lockedMergeRunSpy) MergeLocks() (*autonomous.MergeLocks, error) {
	return m.locks, m.lockErr
}

func TestNewMergeRunnerWiresTheMergeLock(t *testing.T) {
	locks, err := autonomous.NewMergeLocks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	withLocks := newMergeRunner(&lockedMergeRunSpy{locks: locks}, config.MergePipelineConfig{}, t.TempDir(), "demo", t.TempDir(), nil)
	if withLocks.Locks == nil {
		t.Fatal("the runner should take the Manager's lock store")
	}
	if _, err := withLocks.Locks.Acquire("/r", autonomous.MergeLockHolder{RunID: "r1", AutomatonID: "a1"}); err != nil {
		t.Fatalf("the wired lock should be the Manager's store: %v", err)
	}
	if _, held := locks.Status("/r"); !held {
		t.Fatal("the lock taken through the runner must be the Manager's own lock")
	}
}

func TestNewMergeRunnerRefusesWhenLockStoreIsBroken(t *testing.T) {
	broken := newMergeRunner(&lockedMergeRunSpy{lockErr: errors.New("corrupt")}, config.MergePipelineConfig{}, t.TempDir(), "demo", t.TempDir(), nil)
	if broken.Locks == nil {
		t.Fatal("a broken lock store must refuse runs, not leave them unlocked")
	}
	if _, err := broken.Locks.Acquire("/r", autonomous.MergeLockHolder{RunID: "r1"}); err == nil {
		t.Fatal("a broken lock store must refuse every lock")
	}
}

func TestNewMergeRunnerWithoutLockSourceTakesNoLock(t *testing.T) {
	if r := newMergeRunner(&mergeRunSpy{}, config.MergePipelineConfig{}, t.TempDir(), "demo", t.TempDir(), nil); r.Locks != nil {
		t.Fatal("a Manager without a lock store runs with no lock")
	}
}
