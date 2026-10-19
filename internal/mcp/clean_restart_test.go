package mcp

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

// TestCleanRestartReachesRESTAndMCP checks that a clean_restart tool argument
// is forwarded in the reset_task REST body the manager receives.
func TestCleanRestartReachesRESTAndMCP(t *testing.T) {
	var got map[string]any
	rest := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &got)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer rest.Close()

	_, portStr, err := net.SplitHostPort(rest.Listener.Addr().String())
	if err != nil {
		t.Fatalf("split host port: %v", err)
	}
	port, _ := strconv.Atoi(portStr)
	s := &Server{webPort: port}

	req := mcpsdk.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"id":            "prd-1",
		"task_id":       "task-1",
		"clean_restart": true,
	}
	if _, err := s.handleAutonomousPRDResetTask(context.Background(), req); err != nil {
		t.Fatalf("handler: %v", err)
	}
	if got["clean_restart"] != true {
		t.Fatalf("REST body clean_restart = %v; want true (body=%v)", got["clean_restart"], got)
	}
}
