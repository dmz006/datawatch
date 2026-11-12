// Decompose-lint MCP tool tests — the MCP handlers are thin proxies over
// the decompose-lint REST endpoints, so the fakes stand up a loopback
// REST capture (method + path + body) and each test pins the tool's
// exact call contract. httptest only; no daemon, no operator config.

package mcp

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

type decomposeLintCapture struct {
	method string
	path   string
	body   string
	count  int
}

func (c *decomposeLintCapture) add(method, path, body string) {
	c.method = method
	c.path = path
	c.body = body
	c.count++
}

func (c *decomposeLintCapture) bodyObj(t *testing.T) map[string]any {
	t.Helper()
	var m map[string]any
	if c.body != "" {
		if err := json.Unmarshal([]byte(c.body), &m); err != nil {
			t.Fatalf("captured body is not JSON: %v (%s)", err, c.body)
		}
	}
	return m
}

func newDecomposeLintProbeServer(t *testing.T, wantStatus int, wantBody string, cap *decomposeLintCapture) *Server {
	t.Helper()
	rest := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		cap.add(r.Method, r.URL.Path, string(raw))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(wantStatus)
		_, _ = w.Write([]byte(wantBody))
	}))
	t.Cleanup(rest.Close)

	_, portStr, err := net.SplitHostPort(rest.Listener.Addr().String())
	if err != nil {
		t.Fatalf("split host port: %v", err)
	}
	port, _ := strconv.Atoi(portStr)
	return &Server{webPort: port}
}

func mustToolText(t *testing.T, res *mcpsdk.CallToolResult) string {
	t.Helper()
	if res == nil {
		t.Fatal("nil tool result")
	}
	if len(res.Content) == 0 {
		t.Fatal("tool result has no content")
	}
	tc, ok := res.Content[0].(mcpsdk.TextContent)
	if !ok {
		t.Fatalf("tool result content[0] is %T, want TextContent", res.Content[0])
	}
	return tc.Text
}

// TestDecomposeLintMCP_ConfigGet pins autonomous_decompose_lint_config_get:
// a GET to /api/autonomous/decompose-lint/config, and the raw REST body
// returned verbatim to the caller.
func TestDecomposeLintMCP_ConfigGet(t *testing.T) {
	var cap decomposeLintCapture
	s := newDecomposeLintProbeServer(t, http.StatusOK, `{"decompose_lint":{"mode":"shadow"},"decompose_guidance":"x"}`, &cap)

	res, err := s.handleAutonomousDecomposeLintConfigGet(context.Background(), mcpsdk.CallToolRequest{})
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	if cap.method != http.MethodGet {
		t.Errorf("REST method = %q; want GET (read-only)", cap.method)
	}
	if want := "/api/autonomous/decompose-lint/config"; cap.path != want {
		t.Errorf("REST path = %q; want %q", cap.path, want)
	}
	if body := mustToolText(t, res); !strings.Contains(body, `"shadow"`) {
		t.Errorf("result body = %s; wants the REST payload verbatim", body)
	}
}

// TestDecomposeLintMCP_ConfigSet_ForwardsExactlyTheSuppliedBlocks pins
// autonomous_decompose_lint_config_set: a PUT carrying ONLY the supplied
// blocks (absent blocks stay absent — the server keeps them), and the
// raw values forwarded as-is (no trimming, no reshaping).
func TestDecomposeLintMCP_ConfigSet_ForwardsExactlyTheSuppliedBlocks(t *testing.T) {
	var cap decomposeLintCapture
	s := newDecomposeLintProbeServer(t, http.StatusOK, `{"ok":true}`, &cap)

	req := mcpsdk.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"decompose_lint":     map[string]any{"mode": "enforce", "max_retries": 2},
		"decompose_guidance": "be thorough",
	}
	res, err := s.handleAutonomousDecomposeLintConfigSet(context.Background(), req)
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	if cap.method != http.MethodPut {
		t.Errorf("REST method = %q; want PUT", cap.method)
	}
	if want := "/api/autonomous/decompose-lint/config"; cap.path != want {
		t.Errorf("REST path = %q; want %q", cap.path, want)
	}
	body := cap.bodyObj(t)
	lint, ok := body["decompose_lint"].(map[string]any)
	if !ok {
		t.Fatalf("PUT body %s: decompose_lint missing or not an object", cap.body)
	}
	if lint["mode"] != "enforce" || lint["max_retries"] != float64(2) {
		t.Errorf("PUT body decompose_lint = %v; want the raw argument forwarded", lint)
	}
	if body["decompose_guidance"] != "be thorough" {
		t.Errorf("PUT body decompose_guidance = %v; want the raw string forwarded", body["decompose_guidance"])
	}
	if _, present := body["task_templates"]; present {
		t.Errorf("PUT body %s must not fabricate a task_templates block", cap.body)
	}
	_ = mustToolText(t, res)
}

// TestDecomposeLintMCP_ConfigSet_NothingSupplied pins the guard: with no
// blocks supplied the tool returns an error result and makes NO REST call
// (an empty PUT would be a config no-op at best, and it is not a valid
// client move either).
func TestDecomposeLintMCP_ConfigSet_NothingSupplied(t *testing.T) {
	var cap decomposeLintCapture
	s := newDecomposeLintProbeServer(t, http.StatusOK, `{}`, &cap)

	res, err := s.handleAutonomousDecomposeLintConfigSet(context.Background(), mcpsdk.CallToolRequest{})
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	if res == nil || !res.IsError {
		t.Fatalf("expected an error tool result; got %+v", res)
	}
	if cap.count != 0 {
		t.Errorf("expected 0 REST calls, got %d", cap.count)
	}
}

// TestDecomposeLintMCP_DryRun pins autonomous_decompose_lint_dry_run: a
// POST to the per-PRD endpoint with the id verbatim, and an error (no
// REST call) when the id is missing.
func TestDecomposeLintMCP_DryRun(t *testing.T) {
	var cap decomposeLintCapture
	s := newDecomposeLintProbeServer(t, http.StatusOK, `{"prd":"prd-1","mode":"shadow"}`, &cap)

	req := mcpsdk.CallToolRequest{}
	req.Params.Arguments = map[string]any{"id": "prd-1"}
	res, err := s.handleAutonomousDecomposeLintDryRun(context.Background(), req)
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	if cap.method != http.MethodPost {
		t.Errorf("REST method = %q; want POST: the dry run is the action that triggers the check", cap.method)
	}
	if want := "/api/autonomous/prds/prd-1/decompose-lint"; cap.path != want {
		t.Errorf("REST path = %q; want %q", cap.path, want)
	}
	if body := mustToolText(t, res); !strings.Contains(body, `"shadow"`) {
		t.Errorf("result body = %s; wants the report verbatim", body)
	}

	// Missing id → error result, zero REST calls.
	var cap2 decomposeLintCapture
	s2 := newDecomposeLintProbeServer(t, http.StatusOK, `{}`, &cap2)
	res2, err := s2.handleAutonomousDecomposeLintDryRun(context.Background(), mcpsdk.CallToolRequest{})
	if err != nil {
		t.Fatalf("handler (no id): %v", err)
	}
	if res2 == nil || !res2.IsError {
		t.Fatalf("missing id should be an error tool result; got %+v", res2)
	}
	if cap2.count != 0 {
		t.Errorf("missing id: expected 0 REST calls, got %d", cap2.count)
	}
}

// TestDecomposeLintMCP_Findings pins autonomous_decompose_lint_findings:
// a GET to the per-PRD endpoint, id verbatim, and an error (no REST call)
// when the id is missing.
func TestDecomposeLintMCP_Findings(t *testing.T) {
	var cap decomposeLintCapture
	s := newDecomposeLintProbeServer(t, http.StatusOK, `{"prd_id":"prd-1","decompose_lint":null}`, &cap)

	req := mcpsdk.CallToolRequest{}
	req.Params.Arguments = map[string]any{"id": "prd-1"}
	res, err := s.handleAutonomousDecomposeLintFindings(context.Background(), req)
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	if cap.method != http.MethodGet {
		t.Errorf("REST method = %q; want GET (findings is the stored-record read)", cap.method)
	}
	if want := "/api/autonomous/prds/prd-1/decompose-lint"; cap.path != want {
		t.Errorf("REST path = %q; want %q", cap.path, want)
	}
	if body := mustToolText(t, res); !strings.Contains(body, `"decompose_lint"`) {
		t.Errorf("result body = %s; wants the record shape", body)
	}

	var cap2 decomposeLintCapture
	s2 := newDecomposeLintProbeServer(t, http.StatusOK, `{}`, &cap2)
	res2, err := s2.handleAutonomousDecomposeLintFindings(context.Background(), mcpsdk.CallToolRequest{})
	if err != nil {
		t.Fatalf("handler (no id): %v", err)
	}
	if res2 == nil || !res2.IsError {
		t.Fatalf("missing id should be an error tool result; got %+v", res2)
	}
	if cap2.count != 0 {
		t.Errorf("missing id: expected 0 REST calls, got %d", cap2.count)
	}
}

// TestDecomposeLintMCP_ToolDefinitions pins the four tool definitions:
// the exact registered names, the per-PRD pair requiring a single id, and
// the config_set schema exposing its three blocks.
func TestDecomposeLintMCP_ToolDefinitions(t *testing.T) {
	s := &Server{}

	get := s.toolAutonomousDecomposeLintConfigGet()
	set := s.toolAutonomousDecomposeLintConfigSet()
	dry := s.toolAutonomousDecomposeLintDryRun()
	fin := s.toolAutonomousDecomposeLintFindings()

	wantNames := []struct {
		name string
		tool mcpsdk.Tool
	}{
		{"autonomous_decompose_lint_config_get", get},
		{"autonomous_decompose_lint_config_set", set},
		{"autonomous_decompose_lint_dry_run", dry},
		{"autonomous_decompose_lint_findings", fin},
	}
	for _, w := range wantNames {
		if w.tool.Name != w.name {
			t.Errorf("tool name = %q; want %q", w.tool.Name, w.name)
		}
	}

	for name, tool := range map[string]mcpsdk.Tool{"dry_run": dry, "findings": fin} {
		if len(tool.InputSchema.Properties) == 0 {
			t.Errorf("%s: no input schema", name)
			continue
		}
		if _, ok := tool.InputSchema.Properties["id"]; !ok {
			t.Errorf("%s: missing id property", name)
		}
		if reqd := tool.InputSchema.Required; len(reqd) == 0 || reqd[0] != "id" {
			t.Errorf("%s: required = %v; want [id]", name, reqd)
		}
	}

	if len(set.InputSchema.Properties) == 0 {
		t.Fatal("config_set: no input schema")
	}
	for _, k := range []string{"decompose_lint", "task_templates", "decompose_guidance"} {
		if _, ok := set.InputSchema.Properties[k]; !ok {
			t.Errorf("config_set: missing %q property", k)
		}
	}
}
