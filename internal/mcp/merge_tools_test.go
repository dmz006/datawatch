package mcp

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/session"
)

func mergeTestToolNames(t *testing.T) []string {
	t.Helper()
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
	names := make([]string, 0, len(tools))
	for _, tl := range tools {
		names = append(names, tl.Name)
	}
	return names
}

func TestMergeMCP_ToolRegistered(t *testing.T) {
	have := map[string]bool{}
	for _, n := range mergeTestToolNames(t) {
		have[n] = true
	}
	for _, want := range MergeToolNames() {
		if !have[want] {
			t.Errorf("tool %q is not registered", want)
		}
	}
}

func TestMergeMCP_MergeReadsRoute(t *testing.T) {
	var path, method, rawQuery string
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		path, method, rawQuery = r.URL.Path, r.Method, r.URL.RawQuery
		_, _ = w.Write([]byte(`{"mode":"shadow","alert_minutes":30,"automata":[],"shadow_decisions":[]}`))
	})
	res, err := s.handleAutonomousMerge(context.Background(), callReq(nil))
	if err != nil || res == nil || res.IsError {
		t.Fatalf("err=%v res=%+v", err, res)
	}
	if method != http.MethodGet || path != "/api/autonomous/merge" || rawQuery != "" {
		t.Errorf("proxied %s %s?%s", method, path, rawQuery)
	}
	if text := automationText(t, res); !strings.Contains(text, `"mode":"shadow"`) {
		t.Errorf("result lost the daemon body: %s", text)
	}
}

func TestMergeMCP_SafeToDeleteDescriptionMentionsComputedAt(t *testing.T) {
	s := &Server{}
	desc := s.toolAutonomousSafeToDelete().Description
	if !strings.Contains(desc, "computed_at") {
		t.Errorf("tool description does not mention computed_at:\n%s", desc)
	}
	if !strings.Contains(desc, "stale") {
		t.Errorf("tool description does not mention the stale flag:\n%s", desc)
	}
}

func TestMergeMCP_SafeToDeleteReadsRoute(t *testing.T) {
	var path, method, rawQuery string
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		path, method, rawQuery = r.URL.Path, r.Method, r.URL.RawQuery
		_, _ = w.Write([]byte(`{"safe":[],"held":[],"held_dirty":0}`))
	})
	res, err := s.handleAutonomousSafeToDelete(context.Background(), callReq(nil))
	if err != nil || res == nil || res.IsError {
		t.Fatalf("err=%v res=%+v", err, res)
	}
	if method != http.MethodGet || path != "/api/autonomous/safe-to-delete" || rawQuery != "" {
		t.Errorf("proxied %s %s?%s", method, path, rawQuery)
	}
	if text := automationText(t, res); !strings.Contains(text, `"held_dirty":0`) {
		t.Errorf("result lost the daemon body: %s", text)
	}
}

func TestMergeMCP_ReadOnlyNoWriteTool(t *testing.T) {
	s := &Server{}
	for _, tool := range []struct {
		name string
		hint *bool
	}{
		{s.toolAutonomousMerge().Name, s.toolAutonomousMerge().Annotations.ReadOnlyHint},
		{s.toolAutonomousSafeToDelete().Name, s.toolAutonomousSafeToDelete().Annotations.ReadOnlyHint},
	} {
		if tool.hint == nil || !*tool.hint {
			t.Errorf("%s must be marked read-only", tool.name)
		}
	}
	// Only the two reads exist: no tool that merges, deletes or pushes an
	// Automaton branch is registered.
	known := map[string]bool{}
	for _, n := range MergeToolNames() {
		known[n] = true
	}
	// The pipeline tools read, check and keep the daemon's own pipeline
	// record; none of them merges, deletes or pushes a branch.
	for _, n := range MergePipelineToolNames() {
		known[n] = true
	}
	// The shadow status tool only reads shadow plans and comparison counts.
	for _, n := range MergeShadowToolNames() {
		known[n] = true
	}
	for _, n := range mergeTestToolNames(t) {
		if known[n] {
			continue
		}
		if strings.HasPrefix(n, "autonomous_merge") || strings.HasPrefix(n, "autonomous_safe_to_delete") ||
			strings.HasPrefix(n, "autonomous_delete_branch") || strings.HasPrefix(n, "autonomous_prune") {
			t.Errorf("unexpected write-style merge tool %q", n)
		}
	}
	if len(MergeToolNames()) != 2 {
		t.Errorf("want exactly the two read tools, got %v", MergeToolNames())
	}
}

func TestMergeMCP_MergeCarriesLockAndValidator(t *testing.T) {
	body := `{"mode":"on","alert_minutes":30,"automata":[],"shadow_decisions":[],"pipeline_runs":[],` +
		`"merge_lock":{"repo":"/r","run_id":"run-1","automaton_id":"a1","branches":["b"],"since":"2026-11-01T08:00:00Z"},` +
		`"last_validator":{"pass":true,"version":"1.2.3","checked_at":"2026-11-01T08:00:00Z","checks":[]}}`
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(body)) })
	res, err := s.handleAutonomousMerge(context.Background(), callReq(nil))
	if err != nil || res == nil || res.IsError {
		t.Fatalf("err=%v res=%+v", err, res)
	}
	text := automationText(t, res)
	for _, want := range []string{`"merge_lock":{`, `"run_id":"run-1"`, `"last_validator":{`, `"version":"1.2.3"`} {
		if !strings.Contains(text, want) {
			t.Errorf("proxied object lacks %s: %s", want, text)
		}
	}
}

func TestMergeMCP_DescriptionNamesLockAndValidator(t *testing.T) {
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {})
	desc := s.toolAutonomousMerge().Description
	for _, want := range []string{"merge_lock", "last_validator", "absent when empty"} {
		if !strings.Contains(desc, want) {
			t.Errorf("description lacks %q", want)
		}
	}
}
