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
