package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

func (s *Server) toolAutonomousPRDSetProject() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_prd_set_project",
		mcpsdk.WithDescription("Set or clear the explicit project link of an Automaton. The project must be a configured project name; an unknown name is refused with a message listing the configured project names. An empty project clears the link, after which the Automaton is matched through its Automation, its parent or its directory. The change is saved and recorded in the Automaton's decision log. Only the Automaton record changes; no session is touched. The Automaton read view reports the effective project as project and how it was found as project_source (link, automation, parent, directory or none)."),
		mcpsdk.WithString("id", mcpsdk.Required(), mcpsdk.Description("Automaton ID")),
		mcpsdk.WithString("project", mcpsdk.Required(), mcpsdk.Description("configured project name; empty string clears the link")),
	)
}
func (s *Server) handleAutonomousPRDSetProject(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	id := req.GetString("id", "")
	body, _ := json.Marshal(map[string]string{"project": req.GetString("project", "")})
	out, err := s.proxyJSON(http.MethodPost, "/api/autonomous/prds/"+url.PathEscape(id)+"/set_project", body)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) toolAutonomousProjectLinkBackfill() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_project_link_backfill",
		mcpsdk.WithDescription("Run one project link backfill pass over the Automata that have no project link yet. Each is matched to the datawatch project through its Automation, its parent or its repository directory; Automata that already carry a link are never changed and Automata that match nothing are reported as skipped. The default is a dry run that only returns the report (entries with the project, how it was found and the reason for a skip, plus scanned, matched, applied, skipped and failed counts). Set apply=true to write the links; a second apply finds nothing to do. Only Automaton records are read and written; no session is touched."),
		mcpsdk.WithBoolean("apply", mcpsdk.Description("true: write the links; false or absent: dry run, report only (default)")),
	)
}
func (s *Server) handleAutonomousProjectLinkBackfill(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	body, _ := json.Marshal(map[string]bool{"apply": req.GetBool("apply", false)})
	out, err := s.proxyJSON(http.MethodPost, "/api/autonomous/project-link-backfill", body)
	return llmSetterResult(out, err)
}
