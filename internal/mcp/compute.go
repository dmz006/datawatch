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
		mcpsdk.WithDescription("v7.0.0 S1 — register a new ComputeNode. kind = local|ssh|docker|k8s|remote|remote-proxy. address required for ssh/remote/remote-proxy."),
		mcpsdk.WithString("name", mcpsdk.Required(), mcpsdk.Description("kebab-case name")),
		mcpsdk.WithString("kind", mcpsdk.Required(), mcpsdk.Description("local|ssh|docker|k8s|remote|remote-proxy")),
		mcpsdk.WithString("address", mcpsdk.Description("host:port or URL")),
		mcpsdk.WithString("monitoring_endpoint", mcpsdk.Description("stub --listen URL for on-demand detail")),
		mcpsdk.WithString("max_concurrent_models", mcpsdk.Description("declared capacity")),
		mcpsdk.WithString("gpu_mem_gb", mcpsdk.Description("declared GPU memory in GB")),
		mcpsdk.WithString("scheduling_priority", mcpsdk.Description("0-100, default 50")),
		mcpsdk.WithString("tags", mcpsdk.Description("comma-separated tags")),
	)
}

func (s *Server) toolComputeNodeUpdate() mcpsdk.Tool {
	return mcpsdk.NewTool("compute_node_update",
		mcpsdk.WithDescription("v7.0.0 S1 — replace an existing ComputeNode. Same fields as compute_node_add."),
		mcpsdk.WithString("name", mcpsdk.Required(), mcpsdk.Description("ComputeNode name")),
		mcpsdk.WithString("kind", mcpsdk.Required(), mcpsdk.Description("local|ssh|docker|k8s|remote|remote-proxy")),
		mcpsdk.WithString("address", mcpsdk.Description("host:port or URL")),
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
	if v := optString(req, "scheduling_priority"); v != "" {
		var n int
		fmt.Sscanf(v, "%d", &n)
		body["scheduling_priority"] = n
	}
	cap := map[string]any{}
	if v := optString(req, "max_concurrent_models"); v != "" {
		var n int
		fmt.Sscanf(v, "%d", &n)
		cap["max_concurrent_models"] = n
	}
	if v := optString(req, "gpu_mem_gb"); v != "" {
		var n int
		fmt.Sscanf(v, "%d", &n)
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
