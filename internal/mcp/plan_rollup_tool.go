// plan_rollup_tool.go — the plan_rollup MCP tool: a proxy for the plan status
// roll-up routes (internal/server/plans_rollup_api.go): GET /api/plans/rollup
// for a preview and POST /api/plans/rollup to apply.

package mcp

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

func (s *Server) toolPlanRollup() mcpsdk.Tool {
	return mcpsdk.NewTool("plan_rollup",
		mcpsdk.WithDescription("Roll plan statuses up from the work linked to them. A plan's status is derived from the states last reported for its linked Automata and Automations: no linked work leaves it alone; any failed or blocked item makes it blocked; any running item keeps it open; all items shipped makes it shipped; all items finished makes it done; anything else keeps it open. A plan whose status was set by hand is never changed: it is listed with set_by_hand_kept true and the status the roll-up would have written. Pass dry_run true to see what would change and write nothing; without it the changes are written. Each change names the plan (title first, then id), the status it had, the status it gets and the reason. Optionally limit it to one project; without a project every project is rolled up. Returns the same object as GET (dry run) or POST /api/plans/rollup."),
		mcpsdk.WithString("project", mcpsdk.Description("Only roll up the plans of this registered project alias; default every project")),
		mcpsdk.WithBoolean("dry_run", mcpsdk.Description("Only report what would change; write nothing. Default false: write the changes")),
		mcpsdk.WithReadOnlyHintAnnotation(false),
	)
}

func (s *Server) handlePlanRollup(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	const op = "plan_rollup"
	dry := false
	if v, present := req.GetArguments()["dry_run"]; present && v != nil {
		switch b := v.(type) {
		case bool:
			dry = b
		case string:
			parsed, err := strconv.ParseBool(strings.TrimSpace(b))
			if err != nil {
				return toolErrorf(op, "dry_run must be true or false"), nil
			}
			dry = parsed
		default:
			return toolErrorf(op, "dry_run must be true or false"), nil
		}
	}
	project := strings.TrimSpace(req.GetString("project", ""))
	var out []byte
	var err error
	if dry {
		q := url.Values{"dry_run": {"true"}}
		if project != "" {
			q.Set("project", project)
		}
		out, err = s.proxyGet(plansBase+"/rollup", q)
	} else {
		body := map[string]any{"dry_run": false}
		if project != "" {
			body["project"] = project
		}
		out, err = s.proxyJSON(http.MethodPost, plansBase+"/rollup", body)
	}
	if err != nil {
		return toolError(op, err), nil
	}
	return textOK(string(out)), nil
}
