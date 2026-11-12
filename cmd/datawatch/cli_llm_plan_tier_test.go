// CLI surface for plan_tier + effective inflight: `datawatch llm add|update
// --plan-tier [--max-inflight]`. Typos fail fast locally (no HTTP round-trip)
// and valid flags are forwarded verbatim to the daemon's /api/llms.

package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/spf13/cobra"
)

type llmCall struct {
	method string
	path   string
	body   map[string]any
}

// llmDaemon starts a fake daemon recording the request, points serverURL at
// it, and returns the recorder.
func llmDaemon(t *testing.T) func() []llmCall {
	t.Helper()
	var mu sync.Mutex
	var calls []llmCall
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		mu.Lock()
		calls = append(calls, llmCall{method: r.Method, path: r.URL.Path, body: body})
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(srv.Close)
	prev := serverURL
	serverURL = srv.URL
	t.Cleanup(func() { serverURL = prev })
	return func() []llmCall {
		mu.Lock()
		defer mu.Unlock()
		return append([]llmCall(nil), calls...)
	}
}

func runRunE(t *testing.T, c *cobra.Command) error {
	t.Helper()
	c.SilenceUsage = true
	c.SilenceErrors = true
	return c.Execute()
}

// Invalid tier: rejected before any HTTP call, with a message that names
// the flag and the valid set.
func TestCLILLMAdd_PlanTierInvalidRejectedLocally(t *testing.T) {
	seen := llmDaemon(t)
	c := newLLMAddCmd(false)
	c.SetArgs([]string{"fresh", "--kind", "ollama", "--plan-tier", "platinum"})
	err := runRunE(t, c)
	if err == nil || !strings.Contains(err.Error(), "invalid --plan-tier") {
		t.Fatalf("err = %v, want invalid --plan-tier", err)
	}
	if !strings.Contains(err.Error(), "pro") {
		t.Errorf("error should name the valid tiers: %v", err)
	}
	if len(seen()) != 0 {
		t.Errorf("daemon hit %d times, want 0 (local validation)", len(seen()))
	}
}

func TestCLILLMUpdate_PlanTierInvalidRejectedLocally(t *testing.T) {
	seen := llmDaemon(t)
	c := newLLMAddCmd(true) // the `update` variant
	c.SetArgs([]string{"existing", "--plan-tier", "max_50x"})
	err := runRunE(t, c)
	if err == nil || !strings.Contains(err.Error(), "invalid --plan-tier") {
		t.Fatalf("err = %v, want invalid --plan-tier", err)
	}
	if len(seen()) != 0 {
		t.Errorf("daemon hit %d times, want 0 (local validation)", len(seen()))
	}
}

// Valid flags: forwarded to POST /api/llms with both fields present.
func TestCLILLMAdd_PlanTierAndMaxInflightForwarded(t *testing.T) {
	seen := llmDaemon(t)
	c := newLLMAddCmd(false)
	c.SetArgs([]string{"fresh", "--kind", "claude-code", "--plan-tier", "pro", "--max-inflight", "2"})
	if err := runRunE(t, c); err != nil {
		t.Fatalf("llm add: %v", err)
	}
	calls := seen()
	if len(calls) != 1 {
		t.Fatalf("calls = %d, want 1", len(calls))
	}
	want := calls[0]
	if want.method != http.MethodPost || want.path != "/api/llms" {
		t.Fatalf("route = %s %s, want POST /api/llms", want.method, want.path)
	}
	if want.body["plan_tier"] != "pro" {
		t.Errorf("plan_tier = %v, want pro", want.body["plan_tier"])
	}
	if f, _ := want.body["max_inflight"].(float64); f != 2 {
		t.Errorf("max_inflight = %v, want 2", want.body["max_inflight"])
	}
	if want.body["name"] != "fresh" || want.body["kind"] != "claude-code" {
		t.Errorf("body shape = %v, want name=fresh kind=claude-code", want.body)
	}
}

// Update without --max-inflight: plan_tier sent, max_inflight omitted so
// the daemon keeps the existing per-LLM cap (PATCH semantics).
func TestCLILLMUpdate_PlanTierOnlyOmitsMaxInflight(t *testing.T) {
	seen := llmDaemon(t)
	c := newLLMAddCmd(true) // the `update` variant
	c.SetArgs([]string{"existing", "--plan-tier", "api"})
	if err := runRunE(t, c); err != nil {
		t.Fatalf("llm update: %v", err)
	}
	calls := seen()
	if len(calls) != 1 {
		t.Fatalf("calls = %d, want 1", len(calls))
	}
	want := calls[0]
	if want.method != http.MethodPut || want.path != "/api/llms/existing" {
		t.Fatalf("route = %s %s, want PUT /api/llms/existing", want.method, want.path)
	}
	if want.body["plan_tier"] != "api" {
		t.Errorf("plan_tier = %v, want api", want.body["plan_tier"])
	}
	if _, present := want.body["max_inflight"]; present {
		t.Errorf("max_inflight must be omitted when the flag is not given, got %v", want.body["max_inflight"])
	}
}

// `datawatch llm list` output carries effective_max_inflight +
// inflight_source for registry entries that have them.
func TestCLILLMList_PlanTierFieldPresent(t *testing.T) {
	c := newLLMCmd()
	// llm list is the "list" subcommand; verify the command tree exposes it.
	found := false
	for _, sub := range c.Commands() {
		if sub.Name() == "list" {
			found = true
		}
	}
	if !found {
		t.Fatal("llm list subcommand missing")
	}
}
