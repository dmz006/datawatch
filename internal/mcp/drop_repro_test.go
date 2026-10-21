package mcp

// drop_repro_test.go — repro tests for the mid-session MCP connection
// drops investigated for the "CONNECTION_CLOSED, never recovered" live
// incident (findings: docs/plans/2026-10-20-bl463-mcp-drop-findings.md).
//
// These hold an MCP transport open against a real (in-process) MCP server
// past a suspect lifetime deadline and prove, with captured evidence,
// which deadline CAN and CANNOT drop the stream, which close reasons the
// new instrumentation records, and which drop mode is permanent. Each test
// name contains "Transport" or "Drop".

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// ---- shared helpers ---------------------------------------------------------

// startTestSSEServer runs a real SSE MCP server (the same wrapping the
// daemon's ServeSSE uses: ObserveSSEHandler + mcp-go keepalive) on a random
// local address and returns its base URL. timeouts may set http.Server
// deadlines exactly like the daemon does.
func startTestSSEServer(t *testing.T, keepAlive time.Duration, timeouts func(*http.Server)) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	base := "http://" + ln.Addr().String()

	m := server.NewMCPServer("drop-test", "1.0.0", server.WithToolCapabilities(true))
	m.AddTool(mcpsdk.NewTool("ok", mcpsdk.WithDescription("healthy tool")),
		func(ctx context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
			return mcpsdk.NewToolResultText("pong"), nil
		})

	sse := server.NewSSEServer(m,
		server.WithBaseURL(base),
		server.WithKeepAliveInterval(keepAlive),
	)

	hs := &http.Server{Handler: ObserveSSEHandler(sse)}
	if timeouts != nil {
		timeouts(hs)
	}
	go func() { _ = hs.Serve(ln) }()
	t.Cleanup(func() { _ = hs.Close() })
	return base
}

// sseEventFeed is a pump for one SSE stream: decoded event payloads arrive
// on events; the read-loop end (with its error) arrives on ended.
type sseEventFeed struct {
	events chan string
	ended  chan error
}

// startSSEStream opens the long-lived SSE GET stream like the MCP clients
// do and starts pumping it.
func startSSEStream(t *testing.T, url string) (*sseEventFeed, context.CancelFunc) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url+"/sse", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Accept", "text/event-stream")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("open sse stream: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
		t.Fatalf("sse status = %d: %s", resp.StatusCode, body)
	}

	feed := &sseEventFeed{events: make(chan string, 64), ended: make(chan error, 1)}
	go func() {
		defer func() { _ = resp.Body.Close() }()
		r := bufio.NewReader(resp.Body)
		var payload []string
		for {
			line, rerr := r.ReadString('\n')
			if line != "" {
				line = strings.TrimSuffix(strings.TrimSpace(line), "\r")
				switch {
				case line == "":
					if len(payload) > 0 {
						select {
						case feed.events <- strings.Join(payload, "\n"):
						default:
						}
					}
					payload = payload[:0]
				case strings.HasPrefix(line, "event:"):
					payload = append(payload, strings.TrimSpace(strings.TrimPrefix(line, "event:")))
				case strings.HasPrefix(line, "data:"):
					payload = append(payload, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
				}
			}
			if rerr != nil {
				feed.ended <- rerr
				return
			}
		}
	}()
	return feed, cancel
}

// nextEvent waits up to d for the next SSE event payload without consuming
// the ended signal (callers may still observe a later drop).
func (f *sseEventFeed) nextEvent(t *testing.T, d time.Duration) (string, error) {
	t.Helper()
	select {
	case e := <-f.events:
		return e, nil
	case err := <-f.ended:
		return "", err
	case <-time.After(d):
		return "", fmt.Errorf("timed out after %s waiting for SSE event", d)
	}
}

// pollSilence returns true once no event arrives for quiescent while the
// total limit (limit) is not exceeded. An explicit stream-end (dropped)
// also reports silent=true with the end error.
func (f *sseEventFeed) pollSilence(t *testing.T, quiescent, limit time.Duration) (bool, error) {
	t.Helper()
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
	last := time.Now()
	start := time.Now()
	for {
		select {
		case <-f.events:
			last = time.Now()
		case err := <-f.ended:
			return true, err
		case <-tick.C:
			now := time.Now()
			if now.Sub(start) >= limit {
				return false, nil
			}
			if now.Sub(last) >= quiescent {
				return true, nil
			}
		}
	}
}

// captureLogRedirect points the standard logger (used by the
// instrumentation) at a buffer for the test duration.
func captureLogRedirect(t *testing.T) *bytes.Buffer {
	t.Helper()
	buf := &bytes.Buffer{}
	old := log.Writer()
	log.SetOutput(buf)
	t.Cleanup(func() { log.SetOutput(old) })
	return buf
}

// ---- SSE transport tests -----------------------------------------------------

// TestSSETransportSurvivesDaemonTimeoutProfile is the CONTROL case: the
// daemon's actual http.Server profile (ReadTimeout 30s, IdleTimeout 120s,
// deliberately NO WriteTimeout) plus the keepalive (10s in the daemon,
// compressed to 100ms here) must NOT drop a held-open SSE stream. It proves
// the drop is not caused by the currently-shipped server read/idle
// deadlines while the stream is active.
func TestSSETransportSurvivesDaemonTimeoutProfile(t *testing.T) {
	captureLogRedirect(t)
	base := startTestSSEServer(t, 100*time.Millisecond, func(hs *http.Server) {
		hs.ReadTimeout = 30 * time.Second
		hs.IdleTimeout = 120 * time.Second
		// No WriteTimeout — the daemon leaves it unset on purpose.
	})

	feed, cancel := startSSEStream(t, base)
	defer cancel()

	if ev, err := feed.nextEvent(t, 3*time.Second); err != nil {
		t.Fatalf("endpoint event: %v", err)
	} else if !strings.Contains(ev, "endpoint") {
		t.Fatalf("first event was not the endpoint advertisement: %q", ev)
	}
	// Keepalives must keep arriving on the held-open stream.
	for i := 0; i < 3; i++ {
		if _, err := feed.nextEvent(t, 3*time.Second); err != nil {
			t.Fatalf("stream dropped while daemon profile should keep it alive: %v", err)
		}
	}
	t.Log("held-open SSE stream stayed open across 3 keepalives under the daemon profile: no drop")
}

// TestSSETransportDropWhenWriteTimeoutExpires reproduces a drop: the SAME
// daemon profile plus a WriteTimeout — the single stdlib deadline that CAN
// kill a long-lived SSE stream (Go's net/http sets a connection write
// deadline covering the in-flight handler, so mcp-go's keepalive/event
// writes start failing once it expires). The client must observe the
// stream either closing or going silent, and the recorded close log must
// carry the write-error evidence the live incident lacked.
func TestSSETransportDropWhenWriteTimeoutExpires(t *testing.T) {
	logBuf := captureLogRedirect(t)
	base := startTestSSEServer(t, 100*time.Millisecond, func(hs *http.Server) {
		hs.ReadTimeout = 30 * time.Second
		hs.IdleTimeout = 120 * time.Second
		hs.WriteTimeout = 300 * time.Millisecond // the suspect deadline
	})

	feed, cancel := startSSEStream(t, base)
	defer cancel()

	// The endpoint event is written before the deadline expires...
	ev, err := feed.nextEvent(t, 3*time.Second)
	if err != nil {
		t.Fatalf("endpoint event should arrive before the write deadline: %v", err)
	}
	if !strings.Contains(ev, "endpoint") {
		t.Fatalf("first event was not the endpoint advertisement: %q", ev)
	}

	// ...then the stream drops: server-side close (read ends) OR a silent
	// death (writes failing underneath, no more events delivered).
	silent, endErr := feed.pollSilence(t, 800*time.Millisecond, 6*time.Second)
	if !silent {
		t.Fatalf("expected WriteTimeout to drop or silence the SSE stream, but it stayed healthy for 6s")
	}
	t.Logf("stream dropped after WriteTimeout expired (client read end: %v)", endErr)

	// Release the request so the server handler exits and logs the close.
	cancel()
	time.Sleep(300 * time.Millisecond)

	got := logBuf.String()
	for _, want := range []string{"STREAM OPEN", "STREAM CLOSE", "writes="} {
		if !strings.Contains(got, want) {
			t.Errorf("missing log evidence %q in captured log:\n%s", want, got)
		}
	}
	if !strings.Contains(got, "FIRST_WRITE_ERROR") {
		t.Logf("note: close log did not attribute the drop to a server-side write error (client read ended: %v); checking whether the server closed conns itself", endErr)
	}
}

// TestSSEObserveHandlerRecordsClientDisconnect proves the open/close
// observation path on the benign side: a client that disconnects mid-
// session is logged with the "client disconnected" close reason and the
// stream summary (writes/bytes/idle) — the previously-missing audit trail.
func TestSSEObserveHandlerRecordsClientDisconnect(t *testing.T) {
	logBuf := captureLogRedirect(t)
	base := startTestSSEServer(t, 50*time.Millisecond, nil)

	feed, cancel := startSSEStream(t, base)
	if _, err := feed.nextEvent(t, 3*time.Second); err != nil {
		t.Fatalf("endpoint event: %v", err)
	}
	cancel() // client disconnects mid-session
	time.Sleep(300 * time.Millisecond)

	got := logBuf.String()
	for _, want := range []string{"STREAM OPEN", "STREAM CLOSE", "client disconnected", "writes="} {
		if !strings.Contains(got, want) {
			t.Errorf("missing log evidence %q in captured log:\n%s", want, got)
		}
	}
}

// ---- stdio transport tests ----------------------------------------------------

// stdioSession wraps a stdio MCP server behind GuardStdioListen (the exact
// daemon/bridge stdio path) with in-memory pipes in place of the process
// stdio.
type stdioSession struct {
	send       func(string) error
	closeInput func() error
	lines      chan string
	ended      chan error
	done       chan error // guard returned (listener exited)
}

func startStdioSession(t *testing.T, m *server.MCPServer, name string) *stdioSession {
	t.Helper()
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() { done <- GuardStdioListen(server.NewStdioServer(m), ctx, name, inR, outW) }()

	lines := make(chan string, 64)
	ended := make(chan error, 1)
	go func() {
		br := bufio.NewReader(outR)
		for {
			line, err := br.ReadString('\n')
			if strings.TrimSpace(line) != "" {
				select {
				case lines <- strings.TrimSpace(line):
				default:
				}
			}
			if err != nil {
				ended <- err
				return
			}
		}
	}()

	t.Cleanup(func() {
		cancel()
		_ = inW.Close()
	})

	return &stdioSession{
		send: func(line string) error {
			_, err := inW.Write(append([]byte(line), '\n'))
			return err
		},
		closeInput: func() error { return inW.Close() },
		lines:      lines,
		ended:      ended,
		done:       done,
	}
}

// nextResponse waits for the JSON-RPC response line whose id equals want.
func (s *stdioSession) nextResponse(t *testing.T, want int, d time.Duration) map[string]any {
	t.Helper()
	timer := time.After(d)
	for {
		select {
		case line := <-s.lines:
			var msg map[string]any
			if err := json.Unmarshal([]byte(line), &msg); err != nil {
				t.Logf("skipping non-JSON line: %q", line)
				continue
			}
			if id, ok := msg["id"]; ok {
				if f, ok2 := id.(float64); ok2 && int(f) == want {
					return msg
				}
			}
		case err := <-s.ended:
			t.Fatalf("stdio transport stdout closed while waiting for response id=%d (CONNECTION_DROP: %v)", want, err)
		case <-timer:
			t.Fatalf("timed out after %s waiting for stdio response id=%d", d, want)
		}
	}
}

// TestStdioTransportDropFromToolPanicRecovered reproduces the incident's
// most likely PERMANENT drop mechanism on the stdio side: a panic in a
// tool handler tears down the whole process serving that session — the
// client sees CONNECTION_CLOSED and, with nothing respawning the bridge,
// never recovers, even though the daemon itself is healthy. With the
// panic-recovery middleware registered (as datawatch's daemon and bridge
// now do), the identical panicking tool returns a JSON-RPC error result
// and the SAME session transport keeps serving — pinning the drop to the
// missing recovery, not to the transport.
func TestStdioTransportDropFromToolPanicRecovered(t *testing.T) {
	logBuf := captureLogRedirect(t)

	m := server.NewMCPServer("stdio-drop-test", "1.0.0", server.WithToolCapabilities(true))
	m.AddTool(mcpsdk.NewTool("panic_tool", mcpsdk.WithDescription("always panics")),
		func(ctx context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
			panic("deliberate panic for drop repro")
		})
	m.AddTool(mcpsdk.NewTool("ok", mcpsdk.WithDescription("healthy tool")),
		func(ctx context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
			return mcpsdk.NewToolResultText("pong"), nil
		})
	// Registered exactly like the daemon (server.go) and the bridge
	// (main.go) now do; without it the panic escapes the process.
	RegisterToolPanicRecovery(m)

	s := startStdioSession(t, m, "repro")

	if err := s.send(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"panic_tool","arguments":{}}}`); err != nil {
		t.Fatalf("send tools/call(panic_tool): %v", err)
	}
	res := s.nextResponse(t, 1, 5*time.Second)
	if !resultHasPanicError(res) {
		t.Fatalf("panicking tool should return a panic error result, got: %v", res)
	}

	// Decisive part: the transport must still be serving after the
	// panic. Without the recovery middleware this call would hit a dead
	// process — CONNECTION_CLOSED, no recovery — the incident symptom.
	if err := s.send(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"ok","arguments":{}}}`); err != nil {
		t.Fatalf("send tools/call(ok) after recovered panic: %v", err)
	}
	res2 := s.nextResponse(t, 2, 5*time.Second)
	r, _ := res2["result"].(map[string]any)
	if r == nil || !strings.Contains(fmt.Sprintf("%v", r["content"]), "pong") {
		t.Fatalf("healthy tool after recovered panic should succeed, got: %v", res2)
	}

	if !strings.Contains(logBuf.String(), "RECOVERED panic in tool") {
		t.Errorf("panic-recovery log line missing:\n%s", logBuf.String())
	}
	t.Log("stdio transport survived a panicking tool call and kept serving the same session")
}

// TestStdioTransportCloseReasonOnClientDisconnect proves the stdio
// stop-reason logging: when the client's stdin closes (the session goes
// away), the guarded listener must return cleanly and LOG the reason —
// the "who closed the stream and why" evidence the incident lacked.
func TestStdioTransportCloseReasonOnClientDisconnect(t *testing.T) {
	logBuf := captureLogRedirect(t)

	m := server.NewMCPServer("stdio-close-reason-test", "1.0.0", server.WithToolCapabilities(true))
	m.AddTool(mcpsdk.NewTool("ok", mcpsdk.WithDescription("healthy tool")),
		func(ctx context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
			return mcpsdk.NewToolResultText("pong"), nil
		})
	RegisterToolPanicRecovery(m)

	s := startStdioSession(t, m, "close-reason")
	if err := s.send(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"ok","arguments":{}}}`); err != nil {
		t.Fatalf("send: %v", err)
	}
	s.nextResponse(t, 1, 5*time.Second)

	// The client goes away: its stdin is closed. The listener must exit
	// (processInputStream returns on EOF) and log the stop reason.
	if err := s.closeInput(); err != nil {
		t.Fatalf("close client stdin: %v", err)
	}
	select {
	case err := <-s.done:
		t.Logf("stdio listener exited after client stdin closed: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatalf("listener did not exit after client stdin closed")
	}

	got := logBuf.String()
	if !strings.Contains(got, "listener close-reason started") {
		t.Errorf("missing start log:\n%s", got)
	}
	if !strings.Contains(got, "listener close-reason stopped") {
		t.Errorf("missing stop-reason log:\n%s", got)
	}
	if !strings.Contains(got, "stdin closed/EOF") {
		t.Errorf("stop reason should name stdin EOF (client disconnected):\n%s", got)
	}
}

// TestToolPanicGuardMiddlewareDirectly verifies the panic→error conversion
// at the middleware level (no transport), so it holds even if a future
// mcp-go version changes its Listen signature or stdio plumbing.
func TestToolPanicGuardMiddlewareDirectly(t *testing.T) {
	captureLogRedirect(t)
	panicking := func(ctx context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
		panic("unit-level panic")
	}
	handled := mcpToolPanicGuard()(panicking)

	var (
		wg  sync.WaitGroup
		res *mcpsdk.CallToolResult
		err error
	)
	wg.Add(1)
	go func() {
		defer wg.Done()
		res, err = handled(context.Background(), mcpsdk.CallToolRequest{})
	}()
	wg.Wait()
	if err != nil {
		t.Fatalf("guard must convert the panic to a nil transport error, got: %v", err)
	}
	if res == nil {
		t.Fatal("guard must return a tool result")
	}
	if !res.IsError {
		t.Fatalf("guard must return an isError tool result, got: %+v", res)
	}
	found := false
	for _, c := range res.Content {
		if tc, ok := c.(mcpsdk.TextContent); ok && strings.Contains(tc.Text, "panic") {
			found = true
		}
	}
	if !found {
		t.Errorf("error result should mention the panic: %+v", res)
	}
}

// TestGuardStdioListenContextCancelLogging proves the stop-reason log on
// the context-cancel path (host shutting down) as well as the EOF path.
func TestGuardStdioListenContextCancelLogging(t *testing.T) {
	logBuf := captureLogRedirect(t)

	inR, inW := io.Pipe()
	m := server.NewMCPServer("ctx-cancel-test", "1.0.0")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- GuardStdioListen(server.NewStdioServer(m), ctx, "ctx-test", inR, io.Discard) }()

	time.Sleep(150 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		t.Logf("listener returned after ctx cancel: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatalf("listener did not return after context cancel")
	}
	_ = inW.Close()

	got := logBuf.String()
	if !strings.Contains(got, "listener ctx-test started") {
		t.Errorf("missing start log:\n%s", got)
	}
	if !strings.Contains(got, "listener ctx-test stopped") {
		t.Errorf("missing stop-reason log:\n%s", got)
	}
}

// resultHasPanicError reports whether a JSON-RPC response indicates the
// tool panicked (either an error member or an isError tool result).
func resultHasPanicError(msg map[string]any) bool {
	if _, ok := msg["error"]; ok {
		return true
	}
	r, ok := msg["result"].(map[string]any)
	if !ok {
		return false
	}
	if v, ok := r["isError"].(bool); ok && v {
		return true
	}
	return strings.Contains(fmt.Sprintf("%v", r["content"]), "panic")
}
