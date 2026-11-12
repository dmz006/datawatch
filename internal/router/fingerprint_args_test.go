// comm-channel surfaces: `federation peer fingerprint <name> on|off` and
// `billing_group=<text>` in the llm add/update kv syntax.

package router

import (
	"encoding/json"
	"io"
	"net/http"
	"testing"
)

func TestRouterFederationPeerFingerprintSync(t *testing.T) {
	t.Run("on opts the peer in", func(t *testing.T) {
		var method, path string
		var body map[string]any
		_, port := newFakePeerParent(t, func(w http.ResponseWriter, r *http.Request) {
			method, path = r.Method, r.URL.Path
			raw, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(raw, &body)
			_, _ = w.Write([]byte(`{}`))
		})
		r := setupPeerRouter(t, port)
		r.HandleTestMessage("federation peer fingerprint peer-a on")
		if method != http.MethodPut || path != "/api/federation/peers/peer-a" {
			t.Fatalf("routed %s %s, want PUT /api/federation/peers/peer-a", method, path)
		}
		if body["fingerprint_sync"] != true {
			t.Errorf("fingerprint_sync = %v, want true", body["fingerprint_sync"])
		}
	})

	t.Run("off opts the peer back out (explicit false)", func(t *testing.T) {
		var method, path string
		var body map[string]any
		_, port := newFakePeerParent(t, func(w http.ResponseWriter, r *http.Request) {
			method, path = r.Method, r.URL.Path
			raw, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(raw, &body)
			_, _ = w.Write([]byte(`{}`))
		})
		r := setupPeerRouter(t, port)
		r.HandleTestMessage("federation peer fingerprint peer-a off")
		if method != http.MethodPut || path != "/api/federation/peers/peer-a" {
			t.Fatalf("routed %s %s, want PUT /api/federation/peers/peer-a", method, path)
		}
		if v, ok := body["fingerprint_sync"]; !ok || v != false {
			t.Errorf("fingerprint_sync = %v (present=%v), want explicit false", v, ok)
		}
	})

	t.Run("anything other than on/off is rejected without a daemon call", func(t *testing.T) {
		calls := 0
		_, port := newFakePeerParent(t, func(http.ResponseWriter, *http.Request) { calls++ })
		r := setupPeerRouter(t, port)
		r.HandleTestMessage("federation peer fingerprint peer-a maybe")
		if calls != 0 {
			t.Errorf("invalid value must not be sent to the daemon, got %d call(s)", calls)
		}
	})
}

func TestRouterLLMBillingGroupKey(t *testing.T) {
	t.Run("add carries billing_group", func(t *testing.T) {
		method, path, body := commLLMBody(t, "llm add shared kind=claude-code billing_group=shared-plan-a")
		if method != http.MethodPost || path != "/api/llms" {
			t.Fatalf("routed %s %s, want POST /api/llms", method, path)
		}
		if body["billing_group"] != "shared-plan-a" {
			t.Errorf("billing_group = %v, want shared-plan-a", body["billing_group"])
		}
	})

	t.Run("update carries billing_group", func(t *testing.T) {
		_, _, body := commLLMBody(t, "llm update shared kind=claude-code billing_group=shared-plan-b")
		if body["billing_group"] != "shared-plan-b" {
			t.Errorf("billing_group = %v, want shared-plan-b", body["billing_group"])
		}
	})

	t.Run("value passes through (kv syntax is space-delimited)", func(t *testing.T) {
		_, _, body := commLLMBody(t, "llm update shared kind=claude-code billing_group=group-x")
		if body["billing_group"] != "group-x" {
			t.Errorf("billing_group = %q, want group-x", body["billing_group"])
		}
	})
}
