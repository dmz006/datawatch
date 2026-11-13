// MCP tools for the finished-Automaton archive. Both proxy the daemon REST
// routes on the loopback:
//
//	GET  /api/autonomous/prds/{id}/archive → autonomous_prd_archive (read on ask)
//	POST /api/autonomous/archive           → autonomous_archive (dry run by default)
package mcp

import (
	"context"
	"net/http"
	"net/url"
	"strings"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

func (s *Server) toolAutonomousPRDArchive() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_prd_archive",
		mcpsdk.WithDescription("Read the archive of one finished Automaton (GET /api/autonomous/prds/{id}/archive): the decision log, every task's session history and the raw judge text that were moved out of the live record. Read-only, and it reads only that Automaton's archive file. An Automaton that is not archived answers not found."),
		mcpsdk.WithString("id", mcpsdk.Required(), mcpsdk.Description("Automaton id")),
	)
}

func (s *Server) handleAutonomousPRDArchive(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	const tool = "autonomous_prd_archive"
	id := strings.TrimSpace(req.GetString("id", ""))
	if id == "" {
		return toolErrorf(tool, "id is required"), nil
	}
	out, err := s.proxyGet("/api/autonomous/prds/"+url.PathEscape(id)+"/archive", nil)
	if err != nil {
		return toolError(tool, err), nil
	}
	return textOK(string(out)), nil
}

func (s *Server) toolAutonomousArchive() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_archive",
		mcpsdk.WithDescription("Archive finished Automata (POST /api/autonomous/archive). A finished Automaton is completed, shipped or cancelled and older than autonomous.archive.retention_days. The default is a dry run: it reports which Automata would be archived and the bytes before and after, and writes nothing. With dry_run=false it first takes a verified backup of the touched records (size and SHA-256 of every file), stops without changing anything if a copy does not verify, then archives one Automaton at a time and verifies each archive before rewriting its live record. An Automaton with a running task, an open session or an unmerged branch it still needs is skipped, never forced. No data is deleted, and a second run archives nothing more."),
		mcpsdk.WithBoolean("dry_run", mcpsdk.Description("true or absent: report only, write nothing (default). false: take the verified backup and archive.")),
	)
}

func (s *Server) handleAutonomousArchive(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	body := map[string]any{"dry_run": req.GetBool("dry_run", true)}
	out, err := s.proxyJSON(http.MethodPost, "/api/autonomous/archive", body)
	return llmSetterResult(out, err)
}
