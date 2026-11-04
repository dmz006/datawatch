package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/trace"
)

var traceTestNow = time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)

// traceFixtureServer returns a server whose trace store holds two sessions
// of one Automaton and one session of another, on a fixed clock.
func traceFixtureServer(t *testing.T) *Server {
	t.Helper()
	s := bl90Server(t)
	st, err := trace.NewStore(t.TempDir(), trace.Settings{}, func() time.Time { return traceTestNow })
	if err != nil {
		t.Fatal(err)
	}
	at := func(sec int) time.Time { return traceTestNow.Add(time.Duration(sec) * time.Second) }
	evs := []trace.Event{
		{SessionID: "s1", AutomatonID: "a1", TaskID: "t1", Backend: "pi", Kind: trace.KindToolCall, Name: "Read", ArgSummary: "x.go", Timestamp: at(1)},
		{SessionID: "s1", AutomatonID: "a1", TaskID: "t1", Backend: "pi", Kind: trace.KindToolResult, Name: "Read", Outcome: "error: nope", DurationMS: 40, Timestamp: at(2)},
		{SessionID: "s1", AutomatonID: "a1", TaskID: "t1", Backend: "pi", Kind: trace.KindModelRequest, Name: "m", TokensIn: 10, TokensOut: 5, Timestamp: at(3)},
		{SessionID: "s2", AutomatonID: "a1", TaskID: "t2", Backend: "ollama", Kind: trace.KindToolCall, Name: "Bash", Timestamp: at(4)},
		{SessionID: "s3", AutomatonID: "a2", TaskID: "t3", Backend: "pi", Kind: trace.KindCapabilityCheck, Name: "config.write", Outcome: "denied", Timestamp: at(5)},
	}
	if _, err := st.Append(evs...); err != nil {
		t.Fatal(err)
	}
	if err := st.SetCoverage("s1", trace.CoverageFull); err != nil {
		t.Fatal(err)
	}
	if err := st.SetCoverage("s2", trace.CoverageEventsOnly); err != nil {
		t.Fatal(err)
	}
	s.SetTraceStore(st)
	return s
}

func traceGet(t *testing.T, h http.HandlerFunc, target string) *httptest.ResponseRecorder {
	t.Helper()
	rr := httptest.NewRecorder()
	h(rr, httptest.NewRequest(http.MethodGet, target, nil))
	return rr
}

func traceDecode(t *testing.T, rr *httptest.ResponseRecorder) map[string]json.RawMessage {
	t.Helper()
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rr.Code, rr.Body.String())
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(rr.Body.Bytes(), &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestTraceSession(t *testing.T) {
	s := traceFixtureServer(t)
	m := traceDecode(t, traceGet(t, s.handleTraceSession, "/api/trace/session/s1"))
	var evs []trace.Event
	if err := json.Unmarshal(m["events"], &evs); err != nil || len(evs) != 3 {
		t.Fatalf("events = %d err=%v", len(evs), err)
	}
	var ro trace.Rollup
	if err := json.Unmarshal(m["rollup"], &ro); err != nil {
		t.Fatal(err)
	}
	if ro.Key != "s1" || ro.ModelRequests != 1 || ro.TokensIn != 10 {
		t.Errorf("rollup = %+v", ro)
	}
	var idx trace.IndexEntry
	if err := json.Unmarshal(m["index"], &idx); err != nil || idx.Coverage != trace.CoverageFull {
		t.Errorf("index = %+v err=%v", idx, err)
	}
}

func TestTraceSessionFilters(t *testing.T) {
	s := traceFixtureServer(t)
	count := func(q string) int {
		m := traceDecode(t, traceGet(t, s.handleTraceSession, "/api/trace/session/s1?"+q))
		var evs []trace.Event
		_ = json.Unmarshal(m["events"], &evs)
		return len(evs)
	}
	if n := count("errors=true"); n != 1 {
		t.Errorf("errors=true -> %d", n)
	}
	if n := count("kind=model_request"); n != 1 {
		t.Errorf("kind filter -> %d", n)
	}
	if n := count("tool=Read"); n != 2 {
		t.Errorf("tool filter -> %d", n)
	}
	if n := count("limit=2"); n != 2 {
		t.Errorf("limit -> %d", n)
	}
}

func TestTraceSessionErrors(t *testing.T) {
	s := traceFixtureServer(t)
	for target, want := range map[string]int{
		"/api/trace/session/nope":            http.StatusNotFound,
		"/api/trace/session/":                http.StatusBadRequest,
		"/api/trace/session/a/b":             http.StatusBadRequest,
		"/api/trace/session/s1?kind=bogus":   http.StatusBadRequest,
		"/api/trace/session/s1?errors=maybe": http.StatusBadRequest,
		"/api/trace/session/s1?limit=-1":     http.StatusBadRequest,
	} {
		if rr := traceGet(t, s.handleTraceSession, target); rr.Code != want {
			t.Errorf("%s -> %d, want %d", target, rr.Code, want)
		}
	}
	rr := httptest.NewRecorder()
	s.handleTraceSession(rr, httptest.NewRequest(http.MethodPost, "/api/trace/session/s1", nil))
	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST -> %d", rr.Code)
	}
}

func TestTraceAutomaton(t *testing.T) {
	s := traceFixtureServer(t)
	m := traceDecode(t, traceGet(t, s.handleTraceAutomaton, "/api/trace/automaton/a1"))
	var sessions []string
	_ = json.Unmarshal(m["sessions"], &sessions)
	if strings.Join(sessions, ",") != "s1,s2" {
		t.Errorf("sessions = %v", sessions)
	}
	var evs []trace.Event
	_ = json.Unmarshal(m["events"], &evs)
	if len(evs) != 4 {
		t.Fatalf("events = %d", len(evs))
	}
	for i := 1; i < len(evs); i++ {
		if evs[i].Timestamp.Before(evs[i-1].Timestamp) {
			t.Error("events not ordered by time")
		}
	}
	var ro trace.Rollup
	_ = json.Unmarshal(m["rollup"], &ro)
	if ro.Key != "a1" || ro.Events != 4 {
		t.Errorf("rollup = %+v", ro)
	}
	if rr := traceGet(t, s.handleTraceAutomaton, "/api/trace/automaton/zz"); rr.Code != http.StatusNotFound {
		t.Errorf("unknown automaton -> %d", rr.Code)
	}
	if rr := traceGet(t, s.handleTraceAutomaton, "/api/trace/automaton/a1?backend=ollama"); rr.Code != http.StatusOK ||
		!strings.Contains(rr.Body.String(), `"total":1`) {
		t.Errorf("backend filter: %d %s", rr.Code, rr.Body.String())
	}
}

func TestTraceRollup(t *testing.T) {
	s := traceFixtureServer(t)
	rolls := func(q string) []trace.Rollup {
		m := traceDecode(t, traceGet(t, s.handleTraceRollup, "/api/trace/rollup"+q))
		var out []trace.Rollup
		_ = json.Unmarshal(m["rollups"], &out)
		return out
	}
	if got := rolls(""); len(got) != 3 || got[0].Dimension != trace.DimSession {
		t.Errorf("default dimension: %+v", got)
	}
	if got := rolls("?dimension=automaton"); len(got) != 2 || got[0].Key != "a1" || got[0].Events != 4 {
		t.Errorf("automaton: %+v", got)
	}
	if got := rolls("?dimension=backend&key=pi"); len(got) != 1 || got[0].Key != "pi" {
		t.Errorf("backend key: %+v", got)
	}
	if got := rolls("?dimension=backend&key=none"); got == nil || len(got) != 0 {
		t.Errorf("unknown key should be an empty list: %+v", got)
	}
	if rr := traceGet(t, s.handleTraceRollup, "/api/trace/rollup?dimension=planet"); rr.Code != http.StatusBadRequest {
		t.Errorf("bad dimension -> %d", rr.Code)
	}
	if rr := traceGet(t, s.handleTraceRollup, "/api/trace/rollup?loop_threshold=0"); rr.Code != http.StatusBadRequest {
		t.Errorf("bad loop_threshold -> %d", rr.Code)
	}
}

func TestTraceCoverage(t *testing.T) {
	s := traceFixtureServer(t)
	m := traceDecode(t, traceGet(t, s.handleTraceCoverage, "/api/trace/coverage"))
	var rows []traceCoverageRow
	_ = json.Unmarshal(m["sessions"], &rows)
	if len(rows) != 3 {
		t.Fatalf("rows = %+v", rows)
	}
	want := map[string]trace.Coverage{"s1": trace.CoverageFull, "s2": trace.CoverageEventsOnly, "s3": "unknown"}
	for _, r := range rows {
		if r.Coverage != want[r.Session] {
			t.Errorf("%s coverage = %q", r.Session, r.Coverage)
		}
	}
	var by map[string]map[string]int
	_ = json.Unmarshal(m["by_backend"], &by)
	if by["pi"]["full"] != 1 || by["pi"]["unknown"] != 1 || by["ollama"]["events-only"] != 1 {
		t.Errorf("by_backend = %v", by)
	}
	m = traceDecode(t, traceGet(t, s.handleTraceCoverage, "/api/trace/coverage?backend=ollama"))
	_ = json.Unmarshal(m["sessions"], &rows)
	if len(rows) != 1 || rows[0].Session != "s2" {
		t.Errorf("backend filter: %+v", rows)
	}
}

func TestTraceRoutesNeedStore(t *testing.T) {
	s := bl90Server(t)
	for target, h := range map[string]http.HandlerFunc{
		"/api/trace/session/s1":   s.handleTraceSession,
		"/api/trace/automaton/a1": s.handleTraceAutomaton,
		"/api/trace/rollup":       s.handleTraceRollup,
		"/api/trace/coverage":     s.handleTraceCoverage,
	} {
		if rr := traceGet(t, h, target); rr.Code != http.StatusServiceUnavailable {
			t.Errorf("%s without a store -> %d", target, rr.Code)
		}
	}
}

func TestTraceStats(t *testing.T) {
	s := traceFixtureServer(t)
	m := traceDecode(t, traceGet(t, s.handleTraceStats, "/api/trace/stats"))
	want := map[string]float64{"sessions": 3, "events": 5, "error_events": 2, "appended_total": 5, "append_errors_total": 0}
	for k, v := range want {
		var got float64
		if err := json.Unmarshal(m[k], &got); err != nil || got != v {
			t.Errorf("%s = %v (err %v), want %v", k, got, err, v)
		}
	}
	if got := s.TraceStats(); got.Events != 5 || got.Sessions != 3 {
		t.Errorf("TraceStats = %+v", got)
	}
}

func TestTraceStatsNeedsStoreAndGet(t *testing.T) {
	s := bl90Server(t)
	if rr := traceGet(t, s.handleTraceStats, "/api/trace/stats"); rr.Code != http.StatusServiceUnavailable {
		t.Errorf("no store: status %d, want 503", rr.Code)
	}
	if got := s.TraceStats(); got.Events != 0 {
		t.Errorf("TraceStats without a store = %+v", got)
	}
	s = traceFixtureServer(t)
	rr := httptest.NewRecorder()
	s.handleTraceStats(rr, httptest.NewRequest(http.MethodPost, "/api/trace/stats", nil))
	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST: status %d, want 405", rr.Code)
	}
}
