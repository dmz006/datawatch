package router

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

// setLLMFixture serves a fake daemon that records each request, and returns
// a router wired to it.
func setLLMFixture(t *testing.T) (*Router, *[]controlCall) {
	t.Helper()
	var calls []controlCall
	_, port := newFakePeerParent(t, func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		c := controlCall{method: r.Method, path: r.URL.Path}
		if len(raw) > 0 {
			if err := json.Unmarshal(raw, &c.body); err != nil {
				t.Errorf("body %q is not JSON: %v", raw, err)
			}
		}
		calls = append(calls, c)
		_, _ = w.Write([]byte(`{"id":"ab12","status":"running","version":9}`))
	})
	return setupRetryRouter(t, port), &calls
}

func runSetLLMCases(t *testing.T, r *Router, calls *[]controlCall, cases []struct {
	name, in, path string
	want           map[string]any
}) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			*calls = nil
			resp := r.HandleTestMessage(tc.in)
			if len(*calls) != 1 {
				t.Fatalf("%q made %d calls, want 1 (reply %v)", tc.in, len(*calls), resp)
			}
			got := (*calls)[0]
			if got.method != http.MethodPost || got.path != tc.path {
				t.Errorf("request = %s %s, want POST %s", got.method, got.path, tc.path)
			}
			gotJSON, _ := json.Marshal(got.body)
			wantJSON, _ := json.Marshal(tc.want)
			if string(gotJSON) != string(wantJSON) {
				t.Errorf("body = %s, want %s", gotJSON, wantJSON)
			}
			if len(resp) == 0 || !strings.Contains(resp[0], "automation ab12") || !strings.Contains(resp[0], "version 9") {
				t.Errorf("confirmation = %v", resp)
			}
		})
	}
}

func runSetLLMUsage(t *testing.T, r *Router, calls *[]controlCall, inputs []string, usagePrefix string) {
	t.Helper()
	for _, in := range inputs {
		*calls = nil
		resp := r.HandleTestMessage(in)
		if len(*calls) != 0 {
			t.Errorf("%q reached the daemon: %+v", in, *calls)
		}
		if len(resp) == 0 || !strings.HasPrefix(resp[0], usagePrefix) {
			t.Errorf("%q reply = %v, want the usage line %q", in, resp, usagePrefix)
		}
	}
}

func TestAutomationCommSetLLM(t *testing.T) {
	r, calls := setLLMFixture(t)
	const path = "/api/automations/ab12/set_llm"

	runSetLLMCases(t, r, calls, []struct {
		name, in, path string
		want           map[string]any
	}{
		{"all keys", "automation set-llm ab12 backend=pi model=qwen3.8:27b effort=high planning-backend=claude-code planning-model=opus", path,
			map[string]any{"actor": "comm", "backend": "pi", "model": "qwen3.8:27b", "effort": "high",
				"planning_backend": "claude-code", "planning_model": "opus"}},
		{"one key", "automation set-llm ab12 model=sonnet", path,
			map[string]any{"actor": "comm", "model": "sonnet"}},
		{"empty value clears", "automation set-llm ab12 model= planning-model=", path,
			map[string]any{"actor": "comm", "model": "", "planning_model": ""}},
	})

	t.Run("bad input replies with usage", func(t *testing.T) {
		runSetLLMUsage(t, r, calls, []string{
			"automation set-llm", "automation set-llm ab12",
			"automation set-llm ab12 model", "automation set-llm ab12 bogus=x",
			"automation set-llm ab12 model=x extra",
		}, "usage: automation set-llm")
	})
}

func TestAutomationCommSetVerificationLLM(t *testing.T) {
	r, calls := setLLMFixture(t)
	const path = "/api/automations/ab12/set_verification_llm"

	runSetLLMCases(t, r, calls, []struct {
		name, in, path string
		want           map[string]any
	}{
		{"all keys", "automation set-verification-llm ab12 backend=claude-code model=haiku effort=low", path,
			map[string]any{"actor": "comm", "backend": "claude-code", "model": "haiku", "effort": "low"}},
		{"empty value clears", "automation set-verification-llm ab12 model=", path,
			map[string]any{"actor": "comm", "model": ""}},
	})

	t.Run("bad input replies with usage", func(t *testing.T) {
		runSetLLMUsage(t, r, calls, []string{
			"automation set-verification-llm", "automation set-verification-llm ab12",
			"automation set-verification-llm ab12 model", "automation set-verification-llm ab12 bogus=x",
		}, "usage: automation set-verification-llm")
	})

	t.Run("planning keys are refused", func(t *testing.T) {
		runSetLLMUsage(t, r, calls, []string{
			"automation set-verification-llm ab12 planning-backend=pi",
			"automation set-verification-llm ab12 model=x planning-model=y",
		}, "usage: automation set-verification-llm")
	})
}
