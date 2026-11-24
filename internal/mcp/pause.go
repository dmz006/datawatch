// Operator pause MCP tools. They proxy the loopback REST surface
// (/api/pause) so argument names and validation match REST, the CLI and the
// comm channel exactly. A pause stops new work from being admitted; running
// work finishes. The daemon refuses a resume from a session-scoped credential
// (and from any actor that names a worker, watcher, supervisor or model), so
// pause_resume called from a session is refused by the daemon, not by a rule
// the model has to follow.

package mcp

import (
	"context"
	"net/http"
	"net/url"
	"strings"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

func (s *Server) toolPauseStatus() mcpsdk.Tool {
	return mcpsdk.NewTool("pause_status",
		mcpsdk.WithDescription("Show whether new work is paused by the operator: the global pause (scope, reason, who, when) and every per-project pause. A pause stops new Automaton starts, restarts, new worker admissions and lane fill; work already running finishes, and schedules, tabs, history and reports stay up. With peers set, also lists each federation peer's own pause state."),
		mcpsdk.WithBoolean("peers", mcpsdk.Description("Also read every federation peer's own pause state")),
	)
}
func (s *Server) handlePauseStatus(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	q := url.Values{}
	if req.GetBool("peers", false) {
		q.Set("peers", "1")
	}
	out, err := s.proxyGet("/api/pause", q)
	if err != nil {
		return toolError("pause_status", err), nil
	}
	return textOK(string(out)), nil
}

func (s *Server) toolPauseSet() mcpsdk.Tool {
	return mcpsdk.NewTool("pause_set",
		mcpsdk.WithDescription("Pause new work (operator action). Scope global pauses everything; scope project pauses one project (a registered project name or a directory). Running tasks finish; nothing new is admitted or restarted until the operator resumes. A reason is required. Needs the pause:write capability, which is not in a session's default grant."),
		mcpsdk.WithString("scope", mcpsdk.Required(), mcpsdk.Description("global or project")),
		mcpsdk.WithString("reason", mcpsdk.Required(), mcpsdk.Description("Why work is paused (recorded in the audit log)")),
		mcpsdk.WithString("project", mcpsdk.Description("Project name or directory (required for scope project)")),
		mcpsdk.WithBoolean("all_peers", mcpsdk.Description("Also send the pause to every federation peer")),
		mcpsdk.WithArray("peers", mcpsdk.WithStringItems(), mcpsdk.Description("Also send the pause to these federation peers")),
	)
}
func (s *Server) handlePauseSet(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	scope := strings.TrimSpace(req.GetString("scope", ""))
	if scope == "" {
		return toolErrorf("pause_set", "scope is required (global or project)"), nil
	}
	reason := strings.TrimSpace(req.GetString("reason", ""))
	if reason == "" {
		return toolErrorf("pause_set", "reason is required; say why work is being paused"), nil
	}
	body := map[string]any{"scope": scope, "reason": reason}
	if p := strings.TrimSpace(req.GetString("project", "")); p != "" {
		body["project"] = p
	}
	pausePeersArgs(req, body)
	out, err := s.proxyJSON(http.MethodPost, "/api/pause", body)
	if err != nil {
		return toolError("pause_set", err), nil
	}
	return textOK(string(out)), nil
}

func (s *Server) toolPauseResume() mcpsdk.Tool {
	return mcpsdk.NewTool("pause_resume",
		mcpsdk.WithDescription("Resume paused work (operator action). Give scope (and project for a project pause), or all=true to clear the global pause and every project pause in one action. The daemon refuses this from a session-scoped credential or any actor that names a worker, watcher, supervisor or model: only the operator can resume."),
		mcpsdk.WithString("scope", mcpsdk.Description("global or project (omit with all=true)")),
		mcpsdk.WithString("project", mcpsdk.Description("Project name or directory (for scope project)")),
		mcpsdk.WithBoolean("all", mcpsdk.Description("Clear every pause, global and per project")),
		mcpsdk.WithString("note", mcpsdk.Description("Optional note recorded in the audit log")),
		mcpsdk.WithBoolean("all_peers", mcpsdk.Description("Also resume every federation peer")),
		mcpsdk.WithArray("peers", mcpsdk.WithStringItems(), mcpsdk.Description("Also resume these federation peers")),
	)
}
func (s *Server) handlePauseResume(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	body := map[string]any{}
	all := req.GetBool("all", false)
	scope := strings.TrimSpace(req.GetString("scope", ""))
	if !all && scope == "" {
		return toolErrorf("pause_resume", "give scope (global or project) or all=true"), nil
	}
	if all {
		body["all"] = true
	} else {
		body["scope"] = scope
	}
	if p := strings.TrimSpace(req.GetString("project", "")); p != "" {
		body["project"] = p
	}
	if n := strings.TrimSpace(req.GetString("note", "")); n != "" {
		body["note"] = n
	}
	pausePeersArgs(req, body)
	out, err := s.proxyJSON(http.MethodPost, "/api/pause/resume", body)
	if err != nil {
		return toolError("pause_resume", err), nil
	}
	return textOK(string(out)), nil
}

// pausePeersArgs copies the optional federation arguments into the body.
func pausePeersArgs(req mcpsdk.CallToolRequest, body map[string]any) {
	if req.GetBool("all_peers", false) {
		body["all_peers"] = true
	}
	if raw, ok := req.GetArguments()["peers"].([]any); ok {
		var names []string
		for _, v := range raw {
			if n, ok := v.(string); ok && strings.TrimSpace(n) != "" {
				names = append(names, strings.TrimSpace(n))
			}
		}
		if len(names) > 0 {
			body["peers"] = names
		}
	}
}
