// plan_record_tools.go — MCP tools that proxy the plan-record REST routes of
// the daemon (internal/server/plan_records.go). A plan record is a registry
// row (a numbered item of the plans registry) or a finding. The tools show the
// title first and the id second because the daemon's answers do.
//
// A reference (the ref argument) is an id, a uid, "row <n>", an exact title,
// an older title or an unambiguous title prefix. A title that matches more
// than one record is answered with the list of matches so the caller can say
// which one.
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

// planRecordsBase is the REST collection the tools proxy.
const planRecordsBase = "/api/plan-records"

// planRecordKindPath maps the kind argument to its REST collection segment.
// It accepts the CLI spelling (registry-row) and the stored spelling
// (registry_row).
func planRecordKindPath(kind string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "registry-row", "registry_row", "registry-rows":
		return "registry-rows", true
	case "finding", "findings":
		return "findings", true
	}
	return "", false
}

// planRecordKindArg reads and checks the kind argument. An empty kind is
// allowed only when optional is true.
func planRecordKindArg(op string, req mcpsdk.CallToolRequest, optional bool) (string, *mcpsdk.CallToolResult) {
	raw := strings.TrimSpace(req.GetString("kind", ""))
	if raw == "" {
		if optional {
			return "", nil
		}
		return "", toolErrorf(op, "kind is required: registry-row or finding")
	}
	p, ok := planRecordKindPath(raw)
	if !ok {
		return "", toolErrorf(op, "kind %q is not valid: use registry-row or finding", raw)
	}
	return p, nil
}

// planRecordFilterQuery builds the list and search query from the shared
// filter arguments.
func planRecordFilterQuery(op string, req mcpsdk.CallToolRequest) (url.Values, *mcpsdk.CallToolResult) {
	q := url.Values{}
	for _, f := range []string{"project", "state", "automaton"} {
		if v := strings.TrimSpace(req.GetString(f, "")); v != "" {
			q.Set(f, v)
		}
	}
	for _, f := range []string{"limit", "offset", "legacy_number"} {
		n, ok := getNumberArg(req, f)
		if !ok {
			continue
		}
		if n != float64(int(n)) || n < 0 || (f != "offset" && n == 0) {
			return nil, toolErrorf(op, "%s must be a positive whole number", f)
		}
		q.Set(f, strconv.Itoa(int(n)))
	}
	return q, nil
}

// ----- plan_record_list ----------------------------------------------------

func (s *Server) toolPlanRecordList() mcpsdk.Tool {
	return mcpsdk.NewTool("plan_record_list",
		mcpsdk.WithDescription("List plan records of one kind, most recently updated first: registry rows (the numbered items of the plans registry) or findings. Each record shows its title first and its id second, with its project, state, legacy row number and review flag. Filter by project, state or the Automaton that shipped or raised it; pass q to narrow by words. Read-only: it never creates or changes a record. Returns the same object as GET /api/plan-records/registry-rows or /findings."),
		mcpsdk.WithString("kind", mcpsdk.Required(), mcpsdk.Description("registry-row or finding")),
		mcpsdk.WithString("project", mcpsdk.Description("Only records of this registered project alias")),
		mcpsdk.WithString("state", mcpsdk.Description("Only records in this state. Registry rows: open, planned, in_progress, blocked, shipped, done, deferred, cancelled. Findings: open, triaged, fixed, wontfix, duplicate")),
		mcpsdk.WithString("automaton", mcpsdk.Description("Only records that this Automaton shipped or raised")),
		mcpsdk.WithString("q", mcpsdk.Description("Only records that match these words")),
		mcpsdk.WithNumber("legacy_number", mcpsdk.Description("Only the registry row with this old row number")),
		mcpsdk.WithNumber("limit", mcpsdk.Description("At most this many records; default 100, maximum 1000")),
		mcpsdk.WithNumber("offset", mcpsdk.Description("Skip this many records, for paging")),
		mcpsdk.WithReadOnlyHintAnnotation(true),
	)
}

func (s *Server) handlePlanRecordList(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	const op = "plan_record_list"
	kind, bad := planRecordKindArg(op, req, false)
	if bad != nil {
		return bad, nil
	}
	q, bad := planRecordFilterQuery(op, req)
	if bad != nil {
		return bad, nil
	}
	if v := strings.TrimSpace(req.GetString("q", "")); v != "" {
		q.Set("q", v)
	}
	out, err := s.proxyGet(planRecordsBase+"/"+kind, q)
	if err != nil {
		return toolError(op, err), nil
	}
	return textOK(string(out)), nil
}

// ----- plan_record_get -----------------------------------------------------

func (s *Server) toolPlanRecordGet() mcpsdk.Tool {
	return mcpsdk.NewTool("plan_record_get",
		mcpsdk.WithDescription("Read one plan record: title first, then id, uid, project, state, body, the old row number, where an imported record came from, the Automata that shipped or raised it, links, older titles and the review flag. The ref is an id, a uid, \"row <n>\" (the old row number), an exact title, an older title or an unambiguous title prefix. A title that matches more than one record is refused with the list of matches; say which one by its id. Read-only. Returns the same object as GET /api/plan-records/registry-rows/{ref} or /findings/{ref}."),
		mcpsdk.WithString("kind", mcpsdk.Required(), mcpsdk.Description("registry-row or finding")),
		mcpsdk.WithString("ref", mcpsdk.Required(), mcpsdk.Description("The record: id, uid, \"row <n>\", title, older title or title prefix")),
		mcpsdk.WithString("project", mcpsdk.Description("Narrow a title reference to this registered project alias (titles repeat across projects)")),
		mcpsdk.WithReadOnlyHintAnnotation(true),
	)
}

func (s *Server) handlePlanRecordGet(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	const op = "plan_record_get"
	kind, bad := planRecordKindArg(op, req, false)
	if bad != nil {
		return bad, nil
	}
	ref := strings.TrimSpace(req.GetString("ref", ""))
	if ref == "" {
		return toolErrorf(op, "ref is required: an id, \"row <n>\", a title or a title prefix"), nil
	}
	q := url.Values{}
	if v := strings.TrimSpace(req.GetString("project", "")); v != "" {
		q.Set("project", v)
	}
	out, err := s.proxyGet(planRecordsBase+"/"+kind+"/"+url.PathEscape(ref), q)
	if err != nil {
		return toolError(op, err), nil
	}
	return textOK(string(out)), nil
}

// ----- shared content arguments -------------------------------------------

// planRecordListArg reads a list-valued argument as a real array or a JSON
// array string. It reports whether the argument was given at all, and refuses
// text that is not an array.
func planRecordListArg(req mcpsdk.CallToolRequest, key string) ([]any, bool, error) {
	switch v := req.GetArguments()[key].(type) {
	case nil:
		return nil, false, nil
	case []any:
		return v, true, nil
	case string:
		t := strings.TrimSpace(v)
		if t == "" {
			return nil, false, nil
		}
		var arr []any
		if err := json.Unmarshal([]byte(t), &arr); err != nil {
			return nil, false, fmt.Errorf("%s must be a JSON array: %v", key, err)
		}
		return arr, true, nil
	}
	return nil, false, fmt.Errorf("%s must be a JSON array", key)
}

// planRecordStringFields are the text fields both create and update carry.
var planRecordStringFields = []string{"title", "body", "state", "legacy_source", "shipped_by", "raised_by", "shipped_version", "review_reason"}

// planRecordContentBody builds the REST create or update body from the
// arguments given; a field left out is left to the daemon's rules (on update,
// left alone). A text field given as empty text is sent as empty so it can be
// cleared on update.
func planRecordContentBody(op string, req mcpsdk.CallToolRequest, update bool) (map[string]any, *mcpsdk.CallToolResult) {
	args := req.GetArguments()
	body := map[string]any{}
	for _, f := range planRecordStringFields {
		raw, present := args[f]
		if !present || raw == nil {
			continue
		}
		v, ok := raw.(string)
		if !ok {
			return nil, toolErrorf(op, "%s must be text", f)
		}
		if !update && strings.TrimSpace(v) == "" {
			continue
		}
		if f == "title" || f == "state" {
			v = strings.TrimSpace(v)
		}
		body[f] = v
	}
	if n, ok := getNumberArg(req, "legacy_number"); ok {
		if n != float64(int(n)) || n < 1 {
			return nil, toolErrorf(op, "legacy_number must be a positive whole number")
		}
		if update {
			return nil, toolErrorf(op, "legacy_number cannot change once a record exists")
		}
		body["legacy_number"] = int(n)
	}
	if links, ok, err := planRecordListArg(req, "links"); err != nil {
		return nil, toolError(op, err)
	} else if ok {
		body["links"] = links
	}
	aliasKey := "aliases"
	if update {
		aliasKey = "add_aliases"
	}
	if al, ok, err := planRecordListArg(req, aliasKey); err != nil {
		return nil, toolError(op, err)
	} else if ok {
		body[aliasKey] = al
	}
	if v, present := args["needs_review"]; present && v != nil {
		b, ok := v.(bool)
		if !ok {
			return nil, toolErrorf(op, "needs_review must be true or false")
		}
		body["needs_review"] = b
	}
	return body, nil
}

// planRecordWriteParams are the content parameters create and update share.
func planRecordWriteParams(update bool) []mcpsdk.ToolOption {
	opts := []mcpsdk.ToolOption{
		mcpsdk.WithString("title", mcpsdk.Description("The record's title: its human name, unique within a project (case and surrounding spaces ignored)")),
		mcpsdk.WithString("body", mcpsdk.Description("The record's text (up to 100 KiB)")),
		mcpsdk.WithString("state", mcpsdk.Description("Registry rows: open, planned, in_progress, blocked, shipped, done, deferred, cancelled. Findings: open, triaged, fixed, wontfix, duplicate")),
		mcpsdk.WithString("legacy_source", mcpsdk.Description("Where an imported record came from (optional)")),
		mcpsdk.WithString("shipped_by", mcpsdk.Description("The Automaton that shipped it (optional)")),
		mcpsdk.WithString("raised_by", mcpsdk.Description("The Automaton that raised it (optional)")),
		mcpsdk.WithString("shipped_version", mcpsdk.Description("What the archive said shipped it, free text (optional)")),
		mcpsdk.WithString("links", mcpsdk.Description("What the record points at, a JSON array of {\"kind\",\"ref\"} objects (optional)")),
		mcpsdk.WithBoolean("needs_review", mcpsdk.Description("Flag the record for a person to look at")),
		mcpsdk.WithString("review_reason", mcpsdk.Description("Why the record needs review (optional)")),
	}
	if update {
		opts = append(opts, mcpsdk.WithString("add_aliases", mcpsdk.Description("Older titles that should still resolve, a JSON array of strings (optional). Renaming a record already keeps the old title.")))
	} else {
		opts = append(opts,
			mcpsdk.WithNumber("legacy_number", mcpsdk.Description("Registry rows only: the old row number, kept so existing references to \"row <n>\" resolve")),
			mcpsdk.WithString("aliases", mcpsdk.Description("Older titles that should still resolve, a JSON array of strings (optional)")),
		)
	}
	return opts
}

// ----- plan_record_create --------------------------------------------------

func (s *Server) toolPlanRecordCreate() mcpsdk.Tool {
	opts := []mcpsdk.ToolOption{
		mcpsdk.WithDescription("Create a plan record: a registry row (a numbered item of the plans registry) or a finding. The daemon gives it its own short id and a uid that never changes; the old row number is kept only as legacy_number. The title must be unique within the project; a taken title is refused with the record that holds it. Returns the stored record, title first."),
		mcpsdk.WithString("kind", mcpsdk.Required(), mcpsdk.Description("registry-row or finding")),
		mcpsdk.WithString("project", mcpsdk.Required(), mcpsdk.Description("A registered project alias, for example datawatch")),
	}
	opts = append(opts, planRecordWriteParams(false)...)
	opts = append(opts, mcpsdk.WithReadOnlyHintAnnotation(false))
	return mcpsdk.NewTool("plan_record_create", opts...)
}

func (s *Server) handlePlanRecordCreate(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	const op = "plan_record_create"
	kind, bad := planRecordKindArg(op, req, false)
	if bad != nil {
		return bad, nil
	}
	project := strings.TrimSpace(req.GetString("project", ""))
	if project == "" {
		return toolErrorf(op, "project is required: name a registered project alias"), nil
	}
	body, bad := planRecordContentBody(op, req, false)
	if bad != nil {
		return bad, nil
	}
	if _, ok := body["title"]; !ok {
		return toolErrorf(op, "title is required"), nil
	}
	body["project"] = project
	out, err := s.proxyJSON(http.MethodPost, planRecordsBase+"/"+kind, body)
	if err != nil {
		return toolError(op, err), nil
	}
	return textOK(string(out)), nil
}

// ----- plan_record_update --------------------------------------------------

func (s *Server) toolPlanRecordUpdate() mcpsdk.Tool {
	opts := []mcpsdk.ToolOption{
		mcpsdk.WithDescription("Change a plan record; a field you leave out is left alone. A record is never deleted: close it by changing its state. Renaming it keeps the old title as an alias so older references still resolve. The project and the kind cannot change. The ref is an id, a uid, \"row <n>\", an exact title, an older title or an unambiguous title prefix; an ambiguous title is refused with the list of matches. Returns the stored record, title first."),
		mcpsdk.WithString("kind", mcpsdk.Required(), mcpsdk.Description("registry-row or finding")),
		mcpsdk.WithString("ref", mcpsdk.Required(), mcpsdk.Description("The record: id, uid, \"row <n>\", title, older title or title prefix")),
		mcpsdk.WithString("project", mcpsdk.Description("Narrow a title reference to this registered project alias (it does not move the record)")),
	}
	opts = append(opts, planRecordWriteParams(true)...)
	opts = append(opts, mcpsdk.WithReadOnlyHintAnnotation(false))
	return mcpsdk.NewTool("plan_record_update", opts...)
}

func (s *Server) handlePlanRecordUpdate(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	const op = "plan_record_update"
	kind, bad := planRecordKindArg(op, req, false)
	if bad != nil {
		return bad, nil
	}
	ref := strings.TrimSpace(req.GetString("ref", ""))
	if ref == "" {
		return toolErrorf(op, "ref is required: an id, \"row <n>\", a title or a title prefix"), nil
	}
	body, bad := planRecordContentBody(op, req, true)
	if bad != nil {
		return bad, nil
	}
	if len(body) == 0 {
		return toolErrorf(op, "nothing to change: give at least one field to update"), nil
	}
	path := planRecordsBase + "/" + kind + "/" + url.PathEscape(ref)
	if v := strings.TrimSpace(req.GetString("project", "")); v != "" {
		path += "?" + url.Values{"project": {v}}.Encode()
	}
	out, err := s.proxyJSON(http.MethodPut, path, body)
	if err != nil {
		return toolError(op, err), nil
	}
	return textOK(string(out)), nil
}

// ----- plan_record_search --------------------------------------------------

func (s *Server) toolPlanRecordSearch() mcpsdk.Tool {
	return mcpsdk.NewTool("plan_record_search",
		mcpsdk.WithDescription("Search registry rows and findings together by words. A title match ranks above an older-title match, which ranks above a body match. Each hit carries the record (title first, id second), a score and which part matched. Narrow by kind, project or state. Read-only. Returns the same object as GET /api/plan-records/search."),
		mcpsdk.WithString("q", mcpsdk.Required(), mcpsdk.Description("The words to search for")),
		mcpsdk.WithString("kind", mcpsdk.Description("Only registry-row or only finding; both when left out")),
		mcpsdk.WithString("project", mcpsdk.Description("Only records of this registered project alias")),
		mcpsdk.WithString("state", mcpsdk.Description("Only records in this state")),
		mcpsdk.WithNumber("limit", mcpsdk.Description("At most this many hits; default 100, maximum 1000")),
		mcpsdk.WithNumber("offset", mcpsdk.Description("Skip this many hits, for paging")),
		mcpsdk.WithReadOnlyHintAnnotation(true),
	)
}

func (s *Server) handlePlanRecordSearch(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	const op = "plan_record_search"
	text := strings.TrimSpace(req.GetString("q", ""))
	if text == "" {
		return toolErrorf(op, "q is required: the words to search for"), nil
	}
	q, bad := planRecordFilterQuery(op, req)
	if bad != nil {
		return bad, nil
	}
	if raw := strings.TrimSpace(req.GetString("kind", "")); raw != "" {
		p, ok := planRecordKindPath(raw)
		if !ok {
			return toolErrorf(op, "kind %q is not valid: use registry-row or finding", raw), nil
		}
		if p == "findings" {
			q.Set("kind", "finding")
		} else {
			q.Set("kind", "registry_row")
		}
	}
	q.Set("q", text)
	out, err := s.proxyGet(planRecordsBase+"/search", q)
	if err != nil {
		return toolError(op, err), nil
	}
	return textOK(string(out)), nil
}

// ----- plan_record_import --------------------------------------------------

func (s *Server) toolPlanRecordImport() mcpsdk.Tool {
	return mcpsdk.NewTool("plan_record_import",
		mcpsdk.WithDescription("Import a project's plans registry rows and findings files as plan records. It previews by default (dry_run true): it lists every record it would create, change or skip and every title or row-number conflict, and writes nothing, so run the preview first and read it. Set dry_run false to write. Each conflict is settled by the conflict policy: suffix (the default; add a short suffix and flag the record for review), rename or skip. Records already imported are left alone, so running it again is safe. Returns the preview or the result with the counts."),
		mcpsdk.WithString("project", mcpsdk.Description("A registered project alias; default datawatch")),
		mcpsdk.WithString("source", mcpsdk.Description("registry, findings or all; default all")),
		mcpsdk.WithString("conflict_policy", mcpsdk.Description("suffix, rename or skip; default is the daemon's autonomous.plan_records.import_conflict_policy setting")),
		mcpsdk.WithBoolean("dry_run", mcpsdk.Description("Preview only, write nothing. Default true; pass false to write")),
		mcpsdk.WithReadOnlyHintAnnotation(false),
	)
}

func (s *Server) handlePlanRecordImport(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	const op = "plan_record_import"
	// Default to a preview so an agent looks before it writes.
	dry := true
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
	source := strings.ToLower(strings.TrimSpace(req.GetString("source", "")))
	switch source {
	case "", "registry", "findings", "all":
	default:
		return toolErrorf(op, "source %q is not valid: use registry, findings or all", source), nil
	}
	policy := strings.ToLower(strings.TrimSpace(req.GetString("conflict_policy", "")))
	switch policy {
	case "", "suffix", "rename", "skip":
	default:
		return toolErrorf(op, "conflict_policy %q is not valid: use suffix, rename or skip", policy), nil
	}
	body := map[string]any{}
	if v := strings.TrimSpace(req.GetString("project", "")); v != "" {
		body["project"] = v
	}
	if source != "" {
		body["source"] = source
	}
	if policy != "" {
		body["conflict_policy"] = policy
	}
	path := planRecordsBase + "/import?" + url.Values{"dry_run": {strconv.FormatBool(dry)}}.Encode()
	out, err := s.proxyJSON(http.MethodPost, path, body)
	if err != nil {
		return toolError(op, err), nil
	}
	return textOK(string(out)), nil
}

// PlanRecordToolNames lists the plan record tools, for the parity test.
func PlanRecordToolNames() []string {
	return []string{
		"plan_record_create",
		"plan_record_get",
		"plan_record_list",
		"plan_record_update",
		"plan_record_search",
		"plan_record_import",
	}
}
