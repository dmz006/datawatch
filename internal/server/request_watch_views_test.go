package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/requestwatch"
	"github.com/dmz006/datawatch/internal/session"
)

// newRequestWatchViewServer is a Server with a real session manager and the
// given observations wired as the request-watch reader.
func newRequestWatchViewServer(t *testing.T, obs []requestwatch.Observation) *Server {
	t.Helper()
	sm, err := session.NewManager("h", t.TempDir(), "echo", 0)
	if err != nil {
		t.Fatalf("session.NewManager: %v", err)
	}
	trackSessionManager(t, sm)
	s := &Server{manager: sm}
	if obs != nil {
		s.SetRequestWatch(&fakeRequestWatchReader{obs: obs}, func() config.RequestWatchConfig { return config.RequestWatchConfig{} })
	}
	return s
}

func marshalMap(t *testing.T, v any) map[string]json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("unmarshal: %v: %s", err, b)
	}
	return m
}

func decodeObservation(t *testing.T, raw json.RawMessage) requestwatch.Observation {
	t.Helper()
	var o requestwatch.Observation
	if err := json.Unmarshal(raw, &o); err != nil {
		t.Fatalf("decode observation: %v: %s", err, raw)
	}
	return o
}

func TestRequestWatchViewSession(t *testing.T) {
	s := newRequestWatchViewServer(t, requestWatchFixture())
	m := marshalMap(t, s.sessionView(&session.Session{ID: "s1", FullID: "s1"}))
	raw, ok := m["model_request"]
	if !ok {
		t.Fatalf("model_request missing: %v", m)
	}
	o := decodeObservation(t, raw)
	if o.State != requestwatch.StateGenerating || o.Summary != "waiting on a model request: generating" {
		t.Errorf("observation = %+v", o)
	}
	if _, ok := m["inherited_tags"]; !ok {
		t.Errorf("inherited_tags dropped")
	}
}

func TestRequestWatchViewAbsent(t *testing.T) {
	s := newRequestWatchViewServer(t, requestWatchFixture())
	m := marshalMap(t, s.sessionView(&session.Session{ID: "other", FullID: "other"}))
	if _, ok := m["model_request"]; ok {
		t.Errorf("model_request present for a session without an observation: %s", m["model_request"])
	}
}

func TestRequestWatchViewList(t *testing.T) {
	s := newRequestWatchViewServer(t, requestWatchFixture())
	views := s.sessionViews([]*session.Session{
		{ID: "s1", FullID: "s1"}, {ID: "s2", FullID: "s2"}, {ID: "none", FullID: "none"}, {ID: "s3", FullID: "s3"},
	})
	if len(views) != 4 {
		t.Fatalf("views = %d", len(views))
	}
	want := []requestwatch.State{requestwatch.StateGenerating, requestwatch.StateIdle, "", requestwatch.StateUnreachable}
	for i, v := range views {
		m := marshalMap(t, v)
		raw, ok := m["model_request"]
		if want[i] == "" {
			if ok {
				t.Errorf("entry %d carries model_request: %s", i, raw)
			}
			continue
		}
		if !ok {
			t.Errorf("entry %d missing model_request", i)
			continue
		}
		if got := decodeObservation(t, raw); got.State != want[i] || got.SessionID != v.ID {
			t.Errorf("entry %d = %+v, want state %s", i, got, want[i])
		}
	}
}

// The daemon always has a schedule store, and the session list then takes a
// different path from sessionViews; the observation must be on that path too.
func TestRequestWatchViewListWithScheduleStore(t *testing.T) {
	s := sessionViewServer(t, 1, 20,
		&session.Session{ID: "s1", FullID: "s1", State: session.StateRunning},
		&session.Session{ID: "none", FullID: "none", State: session.StateRunning},
	)
	st, err := session.NewScheduleStore(filepath.Join(t.TempDir(), "sched.json"))
	if err != nil {
		t.Fatal(err)
	}
	s.SetScheduleStore(st)
	s.SetRequestWatch(&fakeRequestWatchReader{obs: requestWatchFixture()}, func() config.RequestWatchConfig { return config.RequestWatchConfig{} })

	rr := httptest.NewRecorder()
	s.handleSessions(rr, httptest.NewRequest(http.MethodGet, "/api/sessions", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("list: %d %s", rr.Code, rr.Body.String())
	}
	var rows []map[string]json.RawMessage
	if err := json.Unmarshal(rr.Body.Bytes(), &rows); err != nil {
		t.Fatalf("decode: %v: %s", err, rr.Body.String())
	}
	seen := map[string]bool{}
	for _, row := range rows {
		var id string
		_ = json.Unmarshal(row["full_id"], &id)
		raw, has := row["model_request"]
		seen[id] = has
		if id == "s1" {
			if !has {
				t.Errorf("session s1 has an observation but its list row carries no model_request")
				continue
			}
			if o := decodeObservation(t, raw); o.State != requestwatch.StateGenerating {
				t.Errorf("observation = %+v", o)
			}
		}
		if id == "none" && has {
			t.Errorf("model_request on a session without an observation: %s", raw)
		}
		if _, ok := row["scheduled_count"]; !ok {
			t.Errorf("scheduled_count dropped from the row of %s", id)
		}
	}
	if len(seen) != 2 {
		t.Fatalf("rows = %v", seen)
	}
}

// With the watch switched off the views carry nothing at once, even though the
// poller still holds its last observations until its next tick.
func TestRequestWatchViewSwitchedOffCarriesNothing(t *testing.T) {
	s := newRequestWatchViewServer(t, requestWatchFixture())
	off := false
	s.SetRequestWatch(&fakeRequestWatchReader{obs: requestWatchFixture()}, func() config.RequestWatchConfig {
		return config.RequestWatchConfig{Enabled: &off}
	})
	m := marshalMap(t, s.sessionView(&session.Session{ID: "s1", FullID: "s1"}))
	if raw, ok := m["model_request"]; ok {
		t.Errorf("model_request present with the watch switched off: %s", raw)
	}
	if got := s.automatonObservations("a1"); len(got) != 0 {
		t.Errorf("task observations with the watch switched off: %+v", got)
	}
	on := true
	s.SetRequestWatch(&fakeRequestWatchReader{obs: requestWatchFixture()}, func() config.RequestWatchConfig {
		return config.RequestWatchConfig{Enabled: &on}
	})
	if _, ok := marshalMap(t, s.sessionView(&session.Session{ID: "s1", FullID: "s1"}))["model_request"]; !ok {
		t.Errorf("model_request missing with the watch switched on")
	}
}

func TestRequestWatchViewReaderUnset(t *testing.T) {
	s := newRequestWatchViewServer(t, nil)
	m := marshalMap(t, s.sessionView(&session.Session{ID: "s1", FullID: "s1"}))
	if _, ok := m["model_request"]; ok {
		t.Errorf("model_request present with no reader wired")
	}
	srv, id := newPRDDetailTestServer(t)
	for _, q := range []string{"?detail=tasks", ""} {
		rec := httptest.NewRecorder()
		srv.handleAutonomousPRDs(rec, httptest.NewRequest(http.MethodGet, "/api/autonomous/prds/"+id+q, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("status %d", rec.Code)
		}
		if strings.Contains(rec.Body.String(), "model_request") {
			t.Errorf("model_request in a view with no reader (%q): %s", q, rec.Body.String())
		}
	}
}

// newRequestWatchTaskServer builds two Automata whose tasks share the ids t1
// and t2, with the given observations wired.
func newRequestWatchTaskServer(t *testing.T, obs []requestwatch.Observation) (*Server, string, string) {
	t.Helper()
	mgr, err := autonomous.NewManager(t.TempDir(), autonomous.DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("autonomous.NewManager: %v", err)
	}
	mk := func(status autonomous.TaskStatus) string {
		prd, err := mgr.CreatePRD("spec", t.TempDir(), "", "", autonomous.EffortNormal)
		if err != nil {
			t.Fatalf("CreatePRD: %v", err)
		}
		if err := mgr.Store().SetStories(prd.ID, []autonomous.Story{{ID: "s1", Title: "Story", Tasks: []autonomous.Task{
			{ID: "t1", Title: "One"}, {ID: "t2", Title: "Two"},
		}}}); err != nil {
			t.Fatalf("SetStories: %v", err)
		}
		prd, _ = mgr.Store().GetPRD(prd.ID)
		prd.Status = autonomous.PRDRunning
		prd.Story[0].Tasks[0].Status = status
		prd.Story[0].Tasks[1].Status = autonomous.TaskInProgress
		if err := mgr.Store().SavePRD(prd); err != nil {
			t.Fatalf("SavePRD: %v", err)
		}
		return prd.ID
	}
	idA := mk(autonomous.TaskVerifying)
	idB := mk(autonomous.TaskInProgress)
	s := &Server{autonomousMgr: newTestAPI(mgr)}
	for i := range obs {
		switch obs[i].AutomatonID {
		case "A":
			obs[i].AutomatonID = idA
		case "B":
			obs[i].AutomatonID = idB
		}
	}
	s.SetRequestWatch(&fakeRequestWatchReader{obs: obs}, nil)
	return s, idA, idB
}

type taskRow struct {
	ID           string                    `json:"id"`
	Status       string                    `json:"status"`
	ModelRequest *requestwatch.Observation `json:"model_request"`
}

func getTaskRows(t *testing.T, s *Server, id string) []taskRow {
	t.Helper()
	rec := httptest.NewRecorder()
	s.handleAutonomousPRDs(rec, httptest.NewRequest(http.MethodGet, "/api/autonomous/prds/"+id+"?detail=tasks", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Tasks []taskRow `json:"tasks"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return body.Tasks
}

func TestRequestWatchViewTaskOwnObservation(t *testing.T) {
	s, idA, idB := newRequestWatchTaskServer(t, []requestwatch.Observation{
		{SessionID: "sa", AutomatonID: "A", TaskID: "t2", Role: requestwatch.RoleWorker, State: requestwatch.StateGenerating,
			Summary: "waiting on a model request: generating"},
		{SessionID: "sb", AutomatonID: "B", TaskID: "t2", Role: requestwatch.RoleWorker, State: requestwatch.StateIdle,
			OverThreshold: true, Summary: "waiting on a model request: no progress for 40 minutes"},
	})
	a := getTaskRows(t, s, idA)
	b := getTaskRows(t, s, idB)
	if a[0].ModelRequest != nil || b[0].ModelRequest != nil {
		t.Errorf("task t1 has no observation but carries one: %+v %+v", a[0].ModelRequest, b[0].ModelRequest)
	}
	if a[1].ModelRequest == nil || a[1].ModelRequest.SessionID != "sa" {
		t.Errorf("Automaton A task t2 = %+v, want session sa", a[1].ModelRequest)
	}
	if b[1].ModelRequest == nil || b[1].ModelRequest.SessionID != "sb" ||
		b[1].ModelRequest.Summary != "waiting on a model request: no progress for 40 minutes" {
		t.Errorf("Automaton B task t2 = %+v, want session sb", b[1].ModelRequest)
	}
	if a[1].Status != string(autonomous.TaskInProgress) {
		t.Errorf("status changed: %s", a[1].Status)
	}
}

func TestRequestWatchViewTaskVerifying(t *testing.T) {
	s, idA, _ := newRequestWatchTaskServer(t, []requestwatch.Observation{
		{SessionID: "sv", AutomatonID: "A", TaskID: "t1", Role: requestwatch.RoleVerifier, State: requestwatch.StateGenerating,
			Summary: "waiting on a model request: generating"},
	})
	rows := getTaskRows(t, s, idA)
	if rows[0].Status != string(autonomous.TaskVerifying) {
		t.Fatalf("status = %s, want verifying", rows[0].Status)
	}
	o := rows[0].ModelRequest
	if o == nil || o.Role != requestwatch.RoleVerifier || o.Summary != "waiting on a model request: generating" {
		t.Errorf("verifying task observation = %+v", o)
	}
}

func TestRequestWatchViewTaskDetailFull(t *testing.T) {
	s, idA, _ := newRequestWatchTaskServer(t, []requestwatch.Observation{
		{SessionID: "sa", AutomatonID: "A", TaskID: "t2", Role: requestwatch.RoleWorker, State: requestwatch.StateGenerating,
			Summary: "waiting on a model request: generating"},
	})
	rec := httptest.NewRecorder()
	s.handleAutonomousPRDs(rec, httptest.NewRequest(http.MethodGet, "/api/autonomous/prds/"+idA, nil))
	var doc struct {
		Stories []struct {
			Tasks []taskRow `json:"tasks"`
		} `json:"stories"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatalf("decode: %v", err)
	}
	tasks := doc.Stories[0].Tasks
	if tasks[0].ModelRequest != nil || tasks[1].ModelRequest == nil || tasks[1].ModelRequest.SessionID != "sa" {
		t.Errorf("full detail tasks = %+v %+v", tasks[0].ModelRequest, tasks[1].ModelRequest)
	}
}
