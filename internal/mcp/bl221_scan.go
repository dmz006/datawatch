// BL221 (v6.2.0) Phase 3 — MCP tools for the scan framework.
// Follows the same proxyGet/proxyJSON loopback pattern as autonomous.go.

package mcp

import (
	"context"
	"encoding/json"
	"net/http"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

// ----- autonomous_scan_config_get / set -------------------------------------

func (s *Server) toolAutonomousScanConfigGet() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_scan_config_get",
		mcpsdk.WithDescription("BL221 Phase 3 — read scan framework config (SAST, secrets, deps, rules grader, fix loop)."),
	)
}
func (s *Server) handleAutonomousScanConfigGet(_ context.Context, _ mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	out, err := s.proxyGet("/api/autonomous/scan/config", nil)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) toolAutonomousScanConfigSet() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_scan_config_set",
		mcpsdk.WithDescription("BL221 Phase 3 — update scan framework config. All fields optional (partial update)."),
		mcpsdk.WithBoolean("enabled", mcpsdk.Description("Master switch for the scan framework")),
		mcpsdk.WithBoolean("sast_enabled", mcpsdk.Description("SAST static-analysis scanner")),
		mcpsdk.WithBoolean("secrets_enabled", mcpsdk.Description("Hardcoded-secrets scanner")),
		mcpsdk.WithBoolean("deps_enabled", mcpsdk.Description("Dependency manifest + known-CVE scanner")),
		mcpsdk.WithBoolean("rules_grader_enabled", mcpsdk.Description("LLM rules-check grader (requires ask-compatible backend)")),
		mcpsdk.WithBoolean("fix_loop_enabled", mcpsdk.Description("Create fix sub-PRD automatically on scan failure")),
		mcpsdk.WithString("fail_on_severity", mcpsdk.Description("Minimum severity to fail scan: info|warning|error|critical (default error)")),
		mcpsdk.WithNumber("max_findings", mcpsdk.Description("Cap on findings returned per scan (0 = unlimited)")),
		mcpsdk.WithNumber("fix_loop_max_retries", mcpsdk.Description("Max retries for the fix loop (default 0)")),
	)
}
func (s *Server) handleAutonomousScanConfigSet(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	body := map[string]any{}
	for _, k := range []string{"enabled", "sast_enabled", "secrets_enabled", "deps_enabled", "rules_grader_enabled", "fix_loop_enabled"} {
		if v := req.GetBool(k, false); v {
			body[k] = v
		}
	}
	if v := req.GetString("fail_on_severity", ""); v != "" {
		body["fail_on_severity"] = v
	}
	for _, k := range []string{"max_findings", "fix_loop_max_retries"} {
		if v := req.GetFloat(k, -1); v >= 0 {
			body[k] = int(v)
		}
	}
	rawBody, _ := json.Marshal(body)
	out, err := s.proxyJSON(http.MethodPut, "/api/autonomous/scan/config", rawBody)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

// ----- autonomous_prd_scan / autonomous_prd_scan_results --------------------

func (s *Server) toolAutonomousPRDScan() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_prd_scan",
		mcpsdk.WithDescription("BL221 Phase 3 — run SAST, secrets, and dependency scan against a PRD's project directory. Returns findings + pass/fail verdict."),
		mcpsdk.WithString("id", mcpsdk.Required(), mcpsdk.Description("PRD ID")),
	)
}
func (s *Server) handleAutonomousPRDScan(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	id := req.GetString("id", "")
	out, err := s.proxyJSON(http.MethodPost, "/api/autonomous/prds/"+id+"/scan", nil)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) toolAutonomousPRDScanResults() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_prd_scan_results",
		mcpsdk.WithDescription("BL221 Phase 3 — fetch the latest cached scan results for a PRD (run autonomous_prd_scan first)."),
		mcpsdk.WithString("id", mcpsdk.Required(), mcpsdk.Description("PRD ID")),
	)
}
func (s *Server) handleAutonomousPRDScanResults(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	id := req.GetString("id", "")
	out, err := s.proxyGet("/api/autonomous/prds/"+id+"/scan", nil)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

// ----- autonomous_prd_scan_fix (Phase 3b) ------------------------------------

func (s *Server) toolAutonomousPRDScanFix() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_prd_scan_fix",
		mcpsdk.WithDescription("BL221 Phase 3b — create a fix child-PRD from the latest scan findings. The child PRD spec targets each violation; run it to apply fixes."),
		mcpsdk.WithString("id", mcpsdk.Required(), mcpsdk.Description("PRD ID with a completed scan")),
	)
}
func (s *Server) handleAutonomousPRDScanFix(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	id := req.GetString("id", "")
	out, err := s.proxyJSON(http.MethodPost, "/api/autonomous/prds/"+id+"/scan/fix", nil)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

// ----- autonomous_prd_scan_rules (Phase 3b) ----------------------------------

func (s *Server) toolAutonomousPRDScanRules() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_prd_scan_rules",
		mcpsdk.WithDescription("BL221 Phase 3b — ask the LLM to propose AGENT.md rule changes that would prevent the latest scan violations. Returns proposed diff."),
		mcpsdk.WithString("id", mcpsdk.Required(), mcpsdk.Description("PRD ID with a completed scan")),
	)
}
func (s *Server) handleAutonomousPRDScanRules(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	id := req.GetString("id", "")
	out, err := s.proxyJSON(http.MethodPost, "/api/autonomous/prds/"+id+"/scan/rules", nil)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}
