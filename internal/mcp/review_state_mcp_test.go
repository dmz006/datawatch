// Review state on the MCP surface: autonomous_config_set carries the two new
// judge settings (judge_strong_backend, judge_verdict_cache_enabled) exactly
// when the caller gives them — including an explicit empty backend and an
// explicit false — and autonomous_prd_get documents its review_state.

package mcp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestReviewStateMCPSchemaListsParameters(t *testing.T) {
	s := &Server{}
	var tool struct {
		InputSchema struct {
			Properties map[string]struct {
				Type        string `json:"type"`
				Description string `json:"description"`
			} `json:"properties"`
		} `json:"inputSchema"`
	}
	b, err := json.Marshal(s.toolAutonomousConfigSet())
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &tool); err != nil {
		t.Fatal(err)
	}
	sb, ok := tool.InputSchema.Properties["judge_strong_backend"]
	if !ok {
		t.Fatalf("autonomous_config_set schema lacks judge_strong_backend: %s", b)
	}
	if sb.Type != "string" {
		t.Errorf("judge_strong_backend schema type = %q, want string", sb.Type)
	}
	if !strings.Contains(sb.Description, "last entry of the judge policy") {
		t.Errorf("judge_strong_backend description must name the default; got %q", sb.Description)
	}
	cb, ok := tool.InputSchema.Properties["judge_verdict_cache_enabled"]
	if !ok {
		t.Fatalf("autonomous_config_set schema lacks judge_verdict_cache_enabled: %s", b)
	}
	if cb.Type != "boolean" {
		t.Errorf("judge_verdict_cache_enabled schema type = %q, want boolean", cb.Type)
	}
	if !strings.Contains(cb.Description, "default true") {
		t.Errorf("judge_verdict_cache_enabled description must name the default; got %q", cb.Description)
	}
}

func TestReviewStateMCPPrdGetExposesReviewState(t *testing.T) {
	s := &Server{}
	var tool struct {
		Description string `json:"description"`
	}
	b, err := json.Marshal(s.toolAutonomousPRDGet())
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &tool); err != nil {
		t.Fatal(err)
	}
	for _, word := range []string{"review_state", "timed_out", "blocked"} {
		if !strings.Contains(tool.Description, word) {
			t.Errorf("autonomous_prd_get description must name %q; got %q", word, tool.Description)
		}
	}
}

func reviewStateBody(t *testing.T, args map[string]any) map[string]any {
	t.Helper()
	var got map[string]any
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &got)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	if _, err := s.handleAutonomousConfigSet(context.Background(), callReq(args)); err != nil {
		t.Fatal(err)
	}
	return got
}

func TestReviewStateMCPBodySendsExplicitValues(t *testing.T) {
	got := reviewStateBody(t, map[string]any{"judge_strong_backend": "claude-code", "judge_verdict_cache_enabled": false})
	if got["judge_strong_backend"] != "claude-code" {
		t.Fatalf("judge_strong_backend = %v, want claude-code: %v", got["judge_strong_backend"], got)
	}
	v, ok := got["judge_verdict_cache_enabled"]
	if !ok || v != false {
		t.Fatalf("explicit false must be sent, got %v", got)
	}
}

func TestReviewStateMCPBodySendsExplicitEmptyBackendAndOmitsWhenAbsent(t *testing.T) {
	got := reviewStateBody(t, map[string]any{"judge_strong_backend": ""})
	v, ok := got["judge_strong_backend"]
	if !ok || v != "" {
		t.Fatalf("an explicit empty backend must resolve to the default and still be sent, got %v", got)
	}
	if _, ok := got["judge_verdict_cache_enabled"]; ok {
		t.Fatalf("judge_verdict_cache_enabled present when the caller never gave it: %v", got)
	}

	got = reviewStateBody(t, map[string]any{"enabled": true})
	for _, k := range []string{"judge_strong_backend", "judge_verdict_cache_enabled"} {
		if _, ok := got[k]; ok {
			t.Fatalf("%s present when the caller never gave it: %v", k, got)
		}
	}
}
