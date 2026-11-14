// Decompose-lint MCP tools — thin proxies over the decompose-lint REST
// endpoints:
//
//	autonomous_decompose_lint_config_get  → GET  /api/autonomous/decompose-lint/config
//	autonomous_decompose_lint_config_set  → PUT  /api/autonomous/decompose-lint/config
//	autonomous_decompose_lint_dry_run     → POST /api/autonomous/prds/{id}/decompose-lint
//	autonomous_decompose_lint_findings    → GET  /api/autonomous/prds/{id}/decompose-lint
//
// The dry run and the stored-record read are read-only (the dry run
// reports what the check would record right now and mutates nothing), so
// they carry the read-only hint. The config set goes through the same
// validated REST path an operator would PUT to, including the
// live→YAML persistence round-trip.

package mcp

import (
	"context"
	"net/http"
	"strings"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

// decomposeLintBlockDesc is shared by the config-set object parameters.
const decomposeLintBlockDesc = "Full replacement of one decompose-lint config block. " +
	"decompose_lint takes {mode, max_retries, rules, disabled_rules}: " +
	"mode is off (lint runs, nothing recorded), shadow (recorded, never blocks) or enforce (recorded and blocking; the default when unset); " +
	"max_retries is the decompose retry budget (whole number >= 0; 0 disables auto-retry); " +
	"rules is an array of {id, severity (block|warn), match, message} lint rules; " +
	"disabled_rules is an array of rule ids to turn off. " +
	"task_templates takes {enabled, templates, projects}: enabled toggles template resolution; " +
	"templates is an array of {id, title, spec, files, kind, read_only, depends_on, applies_when} definitions; " +
	"projects maps a project id to the template ids it applies (or the special key \"default\"). " +
	"decompose_guidance is the literal decomposition-guidance string given to the Decomposer."

// ── autonomous_decompose_lint_config_get ─────────────────────────────────

func (s *Server) toolAutonomousDecomposeLintConfigGet() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_decompose_lint_config_get",
		mcpsdk.WithDescription("Read the live decompose-lint configuration: the autonomous.decompose_lint sub-config (mode, max_retries, rules, disabled_rules), the autonomous.task_templates sub-config (enabled, templates, projects) and the autonomous.decompose_guidance string, as one object. Read-only: it changes nothing. The decompose-lint gate runs at decompose time: in enforce mode a failing report blocks the PRD from advancing until it passes; in shadow mode the report is only recorded."),
		mcpsdk.WithReadOnlyHintAnnotation(true),
	)
}

func (s *Server) handleAutonomousDecomposeLintConfigGet(_ context.Context, _ mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	out, err := s.proxyGet("/api/autonomous/decompose-lint/config", nil)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

// ── autonomous_decompose_lint_config_set ─────────────────────────────────

func (s *Server) toolAutonomousDecomposeLintConfigSet() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_decompose_lint_config_set",
		mcpsdk.WithDescription("Update the decompose-lint configuration (autonomous.decompose_lint, autonomous.task_templates and/or autonomous.decompose_guidance). Each supplied block fully replaces the current one; blocks left out are left untouched. The whole value is validated before any of it is applied: decompose_lint.mode must be off, shadow or enforce; rule ids must be unique and non-empty, severities one of block/warn, every message non-empty, every match regex must compile; template ids must be unique. An invalid value is rejected with a 400 and nothing is written. Returns the full new configuration. "+decomposeLintBlockDesc),
		mcpsdk.WithObject("decompose_lint", mcpsdk.Description("Full replacement of the decompose-lint sub-config: {mode, max_retries, rules, disabled_rules}")),
		mcpsdk.WithObject("task_templates", mcpsdk.Description("Full replacement of the task-templates sub-config: {enabled, templates, projects}")),
		mcpsdk.WithString("decompose_guidance", mcpsdk.Description("Replace the decompose-guidance string given to the Decomposer")),
	)
}

func (s *Server) handleAutonomousDecomposeLintConfigSet(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	args := req.GetArguments()
	body := map[string]any{}
	if raw, present := args["decompose_lint"]; present {
		body["decompose_lint"] = raw
	}
	if raw, present := args["task_templates"]; present {
		body["task_templates"] = raw
	}
	if raw, present := args["decompose_guidance"]; present {
		body["decompose_guidance"] = raw
	}
	if len(body) == 0 {
		return toolErrorf("autonomous_decompose_lint_config_set",
			"nothing to update: pass decompose_lint, task_templates and/or decompose_guidance"), nil
	}
	out, err := s.proxyJSON(http.MethodPut, "/api/autonomous/decompose-lint/config", body)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

// ── autonomous_decompose_lint_dry_run ────────────────────────────────────

func (s *Server) toolAutonomousDecomposeLintDryRun() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_decompose_lint_dry_run",
		mcpsdk.WithDescription("Run the decompose-lint check once, read-only, over one stored PRD's decomposed stories. Returns the report the check would record right now at the current configuration (mode, rule_count, findings, summary). It mutates nothing: the PRD is neither re-linted into its stored record nor re-decomposed, and no template is resolved. Use it to preview what the decompose-time gate (or, in enforce mode, the blocking verdict) will say before it matters."),
		mcpsdk.WithString("id", mcpsdk.Required(), mcpsdk.Description("PRD (Automaton) ID")),
		mcpsdk.WithReadOnlyHintAnnotation(true),
	)
}

func (s *Server) handleAutonomousDecomposeLintDryRun(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	id := strings.TrimSpace(req.GetString("id", ""))
	if id == "" {
		return toolErrorf("autonomous_decompose_lint_dry_run", "id is required"), nil
	}
	out, err := s.proxyJSON(http.MethodPost, "/api/autonomous/prds/"+id+"/decompose-lint", nil)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

// ── autonomous_decompose_lint_findings ───────────────────────────────────

func (s *Server) toolAutonomousDecomposeLintFindings() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_decompose_lint_findings",
		mcpsdk.WithDescription("Read the decompose-lint findings recorded on one PRD (its stored decompose_lint record: mode, rule_count, findings, summary) plus the PRD id. Returns decompose_lint: null when nothing is stored — e.g. the PRD predates the gate, or lint mode was off when it decomposed. Read-only: it only reads the stored record; to re-check against the current configuration use autonomous_decompose_lint_dry_run."),
		mcpsdk.WithString("id", mcpsdk.Required(), mcpsdk.Description("PRD (Automaton) ID")),
		mcpsdk.WithReadOnlyHintAnnotation(true),
	)
}

func (s *Server) handleAutonomousDecomposeLintFindings(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	id := strings.TrimSpace(req.GetString("id", ""))
	if id == "" {
		return toolErrorf("autonomous_decompose_lint_findings", "id is required"), nil
	}
	out, err := s.proxyGet("/api/autonomous/prds/"+id+"/decompose-lint", nil)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}
