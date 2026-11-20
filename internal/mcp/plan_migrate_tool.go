// plan_migrate_tool.go — the plan_migrate MCP tool: a proxy for
// POST /api/plans/migrate (internal/server/plans_migrate.go). It has two modes:
// the arcs migration (the default, plans from the older plan lists and
// registry rows) and the release migration (mode release, Automata filed under
// release plans). Both preview by default; the release migration writes only
// with the preview id of the preview that was read.

package mcp

import (
	"context"
	"net/http"
	"strings"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

func (s *Server) toolPlanMigrate() mcpsdk.Tool {
	return mcpsdk.NewTool("plan_migrate",
		mcpsdk.WithDescription("Build plan records. Mode arcs (the default) builds plans from the older project plan lists (arcs) and from the registry rows. Every older plan-list entry becomes a plan linked to its Automation; every registry row that names an Automaton or Automation becomes a plan linked to it; every open row that names none becomes a backlog plan, ordered by its old row number. A plan or link that already exists is never changed. A plan made from a row takes the row's real title (status words dropped, one line, at most 100 characters), else the title of the Automaton or Automation it names, else nothing: a row with no derivable name (never taken from the status column) is listed as needs_review (label 'needs review') and is not created on apply; the counts include titles_derived, titles_from_linked, titles_fell_back and needs_review. No arc, row or config key is deleted. Mode release files a project's Automata under release plans and themes by the migration's own rules: it creates the current and next release plans, attaches each Automaton to the release it belongs to and places it in a theme; an Automaton already held by another plan is reported as a conflict and left alone. Mode release needs project; current_release and next_release name the two release labels (optional), and marks overrides the filing of one Automaton, as a JSON object of Automaton id to {release, theme}. Both modes preview by default (apply false): they list what they would do and write nothing, so run the preview first and read it. Set apply true to write. The release migration also needs the preview_id the preview returned and refuses to write without it, or with a stale one (the plans changed since the preview). Returns the counts and the lists. See docs/howto/plan-records.md (Release plans and themes)."),
		mcpsdk.WithString("mode", mcpsdk.Enum("arcs", "release"), mcpsdk.Description("arcs (the default) or release")),
		mcpsdk.WithBoolean("apply", mcpsdk.Description("Write the plans and links. Default false: preview only")),
		mcpsdk.WithString("project", mcpsdk.Description("Mode release: a registered project alias (required)")),
		mcpsdk.WithString("current_release", mcpsdk.Description("Mode release: the current release label (optional)")),
		mcpsdk.WithString("next_release", mcpsdk.Description("Mode release: the next release label (optional)")),
		mcpsdk.WithString("marks", mcpsdk.Description("Mode release: a JSON object of Automaton id to {\"release\",\"theme\"} that overrides where one Automaton is filed (optional)")),
		mcpsdk.WithString("preview_id", mcpsdk.Description("Mode release: the preview_id of the preview you read; required when apply is true")),
		mcpsdk.WithReadOnlyHintAnnotation(false),
	)
}

func (s *Server) handlePlanMigrate(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	const op = "plan_migrate"
	apply, bad := planBoolArg(op, req, "apply", false)
	if bad != nil {
		return bad, nil
	}
	mode := strings.ToLower(strings.TrimSpace(req.GetString("mode", "")))
	var body map[string]any
	switch mode {
	case "", "arcs":
		if f := planMigrateReleaseFieldsGiven(req); f != "" {
			return toolErrorf(op, "%s belongs to mode release: pass mode release, or leave it out for the arcs migration", f), nil
		}
		body = map[string]any{"apply": apply}
	case "release":
		body, bad = planReleaseMigrateBody(op, req, apply)
		if bad != nil {
			return bad, nil
		}
	default:
		return toolErrorf(op, "mode must be arcs (the default) or release"), nil
	}
	out, err := s.proxyJSON(http.MethodPost, "/api/plans/migrate", body)
	if err != nil {
		return toolError(op, err), nil
	}
	return textOK(string(out)), nil
}
