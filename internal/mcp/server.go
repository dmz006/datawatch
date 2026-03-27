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
//	list_sessions   — list all sessions on this host
//	start_session   — start a new AI session for a task
//	session_output  — get the last N lines of output from a session
//	send_input      — send text input to a session waiting for a response
//	kill_session    — terminate a session
package mcp

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/session"
	"github.com/dmz006/datawatch/internal/tlsutil"
)

// Server wraps the MCP server with session manager access.
type Server struct {
	hostname string
	manager  *session.Manager
	cfg      *config.MCPConfig
	dataDir  string
	srv      *server.MCPServer
}

// New creates a new MCP server backed by the given session manager.
func New(hostname string, manager *session.Manager, cfg *config.MCPConfig, dataDir string) *Server {
	s := &Server{
		hostname: hostname,
		manager:  manager,
		cfg:      cfg,
		dataDir:  dataDir,
	}

	mcpSrv := server.NewMCPServer(
		"datawatch",
		"1.0.0",
		server.WithToolCapabilities(true),
	)

	mcpSrv.AddTool(s.toolListSessions(), s.handleListSessions)
	mcpSrv.AddTool(s.toolStartSession(), s.handleStartSession)
	mcpSrv.AddTool(s.toolSessionOutput(), s.handleSessionOutput)
	mcpSrv.AddTool(s.toolSendInput(), s.handleSendInput)
	mcpSrv.AddTool(s.toolKillSession(), s.handleKillSession)

	s.srv = mcpSrv
	return s
}

// ServeStdio runs the MCP server over stdin/stdout (for local clients like Cursor).
// Blocks until ctx is cancelled or stdin closes.
func (s *Server) ServeStdio(ctx context.Context) error {
	return server.NewStdioServer(s.srv).Listen(ctx, nil, nil)
}

// ServeSSE starts an HTTP/SSE MCP server for remote AI clients.
// Blocks until ctx is cancelled.
func (s *Server) ServeSSE(ctx context.Context) error {
	addr := fmt.Sprintf("%s:%d", s.cfg.SSEHost, s.cfg.SSEPort)

	scheme := "http"
	if s.cfg.TLSEnabled {
		scheme = "https"
	}
	baseURL := fmt.Sprintf("%s://%s", scheme, addr)

	sseSrv := server.NewSSEServer(s.srv, server.WithBaseURL(baseURL))

	var handler http.Handler = sseSrv
	if s.cfg.Token != "" {
		handler = bearerAuthMiddleware(s.cfg.Token, sseSrv)
	}

	mux := http.NewServeMux()
	mux.Handle("/", handler)

	httpSrv := &http.Server{
		Addr:        addr,
		Handler:     mux,
		ReadTimeout: 30 * time.Second,
		IdleTimeout: 120 * time.Second,
	}

	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("MCP SSE listen %s: %w", addr, err)
	}

	errCh := make(chan error, 1)

	if s.cfg.TLSEnabled {
		tlsCfg, err := tlsutil.Build(tlsutil.Config{
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
		go func() { errCh <- httpSrv.ServeTLS(listener, "", "") }()
		fmt.Printf("datawatch MCP SSE server listening on https://%s (TLS 1.3+, post-quantum enabled)\n", addr)
	} else {
		go func() { errCh <- httpSrv.Serve(listener) }()
		fmt.Printf("datawatch MCP SSE server listening on http://%s\n", addr)
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

	if err := s.manager.SendInput(sess.FullID, text); err != nil {
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
