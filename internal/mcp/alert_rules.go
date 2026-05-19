// S14b — MCP-tool parity for per-pod alert rules.

package mcp

import (
	"context"
	"encoding/json"
	"net/http"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

// alert_rule_list -------------------------------------------------------

func (s *Server) toolAlertRuleList() mcpsdk.Tool {
	return mcpsdk.NewTool("alert_rule_list",
		mcpsdk.WithDescription("S14b — list all per-pod alert rules."),
	)
}
func (s *Server) handleAlertRuleList(_ context.Context, _ mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	out, err := s.proxyGet("/api/alert-rules", nil)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

// alert_rule_get --------------------------------------------------------

func (s *Server) toolAlertRuleGet() mcpsdk.Tool {
	return mcpsdk.NewTool("alert_rule_get",
		mcpsdk.WithDescription("S14b — fetch one alert rule by name."),
		mcpsdk.WithString("name", mcpsdk.Required(), mcpsdk.Description("Rule name")),
	)
}
func (s *Server) handleAlertRuleGet(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	name := req.GetString("name", "")
	out, err := s.proxyGet("/api/alert-rules/"+name, nil)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

// alert_rule_create -----------------------------------------------------

func (s *Server) toolAlertRuleCreate() mcpsdk.Tool {
	return mcpsdk.NewTool("alert_rule_create",
		mcpsdk.WithDescription("S14b — create a new per-pod alert rule."),
		mcpsdk.WithString("name", mcpsdk.Required(), mcpsdk.Description("Unique rule name")),
		mcpsdk.WithString("rule_json", mcpsdk.Required(), mcpsdk.Description("Full AlertRule object as JSON")),
	)
}
func (s *Server) handleAlertRuleCreate(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	ruleJSON := req.GetString("rule_json", "")
	var body map[string]any
	if err := json.Unmarshal([]byte(ruleJSON), &body); err != nil {
		return nil, err
	}
	// Enforce name from named param if not present in JSON.
	if _, ok := body["name"]; !ok {
		body["name"] = req.GetString("name", "")
	}
	out, err := s.proxyJSON(http.MethodPost, "/api/alert-rules", body)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

// alert_rule_update -----------------------------------------------------

func (s *Server) toolAlertRuleUpdate() mcpsdk.Tool {
	return mcpsdk.NewTool("alert_rule_update",
		mcpsdk.WithDescription("S14b — replace an existing alert rule."),
		mcpsdk.WithString("name", mcpsdk.Required(), mcpsdk.Description("Rule name to update")),
		mcpsdk.WithString("rule_json", mcpsdk.Required(), mcpsdk.Description("Updated AlertRule object as JSON")),
	)
}
func (s *Server) handleAlertRuleUpdate(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	name := req.GetString("name", "")
	ruleJSON := req.GetString("rule_json", "")
	var body map[string]any
	if err := json.Unmarshal([]byte(ruleJSON), &body); err != nil {
		return nil, err
	}
	out, err := s.proxyJSON(http.MethodPut, "/api/alert-rules/"+name, body)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

// alert_rule_delete -----------------------------------------------------

func (s *Server) toolAlertRuleDelete() mcpsdk.Tool {
	return mcpsdk.NewTool("alert_rule_delete",
		mcpsdk.WithDescription("S14b — delete an alert rule by name."),
		mcpsdk.WithString("name", mcpsdk.Required(), mcpsdk.Description("Rule name")),
	)
}
func (s *Server) handleAlertRuleDelete(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	name := req.GetString("name", "")
	out, err := s.proxyJSON(http.MethodDelete, "/api/alert-rules/"+name, nil)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

// alert_rule_enable -----------------------------------------------------

func (s *Server) toolAlertRuleEnable() mcpsdk.Tool {
	return mcpsdk.NewTool("alert_rule_enable",
		mcpsdk.WithDescription("S14b — enable an alert rule."),
		mcpsdk.WithString("name", mcpsdk.Required(), mcpsdk.Description("Rule name")),
	)
}
func (s *Server) handleAlertRuleEnable(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	name := req.GetString("name", "")
	out, err := s.proxyJSON(http.MethodPost, "/api/alert-rules/"+name+"/enable", nil)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

// alert_rule_disable ----------------------------------------------------

func (s *Server) toolAlertRuleDisable() mcpsdk.Tool {
	return mcpsdk.NewTool("alert_rule_disable",
		mcpsdk.WithDescription("S14b — disable an alert rule."),
		mcpsdk.WithString("name", mcpsdk.Required(), mcpsdk.Description("Rule name")),
	)
}
func (s *Server) handleAlertRuleDisable(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	name := req.GetString("name", "")
	out, err := s.proxyJSON(http.MethodPost, "/api/alert-rules/"+name+"/disable", nil)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

// alert_rule_firings ----------------------------------------------------

func (s *Server) toolAlertRuleFirings() mcpsdk.Tool {
	return mcpsdk.NewTool("alert_rule_firings",
		mcpsdk.WithDescription("S14b — list the last 100 alert rule firings."),
	)
}
func (s *Server) handleAlertRuleFirings(_ context.Context, _ mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	out, err := s.proxyGet("/api/alert-rules/firings", nil)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}
