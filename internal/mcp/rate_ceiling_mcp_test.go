package mcp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

// autonomous_config_set forwards the three rate-ceiling keys to the REST PUT
// body and autonomous_config_get returns what the REST side holds.
func TestRateCeilingMCP_RoundTrip(t *testing.T) {
	stored := map[string]any{}
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			_ = json.NewEncoder(w).Encode(stored)
			return
		}
		b, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(b, &body)
		for k, v := range body {
			stored[k] = v
		}
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	args := map[string]any{"capacity_rate_requests": 60, "capacity_rate_tokens": 2000000, "capacity_rate_window_seconds": 1800}
	if _, err := s.handleAutonomousConfigSet(context.Background(), callReq(args)); err != nil {
		t.Fatal(err)
	}
	for k, want := range map[string]float64{"capacity_rate_requests": 60, "capacity_rate_tokens": 2000000, "capacity_rate_window_seconds": 1800} {
		if stored[k] != want {
			t.Errorf("PUT body %s = %v, want %v", k, stored[k], want)
		}
	}
	res, err := s.handleAutonomousConfigGet(context.Background(), callReq(nil))
	if err != nil {
		t.Fatal(err)
	}
	text := res.Content[0].(mcpsdk.TextContent).Text
	if !strings.Contains(text, `"capacity_rate_requests":60`) {
		t.Fatalf("autonomous_config_get output lacks capacity_rate_requests:60: %s", text)
	}
}

// A zero is forwarded (it clears a ceiling); an absent key is not.
func TestRateCeilingMCP_ForwardsZeroNotAbsent(t *testing.T) {
	var got map[string]any
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		got = nil
		_ = json.Unmarshal(b, &got)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	if _, err := s.handleAutonomousConfigSet(context.Background(), callReq(map[string]any{"capacity_rate_tokens": 0})); err != nil {
		t.Fatal(err)
	}
	if v, ok := got["capacity_rate_tokens"]; !ok || v != float64(0) {
		t.Fatalf("capacity_rate_tokens=0 not forwarded: %v", got)
	}
	if _, ok := got["capacity_rate_requests"]; ok {
		t.Fatalf("capacity_rate_requests present though never given: %v", got)
	}
}
