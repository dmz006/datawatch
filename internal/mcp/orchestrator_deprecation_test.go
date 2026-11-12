package mcp

import (
	"context"
	"net/http"
	"strings"
	"testing"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"

	"github.com/dmz006/datawatch/internal/orchestrator"
)

func TestOrchestratorMCPDeprecationNotice(t *testing.T) {
	orchestrator.ResetDeprecationWarnedForTest()
	s, rec := activateServer(t, http.StatusOK, `{"id":"g1"}`)
	ctx := context.Background()

	cases := []struct {
		tool string
		desc string
		run  func() (*mcpsdk.CallToolResult, error)
	}{
		{"orchestrator_graph_create", s.toolOrchestratorGraphCreate().Description, func() (*mcpsdk.CallToolResult, error) {
			return s.handleOrchestratorGraphCreate(ctx, callReq(map[string]any{"title": "t", "prd_ids": `["a"]`}))
		}},
		{"orchestrator_graph_plan", s.toolOrchestratorGraphPlan().Description, func() (*mcpsdk.CallToolResult, error) {
			return s.handleOrchestratorGraphPlan(ctx, callReq(map[string]any{"id": "g1"}))
		}},
		{"orchestrator_graph_run", s.toolOrchestratorGraphRun().Description, func() (*mcpsdk.CallToolResult, error) {
			return s.handleOrchestratorGraphRun(ctx, callReq(map[string]any{"id": "g1"}))
		}},
	}
	for _, c := range cases {
		if !strings.HasPrefix(c.desc, "DEPRECATED:") {
			t.Errorf("%s: description = %q", c.tool, c.desc)
		}
		res, err := c.run()
		if err != nil || res == nil {
			t.Fatalf("%s: err=%v res=%v", c.tool, err, res)
		}
		text := automationText(t, res)
		if !strings.Contains(text, `"id":"g1"`) {
			t.Errorf("%s: original body dropped: %s", c.tool, text)
		}
		if !strings.Contains(text, orchestrator.DeprecationNotice(c.tool)) {
			t.Errorf("%s: notice missing: %s", c.tool, text)
		}
	}
	if rec.method != http.MethodPost {
		t.Errorf("last proxied method = %s", rec.method)
	}

	// Untouched tools carry no notice.
	for name, d := range map[string]string{
		"list":   s.toolOrchestratorGraphList().Description,
		"get":    s.toolOrchestratorGraphGet().Description,
		"cancel": s.toolOrchestratorGraphCancel().Description,
	} {
		if strings.Contains(d, "DEPRECATED") {
			t.Errorf("%s description carries the notice: %q", name, d)
		}
	}
	res, err := s.handleOrchestratorGraphGet(ctx, callReq(map[string]any{"id": "g1"}))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(automationText(t, res), "deprecated") {
		t.Errorf("get result carries the notice")
	}
}
