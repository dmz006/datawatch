package mcp

import (
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

// headroomDaemon fakes the REST side of the node's headroom endpoints; it
// records the method and path the tools called and answers with the
// configured status and body.
type headroomDaemon struct {
	code   int
	body   string
	method string
	path   string
}

func newHeadroomMCPServer(t *testing.T, code int, body string) (*Server, *headroomDaemon) {
	t.Helper()
	d := &headroomDaemon{code: code, body: body}
	rest := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		d.method, d.path = r.Method, r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(d.code)
		_, _ = w.Write([]byte(d.body))
	}))
	t.Cleanup(rest.Close)
	_, portStr, err := net.SplitHostPort(rest.Listener.Addr().String())
	if err != nil {
		t.Fatalf("split host port: %v", err)
	}
	port, _ := strconv.Atoi(portStr)
	return &Server{webPort: port}, d
}

func TestComputeNodeHeadroomToolSchemasRequireName(t *testing.T) {
	s := &Server{}
	for _, tc := range []struct {
		name string
		tool mcpsdk.Tool
	}{
		{"compute_node_headroom_measure", s.toolComputeNodeHeadroomMeasure()},
		{"compute_node_headroom_get", s.toolComputeNodeHeadroomGet()},
	} {
		if tc.tool.Name != tc.name {
			t.Fatalf("tool name = %q, want %q", tc.tool.Name, tc.name)
		}
		if len(tc.tool.InputSchema.Required) != 1 || tc.tool.InputSchema.Required[0] != "name" {
			t.Fatalf("%s: required = %v, want name only", tc.name, tc.tool.InputSchema.Required)
		}
		prop, ok := tc.tool.InputSchema.Properties["name"].(map[string]any)
		if !ok || prop["type"] != "string" {
			t.Fatalf("%s: name property = %v, want a string", tc.name, tc.tool.InputSchema.Properties["name"])
		}
	}
}

func headroomResultText(t *testing.T, res *mcpsdk.CallToolResult) string {
	t.Helper()
	if res == nil || len(res.Content) == 0 {
		t.Fatalf("no content in result: %v", res)
	}
	tc, ok := res.Content[0].(mcpsdk.TextContent)
	if !ok {
		t.Fatalf("unexpected content type %T", res.Content[0])
	}
	return tc.Text
}

func TestComputeNodeHeadroomMeasureForwardsToREST(t *testing.T) {
	s, d := newHeadroomMCPServer(t, http.StatusOK,
		`{"node":"gpu-a","free_mb":12000,"per_request_mb":1500,"fresh":true,"supported_cap":7}`)
	res, err := s.handleComputeNodeHeadroomMeasureMCP(nil, computeTLSReq(map[string]any{"name": "gpu-a"}))
	if err != nil {
		t.Fatalf("measure tool: %v", err)
	}
	if d.method != http.MethodPost || d.path != "/api/compute/nodes/gpu-a/headroom" {
		t.Fatalf("measure called %s %s, want POST /api/compute/nodes/gpu-a/headroom", d.method, d.path)
	}
	text := headroomResultText(t, res)
	if !strings.Contains(text, `"supported_cap":7`) {
		t.Fatalf("measure result = %q, want the stored measurement JSON", text)
	}
}

func TestComputeNodeHeadroomGetForwardsToREST(t *testing.T) {
	s, d := newHeadroomMCPServer(t, http.StatusOK,
		`{"node":"gpu-a","age_seconds":30,"fresh":true,"supported_cap":7}`)
	res, err := s.handleComputeNodeHeadroomGetMCP(nil, computeTLSReq(map[string]any{"name": "gpu-a"}))
	if err != nil {
		t.Fatalf("get tool: %v", err)
	}
	if d.method != http.MethodGet || d.path != "/api/compute/nodes/gpu-a/headroom" {
		t.Fatalf("get called %s %s, want GET /api/compute/nodes/gpu-a/headroom", d.method, d.path)
	}
	text := headroomResultText(t, res)
	if !strings.Contains(text, `"supported_cap":7`) {
		t.Fatalf("get result = %q, want the measurement JSON", text)
	}
}

func TestComputeNodeHeadroomErrorsSurfaceUnchanged(t *testing.T) {
	s, _ := newHeadroomMCPServer(t, http.StatusConflict, `{"error":"node busy: held=1","node":"gpu-a"}`)
	_, err := s.handleComputeNodeHeadroomMeasureMCP(nil, computeTLSReq(map[string]any{"name": "gpu-a"}))
	if err == nil || !strings.Contains(err.Error(), "409") || !strings.Contains(err.Error(), "node busy") {
		t.Fatalf("busy refusal: err = %v, want it to carry the 409 and the daemon's reason", err)
	}

	s, _ = newHeadroomMCPServer(t, http.StatusNotFound, `no headroom measurement for node gpu-a`)
	_, err = s.handleComputeNodeHeadroomGetMCP(nil, computeTLSReq(map[string]any{"name": "gpu-a"}))
	if err == nil || !strings.Contains(err.Error(), "404") {
		t.Fatalf("unknown node: err = %v, want it to carry the 404", err)
	}
}
