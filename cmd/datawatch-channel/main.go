// datawatch-channel — native Go MCP bridge between claude-code and the
// datawatch parent daemon. Replaces the embedded channel.js (Node.js +
// @modelcontextprotocol/sdk) so channel mode no longer requires a Node
// runtime on the host.
//
// Wire contract is byte-compatible with channel.js:
//
//   daemon → bridge:  HTTP POST 127.0.0.1:$DATAWATCH_CHANNEL_PORT/send
//                     {text, source, session_id}
//                     → forwarded as MCP notification to claude-code
//
//   bridge → daemon:  reply MCP tool ─→ POST $DATAWATCH_API_URL/api/channel/reply
//                                       {text, session_id}
//                     /permission     ─→ POST .../api/channel/permission
//                                       {request_id, behavior}
//
// Env vars (all match channel.js for drop-in swap):
//   DATAWATCH_CHANNEL_PORT  HTTP listen port (default 7433; 0 = random)
//   DATAWATCH_API_URL       parent API base URL (default http://localhost:8080)
//   DATAWATCH_TOKEN         bearer token for parent API (optional)
//   CLAUDE_SESSION_ID       session id to tag in notifications (optional)
//   DATAWATCH_NODE_BIN      ignored — present so old configs do not break
//
// Tracked under BL174.
package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

const (
	defaultChannelPort = 7433
	defaultAPIURL      = "http://localhost:8080"
	bridgeName         = "datawatch"
	bridgeVersion      = "0.1.0"
)

func main() {
	cfg := loadConfig()

	// ── startup diagnostic block ─────────────────────────────────────────────
	tokenStatus := "not set"
	if cfg.token != "" {
		tokenStatus = fmt.Sprintf("set (%d chars)", len(cfg.token))
	}
	fmt.Fprintf(os.Stderr, "[datawatch-channel] starting up\n")
	fmt.Fprintf(os.Stderr, "[datawatch-channel] config: api_url=%s channel_port=%d session_id=%q token=%s\n",
		cfg.apiURL, cfg.channelPort, cfg.sessionID, tokenStatus)

	// ── pre-flight: verify the daemon is reachable ───────────────────────────
	if err := probeDaemon(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "[datawatch-channel] WARN daemon health check failed: %v\n", err)
		fmt.Fprintf(os.Stderr, "[datawatch-channel] WARN tool discovery will likely fail; check DATAWATCH_API_URL=%s and that the daemon is running\n", cfg.apiURL)
	} else {
		fmt.Fprintf(os.Stderr, "[datawatch-channel] daemon reachable at %s\n", cfg.apiURL)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	mcpSrv := server.NewMCPServer(
		bridgeName, bridgeVersion,
		server.WithToolCapabilities(true),
		// BL302 S1 — resource capability (subscribe=false).
		server.WithResourceCapabilities(false, false),
		server.WithInstructions(`You are connected to the datawatch monitoring system.
Events arrive as <channel source="datawatch" ...>. Read and act on them.
When you have a response, use the reply tool to send it back.
When you need permission for a tool and permission relay is active,
the request will be forwarded to the user automatically.`),
	)

	// BL302 S3 — advertise sampling capability so the daemon can request
	// LLM completions from this client via sampling/createMessage.
	// The SDK's StdioServer handles the createMessage request/response
	// roundtrip automatically once the capability is declared here.
	mcpSrv.EnableSampling()

	bridge := &bridge{cfg: cfg, srv: mcpSrv}
	mcpSrv.AddTool(bridge.replyTool(), bridge.handleReply)

	// Discover all daemon tools and register generic forwarding handlers.
	// The reply tool above is the only hardcoded stub — it sends output back
	// through the channel, not through the daemon's tool surface.
	tools, err := bridge.discoverTools()
	if err != nil {
		fmt.Fprintf(os.Stderr, "[datawatch-channel] tool discovery failed: %v; continuing with reply-only\n", err)
	} else {
		for _, t := range tools {
			t := t
			mcpSrv.AddTool(t.asMCPTool(), bridge.makeForwarder(t.Name))
		}
		fmt.Fprintf(os.Stderr, "[datawatch-channel] discovered %d daemon tools\n", len(tools))
	}

	// BL302 S1 — discover resources + templates and register forwarding handlers.
	resources, err := bridge.discoverResources()
	if err != nil {
		fmt.Fprintf(os.Stderr, "[datawatch-channel] resource discovery failed (non-fatal): %v\n", err)
	} else {
		for _, r := range resources {
			r := r
			mcpSrv.AddResource(r.asMCPResource(), bridge.makeResourceForwarder(r.URI))
		}
		fmt.Fprintf(os.Stderr, "[datawatch-channel] discovered %d resources\n", len(resources))
	}

	templates, err := bridge.discoverResourceTemplates()
	if err != nil {
		fmt.Fprintf(os.Stderr, "[datawatch-channel] resource template discovery failed (non-fatal): %v\n", err)
	} else {
		for _, t := range templates {
			t := t
			mcpSrv.AddResourceTemplate(t.asMCPTemplate(), bridge.makeTemplateForwarder())
		}
		fmt.Fprintf(os.Stderr, "[datawatch-channel] discovered %d resource templates\n", len(templates))
	}

	// BL302 S4 — discover prompts and register forwarding handlers.
	prompts, err := bridge.discoverPrompts()
	if err != nil {
		fmt.Fprintf(os.Stderr, "[datawatch-channel] prompt discovery failed (non-fatal): %v\n", err)
	} else {
		for _, p := range prompts {
			p := p
			mcpSrv.AddPrompt(p.asMCPPrompt(), bridge.makePromptForwarder(p.Name))
		}
		fmt.Fprintf(os.Stderr, "[datawatch-channel] discovered %d prompts\n", len(prompts))
	}

	// Start the HTTP listener first so the daemon and channel can begin
	// pushing notifications immediately. Random port (0) picks a free
	// one — the daemon discovers it via /api/channel/ready.
	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", cfg.channelPort))
	if err != nil {
		fmt.Fprintf(os.Stderr, "[datawatch-channel] FATAL HTTP listen on port %d: %v\n", cfg.channelPort, err)
		if isAddrInUse(err) {
			owner := portOwner(cfg.channelPort)
			if owner != "" {
				fmt.Fprintf(os.Stderr, "[datawatch-channel] FATAL port %d is already in use by: %s\n", cfg.channelPort, owner)
			} else {
				fmt.Fprintf(os.Stderr, "[datawatch-channel] FATAL port %d is already in use (could not identify process)\n", cfg.channelPort)
			}
			fmt.Fprintf(os.Stderr, "[datawatch-channel] FIX  set DATAWATCH_CHANNEL_PORT=0 to auto-select a free port, or choose a different port\n")
		}
		os.Exit(1)
	}
	bridge.actualPort = listener.Addr().(*net.TCPAddr).Port
	fmt.Fprintf(os.Stderr, "[datawatch-channel] HTTP listener on 127.0.0.1:%d\n", bridge.actualPort)

	httpSrv := &http.Server{
		Handler:           bridge.httpHandler(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		if err := httpSrv.Serve(listener); err != nil && err != http.ErrServerClosed {
			fmt.Fprintf(os.Stderr, "[datawatch-channel] HTTP serve: %v\n", err)
		}
	}()

	// Tell the parent we are up; best-effort — the daemon may not be
	// running locally if this bridge was launched stand-alone for tests.
	if err := bridge.notifyReady(); err != nil {
		fmt.Fprintf(os.Stderr, "[datawatch-channel] WARN notify ready failed: %v\n", err)
		fmt.Fprintf(os.Stderr, "[datawatch-channel] WARN daemon at %s could not learn our port (%d); push notifications will not work\n", cfg.apiURL, bridge.actualPort)
		fmt.Fprintf(os.Stderr, "[datawatch-channel] WARN check DATAWATCH_API_URL and DATAWATCH_TOKEN\n")
	} else {
		fmt.Fprintf(os.Stderr, "[datawatch-channel] notified daemon: port=%d session_id=%q\n", bridge.actualPort, cfg.sessionID)
	}

	// MCP stdio transport — claude-code spawns us and talks over stdin/stdout.
	go func() {
		fmt.Fprintln(os.Stderr, "[datawatch-channel] MCP stdio transport starting")
		if err := server.NewStdioServer(mcpSrv).Listen(ctx, os.Stdin, os.Stdout); err != nil && err != context.Canceled {
			fmt.Fprintf(os.Stderr, "[datawatch-channel] MCP stdio: %v\n", err)
		}
		stop()
	}()

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = httpSrv.Shutdown(shutdownCtx)
}

// ── config ──────────────────────────────────────────────────────────────────

type config struct {
	channelPort int
	apiURL      string
	token       string
	sessionID   string
}

func loadConfig() config {
	return config{
		channelPort: envInt("DATAWATCH_CHANNEL_PORT", defaultChannelPort),
		apiURL:      envStr("DATAWATCH_API_URL", defaultAPIURL),
		token:       os.Getenv("DATAWATCH_TOKEN"),
		sessionID:   os.Getenv("CLAUDE_SESSION_ID"),
	}
}

func envStr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

// ── bridge ──────────────────────────────────────────────────────────────────

type bridge struct {
	cfg        config
	srv        *server.MCPServer
	actualPort int
	notified   atomic.Bool
}

func (b *bridge) replyTool() mcpsdk.Tool {
	return mcpsdk.NewTool("reply",
		mcpsdk.WithDescription("Send a reply message back through the datawatch channel"),
		mcpsdk.WithString("text",
			mcpsdk.Required(),
			mcpsdk.Description("The reply text to send"),
		),
		mcpsdk.WithString("session_id",
			mcpsdk.Description("Optional: datawatch session ID to associate the reply with"),
		),
	)
}

func (b *bridge) handleReply(ctx context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	text, _ := req.RequireString("text")
	if text == "" {
		return mcpsdk.NewToolResultError("text is required"), nil
	}
	sessionID := req.GetString("session_id", "")
	if sessionID == "" {
		sessionID = b.cfg.sessionID
	}
	body := map[string]any{"text": text, "session_id": sessionID}
	if err := b.postToParent(ctx, "/api/channel/reply", body); err != nil {
		return mcpsdk.NewToolResultError(fmt.Sprintf("post reply: %v", err)), nil
	}
	return mcpsdk.NewToolResultText("Reply sent."), nil
}

// callParent is postToParent generalised for either GET or POST + a
// body-returning shape. v5.27.7 added; the existing postToParent stays
// for the fire-and-forget reply / ready / permission paths that don't
// need the response body.
func (b *bridge) callParent(ctx context.Context, method, path string, body any) ([]byte, error) {
	var rdr io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		rdr = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, b.cfg.apiURL+path, rdr)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if b.cfg.token != "" {
		req.Header.Set("Authorization", "Bearer "+b.cfg.token)
	}
	client := &http.Client{Timeout: 30 * time.Second} // memory ops can be slow (embedding)
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	out, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("parent %s %s: %d %s", method, path, resp.StatusCode, string(out))
	}
	return out, nil
}

// httpHandler — accepts daemon→bridge POSTs on /send and /permission.
// Bound to 127.0.0.1 only; no auth (loopback only).
func (b *bridge) httpHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/send", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var msg struct {
			Text      string `json:"text"`
			Source    string `json:"source"`
			SessionID string `json:"session_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&msg); err != nil {
			http.Error(w, "bad json: "+err.Error(), http.StatusBadRequest)
			return
		}
		if msg.Source == "" {
			msg.Source = "datawatch"
		}
		b.srv.SendNotificationToAllClients("notifications/claude/channel", map[string]any{
			"content": msg.Text,
			"meta": map[string]any{
				"source":     msg.Source,
				"session_id": msg.SessionID,
			},
		})
		writeJSONOK(w)
	})
	mux.HandleFunc("/permission", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var msg struct {
			RequestID string `json:"request_id"`
			Behavior  string `json:"behavior"`
		}
		if err := json.NewDecoder(r.Body).Decode(&msg); err != nil {
			http.Error(w, "bad json: "+err.Error(), http.StatusBadRequest)
			return
		}
		b.srv.SendNotificationToAllClients("notifications/claude/channel/permission", map[string]any{
			"request_id": msg.RequestID,
			"behavior":   msg.Behavior,
		})
		writeJSONOK(w)
	})
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSONOK(w)
	})
	return mux
}

// notifyReady — POST /api/channel/ready so the parent learns the actual
// listening port (relevant when DATAWATCH_CHANNEL_PORT=0). Idempotent.
func (b *bridge) notifyReady() error {
	if !b.notified.CompareAndSwap(false, true) {
		return nil
	}
	body := map[string]any{
		"session_id": b.cfg.sessionID,
		"port":       b.actualPort,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return b.postToParent(ctx, "/api/channel/ready", body)
}

func (b *bridge) postToParent(ctx context.Context, path string, body any) error {
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, b.cfg.apiURL+path, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if b.cfg.token != "" {
		req.Header.Set("Authorization", "Bearer "+b.cfg.token)
	}
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 400 {
		buf, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("parent %s: %d %s", path, resp.StatusCode, string(buf))
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	return nil
}

func writeJSONOK(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"ok":true}`))
}

// ── diagnostic helpers ───────────────────────────────────────────────────────

// probeDaemon does a quick GET /api/health against the configured API URL
// to verify the daemon is reachable before we attempt tool discovery.
func probeDaemon(cfg config) error {
	client := &http.Client{Timeout: 3 * time.Second}
	req, err := http.NewRequest(http.MethodGet, cfg.apiURL+"/api/health", nil)
	if err != nil {
		return err
	}
	if cfg.token != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.token)
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("GET %s/api/health: %w", cfg.apiURL, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return fmt.Errorf("GET %s/api/health: HTTP %d — token may be wrong or missing (DATAWATCH_TOKEN)", cfg.apiURL, resp.StatusCode)
	}
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
		return fmt.Errorf("GET %s/api/health: HTTP %d %s", cfg.apiURL, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}

// isAddrInUse reports whether err is a "address already in use" bind error.
func isAddrInUse(err error) bool {
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		var syscallErr *os.SyscallError
		if errors.As(opErr.Err, &syscallErr) {
			return errors.Is(syscallErr.Err, syscall.EADDRINUSE)
		}
		return errors.Is(opErr.Err, syscall.EADDRINUSE)
	}
	return false
}

// portOwner tries to identify the process holding a TCP port by reading
// /proc/net/tcp (Linux only). Returns an empty string on non-Linux or error.
func portOwner(port int) string {
	f, err := os.Open("/proc/net/tcp")
	if err != nil {
		return "" // non-Linux or permission denied
	}
	defer func() { _ = f.Close() }()

	// /proc/net/tcp columns: sl local_address rem_address ...
	// local_address is hex "IP:PORT" in little-endian byte order.
	target := fmt.Sprintf("%04X", port)
	scanner := bufio.NewScanner(f)
	scanner.Scan() // skip header
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 2 {
			continue
		}
		parts := strings.SplitN(fields[1], ":", 2)
		if len(parts) == 2 && strings.EqualFold(parts[1], target) {
			// Found a listener on this port. Try to decode the inode → pid.
			if len(fields) >= 10 {
				inode := fields[9]
				if pid := inodeToPID(inode); pid != "" {
					return fmt.Sprintf("pid %s (%s)", pid, pidName(pid))
				}
			}
			return fmt.Sprintf("unknown process (inode lookup failed)")
		}
	}
	return ""
}

// inodeToPID walks /proc/*/fd/* looking for a socket with the given inode.
func inodeToPID(inode string) string {
	target := "socket:[" + inode + "]"
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return ""
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		fdDir := "/proc/" + e.Name() + "/fd"
		fds, err := os.ReadDir(fdDir)
		if err != nil {
			continue
		}
		for _, fd := range fds {
			link, err := os.Readlink(fdDir + "/" + fd.Name())
			if err == nil && link == target {
				return e.Name()
			}
		}
	}
	return ""
}

// pidName reads /proc/<pid>/comm for the process name.
func pidName(pid string) string {
	data, err := os.ReadFile("/proc/" + pid + "/comm")
	if err != nil {
		return "?"
	}
	return strings.TrimSpace(string(data))
}

