package mcp

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

func TestAutonomousBranchRefreshStatsTool(t *testing.T) {
	tool := (&Server{}).toolAutonomousBranchRefreshStats()
	if tool.Name != "autonomous_branch_refresh_stats" || !strings.Contains(tool.Description, "/api/autonomous/branch-refresh/stats") {
		t.Fatalf("definition: %q %q", tool.Name, tool.Description)
	}
	if len(tool.InputSchema.Required) != 0 {
		t.Fatalf("required = %v, want none", tool.InputSchema.Required)
	}
	var gotPath, gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotMethod = r.URL.Path, r.Method
		_, _ = w.Write([]byte(`{"enabled":true,"stopped":2}`))
	}))
	defer srv.Close()
	s := &Server{webPort: bl427Port(t, srv)}
	res, err := s.handleAutonomousBranchRefreshStats(t.Context(), mcpsdk.CallToolRequest{})
	if err != nil || res.IsError {
		t.Fatalf("err=%v res=%v", err, res)
	}
	if gotPath != "/api/autonomous/branch-refresh/stats" || gotMethod != http.MethodGet {
		t.Fatalf("proxied %s %s", gotMethod, gotPath)
	}
	if txt := res.Content[0].(mcpsdk.TextContent).Text; !strings.Contains(txt, `"stopped":2`) {
		t.Fatalf("body not passed through: %s", txt)
	}
}
