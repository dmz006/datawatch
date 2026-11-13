// Sprint Sx (v3.7.2) — MCP-tool parity for v3.5.0–v3.7.0 endpoints.
//
// Each tool wraps the corresponding REST endpoint via the in-process
// HTTP loopback so we share validation + business logic with the
// REST surface and don't duplicate wiring. webPort is set in
// New(); when it's 0 (MCP-only test mode) the tools return a clear
// error.

package mcp

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"

	"github.com/dmz006/datawatch/internal/audit"
)

// httpProxy is the shared HTTP client for in-process REST calls.
// InsecureSkipVerify is intentional: this client only calls 127.0.0.1
// (loopback) and the daemon uses a self-signed cert. #nosec G402
var httpProxy = &http.Client{
	Timeout: 90 * time.Second,
	Transport: &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, // #nosec G402 — loopback only (127.0.0.1), self-signed cert
	},
}

// proxyGet calls GET http://127.0.0.1:<webPort>/<path>?<query> and
// returns the response body (or an error on non-2xx).
func (s *Server) proxyGet(path string, q url.Values) ([]byte, error) {
	body, _, err := s.proxyGetWithHeaders(path, q)
	return body, err
}

// proxyGetWithHeaders is proxyGet that also returns the response headers,
// for endpoints that report metadata in a header.
func (s *Server) proxyGetWithHeaders(path string, q url.Values) ([]byte, http.Header, error) {
	if s.webPort == 0 {
		return nil, nil, fmt.Errorf("REST loopback unavailable (web server disabled)")
	}
	u := fmt.Sprintf("http://127.0.0.1:%d%s", s.webPort, path)
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		return nil, nil, err
	}
	if s.token != "" {
		req.Header.Set("Authorization", "Bearer "+s.token)
	}
	resp, err := httpProxy.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close() //nolint:errcheck
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode/100 != 2 {
		return nil, resp.Header, fmt.Errorf("HTTP %d: %s", resp.StatusCode, boundedErrorBody(body))
	}
	return body, resp.Header, nil
}

// isAutonomousPRDWrite reports whether method+path is a write against the
// autonomous PRD surface (/api/autonomous/prds or anything under it).
func isAutonomousPRDWrite(method, path string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
	default:
		return false
	}
	return path == "/api/autonomous/prds" || strings.HasPrefix(path, "/api/autonomous/prds/")
}

// withCompactView appends view=compact to path, using '&' when the path
// already carries a query string and '?' otherwise.
func withCompactView(path string) string {
	if strings.Contains(path, "?") {
		return path + "&view=compact"
	}
	return path + "?view=compact"
}

// boundedErrorBody trims and caps an upstream error body so a verbose
// 5xx dump can't flood the agent's context; successful bodies are
// returned by the callers as-is and are never passed through here.
func boundedErrorBody(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) <= maxToolErrorBody {
		return s
	}
	return s[:maxToolErrorBody] + "…"
}

// proxyJSON sends method+body to the loopback and returns the body.
// Autonomous PRD writes request the compact response view.
func (s *Server) proxyJSON(method, path string, body any) ([]byte, error) {
	if s.webPort == 0 {
		return nil, fmt.Errorf("REST loopback unavailable (web server disabled)")
	}
	if isAutonomousPRDWrite(method, path) {
		path = withCompactView(path)
	}
	u := fmt.Sprintf("http://127.0.0.1:%d%s", s.webPort, path)
	var rdr io.Reader
	if body != nil {
		var buf []byte
		if raw, ok := body.([]byte); ok {
			buf = raw // already marshaled — don't double-encode
		} else {
			buf, _ = json.Marshal(body)
		}
		rdr = bytes.NewReader(buf)
	}
	req, err := http.NewRequest(method, u, rdr)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if s.token != "" {
		req.Header.Set("Authorization", "Bearer "+s.token)
	}
	resp, err := httpProxy.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close() //nolint:errcheck
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, boundedErrorBody(respBody))
	}
	return respBody, nil
}

// textOK is a convenience: marshals v as JSON-string and wraps as a
// CallToolResult.
func textOK(v string) *mcpsdk.CallToolResult {
	return mcpsdk.NewToolResultText(v)
}

// ----- BL34: ask -----------------------------------------------------------

func (s *Server) toolAsk() mcpsdk.Tool {
	return mcpsdk.NewTool("ask",
		mcpsdk.WithDescription("Single-shot LLM ask without spawning a session. Routes to Ollama or OpenWebUI."),
		mcpsdk.WithString("question", mcpsdk.Required(), mcpsdk.Description("The question to ask")),
		mcpsdk.WithString("backend", mcpsdk.Description("ollama (default) or openwebui")),
		mcpsdk.WithString("model", mcpsdk.Description("Optional model override")),
	)
}
func (s *Server) handleAsk(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	body := map[string]any{
		"question": req.GetString("question", ""),
		"backend":  req.GetString("backend", "ollama"),
		"model":    req.GetString("model", ""),
	}
	out, err := s.proxyJSON(http.MethodPost, "/api/ask", body)
	if err != nil {
		return toolError("ask", err), nil
	}
	return textOK(string(out)), nil
}

// ----- BL35: project_summary ----------------------------------------------

func (s *Server) toolProjectSummary() mcpsdk.Tool {
	return mcpsdk.NewTool("project_summary",
		mcpsdk.WithDescription("Project overview: git status + recent commits + per-project session stats."),
		mcpsdk.WithString("dir", mcpsdk.Required(), mcpsdk.Description("Absolute path to the project directory")),
	)
}
func (s *Server) handleProjectSummary(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	q := url.Values{"dir": []string{req.GetString("dir", "")}}
	out, err := s.proxyGet("/api/project/summary", q)
	if err != nil {
		return toolError("project_summary", err), nil
	}
	return textOK(string(out)), nil
}

// ----- BL5: templates ------------------------------------------------------

func (s *Server) toolTemplateList() mcpsdk.Tool {
	return mcpsdk.NewTool("template_list", mcpsdk.WithDescription("List session-start templates."))
}
func (s *Server) handleTemplateList(_ context.Context, _ mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	out, err := s.proxyGet("/api/templates", nil)
	if err != nil {
		return toolError("template_list", err), nil
	}
	return textOK(string(out)), nil
}

func (s *Server) toolTemplateUpsert() mcpsdk.Tool {
	return mcpsdk.NewTool("template_upsert",
		mcpsdk.WithDescription("Create or update a session-start template (BL5)."),
		mcpsdk.WithString("name", mcpsdk.Required(), mcpsdk.Description("Template name")),
		mcpsdk.WithString("project_dir", mcpsdk.Description("Default project_dir")),
		mcpsdk.WithString("backend", mcpsdk.Description("Default backend")),
		mcpsdk.WithString("effort", mcpsdk.Description("Default effort: quick/normal/thorough")),
		mcpsdk.WithString("description", mcpsdk.Description("Description")),
	)
}
func (s *Server) handleTemplateUpsert(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	body := map[string]any{
		"name":        req.GetString("name", ""),
		"project_dir": req.GetString("project_dir", ""),
		"backend":     req.GetString("backend", ""),
		"effort":      req.GetString("effort", ""),
		"description": req.GetString("description", ""),
	}
	out, err := s.proxyJSON(http.MethodPost, "/api/templates", body)
	if err != nil {
		return toolError("template_upsert", err), nil
	}
	return textOK(string(out)), nil
}

func (s *Server) toolTemplateDelete() mcpsdk.Tool {
	return mcpsdk.NewTool("template_delete",
		mcpsdk.WithDescription("Delete a session-start template by name."),
		mcpsdk.WithString("name", mcpsdk.Required(), mcpsdk.Description("Template name")),
	)
}
func (s *Server) handleTemplateDelete(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	out, err := s.proxyJSON(http.MethodDelete, "/api/templates/"+req.GetString("name", ""), nil)
	if err != nil {
		return toolError("template_delete", err), nil
	}
	return textOK(string(out)), nil
}

// ----- BL27: projects ------------------------------------------------------

func (s *Server) toolProjectList() mcpsdk.Tool {
	return mcpsdk.NewTool("project_list", mcpsdk.WithDescription("List registered project aliases."))
}
func (s *Server) handleProjectList(_ context.Context, _ mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	out, err := s.proxyGet("/api/projects", nil)
	if err != nil {
		return toolError("project_list", err), nil
	}
	return textOK(string(out)), nil
}

func (s *Server) toolProjectUpsert() mcpsdk.Tool {
	return mcpsdk.NewTool("project_upsert",
		mcpsdk.WithDescription(
			"Register or update a project alias (BL27), and optionally its per-project policy tier — the layer between the daemon-wide defaults and the Automaton's own overrides. Unset policy keys keep the daemon tier; the daemon re-validates the block and refuses it, naming the offending field, if a key holds a value that key does not know."),
		mcpsdk.WithString("name", mcpsdk.Required(), mcpsdk.Description("Project alias")),
		mcpsdk.WithString("dir", mcpsdk.Required(), mcpsdk.Description("Absolute directory")),
		mcpsdk.WithString("default_backend", mcpsdk.Description("Default LLM backend")),
		mcpsdk.WithString("description", mcpsdk.Description("Description")),
		mcpsdk.WithObject("policy",
			mcpsdk.Description(
				"Optional per-project policy block (e.g. {\"plan_review_mode\":\"block\",\"max_concurrent_tasks\":2,\"retry_policy\":{\"infra\":3}}). Unset keys keep the daemon tier.")),
		mcpsdk.WithString("owner", mcpsdk.Description("Optional project record: who owns the project. Omit to keep the stored value; an empty string clears it.")),
		mcpsdk.WithString("repository", mcpsdk.Description("Optional project record: repository (e.g. owner/name or URL). Omit to keep; empty clears.")),
		mcpsdk.WithString("default_branch", mcpsdk.Description("Optional project record: default git branch. Omit to keep; empty clears.")),
		mcpsdk.WithArray("arcs",
			mcpsdk.Description("Optional ordered arc list, replacing the stored one: [{\"automation_id\":\"<id>\",\"priority\":1,\"state\":\"planned|active|done\"}]. Array position is the arc order. Omit to keep; [] clears."),
			mcpsdk.Items(map[string]any{"type": "object"})),
		mcpsdk.WithObject("target",
			mcpsdk.Description("Optional outcome the project works toward: {\"name\":\"...\",\"definition_of_done\":\"...\"}. Omit to keep the stored value.")),
		mcpsdk.WithObject("lane_defaults",
			mcpsdk.Description("Optional lane defaults for new work: {\"lane\":\"...\",\"backend\":\"...\",\"model\":\"...\"}. Omit to keep the stored value.")),
		mcpsdk.WithNumber("weekly_share", mcpsdk.Description("Optional weekly share (percent of the weekly budget). Omit to keep the stored value.")),
		mcpsdk.WithNumber("branch_retention_days", mcpsdk.Description("Optional number of days to keep merged task branches. Omit to keep the stored value.")),
		mcpsdk.WithString("major_trigger", mcpsdk.Description("Optional major-release trigger. Omit to keep; empty clears.")))
}
func (s *Server) handleProjectUpsert(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	body := map[string]any{
		"name":            req.GetString("name", ""),
		"dir":             req.GetString("dir", ""),
		"default_backend": req.GetString("default_backend", ""),
		"description":     req.GetString("description", ""),
	}
	if raw, ok := req.GetArguments()["policy"]; ok && raw != nil {
		var obj map[string]any
		switch v := raw.(type) {
		case map[string]any:
			obj = v
		case string:
			if err := json.Unmarshal([]byte(v), &obj); err != nil {
				return toolErrorf("project_upsert", "policy must be an object (or a JSON string encoding one): %v", err), nil
			}
		default:
			return toolErrorf("project_upsert", "policy must be an object (or a JSON string encoding one)"), nil
		}
		b, err := json.Marshal(obj)
		if err != nil {
			return toolErrorf("project_upsert", "policy: %v", err), nil
		}
		// json.RawMessage keeps the block an object on the wire; a plain
		// []byte would marshal as a base64 string.
		body["policy"] = json.RawMessage(b)
	}
	if err := applyProjectRecordArgs(req.GetArguments(), body); err != nil {
		return toolErrorf("project_upsert", "%v", err), nil
	}
	out, err := s.proxyJSON(http.MethodPost, "/api/projects", body)
	if err != nil {
		return toolError("project_upsert", err), nil
	}
	return textOK(string(out)), nil
}

func (s *Server) toolProjectAliasDelete() mcpsdk.Tool {
	return mcpsdk.NewTool("project_alias_delete",
		mcpsdk.WithDescription("Delete a project alias by name (does not touch the directory)."),
		mcpsdk.WithString("name", mcpsdk.Required(), mcpsdk.Description("Project alias")),
	)
}
func (s *Server) handleProjectAliasDelete(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	out, err := s.proxyJSON(http.MethodDelete, "/api/projects/"+req.GetString("name", ""), nil)
	if err != nil {
		return toolError("project_alias_delete", err), nil
	}
	return textOK(string(out)), nil
}

// toolProjectPolicyEffective exposes GET /api/projects/{name}/policy/effective:
// every policy setting for the project, with the value in force and the tier
// (automaton, project, or daemon) that supplied it.
func (s *Server) toolProjectPolicyEffective() mcpsdk.Tool {
	return mcpsdk.NewTool("project_policy_effective",
		mcpsdk.WithDescription(
			"Effective policy for one project: every setting with the value in force and the tier (automaton, project, daemon) that supplied it. Optional automaton parameter resolves with that Automaton's own policy fields as the top tier. Read-only."),
		mcpsdk.WithString("name", mcpsdk.Required(), mcpsdk.Description("The project alias.")),
		mcpsdk.WithString("automaton", mcpsdk.Description("Optional Automaton ID whose own policy fields act as the top tier.")),
	)
}
func (s *Server) handleProjectPolicyEffective(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	name := req.GetString("name", "")
	if name == "" {
		return toolErrorf("project_policy_effective", "name is required"), nil
	}
	q := url.Values{}
	if auto := req.GetString("automaton", ""); auto != "" {
		q.Set("automaton", auto)
	}
	out, err := s.proxyGet("/api/projects/"+url.PathEscape(name)+"/policy/effective", q)
	if err != nil {
		return toolError("project_policy_effective", err), nil
	}
	return textOK(string(out)), nil
}

// projectRecordStringKeys are the optional project record fields that hold a
// plain string. A key the caller omits keeps the stored value; a key sent as
// an empty string clears it.
var projectRecordStringKeys = []string{"owner", "repository", "default_branch", "major_trigger"}

// projectRecordIntKeys are the optional project record fields that hold a
// whole number.
var projectRecordIntKeys = []string{"weekly_share", "branch_retention_days"}

// projectRecordObjectKeys are the optional project record fields that hold a
// JSON object.
var projectRecordObjectKeys = []string{"target", "lane_defaults"}

// jsonValueArg accepts a tool argument that is either already decoded JSON or
// a string holding JSON, and returns the decoded value.
func jsonValueArg(name string, raw any) (any, error) {
	if s, ok := raw.(string); ok {
		var out any
		if err := json.Unmarshal([]byte(s), &out); err != nil {
			return nil, fmt.Errorf("%s must be JSON (or a JSON string encoding it): %v", name, err)
		}
		return out, nil
	}
	return raw, nil
}

// applyProjectRecordArgs copies the optional project record fields from the
// project_upsert arguments into the upsert body. Only keys the caller sent are
// copied, so the daemon keeps every other stored value.
func applyProjectRecordArgs(args map[string]any, body map[string]any) error {
	for _, k := range projectRecordStringKeys {
		v, ok := args[k]
		if !ok || v == nil {
			continue
		}
		str, isStr := v.(string)
		if !isStr {
			return fmt.Errorf("%s must be a string", k)
		}
		body[k] = str
	}
	for _, k := range projectRecordIntKeys {
		v, ok := args[k]
		if !ok || v == nil {
			continue
		}
		f, isNum := v.(float64)
		if !isNum || f != float64(int(f)) {
			return fmt.Errorf("%s must be a whole number", k)
		}
		body[k] = int(f)
	}
	for _, k := range projectRecordObjectKeys {
		v, ok := args[k]
		if !ok || v == nil {
			continue
		}
		decoded, err := jsonValueArg(k, v)
		if err != nil {
			return err
		}
		if _, isObj := decoded.(map[string]any); !isObj {
			return fmt.Errorf("%s must be an object", k)
		}
		body[k] = decoded
	}
	if v, ok := args["arcs"]; ok && v != nil {
		decoded, err := jsonValueArg("arcs", v)
		if err != nil {
			return err
		}
		arcs, err := arcsArg(decoded)
		if err != nil {
			return err
		}
		body["arcs"] = arcs
	}
	return nil
}

// arcsArg checks that a decoded arcs argument is an array of arc objects and
// returns it. Each arc is {automation_id, priority?, state?}; the daemon
// validates the values themselves.
func arcsArg(decoded any) ([]any, error) {
	arr, ok := decoded.([]any)
	if !ok {
		return nil, fmt.Errorf("arcs must be an array of arc objects")
	}
	for i, item := range arr {
		if _, isObj := item.(map[string]any); !isObj {
			return nil, fmt.Errorf("arcs[%d] must be an object", i)
		}
	}
	return arr, nil
}

// toolProjectRollup exposes GET /api/projects/{name}/rollup: the project's arcs
// with per-arc progress, cost in points, and throughput over a window.
func (s *Server) toolProjectRollup() mcpsdk.Tool {
	return mcpsdk.NewTool("project_rollup",
		mcpsdk.WithDescription(
			"Project roll-up: arcs in order with their Automata, task progress, cost in points and throughput over a window of days. Automata that no arc holds are in unassigned when they belong to the project, and Automata without a project are in without_project (not counted in total). An Automaton counts as finished when it is completed, when it is cancelled or archived and its work branch is already on the default branch, or when it carries a shipped record; a task counts from its own completion time in the window, whatever the Automaton status. Read-only."),
		mcpsdk.WithString("name", mcpsdk.Required(), mcpsdk.Description("The project alias.")),
		mcpsdk.WithNumber("window", mcpsdk.Description("Throughput window in days (1–365, default 7).")),
	)
}
func (s *Server) handleProjectRollup(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	name := req.GetString("name", "")
	if name == "" {
		return toolErrorf("project_rollup", "name is required"), nil
	}
	q := url.Values{}
	if v, ok := req.GetArguments()["window"]; ok && v != nil {
		f, isNum := v.(float64)
		if !isNum || f != float64(int(f)) {
			return toolErrorf("project_rollup", "window must be a whole number of days"), nil
		}
		q.Set("window", fmt.Sprintf("%d", int(f)))
	}
	out, err := s.proxyGet("/api/projects/"+url.PathEscape(name)+"/rollup", q)
	if err != nil {
		return toolError("project_rollup", err), nil
	}
	return textOK(string(out)), nil
}

// toolProjectHistory exposes GET /api/projects/{name}/history: finished and
// archived Automata, daily progress and Gantt rows, one page at a time.
func (s *Server) toolProjectHistory() mcpsdk.Tool {
	return mcpsdk.NewTool("project_history",
		mcpsdk.WithDescription(
			"Project history: finished and archived Automata, daily progress and Gantt rows. Paged with limit and the cursor from the previous page. Read-only."),
		mcpsdk.WithString("name", mcpsdk.Required(), mcpsdk.Description("The project alias.")),
		mcpsdk.WithNumber("limit", mcpsdk.Description("Maximum rows per page (the daemon's default applies when omitted).")),
		mcpsdk.WithString("cursor", mcpsdk.Description("Opaque cursor from the previous page's next_cursor. Omit for the first page.")),
	)
}
func (s *Server) handleProjectHistory(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	name := req.GetString("name", "")
	if name == "" {
		return toolErrorf("project_history", "name is required"), nil
	}
	q := url.Values{}
	if v, ok := req.GetArguments()["limit"]; ok && v != nil {
		f, isNum := v.(float64)
		if !isNum || f != float64(int(f)) {
			return toolErrorf("project_history", "limit must be a whole number"), nil
		}
		q.Set("limit", fmt.Sprintf("%d", int(f)))
	}
	if c := req.GetString("cursor", ""); c != "" {
		q.Set("cursor", c)
	}
	out, err := s.proxyGet("/api/projects/"+url.PathEscape(name)+"/history", q)
	if err != nil {
		return toolError("project_history", err), nil
	}
	return textOK(string(out)), nil
}

// toolProjectTimeline exposes GET /api/projects/{name}/timeline: when each
// arc, Automaton and task of a project started and finished, one level at a time.
func (s *Server) toolProjectTimeline() mcpsdk.Tool {
	return mcpsdk.NewTool("project_timeline",
		mcpsdk.WithDescription(
			"Project timeline: start, end, status (completed, running, waiting, blocked, failed or cancelled) and release of each arc, Automaton and task, one level at a time (arcs by default). Task rows carry depends_on; Automaton levels and focus replies carry edges, the chain between Automata. Rows still running have no end. Fields are returned unchanged from the daemon. Paged with limit and the cursor from the previous page. Read-only."),
		mcpsdk.WithString("name", mcpsdk.Required(), mcpsdk.Description("The project alias.")),
		mcpsdk.WithString("level", mcpsdk.Description("Which rows to return: arcs (default), automata or tasks.")),
		mcpsdk.WithString("parent", mcpsdk.Description("With level automata: the arc id whose Automata to list, or none for Automata no arc holds.")),
		mcpsdk.WithString("automaton", mcpsdk.Description("With level tasks: the Automaton id whose tasks to list.")),
		mcpsdk.WithString("focus", mcpsdk.Description("One arc id (or none): returns that arc's Automata and their tasks in a single unpaged reply.")),
		mcpsdk.WithNumber("limit", mcpsdk.Description("Maximum rows per page (the daemon's default applies when omitted).")),
		mcpsdk.WithString("cursor", mcpsdk.Description("Opaque cursor from the previous page's next_cursor. Omit for the first page.")),
	)
}
func (s *Server) handleProjectTimeline(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	name := req.GetString("name", "")
	if name == "" {
		return toolErrorf("project_timeline", "name is required"), nil
	}
	q := url.Values{}
	for _, k := range []string{"level", "parent", "automaton", "focus", "cursor"} {
		if v := req.GetString(k, ""); v != "" {
			q.Set(k, v)
		}
	}
	if v, ok := req.GetArguments()["limit"]; ok && v != nil {
		f, isNum := v.(float64)
		if !isNum || f != float64(int(f)) {
			return toolErrorf("project_timeline", "limit must be a whole number"), nil
		}
		q.Set("limit", fmt.Sprintf("%d", int(f)))
	}
	out, err := s.proxyGet("/api/projects/"+url.PathEscape(name)+"/timeline", q)
	if err != nil {
		return toolError("project_timeline", err), nil
	}
	return textOK(string(out)), nil
}

// toolProjectSetArcs exposes PUT /api/projects/{name}/arcs: it replaces the
// project's ordered arc list and returns the project record.
func (s *Server) toolProjectSetArcs() mcpsdk.Tool {
	return mcpsdk.NewTool("project_set_arcs",
		mcpsdk.WithDescription(
			"Replace a project's ordered arc list. The array order is the arc order. Each arc is {automation_id, priority?, state? (planned|active|done)}. Sending [] clears the list. Returns the project record."),
		mcpsdk.WithString("name", mcpsdk.Required(), mcpsdk.Description("The project alias.")),
		mcpsdk.WithArray("arcs",
			mcpsdk.Required(),
			mcpsdk.Description("Ordered arcs: [{\"automation_id\":\"<id>\",\"priority\":1,\"state\":\"planned\"}]."),
			mcpsdk.Items(map[string]any{"type": "object"})),
	)
}
func (s *Server) handleProjectSetArcs(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	name := req.GetString("name", "")
	if name == "" {
		return toolErrorf("project_set_arcs", "name is required"), nil
	}
	raw, ok := req.GetArguments()["arcs"]
	if !ok || raw == nil {
		return toolErrorf("project_set_arcs", "arcs is required (an array; [] clears the list)"), nil
	}
	decoded, err := jsonValueArg("arcs", raw)
	if err != nil {
		return toolErrorf("project_set_arcs", "%v", err), nil
	}
	arcs, err := arcsArg(decoded)
	if err != nil {
		return toolErrorf("project_set_arcs", "%v", err), nil
	}
	body := map[string]any{"arcs": arcs}
	out, err := s.proxyJSON(http.MethodPut, "/api/projects/"+url.PathEscape(name)+"/arcs", body)
	if err != nil {
		return toolError("project_set_arcs", err), nil
	}
	return textOK(string(out)), nil
}

// ----- BL29: rollback ------------------------------------------------------

func (s *Server) toolSessionRollback() mcpsdk.Tool {
	return mcpsdk.NewTool("session_rollback",
		mcpsdk.WithDescription("Roll back a session's project_dir to its pre-session checkpoint tag (BL29)."),
		mcpsdk.WithString("id", mcpsdk.Required(), mcpsdk.Description("Session ID (full or short)")),
		mcpsdk.WithBoolean("force", mcpsdk.Description("Discard uncommitted changes (default false)")),
	)
}
func (s *Server) handleSessionRollback(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	body := map[string]any{"force": req.GetBool("force", false)}
	out, err := s.proxyJSON(http.MethodPost, "/api/sessions/"+req.GetString("id", "")+"/rollback", body)
	if err != nil {
		return toolError("session_rollback", err), nil
	}
	return textOK(string(out)), nil
}

// ----- BL30: cooldown ------------------------------------------------------

func (s *Server) toolCooldownStatus() mcpsdk.Tool {
	return mcpsdk.NewTool("cooldown_status",
		mcpsdk.WithDescription("Get the current global rate-limit cooldown state (BL30)."),
	)
}
func (s *Server) handleCooldownStatus(_ context.Context, _ mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	out, err := s.proxyGet("/api/cooldown", nil)
	if err != nil {
		return toolError("cooldown_status", err), nil
	}
	return textOK(string(out)), nil
}

func (s *Server) toolCooldownSet() mcpsdk.Tool {
	return mcpsdk.NewTool("cooldown_set",
		mcpsdk.WithDescription("Activate a global rate-limit cooldown until the given Unix-ms time (BL30)."),
		mcpsdk.WithNumber("until_unix_ms", mcpsdk.Required(), mcpsdk.Description("Unix epoch milliseconds when cooldown ends")),
		mcpsdk.WithString("reason", mcpsdk.Description("Operator-readable reason")),
	)
}
func (s *Server) handleCooldownSet(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	body := map[string]any{
		"until_unix_ms": int64(req.GetFloat("until_unix_ms", 0)),
		"reason":        req.GetString("reason", ""),
	}
	out, err := s.proxyJSON(http.MethodPost, "/api/cooldown", body)
	if err != nil {
		return toolError("cooldown_set", err), nil
	}
	return textOK(string(out)), nil
}

func (s *Server) toolCooldownClear() mcpsdk.Tool {
	return mcpsdk.NewTool("cooldown_clear",
		mcpsdk.WithDescription("Clear the active global rate-limit cooldown (BL30)."),
	)
}
func (s *Server) handleCooldownClear(_ context.Context, _ mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	out, err := s.proxyJSON(http.MethodDelete, "/api/cooldown", nil)
	if err != nil {
		return toolError("cooldown_clear", err), nil
	}
	if len(out) == 0 {
		out = []byte(`{"status":"cleared"}`)
	}
	return textOK(string(out)), nil
}

// ----- BL40: stale ---------------------------------------------------------

func (s *Server) toolSessionsStale() mcpsdk.Tool {
	return mcpsdk.NewTool("sessions_stale",
		mcpsdk.WithDescription("List running sessions whose UpdatedAt is older than the threshold (BL40)."),
		mcpsdk.WithNumber("seconds", mcpsdk.Description("Override threshold seconds (default uses session.stale_timeout_seconds)")),
	)
}
func (s *Server) handleSessionsStale(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	q := url.Values{}
	if n := int(req.GetFloat("seconds", 0)); n > 0 {
		q.Set("seconds", strconv.Itoa(n))
	}
	out, err := s.proxyGet("/api/sessions/stale", q)
	if err != nil {
		return toolError("sessions_stale", err), nil
	}
	return textOK(string(out)), nil
}

// ----- BL6: cost -----------------------------------------------------------

func (s *Server) toolCostSummary() mcpsdk.Tool {
	return mcpsdk.NewTool("cost_summary",
		mcpsdk.WithDescription("Token + USD cost rollup. Pass session=<full_id> for per-session breakdown."),
		mcpsdk.WithString("session", mcpsdk.Description("Optional session full_id for per-session breakdown")),
	)
}
func (s *Server) handleCostSummary(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	q := url.Values{}
	if id := req.GetString("session", ""); id != "" {
		q.Set("session", id)
	}
	out, err := s.proxyGet("/api/cost", q)
	if err != nil {
		return toolError("cost_summary", err), nil
	}
	return textOK(string(out)), nil
}

func (s *Server) toolCostUsage() mcpsdk.Tool {
	return mcpsdk.NewTool("cost_usage",
		mcpsdk.WithDescription("Add usage to a session's running token + cost counters (BL6)."),
		mcpsdk.WithString("session", mcpsdk.Required(), mcpsdk.Description("Session full_id")),
		mcpsdk.WithNumber("tokens_in", mcpsdk.Required(), mcpsdk.Description("Input tokens consumed")),
		mcpsdk.WithNumber("tokens_out", mcpsdk.Required(), mcpsdk.Description("Output tokens produced")),
		mcpsdk.WithNumber("in_per_k", mcpsdk.Description("Per-1K input rate override (USD)")),
		mcpsdk.WithNumber("out_per_k", mcpsdk.Description("Per-1K output rate override (USD)")),
	)
}
func (s *Server) handleCostUsage(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	body := map[string]any{
		"session":    req.GetString("session", ""),
		"tokens_in":  int(req.GetFloat("tokens_in", 0)),
		"tokens_out": int(req.GetFloat("tokens_out", 0)),
		"in_per_k":   req.GetFloat("in_per_k", 0),
		"out_per_k":  req.GetFloat("out_per_k", 0),
	}
	out, err := s.proxyJSON(http.MethodPost, "/api/cost/usage", body)
	if err != nil {
		return toolError("cost_usage", err), nil
	}
	return textOK(string(out)), nil
}

func (s *Server) toolCostRates() mcpsdk.Tool {
	return mcpsdk.NewTool("cost_rates",
		mcpsdk.WithDescription("Get the effective per-backend USD rate table (BL6)."),
	)
}
func (s *Server) handleCostRates(_ context.Context, _ mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	out, err := s.proxyGet("/api/cost/rates", nil)
	if err != nil {
		return toolError("cost_rates", err), nil
	}
	return textOK(string(out)), nil
}

// ----- BL9: audit ----------------------------------------------------------

func (s *Server) toolAuditQuery() mcpsdk.Tool {
	return mcpsdk.NewTool("audit_query",
		mcpsdk.WithDescription("Query the operator audit log (BL9). Newest-first. "+
			"The action filter is an exact match by default and a prefix match when it ends in '*', "+
			"so action=autonomous_budget_* or action=autonomous_* lists a whole family. "+
			"The subject filter is an exact match on what the event is about (a task id, backend or LLM name). "+
			"Capacity events: "+strings.Join(audit.CapacityActions, ", ")+". "+
			"since/until must be RFC3339 or the call is rejected."),
		mcpsdk.WithString("actor", mcpsdk.Description("Filter by actor (e.g. 'operator', 'channel:signal', 'mcp')")),
		mcpsdk.WithString("action", mcpsdk.Description(audit.QueryActionHelp())),
		mcpsdk.WithString("subject", mcpsdk.Description("Filter by subject, exact match (task id, backend or LLM name)")),
		mcpsdk.WithString("session_id", mcpsdk.Description("Filter by session_id")),
		mcpsdk.WithString("since", mcpsdk.Description("RFC3339 lower bound")),
		mcpsdk.WithString("until", mcpsdk.Description("RFC3339 upper bound")),
		mcpsdk.WithNumber("limit", mcpsdk.Description("Max entries (default 100)")),
	)
}

// AuditQueryToolArgs returns the argument names the audit_query tool declares.
func AuditQueryToolArgs() []string {
	props := (&Server{}).toolAuditQuery().InputSchema.Properties
	names := make([]string, 0, len(props))
	for name := range props {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func (s *Server) handleAuditQuery(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	get := func(name string) string {
		if name == "limit" {
			if n := int(req.GetFloat("limit", 0)); n > 0 {
				return strconv.Itoa(n)
			}
			return ""
		}
		return req.GetString(name, "")
	}
	if _, err := audit.ParseQuery(get); err != nil {
		return toolError("audit_query", err), nil
	}
	q := url.Values{}
	for _, k := range audit.QueryParamNames {
		if v := get(k); v != "" {
			q.Set(k, v)
		}
	}
	out, err := s.proxyGet("/api/audit", q)
	if err != nil {
		return toolError("audit_query", err), nil
	}
	return textOK(string(out)), nil
}

// ----- GH#201: access log query ---------------------------------------------

func (s *Server) toolAuditAccessQuery() mcpsdk.Tool {
	return mcpsdk.NewTool("audit_access_query",
		mcpsdk.WithDescription("Query the HTTP access / WebSocket lifecycle / auth-failure log (GH#201). "+
			"Answers 'who connected, from what IP, when, with what principal' and 'auth failures / new IPs'. Newest-first."),
		mcpsdk.WithString("actor", mcpsdk.Description("Filter by resolved principal (admin, session-scoped, peer:<name>, proxy:<name>, unauthenticated)")),
		mcpsdk.WithString("action", mcpsdk.Description("Filter by event type (http_access, auth_failure, ws_connect, ws_disconnect)")),
		mcpsdk.WithString("since", mcpsdk.Description("RFC3339 lower bound")),
		mcpsdk.WithString("until", mcpsdk.Description("RFC3339 upper bound")),
		mcpsdk.WithNumber("limit", mcpsdk.Description("Max entries (default 100)")),
	)
}
func (s *Server) handleAuditAccessQuery(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	q := url.Values{}
	for _, k := range []string{"actor", "action", "since", "until"} {
		if v := req.GetString(k, ""); v != "" {
			q.Set(k, v)
		}
	}
	if n := int(req.GetFloat("limit", 0)); n > 0 {
		q.Set("limit", strconv.Itoa(n))
	}
	out, err := s.proxyGet("/api/audit/access", q)
	if err != nil {
		return toolError("audit_access_query", err), nil
	}
	return textOK(string(out)), nil
}

// ----- BL37: diagnose ------------------------------------------------------

func (s *Server) toolDiagnose() mcpsdk.Tool {
	return mcpsdk.NewTool("diagnose",
		mcpsdk.WithDescription("Run health checks (tmux, sessions, disk, goroutines) — composite ok flag (BL37)."),
	)
}
func (s *Server) handleDiagnose(_ context.Context, _ mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	out, err := s.proxyGet("/api/diagnose", nil)
	if err != nil {
		return toolError("diagnose", err), nil
	}
	return textOK(string(out)), nil
}

// ----- BL17: reload --------------------------------------------------------

func (s *Server) toolReload() mcpsdk.Tool {
	return mcpsdk.NewTool("reload",
		mcpsdk.WithDescription("Hot-reload config from disk (BL17). Optional subsystem param targets a specific reloader (config / filters / memory) — v5.27.2."),
		mcpsdk.WithString("subsystem", mcpsdk.Description("Optional subsystem name. Empty/'config' = full config reload; 'filters' = invalidate FilterEngine regex cache; 'memory' = refresh memory adapter caches.")),
	)
}
func (s *Server) handleReload(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	path := "/api/reload"
	if sub := req.GetString("subsystem", ""); sub != "" {
		path = "/api/reload?subsystem=" + sub
	}
	out, err := s.proxyJSON(http.MethodPost, path, nil)
	if err != nil {
		return toolError("reload", err), nil
	}
	return textOK(string(out)), nil
}

// ----- S4 (v3.8.0) ---------------------------------------------------------

// BL42 assist
func (s *Server) toolAssist() mcpsdk.Tool {
	return mcpsdk.NewTool("assist",
		mcpsdk.WithDescription("Quick-response assistant — wraps /api/ask with the configured assistant backend + system prompt (BL42)."),
		mcpsdk.WithString("question", mcpsdk.Required(), mcpsdk.Description("The question to ask")),
	)
}
func (s *Server) handleAssist(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	body := map[string]any{"question": req.GetString("question", "")}
	out, err := s.proxyJSON(http.MethodPost, "/api/assist", body)
	if err != nil {
		return toolError("assist", err), nil
	}
	return textOK(string(out)), nil
}

// BL31 device aliases
func (s *Server) toolDeviceAliasList() mcpsdk.Tool {
	return mcpsdk.NewTool("device_alias_list",
		mcpsdk.WithDescription("List operator-defined device aliases (BL31)."),
	)
}
func (s *Server) handleDeviceAliasList(_ context.Context, _ mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	out, err := s.proxyGet("/api/device-aliases", nil)
	if err != nil {
		return toolError("device_alias_list", err), nil
	}
	return textOK(string(out)), nil
}

func (s *Server) toolDeviceAliasUpsert() mcpsdk.Tool {
	return mcpsdk.NewTool("device_alias_upsert",
		mcpsdk.WithDescription("Add or update a device alias for `new: @<alias>:` routing (BL31)."),
		mcpsdk.WithString("alias", mcpsdk.Required(), mcpsdk.Description("Operator-friendly alias")),
		mcpsdk.WithString("server", mcpsdk.Required(), mcpsdk.Description("Remote server name (must exist in `servers:`)")),
	)
}
func (s *Server) handleDeviceAliasUpsert(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	body := map[string]any{
		"alias":  req.GetString("alias", ""),
		"server": req.GetString("server", ""),
	}
	out, err := s.proxyJSON(http.MethodPost, "/api/device-aliases", body)
	if err != nil {
		return toolError("device_alias_upsert", err), nil
	}
	return textOK(string(out)), nil
}

func (s *Server) toolDeviceAliasDelete() mcpsdk.Tool {
	return mcpsdk.NewTool("device_alias_delete",
		mcpsdk.WithDescription("Remove a device alias (BL31)."),
		mcpsdk.WithString("alias", mcpsdk.Required(), mcpsdk.Description("Alias to remove")),
	)
}
func (s *Server) handleDeviceAliasDelete(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	out, err := s.proxyJSON(http.MethodDelete, "/api/device-aliases/"+req.GetString("alias", ""), nil)
	if err != nil {
		return toolError("device_alias_delete", err), nil
	}
	return textOK(string(out)), nil
}

// BL69 splash info (no logo binary tool — operators set the file path in YAML/REST)
func (s *Server) toolSplashInfo() mcpsdk.Tool {
	return mcpsdk.NewTool("splash_info",
		mcpsdk.WithDescription("Returns splash render context (logo URL, tagline, version, hostname) (BL69)."),
	)
}
func (s *Server) handleSplashInfo(_ context.Context, _ mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	out, err := s.proxyGet("/api/splash/info", nil)
	if err != nil {
		return toolError("splash_info", err), nil
	}
	return textOK(string(out)), nil
}

// ----- BL20 routing-rules (S5 v3.9.0) --------------------------------------

func (s *Server) toolRoutingRulesList() mcpsdk.Tool {
	return mcpsdk.NewTool("routing_rules_list",
		mcpsdk.WithDescription("List backend auto-selection routing rules (BL20)."))
}
func (s *Server) handleRoutingRulesList(_ context.Context, _ mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	out, err := s.proxyGet("/api/routing-rules", nil)
	if err != nil {
		return toolError("routing_rules_list", err), nil
	}
	return textOK(string(out)), nil
}

func (s *Server) toolRoutingRulesTest() mcpsdk.Tool {
	return mcpsdk.NewTool("routing_rules_test",
		mcpsdk.WithDescription("Test which backend a task would route to under current rules (BL20)."),
		mcpsdk.WithString("task", mcpsdk.Required(), mcpsdk.Description("Task text to test")),
	)
}
func (s *Server) handleRoutingRulesTest(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	out, err := s.proxyJSON(http.MethodPost, "/api/routing-rules/test",
		map[string]any{"task": req.GetString("task", "")})
	if err != nil {
		return toolError("routing_rules_test", err), nil
	}
	return textOK(string(out)), nil
}

// ----- BL12: analytics -----------------------------------------------------

func (s *Server) toolAnalytics() mcpsdk.Tool {
	return mcpsdk.NewTool("analytics",
		mcpsdk.WithDescription("Day-bucketed session analytics (BL12). range=Nd (1..365)."),
		mcpsdk.WithString("range", mcpsdk.Description("Like '7d', '30d'")),
	)
}
func (s *Server) handleAnalytics(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	q := url.Values{}
	if v := req.GetString("range", ""); v != "" {
		q.Set("range", v)
	}
	out, err := s.proxyGet("/api/analytics", q)
	if err != nil {
		return toolError("analytics", err), nil
	}
	return textOK(string(out)), nil
}
