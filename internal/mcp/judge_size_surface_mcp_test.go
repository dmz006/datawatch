package mcp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

// judge_size_threshold_lines — the autonomous_config_set tool schema lists the
// parameter and the request body carries it whenever the caller gives it,
// including an explicit 0 (size-aware routing off).

func TestJudgeSizeSurface_MCPSchemaListsParameter(t *testing.T) {
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
	p, ok := tool.InputSchema.Properties["judge_size_threshold_lines"]
	if !ok {
		t.Fatalf("autonomous_config_set schema lacks judge_size_threshold_lines: %s", b)
	}
	if p.Type != "number" {
		t.Errorf("judge_size_threshold_lines schema type = %q, want number", p.Type)
	}
	for _, word := range []string{"0 turns size-aware routing off", "400"} {
		if !strings.Contains(p.Description, word) {
			t.Errorf("judge_size_threshold_lines description must contain %q; got %q", word, p.Description)
		}
	}
}

func judgeSizeBody(t *testing.T, args map[string]any) map[string]any {
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

func TestJudgeSizeSurface_MCPBodyIncludesExplicitZero(t *testing.T) {
	got := judgeSizeBody(t, map[string]any{"judge_size_threshold_lines": 0})
	v, ok := got["judge_size_threshold_lines"]
	if !ok {
		t.Fatalf("explicit 0 not sent: %v", got)
	}
	if v != float64(0) {
		t.Fatalf("judge_size_threshold_lines = %v, want 0", v)
	}
}

func TestJudgeSizeSurface_MCPBodySendsValueAndOmitsWhenAbsent(t *testing.T) {
	got := judgeSizeBody(t, map[string]any{"judge_size_threshold_lines": 250})
	if got["judge_size_threshold_lines"] != float64(250) {
		t.Fatalf("judge_size_threshold_lines = %v, want 250", got["judge_size_threshold_lines"])
	}
	got = judgeSizeBody(t, map[string]any{"enabled": true})
	if _, ok := got["judge_size_threshold_lines"]; ok {
		t.Fatalf("judge_size_threshold_lines present when the caller never gave it: %v", got)
	}
}
