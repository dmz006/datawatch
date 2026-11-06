package mcp

import (
	"context"
	"net/http"
	"strings"
	"testing"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"

	"github.com/dmz006/datawatch/internal/federation"
)

func TestDeployMCP_ToolsRegistered(t *testing.T) {
	have := map[string]bool{}
	for _, n := range mergeTestToolNames(t) {
		have[n] = true
	}
	for _, want := range DeployToolNames() {
		if !have[want] {
			t.Errorf("tool %q is not registered", want)
		}
	}
}

func TestDeployMCP_SchemaParameters(t *testing.T) {
	s := &Server{}
	for _, tool := range []struct {
		name  string
		props map[string]any
	}{
		{"get", s.toolAutonomousDeployGet().InputSchema.Properties},
		{"run", s.toolAutonomousDeployRun().InputSchema.Properties},
	} {
		// Neither tool takes arguments; the schema names none.
		if len(tool.props) != 0 {
			t.Errorf("%s: unexpected parameters %v", tool.name, tool.props)
		}
	}
	if s.toolAutonomousDeployGet().Name != "autonomous_deploy_get" || s.toolAutonomousDeployRun().Name != "autonomous_deploy_run" {
		t.Error("tool names wrong")
	}
	if h := s.toolAutonomousDeployGet().Annotations.ReadOnlyHint; h == nil || !*h {
		t.Error("get must be read-only")
	}
	if h := s.toolAutonomousDeployRun().Annotations.ReadOnlyHint; h != nil && *h {
		t.Error("run must not be read-only")
	}
}

func TestDeployMCP_Capabilities(t *testing.T) {
	want := map[string]string{
		"autonomous_deploy_get": federation.CapAutonomousRead,
		"autonomous_deploy_run": federation.CapAutonomousWrite,
	}
	for tool, c := range want {
		if got, ok := federation.RequiredCapForMCPTool(tool); !ok || got != c {
			t.Errorf("%s: cap %q (known %v), want %q", tool, got, ok, c)
		}
	}
}

func TestDeployMCP_ProxiesBothRoutes(t *testing.T) {
	var method, path string
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		method, path = r.Method, r.URL.Path
		_, _ = w.Write([]byte(`{"phase":"idle"}`))
	})
	res, _ := s.handleAutonomousDeployGet(context.Background(), callReq(nil))
	if res == nil || res.IsError || method != http.MethodGet || path != deployPath {
		t.Errorf("get: res=%+v %s %s", res, method, path)
	}
	res, _ = s.handleAutonomousDeployRun(context.Background(), callReq(nil))
	if res == nil || res.IsError || method != http.MethodPost || path != deployRunPath {
		t.Errorf("run: res=%+v %s %s", res, method, path)
	}
}

func TestDeployMCP_RESTErrorIsMCPError(t *testing.T) {
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "a merge or deploy is already running", http.StatusConflict)
	})
	for name, h := range map[string]func(context.Context, mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error){
		"autonomous_deploy_get": s.handleAutonomousDeployGet,
		"autonomous_deploy_run": s.handleAutonomousDeployRun,
	} {
		res, err := h(context.Background(), callReq(nil))
		if err != nil || res == nil || !res.IsError {
			t.Fatalf("%s: want an MCP error result, got %+v err=%v", name, res, err)
		}
		text := automationText(t, res)
		if !strings.Contains(text, name) || !strings.Contains(text, "409") {
			t.Errorf("%s: error text %q lacks tool name or status", name, text)
		}
	}
}
