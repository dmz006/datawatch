package mcp

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/federation"
)

func TestModelRoutingTools_ServedModelsProxiesToREST(t *testing.T) {
	var path, method string
	var query map[string][]string
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		path, method, query = r.URL.Path, r.Method, r.URL.Query()
		_, _ = w.Write([]byte(`{"nodes":[{"name":"gpu1","models":["qwen3:27b"]}]}`))
	})
	res, err := s.handleComputeServedModels(context.Background(), callReq(map[string]any{
		"node": "gpu1", "model": "qwen3:27b", "backend": "pi-prompt", "refresh": true,
	}))
	if err != nil || res == nil || res.IsError {
		t.Fatalf("err=%v res=%+v", err, res)
	}
	if method != http.MethodGet || path != "/api/compute/served-models" {
		t.Errorf("proxied %s %s", method, path)
	}
	for k, want := range map[string]string{"node": "gpu1", "model": "qwen3:27b", "backend": "pi-prompt", "refresh": "true"} {
		if got := query[k]; len(got) != 1 || got[0] != want {
			t.Errorf("%s query = %v, want [%s]", k, got, want)
		}
	}
	if text := automationText(t, res); !strings.Contains(text, "qwen3:27b") {
		t.Errorf("result lost the response body: %s", text)
	}

	// No arguments: no query at all, and refresh=false adds nothing.
	var raw string
	s = capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		raw = r.URL.RawQuery
		_, _ = w.Write([]byte(`{}`))
	})
	if _, err := s.handleComputeServedModels(context.Background(), callReq(map[string]any{"refresh": false})); err != nil {
		t.Fatal(err)
	}
	if raw != "" {
		t.Errorf("query sent without arguments: %q", raw)
	}

	// A refusal from the daemon comes back as a tool error with its text.
	s = capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unknown node", http.StatusNotFound)
	})
	res, err = s.handleComputeServedModels(context.Background(), callReq(map[string]any{"node": "nope"}))
	if err != nil || res == nil || !res.IsError {
		t.Fatalf("a 404 must be a tool error: err=%v res=%+v", err, res)
	}
}

func TestModelRoutingTools_SetTaskNodeProxiesToREST(t *testing.T) {
	s, rec := activateServer(t, http.StatusOK, `{"id":"ab12"}`)
	res, err := s.handleAutonomousPRDSetTaskNode(context.Background(), callReq(map[string]any{
		"prd_id": "ab12", "task_id": "t1", "node": "gpu1",
	}))
	if err != nil || res == nil || res.IsError {
		t.Fatalf("err=%v res=%+v", err, res)
	}
	if rec.method != http.MethodPost || rec.path != "/api/autonomous/prds/ab12/set_task_node" {
		t.Fatalf("proxied %s %s", rec.method, rec.path)
	}
	if rec.body["task_id"] != "t1" || rec.body["node"] != "gpu1" {
		t.Errorf("body = %v", rec.body)
	}

	// An empty node clears the pin, so the key is still sent, empty.
	if _, err := s.handleAutonomousPRDSetTaskNode(context.Background(), callReq(map[string]any{
		"prd_id": "ab12", "task_id": "t1",
	})); err != nil {
		t.Fatal(err)
	}
	if v, ok := rec.body["node"]; !ok || v != "" {
		t.Errorf("clearing must send an empty node: %v", rec.body)
	}

	// A refusal is a tool error carrying the daemon's text.
	s, _ = activateServer(t, http.StatusConflict, "node gpu1 does not serve model x")
	res, err = s.handleAutonomousPRDSetTaskNode(context.Background(), callReq(map[string]any{
		"prd_id": "ab12", "task_id": "t1", "node": "gpu1",
	}))
	if err != nil || res == nil || !res.IsError {
		t.Fatalf("a refusal must be a tool error: err=%v res=%+v", err, res)
	}
	if text := automationText(t, res); !strings.Contains(text, "does not serve") {
		t.Errorf("refusal text lost: %s", text)
	}
}

func TestModelRoutingTools_SetStoryNodeProxiesToREST(t *testing.T) {
	s, rec := activateServer(t, http.StatusOK, `{"id":"ab12"}`)
	res, err := s.handleAutonomousPRDSetStoryNode(context.Background(), callReq(map[string]any{
		"prd_id": "ab12", "story_id": "s1", "node": "gpu2",
	}))
	if err != nil || res == nil || res.IsError {
		t.Fatalf("err=%v res=%+v", err, res)
	}
	if rec.method != http.MethodPost || rec.path != "/api/autonomous/prds/ab12/set_story_node" {
		t.Fatalf("proxied %s %s", rec.method, rec.path)
	}
	if rec.body["story_id"] != "s1" || rec.body["node"] != "gpu2" {
		t.Errorf("body = %v", rec.body)
	}
	if _, err := s.handleAutonomousPRDSetStoryNode(context.Background(), callReq(map[string]any{
		"prd_id": "ab12", "story_id": "s1", "node": "",
	})); err != nil {
		t.Fatal(err)
	}
	if v, ok := rec.body["node"]; !ok || v != "" {
		t.Errorf("clearing must send an empty node: %v", rec.body)
	}
}

func TestModelRoutingTools_RegisteredAndCapMapped(t *testing.T) {
	s := New("testhost", nil, &config.MCPConfig{Enabled: true}, t.TempDir(), Options{Version: "test"})
	tools, err := s.ListTools()
	if err != nil {
		t.Fatal(err)
	}
	registered := map[string]bool{}
	for _, tl := range tools {
		registered[tl.Name] = true
	}
	want := map[string]string{
		"compute_served_models":         federation.CapComputeRead,
		"autonomous_prd_set_task_node":  federation.CapAutonomousWrite,
		"autonomous_prd_set_story_node": federation.CapAutonomousWrite,
	}
	for name, capability := range want {
		if !registered[name] {
			t.Errorf("%s is not registered", name)
		}
		got, ok := federation.RequiredCapForMCPTool(name)
		if !ok || got != capability {
			t.Errorf("%s capability = %v (mapped %v), want %v", name, got, ok, capability)
		}
	}
	for name := range registered {
		if _, ok := federation.RequiredCapForMCPTool(name); !ok {
			t.Errorf("registered tool %s has no capability entry", name)
		}
	}
	// Plain words: Automaton, never PRD, in the tool descriptions.
	for _, tl := range tools {
		if _, ok := want[tl.Name]; ok && strings.Contains(tl.Description, "PRD") {
			t.Errorf("%s description must say Automaton, not PRD", tl.Name)
		}
	}
	if ro := (&Server{}).toolComputeServedModels().Annotations.ReadOnlyHint; ro == nil || !*ro {
		t.Error("compute_served_models must be marked read-only")
	}
	for _, p := range []string{"node", "model", "backend", "refresh"} {
		if _, ok := (&Server{}).toolComputeServedModels().InputSchema.Properties[p]; !ok {
			t.Errorf("compute_served_models lacks the %s parameter", p)
		}
	}
	if req := (&Server{}).toolAutonomousPRDSetTaskNode().InputSchema.Required; len(req) != 2 {
		t.Errorf("set_task_node required = %v, want prd_id and task_id", req)
	}
}

// modelRoutingKeyValues are the six autonomous.model_routing keys with a valid
// value each, as the autonomous_config_set arguments carry them.
var modelRoutingKeyValues = map[string]any{
	"refusal_mode":                 "warn",
	"served_models_cache_seconds":  float64(0),
	"verifier_follows_worker_node": false,
	"unknown_node_policy":          "assume_not",
	"probe_timeout_seconds":        float64(12),
	"alert_on_refusal":             false,
}

func TestModelRoutingTools_ConfigKeysRoundTrip(t *testing.T) {
	// autonomous_config_set has an argument for each of the six keys.
	tool := (&Server{}).toolAutonomousConfigSet()
	for k := range modelRoutingKeyValues {
		prop, ok := tool.InputSchema.Properties["model_routing_"+k].(map[string]any)
		if !ok {
			t.Errorf("autonomous_config_set lacks the model_routing_%s argument", k)
			continue
		}
		if d, _ := prop["description"].(string); strings.Contains(d, "PRD") {
			t.Errorf("model_routing_%s description must say Automaton, not PRD: %s", k, d)
		}
	}

	// The arguments go out as dotted keys on PUT /api/config, explicit false and
	// 0 included.
	s, rec := activateServer(t, http.StatusOK, `{}`)
	args := map[string]any{}
	for k, v := range modelRoutingKeyValues {
		args["model_routing_"+k] = v
	}
	res, err := s.handleAutonomousConfigSet(context.Background(), callReq(args))
	if err != nil || res == nil || res.IsError {
		t.Fatalf("err=%v res=%+v", err, res)
	}
	if rec.method != http.MethodPut || rec.path != "/api/config" {
		t.Fatalf("proxied %s %s, want PUT /api/config", rec.method, rec.path)
	}
	if len(rec.body) != len(modelRoutingKeyValues) {
		t.Fatalf("body = %v, want exactly the six keys", rec.body)
	}
	for k, want := range modelRoutingKeyValues {
		if got, ok := rec.body["autonomous.model_routing."+k]; !ok || got != want {
			t.Errorf("autonomous.model_routing.%s = %v (present %v), want %v", k, got, ok, want)
		}
	}

	// config_set passes a single dotted key through unchanged.
	s, rec = activateServer(t, http.StatusOK, `{}`)
	s.cfg = &config.MCPConfig{AllowSelfConfig: config.BoolPtr(true)}
	if res, err := s.handleConfigSet(context.Background(), callReq(map[string]any{
		"key": "autonomous.model_routing.probe_timeout_seconds", "value": "12",
	})); err != nil || res == nil || res.IsError {
		t.Fatalf("config_set: err=%v res=%+v", err, res)
	}
	if len(rec.body) != 1 || rec.body["autonomous.model_routing.probe_timeout_seconds"] != float64(12) {
		t.Errorf("config_set body = %v", rec.body)
	}

	// A refusal from the daemon reaches the caller with its text.
	s, _ = activateServer(t, http.StatusBadRequest, "autonomous.model_routing.refusal_mode must be refuse or warn")
	res, err = s.handleAutonomousConfigSet(context.Background(), callReq(map[string]any{"model_routing_refusal_mode": "maybe"}))
	refusal := ""
	if err != nil {
		refusal = err.Error()
	} else if res != nil && res.IsError {
		refusal = automationText(t, res)
	} else {
		t.Fatalf("a refused value must not pass silently: res=%+v", res)
	}
	if !strings.Contains(refusal, "refuse or warn") {
		t.Errorf("refusal text lost: %s", refusal)
	}

	// The unchanged tools leave other keys out: no argument, no entry.
	if got := modelRoutingConfigArgs(callReq(map[string]any{"enabled": true})); len(got) != 0 {
		t.Errorf("unrelated arguments produced entries: %v", got)
	}
}
