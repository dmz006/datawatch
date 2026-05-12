// v7.0.0 S2 — MCP tools for the LLM-inference registry. All proxy to REST.

package mcp

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

func (s *Server) toolLLMList() mcpsdk.Tool {
	return mcpsdk.NewTool("llm_list",
		mcpsdk.WithDescription("v7.0.0 S2 — list every LLM in the registry."),
	)
}

func (s *Server) toolLLMGet() mcpsdk.Tool {
	return mcpsdk.NewTool("llm_get",
		mcpsdk.WithDescription("v7.0.0 S2 — fetch one LLM by name."),
		mcpsdk.WithString("name", mcpsdk.Required()),
	)
}

func (s *Server) toolLLMAdd() mcpsdk.Tool {
	return mcpsdk.NewTool("llm_add",
		mcpsdk.WithDescription("v7.0.0 S2 — register a new LLM. Kind: ollama|openwebui|opencode|claude|claude-code|aider|goose|gemini|shell. compute_nodes is comma-separated ordered failover list (local kinds). api_key_ref required for cloud (claude). Session-backend fields (binary, console_cols/rows, output_mode, input_mode, auto_git_init/commit) apply to coding-agent kinds. Claude-code fields (skip_permissions, channel_enabled, auto_accept_disclaimer, permission_mode, default_effort, fallback_chain) only apply to claude-code."),
		mcpsdk.WithString("name", mcpsdk.Required()),
		mcpsdk.WithString("kind", mcpsdk.Required()),
		mcpsdk.WithString("model", mcpsdk.Description("model name")),
		mcpsdk.WithString("compute_nodes", mcpsdk.Description("comma-separated ordered ComputeNode names")),
		mcpsdk.WithString("api_key_ref", mcpsdk.Description("literal key OR ${secret:name}")),
		mcpsdk.WithString("timeout_seconds", mcpsdk.Description("per-call timeout (0=adapter default)")),
		mcpsdk.WithString("tags", mcpsdk.Description("comma-separated user tags")),
		mcpsdk.WithString("auto_add_models", mcpsdk.Description("true/false — auto-append newly-discovered models")),
		// Session-backend fields
		mcpsdk.WithString("binary", mcpsdk.Description("path to CLI binary (session-backend kinds)")),
		mcpsdk.WithString("console_cols", mcpsdk.Description("terminal width override")),
		mcpsdk.WithString("console_rows", mcpsdk.Description("terminal height override")),
		mcpsdk.WithString("output_mode", mcpsdk.Description("output display mode: terminal or log")),
		mcpsdk.WithString("input_mode", mcpsdk.Description("input mode")),
		mcpsdk.WithString("auto_git_init", mcpsdk.Description("true/false — auto-init git repo in project dir")),
		mcpsdk.WithString("auto_git_commit", mcpsdk.Description("true/false — auto-commit after session")),
		// Claude-code-specific
		mcpsdk.WithString("skip_permissions", mcpsdk.Description("true/false — pass --dangerously-skip-permissions (claude-code only)")),
		mcpsdk.WithString("channel_enabled", mcpsdk.Description("true/false — enable MCP channel mode (claude-code only)")),
		mcpsdk.WithString("auto_accept_disclaimer", mcpsdk.Description("true/false — auto-accept startup prompts (claude-code only)")),
		mcpsdk.WithString("permission_mode", mcpsdk.Description("--permission-mode value: default|plan|acceptEdits|auto|bypassPermissions|dontAsk (claude-code only)")),
		mcpsdk.WithString("default_effort", mcpsdk.Description("per-session effort hint: quick|normal|thorough (claude-code only)")),
		mcpsdk.WithString("fallback_chain", mcpsdk.Description("comma-separated ordered profile fallback chain (claude-code only)")),
	)
}

func (s *Server) toolLLMUpdate() mcpsdk.Tool {
	return mcpsdk.NewTool("llm_update",
		mcpsdk.WithDescription("v7.0.0 S2 — replace an existing LLM. Supports all fields from llm_add."),
		mcpsdk.WithString("name", mcpsdk.Required()),
		mcpsdk.WithString("kind", mcpsdk.Required()),
		mcpsdk.WithString("model", mcpsdk.Description("model name")),
		mcpsdk.WithString("compute_nodes", mcpsdk.Description("comma-separated ordered ComputeNode names")),
		mcpsdk.WithString("api_key_ref", mcpsdk.Description("literal key OR ${secret:name}")),
		mcpsdk.WithString("timeout_seconds", mcpsdk.Description("per-call timeout")),
		mcpsdk.WithString("tags", mcpsdk.Description("comma-separated user tags")),
		mcpsdk.WithString("auto_add_models", mcpsdk.Description("true/false — auto-append newly-discovered models")),
		// Session-backend fields
		mcpsdk.WithString("binary", mcpsdk.Description("path to CLI binary (session-backend kinds)")),
		mcpsdk.WithString("console_cols", mcpsdk.Description("terminal width override")),
		mcpsdk.WithString("console_rows", mcpsdk.Description("terminal height override")),
		mcpsdk.WithString("output_mode", mcpsdk.Description("output display mode: terminal or log")),
		mcpsdk.WithString("input_mode", mcpsdk.Description("input mode")),
		mcpsdk.WithString("auto_git_init", mcpsdk.Description("true/false — auto-init git repo in project dir")),
		mcpsdk.WithString("auto_git_commit", mcpsdk.Description("true/false — auto-commit after session")),
		// Claude-code-specific
		mcpsdk.WithString("skip_permissions", mcpsdk.Description("true/false — pass --dangerously-skip-permissions (claude-code only)")),
		mcpsdk.WithString("channel_enabled", mcpsdk.Description("true/false — enable MCP channel mode (claude-code only)")),
		mcpsdk.WithString("auto_accept_disclaimer", mcpsdk.Description("true/false — auto-accept startup prompts (claude-code only)")),
		mcpsdk.WithString("permission_mode", mcpsdk.Description("--permission-mode value (claude-code only)")),
		mcpsdk.WithString("default_effort", mcpsdk.Description("per-session effort hint: quick|normal|thorough (claude-code only)")),
		mcpsdk.WithString("fallback_chain", mcpsdk.Description("comma-separated ordered profile fallback chain (claude-code only)")),
	)
}

func (s *Server) toolLLMDelete() mcpsdk.Tool {
	return mcpsdk.NewTool("llm_delete",
		mcpsdk.WithDescription("v7.0.0 S2 — remove an LLM."),
		mcpsdk.WithString("name", mcpsdk.Required()),
	)
}

func (s *Server) toolLLMTest() mcpsdk.Tool {
	return mcpsdk.NewTool("llm_test",
		mcpsdk.WithDescription("v7.0.0 S2 — send a one-shot inference probe through this LLM. Verifies adapter + ComputeNode reachability."),
		mcpsdk.WithString("name", mcpsdk.Required()),
		mcpsdk.WithString("prompt", mcpsdk.Description("prompt text (default: short reachability probe)")),
	)
}

func (s *Server) handleLLMListMCP(_ context.Context, _ mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	out, err := s.proxyGet("/api/llms", nil)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) handleLLMGetMCP(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	out, err := s.proxyGet("/api/llms/"+mustString(req, "name"), nil)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) handleLLMAddMCP(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	body := llmBodyFromReq(req)
	out, err := s.proxyJSON("POST", "/api/llms", body)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) handleLLMUpdateMCP(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	body := llmBodyFromReq(req)
	out, err := s.proxyJSON("PUT", "/api/llms/"+mustString(req, "name"), body)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) handleLLMDeleteMCP(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	out, err := s.proxyJSON("DELETE", "/api/llms/"+mustString(req, "name"), nil)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) toolLLMEnable() mcpsdk.Tool {
	return mcpsdk.NewTool("llm_enable",
		mcpsdk.WithDescription("v7.0.0-alpha.20 — enable an LLM. Optionally run a one-shot probe before flipping enabled=true."),
		mcpsdk.WithString("name", mcpsdk.Required()),
		mcpsdk.WithString("pretest", mcpsdk.Description("set true to probe before enabling")),
	)
}

func (s *Server) toolLLMDisable() mcpsdk.Tool {
	return mcpsdk.NewTool("llm_disable",
		mcpsdk.WithDescription("v7.0.0-alpha.20 — disable an LLM. Dispatcher refuses to route through it until re-enabled."),
		mcpsdk.WithString("name", mcpsdk.Required()),
	)
}

func (s *Server) handleLLMEnableMCP(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	body := map[string]any{"enabled": true}
	if v := optString(req, "pretest"); strings.EqualFold(v, "true") || v == "1" {
		body["pretest"] = true
	}
	out, err := s.proxyJSON("PATCH", "/api/llms/"+mustString(req, "name")+"/enabled", body)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) handleLLMDisableMCP(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	body := map[string]any{"enabled": false}
	out, err := s.proxyJSON("PATCH", "/api/llms/"+mustString(req, "name")+"/enabled", body)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) handleLLMTestMCP(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	body := map[string]any{}
	if p := optString(req, "prompt"); p != "" {
		body["prompt"] = p
	}
	out, err := s.proxyJSON("POST", "/api/llms/"+mustString(req, "name")+"/test", body)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func llmBodyFromReq(req mcpsdk.CallToolRequest) map[string]any {
	body := map[string]any{
		"name":  mustString(req, "name"),
		"kind":  optString(req, "kind"),
		"model": optString(req, "model"),
	}
	if v := optString(req, "compute_nodes"); v != "" {
		nodes := []string{}
		for _, n := range strings.Split(v, ",") {
			if n = strings.TrimSpace(n); n != "" {
				nodes = append(nodes, n)
			}
		}
		body["compute_nodes"] = nodes
	}
	if v := optString(req, "api_key_ref"); v != "" {
		body["api_key_ref"] = v
	}
	if v := optString(req, "timeout_seconds"); v != "" {
		var n int
		_, _ = fmtSscanf(v, &n)
		body["timeout_seconds"] = n
	}
	// Tags (bug fix: was missing from llm_add/llm_update).
	if v := optString(req, "tags"); v != "" {
		tags := []string{}
		for _, t := range strings.Split(v, ",") {
			if t = strings.TrimSpace(t); t != "" {
				tags = append(tags, t)
			}
		}
		body["tags"] = tags
	}
	// auto_add_models (bug fix: was missing).
	if v := optString(req, "auto_add_models"); v != "" {
		body["auto_add_models"] = strings.EqualFold(v, "true") || v == "1"
	}
	// Session-backend fields.
	if v := optString(req, "binary"); v != "" {
		body["binary"] = v
	}
	if v := optString(req, "console_cols"); v != "" {
		var n int
		_, _ = fmtSscanf(v, &n)
		body["console_cols"] = n
	}
	if v := optString(req, "console_rows"); v != "" {
		var n int
		_, _ = fmtSscanf(v, &n)
		body["console_rows"] = n
	}
	if v := optString(req, "output_mode"); v != "" {
		body["output_mode"] = v
	}
	if v := optString(req, "input_mode"); v != "" {
		body["input_mode"] = v
	}
	if v := optString(req, "auto_git_init"); v != "" {
		body["auto_git_init"] = strings.EqualFold(v, "true") || v == "1"
	}
	if v := optString(req, "auto_git_commit"); v != "" {
		body["auto_git_commit"] = strings.EqualFold(v, "true") || v == "1"
	}
	// Claude-code-specific fields.
	if v := optString(req, "skip_permissions"); v != "" {
		body["skip_permissions"] = strings.EqualFold(v, "true") || v == "1"
	}
	if v := optString(req, "channel_enabled"); v != "" {
		body["channel_enabled"] = strings.EqualFold(v, "true") || v == "1"
	}
	if v := optString(req, "auto_accept_disclaimer"); v != "" {
		body["auto_accept_disclaimer"] = strings.EqualFold(v, "true") || v == "1"
	}
	if v := optString(req, "permission_mode"); v != "" {
		body["permission_mode"] = v
	}
	if v := optString(req, "default_effort"); v != "" {
		body["default_effort"] = v
	}
	if v := optString(req, "fallback_chain"); v != "" {
		chain := []string{}
		for _, p := range strings.Split(v, ",") {
			if p = strings.TrimSpace(p); p != "" {
				chain = append(chain, p)
			}
		}
		body["fallback_chain"] = chain
	}
	return body
}

func fmtSscanf(s string, dst *int) (int, error) {
	var n int
	for i, c := range s {
		if c < '0' || c > '9' {
			return i, nil
		}
		n = n*10 + int(c-'0')
	}
	*dst = n
	return len(s), nil
}

// ---------------------------------------------------------------------------
// v7.0.0-alpha.37 — Enabled Models + in_use MCP tools
// ---------------------------------------------------------------------------

func (s *Server) toolLLMInUse() mcpsdk.Tool {
	return mcpsdk.NewTool("llm_in_use",
		mcpsdk.WithDescription("alpha.37 — list sessions, automata, and personas bound to this LLM."),
		mcpsdk.WithString("name", mcpsdk.Required()),
		mcpsdk.WithString("filter", mcpsdk.Description("AND-substring filter across id/name/state")),
		mcpsdk.WithString("page", mcpsdk.Description("page number (1-based)")),
		mcpsdk.WithString("size", mcpsdk.Description("page size: 5, 10, or 50")),
	)
}

func (s *Server) toolLLMRefreshModels() mcpsdk.Tool {
	return mcpsdk.NewTool("llm_refresh_models",
		mcpsdk.WithDescription("alpha.37 — trigger a model-list refresh from the LLM's ComputeNodes."),
		mcpsdk.WithString("name", mcpsdk.Required()),
	)
}

func (s *Server) toolLLMAddModel() mcpsdk.Tool {
	return mcpsdk.NewTool("llm_add_model",
		mcpsdk.WithDescription("alpha.37 — add an enabled model (node+model pair) to an LLM."),
		mcpsdk.WithString("llm", mcpsdk.Required()),
		mcpsdk.WithString("node", mcpsdk.Description("ComputeNode name (empty for SaaS kinds)")),
		mcpsdk.WithString("model", mcpsdk.Required()),
	)
}

func (s *Server) toolLLMRemoveModel() mcpsdk.Tool {
	return mcpsdk.NewTool("llm_remove_model",
		mcpsdk.WithDescription("alpha.37 — remove an enabled model from an LLM."),
		mcpsdk.WithString("llm", mcpsdk.Required()),
		mcpsdk.WithString("node", mcpsdk.Description("ComputeNode name")),
		mcpsdk.WithString("model", mcpsdk.Required()),
	)
}

func (s *Server) toolLLMListModels() mcpsdk.Tool {
	return mcpsdk.NewTool("llm_list_models",
		mcpsdk.WithDescription("alpha.37 — list enabled models for an LLM."),
		mcpsdk.WithString("name", mcpsdk.Required()),
	)
}

func (s *Server) handleLLMInUseMCP(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	name := mustString(req, "name")
	q := url.Values{}
	if v := optString(req, "filter"); v != "" {
		q.Set("filter", v)
	}
	if v := optString(req, "page"); v != "" {
		q.Set("page", v)
	}
	if v := optString(req, "size"); v != "" {
		q.Set("size", v)
	}
	out, err := s.proxyGet("/api/llms/"+name+"/in_use", q)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) handleLLMRefreshModelsMCP(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	out, err := s.proxyJSON("POST", "/api/llms/"+mustString(req, "name")+"/refresh_models", map[string]any{})
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

// handleLLMAddModelMCP: GET the LLM, append the model, PUT it back.
func (s *Server) handleLLMAddModelMCP(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	llmName := mustString(req, "llm")
	model := mustString(req, "model")
	node := optString(req, "node")

	raw, err := s.proxyGet("/api/llms/"+llmName, nil)
	if err != nil {
		return nil, err
	}
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, err
	}
	models, _ := body["models"].([]any)
	models = append(models, map[string]any{"node": node, "model": model})
	body["models"] = models

	out, err := s.proxyJSON("PUT", "/api/llms/"+llmName, body)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

// handleLLMRemoveModelMCP: GET the LLM, remove the model, PUT it back.
func (s *Server) handleLLMRemoveModelMCP(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	llmName := mustString(req, "llm")
	model := mustString(req, "model")
	node := optString(req, "node")

	raw, err := s.proxyGet("/api/llms/"+llmName, nil)
	if err != nil {
		return nil, err
	}
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, err
	}
	models, _ := body["models"].([]any)
	var kept []any
	for _, m := range models {
		if mm, ok := m.(map[string]any); ok {
			if mm["model"] == model && (node == "" || mm["node"] == node) {
				continue
			}
		}
		kept = append(kept, m)
	}
	body["models"] = kept

	out, err := s.proxyJSON("PUT", "/api/llms/"+llmName, body)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

// handleLLMListModelsMCP: GET the LLM, return just the models field.
func (s *Server) handleLLMListModelsMCP(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	raw, err := s.proxyGet("/api/llms/"+mustString(req, "name"), nil)
	if err != nil {
		return nil, err
	}
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, err
	}
	models := body["models"]
	if models == nil {
		models = []any{}
	}
	out, _ := json.Marshal(map[string]any{"models": models})
	return textOK(string(out)), nil
}
