// v7.0.0 S2 — MCP tools for the LLM-inference registry. All proxy to REST.

package mcp

import (
	"context"
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
