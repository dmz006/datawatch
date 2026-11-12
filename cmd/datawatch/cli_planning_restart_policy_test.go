package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// planningRestartPolicyCLIRequest is one request the fake daemon received.
type planningRestartPolicyCLIRequest struct {
	Method string
	Path   string
	Body   map[string]any
}

// planningRestartPolicyCLIFake serves the daemon side of config-set and records
// every request in arrival order. It overrides the package-level serverURL for
// the duration of the test.
func planningRestartPolicyCLIFake(t *testing.T) *[]planningRestartPolicyCLIRequest {
	t.Helper()
	var mu sync.Mutex
	var seen []planningRestartPolicyCLIRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(b, &body)
		mu.Lock()
		seen = append(seen, planningRestartPolicyCLIRequest{Method: r.Method, Path: r.URL.Path, Body: body})
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(srv.Close)
	old := serverURL
	serverURL = srv.URL
	t.Cleanup(func() { serverURL = old })
	return &seen
}

// runPlanningRestartPolicyConfigSet runs config-set with one JSON argument.
func runPlanningRestartPolicyConfigSet(t *testing.T, arg string) error {
	t.Helper()
	cmd := newAutonomousConfigSetCmd()
	return cmd.RunE(cmd, []string{arg})
}

// The help text names the key, the three values, the how-to link and an
// example, and its planning paragraph uses Automaton wording, not PRD.
func TestPlanningRestartPolicyCLI_ConfigSetHelpNamesValuesAndHowto(t *testing.T) {
	help := newAutonomousConfigSetCmd().Long
	if !strings.Contains(help, "planning_restart_policy") {
		t.Fatalf("config-set help lacks planning_restart_policy:\n%s", help)
	}
	for _, v := range []string{"reattach", "restart", "draft"} {
		if !strings.Contains(help, v) {
			t.Errorf("config-set help does not name the %q value", v)
		}
	}
	if !strings.Contains(help, "docs/howto/restart-with-work-running.md") {
		t.Errorf("config-set help lacks the how-to link")
	}
	if !strings.Contains(help, `config-set '{"planning_restart_policy":"restart"}'`) {
		t.Errorf("config-set help lacks a planning_restart_policy example")
	}
	start := strings.Index(help, "planning_restart_policy (autonomous")
	end := strings.Index(help, "max_parallel_tasks is deprecated")
	if start < 0 || end < start {
		t.Fatalf("could not locate the planning_restart_policy paragraph:\n%s", help)
	}
	if strings.Contains(help[start:end], "PRD") {
		t.Errorf("planning_restart_policy help uses PRD wording:\n%s", help[start:end])
	}
	if !strings.Contains(help[start:end], "Automaton") {
		t.Errorf("planning_restart_policy help does not say Automaton:\n%s", help[start:end])
	}
}

// An invalid value is refused on the client: an error naming the valid values,
// and no request reaches the daemon.
func TestPlanningRestartPolicyCLI_RefusesInvalidBeforeSending(t *testing.T) {
	seen := planningRestartPolicyCLIFake(t)
	for _, arg := range []string{
		`{"planning_restart_policy":"bogus"}`,
		`{"planning_restart_policy":""}`,
		`{"planning_restart_policy":4}`,
		`{"planning_restart_policy":"nope","max_concurrent_tasks":2}`,
	} {
		err := runPlanningRestartPolicyConfigSet(t, arg)
		if err == nil {
			t.Fatalf("%s was accepted, want refused", arg)
		}
		for _, v := range []string{"reattach", "restart", "draft"} {
			if !strings.Contains(err.Error(), v) {
				t.Errorf("refusal for %s does not name %q: %v", arg, v, err)
			}
		}
	}
	if len(*seen) != 0 {
		t.Fatalf("refused values reached the daemon: %+v", *seen)
	}
}

// A valid value is written on PUT /api/config under the dotted daemon key, and
// the autonomous-manager body is not sent for it.
func TestPlanningRestartPolicyCLI_SendsValidValueToDaemonConfig(t *testing.T) {
	seen := planningRestartPolicyCLIFake(t)
	if err := runPlanningRestartPolicyConfigSet(t, `{"planning_restart_policy":"draft"}`); err != nil {
		t.Fatal(err)
	}
	if len(*seen) != 1 {
		t.Fatalf("want one request, got %d: %+v", len(*seen), *seen)
	}
	got := (*seen)[0]
	if got.Method != http.MethodPut || got.Path != "/api/config" {
		t.Fatalf("request = %s %s, want PUT /api/config", got.Method, got.Path)
	}
	if got.Body["autonomous.planning_restart_policy"] != "draft" {
		t.Fatalf("body = %v, want autonomous.planning_restart_policy=draft", got.Body)
	}
	if _, leaked := got.Body["planning_restart_policy"]; leaked {
		t.Fatalf("bare key leaked into the config body: %v", got.Body)
	}
}

// A body with the policy and other fields splits into two requests: the policy
// to /api/config first, then the rest to /api/autonomous/config without the key.
func TestPlanningRestartPolicyCLI_MixedBodySplitsRequests(t *testing.T) {
	seen := planningRestartPolicyCLIFake(t)
	if err := runPlanningRestartPolicyConfigSet(t, `{"planning_restart_policy":"restart","max_concurrent_tasks":2}`); err != nil {
		t.Fatal(err)
	}
	if len(*seen) != 2 {
		t.Fatalf("want two requests, got %d: %+v", len(*seen), *seen)
	}
	if (*seen)[0].Path != "/api/config" || (*seen)[0].Body["autonomous.planning_restart_policy"] != "restart" {
		t.Fatalf("first request = %+v, want the policy on /api/config", (*seen)[0])
	}
	second := (*seen)[1]
	if second.Path != "/api/autonomous/config" {
		t.Fatalf("second request path = %s, want /api/autonomous/config", second.Path)
	}
	if _, ok := second.Body["planning_restart_policy"]; ok {
		t.Fatalf("policy leaked into the autonomous body: %v", second.Body)
	}
	if second.Body["max_concurrent_tasks"] != float64(2) {
		t.Fatalf("autonomous body = %v, want max_concurrent_tasks=2", second.Body)
	}
}
