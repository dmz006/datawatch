// comm-channel LLM verbs must carry plan_tier + max_inflight through
// parseLLMKVPairs into the /api/llms body.

package router

import (
	"encoding/json"
	"io"
	"net/http"
	"testing"
)

func commLLMBody(t *testing.T, msg string) (string, string, map[string]any) {
	t.Helper()
	var method, path string
	var body map[string]any
	_, port := newFakePeerParent(t, func(w http.ResponseWriter, r *http.Request) {
		method = r.Method
		path = r.URL.Path
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		_, _ = w.Write([]byte(`{}`))
	})
	r := setupPeerRouter(t, port)
	r.HandleTestMessage(msg)
	return method, path, body
}

func TestCommLLMUpdate_PlanTierAndMaxInflight(t *testing.T) {
	method, path, body := commLLMBody(t, "llm update cc kind=claude-code plan_tier=max_5x max_inflight=3")
	if method != http.MethodPut || path != "/api/llms/cc" {
		t.Fatalf("route = %s %s, want PUT /api/llms/cc", method, path)
	}
	if tier, _ := body["plan_tier"].(string); tier != "max_5x" {
		t.Errorf("plan_tier = %v (type %T), want max_5x", body["plan_tier"], body["plan_tier"])
	}
	if f, _ := body["max_inflight"].(float64); f != 3 {
		t.Errorf("max_inflight = %v (type %T), want 3 (after JSON round-trip)", body["max_inflight"], body["max_inflight"])
	}
	if name, _ := body["name"].(string); name != "cc" {
		t.Errorf("name = %v, want cc", body["name"])
	}
}

func TestCommLLMAdd_PlanTier(t *testing.T) {
	method, path, body := commLLMBody(t, "llm add fresh kind=ollama plan_tier=pro")
	if method != http.MethodPost || path != "/api/llms" {
		t.Fatalf("route = %s %s, want POST /api/llms", method, path)
	}
	if tier, _ := body["plan_tier"].(string); tier != "pro" {
		t.Errorf("plan_tier = %v, want pro", body["plan_tier"])
	}
}

func TestCommLLM_PlanTierTierNormalizeAndClear(t *testing.T) {
	// Uppercase tiers are lowercased.
	_, _, body := commLLMBody(t, "llm update cc plan_tier=MAX_5X")
	if tier, _ := body["plan_tier"].(string); tier != "max_5x" {
		t.Errorf("plan_tier = %q, want max_5x (lowercased)", tier)
	}

	// "unset" is a no-op in the body (the daemon keeps the previous tier).
	_, _, body = commLLMBody(t, "llm update cc plan_tier=unset")
	if v, present := body["plan_tier"]; present {
		t.Errorf("plan_tier=unset should be dropped from the body, got %v", v)
	}
}
