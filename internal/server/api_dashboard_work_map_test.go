package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/session"
	"github.com/dmz006/datawatch/internal/trace"
)

// newWorkMapServer builds a server wired with an in-memory session manager
// and an empty trace store, so the work-map endpoint has both data sources.
func newWorkMapServer(t *testing.T) (*Server, *trace.Store) {
	t.Helper()
	srv := newLLMServer(t)
	st, err := trace.NewStore(t.TempDir(), trace.Defaults(), time.Now)
	if err != nil {
		t.Fatalf("trace.NewStore: %v", err)
	}
	srv.SetTraceStore(st)
	return srv, st
}

// startWorkMapSession starts a live session (in-memory tmux fake) and points
// the work-map placement tags at the given containers. It returns the
// session's FullID, which is the id the work-map and the trace share.
func startWorkMapSession(t *testing.T, srv *Server, name string, tags ...string) string {
	t.Helper()
	w := startSessionWithBody(t, srv, map[string]any{"task": name, "project_dir": t.TempDir()})
	if w.Code != http.StatusOK {
		t.Fatalf("start: status %d body: %s", w.Code, w.Body.String())
	}
	cleanupStartedSession(t, srv, w)
	var started struct {
		FullID string `json:"full_id"`
		ID     string `json:"id"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &started); err != nil {
		t.Fatalf("decode start: %v", err)
	}
	id := started.FullID
	if id == "" {
		id = started.ID
	}
	if id == "" {
		t.Fatal("start response had no session id")
	}
	if len(tags) > 0 {
		if err := srv.manager.SetSystemTags(id, tags); err != nil {
			t.Fatalf("SetSystemTags: %v", err)
		}
	}
	return id
}

// appendWorkMapTrace records one tool-call event tying the session to a task
// and an Automaton, with the optional failure and timestamp the tests need.
func appendWorkMapTrace(t *testing.T, st *trace.Store, sess, task, automaton string, failed bool, at time.Time) {
	t.Helper()
	ev := trace.Event{
		SessionID:   sess,
		TaskID:      task,
		AutomatonID: automaton,
		Agent:       "pi",
		Kind:        trace.KindToolCall,
		Name:        "bash",
		Timestamp:   at,
	}
	res := trace.Event{
		SessionID:   sess,
		TaskID:      task,
		AutomatonID: automaton,
		Agent:       "pi",
		Kind:        trace.KindToolResult,
		Name:        "bash",
		Timestamp:   at.Add(time.Millisecond),
	}
	if failed {
		res.Outcome = "error"
	}
	if n, err := st.Append(ev, res); err != nil || n != 2 {
		t.Fatalf("Append = (%d, %v), want (2, nil)", n, err)
	}
}

func getWorkMap(t *testing.T, srv *Server) (int, workMapResponse) {
	t.Helper()
	rec := httptest.NewRecorder()
	srv.handleWorkMap(rec, httptest.NewRequest(http.MethodGet, "/api/dashboard/work-map", nil))
	var body workMapResponse
	if rec.Code == http.StatusOK {
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("bad json: %v: %s", err, rec.Body.String())
		}
	}
	return rec.Code, body
}

func workMapNodeByID(t *testing.T, body workMapResponse, id string) workMapNode {
	t.Helper()
	for _, n := range body.Nodes {
		if n.ID == id {
			return n
		}
	}
	t.Fatalf("node %q not in response: %+v", id, body.Nodes)
	return workMapNode{}
}

func workMapHasEdge(t *testing.T, body workMapResponse, from, to string) {
	t.Helper()
	for _, e := range body.Edges {
		if e.From == from && e.To == to {
			return
		}
	}
	t.Fatalf("edge %s→%s not in response: %+v", from, to, body.Edges)
}

func TestWorkMapGraphFromTraceAndTags(t *testing.T) {
	srv, st := newWorkMapServer(t)
	// One live session carrying the full placement chain.
	sess := startWorkMapSession(t, srv, "build thing",
		"project:proj1", "automation:ama1", "automaton:aut1",
		"compute:node1", "lane:gpu", "role:worker")
	// The trace ties the session to a task under the same Automaton.
	appendWorkMapTrace(t, st, sess, "taskA", "aut1", false, time.Now().UTC().Truncate(time.Second))

	code, body := getWorkMap(t, srv)
	if code != http.StatusOK {
		t.Fatalf("code = %d, want 200: %s", code, "")
	}
	if !body.Trace {
		t.Fatalf("trace = false, want true (a trace store is wired)")
	}

	// Every expected kind is present.
	if body.Counts[workMapKindProject] != 1 {
		t.Errorf("project count = %d, want 1", body.Counts[workMapKindProject])
	}
	if body.Counts[workMapKindAutomation] != 1 {
		t.Errorf("automation count = %d, want 1", body.Counts[workMapKindAutomation])
	}
	if body.Counts[workMapKindAutomaton] != 1 {
		t.Errorf("automaton count = %d, want 1", body.Counts[workMapKindAutomaton])
	}
	if body.Counts[workMapKindTask] != 1 {
		t.Errorf("task count = %d, want 1", body.Counts[workMapKindTask])
	}
	if body.Counts[workMapKindSession] != 1 {
		t.Errorf("session count = %d, want 1", body.Counts[workMapKindSession])
	}

	// The session node carries its placement axes.
	snode := workMapNodeByID(t, body, sess)
	if snode.Kind != workMapKindSession {
		t.Errorf("session kind = %q, want session", snode.Kind)
	}
	if snode.Node != "node1" || snode.Lane != "gpu" || snode.Role != "worker" {
		t.Errorf("session axes = node %q lane %q role %q, want node1/gpu/worker", snode.Node, snode.Lane, snode.Role)
	}
	if snode.Status != "running" {
		t.Errorf("session status = %q, want running", snode.Status)
	}

	// The container chain and the task link.
	workMapHasEdge(t, body, "project:proj1", "automation:ama1")
	workMapHasEdge(t, body, "automation:ama1", "aut1")
	workMapHasEdge(t, body, "aut1", "taskA")
	workMapHasEdge(t, body, "taskA", sess)
	workMapHasEdge(t, body, "aut1", sess)

	// The task node shows the recorded events.
	if tn := workMapNodeByID(t, body, "taskA"); tn.Events < 2 {
		t.Errorf("task events = %d, want >= 2 (call + result)", tn.Events)
	}

	// The running strip lists the live session, newest first.
	if len(body.Running) != 1 || body.Running[0].ID != sess {
		t.Fatalf("running = %+v, want the one live session", body.Running)
	}
}

func TestWorkMapTaskCapAndOmitted(t *testing.T) {
	srv, st := newWorkMapServer(t)
	sess := startWorkMapSession(t, srv, "builder", "automaton:autcap", "compute:node1")
	base := time.Now().UTC().Truncate(time.Second)
	// One more task than the cap: the oldest must be omitted, the newest kept.
	wantCap := workMapMaxTasksPerAutomaton
	for i := 0; i <= wantCap; i++ {
		appendWorkMapTrace(t, st, sess, "task-"+string(rune('A'+i)), "autcap", false,
			base.Add(time.Duration(i)*time.Second))
	}

	code, body := getWorkMap(t, srv)
	if code != http.StatusOK {
		t.Fatalf("code = %d, want 200", code)
	}
	if got := body.Counts[workMapKindTask]; got != wantCap {
		t.Fatalf("task count = %d, want the cap %d", got, wantCap)
	}
	aut := workMapNodeByID(t, body, "autcap")
	if aut.TasksOmitted != 1 {
		t.Errorf("automaton tasks_omitted = %d, want 1", aut.TasksOmitted)
	}
	// The newest (highest letter) survives the cap; the oldest is dropped.
	workMapNodeByID(t, body, "task-"+string(rune('A'+wantCap)))
	for _, n := range body.Nodes {
		if n.ID == "task-A" {
			t.Fatalf("oldest task kept despite the cap: %+v", n)
		}
	}
}

func TestWorkMapTraceFailureSetsErrorStatus(t *testing.T) {
	srv, st := newWorkMapServer(t)
	sess := startWorkMapSession(t, srv, "flaky", "automaton:auterr", "lane:gpu")
	appendWorkMapTrace(t, st, sess, "taskF", "auterr", true, time.Now().UTC().Truncate(time.Second))

	code, body := getWorkMap(t, srv)
	if code != http.StatusOK {
		t.Fatalf("code = %d, want 200", code)
	}
	if tn := workMapNodeByID(t, body, "taskF"); tn.Status != "error" {
		t.Errorf("task status = %q, want error (a failed tool result)", tn.Status)
	}
	if an := workMapNodeByID(t, body, "auterr"); an.Status != "error" {
		t.Errorf("automaton status = %q, want error (aggregated from its task)", an.Status)
	}
}

func TestWorkMapTraceOnlyNoSessions(t *testing.T) {
	// A daemon with a trace store but no live sessions still renders the
	// Automaton/task graph from the trace.
	srv := newLLMServer(t)
	st, err := trace.NewStore(t.TempDir(), trace.Defaults(), time.Now)
	if err != nil {
		t.Fatalf("trace.NewStore: %v", err)
	}
	srv.SetTraceStore(st)
	appendWorkMapTrace(t, st, "h/ghost", "taskG", "autG", false, time.Now().UTC().Truncate(time.Second))

	code, body := getWorkMap(t, srv)
	if code != http.StatusOK {
		t.Fatalf("code = %d, want 200", code)
	}
	if !body.Trace {
		t.Fatalf("trace = false, want true")
	}
	workMapNodeByID(t, body, "autG")
	workMapNodeByID(t, body, "taskG")
	if body.Counts[workMapKindSession] != 0 {
		t.Errorf("session count = %d, want 0 (no live sessions)", body.Counts[workMapKindSession])
	}
	if len(body.Running) != 0 {
		t.Errorf("running = %+v, want empty (no live sessions)", body.Running)
	}
}

func TestWorkMapManagerOnlyNoTrace(t *testing.T) {
	// A daemon with live sessions but no trace store degrades to a
	// placement-only map (trace:false) instead of failing.
	srv := newLLMServer(t) // no trace store wired
	sess := startWorkMapSession(t, srv, "plain", "project:projX", "automation:amaX", "compute:node9")

	code, body := getWorkMap(t, srv)
	if code != http.StatusOK {
		t.Fatalf("code = %d, want 200", code)
	}
	if body.Trace {
		t.Fatalf("trace = true, want false (no trace store wired)")
	}
	workMapNodeByID(t, body, sess)
	workMapHasEdge(t, body, "project:projX", "automation:amaX")
	workMapHasEdge(t, body, "automation:amaX", sess)
}

func TestWorkMapManagerNoSessionsIsEmpty(t *testing.T) {
	// A daemon that is live (has a session manager) but has no sessions yet
	// and no trace store returns an empty map, not an error: an operator on
	// a freshly-started daemon should see an empty work map, not a 503.
	srv := newLLMServer(t) // manager present, no trace store, no sessions
	if got := len(srv.manager.ListSessions()); got != 0 {
		t.Fatalf("precondition: manager lists %d sessions, want 0", got)
	}
	rec := httptest.NewRecorder()
	srv.handleWorkMap(rec, httptest.NewRequest(http.MethodGet, "/api/dashboard/work-map", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200 (empty map)", rec.Code)
	}
	var body workMapResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body.Trace {
		t.Errorf("trace = true, want false (no trace store)")
	}
	if len(body.Nodes) != 0 || len(body.Edges) != 0 || len(body.Running) != 0 {
		t.Errorf("empty map expected, got nodes=%d edges=%d running=%d", len(body.Nodes), len(body.Edges), len(body.Running))
	}
}

func TestWorkMapNoSourcesIsUnavailable(t *testing.T) {
	// A daemon with neither a session manager nor a trace store cannot serve
	// a work map at all: that is unavailability (503), not an empty map.
	srv := &Server{}
	if srv.manager != nil || srv.traceStore != nil {
		t.Fatal("precondition: wanted a bare server with no sources")
	}
	rec := httptest.NewRecorder()
	srv.handleWorkMap(rec, httptest.NewRequest(http.MethodGet, "/api/dashboard/work-map", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("code = %d, want 503 (no sessions, no trace)", rec.Code)
	}
}

func TestWorkMapRejectsNonGet(t *testing.T) {
	srv, _ := newWorkMapServer(t)
	rec := httptest.NewRecorder()
	srv.handleWorkMap(rec, httptest.NewRequest(http.MethodPost, "/api/dashboard/work-map", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("code = %d, want 405", rec.Code)
	}
	if rec.Header().Get("Allow") != http.MethodGet {
		t.Errorf("Allow = %q, want GET", rec.Header().Get("Allow"))
	}
}

func TestWorkMapAxesParsing(t *testing.T) {
	axes := workMapAxes([]string{
		"compute:node1", "lane:gpu", "role:worker",
		"project:proj1", "automation:ama1", "automaton:aut1",
		"stage:build",    // not a work-map axis — ignored
		"backend:claude", // not a work-map axis — ignored
		"compute:later",  // first occurrence wins
		"not-a-pair",     // no colon — ignored
		"empty:",         // empty value — ignored
		"",
	})
	want := map[string]string{
		"compute":    "node1",
		"lane":       "gpu",
		"role":       "worker",
		"project":    "proj1",
		"automation": "ama1",
		"automaton":  "aut1",
	}
	if len(axes) != len(want) {
		t.Fatalf("axes = %v, want exactly %d keys", axes, len(want))
	}
	for k, v := range want {
		if axes[k] != v {
			t.Errorf("axes[%q] = %q, want %q", k, axes[k], v)
		}
	}
	if axes["stage"] != "" || axes["backend"] != "" {
		t.Errorf("non work-map axes leaked in: %v", axes)
	}
}

func TestWorkMapStatusLabelMapping(t *testing.T) {
	cases := map[string]string{
		string(session.StateRunning):      "running",
		string(session.StateWaitingInput): "waiting",
		string(session.StateComplete):     "done",
		string(session.StateFailed):       "error",
		string(session.StateKilled):       "error",
		"weird-state":                     "weird-state",
	}
	for in, want := range cases {
		if got := workMapStatusLabel(in); got != want {
			t.Errorf("workMapStatusLabel(%q) = %q, want %q", in, got, want)
		}
	}
}
