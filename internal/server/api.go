package server

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"syscall"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/dmz006/datawatch/internal/alerts"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/llm"
	"github.com/dmz006/datawatch/internal/llm/backends/ollama"
	"github.com/dmz006/datawatch/internal/llm/backends/openwebui"
	"github.com/dmz006/datawatch/internal/router"
	"github.com/dmz006/datawatch/internal/session"
)

// startTime records when the daemon started (for uptime calculation).
var startTime = time.Now()

// Version is set at build time. The server package uses this for /api/health and /api/info.
var Version = "0.6.35"

// Server holds all HTTP handler dependencies
type Server struct {
	hub               *Hub
	manager           *session.Manager
	hostname          string
	token             string
	availableBackends []string // registered LLM backend names
	cfg               *config.Config
	cfgPath           string
	schedStore        *session.ScheduleStore
	cmdLib            *session.CmdLibrary
	alertStore        *alerts.Store
	filterStore       *session.FilterStore

	linkMu      sync.Mutex
	linkStreams  map[string]chan string // stream_id -> event channel

	// Backend version cache — avoids slow serial exec calls on every /api/backends request.
	versionCacheMu sync.RWMutex
	versionCache   interface{} // []backendInfo
	versionCacheAt time.Time

	// restartFn is wired from main.go; it restarts the daemon in-place.
	restartFn func()

	// mcpDocsFunc returns MCP tool documentation (wired from main.go when MCP is enabled).
	mcpDocsFunc func() interface{}

	// installUpdate is wired from main.go; it downloads and installs a new binary.
	// After a successful install, the caller is responsible for restarting.
	installUpdate func(version string) error
	// latestVersion returns the latest available release tag (without "v" prefix).
	latestVersion func() (string, error)
}

func NewServer(hub *Hub, manager *session.Manager, hostname, token string, backends []string, cfg *config.Config, cfgPath string) *Server {
	s := &Server{
		hub:               hub,
		manager:           manager,
		hostname:          hostname,
		token:             token,
		availableBackends: backends,
		cfg:               cfg,
		cfgPath:           cfgPath,
		linkStreams:        make(map[string]chan string),
	}
	// Pre-warm backend version cache in background so first /api/backends is instant.
	go s.warmVersionCache()
	return s
}

// llmEnabled returns whether a named LLM backend is enabled in the config.
func (s *Server) llmEnabled(name string) bool {
	if s.cfg == nil {
		return false
	}
	switch name {
	case "claude-code":
		return true // always enabled if registered
	case "aider":
		return s.cfg.Aider.Enabled
	case "goose":
		return s.cfg.Goose.Enabled
	case "gemini":
		return s.cfg.Gemini.Enabled
	case "ollama":
		return s.cfg.Ollama.Enabled
	case "opencode":
		return s.cfg.OpenCode.Enabled
	case "opencode-acp":
		return s.cfg.OpenCode.ACPEnabled
	case "opencode-prompt":
		return s.cfg.OpenCode.PromptEnabled
	case "openwebui":
		return s.cfg.OpenWebUI.Enabled
	case "shell":
		return s.cfg.Shell.Enabled
	}
	return false
}

func (s *Server) llmPromptRequired(name string) bool {
	b, err := llm.Get(name)
	if err != nil {
		return false
	}
	if pr, ok := b.(llm.PromptRequirer); ok {
		return pr.PromptRequired()
	}
	return false
}

func (s *Server) warmVersionCache() {
	type backendInfo struct {
		Name           string `json:"name"`
		Available      bool   `json:"available"`
		Enabled        bool   `json:"enabled"`
		PromptRequired bool   `json:"prompt_required,omitempty"`
		Version        string `json:"version,omitempty"`
	}
	backends := make([]backendInfo, len(s.availableBackends))
	var wg sync.WaitGroup
	for i, name := range s.availableBackends {
		i, name := i, name
		backends[i] = backendInfo{Name: name, Enabled: s.llmEnabled(name), PromptRequired: s.llmPromptRequired(name)}
		wg.Add(1)
		go func() {
			defer wg.Done()
			if b, err := llm.Get(name); err == nil {
				ver := b.Version()
				backends[i].Available = ver != ""
				backends[i].Version = ver
			}
		}()
	}
	wg.Wait()
	s.versionCacheMu.Lock()
	s.versionCache = backends
	s.versionCacheAt = time.Now()
	s.versionCacheMu.Unlock()
}

// SetScheduleStore wires a schedule store into the API server.
func (s *Server) SetScheduleStore(store *session.ScheduleStore) { s.schedStore = store }

// SetRestartFunc wires the daemon self-restart function.
func (s *Server) SetRestartFunc(fn func()) { s.restartFn = fn }

// handleOpenWebUIModels returns available models from the configured OpenWebUI instance.
func (s *Server) handleOpenWebUIModels(w http.ResponseWriter, r *http.Request) {
	url, apiKey := "", ""
	if s.cfg != nil {
		url = s.cfg.OpenWebUI.URL
		apiKey = s.cfg.OpenWebUI.APIKey
	}
	models, err := openwebui.ListModels(url, apiKey)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(models) //nolint:errcheck
}

// SetMCPDocsFunc wires a function that returns MCP tool documentation.
func (s *Server) SetMCPDocsFunc(fn func() interface{}) { s.mcpDocsFunc = fn }

// handleMCPDocs returns MCP tool documentation as JSON or HTML.
func (s *Server) handleMCPDocs(w http.ResponseWriter, r *http.Request) {
	if s.mcpDocsFunc == nil {
		http.Error(w, "MCP not enabled", http.StatusServiceUnavailable)
		return
	}
	docs := s.mcpDocsFunc()

	// If Accept header prefers HTML, return a rendered page
	accept := r.Header.Get("Accept")
	if strings.Contains(accept, "text/html") {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `<!DOCTYPE html><html><head><title>datawatch MCP Tools</title>
<style>body{font-family:system-ui;max-width:800px;margin:40px auto;padding:0 20px;background:#1a1d27;color:#e2e8f0}
h1{color:#a855f7}h2{color:#7c3aed;border-bottom:1px solid #2d3148;padding-bottom:4px}
.tool{margin:16px 0;padding:12px;background:#22263a;border-radius:8px}
.tool-name{font-weight:bold;color:#a855f7;font-size:16px}
.param{margin:4px 0 4px 16px;font-size:14px}
.required{color:#f59e0b;font-size:11px}
code{background:#2d3148;padding:2px 6px;border-radius:4px;font-size:13px}
</style></head><body><h1>datawatch MCP Tools</h1>
<p>%d tools available via MCP stdio and SSE transports.</p>`, 17)
		if toolDocs, ok := docs.([]interface{}); ok {
			for _, td := range toolDocs {
				if m, ok := td.(map[string]interface{}); ok {
					fmt.Fprintf(w, `<div class="tool"><div class="tool-name">%v</div><p>%v</p>`, m["name"], m["description"])
					if params, ok := m["parameters"].([]interface{}); ok {
						for _, p := range params {
							if pm, ok := p.(map[string]interface{}); ok {
								req := ""
								if r, ok := pm["required"].(bool); ok && r {
									req = ` <span class="required">required</span>`
								}
								fmt.Fprintf(w, `<div class="param"><code>%v</code> (%v)%s — %v</div>`, pm["name"], pm["type"], req, pm["description"])
							}
						}
					}
					fmt.Fprint(w, `</div>`)
				}
			}
		}
		fmt.Fprint(w, `</body></html>`)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(docs) //nolint:errcheck
}

// handleOllamaModels returns available ollama models from the configured host.
func (s *Server) handleOllamaModels(w http.ResponseWriter, r *http.Request) {
	host := ""
	if s.cfg != nil {
		host = s.cfg.Ollama.Host
	}
	models, err := ollama.ListModels(host)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(models) //nolint:errcheck
}

// SetUpdateFuncs wires update-related functions. installFn downloads and installs
// a given version string; latestFn returns the latest available version tag.
func (s *Server) SetUpdateFuncs(installFn func(string) error, latestFn func() (string, error)) {
	s.installUpdate = installFn
	s.latestVersion = latestFn
}

// authMiddleware checks the Bearer token if one is configured
func (s *Server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.token == "" {
			next.ServeHTTP(w, r)
			return
		}
		// Check Authorization header or ?token= query param
		tok := r.URL.Query().Get("token")
		if tok == "" {
			auth := r.Header.Get("Authorization")
			tok = strings.TrimPrefix(auth, "Bearer ")
		}
		if tok != s.token {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// handleSessions returns all sessions as JSON
func (s *Server) handleSessions(w http.ResponseWriter, r *http.Request) {
	sessions := s.manager.ListSessions()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(sessions) //nolint:errcheck
}

// handleSessionOutput returns the last N lines of a session's output
func (s *Server) handleSessionOutput(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	n := 50
	fmt.Sscanf(r.URL.Query().Get("n"), "%d", &n) //nolint:errcheck
	output, err := s.manager.TailOutput(id, n)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "text/plain")
	w.Write([]byte(output)) //nolint:errcheck
}

// handleSessionTimeline returns the structured timeline events for a session as JSON.
func (s *Server) handleSessionTimeline(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	if id == "" {
		http.Error(w, "missing id", http.StatusBadRequest)
		return
	}
	sess, ok := s.manager.GetSession(id)
	if !ok {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}
	lines, err := s.manager.ReadTimeline(sess.FullID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	type timelineResp struct {
		SessionID string   `json:"session_id"`
		Lines     []string `json:"lines"`
	}
	json.NewEncoder(w).Encode(timelineResp{SessionID: sess.FullID, Lines: lines}) //nolint:errcheck
}

// handleCommand processes a command string (same format as Signal commands)
func (s *Server) handleCommand(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Text string `json:"text"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	cmd := router.Parse(req.Text)
	result := s.executeCommand(cmd, req.Text)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"result": result}) //nolint:errcheck
}

// executeCommand runs a parsed command and returns a response string
func (s *Server) executeCommand(cmd router.Command, raw string) string {
	// Handle sendkey command: sends a raw tmux key name without appending Enter.
	// Format: "sendkey <session_id>: <KeyName>" (e.g. "sendkey abc123: Up")
	if strings.HasPrefix(raw, "sendkey ") {
		parts := strings.SplitN(raw[8:], ":", 2)
		if len(parts) == 2 {
			sessID := strings.TrimSpace(parts[0])
			keyName := strings.TrimSpace(parts[1])
			sess, ok := s.manager.GetSession(sessID)
			if !ok {
				return fmt.Sprintf("Session %s not found", sessID)
			}
			if err := exec.Command("tmux", "send-keys", "-t", sess.TmuxSession, keyName).Run(); err != nil {
				return fmt.Sprintf("Error: %v", err)
			}
			return fmt.Sprintf("[%s] Key sent: %s", sessID, keyName)
		}
	}

	switch cmd.Type {
	case router.CmdNew:
		if cmd.Text == "" {
			return "Usage: new: <task>"
		}
		sess, err := s.manager.Start(context.Background(), cmd.Text, "", cmd.ProjectDir)
		if err != nil {
			return fmt.Sprintf("Error: %v", err)
		}
		// Broadcast updated session list
		go s.hub.BroadcastSessions(s.manager.ListSessions())
		return fmt.Sprintf("[%s][%s] Started: %s\nTmux: %s", s.hostname, sess.ID, cmd.Text, sess.TmuxSession)

	case router.CmdList:
		sessions := s.manager.ListSessions()
		if len(sessions) == 0 {
			return "No active sessions."
		}
		var sb strings.Builder
		for _, sess := range sessions {
			sb.WriteString(fmt.Sprintf("[%s] %s — %s\n  %s\n", sess.ID, sess.State, sess.UpdatedAt.Format("15:04:05"), truncate(sess.Task, 60)))
		}
		return sb.String()

	case router.CmdStatus:
		output, err := s.manager.TailOutput(cmd.SessionID, 20)
		if err != nil {
			return fmt.Sprintf("Error: %v", err)
		}
		return output

	case router.CmdSend:
		err := s.manager.SendInput(cmd.SessionID, cmd.Text, "web")
		if err != nil {
			return fmt.Sprintf("Error: %v", err)
		}
		go s.hub.BroadcastSessions(s.manager.ListSessions())
		return fmt.Sprintf("[%s] Input sent.", cmd.SessionID)

	case router.CmdKill:
		err := s.manager.Kill(cmd.SessionID)
		if err != nil {
			return fmt.Sprintf("Error: %v", err)
		}
		go s.hub.BroadcastSessions(s.manager.ListSessions())
		return fmt.Sprintf("[%s] Killed.", cmd.SessionID)

	case router.CmdTail:
		n := cmd.TailN
		if n == 0 {
			n = 20
		}
		output, err := s.manager.TailOutput(cmd.SessionID, n)
		if err != nil {
			return fmt.Sprintf("Error: %v", err)
		}
		return output

	case router.CmdAttach:
		sess, ok := s.manager.GetSession(cmd.SessionID)
		if !ok {
			return "Session not found."
		}
		return fmt.Sprintf("tmux attach -t %s", sess.TmuxSession)

	case router.CmdHelp:
		return router.HelpText(s.hostname)

	default:
		_ = raw // suppress unused variable warning
		return "Unknown command. Send 'help' for available commands."
	}
}

// handleWS upgrades a connection to WebSocket and registers it with the hub
func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}

	c := &client{
		hub:        s.hub,
		conn:       conn,
		send:       make(chan []byte, 256),
		subscribed: make(map[string]bool),
	}
	s.hub.register <- c

	// Send initial session list
	sessions := s.manager.ListSessions()
	raw, _ := json.Marshal(SessionsData{Sessions: sessions})
	msg := WSMessage{Type: MsgSessions, Data: raw, Timestamp: time.Now()}
	payload, _ := json.Marshal(msg)
	c.send <- payload

	go c.writePump()

	// Read pump (blocking)
	defer func() {
		s.hub.unregister <- c
		conn.Close()
	}()

	conn.SetReadLimit(32 * 1024)
	conn.SetReadDeadline(time.Now().Add(60 * time.Second)) //nolint:errcheck
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(60 * time.Second)) //nolint:errcheck
		return nil
	})

	for {
		_, msgBytes, err := conn.ReadMessage()
		if err != nil {
			break
		}
		conn.SetReadDeadline(time.Now().Add(60 * time.Second)) //nolint:errcheck

		var inMsg WSMessage
		if err := json.Unmarshal(msgBytes, &inMsg); err != nil {
			continue
		}

		switch inMsg.Type {
		case MsgCommand:
			var d CommandData
			json.Unmarshal(inMsg.Data, &d) //nolint:errcheck
			cmd := router.Parse(d.Text)
			result := s.executeCommand(cmd, d.Text)
			// Send result back to this client
			respRaw, _ := json.Marshal(NotificationData{Message: result})
			resp := WSMessage{Type: MsgNotification, Data: respRaw, Timestamp: time.Now()}
			respPayload, _ := json.Marshal(resp)
			c.send <- respPayload

		case MsgNewSession:
			var d NewSessionData
			json.Unmarshal(inMsg.Data, &d) //nolint:errcheck
			opts := &session.StartOptions{
				Name:     d.Name,
				Backend:  d.Backend,
				ResumeID: d.ResumeID,
			}
			if d.ProjectDir == "" {
					d.ProjectDir, _ = os.UserHomeDir()
				}
				sess, err := s.manager.Start(context.Background(), d.Task, "", d.ProjectDir, opts)
			var result string
			if err != nil {
				result = fmt.Sprintf("Error: %v", err)
			} else {
				result = fmt.Sprintf("[%s][%s] Started: %s\nTmux: %s", s.hostname, sess.ID, d.Task, sess.TmuxSession)
				s.hub.BroadcastSessions(s.manager.ListSessions())
			}
			respRaw, _ := json.Marshal(NotificationData{Message: result})
			resp := WSMessage{Type: MsgNotification, Data: respRaw, Timestamp: time.Now()}
			respPayload, _ := json.Marshal(resp)
			c.send <- respPayload

		case MsgSendInput:
			var d SendInputData
			json.Unmarshal(inMsg.Data, &d) //nolint:errcheck
			cmd := router.Command{Type: router.CmdSend, SessionID: d.SessionID, Text: d.Text}
			result := s.executeCommand(cmd, "")
			respRaw, _ := json.Marshal(NotificationData{Message: result})
			resp := WSMessage{Type: MsgNotification, Data: respRaw, Timestamp: time.Now()}
			respPayload, _ := json.Marshal(resp)
			c.send <- respPayload

		case MsgSubscribe:
			var d SubscribeData
			json.Unmarshal(inMsg.Data, &d) //nolint:errcheck
			c.mu.Lock()
			c.subscribed[d.SessionID] = true
			c.mu.Unlock()
			// Send recent output immediately
			output, err := s.manager.TailOutput(d.SessionID, 50)
			if err == nil {
				lines := strings.Split(output, "\n")
				outRaw, _ := json.Marshal(OutputData{SessionID: d.SessionID, Lines: lines})
				outMsg := WSMessage{Type: MsgOutput, Data: outRaw, Timestamp: time.Now()}
				outPayload, _ := json.Marshal(outMsg)
				c.send <- outPayload
			}

		case MsgPing:
			pongRaw, _ := json.Marshal(map[string]string{"type": "pong"})
			c.send <- pongRaw
		}
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// handleHealth returns daemon health status. No authentication required.
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	uptime := int(time.Since(startTime).Seconds())
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
		"status":         "ok",
		"hostname":       s.hostname,
		"version":        Version,
		"uptime_seconds": uptime,
	})
}

// handleInfo returns system information.
func (s *Server) handleInfo(w http.ResponseWriter, r *http.Request) {
	sessions := s.manager.ListSessions()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
		"hostname":           s.hostname,
		"version":            Version,
		"llm_backend":        s.manager.ActiveBackend(),
		"available_backends": s.availableBackends,
		"session_count":      len(sessions),
	})
}

// handleBackends returns available LLM backends with availability status.
// Version checks are cached and refreshed in the background every 60 seconds
// to avoid slow serial exec calls on every request.
func (s *Server) handleBackends(w http.ResponseWriter, r *http.Request) {
	type backendInfo struct {
		Name           string `json:"name"`
		Available      bool   `json:"available"`
		Enabled        bool   `json:"enabled"`
		PromptRequired bool   `json:"prompt_required,omitempty"`
		Version        string `json:"version,omitempty"`
	}

	s.versionCacheMu.RLock()
	cached := s.versionCache
	cacheAge := time.Since(s.versionCacheAt)
	s.versionCacheMu.RUnlock()

	// Serve from cache if fresh (< 60s)
	if cached != nil && cacheAge < 60*time.Second {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
			"llm":    cached,
			"active": s.manager.ActiveBackend(),
		})
		return
	}

	// Build fresh cache — run version checks in parallel
	backends := make([]backendInfo, len(s.availableBackends))
	var wg sync.WaitGroup
	for i, name := range s.availableBackends {
		i, name := i, name
		backends[i] = backendInfo{Name: name, Enabled: s.llmEnabled(name), PromptRequired: s.llmPromptRequired(name)}
		wg.Add(1)
		go func() {
			defer wg.Done()
			if b, err := llm.Get(name); err == nil {
				ver := b.Version()
				backends[i].Available = ver != ""
				backends[i].Version = ver
			}
		}()
	}
	wg.Wait()

	s.versionCacheMu.Lock()
	s.versionCache = backends
	s.versionCacheAt = time.Now()
	s.versionCacheMu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
		"llm":    backends,
		"active": s.manager.ActiveBackend(),
	})
}

// handleFiles returns directory contents for path browsing.
func (s *Server) handleFiles(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	if path == "" {
		home, _ := os.UserHomeDir()
		path = home
	}
	// Expand ~ if present
	if len(path) > 0 && path[0] == '~' {
		home, _ := os.UserHomeDir()
		path = filepath.Join(home, path[1:])
	}

	// Enforce root path restriction
	rootPath := ""
	if s.cfg != nil && s.cfg.Session.RootPath != "" {
		rootPath = s.cfg.Session.RootPath
		if len(rootPath) > 0 && rootPath[0] == '~' {
			home, _ := os.UserHomeDir()
			rootPath = filepath.Join(home, rootPath[1:])
		}
		// Clean both paths and ensure requested path is within root
		cleanRoot := filepath.Clean(rootPath)
		cleanPath := filepath.Clean(path)
		if !strings.HasPrefix(cleanPath+string(filepath.Separator), cleanRoot+string(filepath.Separator)) &&
			cleanPath != cleanRoot {
			// Clamp to root path silently
			path = cleanRoot
		}
	}

	entries, err := os.ReadDir(path)
	if err != nil {
		http.Error(w, fmt.Sprintf("cannot read dir: %v", err), http.StatusBadRequest)
		return
	}

	type Entry struct {
		Name    string `json:"name"`
		IsDir   bool   `json:"is_dir"`
		Path    string `json:"path"`
		IsLink  bool   `json:"is_link,omitempty"`
	}
	result := []Entry{}
	// Add parent directory entry (omit if at root path boundary)
	parent := filepath.Dir(path)
	atRoot := rootPath != "" && filepath.Clean(path) == filepath.Clean(rootPath)
	if parent != path && !atRoot {
		result = append(result, Entry{Name: "..", IsDir: true, Path: parent})
	}
	for _, e := range entries {
		if e.Name()[0] == '.' {
			continue // skip hidden files
		}
		entryPath := filepath.Join(path, e.Name())
		isDir := e.IsDir()
		isLink := e.Type()&os.ModeSymlink != 0
		if isLink {
			// Follow symlink to determine if it points to a directory
			if fi, err := os.Stat(entryPath); err == nil {
				isDir = fi.IsDir()
			}
		}
		result = append(result, Entry{
			Name:   e.Name(),
			IsDir:  isDir,
			Path:   entryPath,
			IsLink: isLink,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
		"path":    path,
		"entries": result,
	})
}

// handleRenameSession renames a session.
func (s *Server) handleRenameSession(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if err := s.manager.Rename(req.ID, req.Name); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	go s.hub.BroadcastSessions(s.manager.ListSessions())
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"}) //nolint:errcheck
}

// handleKillSession terminates a running or waiting session.
func (s *Server) handleKillSession(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if err := s.manager.Kill(req.ID); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	go s.hub.BroadcastSessions(s.manager.ListSessions())
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"}) //nolint:errcheck
}

// handleDeleteSession removes a session and optionally its tracking data.
func (s *Server) handleDeleteSession(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		ID         string `json:"id"`
		DeleteData bool   `json:"delete_data"` // also remove tracking dir from disk
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if err := s.manager.Delete(req.ID, req.DeleteData); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	go s.hub.BroadcastSessions(s.manager.ListSessions())
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"}) //nolint:errcheck
}

// handleStartSession starts a new session with optional backend and name overrides.
func (s *Server) handleStartSession(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Task          string `json:"task"`
		ProjectDir    string `json:"project_dir"`
		Backend       string `json:"backend"`
		Name          string `json:"name"`
		ResumeID      string `json:"resume_id"`
		AutoGitCommit *bool  `json:"auto_git_commit,omitempty"`
		AutoGitInit   *bool  `json:"auto_git_init,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	// Default project dir to home directory when not specified
	if req.ProjectDir == "" {
		req.ProjectDir, _ = os.UserHomeDir()
	}

	opts := &session.StartOptions{
		Name:          req.Name,
		Backend:       req.Backend,
		ResumeID:      req.ResumeID,
		AutoGitCommit: req.AutoGitCommit,
		AutoGitInit:   req.AutoGitInit,
	}
	sess, err := s.manager.Start(context.Background(), req.Task, "", req.ProjectDir, opts)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	go s.hub.BroadcastSessions(s.manager.ListSessions())
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(sess) //nolint:errcheck
}

// generateStreamID returns a random hex string suitable for a stream ID.
func generateStreamID() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// handleLinkStart initiates signal-cli device linking and returns a stream ID for SSE.
func (s *Server) handleLinkStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		DeviceName string `json:"device_name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		req.DeviceName = s.hostname
	}
	if req.DeviceName == "" {
		req.DeviceName = s.hostname
	}

	streamID, err := generateStreamID()
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	ch := make(chan string, 4)

	s.linkMu.Lock()
	s.linkStreams[streamID] = ch
	s.linkMu.Unlock()

	// Run signal-cli link in a goroutine, sending events to the channel.
	go func() {
		defer func() {
			// Clean up the stream after a delay so the SSE handler can read the last event.
			time.Sleep(30 * time.Second)
			s.linkMu.Lock()
			delete(s.linkStreams, streamID)
			s.linkMu.Unlock()
		}()

		cmd := exec.Command("signal-cli", "link", "-n", req.DeviceName)
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			ch <- "event: error\ndata: failed to create stdout pipe\n\n"
			return
		}
		stderr, err := cmd.StderrPipe()
		if err != nil {
			ch <- "event: error\ndata: failed to create stderr pipe\n\n"
			return
		}

		if err := cmd.Start(); err != nil {
			ch <- fmt.Sprintf("event: error\ndata: failed to start signal-cli: %s\n\n", err.Error())
			return
		}

		// Read from both stdout and stderr looking for sgnl:// URI
		qrFound := false
		scanFn := func(stream interface{ Scan() bool; Text() string }) {
			for stream.Scan() {
				line := stream.Text()
				if strings.HasPrefix(line, "sgnl://") && !qrFound {
					qrFound = true
					ch <- fmt.Sprintf("event: qr\ndata: %s\n\n", line)
				}
			}
		}

		// Scan stdout and stderr concurrently
		go scanFn(bufio.NewScanner(stdout))
		scanFn(bufio.NewScanner(stderr))

		if err := cmd.Wait(); err != nil {
			ch <- fmt.Sprintf("event: error\ndata: signal-cli exited: %s\n\n", err.Error())
			return
		}
		ch <- "event: linked\ndata: success\n\n"
	}()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"stream_id": streamID}) //nolint:errcheck
}

// handleLinkStream sends Server-Sent Events for the linking process.
func (s *Server) handleLinkStream(w http.ResponseWriter, r *http.Request) {
	streamID := r.URL.Query().Get("id")
	if streamID == "" {
		http.Error(w, "id parameter required", http.StatusBadRequest)
		return
	}

	s.linkMu.Lock()
	ch, ok := s.linkStreams[streamID]
	s.linkMu.Unlock()

	if !ok {
		http.Error(w, "stream not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	flusher, canFlush := w.(http.Flusher)

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case event, open := <-ch:
			if !open {
				return
			}
			fmt.Fprint(w, event) //nolint:errcheck
			if canFlush {
				flusher.Flush()
			}
			// If linked or error, stop streaming
			if strings.HasPrefix(event, "event: linked") || strings.HasPrefix(event, "event: error") {
				return
			}
		case <-time.After(25 * time.Second):
			// Keepalive comment
			fmt.Fprint(w, ": keepalive\n\n") //nolint:errcheck
			if canFlush {
				flusher.Flush()
			}
		}
	}
}

// handleLinkStatus returns the current Signal linking status.
func (s *Server) handleLinkStatus(w http.ResponseWriter, r *http.Request) {
	// We determine link status by checking if signal-cli can list groups (it needs a linked account).
	// A simpler heuristic: check if the signal-cli config directory has an account file.
	// For now, we return a basic response indicating the daemon is running.
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
		"linked":         true,
		"account_number": "",
		"device_name":    s.hostname,
	})
}

// handleConfig dispatches GET (read config) and PUT (update config) requests.
func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleGetConfig(w, r)
	case http.MethodPut:
		s.handlePutConfig(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleGetConfig returns a sanitized view of the current config (sensitive fields masked).
func (s *Server) handleGetConfig(w http.ResponseWriter, _ *http.Request) {
	if s.cfg == nil {
		http.Error(w, "config not available", http.StatusServiceUnavailable)
		return
	}
	mask := func(v string) string {
		if v == "" {
			return ""
		}
		return "***"
	}
	out := map[string]interface{}{
		"hostname": s.cfg.Hostname,
		"server": map[string]interface{}{
			"enabled": s.cfg.Server.Enabled,
			"host":    s.cfg.Server.Host,
			"port":    s.cfg.Server.Port,
			"tls":     s.cfg.Server.TLSEnabled,
		},
		"signal": map[string]interface{}{
			"enabled":        s.cfg.Signal.AccountNumber != "",
			"account_number": s.cfg.Signal.AccountNumber,
			"group_id":       s.cfg.Signal.GroupID,
		},
		"telegram": map[string]interface{}{
			"enabled": s.cfg.Telegram.Enabled,
			"token":   mask(s.cfg.Telegram.Token),
			"chat_id": s.cfg.Telegram.ChatID,
		},
		"discord": map[string]interface{}{
			"enabled":    s.cfg.Discord.Enabled,
			"token":      mask(s.cfg.Discord.Token),
			"channel_id": s.cfg.Discord.ChannelID,
		},
		"slack": map[string]interface{}{
			"enabled":    s.cfg.Slack.Enabled,
			"token":      mask(s.cfg.Slack.Token),
			"channel_id": s.cfg.Slack.ChannelID,
		},
		"matrix": map[string]interface{}{
			"enabled":      s.cfg.Matrix.Enabled,
			"homeserver":   s.cfg.Matrix.Homeserver,
			"user_id":      s.cfg.Matrix.UserID,
			"access_token": mask(s.cfg.Matrix.AccessToken),
			"room_id":      s.cfg.Matrix.RoomID,
		},
		"ntfy": map[string]interface{}{
			"enabled":    s.cfg.Ntfy.Enabled,
			"server_url": s.cfg.Ntfy.ServerURL,
			"topic":      s.cfg.Ntfy.Topic,
			"token":      mask(s.cfg.Ntfy.Token),
		},
		"email": map[string]interface{}{
			"enabled":  s.cfg.Email.Enabled,
			"host":     s.cfg.Email.Host,
			"port":     s.cfg.Email.Port,
			"username": s.cfg.Email.Username,
			"password": mask(s.cfg.Email.Password),
			"from":     s.cfg.Email.From,
			"to":       s.cfg.Email.To,
		},
		"twilio": map[string]interface{}{
			"enabled":       s.cfg.Twilio.Enabled,
			"account_sid":   mask(s.cfg.Twilio.AccountSID),
			"auth_token":    mask(s.cfg.Twilio.AuthToken),
			"from_number":   s.cfg.Twilio.FromNumber,
			"to_number":     s.cfg.Twilio.ToNumber,
			"webhook_addr":  s.cfg.Twilio.WebhookAddr,
		},
		"github_webhook": map[string]interface{}{
			"enabled": s.cfg.GitHubWebhook.Enabled,
			"addr":    s.cfg.GitHubWebhook.Addr,
			"secret":  mask(s.cfg.GitHubWebhook.Secret),
		},
		"webhook": map[string]interface{}{
			"enabled": s.cfg.Webhook.Enabled,
			"addr":    s.cfg.Webhook.Addr,
			"token":   mask(s.cfg.Webhook.Token),
		},
		"session": map[string]interface{}{
			"llm_backend":        s.cfg.Session.LLMBackend,
			"max_sessions":       s.cfg.Session.MaxSessions,
			"input_idle_timeout": s.cfg.Session.InputIdleTimeout,
			"tail_lines":         s.cfg.Session.TailLines,
			"default_project_dir": s.cfg.Session.DefaultProjectDir,
			"skip_permissions":   s.cfg.Session.ClaudeSkipPermissions,
			"channel_enabled":    s.cfg.Session.ClaudeChannelEnabled,
			"auto_git_commit":    s.cfg.Session.AutoGitCommit,
			"auto_git_init":      s.cfg.Session.AutoGitInit,
			"kill_sessions_on_exit": s.cfg.Session.KillSessionsOnExit,
			"root_path":         s.cfg.Session.RootPath,
			"mcp_max_retries":   s.cfg.Session.MCPMaxRetries,
		},
		"mcp": map[string]interface{}{
			"enabled":  s.cfg.MCP.Enabled,
			"sse_host": s.cfg.MCP.SSEHost,
			"sse_port": s.cfg.MCP.SSEPort,
		},
		"update": map[string]interface{}{
			"enabled":     s.cfg.Update.Enabled,
			"schedule":    s.cfg.Update.Schedule,
			"time_of_day": s.cfg.Update.TimeOfDay,
		},
		"ollama": map[string]interface{}{
			"enabled": s.cfg.Ollama.Enabled,
			"model":   s.cfg.Ollama.Model,
			"host":    s.cfg.Ollama.Host,
		},
		"opencode": map[string]interface{}{
			"enabled": s.cfg.OpenCode.Enabled,
			"binary":  s.cfg.OpenCode.Binary,
		},
		"aider": map[string]interface{}{
			"enabled": s.cfg.Aider.Enabled,
			"binary":  s.cfg.Aider.Binary,
		},
		"goose": map[string]interface{}{
			"enabled": s.cfg.Goose.Enabled,
			"binary":  s.cfg.Goose.Binary,
		},
		"gemini": map[string]interface{}{
			"enabled": s.cfg.Gemini.Enabled,
			"binary":  s.cfg.Gemini.Binary,
		},
		"openwebui": map[string]interface{}{
			"enabled": s.cfg.OpenWebUI.Enabled,
			"url":     s.cfg.OpenWebUI.URL,
			"model":   s.cfg.OpenWebUI.Model,
			"api_key": mask(s.cfg.OpenWebUI.APIKey),
		},
		"shell_backend": map[string]interface{}{
			"enabled":     s.cfg.Shell.Enabled,
			"script_path": s.cfg.Shell.ScriptPath,
		},
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(out) //nolint:errcheck
}

// handlePutConfig applies a partial config patch using dot-path keys and saves.
func (s *Server) handlePutConfig(w http.ResponseWriter, r *http.Request) {
	if s.cfg == nil || s.cfgPath == "" {
		http.Error(w, "config not available", http.StatusServiceUnavailable)
		return
	}
	var patch map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
		http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
		return
	}
	applyConfigPatch(s.cfg, patch)
	if err := config.Save(s.cfg, s.cfgPath); err != nil {
		http.Error(w, "save failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	// Invalidate backend version cache so next /api/backends reflects changes.
	s.versionCacheMu.Lock()
	s.versionCacheAt = time.Time{}
	s.versionCacheMu.Unlock()
	go s.warmVersionCache()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"}) //nolint:errcheck
}

// applyConfigPatch applies dot-path key/value pairs from patch to cfg.
// Only known, non-sensitive fields are applied; credential fields are ignored.
func applyConfigPatch(cfg *config.Config, patch map[string]interface{}) {
	for k, v := range patch {
		switch k {
		case "telegram.enabled":
			cfg.Telegram.Enabled = toBool(v)
		case "discord.enabled":
			cfg.Discord.Enabled = toBool(v)
		case "slack.enabled":
			cfg.Slack.Enabled = toBool(v)
		case "matrix.enabled":
			cfg.Matrix.Enabled = toBool(v)
		case "ntfy.enabled":
			cfg.Ntfy.Enabled = toBool(v)
		case "ntfy.server_url":
			if s := toString(v); s != "" { cfg.Ntfy.ServerURL = s }
		case "ntfy.topic":
			cfg.Ntfy.Topic = toString(v)
		case "ntfy.token":
			if s := toString(v); s != "" { cfg.Ntfy.Token = s }
		case "email.enabled":
			cfg.Email.Enabled = toBool(v)
		case "email.host":
			if s := toString(v); s != "" { cfg.Email.Host = s }
		case "email.port":
			if n, ok := toInt(v); ok { cfg.Email.Port = n }
		case "email.username":
			cfg.Email.Username = toString(v)
		case "email.password":
			if s := toString(v); s != "" { cfg.Email.Password = s }
		case "email.from":
			cfg.Email.From = toString(v)
		case "email.to":
			cfg.Email.To = toString(v)
		case "twilio.enabled":
			cfg.Twilio.Enabled = toBool(v)
		case "twilio.account_sid":
			if s := toString(v); s != "" { cfg.Twilio.AccountSID = s }
		case "twilio.auth_token":
			if s := toString(v); s != "" { cfg.Twilio.AuthToken = s }
		case "twilio.from_number":
			cfg.Twilio.FromNumber = toString(v)
		case "twilio.to_number":
			cfg.Twilio.ToNumber = toString(v)
		case "twilio.webhook_addr":
			if s := toString(v); s != "" { cfg.Twilio.WebhookAddr = s }
		case "github_webhook.enabled":
			cfg.GitHubWebhook.Enabled = toBool(v)
		case "github_webhook.addr":
			if s := toString(v); s != "" { cfg.GitHubWebhook.Addr = s }
		case "github_webhook.secret":
			if s := toString(v); s != "" { cfg.GitHubWebhook.Secret = s }
		case "webhook.enabled":
			cfg.Webhook.Enabled = toBool(v)
		case "webhook.addr":
			if s := toString(v); s != "" { cfg.Webhook.Addr = s }
		case "webhook.token":
			if s := toString(v); s != "" { cfg.Webhook.Token = s }
		case "telegram.token":
			if s := toString(v); s != "" { cfg.Telegram.Token = s }
		case "telegram.chat_id":
			if n, ok := toInt(v); ok { cfg.Telegram.ChatID = int64(n) }
		case "discord.token":
			if s := toString(v); s != "" { cfg.Discord.Token = s }
		case "discord.channel_id":
			cfg.Discord.ChannelID = toString(v)
		case "slack.token":
			if s := toString(v); s != "" { cfg.Slack.Token = s }
		case "slack.channel_id":
			cfg.Slack.ChannelID = toString(v)
		case "matrix.homeserver":
			if s := toString(v); s != "" { cfg.Matrix.Homeserver = s }
		case "matrix.user_id":
			cfg.Matrix.UserID = toString(v)
		case "matrix.access_token":
			if s := toString(v); s != "" { cfg.Matrix.AccessToken = s }
		case "matrix.room_id":
			cfg.Matrix.RoomID = toString(v)
		case "server.enabled":
			cfg.Server.Enabled = toBool(v)
		case "session.llm_backend":
			if s := toString(v); s != "" {
				cfg.Session.LLMBackend = s
			}
		case "session.skip_permissions":
			cfg.Session.ClaudeSkipPermissions = toBool(v)
		case "session.auto_git_commit":
			cfg.Session.AutoGitCommit = toBool(v)
		case "session.max_sessions":
			if n, ok := toInt(v); ok {
				cfg.Session.MaxSessions = n
			}
		case "session.input_idle_timeout":
			if n, ok := toInt(v); ok {
				cfg.Session.InputIdleTimeout = n
			}
		case "session.tail_lines":
			if n, ok := toInt(v); ok {
				cfg.Session.TailLines = n
			}
		case "session.default_project_dir":
			if s := toString(v); s != "" {
				cfg.Session.DefaultProjectDir = s
			}
		case "session.channel_enabled":
			cfg.Session.ClaudeChannelEnabled = toBool(v)
		case "session.auto_git_init":
			cfg.Session.AutoGitInit = toBool(v)
		case "session.kill_sessions_on_exit":
			cfg.Session.KillSessionsOnExit = toBool(v)
		case "session.root_path":
			cfg.Session.RootPath = toString(v)
		case "session.mcp_max_retries":
			if n, ok := toInt(v); ok {
				cfg.Session.MCPMaxRetries = n
			}
		case "server.host":
			if s := toString(v); s != "" {
				cfg.Server.Host = s
			}
		case "server.port":
			if n, ok := toInt(v); ok {
				cfg.Server.Port = n
			}
		case "server.tls":
			cfg.Server.TLSEnabled = toBool(v)
		case "mcp.enabled":
			cfg.MCP.Enabled = toBool(v)
		case "mcp.sse_host":
			if s := toString(v); s != "" {
				cfg.MCP.SSEHost = s
			}
		case "mcp.sse_port":
			if n, ok := toInt(v); ok {
				cfg.MCP.SSEPort = n
			}
		case "update.enabled":
			cfg.Update.Enabled = toBool(v)
		case "update.schedule":
			if s := toString(v); s != "" {
				cfg.Update.Schedule = s
			}
		case "update.time_of_day":
			if s := toString(v); s != "" {
				cfg.Update.TimeOfDay = s
			}
		// LLM backend config
		case "aider.enabled":
			cfg.Aider.Enabled = toBool(v)
		case "aider.binary":
			if s := toString(v); s != "" { cfg.Aider.Binary = s }
		case "goose.enabled":
			cfg.Goose.Enabled = toBool(v)
		case "goose.binary":
			if s := toString(v); s != "" { cfg.Goose.Binary = s }
		case "gemini.enabled":
			cfg.Gemini.Enabled = toBool(v)
		case "gemini.binary":
			if s := toString(v); s != "" { cfg.Gemini.Binary = s }
		case "ollama.enabled":
			cfg.Ollama.Enabled = toBool(v)
		case "ollama.model":
			if s := toString(v); s != "" { cfg.Ollama.Model = s }
		case "ollama.host":
			if s := toString(v); s != "" { cfg.Ollama.Host = s }
		case "opencode.enabled":
			cfg.OpenCode.Enabled = toBool(v)
		case "opencode-acp.enabled":
			cfg.OpenCode.ACPEnabled = toBool(v)
		case "opencode-prompt.enabled":
			cfg.OpenCode.PromptEnabled = toBool(v)
		case "opencode.binary":
			if s := toString(v); s != "" { cfg.OpenCode.Binary = s }
		case "openwebui.enabled":
			cfg.OpenWebUI.Enabled = toBool(v)
		case "openwebui.url":
			if s := toString(v); s != "" { cfg.OpenWebUI.URL = s }
		case "openwebui.model":
			if s := toString(v); s != "" { cfg.OpenWebUI.Model = s }
		case "openwebui.api_key":
			if s := toString(v); s != "" { cfg.OpenWebUI.APIKey = s }
		case "shell_backend.enabled", "shell.enabled":
			cfg.Shell.Enabled = toBool(v)
		case "shell_backend.script_path":
			cfg.Shell.ScriptPath = toString(v)
		}
	}
}

func toBool(v interface{}) bool {
	switch x := v.(type) {
	case bool:
		return x
	case string:
		return x == "true" || x == "yes" || x == "1"
	case float64:
		return x != 0
	}
	return false
}

func toString(v interface{}) string {
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", v)
}

func toInt(v interface{}) (int, bool) {
	switch x := v.(type) {
	case float64:
		return int(x), true
	case int:
		return x, true
	}
	return 0, false
}

// ---- Proxy endpoint --------------------------------------------------------

// handleProxy forwards requests to a named remote datawatch server.
// Route: /api/proxy/{serverName}/{...path}
func (s *Server) handleProxy(w http.ResponseWriter, r *http.Request) {
	// Extract serverName from path: /api/proxy/<name>/...
	path := strings.TrimPrefix(r.URL.Path, "/api/proxy/")
	idx := strings.Index(path, "/")
	var serverName, remotePath string
	if idx < 0 {
		serverName = path
		remotePath = "/"
	} else {
		serverName = path[:idx]
		remotePath = path[idx:]
	}

	if serverName == "" {
		http.Error(w, "missing server name", http.StatusBadRequest)
		return
	}

	// Find server config
	var remote *config.RemoteServerConfig
	for i := range s.cfg.Servers {
		if s.cfg.Servers[i].Name == serverName && s.cfg.Servers[i].Enabled {
			remote = &s.cfg.Servers[i]
			break
		}
	}
	if remote == nil {
		http.Error(w, fmt.Sprintf("server %q not found or disabled", serverName), http.StatusNotFound)
		return
	}

	targetURL := strings.TrimRight(remote.URL, "/") + remotePath
	if r.URL.RawQuery != "" {
		targetURL += "?" + r.URL.RawQuery
	}

	proxyReq, err := http.NewRequestWithContext(r.Context(), r.Method, targetURL, r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// Forward headers
	for k, vals := range r.Header {
		for _, v := range vals {
			proxyReq.Header.Add(k, v)
		}
	}
	// Inject remote token
	if remote.Token != "" {
		proxyReq.Header.Set("Authorization", "Bearer "+remote.Token)
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(proxyReq)
	if err != nil {
		http.Error(w, fmt.Sprintf("proxy error: %v", err), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	// Copy response headers and body
	for k, vals := range resp.Header {
		for _, v := range vals {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body) //nolint:errcheck
}

// handleListServers returns the configured remote servers (with tokens masked).
func (s *Server) handleListServers(w http.ResponseWriter, r *http.Request) {
	type serverInfo struct {
		Name    string `json:"name"`
		URL     string `json:"url"`
		HasAuth bool   `json:"has_auth"`
		Enabled bool   `json:"enabled"`
	}
	result := make([]serverInfo, 0, len(s.cfg.Servers)+1)
	// Always include implicit local entry
	result = append(result, serverInfo{
		Name:    "local",
		URL:     fmt.Sprintf("http://localhost:%d", s.cfg.Server.Port),
		HasAuth: s.cfg.Server.Token != "",
		Enabled: true,
	})
	for _, sv := range s.cfg.Servers {
		result = append(result, serverInfo{
			Name:    sv.Name,
			URL:     sv.URL,
			HasAuth: sv.Token != "",
			Enabled: sv.Enabled,
		})
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result) //nolint:errcheck
}

// ---- Schedule endpoints ----------------------------------------------------

// handleSchedule dispatches GET/POST/DELETE for /api/schedule
func (s *Server) handleSchedule(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleGetSchedule(w, r)
	case http.MethodPost:
		s.handlePostSchedule(w, r)
	case http.MethodDelete:
		s.handleDeleteSchedule(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleGetSchedule(w http.ResponseWriter, _ *http.Request) {
	if s.schedStore == nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]interface{}{}) //nolint:errcheck
		return
	}
	entries := s.schedStore.List()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(entries) //nolint:errcheck
}

func (s *Server) handlePostSchedule(w http.ResponseWriter, r *http.Request) {
	if s.schedStore == nil {
		http.Error(w, "scheduling not available", http.StatusServiceUnavailable)
		return
	}
	var req struct {
		SessionID  string `json:"session_id"`
		Command    string `json:"command"`
		RunAt      string `json:"run_at,omitempty"`      // RFC3339 or "" for on-input
		RunAfterID string `json:"run_after_id,omitempty"` // chain after another scheduled command
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	if req.SessionID == "" || req.Command == "" {
		http.Error(w, "session_id and command are required", http.StatusBadRequest)
		return
	}
	var runAt time.Time
	if req.RunAt != "" {
		var err error
		runAt, err = time.Parse(time.RFC3339, req.RunAt)
		if err != nil {
			http.Error(w, "invalid run_at format (use RFC3339)", http.StatusBadRequest)
			return
		}
	}
	sc, err := s.schedStore.Add(req.SessionID, req.Command, runAt, req.RunAfterID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(sc) //nolint:errcheck
}

func (s *Server) handleDeleteSchedule(w http.ResponseWriter, r *http.Request) {
	if s.schedStore == nil {
		http.Error(w, "scheduling not available", http.StatusServiceUnavailable)
		return
	}
	id := r.URL.Query().Get("id")
	if id == "" {
		http.Error(w, "id query param required", http.StatusBadRequest)
		return
	}
	if err := s.schedStore.Cancel(id); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "cancelled"}) //nolint:errcheck
}

// ---- /api/commands --------------------------------------------------------

func (s *Server) handleCommands(w http.ResponseWriter, r *http.Request) {
	if s.cmdLib == nil {
		http.Error(w, "command library not available", http.StatusServiceUnavailable)
		return
	}
	switch r.Method {
	case http.MethodGet:
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(s.cmdLib.List()) //nolint:errcheck
	case http.MethodPost:
		var body struct {
			Name    string `json:"name"`
			Command string `json:"command"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid JSON", http.StatusBadRequest)
			return
		}
		if body.Name == "" || body.Command == "" {
			http.Error(w, "name and command required", http.StatusBadRequest)
			return
		}
		cmd, err := s.cmdLib.Add(body.Name, body.Command)
		if err != nil {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(cmd) //nolint:errcheck
	case http.MethodPut:
		var body struct {
			OldName string `json:"old_name"`
			Name    string `json:"name"`
			Command string `json:"command"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid JSON", http.StatusBadRequest)
			return
		}
		if body.OldName == "" {
			http.Error(w, "old_name required", http.StatusBadRequest)
			return
		}
		updated, err := s.cmdLib.Update(body.OldName, body.Name, body.Command)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(updated) //nolint:errcheck
	case http.MethodDelete:
		name := r.URL.Query().Get("name")
		if name == "" {
			http.Error(w, "name query param required", http.StatusBadRequest)
			return
		}
		if err := s.cmdLib.Delete(name); err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "deleted"}) //nolint:errcheck
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// ---- /api/filters --------------------------------------------------------

func (s *Server) handleFilters(w http.ResponseWriter, r *http.Request) {
	if s.filterStore == nil {
		http.Error(w, "filter store not available", http.StatusServiceUnavailable)
		return
	}
	switch r.Method {
	case http.MethodGet:
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(s.filterStore.List()) //nolint:errcheck
	case http.MethodPost:
		var body struct {
			Pattern string `json:"pattern"`
			Action  string `json:"action"`
			Value   string `json:"value"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid JSON", http.StatusBadRequest)
			return
		}
		if body.Pattern == "" || body.Action == "" {
			http.Error(w, "pattern and action required", http.StatusBadRequest)
			return
		}
		fp, err := s.filterStore.Add(body.Pattern, session.FilterAction(body.Action), body.Value)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(fp) //nolint:errcheck
	case http.MethodDelete:
		id := r.URL.Query().Get("id")
		if id == "" {
			http.Error(w, "id query param required", http.StatusBadRequest)
			return
		}
		if err := s.filterStore.Delete(id); err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "deleted"}) //nolint:errcheck
	case http.MethodPatch:
		var body struct {
			ID      string `json:"id"`
			Enabled *bool  `json:"enabled"`
			Pattern string `json:"pattern"`
			Action  string `json:"action"`
			Value   string `json:"value"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid JSON", http.StatusBadRequest)
			return
		}
		if body.ID == "" {
			http.Error(w, "id required", http.StatusBadRequest)
			return
		}
		var err error
		// Full update when pattern or action provided
		if body.Pattern != "" || body.Action != "" {
			enabled := true
			if body.Enabled != nil {
				enabled = *body.Enabled
			}
			err = s.filterStore.Update(body.ID, body.Pattern, body.Action, body.Value, enabled)
		} else if body.Enabled != nil {
			err = s.filterStore.SetEnabled(body.ID, *body.Enabled)
		} else {
			http.Error(w, "nothing to update", http.StatusBadRequest)
			return
		}
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "updated"}) //nolint:errcheck
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// ---- /api/alerts --------------------------------------------------------

func (s *Server) handleAlerts(w http.ResponseWriter, r *http.Request) {
	if s.alertStore == nil {
		http.Error(w, "alert store not available", http.StatusServiceUnavailable)
		return
	}
	switch r.Method {
	case http.MethodGet:
		w.Header().Set("Content-Type", "application/json")
		type alertsResponse struct {
			Alerts      []*alerts.Alert `json:"alerts"`
			UnreadCount int             `json:"unread_count"`
		}
		json.NewEncoder(w).Encode(alertsResponse{ //nolint:errcheck
			Alerts:      s.alertStore.List(),
			UnreadCount: s.alertStore.UnreadCount(),
		})
	case http.MethodPost:
		// Mark read: body {"id":"<id>"} or {"all":true}
		var body struct {
			ID  string `json:"id"`
			All bool   `json:"all"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid JSON", http.StatusBadRequest)
			return
		}
		if body.All {
			s.alertStore.MarkAllRead()
		} else if body.ID != "" {
			if err := s.alertStore.MarkRead(body.ID); err != nil {
				http.Error(w, err.Error(), http.StatusNotFound)
				return
			}
		} else {
			http.Error(w, "id or all required", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"}) //nolint:errcheck
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// ---- Channel API (MCP channel server integration) -------------------------

// BroadcastChannelReply broadcasts a channel reply to all connected WS clients.
// Used by opencode ACP to route SSE text replies through the same path as
// claude MCP channel replies, so they render as amber channel-reply-line in the UI.
func (s *Server) BroadcastChannelReply(sessionID, text string) {
	replyData := map[string]interface{}{
		"text":       text,
		"session_id": sessionID,
	}
	raw, _ := json.Marshal(replyData)
	outMsg := WSMessage{Type: MsgChannelReply, Data: raw, Timestamp: time.Now()}
	payload, _ := json.Marshal(outMsg)
	s.hub.broadcast <- payload
}

// handleChannelReply receives replies from claude (via the datawatch MCP channel server)
// and broadcasts them to all connected WebSocket clients and messaging backends.
func (s *Server) handleChannelReply(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Text      string `json:"text"`
		SessionID string `json:"session_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	// Broadcast channel_reply to all WS clients.
	replyData := map[string]interface{}{
		"text":       body.Text,
		"session_id": body.SessionID,
	}
	raw, _ := json.Marshal(replyData)
	outMsg := WSMessage{Type: MsgChannelReply, Data: raw, Timestamp: time.Now()}
	payload, _ := json.Marshal(outMsg)
	s.hub.broadcast <- payload

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"}) //nolint:errcheck
}

// handleChannelNotify receives notifications from the MCP channel server
// (e.g. permission relay requests) and broadcasts to WS clients.
func (s *Server) handleChannelNotify(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Text      string `json:"text"`
		Type      string `json:"type"`
		RequestID string `json:"request_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	notifyData := map[string]interface{}{
		"text":       body.Text,
		"subtype":    body.Type,
		"request_id": body.RequestID,
	}
	raw, _ := json.Marshal(notifyData)
	outMsg := WSMessage{Type: MsgChannelNotify, Data: raw, Timestamp: time.Now()}
	payload, _ := json.Marshal(outMsg)
	s.hub.broadcast <- payload

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"}) //nolint:errcheck
}

// handleChannelReady is called by the MCP channel server once it has connected to
// Claude Code and is ready to receive messages. datawatch uses this callback to
// send the session's initial task (if any) as the first channel message.
// POST /api/channel/ready {"session_id":"...", "port":7433}
func (s *Server) handleChannelReady(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		SessionID string `json:"session_id"`
		Port      int    `json:"port"`
	}
	json.NewDecoder(r.Body).Decode(&body) //nolint:errcheck

	port := body.Port
	if port == 0 {
		port = s.cfg.Server.ChannelPort
		if port == 0 {
			port = 7433
		}
	}

	// Find the session this channel belongs to.
	var readySess *session.Session
	if body.SessionID != "" {
		if sess, ok := s.manager.GetSession(body.SessionID); ok {
			readySess = sess
		}
	}
	if readySess == nil {
		// Fallback: find the most recently started running claude-code session
		sessions := s.manager.ListSessions()
		for i := len(sessions) - 1; i >= 0; i-- {
			sess := sessions[i]
			if sess.LLMBackend == "claude-code" &&
				(sess.State == session.StateRunning || sess.State == session.StateWaitingInput) &&
				sess.Hostname == s.hostname {
				readySess = sess
				break
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")

	// Broadcast channel_ready to WebSocket clients so UI can dismiss the banner.
	if readySess != nil {
		s.hub.Broadcast(MsgChannelReady, map[string]string{"session_id": readySess.FullID})
		fmt.Printf("[channel] ready for session %s\n", readySess.FullID)
	}

	// Only forward a task if the session has one
	if readySess == nil || readySess.Task == "" {
		json.NewEncoder(w).Encode(map[string]string{"status": "no_task"}) //nolint:errcheck
		return
	}
	targetSess := readySess

	// Forward the task to the channel server.
	payload, _ := json.Marshal(map[string]string{
		"text":       targetSess.Task,
		"source":     "datawatch",
		"session_id": targetSess.FullID,
	})
	url := fmt.Sprintf("http://127.0.0.1:%d/send", port)
	resp, err := (&http.Client{Timeout: 5 * time.Second}).Post(url, "application/json", strings.NewReader(string(payload)))
	if err != nil {
		json.NewEncoder(w).Encode(map[string]string{"status": "send_failed", "error": err.Error()}) //nolint:errcheck
		return
	}
	defer resp.Body.Close()
	json.NewEncoder(w).Encode(map[string]string{"status": "ok", "session_id": targetSess.FullID}) //nolint:errcheck
}

// handleChannelSend sends a message to the MCP channel server (forwards to claude).
// POST /api/channel/send {"text":"...", "session_id":"..."}
func (s *Server) handleChannelSend(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Text      string `json:"text"`
		SessionID string `json:"session_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	channelPort := s.cfg.Server.ChannelPort
	if channelPort == 0 {
		channelPort = 7433
	}
	url := fmt.Sprintf("http://127.0.0.1:%d/send", channelPort)
	payload, _ := json.Marshal(map[string]string{
		"text":       body.Text,
		"source":     "datawatch",
		"session_id": body.SessionID,
	})
	resp, err := (&http.Client{Timeout: 5 * time.Second}).Post(url, "application/json", strings.NewReader(string(payload)))
	if err != nil {
		http.Error(w, fmt.Sprintf("channel server unreachable: %v", err), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"}) //nolint:errcheck
}

// handleRestart restarts the daemon in-place via syscall.Exec.
// POST /api/restart
func (s *Server) handleRestart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.restartFn == nil {
		http.Error(w, "restart not available", http.StatusNotImplemented)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "restarting"}) //nolint:errcheck
	s.hub.Broadcast(MsgNotification, map[string]string{"message": "Daemon restarting…"})
	go func() {
		time.Sleep(500 * time.Millisecond)
		s.restartFn()
	}()
}

// handleUpdate installs the latest release in the background and restarts the daemon.
// POST /api/update
// Response: {"status":"checking"} immediately; the process restarts on success.
func (s *Server) handleUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.installUpdate == nil || s.latestVersion == nil {
		http.Error(w, "update not available", http.StatusNotImplemented)
		return
	}

	latest, err := s.latestVersion()
	if err != nil {
		http.Error(w, fmt.Sprintf("version check failed: %v", err), http.StatusInternalServerError)
		return
	}

	if latest == Version {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "up_to_date", "version": Version}) //nolint:errcheck
		return
	}

	// Respond immediately; the goroutine restarts the process after install.
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
		"status":   "installing",
		"version":  latest,
		"message":  "Downloading v" + latest + "… daemon will restart automatically.",
	})

	// Broadcast progress to WS clients
	s.hub.Broadcast(MsgNotification, map[string]string{
		"message": "[update] Downloading v" + latest + "…",
	})

	go func() {
		if err := s.installUpdate(latest); err != nil {
			s.hub.Broadcast(MsgNotification, map[string]string{
				"message": "[update] Install failed: " + err.Error(),
			})
			return
		}
		s.hub.Broadcast(MsgNotification, map[string]string{
			"message": "[update] Installed v" + latest + ". Restarting daemon…",
		})
		// Give clients 800ms to receive the message before the process dies.
		time.Sleep(800 * time.Millisecond)
		selfPath, err := os.Executable()
		if err == nil {
			selfPath, _ = filepath.EvalSymlinks(selfPath)
			_ = syscall.Exec(selfPath, os.Args, os.Environ()) //nolint:errcheck
		}
		// If Exec fails (Windows), just exit so the supervisor/user can restart.
		os.Exit(0)
	}()
}
