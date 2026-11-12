// Tests for the uncommitted-work REST surface: GET /api/autonomous/uncommitted-work
// (method guard, disabled gate, federation gate), the config round trip of
// autonomous.automation.uncommitted_work_policy and uncommitted_work_scope
// through PUT and GET /api/config, the strict refusal of an unknown word, and
// the hold record in the Automaton views.

package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/federation"
)

// uncommittedSpy serves a fixed uncommitted-work status and counts reads, so
// a refused request is shown never to reach the manager.
type uncommittedSpy struct {
	fakeOrchAutonomous
	status autonomous.UncommittedWorkStatus
	calls  int
	asked  []string
}

func (u *uncommittedSpy) UncommittedWorkStatus(id string) autonomous.UncommittedWorkStatus {
	u.calls++
	u.asked = append(u.asked, id)
	return u.status
}

func uncommittedFixtureStatus() autonomous.UncommittedWorkStatus {
	return autonomous.UncommittedWorkStatus{
		Policy: "hold",
		Scope:  "task_files",
		Held: []autonomous.UncommittedWorkEntry{{
			AutomatonID: "abcd1234", Name: "Dogfood Automaton",
			TaskID: "t1", TaskTitle: "Wire the check", Kind: "task",
			Files: []string{"a.go"}, OutsideScope: []string{}, Reason: "files were still uncommitted after the commit",
			QuestionID: "q-1", HeldAt: time.Date(2026, 10, 30, 20, 51, 0, 0, time.UTC),
		}},
	}
}

func uncommittedGET(s *Server, query string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	s.handleUncommittedWork(rec, httptest.NewRequest(http.MethodGet, "/api/autonomous/uncommitted-work"+query, nil))
	return rec
}

func TestUncommittedRouteGetOnly(t *testing.T) {
	spy := &uncommittedSpy{status: uncommittedFixtureStatus()}
	s := &Server{autonomousMgr: spy}

	rec := uncommittedGET(s, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET status = %d, want 200: %s", rec.Code, rec.Body)
	}
	var got struct {
		Policy string `json:"policy"`
		Scope  string `json:"scope"`
		Held   []struct {
			AutomatonID  string   `json:"automaton_id"`
			Name         string   `json:"name"`
			TaskID       string   `json:"task_id"`
			Kind         string   `json:"kind"`
			Files        []string `json:"files"`
			OutsideScope []string `json:"outside_scope"`
			QuestionID   string   `json:"question_id"`
		} `json:"held"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v: %s", err, rec.Body)
	}
	if got.Policy != "hold" || got.Scope != "task_files" || len(got.Held) != 1 ||
		got.Held[0].AutomatonID != "abcd1234" || got.Held[0].Name != "Dogfood Automaton" ||
		got.Held[0].TaskID != "t1" || got.Held[0].Kind != "task" || got.Held[0].QuestionID != "q-1" ||
		len(got.Held[0].Files) != 1 || got.Held[0].OutsideScope == nil {
		t.Fatalf("body = %s", rec.Body)
	}

	// The automaton query reaches the manager verbatim.
	uncommittedGET(s, "?automaton=abcd1234")
	if len(spy.asked) != 2 || spy.asked[0] != "" || spy.asked[1] != "abcd1234" {
		t.Errorf("ids asked = %q, want [\"\" \"abcd1234\"]", spy.asked)
	}

	// A status with nothing held answers an empty list, never null.
	empty := &uncommittedSpy{status: autonomous.UncommittedWorkStatus{Policy: "hold", Scope: "task_files"}}
	rec = uncommittedGET(&Server{autonomousMgr: empty}, "?automaton=nope")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"held":[]`) {
		t.Errorf("empty read = %d %s, want 200 with \"held\":[]", rec.Code, rec.Body)
	}

	// Every other method is a 405 with an Allow header, and never reads.
	before := spy.calls
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch} {
		rec := httptest.NewRecorder()
		s.handleUncommittedWork(rec, httptest.NewRequest(method, "/api/autonomous/uncommitted-work", nil))
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s: status = %d, want 405", method, rec.Code)
		}
		if rec.Header().Get("Allow") != http.MethodGet {
			t.Errorf("%s: Allow = %q, want GET", method, rec.Header().Get("Allow"))
		}
	}
	if spy.calls != before {
		t.Errorf("refused methods reached the manager: %d reads", spy.calls-before)
	}
}

func TestUncommittedRouteDisabled503(t *testing.T) {
	rec := uncommittedGET(&Server{}, "")
	if rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), "autonomous disabled") {
		t.Fatalf("with the Automata off: want 503 autonomous disabled, got %d %q", rec.Code, rec.Body)
	}
	// A manager that cannot report the status is also a 503, not a panic.
	rec = uncommittedGET(&Server{autonomousMgr: &fakeOrchAutonomous{}}, "")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("manager without the read: want 503, got %d", rec.Code)
	}
}

func TestUncommittedRouteFederationCap(t *testing.T) {
	spy := &uncommittedSpy{status: uncommittedFixtureStatus()}
	s := &Server{autonomousMgr: spy}

	deny := httptest.NewRequest(http.MethodGet, "/api/autonomous/uncommitted-work", nil)
	deny = deny.WithContext(context.WithValue(deny.Context(), sessionCapsKey, []string{federation.CapHealthRead}))
	rec := httptest.NewRecorder()
	s.handleUncommittedWork(rec, deny)
	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), federation.CapAutonomousRead) {
		t.Fatalf("without the read capability: want 403 naming it, got %d %q", rec.Code, rec.Body)
	}
	if spy.calls != 0 {
		t.Fatalf("a denied request reached the manager (%d reads)", spy.calls)
	}

	allow := httptest.NewRequest(http.MethodGet, "/api/autonomous/uncommitted-work", nil)
	allow = allow.WithContext(context.WithValue(allow.Context(), sessionCapsKey, []string{federation.CapAutonomousRead}))
	rec = httptest.NewRecorder()
	s.handleUncommittedWork(rec, allow)
	if rec.Code != http.StatusOK {
		t.Fatalf("with the read capability: want 200, got %d", rec.Code)
	}
}

func uncommittedConfigServer(t *testing.T) (*Server, *autonomousConfigPropagationSpy) {
	t.Helper()
	spy := &autonomousConfigPropagationSpy{}
	return &Server{
		cfg:           &config.Config{},
		cfgPath:       t.TempDir() + "/config.yaml",
		autonomousMgr: spy,
	}, spy
}

func uncommittedPutConfig(s *Server, patch map[string]any) *httptest.ResponseRecorder {
	b, _ := json.Marshal(patch)
	rec := httptest.NewRecorder()
	s.handlePutConfig(rec, httptest.NewRequest(http.MethodPut, "/api/config", bytes.NewReader(b)))
	return rec
}

// uncommittedGetConfig reads autonomous.automation uncommitted keys back from
// GET /api/config.
func uncommittedGetConfig(t *testing.T, s *Server) (policy, scope string) {
	t.Helper()
	rec := httptest.NewRecorder()
	s.handleGetConfig(rec, httptest.NewRequest(http.MethodGet, "/api/config", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/config = %d: %s", rec.Code, rec.Body)
	}
	var got struct {
		Autonomous struct {
			Automation struct {
				Policy string `json:"uncommitted_work_policy"`
				Scope  string `json:"uncommitted_work_scope"`
			} `json:"automation"`
		} `json:"autonomous"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode GET /api/config: %v", err)
	}
	return got.Autonomous.Automation.Policy, got.Autonomous.Automation.Scope
}

func TestUncommittedConfigPutRoundTrip(t *testing.T) {
	s, spy := uncommittedConfigServer(t)

	// Unset reads the defaults.
	if p, sc := uncommittedGetConfig(t, s); p != "hold" || sc != "task_files" {
		t.Fatalf("defaults = %q/%q, want hold/task_files", p, sc)
	}

	rec := uncommittedPutConfig(s, map[string]any{
		"autonomous.automation.uncommitted_work_policy": "warn",
		"autonomous.automation.uncommitted_work_scope":  "worktree",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT = %d: %s", rec.Code, rec.Body)
	}
	if p, sc := uncommittedGetConfig(t, s); p != "warn" || sc != "worktree" {
		t.Fatalf("after PUT = %q/%q, want warn/worktree", p, sc)
	}

	// The change reaches the live manager without a restart.
	var fwd map[string]json.RawMessage
	if err := json.Unmarshal(spy.lastSetConfig, &fwd); err != nil {
		t.Fatalf("decode forwarded config: %v (%s)", err, spy.lastSetConfig)
	}
	var auto struct {
		Policy string `json:"uncommitted_work_policy"`
		Scope  string `json:"uncommitted_work_scope"`
	}
	if err := json.Unmarshal(fwd["automation"], &auto); err != nil || auto.Policy != "warn" || auto.Scope != "worktree" {
		t.Fatalf("forwarded automation = %s (%v), want warn/worktree", fwd["automation"], err)
	}

	// An absent key keeps the current value.
	if rec := uncommittedPutConfig(s, map[string]any{"autonomous.automation.uncommitted_work_policy": "off"}); rec.Code != http.StatusOK {
		t.Fatalf("PUT policy only = %d: %s", rec.Code, rec.Body)
	}
	if p, sc := uncommittedGetConfig(t, s); p != "off" || sc != "worktree" {
		t.Fatalf("after policy-only PUT = %q/%q, want off/worktree", p, sc)
	}

	// The same keys through PUT /api/autonomous/config.
	body := `{"automation":{"uncommitted_work_policy":"hold"}}`
	rec = httptest.NewRecorder()
	s.handleAutonomousConfig(rec, httptest.NewRequest(http.MethodPut, "/api/autonomous/config", strings.NewReader(body)))
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT /api/autonomous/config = %d: %s", rec.Code, rec.Body)
	}
}

func TestUncommittedConfigPutRejectsUnknownWord(t *testing.T) {
	s, spy := uncommittedConfigServer(t)
	if rec := uncommittedPutConfig(s, map[string]any{
		"autonomous.automation.uncommitted_work_policy": "warn",
		"autonomous.automation.uncommitted_work_scope":  "worktree",
	}); rec.Code != http.StatusOK {
		t.Fatalf("seed PUT = %d: %s", rec.Code, rec.Body)
	}
	calls := spy.calls

	cases := []struct {
		name  string
		patch map[string]any
		want  []string
	}{
		{"policy unknown word", map[string]any{"autonomous.automation.uncommitted_work_policy": "block"}, []string{"hold", "warn", "off"}},
		{"policy empty", map[string]any{"autonomous.automation.uncommitted_work_policy": ""}, []string{"hold", "warn", "off"}},
		{"policy wrong case", map[string]any{"autonomous.automation.uncommitted_work_policy": "HOLD"}, []string{"hold", "warn", "off"}},
		{"policy not a string", map[string]any{"autonomous.automation.uncommitted_work_policy": 3}, []string{"hold", "warn", "off"}},
		{"scope unknown word", map[string]any{"autonomous.automation.uncommitted_work_scope": "everything"}, []string{"task_files", "worktree"}},
		{"scope empty", map[string]any{"autonomous.automation.uncommitted_work_scope": ""}, []string{"task_files", "worktree"}},
		{"one bad key rejects the whole PUT", map[string]any{
			"autonomous.automation.uncommitted_work_policy": "off",
			"autonomous.automation.uncommitted_work_scope":  "nope",
		}, []string{"task_files", "worktree"}},
	}
	for _, c := range cases {
		rec := uncommittedPutConfig(s, c.patch)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400: %s", c.name, rec.Code, rec.Body)
			continue
		}
		for _, w := range c.want {
			if !strings.Contains(rec.Body.String(), w) {
				t.Errorf("%s: message %q does not name %q", c.name, rec.Body.String(), w)
			}
		}
		if p, sc := uncommittedGetConfig(t, s); p != "warn" || sc != "worktree" {
			t.Errorf("%s: a refused PUT changed state to %q/%q", c.name, p, sc)
		}
	}
	if spy.calls != calls {
		t.Errorf("a refused PUT reached the manager (%d forwards)", spy.calls-calls)
	}

	// The autonomous config route refuses the same words.
	for _, body := range []string{
		`{"automation":{"uncommitted_work_policy":"block"}}`,
		`{"automation":{"uncommitted_work_scope":""}}`,
	} {
		rec := httptest.NewRecorder()
		s.handleAutonomousConfig(rec, httptest.NewRequest(http.MethodPut, "/api/autonomous/config", strings.NewReader(body)))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("PUT /api/autonomous/config %s = %d, want 400", body, rec.Code)
		}
	}
}

func TestUncommittedViewCarriesHold(t *testing.T) {
	mgr, err := autonomous.NewManager(t.TempDir(), autonomous.DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	held, err := mgr.CreatePRD("spec", t.TempDir(), "", "", autonomous.EffortNormal)
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	clean, err := mgr.CreatePRD("spec", t.TempDir(), "", "", autonomous.EffortNormal)
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	for _, id := range []string{held.ID, clean.ID} {
		if err := mgr.Store().SetStories(id, []autonomous.Story{
			{ID: "s1", Title: "Story", Tasks: []autonomous.Task{{ID: "t1", Title: "Task one"}, {ID: "t2", Title: "Task two"}}},
		}); err != nil {
			t.Fatalf("SetStories: %v", err)
		}
	}
	p, _ := mgr.Store().GetPRD(held.ID)
	mgr.Store().WithLock(func() {
		p.Story[0].Tasks[0].UncommittedWork = &autonomous.UncommittedWorkHold{
			Kind: "task", Files: []string{"a.go"}, QuestionID: "q-1", HeldAt: time.Now(),
		}
		p.UncommittedWork = &autonomous.UncommittedWorkHold{
			Kind: "worktree", Files: []string{"README.md"}, QuestionID: "q-2", HeldAt: time.Now(),
		}
	})
	if err := mgr.Store().SavePRD(p); err != nil {
		t.Fatalf("SavePRD: %v", err)
	}
	s := &Server{autonomousMgr: newTestAPI(mgr)}

	get := func(t *testing.T, id, query string) map[string]any {
		t.Helper()
		rec := httptest.NewRecorder()
		s.handleAutonomousPRDs(rec, httptest.NewRequest(http.MethodGet, "/api/autonomous/prds/"+id+query, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s%s = %d: %s", id, query, rec.Code, rec.Body)
		}
		var out map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("decode: %v", err)
		}
		return out
	}
	hold := func(v any) map[string]any {
		m, _ := v.(map[string]any)
		return m
	}
	taskHold := func(t *testing.T, list any, taskID string) map[string]any {
		t.Helper()
		tasks, _ := list.([]any)
		for _, e := range tasks {
			if task, _ := e.(map[string]any); task["id"] == taskID {
				return hold(task["uncommitted_work"])
			}
		}
		t.Fatalf("task %s not in %v", taskID, list)
		return nil
	}

	// Full detail: the Automaton hold and the task hold.
	full := get(t, held.ID, "")
	if h := hold(full["uncommitted_work"]); h == nil || h["kind"] != "worktree" || h["question_id"] != "q-2" {
		t.Errorf("full detail Automaton hold = %v", full["uncommitted_work"])
	}
	stories, _ := full["stories"].([]any)
	story, _ := stories[0].(map[string]any)
	if h := taskHold(t, story["tasks"], "t1"); h == nil || h["kind"] != "task" || h["question_id"] != "q-1" {
		t.Errorf("full detail task hold = %v", h)
	}
	if h := taskHold(t, story["tasks"], "t2"); h != nil {
		t.Errorf("a task with no hold carries one: %v", h)
	}

	// Summary and task views.
	if h := hold(get(t, held.ID, "?detail=summary")["uncommitted_work"]); h == nil || h["kind"] != "worktree" {
		t.Errorf("summary view Automaton hold = %v", h)
	}
	tasks := get(t, held.ID, "?detail=tasks")
	if h := hold(tasks["uncommitted_work"]); h == nil || h["kind"] != "worktree" {
		t.Errorf("task view Automaton hold = %v", h)
	}
	if h := taskHold(t, tasks["tasks"], "t1"); h == nil || h["question_id"] != "q-1" {
		t.Errorf("task view task hold = %v", h)
	}
	if h := taskHold(t, tasks["tasks"], "t2"); h != nil {
		t.Errorf("task view: a task with no hold carries one: %v", h)
	}

	// An Automaton with no hold omits the key everywhere.
	for _, q := range []string{"", "?detail=summary", "?detail=tasks"} {
		if _, ok := get(t, clean.ID, q)["uncommitted_work"]; ok {
			t.Errorf("clean Automaton %q view carries uncommitted_work", q)
		}
	}
	rec := httptest.NewRecorder()
	s.handleAutonomousPRDs(rec, httptest.NewRequest(http.MethodGet, "/api/autonomous/prds/"+clean.ID+"?detail=tasks", nil))
	if strings.Contains(rec.Body.String(), "uncommitted_work") {
		t.Errorf("clean task view mentions uncommitted_work: %s", rec.Body)
	}
}
