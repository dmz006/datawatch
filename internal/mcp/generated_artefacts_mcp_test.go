// MCP surface for the generated artefacts: four tools, each a thin proxy of
// one REST route.
package mcp

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

func TestGeneratedArtefacts_MCP_ToolNames(t *testing.T) {
	s := &Server{}
	for want, tool := range map[string]mcpsdk.Tool{
		"generated_artefacts_get":            s.toolGeneratedArtefactsGet(),
		"autonomous_prd_change_record":       s.toolAutonomousPRDChangeRecord(),
		"autonomous_prd_generated_artefact":  s.toolAutonomousPRDGeneratedArtefact(),
		"autonomous_prd_regenerate_artefact": s.toolAutonomousPRDRegenerateArtefact(),
	} {
		if tool.Name != want {
			t.Errorf("tool name = %q, want %q", tool.Name, want)
		}
	}
}

func TestGeneratedArtefacts_MCP_ProxiesRoutes(t *testing.T) {
	cases := []struct {
		name       string
		call       func(s *Server) (*mcpsdk.CallToolResult, error)
		wantMethod string
		wantPath   string
	}{
		{"settings", func(s *Server) (*mcpsdk.CallToolResult, error) {
			return s.handleGeneratedArtefactsGet(context.Background(), callReq(nil))
		}, http.MethodGet, "/api/autonomous/generated-artefacts"},
		{"change record", func(s *Server) (*mcpsdk.CallToolResult, error) {
			return s.handleAutonomousPRDChangeRecord(context.Background(), callReq(map[string]any{"id": "abc"}))
		}, http.MethodGet, "/api/autonomous/prds/abc/change-record"},
		{"preview", func(s *Server) (*mcpsdk.CallToolResult, error) {
			return s.handleAutonomousPRDGeneratedArtefact(context.Background(), callReq(map[string]any{"id": "abc", "kind": "gate_report"}))
		}, http.MethodGet, "/api/autonomous/prds/abc/generated/gate_report"},
		{"regenerate", func(s *Server) (*mcpsdk.CallToolResult, error) {
			return s.handleAutonomousPRDRegenerateArtefact(context.Background(), callReq(map[string]any{"id": "abc", "kind": "parity_draft"}))
		}, http.MethodPost, "/api/autonomous/prds/abc/generated/parity_draft/regenerate"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var method, path string
			s := planTierWebServer(t, func(w http.ResponseWriter, r *http.Request) {
				method, path = r.Method, r.URL.Path
				_, _ = io.Copy(io.Discard, r.Body)
				_, _ = w.Write([]byte(`{"marker":"passed-through"}`))
			})
			res, err := c.call(s)
			if err != nil || res == nil || res.IsError {
				t.Fatalf("result = %+v, err = %v", res, err)
			}
			if method != c.wantMethod || path != c.wantPath {
				t.Fatalf("route = %s %s, want %s %s", method, path, c.wantMethod, c.wantPath)
			}
			if tc, ok := res.Content[0].(mcpsdk.TextContent); !ok || !strings.Contains(tc.Text, "passed-through") {
				t.Errorf("body not passed through: %v", res.Content)
			}
		})
	}
}

func TestGeneratedArtefacts_MCP_MissingArgsNotProxied(t *testing.T) {
	hit := false
	s := planTierWebServer(t, func(w http.ResponseWriter, r *http.Request) { hit = true })
	for _, args := range []map[string]any{nil, {"id": "abc"}, {"kind": "gate_report"}, {"id": " ", "kind": "x"}} {
		for _, h := range []func(context.Context, mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error){
			s.handleAutonomousPRDGeneratedArtefact, s.handleAutonomousPRDRegenerateArtefact,
		} {
			res, err := h(context.Background(), callReq(args))
			if err != nil || res == nil || !res.IsError {
				t.Errorf("args %v: want error result, got %+v (%v)", args, res, err)
			}
		}
	}
	if res, _ := s.handleAutonomousPRDChangeRecord(context.Background(), callReq(nil)); res == nil || !res.IsError {
		t.Error("change record without id should be an error result")
	}
	if hit {
		t.Error("request proxied despite missing arguments")
	}
}
