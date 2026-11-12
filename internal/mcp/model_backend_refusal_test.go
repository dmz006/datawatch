// MCP side of the model/backend refusal: when the REST setter refuses a
// pair, the tool returns the refusal text as a tool error, not a success.

package mcp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

func TestModelBackendRefusalMCPReturnsToolError(t *testing.T) {
	const refusal = `model does not pair with backend: model "claude-haiku-5-5" is a bare Claude alias`
	rest := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/autonomous/prds/prd-ref-1/set_task_llm" || r.Method != http.MethodPost {
			http.Error(w, "unexpected", http.StatusNotFound)
			return
		}
		http.Error(w, refusal, http.StatusBadRequest)
	}))
	defer rest.Close()
	port, _ := strconv.Atoi(strings.Split(rest.URL, ":")[2])
	s := &Server{webPort: port}

	req := mcpsdk.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"id":      "prd-ref-1",
		"task_id": "t1",
		"backend": "pi",
		"model":   "claude-haiku-5-5",
	}
	res, err := s.handleAutonomousPRDSetTaskLLM(context.Background(), req)
	if err != nil {
		t.Fatalf("handler returned a protocol error %v; want a tool error result", err)
	}
	if res == nil || !res.IsError {
		t.Fatalf("refused call not reported as a tool error: %+v", res)
	}
	text := res.Content[0].(mcpsdk.TextContent).Text
	if !strings.Contains(text, "does not pair with backend") {
		t.Fatalf("tool error text %q does not carry the refusal", text)
	}
}
