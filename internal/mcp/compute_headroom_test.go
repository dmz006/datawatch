// Tests for the compute_node_headroom_measure and compute_node_headroom_get
// MCP tools. A fake daemon stands in for the REST node route the tools proxy
// to; it records the method and path and answers the way the server does for
// the success, busy-node and never-measured cases.

package mcp

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/dmz006/datawatch/internal/federation"
	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

type headroomDaemon struct {
	mu     sync.Mutex
	method string
	path   string
	calls  int
}

func (d *headroomDaemon) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	d.mu.Lock()
	d.calls++
	d.method, d.path = r.Method, r.URL.Path
	d.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	switch {
	case strings.HasSuffix(r.URL.Path, "/busy-box/headroom"):
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"error":"measure node busy-box: node busy","node":"busy-box","held":1}`))
	case strings.HasSuffix(r.URL.Path, "/never/headroom"):
		http.Error(w, "no headroom measurement for node never", http.StatusNotFound)
	case r.Method == http.MethodPost:
		_, _ = w.Write([]byte(`{"node":"gpu-1","free_mb":8192,"supported_cap":15,"fresh":true}`))
	default:
		_, _ = w.Write([]byte(`{"node":"gpu-1","age_seconds":30,"fresh":true,"supported_cap":15}`))
	}
}

func newHeadroomMCP(t *testing.T) (*Server, *headroomDaemon) {
	t.Helper()
	d := &headroomDaemon{}
	srv := httptest.NewServer(d)
	t.Cleanup(srv.Close)
	return &Server{webPort: bl427Port(t, srv)}, d
}

func headroomReq(name string) mcpsdk.CallToolRequest {
	req := mcpsdk.CallToolRequest{}
	req.Params.Arguments = map[string]any{"name": name}
	return req
}

func TestComputeNodeHeadroomMeasureMCP_PostsToHeadroomRoute(t *testing.T) {
	s, d := newHeadroomMCP(t)
	res, err := s.handleComputeNodeHeadroomMeasureMCP(t.Context(), headroomReq("gpu-1"))
	if err != nil || res == nil || res.IsError {
		t.Fatalf("measure: res=%v err=%v", res, err)
	}
	if d.method != http.MethodPost || d.path != "/api/compute/nodes/gpu-1/headroom" {
		t.Fatalf("request = %s %s, want POST /api/compute/nodes/gpu-1/headroom", d.method, d.path)
	}
	if !strings.Contains(headroomResultText(res), `"supported_cap":15`) {
		t.Errorf("measure result %q, want the stored view", headroomResultText(res))
	}
}

func TestComputeNodeHeadroomMeasureMCP_BusyNodeRefused(t *testing.T) {
	s, _ := newHeadroomMCP(t)
	res, err := s.handleComputeNodeHeadroomMeasureMCP(t.Context(), headroomReq("busy-box"))
	if err == nil && (res == nil || !res.IsError) {
		t.Fatalf("busy node: expected an error result, got %v", res)
	}
}

func TestComputeNodeHeadroomGetMCP_GetsHeadroomRoute(t *testing.T) {
	s, d := newHeadroomMCP(t)
	res, err := s.handleComputeNodeHeadroomGetMCP(t.Context(), headroomReq("gpu-1"))
	if err != nil || res == nil || res.IsError {
		t.Fatalf("get: res=%v err=%v", res, err)
	}
	if d.method != http.MethodGet || d.path != "/api/compute/nodes/gpu-1/headroom" {
		t.Fatalf("request = %s %s, want GET /api/compute/nodes/gpu-1/headroom", d.method, d.path)
	}
	if !strings.Contains(headroomResultText(res), `"age_seconds":30`) {
		t.Errorf("get result %q, want the age in seconds", headroomResultText(res))
	}
}

func TestComputeNodeHeadroomGetMCP_NoMeasurementYet(t *testing.T) {
	s, _ := newHeadroomMCP(t)
	res, err := s.handleComputeNodeHeadroomGetMCP(t.Context(), headroomReq("never"))
	if err == nil && (res == nil || !res.IsError) {
		t.Fatalf("never measured: expected an error result, got %v", res)
	}
}

// TestComputeNodeHeadroomTools_Registered pins the tool names and the
// federation capability each one needs (read for get, write for measure).
func TestComputeNodeHeadroomTools_Registered(t *testing.T) {
	s := &Server{}
	if got := s.toolComputeNodeHeadroomMeasure().Name; got != "compute_node_headroom_measure" {
		t.Errorf("measure tool name = %q", got)
	}
	if got := s.toolComputeNodeHeadroomGet().Name; got != "compute_node_headroom_get" {
		t.Errorf("get tool name = %q", got)
	}
	if c, ok := federation.RequiredCapForMCPTool("compute_node_headroom_measure"); !ok || c != federation.CapComputeWrite {
		t.Errorf("measure cap = %q ok=%v, want compute write", c, ok)
	}
	if c, ok := federation.RequiredCapForMCPTool("compute_node_headroom_get"); !ok || c != federation.CapComputeRead {
		t.Errorf("get cap = %q ok=%v, want compute read", c, ok)
	}
}

// resultText returns the first text content of a tool result.
func headroomResultText(res *mcpsdk.CallToolResult) string {
	for _, c := range res.Content {
		if tc, ok := c.(mcpsdk.TextContent); ok {
			return tc.Text
		}
	}
	return ""
}
