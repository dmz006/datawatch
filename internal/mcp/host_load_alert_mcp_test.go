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

func hostLoadMCPServer(t *testing.T, stored map[string]any, bodies *[]map[string]any) *Server {
	t.Helper()
	return capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			_ = json.NewEncoder(w).Encode(stored)
			return
		}
		b, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(b, &body)
		*bodies = append(*bodies, body)
		for k, v := range body {
			stored[k] = v
		}
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
}

func TestHostLoadAlert_MCPConfigSetRoundTrip(t *testing.T) {
	stored := map[string]any{}
	var bodies []map[string]any
	s := hostLoadMCPServer(t, stored, &bodies)

	_, err := s.handleAutonomousConfigSet(context.Background(), callReq(map[string]any{
		"host_load_alert_mode":     "reap",
		"host_load_alert_multiple": 2.5,
		"host_load_alert_minutes":  15,
	}))
	if err != nil {
		t.Fatal(err)
	}
	hla, _ := stored["host_load_alert"].(map[string]any)
	if hla["mode"] != "reap" || hla["multiple"] != 2.5 || hla["minutes"] != float64(15) {
		t.Fatalf("PUT body host_load_alert = %v", hla)
	}
	res, err := s.handleAutonomousConfigGet(context.Background(), callReq(nil))
	if err != nil {
		t.Fatal(err)
	}
	if text := res.Content[0].(mcpsdk.TextContent).Text; !strings.Contains(text, `"host_load_alert"`) {
		t.Fatalf("config_get lacks host_load_alert: %s", text)
	}
}

func TestHostLoadAlert_MCPConfigSetSendsOnlyGivenFields(t *testing.T) {
	stored := map[string]any{}
	var bodies []map[string]any
	s := hostLoadMCPServer(t, stored, &bodies)
	if _, err := s.handleAutonomousConfigSet(context.Background(), callReq(map[string]any{"host_load_alert_mode": "off"})); err != nil {
		t.Fatal(err)
	}
	hla, _ := bodies[0]["host_load_alert"].(map[string]any)
	if len(hla) != 1 || hla["mode"] != "off" {
		t.Fatalf("body host_load_alert = %v, want only mode=off", hla)
	}
	// An explicit 0 (use the default) is forwarded, not dropped.
	if _, err := s.handleAutonomousConfigSet(context.Background(), callReq(map[string]any{"host_load_alert_minutes": 0})); err != nil {
		t.Fatal(err)
	}
	hla, _ = bodies[1]["host_load_alert"].(map[string]any)
	if v, ok := hla["minutes"]; !ok || v != float64(0) {
		t.Fatalf("minutes=0 not forwarded: %v", hla)
	}
}

func TestHostLoadAlert_MCPConfigSetOmitsObjectWhenUnset(t *testing.T) {
	stored := map[string]any{}
	var bodies []map[string]any
	s := hostLoadMCPServer(t, stored, &bodies)
	if _, err := s.handleAutonomousConfigSet(context.Background(), callReq(map[string]any{"max_engines_per_node": 2})); err != nil {
		t.Fatal(err)
	}
	if _, ok := bodies[0]["host_load_alert"]; ok {
		t.Fatalf("host_load_alert sent without any argument: %v", bodies[0])
	}
}
