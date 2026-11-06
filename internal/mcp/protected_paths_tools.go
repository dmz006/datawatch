package mcp

import (
	"context"
	"net/http"
	"net/url"
	"strings"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

// The protected-paths tools. autonomous_protected_paths is a read that
// proxies GET /api/autonomous/protected-paths: the action and rules source in
// effect, the effective protected list, the tasks held because their commit
// changed protected paths and the Automata that authorise some in advance.
// autonomous_prd_set_authorized_paths replaces the list of protected paths one
// Automaton's tasks may change without an operator decision (POST
// /api/autonomous/prds/{id}/set_authorized_paths). Neither tool answers the
// held question: the operator approves, rejects or changes the spec through
// the question queue.
//
// The three settings behind the gate (autonomous.automation.protected_paths,
// autonomous.automation.protected_paths_action and
// autonomous.automation.rules_review_source) are written through the generic
// config_set tool; there is no parallel config tool here.

func (s *Server) toolAutonomousProtectedPaths() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_protected_paths",
		mcpsdk.WithDescription("Read the Automaton protected-paths status: the action in effect (\"action\": hold, warn, block or off), where the rules are read from (\"rules_source\": base_commit or worktree), the effective protected list (\"paths\") and two lists, both empty when there is nothing to report. \"held\" has one entry per task held because its commit changed protected paths: the Automaton (\"automaton_id\", \"automaton_title\"), the task (\"task_id\", \"task_title\"), the changed protected paths (\"paths\"), the action that was in force (\"action\"), why it is held (\"reason\"), the question waiting for an answer (\"question_id\": approve, reject or change the spec), when the hold was placed (\"since\") and whether the task awaits a spec change (\"awaiting_spec_change\"). \"authorized\" has one entry per Automaton that authorises protected paths in advance (\"automaton_id\", \"automaton_title\", \"paths\"). Read-only; the same object as GET /api/autonomous/protected-paths. Nothing is approved, rejected or released."),
		mcpsdk.WithReadOnlyHintAnnotation(true),
		mcpsdk.WithString("automaton", mcpsdk.Description("Optional Automaton ID; narrows the held and authorized lists to that Automaton.")),
	)
}

func (s *Server) toolAutonomousPRDSetAuthorizedPaths() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_prd_set_authorized_paths",
		mcpsdk.WithDescription("Set the protected paths this Automaton's tasks may change without an operator decision. The list you send replaces the Automaton's current list; an empty array clears it. Each entry is a repository-relative path or directory (no absolute path, no .. segment); blank entries are ignored and the daemon cleans and de-duplicates the rest. The result is the updated Automaton, carrying \"authorized_paths\" unless the list is empty. An unknown Automaton or an invalid entry is refused with the daemon's message. Authorising paths in advance is the operator's choice: use it when the operator asked for it, not to get around a held task."),
		mcpsdk.WithString("id", mcpsdk.Required(), mcpsdk.Description("Automaton ID")),
		mcpsdk.WithArray("paths",
			mcpsdk.Required(),
			mcpsdk.WithStringItems(),
			mcpsdk.Description("Repository-relative protected paths this Automaton may change (e.g. [\"docs/plans/\",\"AGENT.md\"]); an empty array clears the list. The argument must be present: an omitted paths never clears the list.")),
	)
}

// ProtectedPathsToolNames lists the protected-paths tools, for the parity test.
func ProtectedPathsToolNames() []string {
	return []string{"autonomous_protected_paths", "autonomous_prd_set_authorized_paths"}
}

func (s *Server) handleAutonomousProtectedPaths(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	q := url.Values{}
	if id := strings.TrimSpace(req.GetString("automaton", "")); id != "" {
		q.Set("automaton", id)
	}
	out, err := s.proxyGet("/api/autonomous/protected-paths", q)
	if err != nil {
		return toolError("autonomous_protected_paths", err), nil
	}
	return textOK(string(out)), nil
}

func (s *Server) handleAutonomousPRDSetAuthorizedPaths(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	const op = "autonomous_prd_set_authorized_paths"
	id := strings.TrimSpace(req.GetString("id", ""))
	if id == "" {
		return toolErrorf(op, "id is required (the Automaton ID)"), nil
	}
	if strings.ContainsAny(id, "/?#\\ \t\r\n") {
		return toolErrorf(op, "id %q is not an Automaton ID", id), nil
	}
	raw, present := req.GetArguments()["paths"]
	if !present || raw == nil {
		return toolErrorf(op, "paths is required: an array of repository-relative paths, or an empty array to clear the list"), nil
	}
	paths, ok := authorizedPathsArg(raw)
	if !ok {
		return toolErrorf(op, "paths must be an array of strings"), nil
	}
	out, err := s.proxyJSON(http.MethodPost, "/api/autonomous/prds/"+url.PathEscape(id)+"/set_authorized_paths",
		map[string]any{"paths": paths})
	if err != nil {
		return toolError(op, err), nil
	}
	return textOK(string(out)), nil
}

// authorizedPathsArg reads the paths argument as a list of strings. It returns
// a non-nil slice (so an empty array stays [] on the wire) and false when the
// value is not an array or holds an entry that is not a string.
func authorizedPathsArg(v any) ([]string, bool) {
	switch list := v.(type) {
	case []string:
		return append([]string{}, list...), true
	case []any:
		out := make([]string, 0, len(list))
		for _, item := range list {
			s, ok := item.(string)
			if !ok {
				return nil, false
			}
			out = append(out, s)
		}
		return out, true
	}
	return nil, false
}
