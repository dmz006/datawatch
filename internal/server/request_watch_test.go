package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/federation"
	"github.com/dmz006/datawatch/internal/requestwatch"
)

type fakeRequestWatchReader struct{ obs []requestwatch.Observation }

func (f *fakeRequestWatchReader) Observations() []requestwatch.Observation { return f.obs }

func (f *fakeRequestWatchReader) Observation(id string) (requestwatch.Observation, bool) {
	for _, o := range f.obs {
		if o.SessionID == id {
			return o, true
		}
	}
	return requestwatch.Observation{}, false
}

func requestWatchFixture() []requestwatch.Observation {
	return []requestwatch.Observation{
		{SessionID: "s1", AutomatonID: "a1", TaskID: "t1", Role: requestwatch.RoleWorker, Node: "n1", Model: "m1",
			State: requestwatch.StateGenerating, LastChange: "2026-10-31T03:00:00Z", LastPoll: "2026-10-31T03:00:30Z",
			Summary: "waiting on a model request: generating"},
		{SessionID: "s2", AutomatonID: "a1", TaskID: "t2", Role: requestwatch.RoleVerifier, Node: "n1", Model: "m1",
			State: requestwatch.StateIdle, NoProgressSeconds: 2400, OverThreshold: true,
			Summary: "waiting on a model request: no progress for 40 minutes"},
		{SessionID: "s3", AutomatonID: "a2", TaskID: "t3", Role: requestwatch.RoleWorker, Node: "n2", Model: "m2",
			State: requestwatch.StateUnreachable, Error: "connection refused",
			Summary: "waiting on a model request: node unreachable, no progress for 5 minutes"},
	}
}

func newRequestWatchServer(obs []requestwatch.Observation, cfg config.RequestWatchConfig) *Server {
	s := &Server{}
	s.SetRequestWatch(&fakeRequestWatchReader{obs: obs}, func() config.RequestWatchConfig { return cfg })
	return s
}

func getRequestWatch(s *Server, query string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	s.handleRequestWatch(rec, httptest.NewRequest(http.MethodGet, "/api/autonomous/request-watch"+query, nil))
	return rec
}

func decodeRequestWatch(t *testing.T, rec *httptest.ResponseRecorder) requestwatch.Status {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var st requestwatch.Status
	if err := json.Unmarshal(rec.Body.Bytes(), &st); err != nil {
		t.Fatalf("decode: %v: %s", err, rec.Body.String())
	}
	return st
}

func requestWatchIDs(st requestwatch.Status) string {
	var ids []string
	for _, o := range st.Observations {
		ids = append(ids, o.SessionID)
	}
	return strings.Join(ids, ",")
}

func TestRequestWatchShape(t *testing.T) {
	rec := getRequestWatch(newRequestWatchServer(requestWatchFixture(), config.RequestWatchConfig{}), "")
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("content type = %q", ct)
	}
	st := decodeRequestWatch(t, rec)
	if !st.Enabled || st.PollSeconds != 30 || st.NoProgressMinutes != 30 {
		t.Fatalf("settings = %+v", st)
	}
	if len(st.Observations) != 3 {
		t.Fatalf("want 3 observations, got %d", len(st.Observations))
	}
	byID := map[string]requestwatch.Observation{}
	for _, o := range st.Observations {
		byID[o.SessionID] = o
	}
	if byID["s1"].State != requestwatch.StateGenerating || byID["s2"].State != requestwatch.StateIdle ||
		byID["s3"].State != requestwatch.StateUnreachable {
		t.Errorf("states = %+v", byID)
	}
	if !byID["s2"].OverThreshold || byID["s3"].Error == "" || byID["s1"].Role != "worker" || byID["s2"].Role != "verifier" {
		t.Errorf("fields lost: %+v", byID)
	}
	var raw map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &raw)
	first := raw["observations"].([]any)[0].(map[string]any)
	for _, k := range []string{"session_id", "automaton_id", "task_id", "role", "node", "model", "state",
		"last_change", "no_progress_seconds", "last_poll", "over_threshold", "summary"} {
		if _, ok := first[k]; !ok {
			t.Errorf("observation lacks %q", k)
		}
	}
	if _, ok := first["error"]; ok {
		t.Error("an empty error must be omitted")
	}
}

func TestRequestWatchSessionFilter(t *testing.T) {
	s := newRequestWatchServer(requestWatchFixture(), config.RequestWatchConfig{})
	if got := requestWatchIDs(decodeRequestWatch(t, getRequestWatch(s, "?session=s2"))); got != "s2" {
		t.Errorf("session filter = %q", got)
	}
}

func TestRequestWatchAutomatonFilter(t *testing.T) {
	s := newRequestWatchServer(requestWatchFixture(), config.RequestWatchConfig{})
	if got := requestWatchIDs(decodeRequestWatch(t, getRequestWatch(s, "?automaton=a1"))); got != "s1,s2" {
		t.Errorf("automaton filter = %q", got)
	}
	if got := requestWatchIDs(decodeRequestWatch(t, getRequestWatch(s, "?automaton=a1&session=s1"))); got != "s1" {
		t.Errorf("both filters = %q", got)
	}
}

func TestRequestWatchFilterMatchesNothing(t *testing.T) {
	s := newRequestWatchServer(requestWatchFixture(), config.RequestWatchConfig{})
	for _, q := range []string{"?session=nope", "?automaton=nope"} {
		rec := getRequestWatch(s, q)
		st := decodeRequestWatch(t, rec)
		if len(st.Observations) != 0 {
			t.Errorf("%s: want empty, got %d", q, len(st.Observations))
		}
		if !strings.Contains(rec.Body.String(), `"observations":[]`) {
			t.Errorf("%s: want an empty list, got %s", q, rec.Body.String())
		}
	}
}

func TestRequestWatchDisabledGivesEmpty(t *testing.T) {
	off := false
	s := newRequestWatchServer(requestWatchFixture(), config.RequestWatchConfig{Enabled: &off})
	rec := getRequestWatch(s, "")
	st := decodeRequestWatch(t, rec)
	if st.Enabled || len(st.Observations) != 0 {
		t.Fatalf("disabled: %+v", st)
	}
	if !strings.Contains(rec.Body.String(), `"observations":[]`) {
		t.Errorf("want an empty list, got %s", rec.Body.String())
	}
}

func TestRequestWatchListNeverNull(t *testing.T) {
	for name, obs := range map[string][]requestwatch.Observation{"nil": nil, "empty": {}} {
		rec := getRequestWatch(newRequestWatchServer(obs, config.RequestWatchConfig{}), "")
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"observations":[]`) {
			t.Errorf("%s: %d %s", name, rec.Code, rec.Body.String())
		}
		if strings.Contains(rec.Body.String(), "null") {
			t.Errorf("%s: body holds null: %s", name, rec.Body.String())
		}
	}
}

func TestRequestWatchUsesConfiguredSettings(t *testing.T) {
	s := newRequestWatchServer(nil, config.RequestWatchConfig{PollSeconds: 7, NoProgressMinutes: 9})
	st := decodeRequestWatch(t, getRequestWatch(s, ""))
	if st.PollSeconds != 7 || st.NoProgressMinutes != 9 {
		t.Fatalf("settings = %+v", st)
	}
}

func TestRequestWatchMethodNotAllowed(t *testing.T) {
	s := newRequestWatchServer(requestWatchFixture(), config.RequestWatchConfig{})
	for _, m := range []string{http.MethodPost, http.MethodPut, http.MethodDelete} {
		rec := httptest.NewRecorder()
		s.handleRequestWatch(rec, httptest.NewRequest(m, "/api/autonomous/request-watch", nil))
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s: want 405, got %d", m, rec.Code)
		}
		if got := rec.Header().Get("Allow"); got != http.MethodGet {
			t.Errorf("%s: Allow = %q", m, got)
		}
	}
}

func TestRequestWatchFederationCapability(t *testing.T) {
	s := newRequestWatchServer(requestWatchFixture(), config.RequestWatchConfig{})
	withCaps := func(caps ...string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/api/autonomous/request-watch", nil)
		req = req.WithContext(context.WithValue(req.Context(), sessionCapsKey, caps))
		rec := httptest.NewRecorder()
		s.handleRequestWatch(rec, req)
		return rec
	}
	deny := withCaps(federation.CapHealthRead)
	if deny.Code != http.StatusForbidden || !strings.Contains(deny.Body.String(), federation.CapAutonomousRead) {
		t.Fatalf("without the capability: want 403 naming it, got %d %q", deny.Code, deny.Body.String())
	}
	if allow := withCaps(federation.CapAutonomousRead); allow.Code != http.StatusOK {
		t.Fatalf("with the capability: want 200, got %d", allow.Code)
	}
}

func TestRequestWatchNoReaderIs503(t *testing.T) {
	rec := getRequestWatch(&Server{}, "")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("want 503, got %d", rec.Code)
	}
	if strings.TrimSpace(rec.Body.String()) == "" {
		t.Error("503 should carry a plain note")
	}
}

func TestRequestWatchHTTPServerWiring(t *testing.T) {
	reg := requestwatch.NewRegistry()
	reg.Put(requestwatch.Observation{SessionID: "s1", AutomatonID: "a1", State: requestwatch.StateIdle})
	h := &HTTPServer{api: &Server{}}
	h.SetRequestWatch(reg, nil)
	rec := getRequestWatch(h.api, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := requestWatchIDs(decodeRequestWatch(t, rec)); got != "s1" {
		t.Errorf("ids = %q, want s1", got)
	}
}
