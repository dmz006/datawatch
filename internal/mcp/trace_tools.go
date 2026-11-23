// Trace MCP tools — read-only views over the trace store, each a proxy of
// the matching REST route so the logic lives in one place:
//
//	trace_session   GET /api/trace/session/{id}
//	trace_automaton GET /api/trace/automaton/{id}
//	trace_rollup    GET /api/trace/rollup
//	trace_coverage  GET /api/trace/coverage
//	trace_stats     GET /api/trace/stats
//
// The tracing.* settings are read and changed through the config tools
// (get_config, config_set), like every other setting.

package mcp

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

// traceEventFilterOpts are the filter parameters shared by the session and
// Automaton tools.
func traceEventFilterOpts() []mcpsdk.ToolOption {
	return []mcpsdk.ToolOption{
		mcpsdk.WithString("tool", mcpsdk.Description("Optional — keep events with this tool name")),
		mcpsdk.WithString("backend", mcpsdk.Description("Optional — keep events from this backend")),
		mcpsdk.WithString("kind", mcpsdk.Enum("tool_call", "tool_result", "model_request", "state_change", "capability_check", "permission_decision", "loop_warning", "error"), mcpsdk.Description("Optional — keep one event kind")),
		mcpsdk.WithBoolean("errors", mcpsdk.Description("Optional — true keeps only error events and failed outcomes")),
		mcpsdk.WithNumber("limit", mcpsdk.Description("Optional — keep only the last N events (0 = all)")),
		mcpsdk.WithNumber("loop_threshold", mcpsdk.Description("Optional — repeat count that raises a loop warning in the roll-up (default 5)")),
	}
}

// traceFilterQuery copies the shared filter arguments of req into a query.
func traceFilterQuery(req mcpsdk.CallToolRequest) url.Values {
	q := url.Values{}
	args := req.GetArguments()
	for _, k := range []string{"tool", "backend", "kind"} {
		if v, ok := args[k].(string); ok && strings.TrimSpace(v) != "" {
			q.Set(k, strings.TrimSpace(v))
		}
	}
	if v, ok := args["errors"].(bool); ok && v {
		q.Set("errors", "true")
	}
	for _, k := range []string{"limit", "loop_threshold"} {
		if v, ok := args[k].(float64); ok && v > 0 {
			q.Set(k, strconv.Itoa(int(v)))
		}
	}
	return q
}

func (s *Server) traceProxy(name, path string, q url.Values) (*mcpsdk.CallToolResult, error) {
	out, err := s.proxyGet(path, q)
	if err != nil {
		return toolError(name, err), nil
	}
	return textOK(string(out)), nil
}

func traceRequiredID(req mcpsdk.CallToolRequest, tool, arg string) (string, *mcpsdk.CallToolResult) {
	id, _ := req.GetArguments()[arg].(string)
	id = strings.TrimSpace(id)
	if id == "" {
		return "", toolError(tool, fmt.Errorf("%s is required", arg))
	}
	return id, nil
}

// ── trace_session ───────────────────────────────────────────────────────────

func (s *Server) toolTraceSession() mcpsdk.Tool {
	opts := append([]mcpsdk.ToolOption{
		mcpsdk.WithDescription("Read the trace of one session: its recorded tool calls, results, model requests, state changes, capability and permission decisions, loop warnings and errors in order, plus the session roll-up and how much of the backend's activity the trace covers. Read-only; same data as GET /api/trace/session/{id}. Event text is redacted when stored."),
		mcpsdk.WithString("session", mcpsdk.Required(), mcpsdk.Description("Session full id")),
		mcpsdk.WithReadOnlyHintAnnotation(true),
	}, append(traceEventFilterOpts(), tracePackOpt())...)
	return mcpsdk.NewTool("trace_session", opts...)
}

func (s *Server) handleTraceSession(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	id, bad := traceRequiredID(req, "trace_session", "session")
	if bad != nil {
		return bad, nil
	}
	q := addTracePackQuery(req, traceFilterQuery(req))
	return s.traceProxy("trace_session", "/api/trace/session/"+url.PathEscape(id), q)
}

// ── trace_automaton ─────────────────────────────────────────────────────────

func (s *Server) toolTraceAutomaton() mcpsdk.Tool {
	opts := append([]mcpsdk.ToolOption{
		mcpsdk.WithDescription("Read the trace of one Automaton: the events of every session that ran for it, ordered by time, plus the Automaton roll-up. Read-only; same data as GET /api/trace/automaton/{id}."),
		mcpsdk.WithString("automaton", mcpsdk.Required(), mcpsdk.Description("Automaton id")),
		mcpsdk.WithReadOnlyHintAnnotation(true),
	}, traceEventFilterOpts()...)
	return mcpsdk.NewTool("trace_automaton", opts...)
}

func (s *Server) handleTraceAutomaton(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	id, bad := traceRequiredID(req, "trace_automaton", "automaton")
	if bad != nil {
		return bad, nil
	}
	return s.traceProxy("trace_automaton", "/api/trace/automaton/"+url.PathEscape(id), traceFilterQuery(req))
}

// ── trace_rollup ────────────────────────────────────────────────────────────

func (s *Server) toolTraceRollup() mcpsdk.Tool {
	return mcpsdk.NewTool("trace_rollup",
		mcpsdk.WithDescription("Roll up the trace store along one dimension: tool counts, slowest tools, error rate, repeated-call loop warnings, capability denials, and model requests with tokens. Read-only; same data as GET /api/trace/rollup."),
		mcpsdk.WithString("dimension", mcpsdk.Enum("session", "task", "story", "automaton", "agent", "backend"), mcpsdk.Description("Optional — grouping axis (default session)")),
		mcpsdk.WithString("key", mcpsdk.Description("Optional — keep only this group")),
		mcpsdk.WithNumber("loop_threshold", mcpsdk.Description("Optional — repeat count that raises a loop warning (default 5)")),
		tracePackOpt(),
		mcpsdk.WithReadOnlyHintAnnotation(true),
	)
}

func (s *Server) handleTraceRollup(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	q := url.Values{}
	args := req.GetArguments()
	for _, k := range []string{"dimension", "key"} {
		if v, ok := args[k].(string); ok && strings.TrimSpace(v) != "" {
			q.Set(k, strings.TrimSpace(v))
		}
	}
	if v, ok := args["loop_threshold"].(float64); ok && v > 0 {
		q.Set("loop_threshold", strconv.Itoa(int(v)))
	}
	q = addTracePackQuery(req, q)
	return s.traceProxy("trace_rollup", "/api/trace/rollup", q)
}

// ── trace_coverage ──────────────────────────────────────────────────────────

func (s *Server) toolTraceCoverage() mcpsdk.Tool {
	return mcpsdk.NewTool("trace_coverage",
		mcpsdk.WithDescription("List how much of each traced session's backend activity the trace captures (full, partial, events-only or unknown) and count the levels per backend, so a gap is visible before relying on a trace. Read-only; same data as GET /api/trace/coverage."),
		mcpsdk.WithString("backend", mcpsdk.Description("Optional — only this backend")),
		mcpsdk.WithReadOnlyHintAnnotation(true),
	)
}

func (s *Server) handleTraceCoverage(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	q := url.Values{}
	if v, ok := req.GetArguments()["backend"].(string); ok && strings.TrimSpace(v) != "" {
		q.Set("backend", strings.TrimSpace(v))
	}
	return s.traceProxy("trace_coverage", "/api/trace/coverage", q)
}

// ── trace_stats ─────────────────────────────────────────────────────────────

func (s *Server) toolTraceStats() mcpsdk.Tool {
	return mcpsdk.NewTool("trace_stats",
		mcpsdk.WithDescription("Read the trace store's totals (sessions, events, error events, time of the last event) and its write counters since the daemon started (appended, failed writes, pruned). Takes no parameters. Read-only; same data as GET /api/trace/stats and the trace_* fields of get_stats."),
		mcpsdk.WithReadOnlyHintAnnotation(true),
	)
}

func (s *Server) handleTraceStats(_ context.Context, _ mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	return s.traceProxy("trace_stats", "/api/trace/stats", nil)
}
