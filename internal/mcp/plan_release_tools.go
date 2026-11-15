// plan_release_tools.go — the MCP tools for release plans. A release plan is a
// plan that stands for one release; its themes group the Automata it holds.
// Every tool proxies a REST route of the daemon (internal/server/
// plans_release_api.go), so MCP runs the same code as REST, the CLI and the
// chat channel and the answers cannot drift:
//
//	plan_theme_list          GET  /api/plans/{ref}/themes
//	plan_theme_set           POST /api/plans/{ref}/themes
//	plan_archive             POST /api/plans/{ref}/archive
//	plan_restore             POST /api/plans/{ref}/restore
//	plan_unattributed_list   GET  /api/plans/unattributed
//	plan_attach_bulk         POST /api/plans/{ref}/attach (the "ids" form)
//	plan_row_plan_list       GET  /api/plans/migrate/row-plans
//	plan_row_plan_convert    POST /api/plans/migrate/row-plans
//
// plan_create (plan_kind and release) and plan_migrate (mode release) carry
// the release forms of the two older tools.
//
// A write that moves many things at once, the release migration and the
// row-plan conversion, previews by default and writes only when it is given
// the preview id of the preview the caller read. The tools refuse a write
// without one before the call reaches the daemon.

package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

// planReleaseDocs is the how-to every release plan tool description points at.
const planReleaseDocs = "See docs/howto/plan-records.md (Release plans and themes)."

// PlanReleaseToolNames lists the release plan tools, for the parity tests.
func PlanReleaseToolNames() []string {
	return []string{
		"plan_theme_list",
		"plan_theme_set",
		"plan_archive",
		"plan_restore",
		"plan_unattributed_list",
		"plan_attach_bulk",
		"plan_row_plan_list",
		"plan_row_plan_convert",
	}
}

// planBoolArg reads an optional true/false argument given as a boolean or as
// the text true or false. A missing argument is def.
func planBoolArg(op string, req mcpsdk.CallToolRequest, key string, def bool) (bool, *mcpsdk.CallToolResult) {
	v, present := req.GetArguments()[key]
	if !present || v == nil {
		return def, nil
	}
	switch b := v.(type) {
	case bool:
		return b, nil
	case string:
		parsed, err := strconv.ParseBool(strings.TrimSpace(b))
		if err != nil {
			return false, toolErrorf(op, "%s must be true or false", key)
		}
		return parsed, nil
	}
	return false, toolErrorf(op, "%s must be true or false", key)
}

// planOptString reads an optional text argument, trimmed. A value that is not
// text is an error.
func planOptString(op string, req mcpsdk.CallToolRequest, key string) (string, *mcpsdk.CallToolResult) {
	v, present := req.GetArguments()[key]
	if !present || v == nil {
		return "", nil
	}
	t, ok := v.(string)
	if !ok {
		return "", toolErrorf(op, "%s must be text", key)
	}
	return strings.TrimSpace(t), nil
}

// planItemKind reads the kind of a plan item: an Automaton or an Automation.
// An empty kind is deflt when deflt is not empty.
func planItemKind(op string, req mcpsdk.CallToolRequest, deflt string) (string, *mcpsdk.CallToolResult) {
	kind := strings.ToLower(strings.TrimSpace(req.GetString("kind", "")))
	if kind == "" {
		kind = deflt
	}
	switch kind {
	case "automaton", "automation":
		return kind, nil
	}
	return "", toolErrorf(op, "kind must be automaton or automation")
}

// ----- plan_theme_list ------------------------------------------------------

func (s *Server) toolPlanThemeList() mcpsdk.Tool {
	return mcpsdk.NewTool("plan_theme_list",
		mcpsdk.WithDescription("List the themes of a release plan, in order. Each theme shows its Automata first and then its Automations, each in the order it will run; after the themes come the items the plan holds that no theme has placed yet (unplaced). Only a release plan has themes: any other plan is refused. Read-only. Returns the same object as GET /api/plans/{ref}/themes. "+planReleaseDocs),
		mcpsdk.WithString("plan", mcpsdk.Required(), mcpsdk.Description("A release plan id or title")),
		mcpsdk.WithReadOnlyHintAnnotation(true),
	)
}

func (s *Server) handlePlanThemeList(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	const op = "plan_theme_list"
	plan, bad := planRefArg(op, req)
	if bad != nil {
		return bad, nil
	}
	out, err := s.proxyGet(plansBase+"/"+url.PathEscape(plan)+"/themes", nil)
	if err != nil {
		return toolError(op, err), nil
	}
	return textOK(string(out)), nil
}

// ----- plan_theme_set -------------------------------------------------------

func (s *Server) toolPlanThemeSet() mcpsdk.Tool {
	return mcpsdk.NewTool("plan_theme_set",
		mcpsdk.WithDescription("Change the themes of a release plan with one action per call. create (name) adds an empty theme. rename (theme, name) renames one. delete (theme) removes a theme; its items stay in the plan, unplaced. place (theme, kind, id) puts an Automaton or Automation the plan already holds into a theme and moves it out of any other. unplace (kind, id) takes an item out of its theme. reorder (themes) sets the theme order: list every theme of the plan once. reorder_items (theme, kind, items) sets the order of one kind of item inside a theme: list every item of that kind the theme holds once. A theme is named by its id or its name. A theme never adds an item to the plan, and a plan that is not a release plan is refused. Returns the plan's themes after the change. "+planReleaseDocs),
		mcpsdk.WithString("plan", mcpsdk.Required(), mcpsdk.Description("A release plan id or title")),
		mcpsdk.WithString("action", mcpsdk.Required(), mcpsdk.Enum("create", "rename", "delete", "place", "unplace", "reorder", "reorder_items"), mcpsdk.Description("What to do")),
		mcpsdk.WithString("theme", mcpsdk.Description("The theme, by id or name (rename, delete, place, reorder_items)")),
		mcpsdk.WithString("name", mcpsdk.Description("The theme name (create), or the new name (rename)")),
		mcpsdk.WithString("kind", mcpsdk.Enum("automaton", "automation"), mcpsdk.Description("automaton or automation (place, unplace, reorder_items)")),
		mcpsdk.WithString("id", mcpsdk.Description("The Automaton or Automation id (place, unplace)")),
		mcpsdk.WithArray("themes", mcpsdk.Description("Every theme of the plan, first theme first (reorder)"), mcpsdk.Items(map[string]any{"type": "string"})),
		mcpsdk.WithArray("items", mcpsdk.Description("Every item of that kind the theme holds, first to run first (reorder_items)"), mcpsdk.Items(map[string]any{"type": "string"})),
		mcpsdk.WithReadOnlyHintAnnotation(false),
	)
}

func (s *Server) handlePlanThemeSet(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	const op = "plan_theme_set"
	plan, bad := planRefArg(op, req)
	if bad != nil {
		return bad, nil
	}
	action := strings.ToLower(strings.TrimSpace(req.GetString("action", "")))
	theme := strings.TrimSpace(req.GetString("theme", ""))
	name := strings.TrimSpace(req.GetString("name", ""))
	id := strings.TrimSpace(req.GetString("id", ""))
	need := func(field, v string) *mcpsdk.CallToolResult {
		if v == "" {
			return toolErrorf(op, "%s is required for action %s", field, action)
		}
		return nil
	}
	body := map[string]any{"action": action}
	switch action {
	case "create":
		if bad := need("name", name); bad != nil {
			return bad, nil
		}
		body["name"] = name
	case "rename":
		if bad := need("theme", theme); bad != nil {
			return bad, nil
		}
		if bad := need("name", name); bad != nil {
			return bad, nil
		}
		body["theme"], body["name"] = theme, name
	case "delete":
		if bad := need("theme", theme); bad != nil {
			return bad, nil
		}
		body["theme"] = theme
	case "place", "unplace":
		kind, bad := planItemKind(op, req, "")
		if bad != nil {
			return bad, nil
		}
		if bad := need("id", id); bad != nil {
			return bad, nil
		}
		if action == "place" {
			if bad := need("theme", theme); bad != nil {
				return bad, nil
			}
			body["theme"] = theme
		}
		body["kind"], body["id"] = kind, id
	case "reorder":
		themes, bad := planStringList(op, req, "themes")
		if bad != nil {
			return bad, nil
		}
		body["themes"] = themes
	case "reorder_items":
		kind, bad := planItemKind(op, req, "")
		if bad != nil {
			return bad, nil
		}
		if bad := need("theme", theme); bad != nil {
			return bad, nil
		}
		items, bad := planStringList(op, req, "items")
		if bad != nil {
			return bad, nil
		}
		body["theme"], body["kind"], body["items"] = theme, kind, items
	default:
		return toolErrorf(op, "action must be create, rename, delete, place, unplace, reorder or reorder_items"), nil
	}
	out, err := s.proxyJSON(http.MethodPost, plansBase+"/"+url.PathEscape(plan)+"/themes", body)
	if err != nil {
		return toolError(op, err), nil
	}
	return textOK(string(out)), nil
}

// ----- plan_archive and plan_restore ---------------------------------------

func (s *Server) toolPlanArchive() mcpsdk.Tool {
	return mcpsdk.NewTool("plan_archive",
		mcpsdk.WithDescription("Archive a plan. Archiving only marks the plan: it leaves the default plan lists, the backlog, search and the project counts, but its file, links, Automata, Automations, themes and findings all stay, and nothing is deleted. Archiving an archived plan changes nothing. Name a protected plan (usually the current release plan) and the call is refused while that plan holds Automata. Undo it with plan_restore. Returns the plan, the number of Automata it holds and whether anything changed. "+planReleaseDocs),
		mcpsdk.WithString("plan", mcpsdk.Required(), mcpsdk.Description("A plan id or title")),
		mcpsdk.WithString("reason", mcpsdk.Description("Why it is archived, at most 1000 characters (optional)")),
		mcpsdk.WithString("protected_plan", mcpsdk.Description("A plan that must not be archived while it holds Automata, usually the current release plan (optional)")),
		mcpsdk.WithReadOnlyHintAnnotation(false),
	)
}

func (s *Server) handlePlanArchive(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	const op = "plan_archive"
	plan, bad := planRefArg(op, req)
	if bad != nil {
		return bad, nil
	}
	body := map[string]any{}
	for _, f := range []string{"reason", "protected_plan"} {
		v, bad := planOptString(op, req, f)
		if bad != nil {
			return bad, nil
		}
		if v != "" {
			body[f] = v
		}
	}
	out, err := s.proxyJSON(http.MethodPost, plansBase+"/"+url.PathEscape(plan)+"/archive", body)
	if err != nil {
		return toolError(op, err), nil
	}
	return textOK(string(out)), nil
}

func (s *Server) toolPlanRestore() mcpsdk.Tool {
	return mcpsdk.NewTool("plan_restore",
		mcpsdk.WithDescription("Restore an archived plan: clear the archive mark. The plan returns to the place its order gave it. Restoring a plan that is not archived changes nothing. Returns the plan and whether anything changed. "+planReleaseDocs),
		mcpsdk.WithString("plan", mcpsdk.Required(), mcpsdk.Description("A plan id or title")),
		mcpsdk.WithReadOnlyHintAnnotation(false),
	)
}

func (s *Server) handlePlanRestore(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	const op = "plan_restore"
	plan, bad := planRefArg(op, req)
	if bad != nil {
		return bad, nil
	}
	out, err := s.proxyJSON(http.MethodPost, plansBase+"/"+url.PathEscape(plan)+"/restore", map[string]any{})
	if err != nil {
		return toolError(op, err), nil
	}
	return textOK(string(out)), nil
}

// ----- plan_unattributed_list ----------------------------------------------

func (s *Server) toolPlanUnattributedList() mcpsdk.Tool {
	return mcpsdk.NewTool("plan_unattributed_list",
		mcpsdk.WithDescription("List the Automata that no release plan holds, with the release and theme the release migration's own rules would file each under. An Automaton held by an ordinary plan shows that plan (holding_plan): it cannot join a release plan until it is detached from it. Read-only: it files nothing; use plan_attach_bulk or plan_migrate with mode release to file them. Returns the same object as GET /api/plans/unattributed. "+planReleaseDocs),
		mcpsdk.WithString("project", mcpsdk.Description("Only Automata of this registered project alias")),
		mcpsdk.WithString("current_release", mcpsdk.Description("The current release label, to suggest filings against (optional)")),
		mcpsdk.WithString("next_release", mcpsdk.Description("The next release label (optional)")),
		mcpsdk.WithNumber("limit", mcpsdk.Description("At most this many Automata per page")),
		mcpsdk.WithNumber("offset", mcpsdk.Description("Skip this many Automata, for paging")),
		mcpsdk.WithReadOnlyHintAnnotation(true),
	)
}

func (s *Server) handlePlanUnattributedList(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	const op = "plan_unattributed_list"
	q := url.Values{}
	for _, f := range []string{"project", "current_release", "next_release"} {
		v, bad := planOptString(op, req, f)
		if bad != nil {
			return bad, nil
		}
		if v != "" {
			q.Set(f, v)
		}
	}
	for _, f := range []string{"limit", "offset"} {
		n, ok := getNumberArg(req, f)
		if !ok {
			continue
		}
		if n != float64(int(n)) || n < 0 || (f == "limit" && n == 0) {
			return toolErrorf(op, "%s must be a positive whole number", f), nil
		}
		q.Set(f, strconv.Itoa(int(n)))
	}
	out, err := s.proxyGet(plansBase+"/unattributed", q)
	if err != nil {
		return toolError(op, err), nil
	}
	return textOK(string(out)), nil
}

// ----- plan_attach_bulk -----------------------------------------------------

func (s *Server) toolPlanAttachBulk() mcpsdk.Tool {
	return mcpsdk.NewTool("plan_attach_bulk",
		mcpsdk.WithDescription("Attach a list of Automata (or Automations) to a plan in one call, optionally placing each in a theme of a release plan. Each id gets its own result line: attached, already_attached, refused (the item is linked to a different plan; the holder is named and nothing changes, detach it first), not_found, placement_failed or failed. A refused or unknown id never stops the others. The theme must already exist in the plan. Up to 500 ids. Returns the per-id results and the counts. "+planReleaseDocs),
		mcpsdk.WithString("plan", mcpsdk.Required(), mcpsdk.Description("A plan id or title")),
		mcpsdk.WithArray("ids", mcpsdk.Required(), mcpsdk.Description("The Automaton (or Automation) ids to attach"), mcpsdk.Items(map[string]any{"type": "string"})),
		mcpsdk.WithString("kind", mcpsdk.Enum("automaton", "automation"), mcpsdk.Description("automaton (the default) or automation")),
		mcpsdk.WithString("theme", mcpsdk.Description("A theme of the release plan, by id or name, to place every attached item in (optional)")),
		mcpsdk.WithReadOnlyHintAnnotation(false),
	)
}

func (s *Server) handlePlanAttachBulk(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	const op = "plan_attach_bulk"
	plan, bad := planRefArg(op, req)
	if bad != nil {
		return bad, nil
	}
	kind, bad := planItemKind(op, req, "automaton")
	if bad != nil {
		return bad, nil
	}
	ids, bad := planStringList(op, req, "ids")
	if bad != nil {
		return bad, nil
	}
	theme, bad := planOptString(op, req, "theme")
	if bad != nil {
		return bad, nil
	}
	body := map[string]any{"kind": kind, "ids": ids}
	if theme != "" {
		body["theme"] = theme
	}
	out, err := s.proxyJSON(http.MethodPost, plansBase+"/"+url.PathEscape(plan)+"/attach", body)
	if err != nil {
		return toolError(op, err), nil
	}
	return textOK(string(out)), nil
}

// ----- plan_row_plan_list and plan_row_plan_convert ------------------------

func (s *Server) toolPlanRowPlanList() mcpsdk.Tool {
	return mcpsdk.NewTool("plan_row_plan_list",
		mcpsdk.WithDescription("List the plans the plans migration made from registry rows: the plans that only stand for one row. They are what plan_row_plan_convert turns into items of release plans. Read-only. Returns the same object as GET /api/plans/migrate/row-plans. "+planReleaseDocs),
		mcpsdk.WithString("project", mcpsdk.Description("Only row-plans of this registered project alias")),
		mcpsdk.WithReadOnlyHintAnnotation(true),
	)
}

func (s *Server) handlePlanRowPlanList(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	const op = "plan_row_plan_list"
	q := url.Values{}
	v, bad := planOptString(op, req, "project")
	if bad != nil {
		return bad, nil
	}
	if v != "" {
		q.Set("project", v)
	}
	out, err := s.proxyGet(plansBase+"/migrate/row-plans", q)
	if err != nil {
		return toolError(op, err), nil
	}
	return textOK(string(out)), nil
}

func (s *Server) toolPlanRowPlanConvert() mcpsdk.Tool {
	return mcpsdk.NewTool("plan_row_plan_convert",
		mcpsdk.WithDescription("Convert the plans made from registry rows into items of release plans. It previews by default (apply false): it lists what it would convert, what is waiting and any conflict, writes nothing, and returns a preview_id. Read the preview, then call again with apply true and that preview_id to write exactly what you read. A write without the preview_id is refused, and so is a stale one (the plans changed since the preview): preview again. Each converted plan is archived, not deleted, so running it again is safe. "+planReleaseDocs),
		mcpsdk.WithString("project", mcpsdk.Description("Only plans of this registered project alias")),
		mcpsdk.WithString("current_release", mcpsdk.Description("The current release label (optional)")),
		mcpsdk.WithBoolean("apply", mcpsdk.Description("Write the conversion. Default false: preview only. Needs preview_id")),
		mcpsdk.WithString("preview_id", mcpsdk.Description("The preview_id of the preview you read; required when apply is true")),
		mcpsdk.WithReadOnlyHintAnnotation(false),
	)
}

func (s *Server) handlePlanRowPlanConvert(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	const op = "plan_row_plan_convert"
	apply, bad := planBoolArg(op, req, "apply", false)
	if bad != nil {
		return bad, nil
	}
	body := map[string]any{"apply": apply}
	for _, f := range []string{"project", "current_release", "preview_id"} {
		v, bad := planOptString(op, req, f)
		if bad != nil {
			return bad, nil
		}
		if v != "" {
			body[f] = v
		}
	}
	if bad := refuseApplyWithoutPreview(op, apply, body); bad != nil {
		return bad, nil
	}
	out, err := s.proxyJSON(http.MethodPost, plansBase+"/migrate/row-plans", body)
	if err != nil {
		return toolError(op, err), nil
	}
	return textOK(string(out)), nil
}

// refuseApplyWithoutPreview refuses a write that carries no preview id. A
// preview (apply false) needs none; a preview id sent with a preview is
// harmless and left to the daemon.
func refuseApplyWithoutPreview(op string, apply bool, body map[string]any) *mcpsdk.CallToolResult {
	if !apply {
		return nil
	}
	if id, _ := body["preview_id"].(string); id == "" {
		return toolErrorf(op, "apply needs the preview_id of the preview you read: call without apply first, read the preview, then call again with apply true and its preview_id")
	}
	return nil
}

// ----- the release forms of plan_create and plan_migrate --------------------

// planReleaseMigrateBody builds the body of the release migration from the
// plan_migrate arguments. The caller has checked mode is release.
func planReleaseMigrateBody(op string, req mcpsdk.CallToolRequest, apply bool) (map[string]any, *mcpsdk.CallToolResult) {
	body := map[string]any{"mode": "release", "apply": apply}
	for _, f := range []string{"project", "current_release", "next_release", "preview_id"} {
		v, bad := planOptString(op, req, f)
		if bad != nil {
			return nil, bad
		}
		if v != "" {
			body[f] = v
		}
	}
	if _, ok := body["project"]; !ok {
		return nil, toolErrorf(op, "project is required for mode release: name a registered project alias")
	}
	switch m := req.GetArguments()["marks"].(type) {
	case nil:
	case map[string]any:
		if len(m) > 0 {
			body["marks"] = m
		}
	case string:
		if t := strings.TrimSpace(m); t != "" {
			var parsed map[string]any
			if err := json.Unmarshal([]byte(t), &parsed); err != nil {
				return nil, toolErrorf(op, "marks must be a JSON object: %v", err)
			}
			body["marks"] = parsed
		}
	default:
		return nil, toolErrorf(op, "marks must be a JSON object")
	}
	if bad := refuseApplyWithoutPreview(op, apply, body); bad != nil {
		return nil, bad
	}
	return body, nil
}

// planMigrateReleaseFieldsGiven names a release-only argument the caller sent,
// so the older arcs migration can refuse it instead of dropping it.
func planMigrateReleaseFieldsGiven(req mcpsdk.CallToolRequest) string {
	args := req.GetArguments()
	for _, f := range []string{"project", "current_release", "next_release", "preview_id", "marks"} {
		if v, ok := args[f]; ok && v != nil {
			if t, isText := v.(string); isText && strings.TrimSpace(t) == "" {
				continue
			}
			return f
		}
	}
	return ""
}

// planCreateReleaseFields adds the release kind to a plan_create body: the
// plan_kind and the release label. A release plan needs its label.
func planCreateReleaseFields(op string, req mcpsdk.CallToolRequest, body map[string]any) *mcpsdk.CallToolResult {
	kind := strings.ToLower(strings.TrimSpace(req.GetString("plan_kind", "")))
	release := strings.TrimSpace(req.GetString("release", ""))
	switch kind {
	case "", "ordinary":
		if release != "" {
			return toolErrorf(op, "release is only for a release plan: pass plan_kind release")
		}
		if kind == "ordinary" {
			body["plan_kind"] = "ordinary"
		}
	case "release":
		if release == "" {
			return toolErrorf(op, "release is required for a release plan: the release label, for example the version it stands for")
		}
		body["plan_kind"], body["release"] = "release", release
	default:
		return toolErrorf(op, "plan_kind must be ordinary or release (got %q)", kind)
	}
	return nil
}
