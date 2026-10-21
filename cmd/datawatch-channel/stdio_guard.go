package main

// stdio_guard.go — MCP stdio transport crash-safety for the datawatch-channel
// bridge (findings: docs/plans/2026-10-20-bl463-mcp-drop-findings.md).
//
// Two distinct mid-session death modes of this process were traced and can
// only be observed at the stdio boundary:
//
//  1. A panic in any tool handler (or in the mcp-go synchronous dispatch
//     path) is never recovered: mcp-go's WithRecovery() middleware is
//     opt-in and was not registered. The panic propagates out of either a
//     toolCallWorker goroutine or the Listen loop itself and the whole
//     bridge process exits. The session's MCP connection then reports
//     CONNECTION_CLOSED and never recovers, while the daemon stays fully
//     healthy — the exact symptom profile of the investigated incident.
//
//  2. StdioServer.Listen returns on ANY condition (context cancel, stdin
//     EOF, read error, write error after the client went away). main()
//     called stop() unconditionally in that case. That was intentional
//     (a closed stdio pipe means the client is gone), but it made even a
//     transient error permanent: no supervisor restarts this process.
//
// stdioListenGuarded fixes (1) and makes (2) visible: it recovers panics
// from the synchronous Listen path (so a handler panic on the slow/queue
// fall-through path cannot kill the process), logs the exact reason the
// transport closed, and logs every recovered tool-handler panic with its
// stack instead of letting it tear the process down.

import (
	"context"
	"fmt"
	"io"
	"log"
	"runtime/debug"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// stdioToolPanicGuard is an mcp-go tool-handler middleware (v0.46.0
// WithToolHandlerMiddleware chain) that converts any panic raised by a tool
// handler — on either the queued worker path or the synchronous
// queue-fallback path, since both go through HandleMessage's middleware
// chain — into a JSON-RPC error result. Without it, the panic escapes the
// process and the session's stdio MCP connection dies permanently.
func stdioToolPanicGuard(next server.ToolHandlerFunc) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcpsdk.CallToolRequest) (res *mcpsdk.CallToolResult, err error) {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("mcp/stdio: RECOVERED panic in tool %q: %v\n%s", req.Params.Name, r, debug.Stack())
				res = mcpsdk.NewToolResultError(fmt.Sprintf("internal panic in tool %q: %v", req.Params.Name, r))
				err = nil
			}
		}()
		return next(ctx, req)
	}
}

// stdioListenGuarded is a drop-in replacement for server.StdioServer.Listen
// for datawatch's two stdio entry points (this bridge, and the daemon's
// internal/mcp SseServer-adjacent stdio serve path reuses the same
// mcp-go API). It:
//
//   - logs when the stdio listener starts and the reason it stopped
//     (which is the "connection close reason" evidence for the session
//     transport),
//   - recovers panics that escape the synchronous dispatch path (requests
//     that are not queued async tool calls, e.g. the queue-full fallback)
//     so they terminate cleanly with an error result instead of killing
//     the process.
//
// Panics inside toolCallWorker goroutines cannot be caught here; they are
// covered by stdioToolPanicGuard. in/out are the concrete stdio pipes —
// mcp-go v0.46.0 takes them explicitly (nil readers panic inside its
// bufio.NewReader path).
func stdioListenGuarded(srv *server.StdioServer, ctx context.Context, name string, in io.Reader, out io.Writer) (err error) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("mcp/stdio: RECOVERED panic escaping Listen (%s): %v\n%s", name, r, debug.Stack())
			err = fmt.Errorf("panic recovered on %s: %v", name, r)
		}
	}()
	log.Printf("mcp/stdio: listener %s started", name)
	defer func() {
		if err == nil {
			log.Printf("mcp/stdio: listener %s stopped (stdin closed or EOF — client disconnected or process signal)", name)
		} else {
			log.Printf("mcp/stdio: listener %s stopped with error: %v", name, err)
		}
	}()
	return srv.Listen(ctx, in, out)
}
