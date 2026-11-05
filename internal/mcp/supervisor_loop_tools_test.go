package mcp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/federation"
	"github.com/dmz006/datawatch/internal/session"
)

func TestSupervisorLoopMCPToolsAreListed(t *testing.T) {
	dir := t.TempDir()
	mgr, err := session.NewManager("testhost", dir, "/bin/echo", 0)
	if err != nil {
		t.Fatalf("session.NewManager: %v", err)
	}
	trackSessionManager(t, mgr)
	mgr.WithFakeTmux()
	s := New("testhost", mgr, &config.MCPConfig{Enabled: true}, dir, Options{Version: "test"})
	tools, err := s.ListTools()
	if err != nil {
		t.Fatal(err)
	}
	wantNames := map[string]bool{
		"supervisor_loop_jobs":    false,
		"supervisor_loop_runs":    false,
		"supervisor_loop_run_job": false,
		"supervisor_loop_eval":    false,
		"supervisor_loop_stats":   false,
	}
	for _, tl := range tools {
		if _, ok := wantNames[tl.Name]; ok {
			wantNames[tl.Name] = true
		}
	}
	for name, found := range wantNames {
		if !found {
			t.Errorf("tool %q is not registered", name)
		}
	}

	for name, want := range map[string]string{
		"supervisor_loop_jobs":    federation.CapAutonomousRead,
		"supervisor_loop_runs":    federation.CapAutonomousRead,
		"supervisor_loop_run_job": federation.CapAutonomousRun,
		"supervisor_loop_stats":   federation.CapAutonomousRead,
	} {
		if got := federation.MCPToolCap[name]; got != want {
			t.Errorf("federation capability for %s = %q, want %q", name, got, want)
		}
	}

	jobsTool := (&Server{}).toolSupervisorLoopJobs()
	if jobsTool.Annotations.ReadOnlyHint == nil || !*jobsTool.Annotations.ReadOnlyHint {
		t.Error("supervisor_loop_jobs must be marked read-only")
	}
	if len(jobsTool.InputSchema.Properties) != 0 {
		t.Errorf("supervisor_loop_jobs takes no arguments, schema has %v", jobsTool.InputSchema.Properties)
	}
	runsTool := (&Server{}).toolSupervisorLoopRuns()
	if runsTool.Annotations.ReadOnlyHint == nil || !*runsTool.Annotations.ReadOnlyHint {
		t.Error("supervisor_loop_runs must be marked read-only")
	}
	for _, k := range []string{"job", "outcome", "limit"} {
		if _, ok := runsTool.InputSchema.Properties[k]; !ok {
			t.Errorf("supervisor_loop_runs schema lacks %q", k)
		}
	}
	runJobTool := (&Server{}).toolSupervisorLoopRunJob()
	for _, k := range []string{"job", "input", "candidates"} {
		if _, ok := runJobTool.InputSchema.Properties[k]; !ok {
			t.Errorf("supervisor_loop_run_job schema lacks %q", k)
		}
	}
}

func TestSupervisorLoopJobsMCPHappyPath(t *testing.T) {
	var path, method, rawQuery string
	body := `{"enabled":true,"jobs":[{"name":"draft_spec","kind":"mechanical","enabled":true,"last_outcome":"ok","last_run":"2026-11-01T10:00:00Z"},` +
		`{"name":"choose_backend","kind":"model","style":"fast","backend":"claude","model":"sonnet","enabled":true,"last_outcome":"escalated"}]}`
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		path, method, rawQuery = r.URL.Path, r.Method, r.URL.RawQuery
		_, _ = w.Write([]byte(body))
	})
	res, err := s.handleSupervisorLoopJobs(context.Background(), callReq(nil))
	if err != nil || res == nil || res.IsError {
		t.Fatalf("err=%v res=%+v", err, res)
	}
	if method != http.MethodGet || path != "/api/supervisor-loop/jobs" || rawQuery != "" {
		t.Errorf("proxied %s %s?%s", method, path, rawQuery)
	}
	if text := automationText(t, res); text != body {
		t.Errorf("result differs from the daemon body:\n got %s\nwant %s", text, body)
	}
}

func TestSupervisorLoopJobsMCPRejectsArguments(t *testing.T) {
	called := false
	s := capacityTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		called = true
		_, _ = w.Write([]byte(`{}`))
	})
	res, err := s.handleSupervisorLoopJobs(context.Background(), callReq(map[string]any{"job": "draft_spec"}))
	if err != nil {
		t.Fatalf("a refusal must come back as a tool result, not a protocol error: %v", err)
	}
	if res == nil || !res.IsError {
		t.Fatalf("an argument must be refused: %+v", res)
	}
	text := automationText(t, res)
	if !strings.Contains(text, "job") || !strings.Contains(text, "usage") {
		t.Errorf("refusal does not name the argument with the usage: %s", text)
	}
	if called {
		t.Error("the daemon was called despite the refused argument")
	}
}

func TestSupervisorLoopRunsMCPHappyPathAndFilter(t *testing.T) {
	var path, method, rawQuery string
	body := `{"runs":[{"job":"draft_spec","outcome":"ok","result":"drafted","started":"2026-11-01T10:00:00Z"},{"job":"draft_spec","outcome":"escalated","retries":3,"started":"2026-11-01T09:00:00Z"}]}`
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		path, method, rawQuery = r.URL.Path, r.Method, r.URL.RawQuery
		_, _ = w.Write([]byte(body))
	})

	// No filters: the plain read.
	res, err := s.handleSupervisorLoopRuns(context.Background(), callReq(nil))
	if err != nil || res == nil || res.IsError {
		t.Fatalf("err=%v res=%+v", err, res)
	}
	if method != http.MethodGet || path != "/api/supervisor-loop/runs" || rawQuery != "" {
		t.Errorf("proxied %s %s?%s", method, path, rawQuery)
	}
	if text := automationText(t, res); text != body {
		t.Errorf("result differs from the daemon body:\n got %s\nwant %s", text, body)
	}

	// The one-filter read: job + outcome + limit must reach the daemon as the query.
	res, err = s.handleSupervisorLoopRuns(context.Background(), callReq(map[string]any{
		"job": "draft_spec", "outcome": "escalated", "limit": "25",
	}))
	if err != nil || res == nil || res.IsError {
		t.Fatalf("filtered read: err=%v res=%+v", err, res)
	}
	if rawQuery != "job=draft_spec&limit=25&outcome=escalated" {
		t.Errorf("query = %q, want the filters for the daemon", rawQuery)
	}
}

func TestSupervisorLoopRunsMCPRefusals(t *testing.T) {
	called := false
	s := capacityTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		called = true
		_, _ = w.Write([]byte(`{}`))
	})

	// An outcome outside the vocabulary is refused before the daemon.
	res, err := s.handleSupervisorLoopRuns(context.Background(), callReq(map[string]any{"outcome": "skipped"}))
	if err != nil || res == nil || !res.IsError {
		t.Fatalf("an invalid outcome must be refused: err=%v res=%+v", err, res)
	}
	if !strings.Contains(automationText(t, res), `outcome must be "ok", "validator_failed", "escalated" or "error"`) {
		t.Errorf("refusal does not name the valid outcomes: %s", automationText(t, res))
	}

	// A limit outside 1..500 is refused before the daemon.
	res, err = s.handleSupervisorLoopRuns(context.Background(), callReq(map[string]any{"limit": "501"}))
	if err != nil || res == nil || !res.IsError {
		t.Fatalf("a limit of 501 must be refused: err=%v res=%+v", err, res)
	}
	if !strings.Contains(automationText(t, res), "limit must be a whole number 1..500") {
		t.Errorf("refusal does not name the limit range: %s", automationText(t, res))
	}

	// An unknown argument is refused, naming it.
	res, err = s.handleSupervisorLoopRuns(context.Background(), callReq(map[string]any{"style": "fast"}))
	if err != nil || res == nil || !res.IsError {
		t.Fatalf("an unknown argument must be refused: err=%v res=%+v", err, res)
	}
	text := automationText(t, res)
	if !strings.Contains(text, "style") || !strings.Contains(text, "usage") {
		t.Errorf("refusal does not name the argument with the usage: %s", text)
	}
	if called {
		t.Error("the daemon was called despite the refusals")
	}
}

func TestSupervisorLoopRunJobMCPHappyPath(t *testing.T) {
	var path, method string
	var rawBody []byte
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		path, method = r.URL.Path, r.Method
		rawBody, _ = io.ReadAll(r.Body)
		_, _ = w.Write([]byte(`{"job":"draft_spec","ok":true,"result":"drafted spec v2"}`))
	})
	res, err := s.handleSupervisorLoopRunJob(context.Background(), callReq(map[string]any{
		"job": "draft_spec", "input": "draft the spec", "candidates": "backend-a, backend-b",
	}))
	if err != nil || res == nil || res.IsError {
		t.Fatalf("err=%v res=%+v", err, res)
	}
	if method != http.MethodPost || path != "/api/supervisor-loop/jobs/draft_spec/run" {
		t.Errorf("proxied %s %s", method, path)
	}
	var sent map[string]any
	if err := json.Unmarshal(rawBody, &sent); err != nil {
		t.Fatalf("request body: %v (%s)", err, rawBody)
	}
	if sent["input"] != "draft the spec" {
		t.Errorf("input = %v", sent["input"])
	}
	cands, ok := sent["candidates"].([]any)
	if !ok || len(cands) != 2 || cands[0] != "backend-a" || cands[1] != "backend-b" {
		t.Errorf("candidates = %v", sent["candidates"])
	}
	if text := automationText(t, res); !strings.Contains(text, `"ok":true`) {
		t.Errorf("result lost the daemon body: %s", text)
	}
}

func TestSupervisorLoopRunJobMCPRefusals(t *testing.T) {
	called := false
	s := capacityTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		called = true
		_, _ = w.Write([]byte(`{}`))
	})

	// The job name is required.
	res, err := s.handleSupervisorLoopRunJob(context.Background(), callReq(map[string]any{"input": "draft the spec"}))
	if err != nil || res == nil || !res.IsError {
		t.Fatalf("a missing job must be refused: err=%v res=%+v", err, res)
	}
	if !strings.Contains(automationText(t, res), "job is required") {
		t.Errorf("refusal does not say the job is required: %s", automationText(t, res))
	}

	// An unknown argument is refused, naming it.
	res, err = s.handleSupervisorLoopRunJob(context.Background(), callReq(map[string]any{"job": "draft_spec", "retries": 1}))
	if err != nil || res == nil || !res.IsError {
		t.Fatalf("an unknown argument must be refused: err=%v res=%+v", err, res)
	}
	text := automationText(t, res)
	if !strings.Contains(text, "retries") || !strings.Contains(text, "usage") {
		t.Errorf("refusal does not name the argument with the usage: %s", text)
	}
	if called {
		t.Error("the daemon was called despite the refusals")
	}
}

func TestSupervisorLoopRunJobMCPSurfacesDaemonError(t *testing.T) {
	// An unknown job name: the daemon's 404 comes back as a tool error.
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"unknown job nosuchjob"}`, http.StatusNotFound)
	})
	res, err := s.handleSupervisorLoopRunJob(context.Background(), callReq(map[string]any{"job": "nosuchjob"}))
	if err != nil {
		t.Fatalf("protocol error: %v", err)
	}
	if res == nil || !res.IsError {
		t.Fatalf("a 404 from the daemon must come back as a tool error: %+v", res)
	}
	if text := automationText(t, res); !strings.Contains(text, "nosuchjob") {
		t.Errorf("the daemon's unknown-job error must surface: %s", text)
	}

	// A 503 likewise.
	s = capacityTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "autonomous disabled", http.StatusServiceUnavailable)
	})
	res, err = s.handleSupervisorLoopRunJob(context.Background(), callReq(map[string]any{"job": "draft_spec"}))
	if err != nil || res == nil || !res.IsError {
		t.Fatalf("a 503 must come back as a tool error: err=%v res=%+v", err, res)
	}
}

func TestSupervisorLoopStatsMCP(t *testing.T) {
	var path, method string
	body := `{"enabled":true,"jobs":11,"jobs_enabled":11,"runs":2,"runs_ok":2,"last_run_age_seconds":5}`
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		path, method = r.URL.Path, r.Method
		_, _ = w.Write([]byte(body))
	})
	res, err := s.handleSupervisorLoopStats(context.Background(), callReq(nil))
	if err != nil || res == nil || res.IsError {
		t.Fatalf("err=%v res=%+v", err, res)
	}
	if method != http.MethodGet || path != "/api/supervisor-loop/stats" {
		t.Errorf("proxied %s %s", method, path)
	}
	if text := automationText(t, res); text != body {
		t.Errorf("result differs from the daemon body: %s", text)
	}
	res, _ = s.handleSupervisorLoopStats(context.Background(), callReq(map[string]any{"x": 1}))
	if res == nil || !res.IsError {
		t.Errorf("an argument must be refused: %+v", res)
	}
}
