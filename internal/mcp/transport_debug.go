package mcp

// transport_debug.go — MCP long-lived-transport observation and crash-safety
// hardening for the SSE and stdio entry points.
//
// Purpose: the LIVE incident investigated under the tracker ID kept in
// docs/plans/2026-10-20-bl463-mcp-drop-findings.md — a session's MCP
// connection dropping to CONNECTION_CLOSED mid-session while the daemon
// stays fully healthy — left no server-side evidence of WHY the transport
// closed. This file makes that visible:
//
//   - SSE: every long-lived session stream logs its open and close with
//     the close-reason evidence the incident lacked (duration, bytes
//     written, write/flush errors, idle gap). The http.Server timeout
//     profile is logged at serve start to show which stdlib deadlines
//     (ReadTimeout / IdleTimeout / WriteTimeout) CAN and CANNOT touch an
//     in-flight stream.
//   - stdio: the listener logs start and stop-reason, and a panic in any
//     tool handler is recovered (JSON-RPC error result) instead of killing
//     the process — mcp-go v0.46.0's WithRecovery() is opt-in and was
//     previously unregistered on every datawatch MCP entry point, which
//     made a single handler panic a permanent, unrecoverable MCP drop.
//
// DATAWATCH_MCP_DEBUG=1 enables verbose per-request logging; the
// stream open/close and stdio stop-reason lines are always emitted (one
// line per event) because they are the diagnostic that was missing in the
// incident.

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// mcpDebugEnabled reports whether verbose MCP transport logging is on.
// Accepted truthy values: 1, true, yes, on (case-insensitive).
func mcpDebugEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("DATAWATCH_MCP_DEBUG"))) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

// sseCloseObserver is the SSE close-reason instrumentation. It wraps the
// long-lived GET /sse ResponseWriter and records write activity (count,
// total bytes, first write, last write) plus the first write or flush
// error, so that at close we can say exactly what the stream looked like:
// whether any byte was ever sent, whether it went silent, and whether —
// and when — writing started failing (client going away vs a middlebox
// killing the connection are indistinguishable by timing alone; the
// write-error record is the distinguishing evidence, and previously the
// evidence did not exist at all because mcp-go discards the
// fmt.Fprint/Flush errors on its SSE main loop — see server/sse.go).
type sseCloseObserver struct {
	w          http.ResponseWriter
	mu         sync.Mutex
	writes     int
	bytes      int64
	lastWrite  time.Time
	firstWrite time.Time
	writeErr   error // first error, recorded at most once
}

// NewSSECloseObserver wraps w for the SSE session stream. Pass the
// resulting wrapper to the SSE handler instead of the raw w.
func NewSSECloseObserver(w http.ResponseWriter, remote string) *sseCloseObserver {
	return &sseCloseObserver{w: w, lastWrite: time.Now()}
}

// Header / Write implement http.ResponseWriter pass-through with
// observation: see sseCloseObserver doc.
func (o *sseCloseObserver) Header() http.Header { return o.w.Header() }

func (o *sseCloseObserver) Write(p []byte) (int, error) {
	n, err := o.w.Write(p)
	o.observe(n, err)
	return n, err
}

type sseFlusher interface{ Flush() error }

// Flush captures flush errors — the mcp-go SSE main loop calls
// flusher.Flush() and discards the result; we keep ours.
func (o *sseCloseObserver) Flush() error {
	f, ok := o.w.(sseFlusher)
	if !ok {
		return nil
	}
	err := f.Flush()
	o.observe(0, err)
	return nil
}

func (o *sseCloseObserver) observe(n int, err error) {
	o.mu.Lock()
	o.writes++
	o.bytes += int64(n)
	now := time.Now()
	if o.firstWrite.IsZero() {
		o.firstWrite = now
	}
	o.lastWrite = now
	if err != nil {
		o.writeErr = err
	}
	o.mu.Unlock()
}

// summary returns a one-line description of the observed stream activity,
// including the idle gap at close (time since the last write) and the
// first write/flush error if any.
func (o *sseCloseObserver) summary() string {
	o.mu.Lock()
	defer o.mu.Unlock()
	ago := time.Since(o.lastWrite).Round(time.Millisecond)
	if o.writeErr != nil {
		return fmt.Sprintf("writes=%d bytes=%d idle_at_close=%s FIRST_WRITE_ERROR=%.150q",
			o.writes, o.bytes, ago, o.writeErr)
	}
	return fmt.Sprintf("writes=%d bytes=%d idle_at_close=%s (no write errors observed)",
		o.writes, o.bytes, ago)
}

// ObserveSSEHandler wraps next (the SSE server handler) and, for the
// long-lived stream request (GET with Accept: text/event-stream), logs
// open and close with close-reason evidence. Short requests
// (POST /message, GET /api/mcp/tools) pass through with an optional
// verbose log line when DATAWATCH_MCP_DEBUG is enabled.
func ObserveSSEHandler(next http.Handler) http.Handler {
	debug := mcpDebugEnabled()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		isStream := r.Method == http.MethodGet &&
			strings.Contains(r.Header.Get("Accept"), "text/event-stream")
		if !isStream {
			if debug {
				log.Printf("mcp/sse: req %s %s from %.150q", r.Method, r.URL.Path, r.RemoteAddr)
			}
			next.ServeHTTP(w, r)
			return
		}
		start := time.Now()
		obs := NewSSECloseObserver(w, r.RemoteAddr)
		log.Printf("mcp/sse: STREAM OPEN %s from %.150q (session transport begins)",
			r.URL.Path, r.RemoteAddr)
		next.ServeHTTP(obs, r)
		dur := time.Since(start).Round(time.Millisecond)
		reason := "handler returned (client request context completed)"
		if cerr := r.Context().Err(); cerr != nil {
			reason = "client disconnected (request context: " + cerr.Error() + ")"
		}
		log.Printf("mcp/sse: STREAM CLOSE after %s — %s | %s", dur, reason, obs.summary())
	})
}

// LogSSEServerTimeouts prints the http.Server timeout profile with the
// interpretation the incident investigation needed. The answer lives in
// the Go 1.27.2 net/http source (src/net/http/server.go, conn.serve and
// readRequest):
//
//   - ReadTimeout (30s): applied via SetReadDeadline while reading a
//     request from the client. It does NOT apply while the SSE GET
//     handler is still running — there is no read on that connection
//     once the request has no body.
//   - IdleTimeout (120s): applied between keep-alive requests. After a
//     handler returns, conn.serve() sets a read deadline to
//     idleTimeout() and then blocks on Peek(4); if no new request
//     arrives in that window the connection is closed. So an idle
//     keep-alive POST connection is killed at 120s; the mcp-go SSE
//     client re-issues each POST as a fresh request, so this is
//     normally benign — a client that stalls >120s mid-POST would die
//     on the read deadline.
//   - WriteTimeout (unset): if set, readRequest() wraps the handler run
//     in SetWriteDeadline(now+d), so a slow/long SSE write would fail
//     and the stream would end. The daemon deliberately leaves it unset.
//   - If both IdleTimeout and ReadTimeout are 0, no read deadline is
//     ever set on an already-idle connection.
//   - None of these apply to the bridge stdio path at all (no HTTP).
func LogSSEServerTimeouts(httpSrv *http.Server) {
	log.Printf(`mcp/sse: http.Server timeout profile: ReadHeaderTimeout=%s ReadTimeout=%s IdleTimeout=%s WriteTimeout=%s
  interpretation: read deadlines apply while reading requests; the in-flight SSE GET handler is exempt
  idle keep-alive connections close after IdleTimeout (or ReadTimeout if IdleTimeout=0)
  in-flight SSE event writes never time out because WriteTimeout is unset
  a session-stream write failure can only come from the client/middlebox side`,
		timeoutStr(httpSrv.ReadHeaderTimeout), timeoutStr(httpSrv.ReadTimeout),
		timeoutStr(httpSrv.IdleTimeout), timeoutStr(httpSrv.WriteTimeout))
}

func timeoutStr(d time.Duration) string {
	if d == 0 {
		return "unset"
	}
	return d.String()
}

// RegisterToolPanicRecovery wires a panic-recovery tool-handler middleware
// into the mcp-go MCPServer. Call once after NewMCPServer, before any
// tool call.
//
// Without it, a panic in any tool handler kills the process: on the SSE
// path (handleMessage runs HandleMessage in a goroutine — mcp-go
// server/sse.go) that is the whole daemon; on the stdio path it kills the
// datawatch-channel bridge and permanently drops that session's MCP
// connection (CONNECTION_CLOSED with no recovery, daemon healthy) — the
// exact symptom of the incident. mcp-go's own WithRecovery() does
// precisely this (it registers WithToolHandlerMiddleware internally,
// server/server.go:365) but is opt-in and was unregistered on every
// datawatch entry point.
func RegisterToolPanicRecovery(mcpSrv *server.MCPServer) {
	guard := mcpToolPanicGuard()
	_ = server.WithToolHandlerMiddleware(guard)(mcpSrv)
}

// mcpToolPanicGuard returns the raw middleware so tests can run it
// directly without constructing an MCPServer: it converts a panicking
// handler into a JSON-RPC error result and recovers the process.
func mcpToolPanicGuard() server.ToolHandlerMiddleware {
	return func(next server.ToolHandlerFunc) server.ToolHandlerFunc {
		return func(ctx context.Context, req mcpsdk.CallToolRequest) (res *mcpsdk.CallToolResult, err error) {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("mcp: RECOVERED panic in tool %q: %v\n%s", req.Params.Name, r, debug.Stack())
					res = mcpsdk.NewToolResultError(fmt.Sprintf("internal panic in tool %q: %v", req.Params.Name, r))
					err = nil
				}
			}()
			return next(ctx, req)
		}
	}
}

// GuardStdioListen wraps StdioServer.Listen: recovers panics escaping the
// synchronous dispatch path (tool calls that fell out of the async queue
// and ran inline in the Listen loop) and logs the exact reason the
// listener stopped — the transport close-reason evidence stdio used to
// leave behind only as a process-death signature.
func GuardStdioListen(srv *server.StdioServer, ctx context.Context, name string) (err error) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("mcp/stdio: RECOVERED panic escaping %s listener: %v\n%s", name, r, debug.Stack())
			err = fmt.Errorf("stdio %s: panic recovered: %v", name, r)
		}
	}()
	log.Printf("mcp/stdio: listener %s started", name)
	defer func() {
		if err == nil {
			log.Printf("mcp/stdio: listener %s stopped — stdin closed/EOF (client disconnected or shutting down)", name)
		} else {
			log.Printf("mcp/stdio: listener %s stopped with error: %v", name, err)
		}
	}()
	return srv.Listen(ctx)
}
