package router

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const modelRoutingServedBody = `{"cache_seconds":300,"refusal_mode":"refuse","nodes":[` +
	`{"node":"gpu-1","kind":"ollama","models":["qwen3.8:27b","llama3"],"known":true,"cached":false,"age_seconds":0,"error":""},` +
	`{"node":"gpu-2","kind":"ollama","models":["mistral"],"known":true,"cached":true,"age_seconds":125,"error":""},` +
	`{"node":"gpu-3","kind":"openai-compat","models":[],"known":false,"cached":false,"age_seconds":0,"error":"connection refused"},` +
	`{"node":"gpu-4","kind":"ollama","models":[],"known":false,"cached":false,"age_seconds":0,"error":"","disabled":true}]}`

func TestModelRoutingComm_ServedModelsVerbListsNodes(t *testing.T) {
	log, port := laneFillCommDaemon(t, http.StatusOK, modelRoutingServedBody)
	r := setupPeerRouter(t, port)
	got := strings.Join(r.HandleTestMessage("compute node models"), " ")
	if log.path != "/api/compute/served-models" || log.query != "" {
		t.Errorf("path=%s query=%q, want the served models route with no query", log.path, log.query)
	}
	for _, want := range []string{
		"gpu-1", "2 models, fresh", "qwen3.8:27b, llama3",
		"gpu-2", "1 model, kept copy, 2m old", "mistral",
		"gpu-3", "models unknown: connection refused",
		"gpu-4", "disabled",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("reply lacks %q: %q", want, got)
		}
	}
	if strings.Contains(got, "Check for model") {
		t.Errorf("no model was asked about, so there is no check: %q", got)
	}

	// A node name, backend and refresh are all sent on.
	log, port = laneFillCommDaemon(t, http.StatusOK, modelRoutingServedBody)
	r = setupPeerRouter(t, port)
	r.HandleTestMessage("compute node models gpu-1 backend=ollama refresh")
	if log.query != "backend=ollama&node=gpu-1&refresh=true" {
		t.Errorf("query=%q, want node, backend and refresh sent", log.query)
	}

	// A refused request shows the daemon's text, not a table.
	_, port = laneFillCommDaemon(t, http.StatusNotFound, "unknown compute node nope")
	r = setupPeerRouter(t, port)
	got = strings.Join(r.HandleTestMessage("compute node models nope"), " ")
	if !strings.Contains(got, "failed") || !strings.Contains(got, "unknown compute node nope") {
		t.Errorf("expected the daemon's refusal, got %q", got)
	}

	// Two node names is a usage error and sends nothing.
	log, port = laneFillCommDaemon(t, http.StatusOK, modelRoutingServedBody)
	r = setupPeerRouter(t, port)
	got = strings.Join(r.HandleTestMessage("compute node models a b"), " ")
	if log.calls != 0 || !strings.Contains(got, computeNodeModelsUsage) {
		t.Errorf("calls=%d reply=%q, want the usage and no request", log.calls, got)
	}
}

func TestModelRoutingComm_ServedModelsVerbWithModelShowsRefusal(t *testing.T) {
	body := `{"cache_seconds":300,"refusal_mode":"refuse","nodes":[` +
		`{"node":"gpu-1","kind":"ollama","models":["llama3"],"known":true,"cached":false,"age_seconds":0,"error":""}],` +
		`"check":{"model":"qwen3.8:27b","applicable":true,"refused":true,"node_unknown":false,"served_by":[],` +
		`"message":"node gpu-1 does not serve qwen3.8:27b; it serves llama3"}}`
	log, port := laneFillCommDaemon(t, http.StatusOK, body)
	r := setupPeerRouter(t, port)
	got := strings.Join(r.HandleTestMessage("compute node models gpu-1 model=qwen3.8:27b backend=pi-prompt"), " ")
	if log.query != "backend=pi-prompt&model=qwen3.8%3A27b&node=gpu-1" {
		t.Errorf("query=%q, want node, model and backend sent", log.query)
	}
	for _, want := range []string{
		"Check for model qwen3.8:27b: would be refused",
		"node gpu-1 does not serve qwen3.8:27b; it serves llama3",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("reply lacks %q: %q", want, got)
		}
	}

	// An accepted model names the nodes that serve it.
	ok := strings.Replace(body, `"refused":true`, `"refused":false`, 1)
	ok = strings.Replace(ok, `"served_by":[]`, `"served_by":["gpu-1"]`, 1)
	_, port = laneFillCommDaemon(t, http.StatusOK, ok)
	r = setupPeerRouter(t, port)
	got = strings.Join(r.HandleTestMessage("compute node models model=llama3"), " ")
	if !strings.Contains(got, "Check for model qwen3.8:27b: accepted; served by gpu-1") {
		t.Errorf("expected an accepted check, got %q", got)
	}

	// A backend that does not run on a node says the check does not apply.
	na := strings.Replace(body, `"applicable":true,"refused":true`, `"applicable":false,"refused":false`, 1)
	_, port = laneFillCommDaemon(t, http.StatusOK, na)
	r = setupPeerRouter(t, port)
	got = strings.Join(r.HandleTestMessage("compute node models model=x"), " ")
	if !strings.Contains(got, "does not apply") {
		t.Errorf("expected does not apply, got %q", got)
	}
}

func TestModelRoutingComm_SetTaskNodeVerb(t *testing.T) {
	log, port := laneFillCommDaemon(t, http.StatusOK, `{"id":"p1"}`)
	r := setupPeerRouter(t, port)
	got := strings.Join(r.HandleTestMessage("autonomous set-task-node p1 t1 gpu-1"), " ")
	if log.path != "/api/autonomous/prds/p1/set_task_node" {
		t.Errorf("path=%s, want the task pin route", log.path)
	}
	var sent map[string]string
	if err := json.Unmarshal([]byte(log.body), &sent); err != nil {
		t.Fatalf("body %q: %v", log.body, err)
	}
	if sent["task_id"] != "t1" || sent["node"] != "gpu-1" || sent["actor"] != "operator" {
		t.Errorf("body=%v, want task t1 pinned to gpu-1 by operator", sent)
	}
	if !strings.Contains(got, "Pinned task t1 of Automaton p1 to node gpu-1") || strings.Contains(got, "PRD") {
		t.Errorf("reply=%q", got)
	}

	// clear sends an empty node.
	log, port = laneFillCommDaemon(t, http.StatusOK, `{"id":"p1"}`)
	r = setupPeerRouter(t, port)
	got = strings.Join(r.HandleTestMessage("autonomous set-task-node p1 t1 clear"), " ")
	sent = nil
	_ = json.Unmarshal([]byte(log.body), &sent)
	if n, ok := sent["node"]; !ok || n != "" {
		t.Errorf("body=%q, want an empty node", log.body)
	}
	if !strings.Contains(got, "Cleared the node pin on task t1") {
		t.Errorf("reply=%q", got)
	}

	// A refusal from the daemon is shown, and is not reported as done.
	_, port = laneFillCommDaemon(t, http.StatusBadRequest, "node gpu-1 does not serve the task's model")
	r = setupPeerRouter(t, port)
	got = strings.Join(r.HandleTestMessage("autonomous set-task-node p1 t1 gpu-1"), " ")
	if !strings.Contains(got, "failed") || !strings.Contains(got, "does not serve the task's model") || strings.Contains(got, "Pinned") {
		t.Errorf("reply=%q, want the refusal and no success text", got)
	}

	// Wrong argument count: usage, nothing sent.
	for _, msg := range []string{"autonomous set-task-node p1 t1", "autonomous set-task-node p1 t1 a b"} {
		log, port = laneFillCommDaemon(t, http.StatusOK, `{}`)
		r = setupPeerRouter(t, port)
		got = strings.Join(r.HandleTestMessage(msg), " ")
		if log.calls != 0 || !strings.Contains(got, setTaskNodeUsage) {
			t.Errorf("%q: calls=%d reply=%q, want the usage and no request", msg, log.calls, got)
		}
	}
}

func TestModelRoutingComm_SetStoryNodeVerb(t *testing.T) {
	log, port := laneFillCommDaemon(t, http.StatusOK, `{"id":"p1"}`)
	r := setupPeerRouter(t, port)
	got := strings.Join(r.HandleTestMessage("autonomous set-story-node p1 s1 gpu-2"), " ")
	if log.path != "/api/autonomous/prds/p1/set_story_node" {
		t.Errorf("path=%s, want the story pin route", log.path)
	}
	var sent map[string]string
	if err := json.Unmarshal([]byte(log.body), &sent); err != nil {
		t.Fatalf("body %q: %v", log.body, err)
	}
	if sent["story_id"] != "s1" || sent["node"] != "gpu-2" || sent["actor"] != "operator" {
		t.Errorf("body=%v, want story s1 pinned to gpu-2 by operator", sent)
	}
	if !strings.Contains(got, "Pinned story s1 of Automaton p1 to node gpu-2") {
		t.Errorf("reply=%q", got)
	}

	log, port = laneFillCommDaemon(t, http.StatusOK, `{"id":"p1"}`)
	r = setupPeerRouter(t, port)
	got = strings.Join(r.HandleTestMessage("autonomous set_story_node p1 s1 CLEAR"), " ")
	sent = nil
	_ = json.Unmarshal([]byte(log.body), &sent)
	if n, ok := sent["node"]; !ok || n != "" || !strings.Contains(got, "Cleared the node pin on story s1") {
		t.Errorf("body=%q reply=%q, want an empty node and a cleared reply", log.body, got)
	}

	_, port = laneFillCommDaemon(t, http.StatusBadRequest, "unknown compute node nope")
	r = setupPeerRouter(t, port)
	got = strings.Join(r.HandleTestMessage("autonomous set-story-node p1 s1 nope"), " ")
	if !strings.Contains(got, "failed") || !strings.Contains(got, "unknown compute node nope") {
		t.Errorf("reply=%q, want the refusal", got)
	}

	log, port = laneFillCommDaemon(t, http.StatusOK, `{}`)
	r = setupPeerRouter(t, port)
	got = strings.Join(r.HandleTestMessage("autonomous set-story-node p1"), " ")
	if log.calls != 0 || !strings.Contains(got, setStoryNodeUsage) {
		t.Errorf("calls=%d reply=%q, want the usage and no request", log.calls, got)
	}
}

func TestModelRoutingComm_HelpListsVerbs(t *testing.T) {
	r := &Router{hostname: "h", backend: &captureBackend{name: "test", capture: func(string) {}}}
	r.SetConfigureFunc(func(string, string) error { return nil })

	compute := strings.Join(r.HandleTestMessage("compute help"), " ")
	for _, want := range []string{"compute node models", "served", "refresh"} {
		if !strings.Contains(compute, want) {
			t.Errorf("compute help lacks %q", want)
		}
	}
	auto := strings.Join(r.HandleTestMessage("autonomous"), " ")
	for _, want := range []string{"set-task-node <automaton> <task> <node|clear>", "set-story-node <automaton> <story> <node|clear>"} {
		if !strings.Contains(auto, want) {
			t.Errorf("autonomous help lacks %q", want)
		}
	}
	keys := []string{
		"autonomous.model_routing.refusal_mode", "autonomous.model_routing.served_models_cache_seconds",
		"autonomous.model_routing.verifier_follows_worker_node", "autonomous.model_routing.unknown_node_policy",
		"autonomous.model_routing.probe_timeout_seconds", "autonomous.model_routing.alert_on_refusal",
		"refuse|warn", "assume_serves|assume_not",
	}
	for _, reply := range [][]string{r.HandleTestMessage("configure help"), r.HandleTestMessage("configure list")} {
		text := strings.Join(reply, " ")
		for _, k := range keys {
			if !strings.Contains(text, k) {
				t.Errorf("configure usage lacks %s", k)
			}
		}
	}
}

func TestModelRoutingComm_ConfigureRoundTrip(t *testing.T) {
	var bodies []map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		raw, _ := io.ReadAll(req.Body)
		var body map[string]string
		_ = json.Unmarshal(raw, &body)
		if req.Method != http.MethodPut || req.URL.Path != "/api/config" {
			http.Error(w, "unexpected request", http.StatusNotFound)
			return
		}
		bodies = append(bodies, body)
		if body["autonomous.model_routing.refusal_mode"] == "bogus" {
			http.Error(w, `autonomous.model_routing.refusal_mode: must be refuse or warn`, http.StatusBadRequest)
			return
		}
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	t.Cleanup(srv.Close)

	r := &Router{hostname: "h"}
	r.SetConfigureFunc(func(key, value string) error {
		b, _ := json.Marshal(map[string]string{key: value})
		req, err := http.NewRequest(http.MethodPut, srv.URL+"/api/config", strings.NewReader(string(b)))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close() //nolint:errcheck
		if resp.StatusCode != http.StatusOK {
			msg, _ := io.ReadAll(resp.Body)
			return fmt.Errorf("%s", strings.TrimSpace(string(msg)))
		}
		return nil
	})

	for key, value := range map[string]string{
		"autonomous.model_routing.refusal_mode":                 "warn",
		"autonomous.model_routing.served_models_cache_seconds":  "0",
		"autonomous.model_routing.verifier_follows_worker_node": "false",
		"autonomous.model_routing.unknown_node_policy":          "assume_not",
		"autonomous.model_routing.probe_timeout_seconds":        "12",
		"autonomous.model_routing.alert_on_refusal":             "false",
	} {
		before := len(bodies)
		reply := strings.Join(r.HandleTestMessage("configure "+key+"="+value), " ")
		if !strings.Contains(reply, "Set "+key+" = "+value) {
			t.Errorf("%s: expected a confirmation, got %q", key, reply)
		}
		if len(bodies) != before+1 || bodies[before][key] != value || len(bodies[before]) != 1 {
			t.Errorf("%s: PUT /api/config bodies = %v, want one body {%s: %s}", key, bodies, key, value)
		}
	}

	// The daemon's refusal is shown and the value is not confirmed.
	reply := strings.Join(r.HandleTestMessage("configure autonomous.model_routing.refusal_mode=bogus"), " ")
	if !strings.Contains(reply, "Configure failed") || !strings.Contains(reply, "must be refuse or warn") ||
		strings.Contains(reply, "Set autonomous.model_routing.refusal_mode") {
		t.Errorf("a refused value must be shown and not confirmed: %q", reply)
	}
}
