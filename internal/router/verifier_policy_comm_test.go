// comm `autonomous verifier-policy`, `verifier-policy-set`, `set-verifier-policy`
// and `set-task-verifier-policy` parsing and requests.

package router

import (
	"encoding/json"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

// verifierPolicyRequest is one request the fake daemon received.
type verifierPolicyRequest struct {
	method, path string
	body         map[string]any
}

// commVerifierPolicyCall sends msg to a router backed by a fake daemon that
// answers reply, and returns the request the daemon received and the router's
// replies.
func commVerifierPolicyCall(t *testing.T, msg, reply string) (*verifierPolicyRequest, string) {
	t.Helper()
	got := &verifierPolicyRequest{}
	_, port := newFakePeerParent(t, func(w http.ResponseWriter, r *http.Request) {
		got.method, got.path = r.Method, r.URL.Path
		raw, _ := io.ReadAll(r.Body)
		got.body = nil
		_ = json.Unmarshal(raw, &got.body)
		_, _ = w.Write([]byte(reply))
	})
	r := setupPeerRouter(t, port)
	return got, strings.Join(r.HandleTestMessage(msg), " ")
}

func TestVerifierPolicyCommShowsGlobalPolicy(t *testing.T) {
	got, out := commVerifierPolicyCall(t, "autonomous verifier-policy",
		`{"enabled":true,"verifier_policy":{"mode":"balance","backends":["qwen"]}}`)
	if got.method != http.MethodGet || got.path != "/api/autonomous/config" {
		t.Fatalf("request = %s %s", got.method, got.path)
	}
	if !strings.Contains(out, "balance") || strings.Contains(out, "enabled") {
		t.Errorf("reply should be the verifier_policy object only: %q", out)
	}
}

func TestVerifierPolicyCommSetsGlobalPolicy(t *testing.T) {
	got, _ := commVerifierPolicyCall(t,
		"autonomous verifier-policy-set mode=prefer-free backends=qwen,claude-code claude_session_cap=6 "+
			"claude_cap_window_minutes=0 force_stronger_on_retry=true final_gate_stronger=false "+
			"stronger_backend=claude-code stronger_model=",
		`{}`)
	if got.method != http.MethodPut || got.path != "/api/autonomous/config" {
		t.Fatalf("request = %s %s", got.method, got.path)
	}
	want := map[string]any{"verifier_policy": map[string]any{
		"mode":                      "prefer-free",
		"backends":                  []any{"qwen", "claude-code"},
		"claude_session_cap":        float64(6),
		"claude_cap_window_minutes": float64(0),
		"force_stronger_on_retry":   true,
		"final_gate_stronger":       false,
		"stronger_backend":          "claude-code",
		"stronger_model":            "",
	}}
	if !reflect.DeepEqual(got.body, want) {
		t.Errorf("body = %#v, want %#v", got.body, want)
	}
}

func TestVerifierPolicyCommEmptyBackendsClears(t *testing.T) {
	got, _ := commVerifierPolicyCall(t, "autonomous verifier-policy-set backends=", `{}`)
	want := map[string]any{"verifier_policy": map[string]any{"backends": []any{}}}
	if !reflect.DeepEqual(got.body, want) {
		t.Errorf("body = %#v, want %#v", got.body, want)
	}
}

func TestVerifierPolicyCommSetsPRDOverride(t *testing.T) {
	got, _ := commVerifierPolicyCall(t, "autonomous set-verifier-policy p1 mode=gpu-only", `{}`)
	if got.path != "/api/autonomous/prds/p1/set_verifier_policy" || got.method != http.MethodPost {
		t.Fatalf("request = %s %s", got.method, got.path)
	}
	want := map[string]any{"mode": "gpu-only", "actor": "operator"}
	if !reflect.DeepEqual(got.body, want) {
		t.Errorf("body = %#v, want %#v", got.body, want)
	}
}

func TestVerifierPolicyCommSetsTaskOverride_Underscore(t *testing.T) {
	got, _ := commVerifierPolicyCall(t, "autonomous set_task_verifier_policy p1 t1 mode= backends=qwen,claude-code", `{}`)
	if got.path != "/api/autonomous/prds/p1/set_task_verifier_policy" {
		t.Fatalf("path = %s", got.path)
	}
	want := map[string]any{"task_id": "t1", "mode": "", "backends": []any{"qwen", "claude-code"}, "actor": "operator"}
	if !reflect.DeepEqual(got.body, want) {
		t.Errorf("body = %#v, want %#v", got.body, want)
	}
}

func TestVerifierPolicyCommUsage(t *testing.T) {
	r := &Router{}
	cases := map[string]string{
		"autonomous verifier-policy-set":                       "usage: autonomous verifier-policy-set",
		"autonomous verifier-policy-set bogus=1":               "usage: autonomous verifier-policy-set",
		"autonomous verifier-policy-set claude_session_cap=-2": "usage: autonomous verifier-policy-set",
		"autonomous verifier-policy-set final_gate_stronger=x": "usage: autonomous verifier-policy-set",
		"autonomous verifier-policy extra":                     "usage: autonomous verifier-policy",
		"autonomous set-verifier-policy":                       "usage: autonomous set-verifier-policy",
		"autonomous set-verifier-policy p1":                    "usage: autonomous set-verifier-policy",
		"autonomous set-verifier-policy p1 bogus=1":            "usage: autonomous set-verifier-policy",
		"autonomous set-task-verifier-policy p1 t1":            "usage: autonomous set-task-verifier-policy",
		"autonomous set-task-verifier-policy p1":               "usage: autonomous set-task-verifier-policy",
	}
	for msg, want := range cases {
		if got := strings.Join(r.HandleTestMessage(msg), " "); !strings.Contains(got, want) {
			t.Errorf("%q: expected %q, got %q", msg, want, got)
		}
	}
}

func TestVerifierPolicyCommHelpNamesEveryKey(t *testing.T) {
	keys := []string{"mode", "backends", "claude_session_cap", "claude_cap_window_minutes",
		"force_stronger_on_retry", "final_gate_stronger", "stronger_backend", "stronger_model"}
	help := strings.Join((&Router{}).HandleTestMessage("autonomous"), " ")
	for _, verb := range []string{"verifier-policy", "verifier-policy-set", "set-verifier-policy", "set-task-verifier-policy"} {
		if !strings.Contains(help, verb) {
			t.Errorf("autonomous help lacks %s", verb)
		}
	}
	for _, k := range keys {
		if _, ok := verifierPolicySetKeys[k]; !ok {
			t.Errorf("verifier-policy-set does not accept %s", k)
		}
		if !strings.Contains(verifierPolicySetUsage, k) {
			t.Errorf("verifier-policy-set usage lacks %s", k)
		}
	}
}
