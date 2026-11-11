package mcp

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

func TestUnlandedMCPToolRegistered(t *testing.T) {
	have := map[string]bool{}
	for _, n := range mergeTestToolNames(t) {
		have[n] = true
	}
	for _, want := range UnlandedWorkToolNames() {
		if !have[want] {
			t.Errorf("tool %q is not registered", want)
		}
	}
	tool := (&Server{}).toolAutonomousUnlandedWork()
	if tool.Name != "autonomous_unlanded_work" {
		t.Errorf("tool name = %q", tool.Name)
	}
	if hint := tool.Annotations.ReadOnlyHint; hint == nil || !*hint {
		t.Error("autonomous_unlanded_work must be marked read-only")
	}
	for _, field := range []string{"branches", "automaton_id", "title", "branch", "commits",
		"last_activity", "recommendation", "reason"} {
		if !strings.Contains(tool.Description, `"`+field+`"`) {
			t.Errorf("description does not name %q", field)
		}
	}
}

func TestUnlandedMCPProxiesRoute(t *testing.T) {
	var path, method string
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		path, method = r.URL.Path, r.Method
		_, _ = w.Write([]byte(`{"branches":[{"automaton_id":"abcd1234","branch":"automaton/abcd1234","commits":2}]}`))
	})
	res, err := s.handleAutonomousUnlandedWork(context.Background(), callReq(nil))
	if err != nil || res == nil || res.IsError {
		t.Fatalf("err=%v res=%+v", err, res)
	}
	if method != http.MethodGet || path != "/api/autonomous/unlanded-work" {
		t.Errorf("proxied %s %s; want GET the unlanded-work route", method, path)
	}
	if text := automationText(t, res); !strings.Contains(text, `"commits":2`) {
		t.Errorf("result lost the daemon body: %s", text)
	}
}

func TestUnlandedMCPReportsDaemonError(t *testing.T) {
	s := capacityTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "autonomous disabled", http.StatusServiceUnavailable)
	})
	res, err := s.handleAutonomousUnlandedWork(context.Background(), callReq(nil))
	if err != nil || res == nil || !res.IsError {
		t.Fatalf("err=%v res=%+v; want an error result", err, res)
	}
}
