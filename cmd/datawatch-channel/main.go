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
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
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

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	mcpSrv := server.NewMCPServer(
		bridgeName, bridgeVersion,
		server.WithToolCapabilities(true),
		server.WithInstructions(`You are connected to the datawatch monitoring system.
Events arrive as <channel source="datawatch" ...>. Read and act on them.
When you have a response, use the reply tool to send it back.
When you need permission for a tool and permission relay is active,
the request will be forwarded to the user automatically.`),
	)

	bridge := &bridge{cfg: cfg, srv: mcpSrv}
	mcpSrv.AddTool(bridge.replyTool(), bridge.handleReply)

	// v5.27.7 (BL212, datawatch#29) — operator-flagged: claude-code
	// sessions had no MCP path to the parent's memory subsystem. The
	// daemon's stdio MCP server registers memory tools, but this
	// per-session bridge process only exposed `reply`. Adding the
	// memory tools here (each forwards to the parent's existing
	// /api/memory/* REST surface) closes the gap so claude-code can
	// recall + remember + list memories without curl workarounds.
	mcpSrv.AddTool(bridge.memoryRememberTool(), bridge.handleMemoryRemember)
	mcpSrv.AddTool(bridge.memoryRecallTool(), bridge.handleMemoryRecall)
	mcpSrv.AddTool(bridge.memoryListTool(), bridge.handleMemoryList)
	mcpSrv.AddTool(bridge.memoryForgetTool(), bridge.handleMemoryForget)
	mcpSrv.AddTool(bridge.memoryStatsTool(), bridge.handleMemoryStats)

	// Start the HTTP listener first so the daemon and channel can begin
	// pushing notifications immediately. Random port (0) picks a free
	// one — the daemon discovers it via /api/channel/ready.
	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", cfg.channelPort))
	if err != nil {
		fmt.Fprintf(os.Stderr, "[datawatch-channel] HTTP listen: %v\n", err)
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
		fmt.Fprintf(os.Stderr, "[datawatch-channel] notify ready (non-fatal): %v\n", err)
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

// ── memory tools (BL212, v5.27.7) ───────────────────────────────────────────
// Forwarders to the parent's /api/memory/* REST surface so claude-code
// sessions can use memory through the same bridge they already speak to
// for reply / channel notifications. Each handler is intentionally thin:
// the parent owns validation, dedup, embedding, etc. — the bridge's
// only job is to plumb the call through.

func (b *bridge) memoryRememberTool() mcpsdk.Tool {
	return mcpsdk.NewTool("memory_remember",
		mcpsdk.WithDescription("Save a memory (note, decision, rule) for the current project to the parent datawatch daemon's episodic store. The parent embeds + dedups."),
		mcpsdk.WithString("text",
			mcpsdk.Required(),
			mcpsdk.Description("The text to remember"),
		),
		mcpsdk.WithString("project_dir",
			mcpsdk.Description("Project directory (empty = parent's default project)"),
		),
	)
}

func (b *bridge) handleMemoryRemember(ctx context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	text, _ := req.RequireString("text")
	if text == "" {
		return mcpsdk.NewToolResultError("text is required"), nil
	}
	body := map[string]any{
		"text":        text,
		"project_dir": req.GetString("project_dir", ""),
	}
	out, err := b.callParent(ctx, http.MethodPost, "/api/memory/save", body)
	if err != nil {
		return mcpsdk.NewToolResultError(fmt.Sprintf("save: %v", err)), nil
	}
	return mcpsdk.NewToolResultText(string(out)), nil
}

func (b *bridge) memoryRecallTool() mcpsdk.Tool {
	return mcpsdk.NewTool("memory_recall",
		mcpsdk.WithDescription("Semantic search across the parent daemon's episodic memory. Returns top matches ranked by similarity."),
		mcpsdk.WithString("query",
			mcpsdk.Required(),
			mcpsdk.Description("Search query"),
		),
	)
}

func (b *bridge) handleMemoryRecall(ctx context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	query, _ := req.RequireString("query")
	if query == "" {
		return mcpsdk.NewToolResultError("query is required"), nil
	}
	out, err := b.callParent(ctx, http.MethodGet,
		"/api/memory/search?q="+urlQueryEscape(query), nil)
	if err != nil {
		return mcpsdk.NewToolResultError(fmt.Sprintf("recall: %v", err)), nil
	}
	return mcpsdk.NewToolResultText(string(out)), nil
}

func (b *bridge) memoryListTool() mcpsdk.Tool {
	return mcpsdk.NewTool("memory_list",
		mcpsdk.WithDescription("List the most recently saved memories. Optional project_dir filter."),
		mcpsdk.WithString("project_dir",
			mcpsdk.Description("Project directory filter (empty = default project)"),
		),
		mcpsdk.WithNumber("n",
			mcpsdk.Description("Number of memories to return (default 20)"),
		),
	)
}

func (b *bridge) handleMemoryList(ctx context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	n := req.GetInt("n", 20)
	path := fmt.Sprintf("/api/memory/list?n=%d", n)
	if pd := req.GetString("project_dir", ""); pd != "" {
		path += "&project_dir=" + urlQueryEscape(pd)
	}
	out, err := b.callParent(ctx, http.MethodGet, path, nil)
	if err != nil {
		return mcpsdk.NewToolResultError(fmt.Sprintf("list: %v", err)), nil
	}
	return mcpsdk.NewToolResultText(string(out)), nil
}

func (b *bridge) memoryForgetTool() mcpsdk.Tool {
	return mcpsdk.NewTool("memory_forget",
		mcpsdk.WithDescription("Delete a memory by its numeric ID."),
		mcpsdk.WithNumber("id",
			mcpsdk.Required(),
			mcpsdk.Description("Memory ID to delete"),
		),
	)
}

func (b *bridge) handleMemoryForget(ctx context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	id := req.GetInt("id", 0)
	if id <= 0 {
		return mcpsdk.NewToolResultError("id is required and must be positive"), nil
	}
	body := map[string]any{"id": id}
	out, err := b.callParent(ctx, http.MethodPost, "/api/memory/delete", body)
	if err != nil {
		return mcpsdk.NewToolResultError(fmt.Sprintf("forget: %v", err)), nil
	}
	return mcpsdk.NewToolResultText(string(out)), nil
}

func (b *bridge) memoryStatsTool() mcpsdk.Tool {
	return mcpsdk.NewTool("memory_stats",
		mcpsdk.WithDescription("Memory subsystem stats from the parent daemon — total counts, db size, encryption status."),
	)
}

func (b *bridge) handleMemoryStats(ctx context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	out, err := b.callParent(ctx, http.MethodGet, "/api/memory/stats", nil)
	if err != nil {
		return mcpsdk.NewToolResultError(fmt.Sprintf("stats: %v", err)), nil
	}
	return mcpsdk.NewToolResultText(string(out)), nil
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
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("parent %s %s: %d %s", method, path, resp.StatusCode, string(out))
	}
	return out, nil
}

// urlQueryEscape — minimal query-string escape for the GET paths.
// Avoids a `net/url` import; the bridge uses tight stdlib only.
func urlQueryEscape(s string) string {
	var out []byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9',
			c == '-', c == '_', c == '.', c == '~':
			out = append(out, c)
		case c == ' ':
			out = append(out, '+')
		default:
			out = append(out, '%',
				"0123456789ABCDEF"[c>>4],
				"0123456789ABCDEF"[c&0xF])
		}
	}
	return string(out)
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
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		buf, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("parent %s: %d %s", path, resp.StatusCode, string(buf))
	}
	io.Copy(io.Discard, resp.Body)
	return nil
}

func writeJSONOK(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"ok":true}`))
}
