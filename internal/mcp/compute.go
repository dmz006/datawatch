// v7.0.0 S1 — MCP tools for the ComputeNode registry. All proxy to REST.

package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strings"

	"github.com/dmz006/datawatch/internal/federation"
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
		mcpsdk.WithString("session_slots", mcpsdk.Description("operator-set worker count for this node (0 = derive from observer GPU data: one slot per GPU, limited by free GPU memory). Capped by max_concurrent_sessions when set.")),
		mcpsdk.WithString("tags", mcpsdk.Description("comma-separated tags")),
		mcpsdk.WithString("node_pinned_model", mcpsdk.Description("pin this node to one model. Requests with no explicit model run on it; an explicit request for another model is refused unless allow_model_swap is true. Empty = no pin.")),
		mcpsdk.WithBoolean("allow_model_swap", mcpsdk.Description("let an explicit request replace the pinned model. Default false. Ignored when no node_pinned_model is set.")),
		mcpsdk.WithString("tls_ca_file", mcpsdk.Description("path to a PEM CA bundle trusted in addition to the system roots when connecting to this node over https. Cannot be combined with tls_insecure_skip_verify. Empty = system roots only.")),
		mcpsdk.WithString("tls_cert_fingerprint", mcpsdk.Description("SHA-256 fingerprint of this node's leaf certificate (64 hex chars, colons allowed). Cannot be combined with tls_insecure_skip_verify. Empty = no pin.")),
		mcpsdk.WithBoolean("tls_insecure_skip_verify", mcpsdk.Description("turn certificate checking off for this node. Default false. Every use is audited and shown in the PWA. Cannot be combined with tls_ca_file or tls_cert_fingerprint.")),
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
		mcpsdk.WithString("max_concurrent_sessions", mcpsdk.Description("cap on autonomous sessions in flight on this node (0 = unlimited)")),
		mcpsdk.WithString("session_slots", mcpsdk.Description("operator-set worker count for this node (0 = derive from observer GPU data: one slot per GPU, limited by free GPU memory). Capped by max_concurrent_sessions when set. Update replaces the whole node, so omitting this clears the override.")),
		mcpsdk.WithString("tags", mcpsdk.Description("comma-separated tags")),
		mcpsdk.WithString("node_pinned_model", mcpsdk.Description("pin this node to one model. Update replaces the whole node, so omitting this clears the pin. Empty = no pin.")),
		mcpsdk.WithBoolean("allow_model_swap", mcpsdk.Description("let an explicit request replace the pinned model. Default false. Update replaces the whole node, so omitting this resets it to false.")),
		mcpsdk.WithString("tls_ca_file", mcpsdk.Description("path to a PEM CA bundle trusted in addition to the system roots when connecting to this node over https. Update replaces the whole node, so omitting this clears it. Cannot be combined with tls_insecure_skip_verify.")),
		mcpsdk.WithString("tls_cert_fingerprint", mcpsdk.Description("SHA-256 fingerprint of this node's leaf certificate (64 hex chars, colons allowed). Update replaces the whole node, so omitting this clears the pin. Cannot be combined with tls_insecure_skip_verify.")),
		mcpsdk.WithBoolean("tls_insecure_skip_verify", mcpsdk.Description("turn certificate checking off for this node. Default false. Every use is audited and shown in the PWA. Update replaces the whole node, so omitting this resets it to false (verification back on). Cannot be combined with tls_ca_file or tls_cert_fingerprint.")),
	)
}

// compute_pin_settings reads or sets the pin keeper interval. It is the
// one pin setting that is global rather than per node.
func (s *Server) toolComputePinSettings() mcpsdk.Tool {
	return mcpsdk.NewTool("compute_pin_settings",
		mcpsdk.WithDescription("Read or set pin_check_seconds, how often the pin keeper re-verifies each pinned compute node. Omit pin_check_seconds to read it. Set it to a whole number of seconds >= 0 (0 turns the keeper off). Writing requires mcp.allow_self_config=true. A change takes effect after the next daemon restart. Per-node pins are set with compute_node_add / compute_node_update and read with compute_node_get."),
		mcpsdk.WithNumber("pin_check_seconds", mcpsdk.Description("seconds between pin keeper checks. 0 = off. Omit to read the current value.")),
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
		mcpsdk.WithDescription("On-demand pull from the Node's monitoring sidecar (--listen). Use for live process drill-down. The result also carries session_slots (the operator's override, 0 = derive) and derived_slots (the slot count in effect, when the node has a pool limit)."),
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

func (s *Server) handleComputePinSettingsMCP(ctx context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	args, _ := req.Params.Arguments.(map[string]any)
	if args["pin_check_seconds"] != nil {
		if deny := mcpFedCap(ctx, federation.CapConfigWrite); deny != nil {
			return deny, nil
		}
		if denied := s.allowSelfConfigCheck(); denied != nil {
			return denied, nil
		}
		secs := req.GetFloat("pin_check_seconds", -1)
		if secs < 0 || secs != math.Trunc(secs) {
			return toolError("compute_pin_settings", errors.New("pin_check_seconds must be a whole number >= 0 (0 = pin keeper off)")), nil
		}
		if _, err := s.proxyJSON(http.MethodPut, "/api/config", map[string]any{"pin_check_seconds": int(secs)}); err != nil {
			return toolError("compute_pin_settings", err), nil
		}
	}
	raw, err := s.proxyGet("/api/config", nil)
	if err != nil {
		return toolError("compute_pin_settings", err), nil
	}
	var cfg struct {
		PinCheckSeconds *int `json:"pin_check_seconds"`
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return toolError("compute_pin_settings", err), nil
	}
	out, _ := json.Marshal(map[string]any{
		"pin_check_seconds": cfg.PinCheckSeconds,
		"note":              "0 = pin keeper off. Per-node pins: compute_node_get.",
	})
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
	if v := optString(req, "max_concurrent_sessions"); v != "" {
		var n int
		_, _ = fmt.Sscanf(v, "%d", &n)
		body["max_concurrent_sessions"] = n
	}
	if v := optString(req, "session_slots"); v != "" {
		var n int
		_, _ = fmt.Sscanf(v, "%d", &n)
		body["session_slots"] = n
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
	if v := optString(req, "node_pinned_model"); v != "" {
		body["node_pinned_model"] = v
	}
	body["allow_model_swap"] = req.GetBool("allow_model_swap", false)
	if v := optString(req, "tls_ca_file"); v != "" {
		body["tls_ca_file"] = v
	}
	if v := optString(req, "tls_cert_fingerprint"); v != "" {
		body["tls_cert_fingerprint"] = v
	}
	body["tls_insecure_skip_verify"] = req.GetBool("tls_insecure_skip_verify", false)
	return body
}

var _ = json.Marshal
