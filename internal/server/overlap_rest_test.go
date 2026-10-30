package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
)

// overlapReadServer is a server over the in-memory Automata of the create
// tests, in the given create-search mode ("" reads the default). With no
// seed it holds the one earlier Automaton the create tests use.
func overlapReadServer(mode string, seed ...*autonomous.PRD) *Server {
	if len(seed) == 0 {
		seed = []*autonomous.PRD{overlapEarlier()}
	}
	return &Server{
		autonomousMgr: newOverlapFake(seed...),
		cfg: &config.Config{Autonomous: config.AutonomousConfig{
			Automation: config.AutonomousAutomationConfig{CreateSearchMode: mode},
		}},
	}
}

// overlapReadGET sends one GET to the overlap route with the given query.
func overlapReadGET(s *Server, q url.Values) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	s.handleAutonomousOverlap(rec, httptest.NewRequest(http.MethodGet, "/api/autonomous/overlap?"+q.Encode(), nil))
	return rec
}

// overlapReadWarnings decodes a 200 reply and returns its warnings.
func overlapReadWarnings(t *testing.T, rec *httptest.ResponseRecorder) []autonomous.OverlapWarning {
	t.Helper()
	return overlapReadDecode(t, rec).Warnings
}

// overlapReadDecode decodes a 200 reply into the route's reply type.
func overlapReadDecode(t *testing.T, rec *httptest.ResponseRecorder) overlapReadReply {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}
	var out overlapReadReply
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode reply: %v, body=%s", err, rec.Body.String())
	}
	return out
}

// TestOverlapReadRefusesNoQuery: a title and a spec that are both empty, or
// missing, is a 400 and the search never runs.
func TestOverlapReadRefusesNoQuery(t *testing.T) {
	cases := map[string]url.Values{
		"no parameters":        {},
		"blank title":          {"title": {"   "}},
		"empty spec":           {"spec": {""}},
		"only a limit":         {"limit": {"5"}},
		"blank title and spec": {"title": {" "}, "spec": {"\t"}},
	}
	for name, q := range cases {
		t.Run(name, func(t *testing.T) {
			s := overlapReadServer(config.CreateSearchModeShadow)
			searched := 0
			s.overlapSearch = func(*autonomous.PRD, float64) ([]autonomous.OverlapWarning, error) {
				searched++
				return nil, nil
			}
			rec := overlapReadGET(s, q)
			if rec.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want 400, body=%s", rec.Code, rec.Body.String())
			}
			if searched != 0 {
				t.Errorf("search ran %d times for a refused request, want 0", searched)
			}
		})
	}
}

// TestOverlapReadRefusesBadLimit: a limit that is not a whole number from 1
// to 50 is a 400; 50 itself is allowed.
func TestOverlapReadRefusesBadLimit(t *testing.T) {
	for _, bad := range []string{"0", "-1", "51", "abc", "2.5"} {
		s := overlapReadServer(config.CreateSearchModeShadow)
		rec := overlapReadGET(s, url.Values{"title": {overlapSpec}, "limit": {bad}})
		if rec.Code != http.StatusBadRequest {
			t.Errorf("limit %q: status = %d, want 400", bad, rec.Code)
		}
	}
	s := overlapReadServer(config.CreateSearchModeShadow)
	rec := overlapReadGET(s, url.Values{"title": {overlapSpec}, "limit": {"50"}})
	if rec.Code != http.StatusOK {
		t.Errorf("limit 50: status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}
}

// TestOverlapReadReturnsFields: the reply carries warnings in the create
// reply's shape, the threshold in force and the mode in force.
func TestOverlapReadReturnsFields(t *testing.T) {
	s := overlapReadServer("")
	rec := overlapReadGET(s, url.Values{"title": {overlapSpec}})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}

	var fields map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &fields); err != nil {
		t.Fatalf("decode reply: %v", err)
	}
	for _, key := range []string{"warnings", "threshold", "mode"} {
		if _, ok := fields[key]; !ok {
			t.Errorf("reply lacks %q: %s", key, rec.Body.String())
		}
	}

	out := overlapReadDecode(t, rec)
	if out.Mode != config.CreateSearchModeShadow {
		t.Errorf("mode = %q, want the default %q", out.Mode, config.CreateSearchModeShadow)
	}
	if out.Threshold != config.DefaultAutomationCreateSearchThreshold {
		t.Errorf("threshold = %v, want the default %v", out.Threshold, config.DefaultAutomationCreateSearchThreshold)
	}
	found := false
	for _, w := range out.Warnings {
		if w.Kind == "automaton" && w.ID == overlapEarlier().ID {
			found = true
		}
	}
	if !found {
		t.Errorf("the earlier Automaton is not among the warnings: %+v", out.Warnings)
	}
}

// TestOverlapReadWorksWithModeOff: mode off runs nothing on create, but the
// read still searches, reports the mode off, and stores nothing.
func TestOverlapReadWorksWithModeOff(t *testing.T) {
	s := overlapReadServer(config.CreateSearchModeOff)
	before := len(s.autonomousMgr.ListPRDs())

	out := overlapReadDecode(t, overlapReadGET(s, url.Values{"spec": {overlapSpec}}))
	if out.Mode != config.CreateSearchModeOff {
		t.Errorf("mode = %q, want %q", out.Mode, config.CreateSearchModeOff)
	}
	if len(out.Warnings) == 0 {
		t.Errorf("mode off: the read found no warnings for the earlier Automaton")
	}
	if after := len(s.autonomousMgr.ListPRDs()); after != before {
		t.Errorf("the read changed the store: %d Automata before, %d after", before, after)
	}
}

// TestOverlapReadLimitCapsWarnings: the limit keeps the best matches only,
// and the default keeps up to ten.
func TestOverlapReadLimitCapsWarnings(t *testing.T) {
	var seed []*autonomous.PRD
	for i := 1; i <= 4; i++ {
		seed = append(seed, &autonomous.PRD{ID: fmt.Sprintf("prd-%d", i), Title: overlapSpec, Spec: overlapSpec})
	}
	s := overlapReadServer(config.CreateSearchModeShadow, seed...)

	if got := len(overlapReadWarnings(t, overlapReadGET(s, url.Values{"title": {overlapSpec}, "limit": {"2"}}))); got != 2 {
		t.Errorf("limit 2: %d warnings, want 2", got)
	}
	if got := len(overlapReadWarnings(t, overlapReadGET(s, url.Values{"title": {overlapSpec}}))); got != 4 {
		t.Errorf("default limit: %d warnings, want all 4", got)
	}
}

// TestOverlapReadSearchFailureIs500: a search that fails answers 500, not an
// empty list that would read as "no overlap".
func TestOverlapReadSearchFailureIs500(t *testing.T) {
	s := overlapReadServer(config.CreateSearchModeShadow)
	s.overlapSearch = func(*autonomous.PRD, float64) ([]autonomous.OverlapWarning, error) {
		return nil, errors.New("finder broke")
	}
	rec := overlapReadGET(s, url.Values{"title": {overlapSpec}})
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500, body=%s", rec.Code, rec.Body.String())
	}
}

// TestOverlapReadRefusesNonGET: the route is a read, so any other method is
// a 405.
func TestOverlapReadRefusesNonGET(t *testing.T) {
	s := overlapReadServer(config.CreateSearchModeShadow)
	rec := httptest.NewRecorder()
	s.handleAutonomousOverlap(rec, httptest.NewRequest(http.MethodPost, "/api/autonomous/overlap?title=x", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST status = %d, want 405", rec.Code)
	}
}

// TestOverlapReadNoManagerIs503: with the autonomous manager down the route
// says so rather than searching nothing.
func TestOverlapReadNoManagerIs503(t *testing.T) {
	s := &Server{}
	rec := overlapReadGET(s, url.Values{"title": {overlapSpec}})
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", rec.Code)
	}
}
