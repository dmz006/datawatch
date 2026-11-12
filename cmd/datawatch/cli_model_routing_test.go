package main

import (
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

// servedModelsCLISample has a cached node, a fresh node, an unreadable node,
// a disabled node and a model check that refuses.
const servedModelsCLISample = `{
  "cache_seconds": 300, "refusal_mode": "refuse",
  "nodes": [
    {"node": "gpu-1", "kind": "ollama", "models": ["qwen3:27b", "llama3:8b"], "known": true, "cached": true, "age_seconds": 42, "fetched_at": "2026-10-31T04:00:00Z", "error": ""},
    {"node": "gpu-2", "kind": "ollama", "models": ["qwen3:27b"], "known": true, "cached": false, "age_seconds": 0, "fetched_at": "2026-10-31T04:01:00Z", "error": ""},
    {"node": "gpu-3", "kind": "ollama", "models": [], "known": false, "cached": false, "age_seconds": 0, "fetched_at": "", "error": "connection refused"},
    {"node": "gpu-4", "kind": "ollama", "models": [], "known": false, "cached": false, "age_seconds": 0, "fetched_at": "", "error": "", "disabled": true}
  ],
  "check": {
    "model": "mistral:7b", "applicable": true, "refused": true, "node_unknown": false,
    "served_by": [],
    "checked": [{"node": "gpu-1", "state": "does_not_serve", "reason": "mistral:7b is not in its model list"}],
    "message": "no compute node serves mistral:7b"
  }
}`

func TestCLIModelRouting_ServedModelsCallsRESTWithFlags(t *testing.T) {
	d := newLaneFillCLIDaemon(t, http.StatusOK, servedModelsCLISample)

	if _, err := runLaneFillCmd(t, newComputeNodeServedModelsCmd(), nil); err != nil {
		t.Fatal(err)
	}
	if d.path != "/api/compute/served-models" || d.query != "" {
		t.Errorf("no flags: path=%s query=%q", d.path, d.query)
	}

	args := []string{"gpu-1", "--model", "qwen3:27b", "--backend", "pi-prompt", "--refresh"}
	if _, err := runLaneFillCmd(t, newComputeNodeServedModelsCmd(), args); err != nil {
		t.Fatal(err)
	}
	if d.path != "/api/compute/served-models" {
		t.Errorf("path = %s", d.path)
	}
	for _, want := range []string{"node=gpu-1", "model=qwen3%3A27b", "backend=pi-prompt", "refresh=true"} {
		if !strings.Contains(d.query, want) {
			t.Errorf("query %q lacks %q", d.query, want)
		}
	}

	if _, err := runLaneFillCmd(t, newComputeNodeServedModelsCmd(), []string{"a", "b"}); err == nil {
		t.Error("two node names must be refused")
	}

	out, err := runLaneFillCmd(t, newComputeNodeServedModelsCmd(), []string{"--json"})
	if err != nil {
		t.Fatal(err)
	}
	if out != servedModelsCLISample {
		t.Errorf("--json must print the body untouched, got %q", out)
	}
}

func TestCLIModelRouting_ServedModelsTextOutput(t *testing.T) {
	newLaneFillCLIDaemon(t, http.StatusOK, servedModelsCLISample)
	out, err := runLaneFillCmd(t, newComputeNodeServedModelsCmd(), []string{"--model", "mistral:7b"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"gpu-1 (ollama): 2 models, cached, 42s old: qwen3:27b, llama3:8b\n",
		"gpu-2 (ollama): 1 model, fresh: qwen3:27b\n",
		"gpu-3 (ollama): models unknown (connection refused)\n",
		"gpu-4 (ollama): disabled, not read\n",
		"Model check for mistral:7b: refused\n",
		"no compute node serves mistral:7b\n",
		"  gpu-1: does_not_serve (mistral:7b is not in its model list)\n",
	}
	for _, w := range want {
		if !strings.Contains(out, w) {
			t.Errorf("output lacks %q:\n%s", w, out)
		}
	}
	if strings.Contains(out, "PRD") {
		t.Errorf("operator text must say Automaton, never PRD:\n%s", out)
	}

	// Without --model the answer has no check, and none is printed.
	text, err := renderServedModelsText([]byte(`{"nodes": [{"node": "n1", "kind": "ollama", "models": [], "known": true}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if text != "n1 (ollama): 0 models, fresh: none\n" {
		t.Errorf("text = %q", text)
	}
	if empty, _ := renderServedModelsText([]byte(`{"nodes": []}`)); empty != "No compute nodes are registered.\n" {
		t.Errorf("empty = %q", empty)
	}
	if _, err := renderServedModelsText([]byte("not json")); err == nil {
		t.Error("a body that is not JSON must be an error")
	}
}

func TestCLIModelRouting_SetTaskNodeBody(t *testing.T) {
	d := newLaneFillCLIDaemon(t, http.StatusOK, `{"id":"p1"}`)
	_, err := captureEscalationStdout(t, func() error {
		_, err := runLaneFillCmd(t, newAutonomousPRDSetTaskNodeCmd(), []string{"p1", "--task", "t1", "--node", "gpu-2"})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if d.path != "/api/autonomous/prds/p1/set_task_node" {
		t.Errorf("path = %s", d.path)
	}
	var got map[string]string
	if err := json.Unmarshal([]byte(d.body), &got); err != nil {
		t.Fatalf("body %q: %v", d.body, err)
	}
	want := map[string]string{"task_id": "t1", "node": "gpu-2", "actor": "operator"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("body = %v, want %v", got, want)
	}

	// An empty --node clears the pin: the node key is sent, empty.
	if _, err := captureEscalationStdout(t, func() error {
		_, err := runLaneFillCmd(t, newAutonomousPRDSetTaskNodeCmd(), []string{"p1", "--task", "t1", "--node", ""})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	_ = json.Unmarshal([]byte(d.body), &got)
	if n, ok := got["node"]; !ok || n != "" {
		t.Errorf("clear body = %q, want an empty node", d.body)
	}

	// --task is required, and nothing is sent without it.
	calls := d.calls
	if _, err := runLaneFillCmd(t, newAutonomousPRDSetTaskNodeCmd(), []string{"p1", "--node", "gpu-2"}); err == nil {
		t.Error("--task is required")
	}
	if d.calls != calls {
		t.Error("nothing may be sent without --task")
	}
}

func TestCLIModelRouting_SetStoryNodeBody(t *testing.T) {
	d := newLaneFillCLIDaemon(t, http.StatusOK, `{"id":"p1"}`)
	_, err := captureEscalationStdout(t, func() error {
		_, err := runLaneFillCmd(t, newAutonomousPRDSetStoryNodeCmd(), []string{"p1", "--story", "s3", "--node", "gpu-1"})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if d.path != "/api/autonomous/prds/p1/set_story_node" {
		t.Errorf("path = %s", d.path)
	}
	var got map[string]string
	if err := json.Unmarshal([]byte(d.body), &got); err != nil {
		t.Fatalf("body %q: %v", d.body, err)
	}
	want := map[string]string{"story_id": "s3", "node": "gpu-1", "actor": "operator"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("body = %v, want %v", got, want)
	}

	calls := d.calls
	if _, err := runLaneFillCmd(t, newAutonomousPRDSetStoryNodeCmd(), []string{"p1", "--node", "gpu-1"}); err == nil {
		t.Error("--story is required")
	}
	if d.calls != calls {
		t.Error("nothing may be sent without --story")
	}
}

// TestCLIModelRouting_Registered: the three commands are reachable from their
// parents, and the autonomous help lists the two pin commands.
func TestCLIModelRouting_Registered(t *testing.T) {
	auto := newAutonomousCmd()
	for _, name := range []string{"prd-set-task-node", "prd-set-story-node"} {
		found := false
		for _, c := range auto.Commands() {
			found = found || c.Name() == name
		}
		if !found {
			t.Errorf("%s is not registered under autonomous", name)
		}
		if !strings.Contains(auto.Long, name) {
			t.Errorf("the autonomous help does not list %s", name)
		}
	}
	found := false
	for _, c := range newComputeNodeCmd().Commands() {
		found = found || c.Name() == "served-models"
	}
	if !found {
		t.Error("served-models is not registered under compute node")
	}
}

// TestCLIModelRouting_ConfigSetRoundTrip: datawatch config set sends each of
// the six autonomous.model_routing keys to the daemon with the right JSON type.
// config set has no key list of its own, so a key reaches PUT /api/config as
// typed: numbers and booleans as JSON, words as text.
func TestCLIModelRouting_ConfigSetRoundTrip(t *testing.T) {
	cases := []struct {
		key, value string
		want       any
	}{
		{"autonomous.model_routing.refusal_mode", "warn", "warn"},
		{"autonomous.model_routing.served_models_cache_seconds", "0", float64(0)},
		{"autonomous.model_routing.verifier_follows_worker_node", "false", false},
		{"autonomous.model_routing.unknown_node_policy", "assume_not", "assume_not"},
		{"autonomous.model_routing.probe_timeout_seconds", "12", float64(12)},
		{"autonomous.model_routing.alert_on_refusal", "false", false},
	}
	for _, c := range cases {
		d := startInfraRetryConfigDaemon(t)
		out, err := captureEscalationStdout(t, func() error {
			return runConfigSet(&config.Config{}, c.key, c.value)
		})
		if err != nil {
			t.Errorf("%s: %v", c.key, err)
			continue
		}
		if got := d.config[c.key]; !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: daemon stored %#v, want %#v", c.key, got, c.want)
		}
		if !strings.Contains(out, "Set "+c.key+" = "+c.value) {
			t.Errorf("%s: output = %q", c.key, out)
		}
	}
}
