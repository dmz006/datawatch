// Package mcp exposes datawatch session management as an MCP (Model Context Protocol) server.
// This allows Cursor, Claude Desktop, VS Code, and any other MCP-compatible client to list,
// start, monitor, and interact with AI coding sessions directly from the IDE.
//
// Two transports are supported:
//
//   - stdio (default): run as a subprocess from Cursor/Claude Desktop MCP config.
//   - HTTP/SSE: remote AI clients connect over HTTPS — see MCPConfig.SSEEnabled.
//
// Exposed tools:
//
//	list_sessions       — list all sessions on this host
//	start_session       — start a new AI session for a task
//	session_output      — get the last N lines of output from a session
//	session_timeline    — get the structured event timeline for a session
//	send_input          — send text input to a session waiting for a response
//	kill_session        — terminate a session
//	rename_session      — set a human-readable name for a session
//	stop_all_sessions   — kill all running/waiting sessions
//	get_alerts          — list recent system alerts
//	mark_alert_read     — mark an alert as read
//	restart_daemon      — restart the datawatch daemon
//	get_version         — get current and latest version info
//	list_saved_commands — list the saved command library
//	send_saved_command  — send a named saved command to a session
//	schedule_add        — schedule a command for a session
//	schedule_list       — list pending scheduled commands
//	schedule_cancel     — cancel a pending scheduled command
package mcp

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/dmz006/datawatch/internal/alerts"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/session"
	"github.com/dmz006/datawatch/internal/stats"
	"github.com/dmz006/datawatch/internal/tlsutil"
)

// Server wraps the MCP server with session manager access.
type Server struct {
	hostname   string
	manager    *session.Manager
	cfg        *config.MCPConfig
	dataDir    string
	srv        *server.MCPServer
	alertStore *alerts.Store
	schedStore *session.ScheduleStore
	cmdLib     *session.CmdLibrary
	restartFn  func()
	version    string
	// latestVersion returns the latest release tag (no "v" prefix). May be nil.
	latestVersion func() (string, error)
	// chanStats tracks MCP request/response counts
	chanStats *stats.ChannelCounters
}

// Options holds optional dependencies for the MCP server.
type Options struct {
	AlertStore    *alerts.Store
	SchedStore    *session.ScheduleStore
	CmdLib        *session.CmdLibrary
	RestartFn     func()
	Version       string
	LatestVersion func() (string, error)
}

// New creates a new MCP server backed by the given session manager.
func New(hostname string, manager *session.Manager, cfg *config.MCPConfig, dataDir string, opts Options) *Server {
	s := &Server{
		hostname:      hostname,
		manager:       manager,
		cfg:           cfg,
		dataDir:       dataDir,
		alertStore:    opts.AlertStore,
		schedStore:    opts.SchedStore,
		cmdLib:        opts.CmdLib,
		restartFn:     opts.RestartFn,
		version:       opts.Version,
		latestVersion: opts.LatestVersion,
	}

	mcpSrv := server.NewMCPServer(
		"datawatch",
		"1.0.0",
		server.WithToolCapabilities(true),
	)

	// tracked wraps an MCP handler with channel stats tracking
	tracked := func(fn func(context.Context, mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error)) func(context.Context, mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
		return func(ctx context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
			result, err := fn(ctx, req)
			if s.chanStats != nil {
				reqSize := len(fmt.Sprintf("%v", req.Params.Arguments))
				respSize := 0
				if result != nil {
					for _, c := range result.Content {
						if tc, ok := c.(mcpsdk.TextContent); ok {
							respSize += len(tc.Text)
						}
					}
				}
				s.chanStats.RecordRecv(reqSize)
				s.chanStats.RecordSent(respSize)
				if err != nil {
					s.chanStats.RecordError()
				}
			}
			return result, err
		}
	}

	mcpSrv.AddTool(s.toolListSessions(), tracked(s.handleListSessions))
	mcpSrv.AddTool(s.toolStartSession(), tracked(s.handleStartSession))
	mcpSrv.AddTool(s.toolSessionOutput(), tracked(s.handleSessionOutput))
	mcpSrv.AddTool(s.toolSessionTimeline(), tracked(s.handleSessionTimeline))
	mcpSrv.AddTool(s.toolSendInput(), tracked(s.handleSendInput))
	mcpSrv.AddTool(s.toolKillSession(), tracked(s.handleKillSession))
	mcpSrv.AddTool(s.toolRenameSession(), tracked(s.handleRenameSession))
	mcpSrv.AddTool(s.toolStopAllSessions(), tracked(s.handleStopAllSessions))
	mcpSrv.AddTool(s.toolGetAlerts(), tracked(s.handleGetAlerts))
	mcpSrv.AddTool(s.toolMarkAlertRead(), tracked(s.handleMarkAlertRead))
	mcpSrv.AddTool(s.toolRestartDaemon(), tracked(s.handleRestartDaemon))
	mcpSrv.AddTool(s.toolGetVersion(), tracked(s.handleGetVersion))
	mcpSrv.AddTool(s.toolListSavedCommands(), tracked(s.handleListSavedCommands))
	mcpSrv.AddTool(s.toolSendSavedCommand(), tracked(s.handleSendSavedCommand))
	mcpSrv.AddTool(s.toolScheduleAdd(), tracked(s.handleScheduleAdd))
	mcpSrv.AddTool(s.toolScheduleList(), tracked(s.handleScheduleList))
	mcpSrv.AddTool(s.toolScheduleCancel(), tracked(s.handleScheduleCancel))

	s.srv = mcpSrv
	return s
}

// ToolDoc describes a single MCP tool for documentation.
type ToolDoc struct {
	Name        string     `json:"name"`
	Description string     `json:"description"`
	Parameters  []ParamDoc `json:"parameters,omitempty"`
}

// ParamDoc describes a tool parameter.
type ParamDoc struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Required    bool   `json:"required"`
	Description string `json:"description"`
}

// SetChannelStats sets the stats counters for MCP request/response tracking.
func (s *Server) SetChannelStats(cs *stats.ChannelCounters) {
	s.chanStats = cs
}

// trackCall records a tool call in the channel stats.
func (s *Server) trackCall(reqSize, respSize int) {
	if s.chanStats != nil {
		s.chanStats.RecordRecv(reqSize)
		s.chanStats.RecordSent(respSize)
	}
}

// ToolDocs returns structured documentation for all registered MCP tools.
func (s *Server) ToolDocs() []ToolDoc {
	type toolDef struct {
		fn   func() mcpsdk.Tool
		name string
	}
	defs := []toolDef{
		{s.toolListSessions, "list_sessions"},
		{s.toolStartSession, "start_session"},
		{s.toolSessionOutput, "session_output"},
		{s.toolSessionTimeline, "session_timeline"},
		{s.toolSendInput, "send_input"},
		{s.toolKillSession, "kill_session"},
		{s.toolRenameSession, "rename_session"},
		{s.toolStopAllSessions, "stop_all_sessions"},
		{s.toolGetAlerts, "get_alerts"},
		{s.toolMarkAlertRead, "mark_alert_read"},
		{s.toolRestartDaemon, "restart_daemon"},
		{s.toolGetVersion, "get_version"},
		{s.toolListSavedCommands, "list_saved_commands"},
		{s.toolSendSavedCommand, "send_saved_command"},
		{s.toolScheduleAdd, "schedule_add"},
		{s.toolScheduleList, "schedule_list"},
		{s.toolScheduleCancel, "schedule_cancel"},
	}

	var docs []ToolDoc
	for _, d := range defs {
		tool := d.fn()
		doc := ToolDoc{
			Name:        tool.Name,
			Description: tool.Description,
		}
		if tool.InputSchema.Properties != nil {
			required := make(map[string]bool)
			for _, r := range tool.InputSchema.Required {
				required[r] = true
			}
			for name, prop := range tool.InputSchema.Properties {
				p := ParamDoc{
					Name:     name,
					Required: required[name],
				}
				if m, ok := prop.(map[string]interface{}); ok {
					if t, ok := m["type"].(string); ok {
						p.Type = t
					}
					if d, ok := m["description"].(string); ok {
						p.Description = d
					}
				}
				doc.Parameters = append(doc.Parameters, p)
			}
		}
		docs = append(docs, doc)
	}
	return docs
}

// ServeStdio runs the MCP server over stdin/stdout (for local clients like Cursor).
// Blocks until ctx is cancelled or stdin closes.
func (s *Server) ServeStdio(ctx context.Context) error {
	return server.NewStdioServer(s.srv).Listen(ctx, nil, nil)
}

// ServeSSE starts an HTTP/SSE MCP server for remote AI clients.
// The SSEHost field supports comma-separated addresses for multi-interface binding.
// Blocks until ctx is cancelled.
func (s *Server) ServeSSE(ctx context.Context) error {
	hosts := strings.Split(s.cfg.SSEHost, ",")
	if len(hosts) == 0 {
		hosts = []string{"127.0.0.1"}
	}

	// Use first host for the base URL (SSE server needs one canonical URL)
	firstAddr := fmt.Sprintf("%s:%d", strings.TrimSpace(hosts[0]), s.cfg.SSEPort)
	scheme := "http"
	if s.cfg.TLSEnabled {
		scheme = "https"
	}
	baseURL := fmt.Sprintf("%s://%s", scheme, firstAddr)

	sseSrv := server.NewSSEServer(s.srv, server.WithBaseURL(baseURL))

	var handler http.Handler = sseSrv
	if s.cfg.Token != "" {
		handler = bearerAuthMiddleware(s.cfg.Token, sseSrv)
	}

	mux := http.NewServeMux()
	mux.Handle("/", handler)

	httpSrv := &http.Server{
		Handler:     mux,
		ReadTimeout: 30 * time.Second,
		IdleTimeout: 120 * time.Second,
	}

	var tlsCfg *tls.Config
	if s.cfg.TLSEnabled {
		var err error
		tlsCfg, err = tlsutil.Build(tlsutil.Config{
			Enabled:      true,
			CertFile:     s.cfg.TLSCert,
			KeyFile:      s.cfg.TLSKey,
			AutoGenerate: s.cfg.TLSAutoGenerate,
			DataDir:      s.dataDir,
			Name:         "mcp",
		})
		if err != nil {
			return fmt.Errorf("MCP TLS setup: %w", err)
		}
		httpSrv.TLSConfig = tlsCfg
	}

	errCh := make(chan error, len(hosts))
	for _, host := range hosts {
		host = strings.TrimSpace(host)
		if host == "" {
			continue
		}
		addr := fmt.Sprintf("%s:%d", host, s.cfg.SSEPort)
		listener, err := net.Listen("tcp", addr)
		if err != nil {
			return fmt.Errorf("MCP SSE listen %s: %w", addr, err)
		}
		if tlsCfg != nil {
			go func(l net.Listener, a string) { errCh <- httpSrv.ServeTLS(l, "", "") }(listener, addr)
			fmt.Printf("datawatch MCP SSE server listening on https://%s (TLS 1.3+)\n", addr)
		} else {
			go func(l net.Listener, a string) { errCh <- httpSrv.Serve(l) }(listener, addr)
			fmt.Printf("datawatch MCP SSE server listening on http://%s\n", addr)
		}
	}

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return httpSrv.Shutdown(shutdownCtx)
	case err := <-errCh:
		return err
	}
}

// bearerAuthMiddleware requires a valid Authorization: Bearer <token> header.
func bearerAuthMiddleware(token string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+token {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// ---- tool definitions -------------------------------------------------------

func (s *Server) toolListSessions() mcpsdk.Tool {
	return mcpsdk.NewTool("list_sessions",
		mcpsdk.WithDescription("List all AI coding sessions on this host, including their state and task description."),
	)
}

func (s *Server) toolStartSession() mcpsdk.Tool {
	return mcpsdk.NewTool("start_session",
		mcpsdk.WithDescription("Start a new AI coding session for a task. Returns the session ID."),
		mcpsdk.WithString("task",
			mcpsdk.Required(),
			mcpsdk.Description("Task description to send to the AI"),
		),
		mcpsdk.WithString("project_dir",
			mcpsdk.Description("Absolute path to the project directory. Defaults to home directory."),
		),
	)
}

func (s *Server) toolSessionOutput() mcpsdk.Tool {
	return mcpsdk.NewTool("session_output",
		mcpsdk.WithDescription("Get the last N lines of output from an AI coding session."),
		mcpsdk.WithString("session_id",
			mcpsdk.Required(),
			mcpsdk.Description("Session ID (short 4-char hex or full hostname-hex ID)"),
		),
		mcpsdk.WithNumber("lines",
			mcpsdk.Description("Number of lines to return (default: 50)"),
		),
	)
}

func (s *Server) toolSessionTimeline() mcpsdk.Tool {
	return mcpsdk.NewTool("session_timeline",
		mcpsdk.WithDescription("Get the structured event timeline for a session (state changes, inputs, rate limits, etc.)."),
		mcpsdk.WithString("session_id",
			mcpsdk.Required(),
			mcpsdk.Description("Session ID"),
		),
	)
}

func (s *Server) toolSendInput() mcpsdk.Tool {
	return mcpsdk.NewTool("send_input",
		mcpsdk.WithDescription("Send text input to a session that is waiting for a response."),
		mcpsdk.WithString("session_id",
			mcpsdk.Required(),
			mcpsdk.Description("Session ID"),
		),
		mcpsdk.WithString("text",
			mcpsdk.Required(),
			mcpsdk.Description("Text to send as input"),
		),
	)
}

func (s *Server) toolKillSession() mcpsdk.Tool {
	return mcpsdk.NewTool("kill_session",
		mcpsdk.WithDescription("Terminate an AI coding session."),
		mcpsdk.WithString("session_id",
			mcpsdk.Required(),
			mcpsdk.Description("Session ID to kill"),
		),
	)
}

func (s *Server) toolRenameSession() mcpsdk.Tool {
	return mcpsdk.NewTool("rename_session",
		mcpsdk.WithDescription("Set or update the human-readable name for a session."),
		mcpsdk.WithString("session_id",
			mcpsdk.Required(),
			mcpsdk.Description("Session ID"),
		),
		mcpsdk.WithString("name",
			mcpsdk.Required(),
			mcpsdk.Description("New human-readable name"),
		),
	)
}

func (s *Server) toolStopAllSessions() mcpsdk.Tool {
	return mcpsdk.NewTool("stop_all_sessions",
		mcpsdk.WithDescription("Kill all running and waiting-input sessions on this host."),
	)
}

func (s *Server) toolGetAlerts() mcpsdk.Tool {
	return mcpsdk.NewTool("get_alerts",
		mcpsdk.WithDescription("List recent system alerts (rate limits, trust dialogs, filter matches, etc.)."),
		mcpsdk.WithNumber("limit",
			mcpsdk.Description("Maximum number of alerts to return (default: 10)"),
		),
		mcpsdk.WithString("session_id",
			mcpsdk.Description("Filter alerts to this session ID (optional)"),
		),
	)
}

func (s *Server) toolMarkAlertRead() mcpsdk.Tool {
	return mcpsdk.NewTool("mark_alert_read",
		mcpsdk.WithDescription("Mark an alert as read by ID, or mark all alerts as read."),
		mcpsdk.WithString("id",
			mcpsdk.Description("Alert ID to mark as read. Omit to mark all alerts as read."),
		),
	)
}

func (s *Server) toolRestartDaemon() mcpsdk.Tool {
	return mcpsdk.NewTool("restart_daemon",
		mcpsdk.WithDescription("Restart the datawatch daemon. Active tmux sessions are preserved."),
	)
}

func (s *Server) toolGetVersion() mcpsdk.Tool {
	return mcpsdk.NewTool("get_version",
		mcpsdk.WithDescription("Get the current datawatch version and check for updates."),
	)
}

func (s *Server) toolListSavedCommands() mcpsdk.Tool {
	return mcpsdk.NewTool("list_saved_commands",
		mcpsdk.WithDescription("List the saved command library (named reusable commands like approve/reject)."),
	)
}

func (s *Server) toolSendSavedCommand() mcpsdk.Tool {
	return mcpsdk.NewTool("send_saved_command",
		mcpsdk.WithDescription("Send a named saved command to a session."),
		mcpsdk.WithString("session_id",
			mcpsdk.Required(),
			mcpsdk.Description("Session ID"),
		),
		mcpsdk.WithString("command_name",
			mcpsdk.Required(),
			mcpsdk.Description("Name of the saved command (e.g. 'approve', 'reject')"),
		),
	)
}

func (s *Server) toolScheduleAdd() mcpsdk.Tool {
	return mcpsdk.NewTool("schedule_add",
		mcpsdk.WithDescription("Schedule a command to be sent to a session. Use run_at='prompt' to fire on next input prompt."),
		mcpsdk.WithString("session_id",
			mcpsdk.Required(),
			mcpsdk.Description("Session ID"),
		),
		mcpsdk.WithString("command",
			mcpsdk.Required(),
			mcpsdk.Description("Command text to send"),
		),
		mcpsdk.WithString("run_at",
			mcpsdk.Description("When to run: 'prompt' (next input prompt), 'HH:MM' (24h today), or RFC3339. Default: prompt."),
		),
	)
}

func (s *Server) toolScheduleList() mcpsdk.Tool {
	return mcpsdk.NewTool("schedule_list",
		mcpsdk.WithDescription("List all pending scheduled commands."),
	)
}

func (s *Server) toolScheduleCancel() mcpsdk.Tool {
	return mcpsdk.NewTool("schedule_cancel",
		mcpsdk.WithDescription("Cancel a pending scheduled command by ID."),
		mcpsdk.WithString("id",
			mcpsdk.Required(),
			mcpsdk.Description("Schedule entry ID to cancel"),
		),
	)
}

// ---- handlers ---------------------------------------------------------------

func (s *Server) handleListSessions(_ context.Context, _ mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	sessions := s.manager.ListSessions()
	if len(sessions) == 0 {
		return mcpsdk.NewToolResultText("No active sessions."), nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Sessions on %s:\n\n", s.hostname))
	for _, sess := range sessions {
		if sess.Hostname != s.hostname {
			continue
		}
		sb.WriteString(fmt.Sprintf("ID:      %s\n", sess.ID))
		if sess.Name != "" {
			sb.WriteString(fmt.Sprintf("Name:    %s\n", sess.Name))
		}
		sb.WriteString(fmt.Sprintf("State:   %s\n", sess.State))
		sb.WriteString(fmt.Sprintf("Task:    %s\n", sess.Task))
		sb.WriteString(fmt.Sprintf("Dir:     %s\n", sess.ProjectDir))
		sb.WriteString(fmt.Sprintf("Updated: %s\n", sess.UpdatedAt.Format(time.RFC3339)))
		if sess.State == session.StateWaitingInput && sess.LastPrompt != "" {
			sb.WriteString(fmt.Sprintf("Prompt:  %s\n", sess.LastPrompt))
		}
		sb.WriteString("\n")
	}
	return mcpsdk.NewToolResultText(sb.String()), nil
}

func (s *Server) handleStartSession(ctx context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	task := req.GetString("task", "")
	if strings.TrimSpace(task) == "" {
		return mcpsdk.NewToolResultText("Error: task is required"), nil
	}
	projectDir := req.GetString("project_dir", "")

	startCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	sess, err := s.manager.Start(startCtx, task, "mcp", projectDir)
	if err != nil {
		return mcpsdk.NewToolResultText(fmt.Sprintf("Error starting session: %v", err)), nil
	}

	return mcpsdk.NewToolResultText(fmt.Sprintf(
		"Session started.\nID:      %s\nTask:    %s\nDir:     %s\nTmux:    %s\n\nUse session_output(id=%q) to follow progress.",
		sess.ID, sess.Task, sess.ProjectDir, sess.TmuxSession, sess.ID,
	)), nil
}

func (s *Server) handleSessionOutput(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	id := req.GetString("session_id", "")
	if id == "" {
		return mcpsdk.NewToolResultText("Error: session_id is required"), nil
	}

	n := req.GetInt("lines", 50)
	if n <= 0 {
		n = 50
	}

	sess, ok := s.manager.GetSession(id)
	if !ok {
		return mcpsdk.NewToolResultText(fmt.Sprintf("Session %q not found.", id)), nil
	}

	out, err := s.manager.TailOutput(sess.FullID, n)
	if err != nil {
		return mcpsdk.NewToolResultText(fmt.Sprintf("Error reading output: %v", err)), nil
	}

	header := fmt.Sprintf("[%s] State: %s | Task: %s\n---\n", sess.ID, sess.State, sess.Task)
	if sess.State == session.StateWaitingInput {
		header += fmt.Sprintf("Waiting for input: %s\nUse send_input(session_id=%q, text=...) to respond.\n---\n",
			sess.LastPrompt, sess.ID)
	}
	return mcpsdk.NewToolResultText(header+out), nil
}

func (s *Server) handleSessionTimeline(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	id := req.GetString("session_id", "")
	if id == "" {
		return mcpsdk.NewToolResultText("Error: session_id is required"), nil
	}

	sess, ok := s.manager.GetSession(id)
	if !ok {
		return mcpsdk.NewToolResultText(fmt.Sprintf("Session %q not found.", id)), nil
	}

	lines, err := s.manager.ReadTimeline(sess.FullID)
	if err != nil {
		return mcpsdk.NewToolResultText(fmt.Sprintf("Error reading timeline: %v", err)), nil
	}

	if len(lines) == 0 {
		return mcpsdk.NewToolResultText(fmt.Sprintf("[%s] No timeline events recorded yet.", sess.ID)), nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("[%s] Timeline (format: timestamp | event | details):\n\n", sess.ID))
	for _, l := range lines {
		sb.WriteString(l)
		sb.WriteByte('\n')
	}
	return mcpsdk.NewToolResultText(sb.String()), nil
}

func (s *Server) handleSendInput(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	id := req.GetString("session_id", "")
	if id == "" {
		return mcpsdk.NewToolResultText("Error: session_id is required"), nil
	}
	text := req.GetString("text", "")
	if text == "" {
		return mcpsdk.NewToolResultText("Error: text is required"), nil
	}

	sess, ok := s.manager.GetSession(id)
	if !ok {
		return mcpsdk.NewToolResultText(fmt.Sprintf("Session %q not found.", id)), nil
	}

	if err := s.manager.SendInput(sess.FullID, text, "mcp"); err != nil {
		return mcpsdk.NewToolResultText(fmt.Sprintf("Error sending input: %v", err)), nil
	}
	return mcpsdk.NewToolResultText(fmt.Sprintf("Input sent to session %s.", sess.ID)), nil
}

func (s *Server) handleKillSession(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	id := req.GetString("session_id", "")
	if id == "" {
		return mcpsdk.NewToolResultText("Error: session_id is required"), nil
	}

	sess, ok := s.manager.GetSession(id)
	if !ok {
		return mcpsdk.NewToolResultText(fmt.Sprintf("Session %q not found.", id)), nil
	}

	if err := s.manager.Kill(sess.FullID); err != nil {
		return mcpsdk.NewToolResultText(fmt.Sprintf("Error killing session: %v", err)), nil
	}
	return mcpsdk.NewToolResultText(fmt.Sprintf("Session %s killed.", sess.ID)), nil
}

func (s *Server) handleRenameSession(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	id := req.GetString("session_id", "")
	name := req.GetString("name", "")
	if id == "" || name == "" {
		return mcpsdk.NewToolResultText("Error: session_id and name are required"), nil
	}

	sess, ok := s.manager.GetSession(id)
	if !ok {
		return mcpsdk.NewToolResultText(fmt.Sprintf("Session %q not found.", id)), nil
	}

	if err := s.manager.Rename(sess.FullID, name); err != nil {
		return mcpsdk.NewToolResultText(fmt.Sprintf("Error renaming session: %v", err)), nil
	}
	return mcpsdk.NewToolResultText(fmt.Sprintf("Session %s renamed to %q.", sess.ID, name)), nil
}

func (s *Server) handleStopAllSessions(_ context.Context, _ mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	sessions := s.manager.ListSessions()
	var killed, skipped int
	for _, sess := range sessions {
		if sess.Hostname != s.hostname {
			continue
		}
		if sess.State == session.StateRunning || sess.State == session.StateWaitingInput {
			if err := s.manager.Kill(sess.FullID); err == nil {
				killed++
			} else {
				skipped++
			}
		}
	}
	return mcpsdk.NewToolResultText(fmt.Sprintf("Stopped %d session(s). %d skipped.", killed, skipped)), nil
}

func (s *Server) handleGetAlerts(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	if s.alertStore == nil {
		return mcpsdk.NewToolResultText("Alert store not available."), nil
	}

	limit := req.GetInt("limit", 10)
	if limit <= 0 {
		limit = 10
	}
	filterSess := req.GetString("session_id", "")

	all := s.alertStore.List()
	var sb strings.Builder
	count := 0
	for _, a := range all {
		if filterSess != "" && a.SessionID != filterSess {
			continue
		}
		if count >= limit {
			break
		}
		readMark := ""
		if !a.Read {
			readMark = " [unread]"
		}
		sessLabel := ""
		if a.SessionID != "" {
			sessLabel = fmt.Sprintf(" [%s]", a.SessionID)
		}
		sb.WriteString(fmt.Sprintf("[%s] %s %s%s%s — %s\n  %s\n\n",
			a.ID,
			a.CreatedAt.Format("15:04:05"),
			strings.ToUpper(string(a.Level)),
			sessLabel,
			readMark,
			a.Title,
			a.Body,
		))
		count++
	}
	if count == 0 {
		return mcpsdk.NewToolResultText("No alerts."), nil
	}
	return mcpsdk.NewToolResultText(sb.String()), nil
}

func (s *Server) handleMarkAlertRead(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	if s.alertStore == nil {
		return mcpsdk.NewToolResultText("Alert store not available."), nil
	}
	id := req.GetString("id", "")
	if id == "" {
		s.alertStore.MarkAllRead()
		return mcpsdk.NewToolResultText("All alerts marked as read."), nil
	}
	if err := s.alertStore.MarkRead(id); err != nil {
		return mcpsdk.NewToolResultText(fmt.Sprintf("Error: %v", err)), nil
	}
	return mcpsdk.NewToolResultText(fmt.Sprintf("Alert %s marked as read.", id)), nil
}

func (s *Server) handleRestartDaemon(_ context.Context, _ mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	if s.restartFn == nil {
		return mcpsdk.NewToolResultText("Restart not available (not running as daemon)."), nil
	}
	go func() {
		time.Sleep(500 * time.Millisecond)
		s.restartFn()
	}()
	return mcpsdk.NewToolResultText("Restarting daemon… active tmux sessions will be preserved."), nil
}

func (s *Server) handleGetVersion(_ context.Context, _ mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	var sb strings.Builder
	current := s.version
	if current == "" {
		current = "(unknown)"
	}
	sb.WriteString(fmt.Sprintf("Current version: %s\n", current))

	if s.latestVersion != nil {
		latest, err := s.latestVersion()
		if err != nil {
			sb.WriteString(fmt.Sprintf("Latest version:  (check failed: %v)\n", err))
		} else if latest != "" {
			if latest == current {
				sb.WriteString(fmt.Sprintf("Latest version:  %s (up to date)\n", latest))
			} else {
				sb.WriteString(fmt.Sprintf("Latest version:  %s  ← UPDATE AVAILABLE\n", latest))
				sb.WriteString("Use `datawatch update` or POST /api/update to install.\n")
			}
		}
	}
	return mcpsdk.NewToolResultText(sb.String()), nil
}

func (s *Server) handleListSavedCommands(_ context.Context, _ mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	if s.cmdLib == nil {
		return mcpsdk.NewToolResultText("Command library not available."), nil
	}
	cmds := s.cmdLib.List()
	if len(cmds) == 0 {
		return mcpsdk.NewToolResultText("No saved commands. Run `datawatch seed` to populate defaults."), nil
	}
	var sb strings.Builder
	for _, c := range cmds {
		seeded := ""
		if c.Seeded {
			seeded = " (seeded)"
		}
		sb.WriteString(fmt.Sprintf("%-16s  %s%s\n", c.Name, c.Command, seeded))
	}
	return mcpsdk.NewToolResultText(sb.String()), nil
}

func (s *Server) handleSendSavedCommand(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	if s.cmdLib == nil {
		return mcpsdk.NewToolResultText("Command library not available."), nil
	}
	id := req.GetString("session_id", "")
	name := req.GetString("command_name", "")
	if id == "" || name == "" {
		return mcpsdk.NewToolResultText("Error: session_id and command_name are required"), nil
	}

	cmd, ok := s.cmdLib.Get(name)
	if !ok {
		return mcpsdk.NewToolResultText(fmt.Sprintf("Saved command %q not found.", name)), nil
	}

	sess, ok := s.manager.GetSession(id)
	if !ok {
		return mcpsdk.NewToolResultText(fmt.Sprintf("Session %q not found.", id)), nil
	}

	if err := s.manager.SendInput(sess.FullID, cmd.Command, "mcp"); err != nil {
		return mcpsdk.NewToolResultText(fmt.Sprintf("Error sending command: %v", err)), nil
	}
	return mcpsdk.NewToolResultText(fmt.Sprintf("Sent command %q (%q) to session %s.", name, cmd.Command, sess.ID)), nil
}

func (s *Server) handleScheduleAdd(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	if s.schedStore == nil {
		return mcpsdk.NewToolResultText("Schedule store not available."), nil
	}
	id := req.GetString("session_id", "")
	command := req.GetString("command", "")
	if id == "" || command == "" {
		return mcpsdk.NewToolResultText("Error: session_id and command are required"), nil
	}

	sess, ok := s.manager.GetSession(id)
	if !ok {
		return mcpsdk.NewToolResultText(fmt.Sprintf("Session %q not found.", id)), nil
	}

	runAtStr := req.GetString("run_at", "prompt")
	var runAt time.Time
	if runAtStr != "" && runAtStr != "prompt" {
		// Try HH:MM
		if t, err := time.ParseInLocation("15:04", runAtStr, time.Local); err == nil {
			now := time.Now()
			runAt = time.Date(now.Year(), now.Month(), now.Day(), t.Hour(), t.Minute(), 0, 0, time.Local)
		} else if t, err := time.Parse(time.RFC3339, runAtStr); err == nil {
			runAt = t
		} else {
			return mcpsdk.NewToolResultText(fmt.Sprintf("Invalid run_at %q — use 'prompt', 'HH:MM', or RFC3339.", runAtStr)), nil
		}
	}

	sc, err := s.schedStore.Add(sess.FullID, command, runAt, "")
	if err != nil {
		return mcpsdk.NewToolResultText(fmt.Sprintf("Error scheduling: %v", err)), nil
	}
	runDesc := "on next input prompt"
	if !runAt.IsZero() {
		runDesc = "at " + runAt.Format("15:04:05")
	}
	return mcpsdk.NewToolResultText(fmt.Sprintf("Scheduled [%s]: %q → session %s %s.", sc.ID, command, sess.ID, runDesc)), nil
}

func (s *Server) handleScheduleList(_ context.Context, _ mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	if s.schedStore == nil {
		return mcpsdk.NewToolResultText("Schedule store not available."), nil
	}
	pending := s.schedStore.List("pending")
	if len(pending) == 0 {
		return mcpsdk.NewToolResultText("No pending scheduled commands."), nil
	}
	var sb strings.Builder
	for _, sc := range pending {
		runDesc := "on next input prompt"
		if !sc.RunAt.IsZero() {
			runDesc = "at " + sc.RunAt.Format("15:04:05")
		}
		sb.WriteString(fmt.Sprintf("[%s] session:%s %s — %q\n", sc.ID, sc.SessionID, runDesc, sc.Command))
	}
	return mcpsdk.NewToolResultText(sb.String()), nil
}

func (s *Server) handleScheduleCancel(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	if s.schedStore == nil {
		return mcpsdk.NewToolResultText("Schedule store not available."), nil
	}
	id := req.GetString("id", "")
	if id == "" {
		return mcpsdk.NewToolResultText("Error: id is required"), nil
	}
	if err := s.schedStore.Cancel(id); err != nil {
		return mcpsdk.NewToolResultText(fmt.Sprintf("Error: %v", err)), nil
	}
	return mcpsdk.NewToolResultText(fmt.Sprintf("Scheduled command [%s] cancelled.", id)), nil
}
