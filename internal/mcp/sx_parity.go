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
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

// httpProxy is the shared HTTP client for in-process REST calls.
var httpProxy = &http.Client{Timeout: 90 * time.Second}

// proxyGet calls GET http://127.0.0.1:<webPort>/<path>?<query> and
// returns the response body (or an error on non-2xx).
func (s *Server) proxyGet(path string, q url.Values) ([]byte, error) {
	if s.webPort == 0 {
		return nil, fmt.Errorf("REST loopback unavailable (web server disabled)")
	}
	u := fmt.Sprintf("http://127.0.0.1:%d%s", s.webPort, path)
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	resp, err := httpProxy.Get(u)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}
	return body, nil
}

// proxyJSON sends method+body to the loopback and returns the body.
func (s *Server) proxyJSON(method, path string, body any) ([]byte, error) {
	if s.webPort == 0 {
		return nil, fmt.Errorf("REST loopback unavailable (web server disabled)")
	}
	u := fmt.Sprintf("http://127.0.0.1:%d%s", s.webPort, path)
	var rdr io.Reader
	if body != nil {
		buf, _ := json.Marshal(body)
		rdr = bytes.NewReader(buf)
	}
	req, err := http.NewRequest(method, u, rdr)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := httpProxy.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(respBody))
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
		return textOK("Error: " + err.Error()), nil
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
		return textOK("Error: " + err.Error()), nil
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
		return textOK("Error: " + err.Error()), nil
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
		return textOK("Error: " + err.Error()), nil
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
		return textOK("Error: " + err.Error()), nil
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
		return textOK("Error: " + err.Error()), nil
	}
	return textOK(string(out)), nil
}

func (s *Server) toolProjectUpsert() mcpsdk.Tool {
	return mcpsdk.NewTool("project_upsert",
		mcpsdk.WithDescription("Register or update a project alias (BL27)."),
		mcpsdk.WithString("name", mcpsdk.Required(), mcpsdk.Description("Project alias")),
		mcpsdk.WithString("dir", mcpsdk.Required(), mcpsdk.Description("Absolute directory")),
		mcpsdk.WithString("default_backend", mcpsdk.Description("Default LLM backend")),
		mcpsdk.WithString("description", mcpsdk.Description("Description")),
	)
}
func (s *Server) handleProjectUpsert(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	body := map[string]any{
		"name":            req.GetString("name", ""),
		"dir":             req.GetString("dir", ""),
		"default_backend": req.GetString("default_backend", ""),
		"description":     req.GetString("description", ""),
	}
	out, err := s.proxyJSON(http.MethodPost, "/api/projects", body)
	if err != nil {
		return textOK("Error: " + err.Error()), nil
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
		return textOK("Error: " + err.Error()), nil
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
		return textOK("Error: " + err.Error()), nil
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
		return textOK("Error: " + err.Error()), nil
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
		return textOK("Error: " + err.Error()), nil
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
		return textOK("Error: " + err.Error()), nil
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
		return textOK("Error: " + err.Error()), nil
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
		return textOK("Error: " + err.Error()), nil
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
		return textOK("Error: " + err.Error()), nil
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
		return textOK("Error: " + err.Error()), nil
	}
	return textOK(string(out)), nil
}

// ----- BL9: audit ----------------------------------------------------------

func (s *Server) toolAuditQuery() mcpsdk.Tool {
	return mcpsdk.NewTool("audit_query",
		mcpsdk.WithDescription("Query the operator audit log (BL9). Newest-first."),
		mcpsdk.WithString("actor", mcpsdk.Description("Filter by actor (e.g. 'operator', 'channel:signal', 'mcp')")),
		mcpsdk.WithString("action", mcpsdk.Description("Filter by action (start, kill, send_input, configure, ...)")),
		mcpsdk.WithString("session_id", mcpsdk.Description("Filter by session_id")),
		mcpsdk.WithString("since", mcpsdk.Description("RFC3339 lower bound")),
		mcpsdk.WithString("until", mcpsdk.Description("RFC3339 upper bound")),
		mcpsdk.WithNumber("limit", mcpsdk.Description("Max entries (default 100)")),
	)
}
func (s *Server) handleAuditQuery(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	q := url.Values{}
	for _, k := range []string{"actor", "action", "session_id", "since", "until"} {
		if v := req.GetString(k, ""); v != "" {
			q.Set(k, v)
		}
	}
	if n := int(req.GetFloat("limit", 0)); n > 0 {
		q.Set("limit", strconv.Itoa(n))
	}
	out, err := s.proxyGet("/api/audit", q)
	if err != nil {
		return textOK("Error: " + err.Error()), nil
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
		return textOK("Error: " + err.Error()), nil
	}
	return textOK(string(out)), nil
}

// ----- BL17: reload --------------------------------------------------------

func (s *Server) toolReload() mcpsdk.Tool {
	return mcpsdk.NewTool("reload",
		mcpsdk.WithDescription("Hot-reload config from disk (BL17). Returns applied + requires_restart fields."),
	)
}
func (s *Server) handleReload(_ context.Context, _ mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	out, err := s.proxyJSON(http.MethodPost, "/api/reload", nil)
	if err != nil {
		return textOK("Error: " + err.Error()), nil
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
		return textOK("Error: " + err.Error()), nil
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
		return textOK("Error: " + err.Error()), nil
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
		return textOK("Error: " + err.Error()), nil
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
		return textOK("Error: " + err.Error()), nil
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
		return textOK("Error: " + err.Error()), nil
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
		return textOK("Error: " + err.Error()), nil
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
		return textOK("Error: " + err.Error()), nil
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
		return textOK("Error: " + err.Error()), nil
	}
	return textOK(string(out)), nil
}
