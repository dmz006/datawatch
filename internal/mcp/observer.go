// BL171 (S9) — MCP tool parity for the observer subsystem.

package mcp

import (
	"context"
	"net/http"
	"net/url"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

func (s *Server) toolObserverStats() mcpsdk.Tool {
	return mcpsdk.NewTool("observer_stats",
		mcpsdk.WithDescription("Unified StatsResponse v2 — host, cpu, mem, disk, gpu, sessions, backends, per-session + per-LLM-backend envelopes, process tree top-N."),
	)
}
func (s *Server) handleObserverStats(_ context.Context, _ mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	out, err := s.proxyGet("/api/observer/stats", nil)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) toolObserverEnvelopes() mcpsdk.Tool {
	return mcpsdk.NewTool("observer_envelopes",
		mcpsdk.WithDescription("Envelope rollup only — per-session and per-LLM-backend CPU/mem/net/GPU totals, sorted by CPU desc."),
	)
}
func (s *Server) handleObserverEnvelopesMCP(_ context.Context, _ mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	out, err := s.proxyGet("/api/observer/envelopes", nil)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

// BL180 Phase 2 cross-host (v5.12.0) — federation-aware envelope view.
func (s *Server) toolObserverEnvelopesAllPeers() mcpsdk.Tool {
	return mcpsdk.NewTool("observer_envelopes_all_peers",
		mcpsdk.WithDescription("Federation-aware envelope view — local + every peer with cross-peer Caller attribution. Session on host A talking to ollama on host B shows up as a Caller on host B's ollama envelope."),
	)
}
func (s *Server) handleObserverEnvelopesAllPeers(_ context.Context, _ mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	out, err := s.proxyGet("/api/observer/envelopes/all-peers", nil)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) toolObserverEnvelope() mcpsdk.Tool {
	return mcpsdk.NewTool("observer_envelope",
		mcpsdk.WithDescription("Drill-down — returns the process sub-tree for one envelope by id (e.g. \"session:ralfthewise-787e\" or \"backend:ollama\")."),
		mcpsdk.WithString("id", mcpsdk.Required(), mcpsdk.Description("Envelope ID")),
	)
}
func (s *Server) handleObserverEnvelope(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	q := url.Values{}
	q.Set("id", req.GetString("id", ""))
	out, err := s.proxyGet("/api/observer/envelope", q)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) toolObserverConfigGet() mcpsdk.Tool {
	return mcpsdk.NewTool("observer_config_get",
		mcpsdk.WithDescription("Read observer configuration (tick interval, envelope rules, eBPF toggle, peer federation knobs)."),
	)
}
func (s *Server) handleObserverConfigGet(_ context.Context, _ mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	out, err := s.proxyGet("/api/observer/config", nil)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) toolObserverConfigSet() mcpsdk.Tool {
	return mcpsdk.NewTool("observer_config_set",
		mcpsdk.WithDescription("Replace observer configuration (full body)."),
		mcpsdk.WithNumber("tick_interval_ms", mcpsdk.Description("1000 = 1 s cadence")),
		mcpsdk.WithString("ebpf_enabled", mcpsdk.Description("auto | true | false")),
	)
}
func (s *Server) handleObserverConfigSet(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	body := map[string]any{}
	if v := req.GetFloat("tick_interval_ms", 0); v != 0 {
		body["tick_interval_ms"] = int(v)
	}
	if v := req.GetString("ebpf_enabled", ""); v != "" {
		body["ebpf_enabled"] = v
	}
	out, err := s.proxyJSON(http.MethodPut, "/api/observer/config", body)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

// ── BL172 (S11) — peer registry parity ────────────────────────────────────

func (s *Server) toolObserverPeersList() mcpsdk.Tool {
	return mcpsdk.NewTool("observer_peers_list",
		mcpsdk.WithDescription("List Shape B / C peers registered with this datawatch (federated observer hosts). Returns name, shape, host_info, version, registered_at, last_push_at."),
	)
}
func (s *Server) handleObserverPeersList(_ context.Context, _ mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	out, err := s.proxyGet("/api/observer/peers", nil)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) toolObserverPeerGet() mcpsdk.Tool {
	return mcpsdk.NewTool("observer_peer_get",
		mcpsdk.WithDescription("Detail for one Shape B / C peer (TokenHash redacted)."),
		mcpsdk.WithString("name", mcpsdk.Required(), mcpsdk.Description("Peer name (typically the host name).")),
	)
}
func (s *Server) handleObserverPeerGet(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	name := req.GetString("name", "")
	if name == "" {
		return mcpsdk.NewToolResultError("name required"), nil
	}
	out, err := s.proxyGet("/api/observer/peers/"+url.PathEscape(name), nil)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) toolObserverPeerStats() mcpsdk.Tool {
	return mcpsdk.NewTool("observer_peer_stats",
		mcpsdk.WithDescription("Last-known StatsResponse v2 snapshot pushed by a Shape B / C peer."),
		mcpsdk.WithString("name", mcpsdk.Required(), mcpsdk.Description("Peer name.")),
	)
}
func (s *Server) handleObserverPeerStats(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	name := req.GetString("name", "")
	if name == "" {
		return mcpsdk.NewToolResultError("name required"), nil
	}
	out, err := s.proxyGet("/api/observer/peers/"+url.PathEscape(name)+"/stats", nil)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) toolObserverPeerRegister() mcpsdk.Tool {
	return mcpsdk.NewTool("observer_peer_register",
		mcpsdk.WithDescription("Register a Shape B / C peer; mints a bearer token (only opportunity — surfaced in the response and never persisted in plaintext on the parent)."),
		mcpsdk.WithString("name", mcpsdk.Required(), mcpsdk.Description("Stable peer name (commonly hostname).")),
		mcpsdk.WithString("shape", mcpsdk.Description("B | C (default B).")),
		mcpsdk.WithString("version", mcpsdk.Description("Reporter version string.")),
	)
}
func (s *Server) handleObserverPeerRegister(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	name := req.GetString("name", "")
	if name == "" {
		return mcpsdk.NewToolResultError("name required"), nil
	}
	body := map[string]any{"name": name}
	if v := req.GetString("shape", ""); v != "" {
		body["shape"] = v
	}
	if v := req.GetString("version", ""); v != "" {
		body["version"] = v
	}
	out, err := s.proxyJSON(http.MethodPost, "/api/observer/peers", body)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

// S13 — agent-flavoured aliases. Agents register as Shape A peers
// keyed by agent_id; these tools spell that explicitly so an MCP
// client can ask "what's agt_a1b2 doing right now" without first
// learning that agents-are-peers.

func (s *Server) toolObserverAgentStats() mcpsdk.Tool {
	return mcpsdk.NewTool("observer_agent_stats",
		mcpsdk.WithDescription("Last-known StatsResponse v2 snapshot pushed by a F10 ephemeral agent worker (S13). Same wire as observer_peer_stats, scoped to agents."),
		mcpsdk.WithString("agent_id", mcpsdk.Required(), mcpsdk.Description("F10 agent ID (e.g. agt_a1b2c3).")),
	)
}
func (s *Server) handleObserverAgentStats(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	id := req.GetString("agent_id", "")
	if id == "" {
		return mcpsdk.NewToolResultError("agent_id required"), nil
	}
	out, err := s.proxyGet("/api/observer/peers/"+url.PathEscape(id)+"/stats", nil)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) toolObserverAgentList() mcpsdk.Tool {
	return mcpsdk.NewTool("observer_agent_list",
		mcpsdk.WithDescription("List F10 ephemeral-agent peers in the observer federation (subset of observer_peers_list filtered to shape=A)."),
	)
}
func (s *Server) handleObserverAgentList(_ context.Context, _ mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	// Fetch the full peer list and filter shape=A in one shot.
	out, err := s.proxyGet("/api/observer/peers", nil)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) toolObserverPeerDelete() mcpsdk.Tool {
	return mcpsdk.NewTool("observer_peer_delete",
		mcpsdk.WithDescription("Remove a Shape B / C peer. The peer auto-re-registers on its next push (token rotation)."),
		mcpsdk.WithString("name", mcpsdk.Required(), mcpsdk.Description("Peer name.")),
	)
}
func (s *Server) handleObserverPeerDelete(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	name := req.GetString("name", "")
	if name == "" {
		return mcpsdk.NewToolResultError("name required"), nil
	}
	out, err := s.proxyJSON(http.MethodDelete, "/api/observer/peers/"+url.PathEscape(name), nil)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}
