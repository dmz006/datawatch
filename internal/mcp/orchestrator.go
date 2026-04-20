// BL117 (v4.0.0) — MCP-tool parity for the PRD-DAG orchestrator.

package mcp

import (
	"context"
	"encoding/json"
	"net/http"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

func (s *Server) toolOrchestratorConfigGet() mcpsdk.Tool {
	return mcpsdk.NewTool("orchestrator_config_get",
		mcpsdk.WithDescription("BL117 — read orchestrator config (default guardrails, timeouts, backends, parallelism)."),
	)
}
func (s *Server) handleOrchestratorConfigGet(_ context.Context, _ mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	out, err := s.proxyGet("/api/orchestrator/config", nil)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) toolOrchestratorConfigSet() mcpsdk.Tool {
	return mcpsdk.NewTool("orchestrator_config_set",
		mcpsdk.WithDescription("BL117 — replace orchestrator config (full body)."),
		mcpsdk.WithBoolean("enabled"),
		mcpsdk.WithString("guardrail_backend"),
		mcpsdk.WithNumber("guardrail_timeout_ms"),
		mcpsdk.WithNumber("max_parallel_prds"),
	)
}
func (s *Server) handleOrchestratorConfigSet(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	body := map[string]any{}
	if v := req.GetBool("enabled", false); v {
		body["enabled"] = v
	}
	if v := req.GetString("guardrail_backend", ""); v != "" {
		body["guardrail_backend"] = v
	}
	if v := req.GetFloat("guardrail_timeout_ms", 0); v != 0 {
		body["guardrail_timeout_ms"] = int(v)
	}
	if v := req.GetFloat("max_parallel_prds", 0); v != 0 {
		body["max_parallel_prds"] = int(v)
	}
	out, err := s.proxyJSON(http.MethodPut, "/api/orchestrator/config", body)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) toolOrchestratorGraphList() mcpsdk.Tool {
	return mcpsdk.NewTool("orchestrator_graph_list",
		mcpsdk.WithDescription("BL117 — list all PRD-DAG graphs newest-first."),
	)
}
func (s *Server) handleOrchestratorGraphList(_ context.Context, _ mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	out, err := s.proxyGet("/api/orchestrator/graphs", nil)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) toolOrchestratorGraphCreate() mcpsdk.Tool {
	return mcpsdk.NewTool("orchestrator_graph_create",
		mcpsdk.WithDescription("BL117 — create a PRD-DAG graph from a set of BL24 PRD IDs with optional dependencies."),
		mcpsdk.WithString("title", mcpsdk.Required(), mcpsdk.Description("Graph title")),
		mcpsdk.WithString("project_dir", mcpsdk.Description("Project directory")),
		mcpsdk.WithString("prd_ids", mcpsdk.Required(), mcpsdk.Description(`JSON array of PRD IDs, e.g. ["abc","def"]`)),
		mcpsdk.WithString("deps", mcpsdk.Description(`Optional JSON object mapping prd_id -> [prd_id_that_must_complete_first], e.g. {"b":["a"]}`)),
	)
}
func (s *Server) handleOrchestratorGraphCreate(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	var prdIDs []string
	if err := json.Unmarshal([]byte(req.GetString("prd_ids", "[]")), &prdIDs); err != nil {
		return nil, err
	}
	var deps map[string][]string
	if raw := req.GetString("deps", ""); raw != "" {
		if err := json.Unmarshal([]byte(raw), &deps); err != nil {
			return nil, err
		}
	}
	body := map[string]any{
		"title":       req.GetString("title", ""),
		"project_dir": req.GetString("project_dir", ""),
		"prd_ids":     prdIDs,
	}
	if deps != nil {
		body["deps"] = deps
	}
	out, err := s.proxyJSON(http.MethodPost, "/api/orchestrator/graphs", body)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) toolOrchestratorGraphGet() mcpsdk.Tool {
	return mcpsdk.NewTool("orchestrator_graph_get",
		mcpsdk.WithDescription("BL117 — fetch one graph with nodes + verdicts."),
		mcpsdk.WithString("id", mcpsdk.Required()),
	)
}
func (s *Server) handleOrchestratorGraphGet(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	out, err := s.proxyGet("/api/orchestrator/graphs/"+req.GetString("id", ""), nil)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) toolOrchestratorGraphPlan() mcpsdk.Tool {
	return mcpsdk.NewTool("orchestrator_graph_plan",
		mcpsdk.WithDescription("BL117 — (re)build the node tree for a graph. Creates PRD + guardrail nodes with configured dependencies."),
		mcpsdk.WithString("id", mcpsdk.Required()),
		mcpsdk.WithString("deps", mcpsdk.Description("Optional JSON deps map")),
	)
}
func (s *Server) handleOrchestratorGraphPlan(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	id := req.GetString("id", "")
	body := map[string]any{}
	if raw := req.GetString("deps", ""); raw != "" {
		var deps map[string][]string
		if err := json.Unmarshal([]byte(raw), &deps); err != nil {
			return nil, err
		}
		body["deps"] = deps
	}
	out, err := s.proxyJSON(http.MethodPost, "/api/orchestrator/graphs/"+id+"/plan", body)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) toolOrchestratorGraphRun() mcpsdk.Tool {
	return mcpsdk.NewTool("orchestrator_graph_run",
		mcpsdk.WithDescription("BL117 — kick the runner for a graph (fire-and-forget; poll graph_get for progress)."),
		mcpsdk.WithString("id", mcpsdk.Required()),
	)
}
func (s *Server) handleOrchestratorGraphRun(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	id := req.GetString("id", "")
	out, err := s.proxyJSON(http.MethodPost, "/api/orchestrator/graphs/"+id+"/run", nil)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) toolOrchestratorGraphCancel() mcpsdk.Tool {
	return mcpsdk.NewTool("orchestrator_graph_cancel",
		mcpsdk.WithDescription("BL117 — cancel + archive a graph."),
		mcpsdk.WithString("id", mcpsdk.Required()),
	)
}
func (s *Server) handleOrchestratorGraphCancel(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	id := req.GetString("id", "")
	out, err := s.proxyJSON(http.MethodDelete, "/api/orchestrator/graphs/"+id, nil)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) toolOrchestratorVerdicts() mcpsdk.Tool {
	return mcpsdk.NewTool("orchestrator_verdicts",
		mcpsdk.WithDescription("BL117 — list guardrail verdicts across all graphs."),
	)
}
func (s *Server) handleOrchestratorVerdicts(_ context.Context, _ mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	out, err := s.proxyGet("/api/orchestrator/verdicts", nil)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}
