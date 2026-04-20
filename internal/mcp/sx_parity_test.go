// Sprint Sx — verify all parity-backfill MCP tools register with
// the right names + required args.

package mcp

import (
	"testing"
)

func TestSx_MCP_ToolNames(t *testing.T) {
	s := &Server{}
	cases := []struct {
		want string
		got  string
	}{
		{"ask", s.toolAsk().Name},
		{"project_summary", s.toolProjectSummary().Name},
		{"template_list", s.toolTemplateList().Name},
		{"template_upsert", s.toolTemplateUpsert().Name},
		{"template_delete", s.toolTemplateDelete().Name},
		{"project_list", s.toolProjectList().Name},
		{"project_upsert", s.toolProjectUpsert().Name},
		{"project_alias_delete", s.toolProjectAliasDelete().Name},
		{"session_rollback", s.toolSessionRollback().Name},
		{"cooldown_status", s.toolCooldownStatus().Name},
		{"cooldown_set", s.toolCooldownSet().Name},
		{"cooldown_clear", s.toolCooldownClear().Name},
		{"sessions_stale", s.toolSessionsStale().Name},
		{"cost_summary", s.toolCostSummary().Name},
		{"cost_usage", s.toolCostUsage().Name},
		{"cost_rates", s.toolCostRates().Name},
		{"audit_query", s.toolAuditQuery().Name},
		{"diagnose", s.toolDiagnose().Name},
		{"reload", s.toolReload().Name},
		{"analytics", s.toolAnalytics().Name},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("tool name = %q, want %q", c.got, c.want)
		}
	}
}

func TestSx_MCP_AskRequiresQuestion(t *testing.T) {
	s := &Server{}
	tool := s.toolAsk()
	required := false
	for _, r := range tool.InputSchema.Required {
		if r == "question" {
			required = true
		}
	}
	if !required {
		t.Errorf("ask: question must be required, got: %v", tool.InputSchema.Required)
	}
}

func TestSx_MCP_ProjectSummaryRequiresDir(t *testing.T) {
	s := &Server{}
	tool := s.toolProjectSummary()
	required := false
	for _, r := range tool.InputSchema.Required {
		if r == "dir" {
			required = true
		}
	}
	if !required {
		t.Errorf("project_summary: dir must be required: %v", tool.InputSchema.Required)
	}
}

func TestSx_MCP_LoopbackUnavailableErrors(t *testing.T) {
	// webPort=0 means loopback unavailable. proxyGet/proxyJSON should
	// surface a clear error rather than panic.
	s := &Server{}
	if _, err := s.proxyGet("/api/cost", nil); err == nil {
		t.Error("expected error when webPort is 0")
	}
	if _, err := s.proxyJSON("POST", "/api/ask", map[string]any{"q": "x"}); err == nil {
		t.Error("expected error when webPort is 0")
	}
}
