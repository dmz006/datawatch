// Tests for the protected-paths REST surface: GET /api/autonomous/protected-paths
// (shape, empty lists, method guard, disabled gate, federation gate, the
// automaton filter), the config round trip of autonomous.automation.protected_paths,
// protected_paths_action and rules_review_source through PUT and GET /api/config
// and PUT /api/autonomous/config, the strict refusal of a bad word or entry, and
// POST /api/autonomous/prds/{id}/set_authorized_paths.

package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/federation"
)

// protectedSpy serves a fixed protected-paths status and counts reads, so a
// refused request is shown never to reach the manager.
type protectedSpy struct {
	fakeOrchAutonomous
	status autonomous.ProtectedPathsStatus
	calls  int
	asked  []string
}

func (p *protectedSpy) ProtectedPathsStatus(id string) autonomous.ProtectedPathsStatus {
	p.calls++
	p.asked = append(p.asked, id)
	return p.status
}

func protectedFixtureStatus() autonomous.ProtectedPathsStatus {
	return autonomous.ProtectedPathsStatus{
		Action:      "hold",
		RulesSource: "base_commit",
		Paths:       []string{"AGENT.md", ".github/workflows/"},
		Held: []autonomous.ProtectedPathsHeldEntry{{
			AutomatonID: "abcd1234", AutomatonTitle: "Dogfood Automaton",
			TaskID: "t1", TaskTitle: "Wire the gate", Paths: []string{"AGENT.md"},
			Action: "hold", Reason: "the task commit changed protected paths",
			QuestionID: "q-1", Since: time.Date(2026, 11, 5, 9, 30, 0, 0, time.UTC),
			BaseCommit: "abc123", AwaitingSpecChange: true,
		}},
		Authorized: []autonomous.ProtectedPathsAuthorizedEntry{{
			AutomatonID: "abcd1234", AutomatonTitle: "Dogfood Automaton", Paths: []string{"docs/plans/"},
		}},
	}
}

func protectedGET(s *Server, query string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	s.handleProtectedPaths(rec, httptest.NewRequest(http.MethodGet, "/api/autonomous/protected-paths"+query, nil))
	return rec
}

func TestProtectedPathsRESTGetShape(t *testing.T) {
	spy := &protectedSpy{status: protectedFixtureStatus()}
	s := &Server{autonomousMgr: spy}

	rec := protectedGET(s, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET status = %d, want 200: %s", rec.Code, rec.Body)
	}
	var got struct {
		Action      string   `json:"action"`
		RulesSource string   `json:"rules_source"`
		Paths       []string `json:"paths"`
		Held        []struct {
			AutomatonID        string   `json:"automaton_id"`
			AutomatonTitle     string   `json:"automaton_title"`
			TaskID             string   `json:"task_id"`
			TaskTitle          string   `json:"task_title"`
			Paths              []string `json:"paths"`
			Action             string   `json:"action"`
			Reason             string   `json:"reason"`
			QuestionID         string   `json:"question_id"`
			Since              string   `json:"since"`
			BaseCommit         string   `json:"base_commit"`
			AwaitingSpecChange bool     `json:"awaiting_spec_change"`
		} `json:"held"`
		Authorized []struct {
			AutomatonID    string   `json:"automaton_id"`
			AutomatonTitle string   `json:"automaton_title"`
			Paths          []string `json:"paths"`
		} `json:"authorized"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v: %s", err, rec.Body)
	}
	if got.Action != "hold" || got.RulesSource != "base_commit" || len(got.Paths) != 2 {
		t.Fatalf("settings in body = %s", rec.Body)
	}
	if len(got.Held) != 1 {
		t.Fatalf("held in body = %s", rec.Body)
	}
	h := got.Held[0]
	if h.AutomatonID != "abcd1234" || h.AutomatonTitle != "Dogfood Automaton" || h.TaskID != "t1" || h.TaskTitle != "Wire the gate" ||
		len(h.Paths) != 1 || h.Paths[0] != "AGENT.md" || h.Action != "hold" || h.QuestionID != "q-1" || h.BaseCommit != "abc123" ||
		!h.AwaitingSpecChange || h.Reason == "" || !strings.HasPrefix(h.Since, "2026-11-05T09:30:00") {
		t.Errorf("held entry = %+v", h)
	}
	if len(got.Authorized) != 1 || got.Authorized[0].AutomatonID != "abcd1234" || len(got.Authorized[0].Paths) != 1 ||
		got.Authorized[0].Paths[0] != "docs/plans/" || got.Authorized[0].AutomatonTitle != "Dogfood Automaton" {
		t.Errorf("authorized entry = %+v", got.Authorized)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Errorf("Content-Type = %q", ct)
	}
}

func TestProtectedPathsRESTEmptyListsAreNotNull(t *testing.T) {
	// A manager that returns nil slices still answers [] for every list.
	empty := &protectedSpy{status: autonomous.ProtectedPathsStatus{Action: "hold", RulesSource: "base_commit"}}
	rec := protectedGET(&Server{autonomousMgr: empty}, "?automaton=nope")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}
	for _, want := range []string{`"paths":[]`, `"held":[]`, `"authorized":[]`} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Errorf("body lacks %s: %s", want, rec.Body)
		}
	}
	if strings.Contains(rec.Body.String(), "null") {
		t.Errorf("body carries null: %s", rec.Body)
	}

	// Entries whose own path lists are nil also answer [].
	odd := &protectedSpy{status: autonomous.ProtectedPathsStatus{
		Held:       []autonomous.ProtectedPathsHeldEntry{{AutomatonID: "a", TaskID: "t"}},
		Authorized: []autonomous.ProtectedPathsAuthorizedEntry{{AutomatonID: "a"}},
	}}
	rec = protectedGET(&Server{autonomousMgr: odd}, "")
	if strings.Contains(rec.Body.String(), "null") {
		t.Errorf("entry path lists carry null: %s", rec.Body)
	}
}

func TestProtectedPathsRESTGetOnly405(t *testing.T) {
	spy := &protectedSpy{status: protectedFixtureStatus()}
	s := &Server{autonomousMgr: spy}
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch} {
		rec := httptest.NewRecorder()
		s.handleProtectedPaths(rec, httptest.NewRequest(method, "/api/autonomous/protected-paths", nil))
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s: status = %d, want 405", method, rec.Code)
		}
		if rec.Header().Get("Allow") != http.MethodGet {
			t.Errorf("%s: Allow = %q, want GET", method, rec.Header().Get("Allow"))
		}
	}
	if spy.calls != 0 {
		t.Errorf("refused methods reached the manager: %d reads", spy.calls)
	}
}

func TestProtectedPathsRESTDisabled503(t *testing.T) {
	rec := protectedGET(&Server{}, "")
	if rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), "autonomous disabled") {
		t.Fatalf("with the Automata off: want 503 autonomous disabled, got %d %q", rec.Code, rec.Body)
	}
	// A manager that cannot report the status is also a 503, not a panic.
	rec = protectedGET(&Server{autonomousMgr: &fakeOrchAutonomous{}}, "")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("manager without the read: want 503, got %d", rec.Code)
	}
	// With the Automata off, even a method other than GET is refused first.
	rec = httptest.NewRecorder()
	(&Server{}).handleProtectedPaths(rec, httptest.NewRequest(http.MethodPost, "/api/autonomous/protected-paths", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST with the Automata off: want 405, got %d", rec.Code)
	}
}

func TestProtectedPathsRESTAutomatonFilter(t *testing.T) {
	spy := &protectedSpy{status: protectedFixtureStatus()}
	s := &Server{autonomousMgr: spy}
	protectedGET(s, "")
	protectedGET(s, "?automaton=abcd1234")
	if len(spy.asked) != 2 || spy.asked[0] != "" || spy.asked[1] != "abcd1234" {
		t.Errorf("ids asked = %q, want [\"\" \"abcd1234\"]", spy.asked)
	}
}

func TestProtectedPathsRESTFederationCap(t *testing.T) {
	spy := &protectedSpy{status: protectedFixtureStatus()}
	s := &Server{autonomousMgr: spy}

	deny := httptest.NewRequest(http.MethodGet, "/api/autonomous/protected-paths", nil)
	deny = deny.WithContext(context.WithValue(deny.Context(), sessionCapsKey, []string{federation.CapHealthRead}))
	rec := httptest.NewRecorder()
	s.handleProtectedPaths(rec, deny)
	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), federation.CapAutonomousRead) {
		t.Fatalf("without the read capability: want 403 naming it, got %d %q", rec.Code, rec.Body)
	}
	if spy.calls != 0 {
		t.Fatalf("a denied request reached the manager (%d reads)", spy.calls)
	}

	allow := httptest.NewRequest(http.MethodGet, "/api/autonomous/protected-paths", nil)
	allow = allow.WithContext(context.WithValue(allow.Context(), sessionCapsKey, []string{federation.CapAutonomousRead}))
	rec = httptest.NewRecorder()
	s.handleProtectedPaths(rec, allow)
	if rec.Code != http.StatusOK {
		t.Fatalf("with the read capability: want 200, got %d", rec.Code)
	}
}

func protectedConfigServer(t *testing.T) (*Server, *autonomousConfigPropagationSpy) {
	t.Helper()
	spy := &autonomousConfigPropagationSpy{}
	return &Server{
		cfg:           &config.Config{},
		cfgPath:       t.TempDir() + "/config.yaml",
		autonomousMgr: spy,
	}, spy
}

func protectedPutConfig(s *Server, patch map[string]any) *httptest.ResponseRecorder {
	b, _ := json.Marshal(patch)
	rec := httptest.NewRecorder()
	s.handlePutConfig(rec, httptest.NewRequest(http.MethodPut, "/api/config", bytes.NewReader(b)))
	return rec
}

type protectedConfigRead struct {
	Paths  []string `json:"protected_paths"`
	Action string   `json:"protected_paths_action"`
	Source string   `json:"rules_review_source"`
}

// protectedGetConfig reads the three keys back from GET /api/config.
func protectedGetConfig(t *testing.T, s *Server) protectedConfigRead {
	t.Helper()
	rec := httptest.NewRecorder()
	s.handleGetConfig(rec, httptest.NewRequest(http.MethodGet, "/api/config", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/config = %d: %s", rec.Code, rec.Body)
	}
	var got struct {
		Autonomous struct {
			Automation protectedConfigRead `json:"automation"`
		} `json:"autonomous"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode GET /api/config: %v", err)
	}
	return got.Autonomous.Automation
}

func TestProtectedPathsRESTConfigRoundTrip(t *testing.T) {
	s, spy := protectedConfigServer(t)

	// Unset reads the effective defaults: the default list, hold, base_commit.
	def := protectedGetConfig(t, s)
	if def.Action != "hold" || def.Source != "base_commit" || !reflect.DeepEqual(def.Paths, config.DefaultProtectedPaths("AGENT.md")) {
		t.Fatalf("defaults = %+v", def)
	}

	// A JSON array, the action and the source round trip.
	rec := protectedPutConfig(s, map[string]any{
		"autonomous.automation.protected_paths":        []string{"secrets/", "./deploy/*.yaml", "AGENT.md"},
		"autonomous.automation.protected_paths_action": "block",
		"autonomous.automation.rules_review_source":    "worktree",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT = %d: %s", rec.Code, rec.Body)
	}
	got := protectedGetConfig(t, s)
	if got.Action != "block" || got.Source != "worktree" || !reflect.DeepEqual(got.Paths, []string{"secrets/", "deploy/*.yaml", "AGENT.md"}) {
		t.Fatalf("after PUT = %+v", got)
	}

	// The change reaches the live manager without a restart.
	var fwd map[string]json.RawMessage
	if err := json.Unmarshal(spy.lastSetConfig, &fwd); err != nil {
		t.Fatalf("decode forwarded config: %v (%s)", err, spy.lastSetConfig)
	}
	var auto struct {
		Paths  []string `json:"protected_paths"`
		Action string   `json:"protected_paths_action"`
		Source string   `json:"rules_review_source"`
	}
	if err := json.Unmarshal(fwd["automation"], &auto); err != nil || auto.Action != "block" || auto.Source != "worktree" ||
		!reflect.DeepEqual(auto.Paths, []string{"secrets/", "deploy/*.yaml", "AGENT.md"}) {
		t.Fatalf("forwarded automation = %s (%v)", fwd["automation"], err)
	}

	// A comma separated string is the same as a list; blanks are dropped.
	if rec := protectedPutConfig(s, map[string]any{"autonomous.automation.protected_paths": " CODEOWNERS , docs/plans/,, "}); rec.Code != http.StatusOK {
		t.Fatalf("PUT comma string = %d: %s", rec.Code, rec.Body)
	}
	got = protectedGetConfig(t, s)
	if !reflect.DeepEqual(got.Paths, []string{"CODEOWNERS", "docs/plans/"}) {
		t.Errorf("comma string paths = %v", got.Paths)
	}
	// Only the touched key changed.
	if got.Action != "block" || got.Source != "worktree" {
		t.Errorf("an untouched key changed: %+v", got)
	}

	// An empty list clears the key back to the default list, on the live
	// manager too (the forward must carry the clear).
	for _, clear := range []any{[]string{}, ""} {
		if rec := protectedPutConfig(s, map[string]any{"autonomous.automation.protected_paths": []string{"x/"}}); rec.Code != http.StatusOK {
			t.Fatalf("PUT seed = %d: %s", rec.Code, rec.Body)
		}
		if rec := protectedPutConfig(s, map[string]any{"autonomous.automation.protected_paths": clear}); rec.Code != http.StatusOK {
			t.Fatalf("PUT clear %#v = %d: %s", clear, rec.Code, rec.Body)
		}
		got = protectedGetConfig(t, s)
		if !reflect.DeepEqual(got.Paths, config.DefaultProtectedPaths("AGENT.md")) {
			t.Errorf("after clearing with %#v: paths = %v, want the default list", clear, got.Paths)
		}
		fwd = nil
		if err := json.Unmarshal(spy.lastSetConfig, &fwd); err != nil {
			t.Fatalf("decode forwarded config: %v", err)
		}
		if !bytes.Contains(fwd["automation"], []byte(`"protected_paths":[]`)) {
			t.Errorf("clearing with %#v did not reach the live manager: %s", clear, fwd["automation"])
		}
	}

	// An absent key keeps the current value.
	if rec := protectedPutConfig(s, map[string]any{"autonomous.automation.protected_paths_action": "off"}); rec.Code != http.StatusOK {
		t.Fatalf("PUT action only = %d: %s", rec.Code, rec.Body)
	}
	if got = protectedGetConfig(t, s); got.Action != "off" || got.Source != "worktree" {
		t.Errorf("after action-only PUT = %+v", got)
	}

	// The same keys through PUT /api/autonomous/config.
	body := `{"automation":{"protected_paths_action":"warn","rules_review_source":"base_commit","protected_paths":["a/b.go"]}}`
	rec = httptest.NewRecorder()
	s.handleAutonomousConfig(rec, httptest.NewRequest(http.MethodPut, "/api/autonomous/config", strings.NewReader(body)))
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT /api/autonomous/config = %d: %s", rec.Code, rec.Body)
	}
}

func TestProtectedPathsRESTConfigRejectsBadWord(t *testing.T) {
	s, spy := protectedConfigServer(t)
	if rec := protectedPutConfig(s, map[string]any{
		"autonomous.automation.protected_paths_action": "warn",
		"autonomous.automation.rules_review_source":    "worktree",
	}); rec.Code != http.StatusOK {
		t.Fatalf("seed PUT = %d: %s", rec.Code, rec.Body)
	}
	calls := spy.calls

	cases := []struct {
		name  string
		patch map[string]any
		want  []string
	}{
		{"action unknown word", map[string]any{"autonomous.automation.protected_paths_action": "refuse"}, []string{"hold", "warn", "block", "off"}},
		{"action empty", map[string]any{"autonomous.automation.protected_paths_action": ""}, []string{"hold", "warn", "block", "off"}},
		{"action wrong case", map[string]any{"autonomous.automation.protected_paths_action": "HOLD"}, []string{"hold", "warn", "block", "off"}},
		{"action not a string", map[string]any{"autonomous.automation.protected_paths_action": 3}, []string{"hold", "warn", "block", "off"}},
		{"source unknown word", map[string]any{"autonomous.automation.rules_review_source": "head"}, []string{"base_commit", "worktree"}},
		{"source empty", map[string]any{"autonomous.automation.rules_review_source": ""}, []string{"base_commit", "worktree"}},
		{"source not a string", map[string]any{"autonomous.automation.rules_review_source": true}, []string{"base_commit", "worktree"}},
		{"one bad key rejects the whole PUT", map[string]any{
			"autonomous.automation.protected_paths_action": "off",
			"autonomous.automation.rules_review_source":    "nope",
		}, []string{"base_commit", "worktree"}},
	}
	for _, c := range cases {
		rec := protectedPutConfig(s, c.patch)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400: %s", c.name, rec.Code, rec.Body)
			continue
		}
		for _, w := range c.want {
			if !strings.Contains(rec.Body.String(), w) {
				t.Errorf("%s: message %q does not name %q", c.name, rec.Body.String(), w)
			}
		}
		if got := protectedGetConfig(t, s); got.Action != "warn" || got.Source != "worktree" {
			t.Errorf("%s: a refused PUT changed state to %+v", c.name, got)
		}
	}
	if spy.calls != calls {
		t.Errorf("a refused PUT reached the manager (%d forwards)", spy.calls-calls)
	}

	// The autonomous config route refuses the same words.
	for _, body := range []string{
		`{"automation":{"protected_paths_action":"refuse"}}`,
		`{"automation":{"protected_paths_action":""}}`,
		`{"automation":{"rules_review_source":"head"}}`,
	} {
		rec := httptest.NewRecorder()
		s.handleAutonomousConfig(rec, httptest.NewRequest(http.MethodPut, "/api/autonomous/config", strings.NewReader(body)))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("PUT /api/autonomous/config %s = %d, want 400", body, rec.Code)
		}
	}
}

func TestProtectedPathsRESTConfigRejectsBadEntries(t *testing.T) {
	s, spy := protectedConfigServer(t)
	if rec := protectedPutConfig(s, map[string]any{"autonomous.automation.protected_paths": []string{"keep/"}}); rec.Code != http.StatusOK {
		t.Fatalf("seed PUT = %d: %s", rec.Code, rec.Body)
	}
	calls := spy.calls

	many := make([]string, 101)
	for i := range many {
		many[i] = fmt.Sprintf("dir%d/", i)
	}
	long := strings.Repeat("a", 257)

	cases := []struct {
		name  string
		value any
		want  string
	}{
		{"absolute", []string{"/etc/passwd"}, "/etc/passwd"},
		{"absolute among good ones", []string{"ok/", "/abs/file"}, "/abs/file"},
		{"dot dot first", []string{"../outside"}, "../outside"},
		{"dot dot middle", []string{"a/../b"}, "a/../b"},
		{"dot dot only", []string{".."}, ".."},
		{"over 256 bytes", []string{long}, "256"},
		{"101 entries", many, "100"},
		{"comma string absolute", "ok/,/abs", "/abs"},
		{"comma string dot dot", "ok/,x/../y", "x/../y"},
		{"entry not a string", []any{"ok/", 5}, "string"},
		{"value is a number", 7, "list"},
		{"value is an object", map[string]any{"a": "b"}, "list"},
	}
	for _, c := range cases {
		rec := protectedPutConfig(s, map[string]any{"autonomous.automation.protected_paths": c.value})
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400: %s", c.name, rec.Code, rec.Body)
			continue
		}
		if !strings.Contains(rec.Body.String(), c.want) {
			t.Errorf("%s: message %q does not mention %q", c.name, rec.Body.String(), c.want)
		}
		if got := protectedGetConfig(t, s); !reflect.DeepEqual(got.Paths, []string{"keep/"}) {
			t.Errorf("%s: a refused PUT changed paths to %v", c.name, got.Paths)
		}
	}
	if spy.calls != calls {
		t.Errorf("a refused PUT reached the manager (%d forwards)", spy.calls-calls)
	}

	// Exactly 100 entries of exactly 256 bytes are fine.
	okEntry := strings.Repeat("b", 256)
	exact := make([]string, 100)
	for i := range exact {
		exact[i] = fmt.Sprintf("d%d/", i)
	}
	exact[0] = okEntry
	if rec := protectedPutConfig(s, map[string]any{"autonomous.automation.protected_paths": exact}); rec.Code != http.StatusOK {
		t.Errorf("100 entries with a 256-byte entry = %d, want 200: %s", rec.Code, rec.Body)
	}

	// The autonomous config route refuses the same entries.
	for _, body := range []string{
		`{"automation":{"protected_paths":["/etc/passwd"]}}`,
		`{"automation":{"protected_paths":["a/../b"]}}`,
		`{"automation":{"protected_paths":"not-a-list"}}`,
		`{"automation":{"protected_paths":[1]}}`,
	} {
		rec := httptest.NewRecorder()
		s.handleAutonomousConfig(rec, httptest.NewRequest(http.MethodPut, "/api/autonomous/config", strings.NewReader(body)))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("PUT /api/autonomous/config %s = %d, want 400", body, rec.Code)
		}
	}
}

// protectedRig is a real Manager with one held task and one authorised list,
// served through the route and the action switch.
type protectedRig struct {
	mgr    *autonomous.Manager
	s      *Server
	held   *autonomous.PRD
	clean  *autonomous.PRD
	taskID string
}

func newProtectedRig(t *testing.T) *protectedRig {
	t.Helper()
	cfg := autonomous.DefaultConfig()
	mgr, err := autonomous.NewManager(t.TempDir(), cfg, nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	mk := func(title string) *autonomous.PRD {
		prd, err := mgr.CreatePRD("spec", t.TempDir(), "", "", autonomous.EffortNormal)
		if err != nil {
			t.Fatalf("CreatePRD: %v", err)
		}
		if err := mgr.Store().SetStories(prd.ID, []autonomous.Story{
			{ID: "s1", Title: "Story", Tasks: []autonomous.Task{{ID: "t1", Title: "Task one"}}},
		}); err != nil {
			t.Fatalf("SetStories: %v", err)
		}
		prd, _ = mgr.Store().GetPRD(prd.ID)
		prd.Title = title
		if err := mgr.Store().SavePRD(prd); err != nil {
			t.Fatalf("SavePRD: %v", err)
		}
		return prd
	}
	r := &protectedRig{mgr: mgr, held: mk("Held Automaton"), clean: mk("Clean Automaton")}
	r.taskID = r.held.Story[0].Tasks[0].ID
	mgr.Store().WithLock(func() {
		r.held.Story[0].Tasks[0].ProtectedPathHold = &autonomous.ProtectedPathHold{
			Paths: []string{"AGENT.md"}, Action: "hold", QuestionID: "q-1",
			BaseCommit: "abc123", Since: time.Date(2026, 11, 5, 9, 30, 0, 0, time.UTC),
		}
	})
	if err := mgr.Store().SaveTask(&r.held.Story[0].Tasks[0]); err != nil {
		t.Fatalf("SaveTask: %v", err)
	}
	r.s = &Server{autonomousMgr: newTestAPI(mgr)}
	return r
}

func (r *protectedRig) post(t *testing.T, id, action, body string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	r.s.handleAutonomousPRDs(rec, httptest.NewRequest(http.MethodPost, "/api/autonomous/prds/"+id+"/"+action, strings.NewReader(body)))
	return rec
}

func TestProtectedPathsRESTSetAuthorizedPaths(t *testing.T) {
	r := newProtectedRig(t)
	var audits []string
	r.mgr.SetAuditFn(func(action string, _ map[string]any) { audits = append(audits, action) })

	authorized := func() []string {
		prd, _ := r.mgr.Store().GetPRD(r.held.ID)
		return prd.AuthorizedProtectedPaths
	}

	// Stores: the response is the Automaton and carries authorized_paths.
	rec := r.post(t, r.held.ID, "set_authorized_paths", `{"paths":["docs/plans/","./AGENT.md","docs/plans/"]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("set = %d: %s", rec.Code, rec.Body)
	}
	var out struct {
		ID              string   `json:"id"`
		AuthorizedPaths []string `json:"authorized_paths"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v: %s", err, rec.Body)
	}
	if out.ID != r.held.ID || !reflect.DeepEqual(out.AuthorizedPaths, []string{"docs/plans/", "AGENT.md"}) {
		t.Errorf("response = %+v; want the Automaton with the cleaned list", out)
	}
	if got := authorized(); !reflect.DeepEqual(got, []string{"docs/plans/", "AGENT.md"}) {
		t.Errorf("stored list = %v", got)
	}
	if len(audits) == 0 {
		t.Errorf("no audit entry was written for the change")
	}

	// The status read lists the authorisation and the held task.
	rec = httptest.NewRecorder()
	r.s.handleProtectedPaths(rec, httptest.NewRequest(http.MethodGet, "/api/autonomous/protected-paths", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET = %d: %s", rec.Code, rec.Body)
	}
	var st struct {
		Action string   `json:"action"`
		Paths  []string `json:"paths"`
		Held   []struct {
			AutomatonID string   `json:"automaton_id"`
			TaskID      string   `json:"task_id"`
			TaskTitle   string   `json:"task_title"`
			Paths       []string `json:"paths"`
			QuestionID  string   `json:"question_id"`
		} `json:"held"`
		Authorized []struct {
			AutomatonID string   `json:"automaton_id"`
			Paths       []string `json:"paths"`
		} `json:"authorized"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &st); err != nil {
		t.Fatalf("decode: %v: %s", err, rec.Body)
	}
	if st.Action != "hold" || len(st.Paths) == 0 {
		t.Errorf("settings = %s", rec.Body)
	}
	if len(st.Held) != 1 || st.Held[0].AutomatonID != r.held.ID || st.Held[0].TaskID != r.taskID ||
		st.Held[0].TaskTitle != "Task one" || st.Held[0].QuestionID != "q-1" || len(st.Held[0].Paths) != 1 || st.Held[0].Paths[0] != "AGENT.md" {
		t.Errorf("held = %s", rec.Body)
	}
	if len(st.Authorized) != 1 || st.Authorized[0].AutomatonID != r.held.ID || !reflect.DeepEqual(st.Authorized[0].Paths, []string{"AGENT.md", "docs/plans/"}) {
		t.Errorf("authorized = %s", rec.Body)
	}

	// The automaton filter narrows both lists.
	rec = httptest.NewRecorder()
	r.s.handleProtectedPaths(rec, httptest.NewRequest(http.MethodGet, "/api/autonomous/protected-paths?automaton="+r.clean.ID, nil))
	if !strings.Contains(rec.Body.String(), `"held":[]`) || !strings.Contains(rec.Body.String(), `"authorized":[]`) {
		t.Errorf("filter on the clean Automaton = %s; want empty lists", rec.Body)
	}

	// An empty list clears.
	rec = r.post(t, r.held.ID, "set_authorized_paths", `{"paths":[]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("clear = %d: %s", rec.Code, rec.Body)
	}
	if strings.Contains(rec.Body.String(), "authorized_paths") {
		t.Errorf("a cleared Automaton still carries authorized_paths: %s", rec.Body)
	}
	if got := authorized(); len(got) != 0 {
		t.Errorf("list after clear = %v", got)
	}
	// So does a body with no paths key.
	if rec := r.post(t, r.held.ID, "set_authorized_paths", `{"paths":["x/"]}`); rec.Code != http.StatusOK {
		t.Fatalf("set = %d", rec.Code)
	}
	if rec := r.post(t, r.held.ID, "set_authorized_paths", `{}`); rec.Code != http.StatusOK {
		t.Fatalf("clear with {} = %d: %s", rec.Code, rec.Body)
	}
	if got := authorized(); len(got) != 0 {
		t.Errorf("list after {} = %v", got)
	}
}

func TestProtectedPathsRESTSetAuthorizedPathsValidates(t *testing.T) {
	r := newProtectedRig(t)
	if rec := r.post(t, r.held.ID, "set_authorized_paths", `{"paths":["keep/"]}`); rec.Code != http.StatusOK {
		t.Fatalf("seed = %d: %s", rec.Code, rec.Body)
	}

	many := make([]string, 101)
	for i := range many {
		many[i] = fmt.Sprintf("dir%d/", i)
	}
	manyBody, _ := json.Marshal(map[string]any{"paths": many})
	longBody, _ := json.Marshal(map[string]any{"paths": []string{strings.Repeat("a", 257)}})

	cases := []struct {
		name string
		body string
		want string
	}{
		{"absolute", `{"paths":["/etc/passwd"]}`, "/etc/passwd"},
		{"dot dot", `{"paths":["ok/","a/../b"]}`, "a/../b"},
		{"over 256 bytes", string(longBody), "256"},
		{"101 entries", string(manyBody), "100"},
		{"bad json", `{"paths":`, "bad request"},
		{"paths not a list", `{"paths":"a/"}`, "bad request"},
	}
	for _, c := range cases {
		rec := r.post(t, r.held.ID, "set_authorized_paths", c.body)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400: %s", c.name, rec.Code, rec.Body)
			continue
		}
		if !strings.Contains(rec.Body.String(), c.want) {
			t.Errorf("%s: message %q does not mention %q", c.name, rec.Body.String(), c.want)
		}
		prd, _ := r.mgr.Store().GetPRD(r.held.ID)
		if !reflect.DeepEqual(prd.AuthorizedProtectedPaths, []string{"keep/"}) {
			t.Errorf("%s: a refused request changed the list to %v", c.name, prd.AuthorizedProtectedPaths)
		}
	}
}

func TestProtectedPathsRESTSetAuthorizedPathsUnknownAndMethod(t *testing.T) {
	r := newProtectedRig(t)

	if rec := r.post(t, "no-such-id", "set_authorized_paths", `{"paths":["a/"]}`); rec.Code != http.StatusNotFound {
		t.Errorf("unknown id = %d, want 404: %s", rec.Code, rec.Body)
	}
	// A refused entry on an unknown id is still a 404, not a 400.
	if rec := r.post(t, "no-such-id", "set_authorized_paths", `{"paths":["/abs"]}`); rec.Code != http.StatusNotFound {
		t.Errorf("unknown id with a bad entry = %d, want 404", rec.Code)
	}

	rec := httptest.NewRecorder()
	r.s.handleAutonomousPRDs(rec, httptest.NewRequest(http.MethodGet, "/api/autonomous/prds/"+r.held.ID+"/set_authorized_paths", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET = %d, want 405", rec.Code)
	}
}

func TestProtectedPathsRESTSetAuthorizedPathsFederationCap(t *testing.T) {
	r := newProtectedRig(t)

	deny := httptest.NewRequest(http.MethodPost, "/api/autonomous/prds/"+r.held.ID+"/set_authorized_paths", strings.NewReader(`{"paths":["a/"]}`))
	deny = deny.WithContext(context.WithValue(deny.Context(), sessionCapsKey, []string{federation.CapAutonomousRead}))
	rec := httptest.NewRecorder()
	r.s.handleAutonomousPRDs(rec, deny)
	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), federation.CapAutonomousWrite) {
		t.Fatalf("with only the read capability: want 403 naming %s, got %d %q", federation.CapAutonomousWrite, rec.Code, rec.Body)
	}
	if prd, _ := r.mgr.Store().GetPRD(r.held.ID); len(prd.AuthorizedProtectedPaths) != 0 {
		t.Fatalf("a denied request changed the list: %v", prd.AuthorizedProtectedPaths)
	}

	allow := httptest.NewRequest(http.MethodPost, "/api/autonomous/prds/"+r.held.ID+"/set_authorized_paths", strings.NewReader(`{"paths":["a/"]}`))
	allow = allow.WithContext(context.WithValue(allow.Context(), sessionCapsKey, []string{federation.CapAutonomousWrite}))
	rec = httptest.NewRecorder()
	r.s.handleAutonomousPRDs(rec, allow)
	if rec.Code != http.StatusOK {
		t.Fatalf("with the write capability: want 200, got %d %s", rec.Code, rec.Body)
	}
}

func TestProtectedPathsRESTSetAuthorizedPathsUnavailable(t *testing.T) {
	// A manager without the setter answers 503, not a panic.
	s := &Server{autonomousMgr: &fakeOrchAutonomous{}}
	rec := httptest.NewRecorder()
	s.handleAutonomousPRDs(rec, httptest.NewRequest(http.MethodPost, "/api/autonomous/prds/abcd1234/set_authorized_paths", strings.NewReader(`{"paths":[]}`)))
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("manager without the setter: want 503, got %d %s", rec.Code, rec.Body)
	}
}
