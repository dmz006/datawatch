package server

// GET /api/autonomous/infra-retries: the response shape, the Automaton filter,
// the limit rule, the method guard and the federation guard. The shape and the
// guard run against a recording double; the filter runs against a real manager
// holding stored Automata.

import (
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

// infraRetrySpy is the AutonomousAPI double for the route. It records the
// arguments of each read and serves a fixed status.
type infraRetrySpy struct {
	fakeOrchAutonomous
	status    autonomous.InfraRetryStatus
	gotID     string
	gotLimit  int
	callCount int
}

func (s *infraRetrySpy) InfraRetryStatus(id string, limit int) autonomous.InfraRetryStatus {
	s.callCount++
	s.gotID, s.gotLimit = id, limit
	return s.status
}

func infraRetryFixtureStatus() autonomous.InfraRetryStatus {
	at := time.Date(2026, 10, 30, 9, 0, 0, 0, time.UTC)
	return autonomous.InfraRetryStatus{
		InfraRetrySettings: autonomous.InfraRetrySettingsFrom(config.InfraRetryConfig{}),
		Tasks: []autonomous.InfraRetryTask{{
			AutomatonID: "abcd1234", AutomatonTitle: "Parser", TaskID: "t1", TaskTitle: "Parse the input",
			Status: autonomous.TaskReviewNeeded, RetryCount: 3, MaxTries: 3,
			Retries: []autonomous.InfraRetryEntry{{
				Number: 1, Class: config.InfraRetryClassSpawnError, Reason: "tmux: server exited",
				At: at, DelaySeconds: 60, NextAt: at.Add(time.Minute),
			}},
			Held: true, HeldCause: "spawn_error: tmux: server exited", QuestionID: "q1",
		}},
		Count: 1,
	}
}

func getInfraRetries(s *Server, query string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	s.handleAutonomousInfraRetries(rec, httptest.NewRequest(http.MethodGet, "/api/autonomous/infra-retries"+query, nil))
	return rec
}

func TestInfraRetryRouteShape(t *testing.T) {
	spy := &infraRetrySpy{status: infraRetryFixtureStatus()}
	rec := getInfraRetries(&Server{autonomousMgr: spy}, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET infra-retries: want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var got map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	for _, key := range []string{"enabled", "max_tries", "backoff_seconds", "categories", "exhausted_action",
		"retry_dependents", "raise_question", "tasks", "count"} {
		if _, ok := got[key]; !ok {
			t.Errorf("response lacks %q: %s", key, rec.Body.String())
		}
	}
	var tasks []map[string]json.RawMessage
	if err := json.Unmarshal(got["tasks"], &tasks); err != nil || len(tasks) != 1 {
		t.Fatalf("tasks: %v %s", err, got["tasks"])
	}
	for _, key := range []string{"automaton_id", "automaton_title", "task_id", "task_title", "status",
		"retry_count", "max_tries", "retries", "held", "held_cause", "question_id"} {
		if _, ok := tasks[0][key]; !ok {
			t.Errorf("task lacks %q", key)
		}
	}
	var retries []map[string]json.RawMessage
	if err := json.Unmarshal(tasks[0]["retries"], &retries); err != nil || len(retries) != 1 {
		t.Fatalf("retries: %v %s", err, tasks[0]["retries"])
	}
	for _, key := range []string{"number", "class", "reason", "at", "delay_seconds", "next_at"} {
		if _, ok := retries[0][key]; !ok {
			t.Errorf("retry lacks %q", key)
		}
	}
	if string(got["count"]) != "1" {
		t.Errorf("count = %s, want 1", got["count"])
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("Content-Type = %q", ct)
	}
}

func TestInfraRetryRoutePassesQueryToTheManager(t *testing.T) {
	cases := []struct {
		query     string
		wantID    string
		wantLimit int
	}{
		{"", "", 50},
		{"?limit=7", "", 7},
		{"?limit=1000", "", 1000},
		{"?automaton=abcd1234", "abcd1234", 50},
		{"?automaton=%20abcd1234%20&limit=3", "abcd1234", 3},
	}
	for _, c := range cases {
		spy := &infraRetrySpy{}
		if rec := getInfraRetries(&Server{autonomousMgr: spy}, c.query); rec.Code != http.StatusOK {
			t.Fatalf("%q: want 200, got %d: %s", c.query, rec.Code, rec.Body.String())
		}
		if spy.gotID != c.wantID || spy.gotLimit != c.wantLimit {
			t.Errorf("%q: manager got (%q, %d), want (%q, %d)", c.query, spy.gotID, spy.gotLimit, c.wantID, c.wantLimit)
		}
	}
}

func TestInfraRetryRouteBadLimit(t *testing.T) {
	for _, q := range []string{"?limit=0", "?limit=-1", "?limit=1001", "?limit=abc", "?limit=1.5", "?limit="} {
		spy := &infraRetrySpy{}
		rec := getInfraRetries(&Server{autonomousMgr: spy}, q)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%q: want 400, got %d", q, rec.Code)
			continue
		}
		if body := rec.Body.String(); !strings.Contains(body, "limit must be a whole number from 1 to 1000") {
			t.Errorf("%q: message = %q", q, body)
		}
		if spy.callCount != 0 {
			t.Errorf("%q: a refused limit must not reach the manager", q)
		}
	}
}

func TestInfraRetryRouteMethodsAndAvailability(t *testing.T) {
	spy := &infraRetrySpy{}
	s := &Server{autonomousMgr: spy}
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete} {
		rec := httptest.NewRecorder()
		s.handleAutonomousInfraRetries(rec, httptest.NewRequest(method, "/api/autonomous/infra-retries", nil))
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s: want 405, got %d", method, rec.Code)
		}
	}
	if spy.callCount != 0 {
		t.Errorf("refused methods must not reach the manager, got %d calls", spy.callCount)
	}
	if rec := getInfraRetries(&Server{}, ""); rec.Code != http.StatusServiceUnavailable {
		t.Errorf("no manager: want 503, got %d", rec.Code)
	}
	if rec := getInfraRetries(&Server{autonomousMgr: &fakeOrchAutonomous{}}, ""); rec.Code != http.StatusNotImplemented {
		t.Errorf("a manager without the read: want 501, got %d", rec.Code)
	}
}

func TestInfraRetryRouteFederationGuard(t *testing.T) {
	s := &Server{autonomousMgr: &infraRetrySpy{}}
	withCaps := func(caps ...string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/api/autonomous/infra-retries", nil)
		req = req.WithContext(context.WithValue(req.Context(), sessionCapsKey, caps))
		rec := httptest.NewRecorder()
		s.handleAutonomousInfraRetries(rec, req)
		return rec
	}
	deny := withCaps(federation.CapHealthRead)
	if deny.Code != http.StatusForbidden || !strings.Contains(deny.Body.String(), federation.CapAutonomousRead) {
		t.Fatalf("a peer without the read capability: want 403 naming it, got %d %q", deny.Code, deny.Body.String())
	}
	if allow := withCaps(federation.CapAutonomousRead); allow.Code != http.StatusOK {
		t.Fatalf("a peer with the read capability: want 200, got %d", allow.Code)
	}
}

// A bad method from a peer without the capability is a method error first,
// the way every read route orders its guards; the capability check comes next.
func TestInfraRetryRouteGuardOrder(t *testing.T) {
	s := &Server{autonomousMgr: &infraRetrySpy{}}
	req := httptest.NewRequest(http.MethodPost, "/api/autonomous/infra-retries?limit=0", nil)
	req = req.WithContext(context.WithValue(req.Context(), sessionCapsKey, []string{federation.CapHealthRead}))
	rec := httptest.NewRecorder()
	s.handleAutonomousInfraRetries(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST: want 405, got %d", rec.Code)
	}
	req = httptest.NewRequest(http.MethodGet, "/api/autonomous/infra-retries?limit=0", nil)
	req = req.WithContext(context.WithValue(req.Context(), sessionCapsKey, []string{federation.CapHealthRead}))
	rec = httptest.NewRecorder()
	s.handleAutonomousInfraRetries(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("a peer without the capability and a bad limit: want 403 first, got %d", rec.Code)
	}
}

// seedInfraRetryAutomaton stores an Automaton with one task that has one
// infrastructure retry on record.
func seedInfraRetryAutomaton(t *testing.T, mgr *autonomous.Manager, title string, at time.Time) string {
	t.Helper()
	prd, err := mgr.CreatePRD("test spec", t.TempDir(), "opencode", "", autonomous.EffortNormal)
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	if err := mgr.Store().SetStories(prd.ID, []autonomous.Story{
		{Title: "Story", Tasks: []autonomous.Task{{Title: title}}},
	}); err != nil {
		t.Fatalf("SetStories: %v", err)
	}
	fresh, _ := mgr.Store().GetPRD(prd.ID)
	task := &fresh.Story[0].Tasks[0]
	task.InfraRetryCount = 1
	task.InfraRetries = []autonomous.InfraRetryRecord{{
		Number: 1, Class: config.InfraRetryClassStorageWrite, Reason: "disk full", At: at, DelaySeconds: 60,
	}}
	if err := mgr.Store().SavePRD(fresh); err != nil {
		t.Fatalf("SavePRD: %v", err)
	}
	return prd.ID
}

func TestInfraRetryRouteFilterByAutomaton(t *testing.T) {
	s, mgr, _ := newMaxConcurrentTasksServer(t)
	base := time.Date(2026, 10, 30, 9, 0, 0, 0, time.UTC)
	first := seedInfraRetryAutomaton(t, mgr, "First task", base)
	second := seedInfraRetryAutomaton(t, mgr, "Second task", base.Add(time.Hour))

	decode := func(query string) autonomous.InfraRetryStatus {
		t.Helper()
		rec := getInfraRetries(s, query)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %q: want 200, got %d: %s", query, rec.Code, rec.Body.String())
		}
		var st autonomous.InfraRetryStatus
		if err := json.Unmarshal(rec.Body.Bytes(), &st); err != nil {
			t.Fatalf("decode %q: %v", query, err)
		}
		return st
	}

	all := decode("")
	if all.Count != 2 || len(all.Tasks) != 2 || all.Tasks[0].AutomatonID != second || all.Tasks[1].AutomatonID != first {
		t.Fatalf("all = %+v, want both Automata, the newest retry first", all.Tasks)
	}
	one := decode("?automaton=" + first)
	if one.Count != 1 || len(one.Tasks) != 1 || one.Tasks[0].AutomatonID != first || one.Tasks[0].TaskTitle != "First task" {
		t.Fatalf("filtered = %+v, want only the first Automaton", one.Tasks)
	}
	if got := one.Tasks[0].Retries; len(got) != 1 || got[0].Class != config.InfraRetryClassStorageWrite || got[0].Reason != "disk full" {
		t.Errorf("retries = %+v", got)
	}
	capped := decode("?limit=1")
	if capped.Count != 1 || capped.Tasks[0].AutomatonID != second {
		t.Errorf("limit 1 = %+v, want only the newest", capped.Tasks)
	}
	none := decode("?automaton=missing")
	if none.Count != 0 || len(none.Tasks) != 0 {
		t.Errorf("an unknown Automaton = %+v, want an empty list", none.Tasks)
	}
	if rec := getInfraRetries(s, "?automaton=missing"); !strings.Contains(rec.Body.String(), `"tasks":[]`) {
		t.Errorf("an empty list must encode as [], got %s", rec.Body.String())
	}
	if !all.Enabled || all.MaxTries != 3 || all.ExhaustedAction != "hold" {
		t.Errorf("settings = %+v, want the defaults", all.InfraRetrySettings)
	}
}
