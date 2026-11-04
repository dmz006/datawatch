// secret_scan_stats — MCP mirror of GET /api/git/secret-scan/stats.

package mcp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

func TestSecretScanStats_ToolDeclaration(t *testing.T) {
	tool := (&Server{}).toolSecretScanStats()
	if tool.Name != "secret_scan_stats" {
		t.Fatalf("name=%q, want secret_scan_stats", tool.Name)
	}
	if props := tool.InputSchema.Properties; len(props) != 0 {
		t.Errorf("tool declares parameters %v, want none", props)
	}
	if tool.Description == "" {
		t.Error("tool has no description")
	}
}

// fakeStatsBackend answers /api/git/secret-scan/stats (or fails it) and
// records the proxied request.
func fakeStatsBackend(t *testing.T, status int, body string) (*httptest.Server, func() (string, string, int)) {
	t.Helper()
	var gotPath, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotAuth = r.URL.Path, r.Header.Get("Authorization")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv, func() (string, string, int) { return gotPath, gotAuth, status }
}

func TestSecretScanStats_ProxiesREST(t *testing.T) {
	const body = `{"mode":"shadow","stats":{"runs":4,"findings":1,"shadow_reports":1,"refusals":2,"last_run_at":"2026-11-01T10:12:05Z","last_refusal_at":"2026-11-01T10:13:00Z"}}`
	srv, _ := fakeStatsBackend(t, http.StatusOK, body)
	s := &Server{webPort: bl427Port(t, srv)}

	res, err := s.handleSecretScanStats(t.Context(), mcpsdk.CallToolRequest{})
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected MCP error: %+v", res)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(toolText(t, res)), &got); err != nil {
		t.Fatalf("response is not JSON: %v", err)
	}
	if got["mode"] != "shadow" {
		t.Errorf("mode=%v, want shadow", got["mode"])
	}
	if _, ok := got["stats"].(map[string]any); !ok {
		t.Errorf("stats missing or not an object: %v", got)
	}
	want := map[string]int{"runs": 4, "findings": 1, "shadow_reports": 1, "refusals": 2}
	stats := got["stats"].(map[string]any)
	for k, v := range want {
		if stats[k].(float64) != float64(v) {
			t.Errorf("stats[%s]=%v, want %d", k, stats[k], v)
		}
	}
}

func TestSecretScanStats_BackendErrorIsToolError(t *testing.T) {
	srv, _ := fakeStatsBackend(t, http.StatusInternalServerError, "boom")
	s := &Server{webPort: bl427Port(t, srv)}

	res, err := s.handleSecretScanStats(t.Context(), mcpsdk.CallToolRequest{})
	if err != nil {
		t.Fatalf("transport error leaked: %v", err)
	}
	if !res.IsError {
		t.Fatal("proxy failure must surface as an MCP error result")
	}
	if !strings.Contains(mcpResultText(t, res), "500") {
		t.Errorf("error result should carry the upstream status: %+v", res)
	}
}

func TestSecretScanStats_NoLoopbackIsToolError(t *testing.T) {
	s := &Server{webPort: 0} // web server disabled
	res, err := s.handleSecretScanStats(t.Context(), mcpsdk.CallToolRequest{})
	if err != nil {
		t.Fatalf("transport error leaked: %v", err)
	}
	if !res.IsError {
		t.Fatal("missing loopback must surface as an MCP error result")
	}
}

// toolText pulls the single text content out of a CallToolResult.
func toolText(t *testing.T, res *mcpsdk.CallToolResult) string {
	t.Helper()
	if len(res.Content) != 1 {
		t.Fatalf("expected exactly one content block, got %d", len(res.Content))
	}
	c, ok := res.Content[0].(mcpsdk.TextContent)
	if !ok {
		t.Fatalf("content block is %T", res.Content[0])
	}
	return c.Text
}

// mcpResultText concatenates every text content block of a result.
func mcpResultText(t *testing.T, res *mcpsdk.CallToolResult) string {
	t.Helper()
	var out strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(mcpsdk.TextContent); ok {
			out.WriteString(tc.Text)
		}
	}
	return out.String()
}
