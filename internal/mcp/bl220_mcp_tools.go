// BL220 — MCP surface parity gap closure (G11/G12/G13) — 2026-05-02.
//
// Three feature areas reachable via REST and config_set but lacking
// dedicated MCP tools with typed parameters and discovery docs:
//
//   detection     — prompt/completion/rate-limit/debounce config + health status
//   dns_channel   — DNS covert-channel config
//   proxy         — reverse-proxy connection pooling config
//
// _get tools: read from GET /api/config and return only the relevant section.
// _set tools: write via PUT /api/config (applyConfigPatch).  Gated behind
//             mcp.allow_self_config — same guard as the generic config_set tool.
// detection_status: read-only GET /api/diagnose; no permission gate.

package mcp

import (
	"context"
	"encoding/json"
	"net/http"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

// allowSelfConfigCheck returns a denied result if mcp.allow_self_config is not
// set, nil otherwise.  Shared by all _config_set tools.
func (s *Server) allowSelfConfigCheck() *mcpsdk.CallToolResult {
	if s.cfg == nil || !s.cfg.AllowSelfConfig {
		return mcpsdk.NewToolResultText(
			"permission denied: set mcp.allow_self_config=true in the config file (and restart) to enable self-modify",
		)
	}
	return nil
}

// configSection reads GET /api/config and returns the named top-level section
// as a JSON string.  Returns the full body unchanged if parsing fails.
func (s *Server) configSection(section string) (string, error) {
	raw, err := s.proxyGet("/api/config", nil)
	if err != nil {
		return "", err
	}
	var full map[string]json.RawMessage
	if err := json.Unmarshal(raw, &full); err != nil {
		return string(raw), nil
	}
	if sec, ok := full[section]; ok {
		return string(sec), nil
	}
	return "{}", nil
}

// ── detection_status ─────────────────────────────────────────────────────────

func (s *Server) toolDetectionStatus() mcpsdk.Tool {
	return mcpsdk.NewTool("detection_status",
		mcpsdk.WithDescription("Full system / eBPF health snapshot. Returns per-subsystem {name, ok, detail} checks including eBPF probe status. Read-only — no permission gate required."),
	)
}

func (s *Server) handleDetectionStatus(_ context.Context, _ mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	out, err := s.proxyGet("/api/diagnose", nil)
	if err != nil {
		return textOK("Error: " + err.Error()), nil
	}
	return textOK(string(out)), nil
}

// ── detection_config_get ─────────────────────────────────────────────────────

func (s *Server) toolDetectionConfigGet() mcpsdk.Tool {
	return mcpsdk.NewTool("detection_config_get",
		mcpsdk.WithDescription("Read the global detection config: prompt/completion/rate-limit/input-needed pattern lists and debounce/cooldown timing. Per-LLM overrides are visible in get_config under each backend."),
	)
}

func (s *Server) handleDetectionConfigGet(_ context.Context, _ mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	out, err := s.configSection("detection")
	if err != nil {
		return textOK("Error: " + err.Error()), nil
	}
	return textOK(out), nil
}

// ── detection_config_set ─────────────────────────────────────────────────────

func (s *Server) toolDetectionConfigSet() mcpsdk.Tool {
	return mcpsdk.NewTool("detection_config_set",
		mcpsdk.WithDescription("Update global detection timing. Pattern lists (prompt_patterns, completion_patterns, rate_limit_patterns, input_needed_patterns) require JSON-array values — use config_set with key=detection.<field> for those. Requires mcp.allow_self_config=true."),
		mcpsdk.WithNumber("prompt_debounce", mcpsdk.Description("Seconds to wait after detecting a prompt before transitioning to waiting_input (0 = disabled). Default 3.")),
		mcpsdk.WithNumber("notify_cooldown", mcpsdk.Description("Minimum seconds between repeated needs-input notifications for the same session. Default 15.")),
	)
}

func (s *Server) handleDetectionConfigSet(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	if denied := s.allowSelfConfigCheck(); denied != nil {
		return denied, nil
	}
	patch := map[string]any{}
	if v := req.GetFloat("prompt_debounce", -1); v >= 0 {
		patch["detection.prompt_debounce"] = int(v)
	}
	if v := req.GetFloat("notify_cooldown", -1); v >= 0 {
		patch["detection.notify_cooldown"] = int(v)
	}
	if len(patch) == 0 {
		return textOK("no fields provided — nothing updated"), nil
	}
	out, err := s.proxyJSON(http.MethodPut, "/api/config", patch)
	if err != nil {
		return textOK("Error: " + err.Error()), nil
	}
	return textOK(string(out)), nil
}

// ── dns_channel_config_get ───────────────────────────────────────────────────

func (s *Server) toolDNSChannelConfigGet() mcpsdk.Tool {
	return mcpsdk.NewTool("dns_channel_config_get",
		mcpsdk.WithDescription("Read the DNS covert-channel config: enabled, mode (server|client), domain, listen, upstream, TTL, poll_interval, rate_limit. Secret is masked."),
	)
}

func (s *Server) handleDNSChannelConfigGet(_ context.Context, _ mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	out, err := s.configSection("dns_channel")
	if err != nil {
		return textOK("Error: " + err.Error()), nil
	}
	return textOK(out), nil
}

// ── dns_channel_config_set ───────────────────────────────────────────────────

func (s *Server) toolDNSChannelConfigSet() mcpsdk.Tool {
	return mcpsdk.NewTool("dns_channel_config_set",
		mcpsdk.WithDescription("Update DNS covert-channel configuration. Requires mcp.allow_self_config=true. Daemon restart needed for listen/domain changes to take effect."),
		mcpsdk.WithBoolean("enabled", mcpsdk.Description("Enable or disable the DNS channel.")),
		mcpsdk.WithString("mode", mcpsdk.Description("server or client")),
		mcpsdk.WithString("domain", mcpsdk.Description("Authoritative subdomain (e.g. ctl.example.com)")),
		mcpsdk.WithString("listen", mcpsdk.Description("Server UDP/TCP bind address (default :53)")),
		mcpsdk.WithString("upstream", mcpsdk.Description("Client resolver address (e.g. 8.8.8.8:53)")),
		mcpsdk.WithString("secret", mcpsdk.Description("HMAC-SHA256 shared secret")),
		mcpsdk.WithNumber("ttl", mcpsdk.Description("DNS response TTL in seconds (0 = non-cacheable)")),
		mcpsdk.WithNumber("max_response_size", mcpsdk.Description("Max response bytes before truncation (default 512)")),
		mcpsdk.WithString("poll_interval", mcpsdk.Description("Client polling interval (e.g. 5s, default 5s)")),
		mcpsdk.WithNumber("rate_limit", mcpsdk.Description("Max queries per IP per minute (0 = unlimited, default 30)")),
	)
}

func (s *Server) handleDNSChannelConfigSet(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	if denied := s.allowSelfConfigCheck(); denied != nil {
		return denied, nil
	}
	patch := map[string]any{}
	// Booleans: mcpsdk returns false as default — only set if explicitly supplied.
	// Type-assert Arguments to map[string]any before checking key presence.
	if args, ok := req.Params.Arguments.(map[string]any); ok {
		if v, exists := args["enabled"]; exists && v != nil {
			patch["dns_channel.enabled"] = req.GetBool("enabled", false)
		}
	}
	if v := req.GetString("mode", ""); v != "" {
		patch["dns_channel.mode"] = v
	}
	if v := req.GetString("domain", ""); v != "" {
		patch["dns_channel.domain"] = v
	}
	if v := req.GetString("listen", ""); v != "" {
		patch["dns_channel.listen"] = v
	}
	if v := req.GetString("upstream", ""); v != "" {
		patch["dns_channel.upstream"] = v
	}
	if v := req.GetString("secret", ""); v != "" {
		patch["dns_channel.secret"] = v
	}
	if v := req.GetFloat("ttl", -1); v >= 0 {
		patch["dns_channel.ttl"] = int(v)
	}
	if v := req.GetFloat("max_response_size", -1); v >= 0 {
		patch["dns_channel.max_response_size"] = int(v)
	}
	if v := req.GetString("poll_interval", ""); v != "" {
		patch["dns_channel.poll_interval"] = v
	}
	if v := req.GetFloat("rate_limit", -1); v >= 0 {
		patch["dns_channel.rate_limit"] = int(v)
	}
	if len(patch) == 0 {
		return textOK("no fields provided — nothing updated"), nil
	}
	out, err := s.proxyJSON(http.MethodPut, "/api/config", patch)
	if err != nil {
		return textOK("Error: " + err.Error()), nil
	}
	return textOK(string(out)), nil
}

// ── proxy_config_get ─────────────────────────────────────────────────────────

func (s *Server) toolProxyConfigGet() mcpsdk.Tool {
	return mcpsdk.NewTool("proxy_config_get",
		mcpsdk.WithDescription("Read reverse-proxy aggregation config: enabled, health_interval, request_timeout, offline_queue_size, circuit_breaker_threshold, circuit_breaker_reset."),
	)
}

func (s *Server) handleProxyConfigGet(_ context.Context, _ mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	out, err := s.configSection("proxy")
	if err != nil {
		return textOK("Error: " + err.Error()), nil
	}
	return textOK(out), nil
}

// ── proxy_config_set ─────────────────────────────────────────────────────────

func (s *Server) toolProxyConfigSet() mcpsdk.Tool {
	return mcpsdk.NewTool("proxy_config_set",
		mcpsdk.WithDescription("Update reverse-proxy aggregation config. Requires mcp.allow_self_config=true."),
		mcpsdk.WithBoolean("enabled", mcpsdk.Description("Enable or disable proxy aggregation mode.")),
		mcpsdk.WithNumber("health_interval", mcpsdk.Description("Seconds between remote health checks (default 30).")),
		mcpsdk.WithNumber("request_timeout", mcpsdk.Description("Seconds per remote request (default 10).")),
		mcpsdk.WithNumber("offline_queue_size", mcpsdk.Description("Max queued commands per server when offline (default 100).")),
		mcpsdk.WithNumber("circuit_breaker_threshold", mcpsdk.Description("Failures before marking a server down (default 3).")),
		mcpsdk.WithNumber("circuit_breaker_reset", mcpsdk.Description("Seconds before retrying a downed server (default 30).")),
	)
}

func (s *Server) handleProxyConfigSet(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	if denied := s.allowSelfConfigCheck(); denied != nil {
		return denied, nil
	}
	patch := map[string]any{}
	if args, ok := req.Params.Arguments.(map[string]any); ok {
		if v, exists := args["enabled"]; exists && v != nil {
			patch["proxy.enabled"] = req.GetBool("enabled", false)
		}
	}
	if v := req.GetFloat("health_interval", -1); v >= 0 {
		patch["proxy.health_interval"] = int(v)
	}
	if v := req.GetFloat("request_timeout", -1); v >= 0 {
		patch["proxy.request_timeout"] = int(v)
	}
	if v := req.GetFloat("offline_queue_size", -1); v >= 0 {
		patch["proxy.offline_queue_size"] = int(v)
	}
	if v := req.GetFloat("circuit_breaker_threshold", -1); v >= 0 {
		patch["proxy.circuit_breaker_threshold"] = int(v)
	}
	if v := req.GetFloat("circuit_breaker_reset", -1); v >= 0 {
		patch["proxy.circuit_breaker_reset"] = int(v)
	}
	if len(patch) == 0 {
		return textOK("no fields provided — nothing updated"), nil
	}
	out, err := s.proxyJSON(http.MethodPut, "/api/config", patch)
	if err != nil {
		return textOK("Error: " + err.Error()), nil
	}
	return textOK(string(out)), nil
}
