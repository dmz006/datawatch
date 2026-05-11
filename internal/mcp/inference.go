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
		mcpsdk.WithDescription("v7.0.0 S2 — register a new LLM. Kind: ollama|openwebui|opencode|claude. compute_nodes is comma-separated ordered failover list (local kinds). api_key_ref required for cloud (claude)."),
		mcpsdk.WithString("name", mcpsdk.Required()),
		mcpsdk.WithString("kind", mcpsdk.Required()),
		mcpsdk.WithString("model", mcpsdk.Description("model name")),
		mcpsdk.WithString("compute_nodes", mcpsdk.Description("comma-separated ordered ComputeNode names")),
		mcpsdk.WithString("api_key_ref", mcpsdk.Description("literal key OR ${secret:name}")),
		mcpsdk.WithString("timeout_seconds", mcpsdk.Description("per-call timeout (0=adapter default)")),
	)
}

func (s *Server) toolLLMUpdate() mcpsdk.Tool {
	return mcpsdk.NewTool("llm_update",
		mcpsdk.WithDescription("v7.0.0 S2 — replace an existing LLM."),
		mcpsdk.WithString("name", mcpsdk.Required()),
		mcpsdk.WithString("kind", mcpsdk.Required()),
		mcpsdk.WithString("model", mcpsdk.Description("model name")),
		mcpsdk.WithString("compute_nodes", mcpsdk.Description("comma-separated ordered ComputeNode names")),
		mcpsdk.WithString("api_key_ref", mcpsdk.Description("literal key OR ${secret:name}")),
		mcpsdk.WithString("timeout_seconds", mcpsdk.Description("per-call timeout")),
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
