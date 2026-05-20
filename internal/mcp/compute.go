// v7.0.0 S1 — MCP tools for the ComputeNode registry. All proxy to REST.

package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

func (s *Server) toolComputeNodeList() mcpsdk.Tool {
	return mcpsdk.NewTool("compute_node_list",
		mcpsdk.WithDescription("v7.0.0 S1 — list every ComputeNode in the registry."),
	)
}

func (s *Server) toolComputeNodeGet() mcpsdk.Tool {
	return mcpsdk.NewTool("compute_node_get",
		mcpsdk.WithDescription("v7.0.0 S1 — fetch one ComputeNode by name."),
		mcpsdk.WithString("name", mcpsdk.Required(), mcpsdk.Description("ComputeNode name")),
	)
}

func (s *Server) toolComputeNodeAdd() mcpsdk.Tool {
	return mcpsdk.NewTool("compute_node_add",
		mcpsdk.WithDescription("v8.0 BL322 — register a new ComputeNode. kind = ollama|openai-compat|gemini-api|opencode-api. routing = direct|docker-network|datawatch-proxy. Pass routing_docker_network_json or routing_datawatch_proxy_json as JSON strings for sub-config."),
		mcpsdk.WithString("name", mcpsdk.Required(), mcpsdk.Description("kebab-case name")),
		mcpsdk.WithString("kind", mcpsdk.Required(), mcpsdk.Description("ollama|openai-compat|gemini-api|opencode-api")),
		mcpsdk.WithString("address", mcpsdk.Description("host:port or URL (not used for docker-network / datawatch-proxy routing)")),
		mcpsdk.WithString("routing", mcpsdk.Description("direct|docker-network|datawatch-proxy (default: direct)")),
		mcpsdk.WithString("routing_docker_network_json", mcpsdk.Description(`JSON for docker-network config, e.g. {"image":"ollama/ollama","network_name":"datawatch-llm","port":11434,"auto_start":true}`)),
		mcpsdk.WithString("routing_datawatch_proxy_json", mcpsdk.Description(`JSON for datawatch-proxy config, e.g. {"peer":"remote-1","remote_llm_name":"llama3","timeout_seconds":30}`)),
		mcpsdk.WithString("monitoring_endpoint", mcpsdk.Description("stub --listen URL for on-demand detail")),
		mcpsdk.WithString("max_concurrent_models", mcpsdk.Description("declared capacity")),
		mcpsdk.WithString("gpu_mem_gb", mcpsdk.Description("declared GPU memory in GB")),
		mcpsdk.WithString("scheduling_priority", mcpsdk.Description("0-100, default 50")),
		mcpsdk.WithString("tags", mcpsdk.Description("comma-separated tags")),
	)
}

func (s *Server) toolComputeNodeUpdate() mcpsdk.Tool {
	return mcpsdk.NewTool("compute_node_update",
		mcpsdk.WithDescription("v8.0 BL322 — replace an existing ComputeNode. Same fields as compute_node_add including routing, routing_docker_network_json, routing_datawatch_proxy_json."),
		mcpsdk.WithString("name", mcpsdk.Required(), mcpsdk.Description("ComputeNode name")),
		mcpsdk.WithString("kind", mcpsdk.Required(), mcpsdk.Description("ollama|openai-compat|gemini-api|opencode-api")),
		mcpsdk.WithString("address", mcpsdk.Description("host:port or URL")),
		mcpsdk.WithString("routing", mcpsdk.Description("direct|docker-network|datawatch-proxy")),
		mcpsdk.WithString("routing_docker_network_json", mcpsdk.Description(`JSON for docker-network config`)),
		mcpsdk.WithString("routing_datawatch_proxy_json", mcpsdk.Description(`JSON for datawatch-proxy config`)),
		mcpsdk.WithString("monitoring_endpoint", mcpsdk.Description("stub --listen URL")),
		mcpsdk.WithString("max_concurrent_models", mcpsdk.Description("declared capacity")),
		mcpsdk.WithString("gpu_mem_gb", mcpsdk.Description("declared GPU memory in GB")),
		mcpsdk.WithString("scheduling_priority", mcpsdk.Description("0-100")),
		mcpsdk.WithString("tags", mcpsdk.Description("comma-separated tags")),
	)
}

func (s *Server) toolComputeNodeDelete() mcpsdk.Tool {
	return mcpsdk.NewTool("compute_node_delete",
		mcpsdk.WithDescription("v7.0.0 S1 — remove a ComputeNode from the registry."),
		mcpsdk.WithString("name", mcpsdk.Required(), mcpsdk.Description("ComputeNode name")),
	)
}

func (s *Server) toolComputeNodeHealth() mcpsdk.Tool {
	return mcpsdk.NewTool("compute_node_health",
		mcpsdk.WithDescription("v7.0.0 S1 — declared capacity + maintenance state for a ComputeNode."),
		mcpsdk.WithString("name", mcpsdk.Required(), mcpsdk.Description("ComputeNode name")),
	)
}

func (s *Server) toolComputeNodeDetail() mcpsdk.Tool {
	return mcpsdk.NewTool("compute_node_detail",
		mcpsdk.WithDescription("v7.0.0 S1 — on-demand pull from the Node's monitoring sidecar (--listen). Use for live process drill-down."),
		mcpsdk.WithString("name", mcpsdk.Required(), mcpsdk.Description("ComputeNode name")),
	)
}

func (s *Server) handleComputeNodeListMCP(_ context.Context, _ mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	out, err := s.proxyGet("/api/compute/nodes", nil)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) handleComputeNodeGetMCP(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	out, err := s.proxyGet("/api/compute/nodes/"+mustString(req, "name"), nil)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) handleComputeNodeAddMCP(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	body := computeBodyFromReq(req)
	out, err := s.proxyJSON("POST", "/api/compute/nodes", body)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) handleComputeNodeUpdateMCP(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	body := computeBodyFromReq(req)
	name := mustString(req, "name")
	out, err := s.proxyJSON("PUT", "/api/compute/nodes/"+name, body)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) handleComputeNodeDeleteMCP(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	out, err := s.proxyJSON("DELETE", "/api/compute/nodes/"+mustString(req, "name"), nil)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) handleComputeNodeHealthMCP(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	out, err := s.proxyGet("/api/compute/nodes/"+mustString(req, "name")+"/health", nil)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) handleComputeNodeDetailMCP(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	out, err := s.proxyGet("/api/compute/nodes/"+mustString(req, "name")+"/detail", nil)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

// alpha.23b — observer-peer attach/detach + free-list MCP surface.

func (s *Server) toolObserverPeersFree() mcpsdk.Tool {
	return mcpsdk.NewTool("observer_peers_free",
		mcpsdk.WithDescription("v7.0.0-alpha.23b — list registered observer peers with NO bound ComputeNode. Used to discover candidates for compute_node_attach_observer."),
	)
}

func (s *Server) handleObserverPeersFreeMCP(_ context.Context, _ mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	out, err := s.proxyGet("/api/observer/peers/free", nil)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) toolComputeNodeAttachObserver() mcpsdk.Tool {
	return mcpsdk.NewTool("compute_node_attach_observer",
		mcpsdk.WithDescription("v7.0.0-alpha.23b — attach a registered observer peer (datawatch-stats) to a ComputeNode. Operator-driven; observer-down does NOT auto-detach."),
		mcpsdk.WithString("name", mcpsdk.Required(), mcpsdk.Description("ComputeNode name")),
		mcpsdk.WithString("peer", mcpsdk.Required(), mcpsdk.Description("registered observer peer name")),
	)
}

func (s *Server) handleComputeNodeAttachObserverMCP(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	body := map[string]any{"peer": mustString(req, "peer")}
	out, err := s.proxyJSON("PUT", "/api/compute/nodes/"+mustString(req, "name")+"/observer-peer", body)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) toolComputeNodeDetachObserver() mcpsdk.Tool {
	return mcpsdk.NewTool("compute_node_detach_observer",
		mcpsdk.WithDescription("v7.0.0-alpha.23b — clear the observer-peer binding on a ComputeNode. Node continues to work from declared config."),
		mcpsdk.WithString("name", mcpsdk.Required(), mcpsdk.Description("ComputeNode name")),
	)
}

func (s *Server) handleComputeNodeDetachObserverMCP(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	out, err := s.proxyJSON("DELETE", "/api/compute/nodes/"+mustString(req, "name")+"/observer-peer", nil)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

// alpha.24 #231 — observer peers grouped by ComputeNode + federation meta-peers.

func (s *Server) toolObserverPeersByNode() mcpsdk.Tool {
	return mcpsdk.NewTool("observer_peers_by_node",
		mcpsdk.WithDescription("v7.0.0-alpha.24 #231 — local observer peers grouped by their bound ComputeNode."),
	)
}

func (s *Server) handleObserverPeersByNodeMCP(_ context.Context, _ mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	out, err := s.proxyGet("/api/observer/peers/by-node", nil)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) toolFederationMetaPeers() mcpsdk.Tool {
	return mcpsdk.NewTool("federation_meta_peers",
		mcpsdk.WithDescription("v7.0.0-alpha.24 #231 — federation meta-peers view: peers grouped by ComputeNode across primaries (cross-instance fanout in alpha.24b)."),
	)
}

func (s *Server) handleFederationMetaPeersMCP(_ context.Context, _ mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	out, err := s.proxyGet("/api/federation/meta-peers", nil)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

// alpha.33 #244 — Ollama marketplace.

func (s *Server) toolMarketplaceCatalog() mcpsdk.Tool {
	return mcpsdk.NewTool("marketplace_ollama_catalog",
		mcpsdk.WithDescription("v7.0.0-alpha.33 #244 — embedded curated Ollama model catalog (refresh from ollama.com is POST v7.0)."),
	)
}

func (s *Server) handleMarketplaceCatalogMCP(_ context.Context, _ mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	out, err := s.proxyGet("/api/marketplace/ollama/catalog", nil)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) toolComputeNodePullModel() mcpsdk.Tool {
	return mcpsdk.NewTool("compute_node_pull_model",
		mcpsdk.WithDescription("v7.0.0-alpha.33 #244 — start a background Ollama model pull on a Compute Node. Returns task descriptor."),
		mcpsdk.WithString("name", mcpsdk.Required(), mcpsdk.Description("ComputeNode name")),
		mcpsdk.WithString("model", mcpsdk.Required(), mcpsdk.Description("Model name with optional tag, e.g. 'llama3.1:8b'")),
	)
}

func (s *Server) handleComputeNodePullModelMCP(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	body := map[string]any{"model": mustString(req, "model")}
	out, err := s.proxyJSON("POST", "/api/compute/nodes/"+mustString(req, "name")+"/models/pull", body)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) toolComputeNodeRemoveModel() mcpsdk.Tool {
	return mcpsdk.NewTool("compute_node_remove_model",
		mcpsdk.WithDescription("v7.0.0-alpha.33 #244 — remove an Ollama model from a Compute Node."),
		mcpsdk.WithString("name", mcpsdk.Required(), mcpsdk.Description("ComputeNode name")),
		mcpsdk.WithString("model", mcpsdk.Required(), mcpsdk.Description("Model name with tag, e.g. 'llama3.1:8b'")),
	)
}

func (s *Server) handleComputeNodeRemoveModelMCP(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	out, err := s.proxyJSON("DELETE", "/api/compute/nodes/"+mustString(req, "name")+"/models/"+mustString(req, "model"), nil)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) toolMarketplacePullTask() mcpsdk.Tool {
	return mcpsdk.NewTool("marketplace_pull_task",
		mcpsdk.WithDescription("v7.0.0-alpha.33 #244 — poll a model-pull task by ID."),
		mcpsdk.WithString("task_id", mcpsdk.Required(), mcpsdk.Description("Task ID returned by compute_node_pull_model")),
	)
}

func (s *Server) handleMarketplacePullTaskMCP(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	out, err := s.proxyGet("/api/marketplace/ollama/tasks/"+mustString(req, "task_id"), nil)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

// computeBodyFromReq translates the loosely-typed MCP string params
// into a compute.Node JSON body. Numeric fields fall back to 0 on
// parse failure; the REST validator rejects negatives.
func computeBodyFromReq(req mcpsdk.CallToolRequest) map[string]any {
	body := map[string]any{
		"name":                mustString(req, "name"),
		"kind":                optString(req, "kind"),
		"address":             optString(req, "address"),
		"monitoring_endpoint": optString(req, "monitoring_endpoint"),
	}
	if v := optString(req, "routing"); v != "" {
		body["routing"] = v
	}
	// BL322-S5: decode optional JSON sub-config strings.
	if v := optString(req, "routing_docker_network_json"); v != "" {
		var sub map[string]any
		if err := json.Unmarshal([]byte(v), &sub); err == nil {
			body["routing_docker_network"] = sub
		}
	}
	if v := optString(req, "routing_datawatch_proxy_json"); v != "" {
		var sub map[string]any
		if err := json.Unmarshal([]byte(v), &sub); err == nil {
			body["routing_datawatch_proxy"] = sub
		}
	}
	if v := optString(req, "scheduling_priority"); v != "" {
		var n int
		_, _ = fmt.Sscanf(v, "%d", &n)
		body["scheduling_priority"] = n
	}
	cap := map[string]any{}
	if v := optString(req, "max_concurrent_models"); v != "" {
		var n int
		_, _ = fmt.Sscanf(v, "%d", &n)
		cap["max_concurrent_models"] = n
	}
	if v := optString(req, "gpu_mem_gb"); v != "" {
		var n int
		_, _ = fmt.Sscanf(v, "%d", &n)
		cap["gpu_mem_gb"] = n
	}
	if len(cap) > 0 {
		body["declared_capacity"] = cap
	}
	if tags := optString(req, "tags"); tags != "" {
		body["tags"] = strings.Split(tags, ",")
	}
	return body
}

var _ = json.Marshal
