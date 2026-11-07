package mcp

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

func TestAutonomousStatusToolDescribesMemoryCeiling(t *testing.T) {
	s := &Server{}
	d := s.toolAutonomousStatus().Description
	for _, want := range []string{"memory_ceiling", "effective_mb", "source", "enforcement_path", "watchdog", "resource_limits.memory_cap_mb"} {
		if !strings.Contains(d, want) {
			t.Errorf("autonomous_status description lacks %q", want)
		}
	}
}

// The handler passes the REST status through, so the memory_ceiling values the
// daemon reports reach the MCP client unchanged.
func TestAutonomousStatusCarriesMemoryCeilingValues(t *testing.T) {
	rest := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"running":true,"memory_ceiling":{"effective_mb":2048,"source":"default","enforcement_path":"scope","share_percent":25,"floor_mb":1024,"cap_mb":8192}}`))
	}))
	defer rest.Close()
	_, portStr, _ := net.SplitHostPort(rest.Listener.Addr().String())
	port, _ := strconv.Atoi(portStr)
	s := &Server{webPort: port}
	res, err := s.handleAutonomousStatus(context.Background(), mcpsdk.CallToolRequest{})
	if err != nil {
		t.Fatal(err)
	}
	var out struct {
		MemoryCeiling struct {
			EffectiveMB int    `json:"effective_mb"`
			Source      string `json:"source"`
			Path        string `json:"enforcement_path"`
		} `json:"memory_ceiling"`
	}
	text := res.Content[0].(mcpsdk.TextContent).Text
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatal(err)
	}
	if out.MemoryCeiling.EffectiveMB != 2048 || out.MemoryCeiling.Source != "default" || out.MemoryCeiling.Path != "scope" {
		t.Fatalf("memory_ceiling = %+v", out.MemoryCeiling)
	}
}
