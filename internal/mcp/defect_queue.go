// MCP tools for the defect queue. Each proxies the matching REST endpoint in
// internal/server/defect_queue.go, so the engine rules (intake routing, batch
// split-out, tiny-fix lane) stay in one place.
package mcp

import (
	"context"
	"net/http"
	"net/url"
	"strings"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

func (s *Server) toolAutonomousDefectIntake() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_defect_intake",
		mcpsdk.WithDescription("Route one incoming defect. The decision is tiny_fix (a one-task Automaton), open_batch (added to an open draft batch of the same project and area) or new_batch. The reply carries route, reason and batch_id; when the tiny-fix lane refused a change that looked eligible, tiny_fix_refusal names the paths and the reason, and the defect went to a batch instead. Writes: it creates or extends an Automaton and appends to the intake decision log."),
		mcpsdk.WithString("id", mcpsdk.Required(), mcpsdk.Description("Defect id")),
		mcpsdk.WithString("title", mcpsdk.Required(), mcpsdk.Description("Short defect title")),
		mcpsdk.WithString("project", mcpsdk.Required(), mcpsdk.Description("Project the defect belongs to")),
		mcpsdk.WithString("description", mcpsdk.Description("Defect description")),
		mcpsdk.WithArray("files", mcpsdk.WithStringItems(), mcpsdk.Description("Repo-relative paths the fix is expected to touch")),
		mcpsdk.WithString("kind", mcpsdk.Description("Declared change kind: test_only, docs_only, script_only, rule_tweak or lint_tweak. Anything else is not a tiny fix.")),
		mcpsdk.WithString("area", mcpsdk.Description("Area used to pick an open batch")),
		mcpsdk.WithNumber("priority", mcpsdk.Description("Higher is more urgent (default 0)")),
	)
}

func (s *Server) handleAutonomousDefectIntake(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	for _, k := range []string{"id", "title", "project"} {
		if strings.TrimSpace(req.GetString(k, "")) == "" {
			return mcpsdk.NewToolResultError(k + " is required"), nil
		}
	}
	body := map[string]any{
		"id":          req.GetString("id", ""),
		"title":       req.GetString("title", ""),
		"project":     req.GetString("project", ""),
		"description": req.GetString("description", ""),
		"files":       req.GetStringSlice("files", nil),
		"kind":        req.GetString("kind", ""),
		"area":        req.GetString("area", ""),
		"priority":    int(req.GetFloat("priority", 0)),
	}
	out, err := s.proxyJSON(http.MethodPost, "/api/autonomous/defects/intake", body)
	return llmSetterResult(out, err)
}

func (s *Server) toolAutonomousDefectBatches() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_defect_batches",
		mcpsdk.WithDescription("List the open defect batches with the story-to-defect mapping of each: batch id, project, area, status, size and one {story_id, defect_id} per story. Read-only."),
		mcpsdk.WithString("project", mcpsdk.Description("Only batches of this project; empty = all")),
	)
}

func (s *Server) handleAutonomousDefectBatches(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	q := url.Values{}
	if p := strings.TrimSpace(req.GetString("project", "")); p != "" {
		q.Set("project", p)
	}
	out, err := s.proxyGet("/api/autonomous/defects/batches", q)
	if err != nil {
		return mcpsdk.NewToolResultError(err.Error()), nil
	}
	return textOK(string(out)), nil
}

func (s *Server) toolAutonomousDefectBatchSplit() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_defect_batch_split",
		mcpsdk.WithDescription("Split one story out of a defect batch into its own new Automaton (for example after that story failed). The new Automaton holds that story with its own gate, docs and changelog tasks; the batch keeps its other stories. Returns the new automaton_id. Writes."),
		mcpsdk.WithString("batch_id", mcpsdk.Required(), mcpsdk.Description("Defect batch id")),
		mcpsdk.WithString("story_id", mcpsdk.Required(), mcpsdk.Description("Story to split out")),
	)
}

func (s *Server) handleAutonomousDefectBatchSplit(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	batch := strings.TrimSpace(req.GetString("batch_id", ""))
	story := strings.TrimSpace(req.GetString("story_id", ""))
	if batch == "" || story == "" {
		return mcpsdk.NewToolResultError("batch_id and story_id are required"), nil
	}
	out, err := s.proxyJSON(http.MethodPost, "/api/autonomous/defects/batches/"+url.PathEscape(batch)+"/split",
		map[string]any{"story_id": story})
	return llmSetterResult(out, err)
}

func (s *Server) toolAutonomousDefectTinyFixCheck() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_defect_tinyfix_check",
		mcpsdk.WithDescription("Check whether a change qualifies for the tiny-fix lane, without creating anything. Returns eligible and a reason; a change that touches operator-visible or out-of-lane paths is refused with the paths and the reason named. Read-only."),
		mcpsdk.WithArray("files", mcpsdk.Required(), mcpsdk.WithStringItems(), mcpsdk.Description("Repo-relative paths the change touches")),
		mcpsdk.WithString("kind", mcpsdk.Required(), mcpsdk.Description("Declared change kind: test_only, docs_only, script_only, rule_tweak or lint_tweak")),
		mcpsdk.WithString("project", mcpsdk.Description("Project whose settings apply; empty = block-level settings")),
	)
}

func (s *Server) handleAutonomousDefectTinyFixCheck(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	files := req.GetStringSlice("files", nil)
	if len(files) == 0 {
		return mcpsdk.NewToolResultError("files is required (at least one path)"), nil
	}
	kind := strings.TrimSpace(req.GetString("kind", ""))
	if kind == "" {
		return mcpsdk.NewToolResultError("kind is required"), nil
	}
	out, err := s.proxyJSON(http.MethodPost, "/api/autonomous/defects/tiny-fix/check", map[string]any{
		"files": files, "kind": kind, "project": req.GetString("project", ""),
	})
	return llmSetterResult(out, err)
}
