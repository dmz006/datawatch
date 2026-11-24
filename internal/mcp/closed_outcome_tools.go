// Closed work MCP tools:
//
//	POST /api/autonomous/prds/{id}/outcome  → autonomous_prd_set_outcome
//	     (archive=true: POST /api/autonomous/prds/{id}/archive with the outcome)
//	GET|POST /api/autonomous/outcome-check  → autonomous_outcome_check
//	POST /api/autonomous/deploy/record      → autonomous_deploy_record
package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

func (s *Server) toolAutonomousPRDSetOutcome() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_prd_set_outcome",
		mcpsdk.WithDescription("Record the outcome class of a finished Automaton (completed, rejected, cancelled or archived): delivered (its work is on the base branch, by itself or by a twin named in delivered_via), superseded (replaced by the Automaton named in delivered_via), dropped (thrown away on purpose) or held (waiting by decision; held work is never archived). A delivered twin must be a stored Automaton with a shipped record. Never changes the status, unless archive=true: then the Automaton is archived in the same step, and the archive is refused when no outcome can be recorded or a hold is not met. Proxies POST /api/autonomous/prds/{id}/outcome (or .../archive)."),
		mcpsdk.WithString("id", mcpsdk.Required(), mcpsdk.Description("Automaton (PRD) ID")),
		mcpsdk.WithString("outcome", mcpsdk.Required(), mcpsdk.Enum("delivered", "superseded", "dropped", "held"), mcpsdk.Description("The outcome class")),
		mcpsdk.WithString("delivered_via", mcpsdk.Description("ID of the twin Automaton that delivered the work (delivered), or of the Automaton that replaced it (superseded; required)")),
		mcpsdk.WithString("note", mcpsdk.Description("Why, in a sentence")),
		mcpsdk.WithBoolean("archive", mcpsdk.Description("Archive the Automaton in the same step (default false)")),
	)
}

func (s *Server) handleAutonomousPRDSetOutcome(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	id := strings.TrimSpace(req.GetString("id", ""))
	outcome := strings.TrimSpace(req.GetString("outcome", ""))
	if id == "" || outcome == "" {
		return mcpsdk.NewToolResultError("id and outcome are required"), nil
	}
	body := map[string]any{"outcome": outcome}
	if v := req.GetString("delivered_via", ""); v != "" {
		body["delivered_via"] = v
	}
	if v := req.GetString("note", ""); v != "" {
		body["note"] = v
	}
	action := "outcome"
	if req.GetBool("archive", false) {
		action = "archive"
	}
	b, _ := json.Marshal(body)
	out, err := s.proxyJSON(http.MethodPost, "/api/autonomous/prds/"+url.PathEscape(id)+"/"+action, b)
	return llmSetterResult(out, err)
}

func (s *Server) toolAutonomousOutcomeCheck() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_outcome_check",
		mcpsdk.WithDescription("Read the standing check of closed work: Automata that closed or were archived with no outcome, completed work with no shipped record, shipped work with no deploy record on a host that deploys after ship, a deployed version older than the shipped one, and work closed as delivered whose intent proof (the files and symbols its spec names on the base branch, plus task completion) no longer holds. Returns mode (off, shadow or on; autonomous.closed_outcome_check), scanned, counts, findings (never null) and the proof counts. Read-only unless run=true, which runs the check now including the intent proof and stores the report; it never changes an Automaton. Proxies GET or POST /api/autonomous/outcome-check."),
		mcpsdk.WithBoolean("run", mcpsdk.Description("Run the check now (default false: read the live view)")),
	)
}

func (s *Server) handleAutonomousOutcomeCheck(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	var out []byte
	var err error
	if req.GetBool("run", false) {
		out, err = s.proxyJSON(http.MethodPost, "/api/autonomous/outcome-check", nil)
	} else {
		out, err = s.proxyGet("/api/autonomous/outcome-check", nil)
	}
	return llmSetterResult(out, err)
}

func (s *Server) toolAutonomousDeployRecord() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_deploy_record",
		mcpsdk.WithDescription("Record that a release was deployed: writes the deploy record (version, time, host, health check result) onto every Automaton shipped in that release. The deploy-after-ship step writes it by itself; use this when a deploy was done by hand or by your own script. Idempotent for the same deploy. Never changes a status. Proxies POST /api/autonomous/deploy/record."),
		mcpsdk.WithString("release", mcpsdk.Required(), mcpsdk.Description("The shipped release that was deployed, e.g. v1.2.0")),
		mcpsdk.WithString("version", mcpsdk.Description("The version the host runs after the deploy (default: the release)")),
		mcpsdk.WithString("host", mcpsdk.Description("The host deployed to (default: the daemon's host name)")),
		mcpsdk.WithString("health", mcpsdk.Enum("passed", "failed", "unchecked"), mcpsdk.Description("The health check result (default unchecked)")),
		mcpsdk.WithString("detail", mcpsdk.Description("A short account of the deploy or its health check")),
		mcpsdk.WithString("at", mcpsdk.Description("When the deploy finished, RFC 3339 (default now)")),
	)
}

func (s *Server) handleAutonomousDeployRecord(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	release := strings.TrimSpace(req.GetString("release", ""))
	if release == "" {
		return mcpsdk.NewToolResultError("release is required"), nil
	}
	body := map[string]any{"release": release}
	for _, k := range []string{"version", "host", "health", "detail", "at"} {
		if v := req.GetString(k, ""); v != "" {
			body[k] = v
		}
	}
	b, _ := json.Marshal(body)
	out, err := s.proxyJSON(http.MethodPost, "/api/autonomous/deploy/record", b)
	return llmSetterResult(out, err)
}
