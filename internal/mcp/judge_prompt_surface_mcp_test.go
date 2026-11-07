package mcp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

// judge_prompt_file_threshold_lines and
// judge_delivery_wait_per_thousand_lines_seconds on the autonomous config MCP
// tools: get returns both values (defaults included), set changes them, and an
// out-of-range or non-whole value is refused before anything is written.

func TestJudgePromptSurface_MCPSchemaListsParameters(t *testing.T) {
	s := &Server{}
	var tool struct {
		InputSchema struct {
			Properties map[string]struct {
				Type string `json:"type"`
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
	for _, k := range judgePromptKeys {
		p, ok := tool.InputSchema.Properties[k.key]
		if !ok || p.Type != "number" {
			t.Errorf("autonomous_config_set schema: %s = %+v, want a number", k.key, p)
		}
	}
}

func TestJudgePromptSurface_MCPGetReturnsBothWithDefaults(t *testing.T) {
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/autonomous/config":
			_, _ = w.Write([]byte(`{"enabled":true,"judge_size_threshold_lines":400}`))
		case "/api/config":
			_, _ = w.Write([]byte(`{"autonomous":{"judge_prompt_file_threshold_lines":300,"judge_delivery_wait_per_thousand_lines_seconds":60}}`))
		default:
			http.NotFound(w, r)
		}
	})
	res, err := s.handleAutonomousConfigGet(context.Background(), callReq(nil))
	if err != nil || res.IsError {
		t.Fatalf("get: err=%v res=%+v", err, res)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(resContentText(t, res)), &got); err != nil {
		t.Fatal(err)
	}
	if got["judge_prompt_file_threshold_lines"] != float64(300) {
		t.Errorf("judge_prompt_file_threshold_lines = %v, want 300", got["judge_prompt_file_threshold_lines"])
	}
	if got["judge_delivery_wait_per_thousand_lines_seconds"] != float64(60) {
		t.Errorf("judge_delivery_wait_per_thousand_lines_seconds = %v, want 60", got["judge_delivery_wait_per_thousand_lines_seconds"])
	}
	if got["judge_size_threshold_lines"] != float64(400) || got["enabled"] != true {
		t.Errorf("existing keys lost in merge: %v", got)
	}
}

func TestJudgePromptSurface_MCPSetSendsValuesIncludingZero(t *testing.T) {
	for _, val := range []float64{0, 123} {
		var path, method string
		var got map[string]any
		s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
			path, method = r.URL.Path, r.Method
			b, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(b, &got)
			_, _ = w.Write([]byte(`{"status":"ok"}`))
		})
		res, err := s.handleAutonomousConfigSet(context.Background(), callReq(map[string]any{
			"judge_prompt_file_threshold_lines":              val,
			"judge_delivery_wait_per_thousand_lines_seconds": val,
		}))
		if err != nil || res.IsError {
			t.Fatalf("set %v: err=%v res=%+v", val, err, res)
		}
		if path != "/api/config" || method != http.MethodPut {
			t.Errorf("set went to %s %s, want PUT /api/config", method, path)
		}
		for _, k := range judgePromptKeys {
			if v, ok := got["autonomous."+k.key]; !ok || v != val {
				t.Errorf("autonomous.%s = %v (present=%v), want %v", k.key, v, ok, val)
			}
		}
	}
}

func TestJudgePromptSurface_MCPSetOmitsWhenAbsent(t *testing.T) {
	calls := 0
	var got map[string]any
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &got)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	if _, err := s.handleAutonomousConfigSet(context.Background(), callReq(map[string]any{"enabled": true})); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Errorf("calls = %d, want 1 (autonomous config only)", calls)
	}
	for _, k := range judgePromptKeys {
		if _, ok := got[k.key]; ok {
			t.Errorf("%s sent though the caller never gave it: %v", k.key, got)
		}
	}
}

func TestJudgePromptSurface_MCPSetRejectsOutOfRange(t *testing.T) {
	for _, k := range judgePromptKeys {
		for _, bad := range []any{float64(-1), float64(k.max + 1), 2.5, "10", true} {
			calls := 0
			s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
				calls++
				_, _ = w.Write([]byte(`{"status":"ok"}`))
			})
			res, err := s.handleAutonomousConfigSet(context.Background(), callReq(map[string]any{
				"enabled": true,
				k.key:     bad,
			}))
			if err != nil || res == nil || !res.IsError {
				t.Errorf("%s=%v: want an error result, got err=%v res=%+v", k.key, bad, err, res)
				continue
			}
			if !strings.Contains(resContentText(t, res), k.key) {
				t.Errorf("%s=%v: error does not name the key: %s", k.key, bad, resContentText(t, res))
			}
			if calls != 0 {
				t.Errorf("%s=%v: %d REST calls made for a rejected value", k.key, bad, calls)
			}
		}
	}
}
