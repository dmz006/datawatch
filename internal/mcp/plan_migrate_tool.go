// plan_migrate_tool.go — the plan_migrate MCP tool: a proxy for
// POST /api/plans/migrate (internal/server/plans_migrate.go).

package mcp

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

func (s *Server) toolPlanMigrate() mcpsdk.Tool {
	return mcpsdk.NewTool("plan_migrate",
		mcpsdk.WithDescription("Build plan records from the older project plan lists (arcs) and from the registry rows. Every older plan-list entry becomes a plan linked to its Automation; every registry row that names an Automaton or Automation becomes a plan linked to it; every open row that names none becomes a backlog plan, ordered by its old row number. It previews by default (apply false): it lists what it would create and link, what already exists and any conflict, and writes nothing, so run the preview first and read it. Set apply true to write. A plan or link that already exists is never changed, so running it again is safe. No arc, row or config key is deleted. Returns the counts and the lists."),
		mcpsdk.WithBoolean("apply", mcpsdk.Description("Write the plans and links. Default false: preview only")),
		mcpsdk.WithReadOnlyHintAnnotation(false),
	)
}

func (s *Server) handlePlanMigrate(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	const op = "plan_migrate"
	apply := false
	if v, present := req.GetArguments()["apply"]; present && v != nil {
		switch b := v.(type) {
		case bool:
			apply = b
		case string:
			parsed, err := strconv.ParseBool(strings.TrimSpace(b))
			if err != nil {
				return toolErrorf(op, "apply must be true or false"), nil
			}
			apply = parsed
		default:
			return toolErrorf(op, "apply must be true or false"), nil
		}
	}
	out, err := s.proxyJSON(http.MethodPost, "/api/plans/migrate", map[string]any{"apply": apply})
	if err != nil {
		return toolError(op, err), nil
	}
	return textOK(string(out)), nil
}
