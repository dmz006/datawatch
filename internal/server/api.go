package server

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"syscall"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/dmz006/datawatch/internal/agents"
	"github.com/dmz006/datawatch/internal/alerts"
	"github.com/dmz006/datawatch/internal/audit"
	"github.com/dmz006/datawatch/internal/devices"
	"github.com/dmz006/datawatch/internal/messaging"
	"github.com/dmz006/datawatch/internal/profile"
	"github.com/dmz006/datawatch/internal/proxy"
	"github.com/dmz006/datawatch/internal/stats"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/llm"
	"github.com/dmz006/datawatch/internal/llm/backends/ollama"
	"github.com/dmz006/datawatch/internal/rtk"
	"github.com/dmz006/datawatch/internal/llm/backends/openwebui"
	"github.com/dmz006/datawatch/internal/router"
	"github.com/dmz006/datawatch/internal/session"
)

// PipelineAPI is the interface for pipeline operations from the HTTP server.
type PipelineAPI interface {
	StartPipeline(name, projectDir string, taskSpecs []string, maxParallel int) (string, error)
	GetStatus(id string) string
	Cancel(id string) error
	ListAll() string
	ListJSON() []map[string]interface{}
}

// MemoryAPI is the interface for memory operations from the HTTP server.
type MemoryAPI interface {
	Stats() map[string]interface{}
	ListRecent(projectDir string, n int) ([]map[string]interface{}, error)
	ListFiltered(projectDir, role, since string, n int) ([]map[string]interface{}, error)
	Search(query string, topK int) ([]map[string]interface{}, error)
	// SearchInNamespaces (BL101) — namespace-filtered semantic search.
	// Used by the cross-profile expansion path on /api/memory/search
	// when the caller supplies a profile name.
	SearchInNamespaces(query string, namespaces []string, topK int) ([]map[string]interface{}, error)
	Delete(id int64) error
	Remember(projectDir, text string) (int64, error)
	Export(w io.Writer) error
	Import(r io.Reader) (int, error)
	WALRecent(n int) []map[string]interface{}
	Reindex() (int, error)
	ListLearnings(projectDir, query string, n int) ([]map[string]interface{}, error)
	Research(query string, maxResults int) ([]map[string]interface{}, error)
}

// KGAPI is the interface for knowledge graph operations from the HTTP server.
type KGAPI interface {
	AddTriple(subject, predicate, object, validFrom, source string) (int64, error)
	Invalidate(subject, predicate, object, ended string) error
	QueryEntity(name, asOf string) ([]map[string]interface{}, error)
	Timeline(name string) ([]map[string]interface{}, error)
	Stats() map[string]interface{}
}

// startTime records when the daemon started (for uptime calculation).
var startTime = time.Now()

// Version is set at build time. The server package uses this for /api/health and /api/info.
var Version = "5.26.34"

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
	statsCollector    *stats.Collector
	// F10 sprint 2: profile stores. Wired from main.go via setters so
	// unit tests can leave them nil (handlers return 503).
	projectStore      *profile.ProjectStore
	clusterStore      *profile.ClusterStore
	// F10 sprint 3: agent lifecycle manager.
	agentMgr          *agents.Manager

	// BL107 — wired on startup; consumed by handleAgentAudit.
	// Empty path or CEF format disables the REST query.
	agentAuditPath string
	agentAuditCEF  bool

	// BL104 — peer broker for worker P2P messaging.
	peerBroker *agents.PeerBroker

	// BL102 — registry of comm backends + per-channel default
	// recipients so workers can post outbound alerts via
	// /api/proxy/comm/{channel}/send.
	commBackends map[string]messaging.Backend
	commDefaults map[string]string

	// Issue #1 — mobile push token registry.
	deviceStore *devices.Store

	// Issue #2 — whisper transcriber for /api/voice/transcribe.
	transcriber transcribeSurface

	// BL9 — operator audit log.
	auditLog *audit.Log

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
	// The progress callback is invoked with (downloaded, total) byte counts
	// while the asset is streaming — total may be 0 if the server didn't
	// send Content-Length.
	installUpdate func(version string, progress func(downloaded, total int64)) error
	// latestVersion returns the latest available release tag (without "v" prefix).
	latestVersion func() (string, error)

	// testMessageHandler is an optional function that routes a simulated incoming
	// message through the router, enabling comm channel testing via the API.
	testMessageHandler func(text string) []string

	// memoryAPI provides memory operations for the REST API.
	memoryAPI MemoryAPI
	// kgAPI provides knowledge graph operations for the REST API.
	kgAPI KGAPI

	// memoryTestFn tests Ollama embedding capability before enabling memory.
	memoryTestFn func(host, model string) (int, error)

	// pipelineExec provides pipeline operations for the REST API.
	pipelineExec PipelineAPI

	// proxyPool tracks remote server health and provides pooled HTTP clients.
	proxyPool interface {
		Health() []proxy.ServerHealth
		IsHealthy(string) bool
	}

	// offlineQueue tracks commands queued for unreachable servers.
	offlineQueue interface {
		PendingAll() map[string]int
	}

	// BL24+BL25 — autonomous PRD decomposition manager (Sprint S6, v3.10.0).
	// Wired from main.go; nil when autonomous is disabled. Handlers
	// return 503 in that case.
	autonomousMgr AutonomousAPI

	// BL33 — plugin framework registry (Sprint S7, v3.11.0).
	// Wired from main.go; nil when plugins.enabled=false. Handlers
	// return 503 in that case.
	pluginsAPI PluginsAPI

	// BL117 — PRD-DAG orchestrator (Sprint S8, v4.0.0). Wired from
	// main.go; nil when orchestrator.enabled=false. Handlers return
	// 503 in that case.
	orchestratorAPI OrchestratorAPI

	// BL171 — observer (Sprint S9, v4.1.0). Unified stats +
	// process tree + envelope roll-up. Nil when observer.plugin_enabled
	// = false; /api/stats falls back to the v1 statsCollector.
	observerAPI ObserverAPI

	// Native plugins — built-in subsystems that look like plugins to the
	// operator (observer, future native bridges) but are linked into the
	// daemon. Surfaced under /api/plugins so the PWA's plugin card can
	// list them alongside subprocess plugins. Wired from main.go.
	nativePlugins []NativePlugin

	// BL172 (S11) — Shape B / C peer registry. Nil when
	// observer.peers.allow_register is false.
	peerRegistry PeerRegistryAPI

	// S14a (v4.8.0) — federation loop-prevention. Set by main.go
	// to the local primary's peer name (typically host name) when
	// federation is configured. Empty leaves loop detection off.
	federationSelfName string

	// v5.27.0 — per-session channel ring buffer. The PWA Channel tab
	// connects after messages have already been broadcast over WS, so
	// without this it shows nothing while the datawatch-app (which
	// stays connected) shows full activity. We keep the last
	// channelHistoryMax entries per session FullID.
	channelHistMu sync.Mutex
	channelHist   map[string][]channelHistEntry

	// v5.26.24 — BL113 token broker for daemon-side clone of
	// project_profile-based PRDs. When wired, each clone mints a
	// short-lived per-spawn token via gitMinter.MintForWorker and
	// revokes it after. Nil = use DATAWATCH_GIT_TOKEN env / local
	// creds (v5.26.22 path). main.go's brokerAdapter satisfies the
	// interface.
	gitMinter GitTokenMinter
}

// SetGitTokenMinter wires the BL113 token broker for daemon-side
// clone. Optional: when nil, clones use the env / local-creds path.
func (s *Server) SetGitTokenMinter(m GitTokenMinter) { s.gitMinter = m }

// channelHistEntry is one stored message for the per-session channel
// ring buffer. Direction is "incoming" (claude → operator) or
// "outgoing" (operator → claude).
type channelHistEntry struct {
	Text      string    `json:"text"`
	SessionID string    `json:"session_id"`
	Direction string    `json:"direction,omitempty"`
	Timestamp time.Time `json:"timestamp"`
}

const channelHistoryMax = 100

// NativePlugin describes a built-in subsystem that the /api/plugins list
// should surface to operators. Status is computed on the fly so it
// reflects current config / runtime state.
type NativePlugin struct {
	Name        string `json:"name"`
	Kind        string `json:"kind"` // always "native"
	Description string `json:"description,omitempty"`
	// Status returns enabled/version/message for this subsystem. Called
	// every time /api/plugins is read.
	Status func() NativePluginStatus `json:"-"`
}

// NativePluginStatus is the runtime view of a native plugin.
type NativePluginStatus struct {
	Enabled bool   `json:"enabled"`
	Version string `json:"version,omitempty"`
	Message string `json:"message,omitempty"`
}

// RegisterNativePlugin appends a built-in plugin entry. main.go calls
// this for the observer (and future native subsystems) so /api/plugins
// can list them alongside subprocess plugins.
func (s *Server) RegisterNativePlugin(p NativePlugin) {
	s.nativePlugins = append(s.nativePlugins, p)
}

// AutonomousAPI is the surface the REST handlers need from
// internal/autonomous.Manager. Defining it as an interface keeps
// server-package tests free of a hard dependency on the autonomous
// package and lets us swap in a fake.
type AutonomousAPI interface {
	Config() any
	SetConfig(any) error
	Status() any
	CreatePRD(spec, projectDir, backend, effort string) (any, error)
	GetPRD(id string) (any, bool)
	ListPRDs() []any
	Decompose(id string) (any, error)
	Run(id string) error
	Cancel(id string) error
	ListLearnings() []any

	// SessionIDsForPRD walks a PRD's tasks and returns every
	// Task.SessionID that's been scheduled. S13 follow-up — used by
	// the orchestrator handler to enrich graph nodes with per-node
	// ObserverSummary. Returns nil for unknown PRDs.
	SessionIDsForPRD(prdID string) []string

	// BL191 Q1 (v5.2.0) — review/approve gate.
	Approve(id, actor, note string) (any, error)
	Reject(id, actor, reason string) (any, error)
	RequestRevision(id, actor, note string) (any, error)
	EditTaskSpec(prdID, taskID, newSpec, actor string) (any, error)
	// v5.26.32 — story title + description edit (parallels
	// EditTaskSpec; same gate: needs_review / revisions_asked).
	EditStory(prdID, storyID, newTitle, newDescription, actor string) (any, error)

	// BL191 Q2 — template instantiation.
	InstantiateTemplate(templateID string, vars map[string]string, actor string) (any, error)

	// BL203 (v5.4.0) — flexible LLM overrides at PRD + task level.
	SetPRDLLM(prdID, backend, effort, model, actor string) (any, error)
	SetTaskLLM(prdID, taskID, backend, effort, model, actor string) (any, error)

	// BL191 Q4 (v5.9.0) — child PRDs spawned from a parent's SpawnPRD
	// tasks. Empty list when none.
	ListChildPRDs(prdID string) []any

	// v5.19.0 — full CRUD finally. Hard-delete (removes from JSONL +
	// descendants) and edit-PRD-fields (Title + Spec, non-running only).
	DeletePRD(id string) error
	EditPRDFields(id, title, spec, actor string) (any, error)

	// v5.26.19 — F10 project + cluster profile attachment for autonomous
	// PRDs. Either or both can be set (empty = unset). Returns an error
	// if a named profile doesn't exist. Operator-reported: PRDs should
	// be based on directory or profile, with cluster_profile dispatching
	// the worker to /api/agents instead of local tmux.
	SetPRDProfiles(prdID, projectProfile, clusterProfile string) error
}

// SetAutonomousAPI is the wiring entry point used by main.go.
func (s *Server) SetAutonomousAPI(a AutonomousAPI) { s.autonomousMgr = a }

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
		channelHist:        make(map[string][]channelHistEntry),
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
		return s.cfg.Session.ClaudeEnabled
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
		return s.cfg.OpenCodeACP.Enabled
	case "opencode-prompt":
		return s.cfg.OpenCodePrompt.Enabled
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
		SupportsResume bool   `json:"supports_resume,omitempty"`
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
				if _, ok := b.(llm.Resumable); ok {
					backends[i].SupportsResume = true
				}
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
func (s *Server) SetStatsCollector(c *stats.Collector) { s.statsCollector = c }

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

// SetUpdateFuncs wires update-related functions. installFn downloads
// and installs a given version string; the progress callback is
// invoked during the download stream with (downloaded, total) byte
// counts so the REST handler can fan progress out over the WS hub.
// latestFn returns the latest available version tag.
func (s *Server) SetUpdateFuncs(installFn func(version string, progress func(downloaded, total int64)) error, latestFn func() (string, error)) {
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

// handleSessions returns all sessions as JSON.
//
// BL116 — when the schedule store is wired, each session is decorated
// with a `scheduled_count` field so the web UI / comm renderers can
// surface a badge without a follow-up RPC. The decorator is additive
// (wraps the embedded *session.Session) so older clients keep
// parsing without changes.
func (s *Server) handleSessions(w http.ResponseWriter, r *http.Request) {
	sessions := s.manager.ListSessions()
	w.Header().Set("Content-Type", "application/json")

	if s.schedStore == nil {
		_ = json.NewEncoder(w).Encode(sessions)
		return
	}
	type sessionWithCounts struct {
		*session.Session
		ScheduledCount int `json:"scheduled_count"`
	}
	enriched := make([]sessionWithCounts, 0, len(sessions))
	for _, sess := range sessions {
		enriched = append(enriched, sessionWithCounts{
			Session:        sess,
			ScheduledCount: s.schedStore.CountForSession(sess.FullID),
		})
	}
	_ = json.NewEncoder(w).Encode(enriched)
}

// handleSessionOutput returns the last N lines of a session's output.
// For agent-bound sessions (F10 sprint 3.6) the request is forwarded
// to the worker through the agent reverse proxy.
func (s *Server) handleSessionOutput(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	if s.forwardSessionToAgent(w, r, id) {
		return
	}
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

// forwardSessionToAgent inspects the session for AgentID; if set, it
// forwards the current request to the worker via the agent proxy and
// returns true (caller must NOT continue). Returns false when the
// session is local (caller proceeds normally) or unknown (caller
// handles the 404 itself for backward-compatible error messages).
func (s *Server) forwardSessionToAgent(w http.ResponseWriter, r *http.Request, sessID string) bool {
	if sessID == "" || s.agentMgr == nil {
		return false
	}
	sess, ok := s.manager.GetSession(sessID)
	if !ok || sess.AgentID == "" {
		return false
	}
	// Re-dispatch through the agent proxy. Preserves the existing
	// path so the worker sees the same /api/output?id=… request it
	// would have served if called directly.
	s.handleAgentProxy(w, r, sess.AgentID, r.URL.Path)
	return true
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
	// Handle tmux-copy-mode command: enters tmux copy-mode directly (more reliable than C-b [).
	if strings.HasPrefix(raw, "tmux-copy-mode ") {
		sessID := strings.TrimSpace(raw[15:])
		sess, ok := s.manager.GetSession(sessID)
		if !ok {
			return fmt.Sprintf("Session %s not found", sessID)
		}
		if err := exec.Command("tmux", "copy-mode", "-t", sess.TmuxSession).Run(); err != nil {
			return fmt.Sprintf("Error: %v", err)
		}
		return fmt.Sprintf("[%s] Copy mode entered", sessID)
	}

	// Handle sendkey command: sends raw tmux key name(s) without appending Enter.
	// Format: "sendkey <session_id>: <KeyName>" (e.g. "sendkey abc123: Up")
	// Supports space-separated multi-key sequences: "sendkey abc123: C-b ["
	if strings.HasPrefix(raw, "sendkey ") {
		parts := strings.SplitN(raw[8:], ":", 2)
		if len(parts) == 2 {
			sessID := strings.TrimSpace(parts[0])
			keyName := strings.TrimSpace(parts[1])
			sess, ok := s.manager.GetSession(sessID)
			if !ok {
				return fmt.Sprintf("Session %s not found", sessID)
			}
			// Split into individual keys and send sequentially
			keys := strings.Fields(keyName)
			for _, k := range keys {
				if err := exec.Command("tmux", "send-keys", "-t", sess.TmuxSession, k).Run(); err != nil {
					return fmt.Sprintf("Error sending key %q: %v", k, err)
				}
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
		send:       make(chan []byte, 2048),
		subscribed: make(map[string]bool),
	}
	s.hub.register <- c

	// Send initial session list
	sessions := s.manager.ListSessions()
	raw, _ := json.Marshal(SessionsData{Sessions: sessions})
	msg := WSMessage{Type: MsgSessions, Data: raw, Timestamp: time.Now()}
	payload, _ := json.Marshal(msg)
	c.safeSend(payload)

	go c.writePump()

	// Read pump (blocking)
	defer func() {
		// Cancel all screen captures for this client
		c.mu.Lock()
		for _, cancel := range c.captureCancels {
			cancel()
		}
		c.mu.Unlock()
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
		if s.hub.chanStats != nil {
			s.hub.chanStats.RecordRecv(len(msgBytes))
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
			c.safeSend(respPayload)

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
			c.safeSend(respPayload)

		case MsgSendInput:
			var d SendInputData
			json.Unmarshal(inMsg.Data, &d) //nolint:errcheck
			if d.Raw {
				// Raw mode: send literal bytes to tmux (for interactive terminal)
				sess, ok := s.manager.GetSession(d.SessionID)
				if !ok {
					// Try short ID
					for _, sr := range s.manager.ListSessions() {
						if sr.ID == d.SessionID {
							sess = sr
							ok = true
							break
						}
					}
				}
				if ok {
					s.manager.SendRawKeys(sess.FullID, d.Text)
				}
			} else {
				cmd := router.Command{Type: router.CmdSend, SessionID: d.SessionID, Text: d.Text}
				result := s.executeCommand(cmd, "")
				respRaw, _ := json.Marshal(NotificationData{Message: result})
				resp := WSMessage{Type: MsgNotification, Data: respRaw, Timestamp: time.Now()}
				respPayload, _ := json.Marshal(resp)
				c.safeSend(respPayload)
			}

		case MsgSubscribe:
			var d SubscribeData
			json.Unmarshal(inMsg.Data, &d) //nolint:errcheck
			c.mu.Lock()
			c.subscribed[d.SessionID] = true
			c.mu.Unlock()
			// Send recent output immediately — both stripped (for fallback) and raw (for xterm.js)
			output, err := s.manager.TailOutput(d.SessionID, 50)
			if err == nil {
				lines := strings.Split(output, "\n")
				outRaw, _ := json.Marshal(OutputData{SessionID: d.SessionID, Lines: lines})
				outMsg := WSMessage{Type: MsgOutput, Data: outRaw, Timestamp: time.Now()}
				outPayload, _ := json.Marshal(outMsg)
				c.safeSend(outPayload)
			}
			// Start screen capture for real-time terminal updates.
			// Uses tmux capture-pane every 200ms — only sends when content changes.
			capCtx, capCancel := context.WithCancel(context.Background())
			c.mu.Lock()
			// Cancel any previous capture for this client
			if c.captureCancels == nil {
				c.captureCancels = make(map[string]context.CancelFunc)
			}
			if prev, ok := c.captureCancels[d.SessionID]; ok {
				prev()
			}
			c.captureCancels[d.SessionID] = capCancel
			c.mu.Unlock()
			s.manager.StartScreenCapture(capCtx, d.SessionID, 200)

			// Send initial pane capture immediately with priority — blocking send
			// ensures it arrives before output flood can fill the channel buffer.
			captured, capErr := s.manager.CapturePaneANSI(d.SessionID)
			// (debug logging removed — initial capture silently handles errors)
			if capErr == nil && captured != "" {
				capLines := strings.Split(captured, "\n")
				capRaw, _ := json.Marshal(map[string]interface{}{
					"session_id": d.SessionID,
					"lines":      capLines,
				})
				capMsg := WSMessage{Type: "pane_capture", Data: capRaw, Timestamp: time.Now()}
				capPayload, _ := json.Marshal(capMsg)
				select {
				case c.send <- capPayload: // blocking priority send
				default:
					c.safeSend(capPayload) // fallback if still full
				}
			}

		case MsgResizeTerm:
			var d struct {
				SessionID string `json:"session_id"`
				Cols      int    `json:"cols"`
				Rows      int    `json:"rows"`
			}
			json.Unmarshal(inMsg.Data, &d) //nolint:errcheck
			if d.SessionID != "" && d.Cols > 0 && d.Rows > 0 {
				// Enforce minimum: never shrink below the session's configured console size
				sess, sok := s.manager.GetSession(d.SessionID)
				if sok {
					if sess.ConsoleCols > 0 && d.Cols < sess.ConsoleCols {
						d.Cols = sess.ConsoleCols
					}
					if sess.ConsoleRows > 0 && d.Rows < sess.ConsoleRows {
						d.Rows = sess.ConsoleRows
					}
				}
				s.manager.ResizeTmux(d.SessionID, d.Cols, d.Rows)
				// After resize, capture fresh pane content at the new dimensions
				// and send it back so xterm.js can re-render correctly.
				go func() {
					// Small delay to let tmux reflow content at new width
					time.Sleep(50 * time.Millisecond)
					captured, err := s.manager.CapturePaneANSI(d.SessionID)
					if err == nil && captured != "" {
						capLines := strings.Split(captured, "\n")
						capRaw, _ := json.Marshal(map[string]interface{}{
							"session_id": d.SessionID,
							"lines":      capLines,
						})
						capMsg := WSMessage{Type: "pane_capture", Data: capRaw, Timestamp: time.Now()}
						capPayload, _ := json.Marshal(capMsg)
						c.safeSend(capPayload)
					}
				}()
			}

		case MsgPing:
			pongRaw, _ := json.Marshal(map[string]string{"type": "pong"})
			c.safeSend(pongRaw)
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
	encrypted := s.manager.IsEncrypted()
	hasEnvPassword := os.Getenv("DATAWATCH_SECURE_PASSWORD") != ""
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
		"status":           "ok",
		"hostname":         s.hostname,
		"version":          Version,
		"uptime_seconds":   uptime,
		"encrypted":        encrypted,
		"has_env_password":  hasEnvPassword,
	})
}

// handleHealthz is a k8s liveness probe. Returns 200 if the HTTP server is responding.
// GET /healthz
func (s *Server) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"}) //nolint:errcheck
}

// handleReadyz is a k8s readiness probe. Returns 200 only when every subsystem
// the parent depends on is reachable. The structured body lists each subsystem
// individually so operators can see which one tipped the probe red without
// reading server logs.
//
// Subsystems checked (each marked ok|degraded|down|disabled):
//   - manager: session manager initialized + session store readable
//   - memory:  memory store reachable (skipped if not configured)
//   - mcp:     MCP docs registry available (skipped if not configured)
//
// A subsystem is "down" when its required dependency cannot be reached.
// "degraded" is reserved for partial capability (e.g. memory reachable but
// embedder unhealthy — future). "disabled" means the operator turned it off.
//
// The probe returns 503 if any required subsystem is "down". Disabled and
// degraded subsystems do not fail the probe.
//
// GET /readyz
func (s *Server) handleReadyz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	type sub struct {
		Status string `json:"status"`
		Reason string `json:"reason,omitempty"`
	}
	subs := map[string]sub{}
	overallDown := false

	// manager + session store
	switch {
	case s.manager == nil:
		subs["manager"] = sub{Status: "down", Reason: "manager not initialized"}
		overallDown = true
	case s.manager.ListSessions() == nil:
		// ListSessions returning nil indicates the underlying store is unloaded;
		// an empty slice is fine.
		subs["manager"] = sub{Status: "down", Reason: "session store not loaded"}
		overallDown = true
	default:
		subs["manager"] = sub{Status: "ok"}
	}

	// memory — optional. Stats() round-trips to the backend, so a returning
	// call confirms reachability.
	if s.memoryAPI == nil {
		subs["memory"] = sub{Status: "disabled"}
	} else {
		// Stats() can panic if the backend died mid-flight; recover so a
		// memory hiccup never takes the readiness probe with it.
		func() {
			defer func() {
				if r := recover(); r != nil {
					subs["memory"] = sub{Status: "down", Reason: fmt.Sprintf("stats panic: %v", r)}
					overallDown = true
				}
			}()
			if st := s.memoryAPI.Stats(); st == nil {
				subs["memory"] = sub{Status: "down", Reason: "stats returned nil"}
				overallDown = true
			} else {
				subs["memory"] = sub{Status: "ok"}
			}
		}()
	}

	// mcp docs func — set when MCP server wired in main.go
	if s.mcpDocsFunc == nil {
		subs["mcp"] = sub{Status: "disabled"}
	} else {
		subs["mcp"] = sub{Status: "ok"}
	}

	body := map[string]interface{}{
		"status":         "ready",
		"subsystems":     subs,
		"uptime_seconds": int(time.Since(startTime).Seconds()),
		"version":        Version,
	}
	if overallDown {
		body["status"] = "not_ready"
		w.WriteHeader(http.StatusServiceUnavailable)
	} else {
		w.WriteHeader(http.StatusOK)
		body["active_sessions"] = len(s.manager.ListSessions())
	}
	json.NewEncoder(w).Encode(body) //nolint:errcheck
}

// handleInterfaces returns available network interfaces for bind configuration.
// GET /api/interfaces → [{addr, name, label}] ordered: 0.0.0.0, 127.0.0.1, then other IPv4.
func (s *Server) handleInterfaces(w http.ResponseWriter, r *http.Request) {
	type ifaceEntry struct {
		Addr  string `json:"addr"`
		Name  string `json:"name"`
		Label string `json:"label"`
	}
	// Start with special entries
	result := []ifaceEntry{
		{Addr: "0.0.0.0", Name: "all", Label: "0.0.0.0 (all interfaces)"},
		{Addr: "127.0.0.1", Name: "loopback", Label: "127.0.0.1 (localhost)"},
	}
	// Add real interfaces
	ifaces, err := net.Interfaces()
	if err == nil {
		seen := map[string]bool{"0.0.0.0": true, "127.0.0.1": true}
		for _, iface := range ifaces {
			if iface.Flags&net.FlagUp == 0 {
				continue
			}
			addrs, err := iface.Addrs()
			if err != nil {
				continue
			}
			for _, a := range addrs {
				var ip net.IP
				switch v := a.(type) {
				case *net.IPNet:
					ip = v.IP
				case *net.IPAddr:
					ip = v.IP
				}
				if ip == nil || ip.To4() == nil {
					continue // skip IPv6 for now
				}
				ipStr := ip.String()
				if seen[ipStr] {
					continue
				}
				seen[ipStr] = true
				result = append(result, ifaceEntry{
					Addr:  ipStr,
					Name:  iface.Name,
					Label: fmt.Sprintf("%s (%s)", ipStr, iface.Name),
				})
			}
		}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result) //nolint:errcheck
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
		SupportsResume bool   `json:"supports_resume,omitempty"`
		Version        string `json:"version,omitempty"`
	}

	s.versionCacheMu.RLock()
	cached := s.versionCache
	cacheAge := time.Since(s.versionCacheAt)
	s.versionCacheMu.RUnlock()

	// Serve from cache if fresh (< 5 min)
	if cached != nil && cacheAge < 5*time.Minute {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
			"llm":    cached,
			"active": s.manager.ActiveBackend(),
		})
		return
	}

	// If no cache at all, return names immediately (no version checks) and warm in background
	if cached == nil {
		fast := make([]backendInfo, len(s.availableBackends))
		for i, name := range s.availableBackends {
			fast[i] = backendInfo{Name: name, Enabled: s.llmEnabled(name), Available: s.llmEnabled(name), PromptRequired: s.llmPromptRequired(name)}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"llm": fast, "active": s.manager.ActiveBackend()}) //nolint:errcheck
		go s.warmVersionCache()
		return
	}

	// Cache is stale — return it immediately and refresh in background.
	// Never block the API response on version checks.
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
		"llm":    cached,
		"active": s.manager.ActiveBackend(),
	})
	go s.warmVersionCache()
}

// handleFiles returns directory contents for path browsing, or
// creates a new directory when POSTed a {path, action:"mkdir"} body.
// Mkdir respects the same root-path restriction as GET listing.
func (s *Server) handleFiles(w http.ResponseWriter, r *http.Request) {
	// v4.0.1 — POST for directory creation. Backs the web UI +
	// mobile "create folder" affordance in the directory picker.
	if r.Method == http.MethodPost {
		s.handleFilesMkdir(w, r)
		return
	}
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

// handleFilesMkdir creates a directory under the operator's configured
// root path. POST body: {path, name} — creates <path>/<name>. Returns
// the new absolute path on success. v4.0.1 (directory-picker "create
// folder" affordance).
func (s *Server) handleFilesMkdir(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path string `json:"path"`
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.Path) == "" || strings.TrimSpace(req.Name) == "" {
		http.Error(w, "path + name required", http.StatusBadRequest)
		return
	}
	// Reject path traversal in the name — directory names only.
	if strings.ContainsAny(req.Name, "/\\") || req.Name == "." || req.Name == ".." {
		http.Error(w, "name must be a single directory component", http.StatusBadRequest)
		return
	}
	parent := req.Path
	if len(parent) > 0 && parent[0] == '~' {
		home, _ := os.UserHomeDir()
		parent = filepath.Join(home, parent[1:])
	}
	target := filepath.Join(parent, req.Name)

	// Enforce root-path restriction (same clamp as GET listing).
	if s.cfg != nil && s.cfg.Session.RootPath != "" {
		rootPath := s.cfg.Session.RootPath
		if len(rootPath) > 0 && rootPath[0] == '~' {
			home, _ := os.UserHomeDir()
			rootPath = filepath.Join(home, rootPath[1:])
		}
		cleanRoot := filepath.Clean(rootPath)
		cleanTarget := filepath.Clean(target)
		if !strings.HasPrefix(cleanTarget+string(filepath.Separator), cleanRoot+string(filepath.Separator)) &&
			cleanTarget != cleanRoot {
			http.Error(w, "target outside configured root", http.StatusForbidden)
			return
		}
	}
	// Parent must exist; don't silently create long chains.
	if fi, err := os.Stat(parent); err != nil {
		http.Error(w, "parent: "+err.Error(), http.StatusBadRequest)
		return
	} else if !fi.IsDir() {
		http.Error(w, "parent is not a directory", http.StatusBadRequest)
		return
	}
	if err := os.Mkdir(target, 0755); err != nil {
		if os.IsExist(err) {
			http.Error(w, "already exists", http.StatusConflict)
			return
		}
		http.Error(w, "mkdir: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status": "ok", "path": target,
	})
}

// handleRenameSession renames a session.
// ── Memory API endpoints ─────────────────────────────────────────────────────

func (s *Server) handleMemoryStats(w http.ResponseWriter, r *http.Request) {
	if s.memoryAPI == nil {
		json.NewEncoder(w).Encode(map[string]interface{}{"enabled": false}) //nolint:errcheck
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(s.memoryAPI.Stats()) //nolint:errcheck
}

func (s *Server) handleMemoryList(w http.ResponseWriter, r *http.Request) {
	if s.memoryAPI == nil {
		http.Error(w, "memory not enabled", http.StatusServiceUnavailable)
		return
	}
	projectDir := r.URL.Query().Get("project")
	role := r.URL.Query().Get("role")
	since := r.URL.Query().Get("since")
	n := 50
	if ns := r.URL.Query().Get("n"); ns != "" {
		fmt.Sscanf(ns, "%d", &n) //nolint:errcheck
	}
	results, err := s.memoryAPI.ListFiltered(projectDir, role, since, n)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(results) //nolint:errcheck
}

func (s *Server) handleMemorySearch(w http.ResponseWriter, r *http.Request) {
	if s.memoryAPI == nil {
		http.Error(w, "memory not enabled", http.StatusServiceUnavailable)
		return
	}
	query := r.URL.Query().Get("q")
	if query == "" {
		http.Error(w, "missing q parameter", http.StatusBadRequest)
		return
	}

	// BL101 — cross-profile namespace expansion. When the caller
	// passes ?profile=<name> (or ?agent_id=<id>) the server looks up
	// the effective namespace set (own + mutual-opt-in peers via
	// ProjectStore.EffectiveNamespacesFor) and runs a namespace-
	// filtered search. Workers use this so they don't need to know
	// peer profiles' raw namespace strings.
	profileName := r.URL.Query().Get("profile")
	if profileName == "" {
		if agentID := r.URL.Query().Get("agent_id"); agentID != "" && s.agentMgr != nil {
			if a := s.agentMgr.Get(agentID); a != nil {
				profileName = a.ProjectProfile
			}
		}
	}

	if profileName != "" && s.projectStore != nil {
		namespaces := s.projectStore.EffectiveNamespacesFor(profileName)
		results, err := s.memoryAPI.SearchInNamespaces(query, namespaces, 10)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(results)
		return
	}

	results, err := s.memoryAPI.Search(query, 10)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(results) //nolint:errcheck
}

// handleMemorySave saves a new memory via POST /api/memory/save.
// Body: {"content": "text to remember", "project_dir": "/optional/path"}
func (s *Server) handleMemorySave(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.memoryAPI == nil {
		http.Error(w, "memory not enabled", http.StatusServiceUnavailable)
		return
	}
	var req struct {
		Content    string `json:"content"`
		ProjectDir string `json:"project_dir"`
	}
	json.NewDecoder(r.Body).Decode(&req) //nolint:errcheck
	if req.Content == "" {
		http.Error(w, "missing content", http.StatusBadRequest)
		return
	}
	id, err := s.memoryAPI.Remember(req.ProjectDir, req.Content)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"status": "ok", "id": id}) //nolint:errcheck
}

func (s *Server) handleMemoryDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.memoryAPI == nil {
		http.Error(w, "memory not enabled", http.StatusServiceUnavailable)
		return
	}
	var req struct {
		ID int64 `json:"id"`
	}
	json.NewDecoder(r.Body).Decode(&req) //nolint:errcheck
	if req.ID <= 0 {
		http.Error(w, "missing id", http.StatusBadRequest)
		return
	}
	if err := s.memoryAPI.Delete(req.ID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"}) //nolint:errcheck
}

// handleMemoryReindex triggers re-embedding of all memories.
func (s *Server) handleMemoryReindex(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.memoryAPI == nil {
		http.Error(w, "memory not enabled", http.StatusServiceUnavailable)
		return
	}
	count, err := s.memoryAPI.Reindex()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"status": "ok", "reindexed": count}) //nolint:errcheck
}

// handleMemoryLearnings lists or searches task learnings.
func (s *Server) handleMemoryLearnings(w http.ResponseWriter, r *http.Request) {
	if s.memoryAPI == nil {
		http.Error(w, "memory not enabled", http.StatusServiceUnavailable)
		return
	}
	query := r.URL.Query().Get("q")
	projectDir := r.URL.Query().Get("project_dir")
	n := 20
	if nStr := r.URL.Query().Get("limit"); nStr != "" {
		fmt.Sscanf(nStr, "%d", &n)
	}
	results, err := s.memoryAPI.ListLearnings(projectDir, query, n)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(results) //nolint:errcheck
}

// handleMemoryResearch performs deep cross-session/cross-project search.
func (s *Server) handleMemoryResearch(w http.ResponseWriter, r *http.Request) {
	if s.memoryAPI == nil {
		http.Error(w, "memory not enabled", http.StatusServiceUnavailable)
		return
	}
	query := r.URL.Query().Get("q")
	if query == "" {
		http.Error(w, "missing q parameter", http.StatusBadRequest)
		return
	}
	maxResults := 20
	if nStr := r.URL.Query().Get("limit"); nStr != "" {
		fmt.Sscanf(nStr, "%d", &maxResults)
	}
	results, err := s.memoryAPI.Research(query, maxResults)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(results) //nolint:errcheck
}

// handlePipelines handles GET /api/pipelines (list) and POST /api/pipelines (start).
func (s *Server) handlePipelines(w http.ResponseWriter, r *http.Request) {
	if s.pipelineExec == nil {
		http.Error(w, "pipelines not available", http.StatusServiceUnavailable)
		return
	}
	switch r.Method {
	case http.MethodGet:
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(s.pipelineExec.ListJSON()) //nolint:errcheck
	case http.MethodPost:
		var req struct {
			Spec        string `json:"spec"`        // "task1 -> task2 -> task3"
			ProjectDir  string `json:"project_dir"`
			MaxParallel int    `json:"max_parallel"`
		}
		json.NewDecoder(r.Body).Decode(&req) //nolint:errcheck
		if req.Spec == "" {
			http.Error(w, "missing spec", http.StatusBadRequest)
			return
		}
		id, err := s.pipelineExec.StartPipeline(req.Spec, req.ProjectDir, nil, req.MaxParallel)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok", "id": id}) //nolint:errcheck
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handlePipelineAction handles POST /api/pipeline/cancel and GET /api/pipeline/status.
func (s *Server) handlePipelineAction(w http.ResponseWriter, r *http.Request) {
	if s.pipelineExec == nil {
		http.Error(w, "pipelines not available", http.StatusServiceUnavailable)
		return
	}
	id := r.URL.Query().Get("id")
	action := r.URL.Query().Get("action")
	if id == "" {
		http.Error(w, "missing id", http.StatusBadRequest)
		return
	}
	switch action {
	case "cancel":
		if err := s.pipelineExec.Cancel(id); err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"}) //nolint:errcheck
	default:
		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprint(w, s.pipelineExec.GetStatus(id))
	}
}

func (s *Server) handleMemoryExport(w http.ResponseWriter, r *http.Request) {
	if s.memoryAPI == nil {
		http.Error(w, "memory not enabled", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", "attachment; filename=datawatch-memories.json")
	s.memoryAPI.Export(w) //nolint:errcheck
}

func (s *Server) handleMemoryImport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.memoryAPI == nil {
		http.Error(w, "memory not enabled", http.StatusServiceUnavailable)
		return
	}
	count, err := s.memoryAPI.Import(r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"status": "ok", "imported": count}) //nolint:errcheck
}

func (s *Server) handleMemoryWAL(w http.ResponseWriter, r *http.Request) {
	if s.memoryAPI == nil {
		http.Error(w, "memory not enabled", http.StatusServiceUnavailable)
		return
	}
	n := 50
	if ns := r.URL.Query().Get("n"); ns != "" {
		fmt.Sscanf(ns, "%d", &n) //nolint:errcheck
	}
	entries := s.memoryAPI.WALRecent(n)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(entries) //nolint:errcheck
}

// ── Knowledge Graph API endpoints ────────────────────────────────────────────

func (s *Server) handleKGQuery(w http.ResponseWriter, r *http.Request) {
	if s.kgAPI == nil { http.Error(w, "KG not enabled", http.StatusServiceUnavailable); return }
	entity := r.URL.Query().Get("entity")
	asOf := r.URL.Query().Get("as_of")
	if entity == "" { http.Error(w, "missing entity param", http.StatusBadRequest); return }
	results, err := s.kgAPI.QueryEntity(entity, asOf)
	if err != nil { http.Error(w, err.Error(), http.StatusInternalServerError); return }
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(results) //nolint:errcheck
}

func (s *Server) handleKGAdd(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost { http.Error(w, "method not allowed", http.StatusMethodNotAllowed); return }
	if s.kgAPI == nil { http.Error(w, "KG not enabled", http.StatusServiceUnavailable); return }
	var req struct {
		Subject   string `json:"subject"`
		Predicate string `json:"predicate"`
		Object    string `json:"object"`
		ValidFrom string `json:"valid_from"`
		Source    string `json:"source"`
	}
	json.NewDecoder(r.Body).Decode(&req) //nolint:errcheck
	if req.Subject == "" || req.Predicate == "" || req.Object == "" {
		http.Error(w, "subject, predicate, object required", http.StatusBadRequest); return
	}
	id, err := s.kgAPI.AddTriple(req.Subject, req.Predicate, req.Object, req.ValidFrom, req.Source)
	if err != nil { http.Error(w, err.Error(), http.StatusInternalServerError); return }
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"id": id}) //nolint:errcheck
}

func (s *Server) handleKGInvalidate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost { http.Error(w, "method not allowed", http.StatusMethodNotAllowed); return }
	if s.kgAPI == nil { http.Error(w, "KG not enabled", http.StatusServiceUnavailable); return }
	var req struct {
		Subject   string `json:"subject"`
		Predicate string `json:"predicate"`
		Object    string `json:"object"`
		Ended     string `json:"ended"`
	}
	json.NewDecoder(r.Body).Decode(&req) //nolint:errcheck
	if err := s.kgAPI.Invalidate(req.Subject, req.Predicate, req.Object, req.Ended); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError); return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"}) //nolint:errcheck
}

func (s *Server) handleKGTimeline(w http.ResponseWriter, r *http.Request) {
	if s.kgAPI == nil { http.Error(w, "KG not enabled", http.StatusServiceUnavailable); return }
	entity := r.URL.Query().Get("entity")
	if entity == "" { http.Error(w, "missing entity param", http.StatusBadRequest); return }
	results, err := s.kgAPI.Timeline(entity)
	if err != nil { http.Error(w, err.Error(), http.StatusInternalServerError); return }
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(results) //nolint:errcheck
}

func (s *Server) handleKGStats(w http.ResponseWriter, r *http.Request) {
	if s.kgAPI == nil { json.NewEncoder(w).Encode(map[string]bool{"enabled": false}); return } //nolint:errcheck
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(s.kgAPI.Stats()) //nolint:errcheck
}

// handleMemoryTest tests Ollama connectivity and embedding capability before enabling memory.
func (s *Server) handleMemoryTest(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// Determine host and model from config
	host := s.cfg.Memory.EmbedderHost
	if host == "" {
		host = s.cfg.Ollama.Host
	}
	if host == "" {
		host = "http://localhost:11434"
	}
	model := s.cfg.Memory.EmbedderModel
	if model == "" {
		model = "nomic-embed-text"
	}
	embedder := s.cfg.Memory.EffectiveEmbedder()

	if embedder == "openai" {
		if s.cfg.Memory.OpenAIKey == "" {
			json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "error": "OpenAI API key not configured"}) //nolint:errcheck
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "embedder": "openai", "model": model, "note": "OpenAI key configured (not tested live)"}) //nolint:errcheck
		return
	}

	// Test Ollama: connect + embedding
	// Import-safe: use the test function via the memoryTestFn field
	if s.memoryTestFn == nil {
		json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "error": "memory test not wired"}) //nolint:errcheck
		return
	}
	dims, err := s.memoryTestFn(host, model)
	if err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   err.Error(),
			"host":    host,
			"model":   model,
		}) //nolint:errcheck
		return
	}
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":    true,
		"embedder":   "ollama",
		"host":       host,
		"model":      model,
		"dimensions": dims,
	}) //nolint:errcheck
}

// handleOllamaStats returns current Ollama server statistics.
func (s *Server) handleOllamaStats(w http.ResponseWriter, r *http.Request) {
	host := s.cfg.Ollama.Host
	if host == "" {
		json.NewEncoder(w).Encode(map[string]bool{"available": false}) //nolint:errcheck
		return
	}
	olStats := stats.FetchOllamaStats(host)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(olStats) //nolint:errcheck
}

// handleSessionResponse returns the last captured LLM response for a session.
// GET /api/sessions/response?id=<session_id>
func (s *Server) handleSessionResponse(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id := r.URL.Query().Get("id")
	if id == "" {
		http.Error(w, "missing id parameter", http.StatusBadRequest)
		return
	}
	resp := s.manager.GetLastResponse(id)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"response": resp, "session_id": id}) //nolint:errcheck
}

// handleSessionPrompt returns the last user prompt for a session.
func (s *Server) handleSessionPrompt(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id := r.URL.Query().Get("id")
	if id == "" {
		http.Error(w, "missing id parameter", http.StatusBadRequest)
		return
	}
	sess, ok := s.manager.GetSession(id)
	if !ok {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"prompt": sess.LastInput, "session_id": id}) //nolint:errcheck
}

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

// handleBindSessionAgent binds (or rebinds / unbinds) a session to a
// parent-spawned worker agent (F10 sprint 3.6). After binding,
// session API calls forward through /api/proxy/agent/{agent_id}/...
// rather than touching the local tmux. Pass agent_id="" to unbind.
//
// POST /api/sessions/bind {"id":"<session>","agent_id":"<agent>"}
func (s *Server) handleBindSessionAgent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		ID      string `json:"id"`
		AgentID string `json:"agent_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if req.ID == "" {
		http.Error(w, "id required", http.StatusBadRequest)
		return
	}
	// Validate the agent exists when binding (allow empty for unbind).
	if req.AgentID != "" {
		if s.agentMgr == nil {
			http.Error(w, "agent manager not available", http.StatusServiceUnavailable)
			return
		}
		if a := s.agentMgr.Get(req.AgentID); a == nil {
			http.Error(w, fmt.Sprintf("agent %q not found", req.AgentID), http.StatusNotFound)
			return
		}
	}
	if err := s.manager.SetAgentBinding(req.ID, req.AgentID); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	if req.AgentID != "" {
		// Best-effort: tell the agent manager too. Idempotent.
		_ = s.agentMgr.MarkSessionBound(req.AgentID, req.ID)
	}
	if s.hub != nil {
		go s.hub.BroadcastSessions(s.manager.ListSessions())
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok", "agent_id": req.AgentID}) //nolint:errcheck
}

// handleSetSessionState allows manual override of a session's state.
// POST /api/sessions/state {"id":"...","state":"running|waiting_input|complete|killed"}
func (s *Server) handleSetSessionState(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		ID    string `json:"id"`
		State string `json:"state"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	validStates := map[string]bool{"running": true, "waiting_input": true, "complete": true, "killed": true, "failed": true, "rate_limited": true}
	if !validStates[req.State] {
		http.Error(w, "invalid state", http.StatusBadRequest)
		return
	}
	if err := s.manager.SetState(req.ID, session.State(req.State)); err != nil {
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
		Profile       string `json:"profile"`
		AutoGitCommit *bool  `json:"auto_git_commit,omitempty"`
		AutoGitInit   *bool  `json:"auto_git_init,omitempty"`
		Effort        string `json:"effort,omitempty"`   // BL41
		Template      string `json:"template,omitempty"` // BL5
		Project       string `json:"project,omitempty"`  // BL27
		// v5.26.21 — autonomous PRDs with project_profile but no
		// cluster_profile fall through here. Resolve the profile to a
		// git URL + branch, clone into a per-session workspace, and
		// use that as the worker's project_dir. Cloned with whatever
		// auth the daemon's user has locally (SSH agent or git
		// credential helper); F10 BL113 token-broker integration is a
		// follow-up.
		ProjectProfile string `json:"project_profile,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	// BL5 — apply template defaults BEFORE per-request overrides.
	if req.Template != "" {
		tmpl, ok := s.cfg.Templates[req.Template]
		if !ok {
			http.Error(w, "template not found: "+req.Template, http.StatusBadRequest)
			return
		}
		if req.ProjectDir == "" {
			req.ProjectDir = tmpl.ProjectDir
		}
		if req.Backend == "" {
			req.Backend = tmpl.Backend
		}
		if req.Profile == "" {
			req.Profile = tmpl.Profile
		}
		if req.Effort == "" {
			req.Effort = tmpl.Effort
		}
		if req.AutoGitCommit == nil {
			req.AutoGitCommit = tmpl.AutoGitCommit
		}
		if req.AutoGitInit == nil {
			req.AutoGitInit = tmpl.AutoGitInit
		}
	}
	// BL20 — routing rules: pattern→backend before any other resolution
	// step. A rule match overrides the request's req.Backend; the
	// operator can disable that by removing the rule.
	if req.Backend == "" && s.cfg != nil && len(s.cfg.Session.RoutingRules) > 0 {
		if rule := MatchRoutingRule(s.cfg.Session.RoutingRules, req.Task); rule != nil {
			req.Backend = rule.Backend
		}
	}
	// BL27 — resolve named project alias if requested.
	if req.Project != "" {
		proj, ok := s.cfg.Projects[req.Project]
		if !ok {
			http.Error(w, "project not found: "+req.Project, http.StatusBadRequest)
			return
		}
		if req.ProjectDir == "" {
			req.ProjectDir = proj.Dir
		}
		if req.Backend == "" {
			req.Backend = proj.DefaultBackend
		}
	}
	// v5.26.21 — F10 project profile clone-then-use. Triggered when
	// the autonomous executor passes project_profile (no
	// cluster_profile, otherwise the F10 agent path handles it). We
	// shell out to git clone into a per-PRD workspace inside the
	// daemon's data dir; the worker session's project_dir becomes
	// the clone target. Errors short-circuit with a 400 so the
	// autonomous executor sees the failure on the spawn round-trip
	// instead of running against the wrong directory.
	// v5.26.26 — flag passed to Manager.Start so Delete reaps the
	// daemon-owned workspace tree once the session is gone. Set true
	// only when the clone path below actually creates ProjectDir.
	var ephemeralWorkspace bool
	if req.ProjectProfile != "" && req.ProjectDir == "" {
		if s.projectStore == nil {
			http.Error(w, "project_profile requires the daemon's profile subsystem; not wired", http.StatusBadRequest)
			return
		}
		prof, err := s.projectStore.Get(req.ProjectProfile)
		if err != nil || prof == nil {
			http.Error(w, "project profile "+req.ProjectProfile+" not found: "+fmt.Sprint(err), http.StatusBadRequest)
			return
		}
		if prof.Git.URL == "" {
			http.Error(w, "project profile "+req.ProjectProfile+" has no git.url to clone from", http.StatusBadRequest)
			return
		}
		// Clone target: <data_dir>/workspaces/<profile>-<8char-random>/
		// Random suffix prevents collision when the same profile is
		// reused across simultaneous PRD spawns.
		dataDir := os.Getenv("DATAWATCH_DATA_DIR")
		if dataDir == "" {
			home, _ := os.UserHomeDir()
			dataDir = filepath.Join(home, ".datawatch")
		}
		cloneRoot := filepath.Join(dataDir, "workspaces")
		if err := os.MkdirAll(cloneRoot, 0o755); err != nil {
			http.Error(w, "create workspace root: "+err.Error(), http.StatusInternalServerError)
			return
		}
		buf := make([]byte, 4)
		_, _ = rand.Read(buf)
		suffix := hex.EncodeToString(buf)
		clonePath := filepath.Join(cloneRoot, prof.Name+"-"+suffix)
		args := []string{"clone", "--depth", "1"}
		if prof.Git.Branch != "" {
			args = append(args, "--branch", prof.Git.Branch)
		}
		// v5.26.22 — k8s-friendly auth abstraction. When the daemon
		// runs in a Pod, the Helm chart injects DATAWATCH_GIT_TOKEN
		// from a Secret (gitToken.existingSecret). For HTTPS URLs
		// without an embedded token, rewrite to use it; SSH URLs
		// continue to use the daemon user's SSH agent / mounted key
		// (no rewrite needed). The daemon's local case (no Pod) is
		// unchanged: no env var → no rewrite → git uses local
		// credential helper.
		//
		// v5.26.24 — BL113 token broker takes priority when wired.
		// Mints a 5-minute token scoped to the repo, revokes after
		// clone (success OR failure). No long-lived secret in env.
		// Falls back to env-token / local-creds when broker isn't
		// wired or minting fails (operator gets a clear log line).
		cloneURL := prof.Git.URL
		var brokerWorkerID string
		if s.gitMinter != nil && strings.HasPrefix(strings.ToLower(cloneURL), "http") {
			// Use a workerID prefix that's clearly broker-clone
			// scope and guaranteed unique per request.
			brokerWorkerID = "clone:" + suffix
			repo := repoFromGitURL(prof.Git.URL)
			tok, mErr := s.gitMinter.MintForWorker(r.Context(), brokerWorkerID, repo, 5*time.Minute)
			if mErr == nil && tok != "" {
				cloneURL = injectGitToken(cloneURL, tok)
				defer func() {
					_ = s.gitMinter.RevokeForWorker(context.Background(), brokerWorkerID)
				}()
			} else if mErr != nil {
				// Broker mint failed — fall back to env / local. Log
				// for operator visibility but don't fail the clone.
				fmt.Fprintf(os.Stderr, "[clone] broker mint failed for %s: %v (falling back to env)\n", repo, mErr)
				brokerWorkerID = "" // skip the deferred revoke
			}
		}
		if brokerWorkerID == "" {
			// Broker not wired or mint-fail — env / local-creds fallback.
			if tok := os.Getenv("DATAWATCH_GIT_TOKEN"); tok != "" {
				cloneURL = injectGitToken(cloneURL, tok)
			}
		}
		args = append(args, cloneURL, clonePath)
		// Use a context with a generous timeout — large repos take time.
		cloneCtx, cloneCancel := context.WithTimeout(r.Context(), 5*time.Minute)
		defer cloneCancel()
		out, gerr := exec.CommandContext(cloneCtx, "git", args...).CombinedOutput() // #nosec G702 -- argv list, not shell
		if gerr != nil {
			// Redact any token we injected before surfacing the error
			// — git failures sometimes echo the URL.
			redacted := redactGitToken(string(out), prof.Git.URL)
			http.Error(w, fmt.Sprintf("git clone %s failed: %v\n%s", prof.Git.URL, gerr, redacted), http.StatusBadGateway)
			return
		}
		req.ProjectDir = clonePath
		ephemeralWorkspace = true
	}
	// Default project dir to home directory when not specified
	if req.ProjectDir == "" {
		req.ProjectDir, _ = os.UserHomeDir()
	}
	// BL41 — validate Effort if supplied (empty = manager default).
	if req.Effort != "" && !session.IsValidEffort(req.Effort) {
		http.Error(w, "invalid effort: must be one of quick, normal, thorough", http.StatusBadRequest)
		return
	}

	opts := &session.StartOptions{
		Name:               req.Name,
		Backend:            req.Backend,
		ResumeID:           req.ResumeID,
		AutoGitCommit:      req.AutoGitCommit,
		AutoGitInit:        req.AutoGitInit,
		Effort:             req.Effort,
		EphemeralWorkspace: ephemeralWorkspace,
	}
	// BL5 — propagate template env vars (request takes precedence
	// only if a profile already set them; templates fill the gap).
	if req.Template != "" {
		if tmpl, ok := s.cfg.Templates[req.Template]; ok && len(tmpl.Env) > 0 && opts.Env == nil {
			opts.Env = tmpl.Env
		}
	}
	// Apply profile overrides if specified
	if req.Profile != "" && s.cfg.Profiles != nil {
		if profile, ok := s.cfg.Profiles[req.Profile]; ok {
			if profile.Backend != "" {
				opts.Backend = profile.Backend
			}
			opts.Env = profile.Env
		}
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

// handleRestartSession restarts a completed/failed/killed session in-place,
// reusing the same session ID and resuming the LLM conversation.
func (s *Server) handleRestartSession(w http.ResponseWriter, r *http.Request) {
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
	sess, err := s.manager.Restart(context.Background(), req.ID)
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
			"enabled":          s.cfg.Server.Enabled,
			"host":             s.cfg.Server.Host,
			"port":             s.cfg.Server.Port,
			"public_url":       s.cfg.Server.PublicURL,
			"token":            mask(s.cfg.Server.Token),
			"tls":              s.cfg.Server.TLSEnabled,
			"tls_auto_generate": s.cfg.Server.TLSAutoGenerate,
			"tls_cert":         s.cfg.Server.TLSCert,
			"tls_key":          s.cfg.Server.TLSKey,
			"channel_port":              s.cfg.Server.ChannelPort,
			"tls_port":                  s.cfg.Server.TLSPort,
			"auto_restart_on_config":    s.cfg.Server.AutoRestartOnConfig,
			"recent_session_minutes":    s.cfg.Server.RecentSessionMinutes,
			"suppress_active_toasts":    s.cfg.Server.SuppressActiveToasts,
		},
		"signal": map[string]interface{}{
			"enabled":        s.cfg.Signal.AccountNumber != "",
			"account_number": s.cfg.Signal.AccountNumber,
			"group_id":       s.cfg.Signal.GroupID,
			"config_dir":     s.cfg.Signal.ConfigDir,
			"device_name":    s.cfg.Signal.DeviceName,
		},
		"telegram": map[string]interface{}{
			"enabled":            s.cfg.Telegram.Enabled,
			"token":              mask(s.cfg.Telegram.Token),
			"chat_id":            s.cfg.Telegram.ChatID,
			"auto_manage_group":  s.cfg.Telegram.AutoManageGroup,
		},
		"discord": map[string]interface{}{
			"enabled":              s.cfg.Discord.Enabled,
			"token":                mask(s.cfg.Discord.Token),
			"channel_id":           s.cfg.Discord.ChannelID,
			"auto_manage_channel":  s.cfg.Discord.AutoManageChannel,
		},
		"slack": map[string]interface{}{
			"enabled":              s.cfg.Slack.Enabled,
			"token":                mask(s.cfg.Slack.Token),
			"channel_id":           s.cfg.Slack.ChannelID,
			"auto_manage_channel":  s.cfg.Slack.AutoManageChannel,
		},
		"matrix": map[string]interface{}{
			"enabled":           s.cfg.Matrix.Enabled,
			"homeserver":        s.cfg.Matrix.Homeserver,
			"user_id":           s.cfg.Matrix.UserID,
			"access_token":      mask(s.cfg.Matrix.AccessToken),
			"room_id":           s.cfg.Matrix.RoomID,
			"auto_manage_room":  s.cfg.Matrix.AutoManageRoom,
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
				"alert_context_lines": s.cfg.Session.AlertContextLines,
			"default_project_dir": s.cfg.Session.DefaultProjectDir,
			"workspace_root":     s.cfg.Session.WorkspaceRoot,
			"claude_enabled":     s.cfg.Session.ClaudeEnabled,
			"skip_permissions":   s.cfg.Session.ClaudeSkipPermissions,
			"channel_enabled":    s.cfg.Session.ClaudeChannelEnabled,
			"auto_git_commit":    s.cfg.Session.AutoGitCommit,
			"auto_git_init":      s.cfg.Session.AutoGitInit,
			"kill_sessions_on_exit": s.cfg.Session.KillSessionsOnExit,
			"root_path":         s.cfg.Session.RootPath,
			"mcp_max_retries":   s.cfg.Session.MCPMaxRetries,
			"schedule_settle_ms": s.cfg.Session.ScheduleSettleMs,
			"default_effort":    s.cfg.Session.DefaultEffort,
			"console_cols":      s.cfg.Session.ConsoleCols,
			"console_rows":      s.cfg.Session.ConsoleRows,
			"log_level":         s.cfg.Session.LogLevel,
		},
		"mcp": map[string]interface{}{
			"enabled":          s.cfg.MCP.Enabled,
			"sse_enabled":      s.cfg.MCP.SSEEnabled,
			"sse_host":         s.cfg.MCP.SSEHost,
			"sse_port":         s.cfg.MCP.SSEPort,
			"token":            mask(s.cfg.MCP.Token),
			"tls_enabled":      s.cfg.MCP.TLSEnabled,
			"tls_auto_generate": s.cfg.MCP.TLSAutoGenerate,
			"tls_cert":         s.cfg.MCP.TLSCert,
			"tls_key":          s.cfg.MCP.TLSKey,
		},
		"detection": map[string]interface{}{
			"prompt_patterns":       s.cfg.Detection.PromptPatterns,
			"completion_patterns":   s.cfg.Detection.CompletionPatterns,
			"rate_limit_patterns":   s.cfg.Detection.RateLimitPatterns,
			"input_needed_patterns": s.cfg.Detection.InputNeededPatterns,
			"prompt_debounce":       s.cfg.Detection.PromptDebounce,
			"notify_cooldown":       s.cfg.Detection.NotifyCooldown,
		},
		"update": map[string]interface{}{
			"enabled":     s.cfg.Update.Enabled,
			"schedule":    s.cfg.Update.Schedule,
			"time_of_day": s.cfg.Update.TimeOfDay,
		},
		"dns_channel": map[string]interface{}{
			"enabled":           s.cfg.DNSChannel.Enabled,
			"mode":              s.cfg.DNSChannel.Mode,
			"domain":            s.cfg.DNSChannel.Domain,
			"listen":            s.cfg.DNSChannel.Listen,
			"upstream":          s.cfg.DNSChannel.Upstream,
			"secret":            mask(s.cfg.DNSChannel.Secret),
			"ttl":               s.cfg.DNSChannel.TTL,
			"max_response_size": s.cfg.DNSChannel.MaxResponseSize,
			"poll_interval":     s.cfg.DNSChannel.PollInterval,
			"rate_limit":        s.cfg.DNSChannel.RateLimit,
		},
		"ollama": map[string]interface{}{
			"enabled":      s.cfg.Ollama.Enabled,
			"model":        s.cfg.Ollama.Model,
			"host":         s.cfg.Ollama.Host,
			"console_cols": s.cfg.Ollama.ConsoleCols,
			"console_rows": s.cfg.Ollama.ConsoleRows,
			"output_mode":  s.cfg.Ollama.OutputMode,
			"input_mode":   s.cfg.Ollama.InputMode,
		},
		"opencode": map[string]interface{}{
			"enabled":      s.cfg.OpenCode.Enabled,
			"binary":       s.cfg.OpenCode.Binary,
			"console_cols": s.cfg.OpenCode.ConsoleCols,
			"console_rows": s.cfg.OpenCode.ConsoleRows,
			"output_mode":  s.cfg.OpenCode.OutputMode,
			"input_mode":   s.cfg.OpenCode.InputMode,
		},
		"opencode_acp": map[string]interface{}{
			"enabled":             s.cfg.OpenCodeACP.Enabled,
			"binary":              s.cfg.OpenCodeACP.Binary,
			"acp_startup_timeout": s.cfg.OpenCodeACP.ACPStartupTimeout,
			"acp_health_interval": s.cfg.OpenCodeACP.ACPHealthInterval,
			"acp_message_timeout": s.cfg.OpenCodeACP.ACPMessageTimeout,
			"console_cols":        s.cfg.OpenCodeACP.ConsoleCols,
			"console_rows":        s.cfg.OpenCodeACP.ConsoleRows,
			"output_mode":         s.cfg.OpenCodeACP.OutputMode,
			"input_mode":          s.cfg.OpenCodeACP.InputMode,
		},
		"opencode_prompt": map[string]interface{}{
			"enabled":      s.cfg.OpenCodePrompt.Enabled,
			"binary":       s.cfg.OpenCodePrompt.Binary,
			"console_cols": s.cfg.OpenCodePrompt.ConsoleCols,
			"console_rows": s.cfg.OpenCodePrompt.ConsoleRows,
			"output_mode":  s.cfg.OpenCodePrompt.OutputMode,
			"input_mode":   s.cfg.OpenCodePrompt.InputMode,
		},
		"aider": map[string]interface{}{
			"enabled": s.cfg.Aider.Enabled,
			"binary":  s.cfg.Aider.Binary,
			"console_cols": s.cfg.Aider.ConsoleCols,
			"console_rows": s.cfg.Aider.ConsoleRows,
			"output_mode":  s.cfg.Aider.OutputMode,
			"input_mode":   s.cfg.Aider.InputMode,
		},
		"goose": map[string]interface{}{
			"enabled": s.cfg.Goose.Enabled,
			"binary":  s.cfg.Goose.Binary,
			"console_cols": s.cfg.Goose.ConsoleCols,
			"console_rows": s.cfg.Goose.ConsoleRows,
			"output_mode":  s.cfg.Goose.OutputMode,
			"input_mode":   s.cfg.Goose.InputMode,
		},
		"gemini": map[string]interface{}{
			"enabled": s.cfg.Gemini.Enabled,
			"binary":  s.cfg.Gemini.Binary,
			"console_cols": s.cfg.Gemini.ConsoleCols,
			"console_rows": s.cfg.Gemini.ConsoleRows,
			"output_mode":  s.cfg.Gemini.OutputMode,
			"input_mode":   s.cfg.Gemini.InputMode,
		},
		"openwebui": map[string]interface{}{
			"enabled": s.cfg.OpenWebUI.Enabled,
			"url":     s.cfg.OpenWebUI.URL,
			"model":   s.cfg.OpenWebUI.Model,
			"api_key": mask(s.cfg.OpenWebUI.APIKey),
			"console_cols": s.cfg.OpenWebUI.ConsoleCols,
			"console_rows": s.cfg.OpenWebUI.ConsoleRows,
			"output_mode":  s.cfg.OpenWebUI.OutputMode,
			"input_mode":   s.cfg.OpenWebUI.InputMode,
		},
		"shell_backend": map[string]interface{}{
			"enabled":     s.cfg.Shell.Enabled,
			"script_path": s.cfg.Shell.ScriptPath,
			"console_cols": s.cfg.Shell.ConsoleCols,
			"console_rows": s.cfg.Shell.ConsoleRows,
			"output_mode":  s.cfg.Shell.OutputMode,
			"input_mode":   s.cfg.Shell.InputMode,
		},
		"rtk": map[string]interface{}{
			"enabled":            s.cfg.RTK.Enabled,
			"binary":             s.cfg.RTK.Binary,
			"show_savings":       s.cfg.RTK.ShowSavings,
			"auto_init":          s.cfg.RTK.AutoInit,
			"discover_interval":       s.cfg.RTK.DiscoverInterval,
			"auto_update":             s.cfg.RTK.AutoUpdate,
			"update_check_interval":   s.cfg.RTK.UpdateCheckInterval,
		},
		"pipeline": map[string]interface{}{
			"max_parallel":    s.cfg.Pipeline.MaxParallel,
			"default_backend": s.cfg.Pipeline.DefaultBackend,
		},
		// v4.0.8 (B38) — autonomous / plugins / orchestrator must be
		// in the GET response too. Without them the PWA and mobile
		// Settings cards render empty fields on reload even though
		// the PUT path now persists correctly.
		"autonomous": map[string]interface{}{
			"enabled":                s.cfg.Autonomous.Enabled,
			"poll_interval_seconds":  s.cfg.Autonomous.PollIntervalSeconds,
			"max_parallel_tasks":     s.cfg.Autonomous.MaxParallelTasks,
			"decomposition_backend":  s.cfg.Autonomous.DecompositionBackend,
			"verification_backend":   s.cfg.Autonomous.VerificationBackend,
			"decomposition_effort":   s.cfg.Autonomous.DecompositionEffort,
			"verification_effort":    s.cfg.Autonomous.VerificationEffort,
			"stale_task_seconds":     s.cfg.Autonomous.StaleTaskSeconds,
			"auto_fix_retries":       s.cfg.Autonomous.AutoFixRetries,
			"security_scan":          s.cfg.Autonomous.SecurityScan,
		},
		"plugins": map[string]interface{}{
			"enabled":    s.cfg.Plugins.Enabled,
			"dir":        s.cfg.Plugins.Dir,
			"timeout_ms": s.cfg.Plugins.TimeoutMs,
			"disabled":   s.cfg.Plugins.Disabled,
		},
		"orchestrator": map[string]interface{}{
			"enabled":               s.cfg.Orchestrator.Enabled,
			"default_guardrails":    s.cfg.Orchestrator.DefaultGuardrails,
			"guardrail_timeout_ms":  s.cfg.Orchestrator.GuardrailTimeoutMs,
			"guardrail_backend":     s.cfg.Orchestrator.GuardrailBackend,
			"max_parallel_prds":     s.cfg.Orchestrator.MaxParallelPRDs,
		},
		"profiles":       s.cfg.Profiles,
		"fallback_chain": s.cfg.Session.FallbackChain,
		"whisper": map[string]interface{}{
			"enabled":   s.cfg.Whisper.Enabled,
			"model":     s.cfg.Whisper.Model,
			"language":  s.cfg.Whisper.Language,
			"venv_path": s.cfg.Whisper.VenvPath,
		},
		"memory": map[string]interface{}{
			"enabled":          s.cfg.Memory.Enabled,
			"backend":         s.cfg.Memory.Backend,
			"db_path":         s.cfg.Memory.DBPath,
			"postgres_url":    mask(s.cfg.Memory.PostgresURL),
			"fallback_sqlite": s.cfg.Memory.FallbackSQLite,
			"embedder":        s.cfg.Memory.Embedder,
			"embedder_model":  s.cfg.Memory.EmbedderModel,
			"embedder_host":   s.cfg.Memory.EmbedderHost,
			"openai_key":      mask(s.cfg.Memory.OpenAIKey),
			"dimensions":      s.cfg.Memory.Dimensions,
			"top_k":           s.cfg.Memory.TopK,
			"auto_save":       s.cfg.Memory.IsAutoSave(),
			"learnings_enabled": s.cfg.Memory.IsLearningsEnabled(),
			"retention_days":  s.cfg.Memory.RetentionDays,
			"storage_mode":    s.cfg.Memory.StorageMode,
			"entity_detection":    s.cfg.Memory.EntityDetection,
			"auto_hooks":          s.cfg.Memory.IsAutoHooks(),
			"hook_save_interval":  s.cfg.Memory.EffectiveHookInterval(),
			"session_awareness":   s.cfg.Memory.IsSessionAwareness(),
			"session_broadcast":   s.cfg.Memory.IsSessionBroadcast(),
		},
		"proxy": map[string]interface{}{
			"enabled":                    s.cfg.Proxy.Enabled,
			"health_interval":            s.cfg.Proxy.HealthInterval,
			"request_timeout":            s.cfg.Proxy.RequestTimeout,
			"offline_queue_size":         s.cfg.Proxy.OfflineQueueSize,
			"circuit_breaker_threshold":  s.cfg.Proxy.CircuitBreakerThreshold,
			"circuit_breaker_reset":      s.cfg.Proxy.CircuitBreakerReset,
		},
		// F10 sprint 3: agent layer configuration.
		"agents": map[string]interface{}{
			"image_prefix":                       s.cfg.Agents.ImagePrefix,
			"image_tag":                          s.cfg.Agents.ImageTag,
			"docker_bin":                         s.cfg.Agents.DockerBin,
			"kubectl_bin":                        s.cfg.Agents.KubectlBin,
			"callback_url":                       s.cfg.Agents.CallbackURL,
			"bootstrap_token_ttl_seconds":        s.cfg.Agents.BootstrapTokenTTLSeconds,
			"worker_bootstrap_deadline_seconds":  s.cfg.Agents.WorkerBootstrapDeadlineSeconds,
		},
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(out) //nolint:errcheck
}

// saveConfig persists the current config to disk.
func (s *Server) saveConfig() error {
	if s.cfg == nil || s.cfgPath == "" {
		return fmt.Errorf("config not available")
	}
	return config.Save(s.cfg, s.cfgPath)
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
	// B30 + BL41: apply hot-reloadable session knobs to live manager.
	if s.manager != nil {
		s.manager.SetScheduleSettleMs(s.cfg.Session.ScheduleSettleMs)
		s.manager.SetDefaultEffort(s.cfg.Session.DefaultEffort)
	}
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
		case "session.claude_enabled":
			cfg.Session.ClaudeEnabled = toBool(v)
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
		case "session.alert_context_lines":
			if n, ok := toInt(v); ok {
				cfg.Session.AlertContextLines = n
			}
		case "session.default_project_dir":
			if s := toString(v); s != "" {
				cfg.Session.DefaultProjectDir = s
			}
		case "session.workspace_root":
			// F10: container/PVC base for relative project_dirs.
			// Empty string is a valid value (disables the rewrite),
			// so don't gate on non-empty here.
			cfg.Session.WorkspaceRoot = toString(v)
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
		case "session.schedule_settle_ms":
			if n, ok := toInt(v); ok {
				cfg.Session.ScheduleSettleMs = n
			}
		case "session.default_effort":
			if s := toString(v); s != "" {
				cfg.Session.DefaultEffort = s
			}
		case "session.console_cols":
			if n, ok := toInt(v); ok { cfg.Session.ConsoleCols = n }
		case "session.console_rows":
			if n, ok := toInt(v); ok { cfg.Session.ConsoleRows = n }
		case "server.host":
			if s := toString(v); s != "" {
				cfg.Server.Host = s
			}
		case "server.port":
			if n, ok := toInt(v); ok {
				cfg.Server.Port = n
			}
		case "server.public_url":
			cfg.Server.PublicURL = toString(v)
		case "server.tls":
			cfg.Server.TLSEnabled = toBool(v)
		case "server.token":
			if s := toString(v); s != "" { cfg.Server.Token = s }
		case "server.tls_auto_generate":
			cfg.Server.TLSAutoGenerate = toBool(v)
		case "server.tls_cert":
			cfg.Server.TLSCert = toString(v)
		case "server.tls_key":
			cfg.Server.TLSKey = toString(v)
		case "server.channel_port":
			if n, ok := toInt(v); ok { cfg.Server.ChannelPort = n }
		case "server.tls_port":
			if n, ok := toInt(v); ok { cfg.Server.TLSPort = n }
		case "server.auto_restart_on_config":
			cfg.Server.AutoRestartOnConfig = toBool(v)
		case "server.recent_session_minutes":
			if n, ok := toInt(v); ok { cfg.Server.RecentSessionMinutes = n }
		case "server.suppress_active_toasts":
			cfg.Server.SuppressActiveToasts = toBool(v)
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
		case "mcp.sse_enabled":
			cfg.MCP.SSEEnabled = toBool(v)
		case "mcp.token":
			if s := toString(v); s != "" { cfg.MCP.Token = s }
		case "mcp.tls_enabled":
			cfg.MCP.TLSEnabled = toBool(v)
		case "mcp.tls_auto_generate":
			cfg.MCP.TLSAutoGenerate = toBool(v)
		case "mcp.tls_cert":
			cfg.MCP.TLSCert = toString(v)
		case "mcp.tls_key":
			cfg.MCP.TLSKey = toString(v)
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
		// DNS channel config
		case "dns_channel.enabled":
			cfg.DNSChannel.Enabled = toBool(v)
		case "dns_channel.mode":
			if s := toString(v); s != "" { cfg.DNSChannel.Mode = s }
		case "dns_channel.domain":
			if s := toString(v); s != "" { cfg.DNSChannel.Domain = s }
		case "dns_channel.listen":
			if s := toString(v); s != "" { cfg.DNSChannel.Listen = s }
		case "dns_channel.upstream":
			if s := toString(v); s != "" { cfg.DNSChannel.Upstream = s }
		case "dns_channel.secret":
			if s := toString(v); s != "" { cfg.DNSChannel.Secret = s }
		case "dns_channel.ttl":
			if n, ok := toInt(v); ok { cfg.DNSChannel.TTL = n }
		case "dns_channel.max_response_size":
			if n, ok := toInt(v); ok { cfg.DNSChannel.MaxResponseSize = n }
		case "dns_channel.poll_interval":
			if s := toString(v); s != "" { cfg.DNSChannel.PollInterval = s }
		case "dns_channel.rate_limit":
			if n, ok := toInt(v); ok { cfg.DNSChannel.RateLimit = n }

		// Memory config
		case "memory.enabled":
			cfg.Memory.Enabled = toBool(v)
		case "memory.backend":
			if s := toString(v); s == "sqlite" || s == "postgres" { cfg.Memory.Backend = s }
		case "memory.db_path":
			cfg.Memory.DBPath = toString(v)
		case "memory.postgres_url":
			cfg.Memory.PostgresURL = toString(v)
		case "memory.fallback_sqlite":
			cfg.Memory.FallbackSQLite = toBool(v)
		case "memory.embedder":
			if s := toString(v); s == "ollama" || s == "openai" { cfg.Memory.Embedder = s }
		case "memory.embedder_model":
			if s := toString(v); s != "" { cfg.Memory.EmbedderModel = s }
		case "memory.embedder_host":
			cfg.Memory.EmbedderHost = toString(v)
		case "memory.openai_key":
			cfg.Memory.OpenAIKey = toString(v)
		case "memory.dimensions":
			if n, ok := toInt(v); ok && n >= 0 { cfg.Memory.Dimensions = n }
		case "memory.top_k":
			if n, ok := toInt(v); ok && n > 0 { cfg.Memory.TopK = n }
		case "memory.auto_save":
			val := toBool(v); cfg.Memory.AutoSave = &val
		case "memory.learnings_enabled":
			val := toBool(v); cfg.Memory.LearningsEnabled = &val
		case "memory.retention_days":
			if n, ok := toInt(v); ok && n >= 0 { cfg.Memory.RetentionDays = n }
		case "memory.session_awareness":
			val := toBool(v); cfg.Memory.SessionAwareness = &val
		case "memory.session_broadcast":
			val := toBool(v); cfg.Memory.SessionBroadcast = &val
		case "memory.auto_hooks":
			val := toBool(v); cfg.Memory.AutoHooks = &val
		case "memory.hook_save_interval":
			if n, ok := toInt(v); ok && n > 0 { cfg.Memory.HookSaveInterval = n }
		case "memory.storage_mode":
			if s := toString(v); s == "summary" || s == "verbatim" { cfg.Memory.StorageMode = s }
		case "memory.entity_detection":
			cfg.Memory.EntityDetection = toBool(v)

		// Proxy resilience config
		case "proxy.enabled":
			cfg.Proxy.Enabled = toBool(v)
		case "proxy.health_interval":
			if n, ok := toInt(v); ok && n > 0 { cfg.Proxy.HealthInterval = n }
		case "proxy.request_timeout":
			if n, ok := toInt(v); ok && n > 0 { cfg.Proxy.RequestTimeout = n }
		case "proxy.offline_queue_size":
			if n, ok := toInt(v); ok && n > 0 { cfg.Proxy.OfflineQueueSize = n }
		case "proxy.circuit_breaker_threshold":
			if n, ok := toInt(v); ok && n > 0 { cfg.Proxy.CircuitBreakerThreshold = n }
		case "proxy.circuit_breaker_reset":
			if n, ok := toInt(v); ok && n > 0 { cfg.Proxy.CircuitBreakerReset = n }

		// F10 sprint 3: agent layer config
		case "agents.image_prefix":
			cfg.Agents.ImagePrefix = toString(v)
		case "agents.image_tag":
			cfg.Agents.ImageTag = toString(v)
		case "agents.docker_bin":
			cfg.Agents.DockerBin = toString(v)
		case "agents.kubectl_bin":
			cfg.Agents.KubectlBin = toString(v)
		case "agents.callback_url":
			cfg.Agents.CallbackURL = toString(v)
		case "agents.bootstrap_token_ttl_seconds":
			if n, ok := toInt(v); ok && n >= 0 {
				cfg.Agents.BootstrapTokenTTLSeconds = n
			}
		case "agents.worker_bootstrap_deadline_seconds":
			if n, ok := toInt(v); ok && n >= 0 {
				cfg.Agents.WorkerBootstrapDeadlineSeconds = n
			}

		// Detection patterns
		case "detection.prompt_patterns":
			if arr, ok := toStringArray(v); ok { cfg.Detection.PromptPatterns = arr }
		case "detection.completion_patterns":
			if arr, ok := toStringArray(v); ok { cfg.Detection.CompletionPatterns = arr }
		case "detection.rate_limit_patterns":
			if arr, ok := toStringArray(v); ok { cfg.Detection.RateLimitPatterns = arr }
		case "detection.input_needed_patterns":
			if arr, ok := toStringArray(v); ok { cfg.Detection.InputNeededPatterns = arr }
		case "detection.prompt_debounce":
			if n, ok := toInt(v); ok && n >= 0 { cfg.Detection.PromptDebounce = n }
		case "detection.notify_cooldown":
			if n, ok := toInt(v); ok && n >= 0 { cfg.Detection.NotifyCooldown = n }

		// Signal config
		case "signal.config_dir":
			if s := toString(v); s != "" { cfg.Signal.ConfigDir = s }
		case "signal.device_name":
			if s := toString(v); s != "" { cfg.Signal.DeviceName = s }
		case "signal.group_id":
			cfg.Signal.GroupID = toString(v)

		// Auto-manage flags for messaging backends
		case "discord.auto_manage_channel":
			cfg.Discord.AutoManageChannel = toBool(v)
		case "slack.auto_manage_channel":
			cfg.Slack.AutoManageChannel = toBool(v)
		case "telegram.auto_manage_group":
			cfg.Telegram.AutoManageGroup = toBool(v)
		case "matrix.auto_manage_room":
			cfg.Matrix.AutoManageRoom = toBool(v)

		// Session log level
		case "session.log_level":
			cfg.Session.LogLevel = toString(v)

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
		case "opencode.binary":
			if s := toString(v); s != "" { cfg.OpenCode.Binary = s }
		case "opencode_acp.enabled":
			cfg.OpenCodeACP.Enabled = toBool(v)
		case "opencode_acp.binary":
			if s := toString(v); s != "" { cfg.OpenCodeACP.Binary = s }
		case "opencode_acp.acp_startup_timeout":
			if n, ok := toInt(v); ok { cfg.OpenCodeACP.ACPStartupTimeout = n }
		case "opencode_acp.acp_health_interval":
			if n, ok := toInt(v); ok { cfg.OpenCodeACP.ACPHealthInterval = n }
		case "opencode_acp.acp_message_timeout":
			if n, ok := toInt(v); ok { cfg.OpenCodeACP.ACPMessageTimeout = n }
		case "opencode_prompt.enabled":
			cfg.OpenCodePrompt.Enabled = toBool(v)
		case "opencode_prompt.binary":
			if s := toString(v); s != "" { cfg.OpenCodePrompt.Binary = s }
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

		// Per-LLM console size
		case "aider.console_cols":
			if n, ok := toInt(v); ok { cfg.Aider.ConsoleCols = n }
		case "aider.console_rows":
			if n, ok := toInt(v); ok { cfg.Aider.ConsoleRows = n }
		case "goose.console_cols":
			if n, ok := toInt(v); ok { cfg.Goose.ConsoleCols = n }
		case "goose.console_rows":
			if n, ok := toInt(v); ok { cfg.Goose.ConsoleRows = n }
		case "gemini.console_cols":
			if n, ok := toInt(v); ok { cfg.Gemini.ConsoleCols = n }
		case "gemini.console_rows":
			if n, ok := toInt(v); ok { cfg.Gemini.ConsoleRows = n }
		case "ollama.console_cols":
			if n, ok := toInt(v); ok { cfg.Ollama.ConsoleCols = n }
		case "ollama.console_rows":
			if n, ok := toInt(v); ok { cfg.Ollama.ConsoleRows = n }
		case "opencode.console_cols":
			if n, ok := toInt(v); ok { cfg.OpenCode.ConsoleCols = n }
		case "opencode.console_rows":
			if n, ok := toInt(v); ok { cfg.OpenCode.ConsoleRows = n }
		case "opencode_acp.console_cols":
			if n, ok := toInt(v); ok { cfg.OpenCodeACP.ConsoleCols = n }
		case "opencode_acp.console_rows":
			if n, ok := toInt(v); ok { cfg.OpenCodeACP.ConsoleRows = n }
		case "opencode_prompt.console_cols":
			if n, ok := toInt(v); ok { cfg.OpenCodePrompt.ConsoleCols = n }
		case "opencode_prompt.console_rows":
			if n, ok := toInt(v); ok { cfg.OpenCodePrompt.ConsoleRows = n }
		case "openwebui.console_cols":
			if n, ok := toInt(v); ok { cfg.OpenWebUI.ConsoleCols = n }
		case "openwebui.console_rows":
			if n, ok := toInt(v); ok { cfg.OpenWebUI.ConsoleRows = n }
		case "shell_backend.console_cols":
			if n, ok := toInt(v); ok { cfg.Shell.ConsoleCols = n }
		case "shell_backend.console_rows":
			if n, ok := toInt(v); ok { cfg.Shell.ConsoleRows = n }
		// output_mode per backend
		case "opencode.output_mode":
			cfg.OpenCode.OutputMode = toString(v)
		case "opencode_acp.output_mode":
			cfg.OpenCodeACP.OutputMode = toString(v)
		case "opencode_prompt.output_mode":
			cfg.OpenCodePrompt.OutputMode = toString(v)
		case "ollama.output_mode":
			cfg.Ollama.OutputMode = toString(v)
		case "openwebui.output_mode":
			cfg.OpenWebUI.OutputMode = toString(v)
		case "aider.output_mode":
			cfg.Aider.OutputMode = toString(v)
		case "goose.output_mode":
			cfg.Goose.OutputMode = toString(v)
		case "gemini.output_mode":
			cfg.Gemini.OutputMode = toString(v)
		case "shell_backend.output_mode":
			cfg.Shell.OutputMode = toString(v)
		// input_mode per backend
		case "opencode.input_mode":
			cfg.OpenCode.InputMode = toString(v)
		case "opencode_acp.input_mode":
			cfg.OpenCodeACP.InputMode = toString(v)
		case "opencode_prompt.input_mode":
			cfg.OpenCodePrompt.InputMode = toString(v)
		case "ollama.input_mode":
			cfg.Ollama.InputMode = toString(v)
		case "openwebui.input_mode":
			cfg.OpenWebUI.InputMode = toString(v)
		case "aider.input_mode":
			cfg.Aider.InputMode = toString(v)
		case "goose.input_mode":
			cfg.Goose.InputMode = toString(v)
		case "gemini.input_mode":
			cfg.Gemini.InputMode = toString(v)
		case "shell_backend.input_mode":
			cfg.Shell.InputMode = toString(v)
		// Profiles & Fallback
		case "session.fallback_chain":
			s := toString(v)
			if s == "" {
				cfg.Session.FallbackChain = nil
			} else {
				parts := strings.Split(s, ",")
				chain := make([]string, 0, len(parts))
				for _, p := range parts {
					p = strings.TrimSpace(p)
					if p != "" {
						chain = append(chain, p)
					}
				}
				cfg.Session.FallbackChain = chain
			}
		// RTK config
		case "rtk.enabled":
			cfg.RTK.Enabled = toBool(v)
		case "rtk.binary":
			if s := toString(v); s != "" { cfg.RTK.Binary = s }
		case "rtk.show_savings":
			cfg.RTK.ShowSavings = toBool(v)
		case "rtk.auto_init":
			cfg.RTK.AutoInit = toBool(v)
		case "rtk.discover_interval":
			if n, ok := toInt(v); ok { cfg.RTK.DiscoverInterval = n }
		case "rtk.auto_update":
			cfg.RTK.AutoUpdate = toBool(v)
		case "rtk.update_check_interval":
			if n, ok := toInt(v); ok && n >= 0 { cfg.RTK.UpdateCheckInterval = n }
		case "pipeline.max_parallel":
			if n, ok := toInt(v); ok && n >= 0 { cfg.Pipeline.MaxParallel = n }
		case "pipeline.default_backend":
			cfg.Pipeline.DefaultBackend = toString(v)
		case "whisper.enabled":
			cfg.Whisper.Enabled = toBool(v)
		case "whisper.model":
			if s := toString(v); s != "" { cfg.Whisper.Model = s }
		case "whisper.language":
			cfg.Whisper.Language = toString(v)
		case "whisper.venv_path":
			if s := toString(v); s != "" { cfg.Whisper.VenvPath = s }
		// v4.0.8 (B38) — autonomous / plugins / orchestrator keys.
		// Without these cases the PWA + mobile-client save forms
		// for these sections silently no-op: the handler returns
		// 200 but nothing lands in config.yaml or the live Config.
		// See https://github.com/dmz006/datawatch/issues/19.
		case "autonomous.enabled":
			cfg.Autonomous.Enabled = toBool(v)
		case "autonomous.poll_interval_seconds":
			if n, ok := toInt(v); ok && n >= 0 { cfg.Autonomous.PollIntervalSeconds = n }
		case "autonomous.max_parallel_tasks":
			if n, ok := toInt(v); ok && n >= 0 { cfg.Autonomous.MaxParallelTasks = n }
		case "autonomous.decomposition_backend":
			cfg.Autonomous.DecompositionBackend = toString(v)
		case "autonomous.verification_backend":
			cfg.Autonomous.VerificationBackend = toString(v)
		case "autonomous.decomposition_effort":
			cfg.Autonomous.DecompositionEffort = toString(v)
		case "autonomous.verification_effort":
			cfg.Autonomous.VerificationEffort = toString(v)
		// v5.26.16 — operator-pinned model overrides.
		case "autonomous.decomposition_model":
			cfg.Autonomous.DecompositionModel = toString(v)
		case "autonomous.verification_model":
			cfg.Autonomous.VerificationModel = toString(v)
		case "autonomous.stale_task_seconds":
			if n, ok := toInt(v); ok && n >= 0 { cfg.Autonomous.StaleTaskSeconds = n }
		case "autonomous.auto_fix_retries":
			if n, ok := toInt(v); ok && n >= 0 { cfg.Autonomous.AutoFixRetries = n }
		case "autonomous.security_scan":
			cfg.Autonomous.SecurityScan = toBool(v)
		// v5.17.0 — BL191 Q4 (recursion) + Q6 (guardrails) config
		// surface. Pre-v5.17.0 these keys silently no-op'd through
		// the PUT /api/config path because the case wasn't here.
		case "autonomous.max_recursion_depth":
			if n, ok := toInt(v); ok && n >= 0 {
				cfg.Autonomous.MaxRecursionDepth = n
			}
		case "autonomous.auto_approve_children":
			cfg.Autonomous.AutoApproveChildren = toBool(v)
		case "autonomous.per_task_guardrails":
			if arr, ok := toStringArray(v); ok {
				cfg.Autonomous.PerTaskGuardrails = arr
			} else if s, ok := v.(string); ok {
				cfg.Autonomous.PerTaskGuardrails = splitCSV(s)
			}
		case "autonomous.per_story_guardrails":
			if arr, ok := toStringArray(v); ok {
				cfg.Autonomous.PerStoryGuardrails = arr
			} else if s, ok := v.(string); ok {
				cfg.Autonomous.PerStoryGuardrails = splitCSV(s)
			}
		case "plugins.enabled":
			cfg.Plugins.Enabled = toBool(v)
		case "plugins.dir":
			cfg.Plugins.Dir = toString(v)
		case "plugins.timeout_ms":
			if n, ok := toInt(v); ok && n >= 0 { cfg.Plugins.TimeoutMs = n }
		case "orchestrator.enabled":
			cfg.Orchestrator.Enabled = toBool(v)
		case "orchestrator.guardrail_backend":
			cfg.Orchestrator.GuardrailBackend = toString(v)
		case "orchestrator.guardrail_model":
			cfg.Orchestrator.GuardrailModel = toString(v)
		case "orchestrator.guardrail_timeout_ms":
			if n, ok := toInt(v); ok && n >= 0 { cfg.Orchestrator.GuardrailTimeoutMs = n }
		case "orchestrator.max_parallel_prds":
			if n, ok := toInt(v); ok && n >= 0 { cfg.Orchestrator.MaxParallelPRDs = n }
		// v5.21.0 — observer.* config-parity sweep. Pre-v5.21.0 every
		// observer.* key silently no-op'd through PUT /api/config because
		// applyConfigPatch had zero observer cases. Operators using
		// `datawatch config set observer.foo …` got 200 with no effect.
		case "observer.plugin_enabled":
			b := toBool(v)
			cfg.Observer.PluginEnabled = &b
		case "observer.tick_interval_ms":
			if n, ok := toInt(v); ok && n >= 0 { cfg.Observer.TickIntervalMs = n }
		case "observer.process_tree_enabled":
			b := toBool(v)
			cfg.Observer.ProcessTreeEnabled = &b
		case "observer.top_n_broadcast":
			if n, ok := toInt(v); ok && n >= 0 { cfg.Observer.TopNBroadcast = n }
		case "observer.include_kthreads":
			cfg.Observer.IncludeKthreads = toBool(v)
		case "observer.session_attribution":
			b := toBool(v)
			cfg.Observer.SessionAttribution = &b
		case "observer.backend_attribution":
			b := toBool(v)
			cfg.Observer.BackendAttribution = &b
		case "observer.docker_discovery":
			b := toBool(v)
			cfg.Observer.DockerDiscovery = &b
		case "observer.gpu_attribution":
			b := toBool(v)
			cfg.Observer.GPUAttribution = &b
		case "observer.ebpf_enabled":
			cfg.Observer.EBPFEnabled = toString(v)
		case "observer.conn_correlator":
			cfg.Observer.ConnCorrelator = toBool(v)
		case "observer.federation.parent_url":
			cfg.Observer.Federation.ParentURL = toString(v)
		case "observer.federation.peer_name":
			cfg.Observer.Federation.PeerName = toString(v)
		case "observer.federation.push_interval_seconds":
			if n, ok := toInt(v); ok && n >= 0 { cfg.Observer.Federation.PushIntervalSeconds = n }
		case "observer.federation.token_path":
			cfg.Observer.Federation.TokenPath = toString(v)
		case "observer.federation.insecure":
			cfg.Observer.Federation.Insecure = toBool(v)
		case "observer.ollama_tap.endpoint":
			cfg.Observer.OllamaTap.Endpoint = toString(v)
		case "observer.peers.allow_register":
			cfg.Observer.Peers.AllowRegister = toBool(v)
		case "observer.peers.token_ttl_rotation_grace_s":
			if n, ok := toInt(v); ok && n >= 0 { cfg.Observer.Peers.TokenRotationGraceS = n }
		case "observer.peers.push_interval_seconds":
			if n, ok := toInt(v); ok && n >= 0 { cfg.Observer.Peers.PushIntervalSeconds = n }
		case "observer.peers.listen_addr":
			cfg.Observer.Peers.ListenAddr = toString(v)
		// v5.21.0 — fill in the missing whisper.* keys. Pre-v5.21.0
		// only enabled/model/language/venv_path were patchable; the
		// HTTP-shape backends (openai/openai_compat/openwebui/ollama)
		// needed `backend` + `endpoint` + `api_key` to round-trip but
		// those silently no-op'd.
		case "whisper.backend":
			cfg.Whisper.Backend = toString(v)
		case "whisper.endpoint":
			cfg.Whisper.Endpoint = toString(v)
		case "whisper.api_key":
			cfg.Whisper.APIKey = toString(v)
		default:
			// Unknown keys are logged so future mobile/PWA schema
			// drift surfaces instead of silent no-op'ing again.
			// Clients still get 200 for now — returning 4xx would
			// break existing saves that mix known+unknown keys;
			// a follow-up (issue #19 Option C) can add a stricter
			// mode behind a flag.
			fmt.Fprintf(os.Stderr, "[config] applyConfigPatch: unknown key %q (no-op)\n", k)
		}
	}
}

// SetTestMessageHandler wires a function that routes simulated messages through
// the router. Used by POST /api/test/message for comm channel testing.
func (s *Server) SetTestMessageHandler(fn func(text string) []string) {
	s.testMessageHandler = fn
}

// handleTestMessage simulates an incoming messaging backend message.
// POST /api/test/message { "text": "help" }
// Returns the responses the router would send back.
func (s *Server) handleTestMessage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	if s.testMessageHandler == nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"error": "test message handler not wired"})
		return
	}
	var req struct {
		Text string `json:"text"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Text == "" {
		http.Error(w, "need {\"text\":\"...\"}", http.StatusBadRequest)
		return
	}
	responses := s.testMessageHandler(req.Text)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"input":     req.Text,
		"responses": responses,
		"count":     len(responses),
	})
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

// handleStats returns system metrics.
// GET /api/stats — latest snapshot
// GET /api/stats?history=60 — last N minutes of history (v1 collector only)
// GET /api/stats?v=2      — BL171: structured StatsResponse v2 from the observer
func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	// BL171 v2 path — explicit query param or header. Falls back to
	// v1 collector when the observer isn't wired.
	wantV2 := r.URL.Query().Get("v") == "2" || r.Header.Get("Accept-Version") == "2"
	if wantV2 && s.observerAPI != nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(s.observerAPI.Stats()) //nolint:errcheck
		return
	}
	if s.statsCollector == nil {
		http.Error(w, "stats not available", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	histParam := r.URL.Query().Get("history")
	if histParam != "" {
		minutes, _ := strconv.Atoi(histParam)
		if minutes <= 0 {
			minutes = 5
		}
		json.NewEncoder(w).Encode(s.statsCollector.History(minutes)) //nolint:errcheck
		return
	}
	json.NewEncoder(w).Encode(s.statsCollector.Latest()) //nolint:errcheck
}

// handleProfiles manages named backend profiles (CRUD).
// GET /api/profiles — list all profiles
// POST /api/profiles — create/update a profile {"name":"...","backend":"...","env":{...}}
// DELETE /api/profiles?name=xxx — delete a profile
func (s *Server) handleProfiles(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		profiles := s.cfg.Profiles
		if profiles == nil {
			profiles = map[string]config.ProfileConfig{}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(profiles) //nolint:errcheck

	case http.MethodPost:
		var req struct {
			Name    string            `json:"name"`
			Backend string            `json:"backend"`
			Env     map[string]string `json:"env"`
			Binary  string            `json:"binary"`
			Model   string            `json:"model"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid JSON", http.StatusBadRequest)
			return
		}
		if req.Name == "" || req.Backend == "" {
			http.Error(w, "name and backend required", http.StatusBadRequest)
			return
		}
		if s.cfg.Profiles == nil {
			s.cfg.Profiles = make(map[string]config.ProfileConfig)
		}
		s.cfg.Profiles[req.Name] = config.ProfileConfig{
			Backend: req.Backend,
			Env:     req.Env,
			Binary:  req.Binary,
			Model:   req.Model,
		}
		if err := s.saveConfig(); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "saved", "name": req.Name}) //nolint:errcheck

	case http.MethodDelete:
		name := r.URL.Query().Get("name")
		if name == "" {
			http.Error(w, "name required", http.StatusBadRequest)
			return
		}
		if s.cfg.Profiles != nil {
			delete(s.cfg.Profiles, name)
		}
		if err := s.saveConfig(); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "deleted"}) //nolint:errcheck

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleRTKDiscover returns RTK optimization suggestions.
// GET /api/rtk/discover?since=7
func (s *Server) handleRTKDiscover(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	sinceDays := 7
	if v := r.URL.Query().Get("since"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			sinceDays = n
		}
	}
	report, err := rtk.GetDiscover(sinceDays)
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(report) //nolint:errcheck
}

// handleKillOrphans kills tmux sessions that have no matching datawatch session.
func (s *Server) handleKillOrphans(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.statsCollector == nil {
		http.Error(w, "stats not available", http.StatusServiceUnavailable)
		return
	}
	latest := s.statsCollector.Latest()
	killed := 0
	for _, name := range latest.OrphanedTmux {
		if err := exec.Command("tmux", "kill-session", "-t", name).Run(); err == nil {
			killed++
		}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]int{"killed": killed}) //nolint:errcheck
}

func toStringArray(v interface{}) ([]string, bool) {
	arr, ok := v.([]interface{})
	if !ok {
		return nil, false
	}
	result := make([]string, 0, len(arr))
	for _, item := range arr {
		if s, ok := item.(string); ok {
			result = append(result, s)
		}
	}
	return result, true
}

// splitCSV (v5.17.0) is the convenience fallback for the PWA's
// text-input multi-value entry (e.g. "rules, security,
// release-readiness" → []string{"rules","security","release-readiness"}).
// Empty entries get dropped; whitespace-only entries get dropped.
func splitCSV(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		t := strings.TrimSpace(p)
		if t != "" {
			out = append(out, t)
		}
	}
	return out
}

// ---- Proxy endpoint --------------------------------------------------------

// handleProxy forwards requests to a named remote datawatch server,
// or — when the path starts with "agent/" — to a spawned worker
// container's HTTP API (F10 sprint 3.5).
//
// Route forms:
//
//   /api/proxy/{serverName}/{...path}    → F16 remote-server proxy
//   /api/proxy/agent/{worker_id}/{...}   → S3.5 agent-worker proxy
func (s *Server) handleProxy(w http.ResponseWriter, r *http.Request) {
	// Extract first segment from path: /api/proxy/<name>/...
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

	// Agent-worker namespace: /api/proxy/agent/<id>/...
	// Strip one more segment to get the worker ID, hand off to the
	// dedicated agent proxy handler.
	if serverName == "agent" {
		stripped := strings.TrimPrefix(remotePath, "/")
		idx2 := strings.Index(stripped, "/")
		var agentID, agentPath string
		if idx2 < 0 {
			agentID = stripped
			agentPath = "/"
		} else {
			agentID = stripped[:idx2]
			agentPath = stripped[idx2:]
		}
		s.handleAgentProxy(w, r, agentID, agentPath)
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

	// WebSocket upgrade — hand off to WS proxy handler
	if strings.HasSuffix(remotePath, "/ws") || remotePath == "/ws" {
		s.handleProxyWS(w, r)
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

// handleServerHealth returns health status for all remote servers including
// circuit breaker state and queued command counts.
func (s *Server) handleServerHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	type healthInfo struct {
		Name        string    `json:"name"`
		URL         string    `json:"url"`
		Healthy     bool      `json:"healthy"`
		LastCheck   time.Time `json:"last_check,omitempty"`
		LastError   string    `json:"last_error,omitempty"`
		ConsecFails int       `json:"consec_fails"`
		BreakerOpen bool      `json:"breaker_open"`
		QueuedCmds  int       `json:"queued_cmds"`
	}

	var result []healthInfo

	if s.proxyPool != nil {
		pending := map[string]int{}
		if s.offlineQueue != nil {
			pending = s.offlineQueue.PendingAll()
		}
		for _, h := range s.proxyPool.Health() {
			result = append(result, healthInfo{
				Name:        h.Name,
				URL:         h.URL,
				Healthy:     h.Healthy,
				LastCheck:   h.LastCheck,
				LastError:   h.LastError,
				ConsecFails: h.ConsecFails,
				BreakerOpen: h.BreakerOpen,
				QueuedCmds:  pending[h.Name],
			})
		}
	}

	if result == nil {
		result = []healthInfo{}
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

// handleSchedules provides the enhanced /api/schedules endpoint with deferred session
// support, editing, natural language time parsing, and session filtering.
func (s *Server) handleSchedules(w http.ResponseWriter, r *http.Request) {
	if s.schedStore == nil {
		http.Error(w, "scheduling not available", http.StatusServiceUnavailable)
		return
	}
	switch r.Method {
	case http.MethodGet:
		// Optional filter: ?session_id=xxx, ?state=pending
		sessionID := r.URL.Query().Get("session_id")
		stateFilter := r.URL.Query().Get("state")
		var entries []*session.ScheduledCommand
		if stateFilter != "" {
			entries = s.schedStore.List(stateFilter)
		} else {
			entries = s.schedStore.List()
		}
		if sessionID != "" {
			var filtered []*session.ScheduledCommand
			for _, sc := range entries {
				if sc.SessionID == sessionID {
					filtered = append(filtered, sc)
				}
			}
			entries = filtered
		}
		w.Header().Set("Content-Type", "application/json")
		if entries == nil {
			entries = []*session.ScheduledCommand{}
		}
		json.NewEncoder(w).Encode(entries) //nolint:errcheck

	case http.MethodPost:
		var req struct {
			Type       string `json:"type"`        // "command" or "new_session"
			SessionID  string `json:"session_id"`   // for command type
			Command    string `json:"command"`       // text to send or task for new session
			RunAt      string `json:"run_at"`        // natural language or RFC3339
			RunAfterID string `json:"run_after_id"`
			// For deferred sessions
			Name       string `json:"name"`
			ProjectDir string `json:"project_dir"`
			Backend    string `json:"backend"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid JSON", http.StatusBadRequest)
			return
		}
		// Parse time (supports natural language)
		var runAt time.Time
		if req.RunAt != "" {
			var err error
			runAt, err = session.ParseScheduleTime(req.RunAt, time.Now())
			if err != nil {
				// Fallback: try RFC3339
				runAt, err = time.Parse(time.RFC3339, req.RunAt)
				if err != nil {
					http.Error(w, "cannot parse time: "+req.RunAt, http.StatusBadRequest)
					return
				}
			}
		}

		var sc *session.ScheduledCommand
		var err error
		if req.Type == session.SchedTypeNewSession {
			if req.Command == "" && req.Name == "" {
				http.Error(w, "task or name required for new session", http.StatusBadRequest)
				return
			}
			sc, err = s.schedStore.AddDeferredSession(req.Name, req.Command, req.ProjectDir, req.Backend, runAt)
		} else {
			if req.SessionID == "" || req.Command == "" {
				http.Error(w, "session_id and command required", http.StatusBadRequest)
				return
			}
			sc, err = s.schedStore.Add(req.SessionID, req.Command, runAt, req.RunAfterID)
		}
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(sc) //nolint:errcheck

	case http.MethodPut:
		var req struct {
			ID      string `json:"id"`
			Command string `json:"command"`
			RunAt   string `json:"run_at"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid JSON", http.StatusBadRequest)
			return
		}
		if req.ID == "" {
			http.Error(w, "id required", http.StatusBadRequest)
			return
		}
		var runAt time.Time
		if req.RunAt != "" {
			var err error
			runAt, err = session.ParseScheduleTime(req.RunAt, time.Now())
			if err != nil {
				runAt, err = time.Parse(time.RFC3339, req.RunAt)
				if err != nil {
					http.Error(w, "cannot parse time: "+req.RunAt, http.StatusBadRequest)
					return
				}
			}
		}
		if err := s.schedStore.Update(req.ID, req.Command, runAt); err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "updated"}) //nolint:errcheck

	case http.MethodDelete:
		id := r.URL.Query().Get("id")
		all := r.URL.Query().Get("all")
		if id == "" && all == "" {
			http.Error(w, "id or all required", http.StatusBadRequest)
			return
		}
		if id != "" {
			// Try cancel first (for pending), then delete (for any state)
			err := s.schedStore.Cancel(id)
			if err != nil {
				err = s.schedStore.Delete(id)
			}
			if err != nil {
				http.Error(w, err.Error(), http.StatusNotFound)
				return
			}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "deleted"}) //nolint:errcheck

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
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

// recordChannelHistory appends a message to the per-session ring buffer
// so the PWA Channel tab can render the backlog when the operator opens
// a session that's already been chatting. Caller passes the same
// fields that get broadcast over WS.
func (s *Server) recordChannelHistory(sessionID, text, direction string) {
	if sessionID == "" {
		return
	}
	s.channelHistMu.Lock()
	defer s.channelHistMu.Unlock()
	if s.channelHist == nil {
		s.channelHist = make(map[string][]channelHistEntry)
	}
	entry := channelHistEntry{
		Text:      text,
		SessionID: sessionID,
		Direction: direction,
		Timestamp: time.Now(),
	}
	buf := s.channelHist[sessionID]
	buf = append(buf, entry)
	if len(buf) > channelHistoryMax {
		buf = buf[len(buf)-channelHistoryMax:]
	}
	s.channelHist[sessionID] = buf
}

// handleChannelHistory returns the per-session channel ring buffer.
// GET /api/channel/history?session_id=...
func (s *Server) handleChannelHistory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	sessionID := r.URL.Query().Get("session_id")
	if sessionID == "" {
		http.Error(w, "session_id required", http.StatusBadRequest)
		return
	}
	s.channelHistMu.Lock()
	buf := append([]channelHistEntry(nil), s.channelHist[sessionID]...)
	s.channelHistMu.Unlock()
	// v5.26.9 — JSON-marshal empty slice as `[]`, not `null`.
	// Pre-v5.26.9 release-smoke.sh #5 failed because PWA + smoke
	// scripts both expect a list shape, not nil.
	if buf == nil {
		buf = []channelHistEntry{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
		"session_id": sessionID,
		"messages":   buf,
	})
}

// BroadcastChannelReply broadcasts a channel reply to all connected WS clients.
// Used by opencode ACP to route SSE text replies through the same path as
// claude MCP channel replies, so they render as amber channel-reply-line in the UI.
func (s *Server) BroadcastChannelReply(sessionID, text string) {
	s.recordChannelHistory(sessionID, text, "incoming")
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
	s.recordChannelHistory(body.SessionID, body.Text, "incoming")
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

	// Mark session as channel-ready and store its channel port.
	if readySess != nil {
		readySess.ChannelReady = true
		readySess.ChannelPort = port
		if err := s.manager.SaveSession(readySess); err != nil {
			fmt.Printf("[channel] failed to save channel_ready for %s: %v\n", readySess.FullID, err)
		}
		s.hub.Broadcast(MsgChannelReady, map[string]string{"session_id": readySess.FullID})
		fmt.Printf("[channel] ready for session %s (channel_ready=%v, port=%d)\n", readySess.FullID, readySess.ChannelReady, port)
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

	// Broadcast the task delivery to WS clients for the channel tab
	s.recordChannelHistory(targetSess.FullID, targetSess.Task, "outgoing")
	taskData := map[string]interface{}{
		"text":       targetSess.Task,
		"session_id": targetSess.FullID,
		"direction":  "outgoing",
	}
	taskRaw, _ := json.Marshal(taskData)
	taskMsg := WSMessage{Type: MsgChannelReply, Data: taskRaw, Timestamp: time.Now()}
	taskPayload, _ := json.Marshal(taskMsg)
	s.hub.broadcast <- taskPayload

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
	// Use the per-session channel port if available, fall back to global config
	channelPort := 0
	if body.SessionID != "" {
		if sess, ok := s.manager.GetSession(body.SessionID); ok && sess.ChannelPort > 0 {
			channelPort = sess.ChannelPort
		}
	}
	if channelPort == 0 {
		channelPort = s.cfg.Server.ChannelPort
	}
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

	// Broadcast the outgoing send to WS clients so the channel tab shows it
	s.recordChannelHistory(body.SessionID, body.Text, "outgoing")
	sendData := map[string]interface{}{
		"text":       body.Text,
		"session_id": body.SessionID,
		"direction":  "outgoing",
	}
	raw, _ := json.Marshal(sendData)
	outMsg := WSMessage{Type: MsgChannelReply, Data: raw, Timestamp: time.Now()}
	outPayload, _ := json.Marshal(outMsg)
	s.hub.broadcast <- outPayload

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
	// If eBPF is enabled but caps missing, warn but don't block — daemon will start without eBPF
	if s.cfg != nil && s.cfg.Stats.EBPFEnabled {
		binaryPath, _ := os.Executable()
		if !stats.HasCapBPF(binaryPath) {
			s.hub.Broadcast(MsgNotification, map[string]string{
				"message": "Warning: eBPF enabled but CAP_BPF missing. Restart will start without eBPF. Run: sudo setcap cap_bpf,cap_perfmon+ep " + binaryPath,
			})
		}
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

	// Initial progress event so the PWA can pop the progress UI
	// before the first byte lands.
	s.hub.Broadcast(MsgUpdateProgress, map[string]any{
		"version":    latest,
		"phase":      "starting",
		"downloaded": 0,
		"total":      0,
	})
	s.hub.Broadcast(MsgNotification, map[string]string{
		"message": "[update] Downloading v" + latest + "…",
	})

	go func() {
		// v4.0.6 — progress callback fans download bytes out as
		// MsgUpdateProgress events. Throttled to one event per ~64 KB
		// or per 250 ms so a 40 MB binary produces ~600 events, not
		// one-per-32 KB-chunk which would flood the WS.
		var lastEmit time.Time
		var lastBytes int64
		progress := func(downloaded, total int64) {
			now := time.Now()
			if now.Sub(lastEmit) < 250*time.Millisecond &&
				downloaded-lastBytes < 64*1024 &&
				(total == 0 || downloaded < total) {
				return
			}
			lastEmit = now
			lastBytes = downloaded
			s.hub.Broadcast(MsgUpdateProgress, map[string]any{
				"version":    latest,
				"phase":      "downloading",
				"downloaded": downloaded,
				"total":      total,
			})
		}
		if err := s.installUpdate(latest, progress); err != nil {
			s.hub.Broadcast(MsgUpdateProgress, map[string]any{
				"version": latest,
				"phase":   "failed",
				"error":   err.Error(),
			})
			s.hub.Broadcast(MsgNotification, map[string]string{
				"message": "[update] Install failed: " + err.Error(),
			})
			return
		}
		s.hub.Broadcast(MsgUpdateProgress, map[string]any{
			"version": latest,
			"phase":   "installed",
		})
		s.hub.Broadcast(MsgNotification, map[string]string{
			"message": "[update] Installed v" + latest + ". Restarting daemon…",
		})
		// Give clients 800ms to receive the message before the process dies.
		s.hub.Broadcast(MsgUpdateProgress, map[string]any{
			"version": latest,
			"phase":   "restarting",
		})
		time.Sleep(800 * time.Millisecond)
		selfPath, err := os.Executable()
		if err == nil {
			selfPath, _ = filepath.EvalSymlinks(selfPath)
			_ = syscall.Exec(selfPath, os.Args, os.Environ()) // #nosec G702 -- argv-list, not shell; selfPath from os.Executable()
		}
		// If Exec fails (Windows), just exit so the supervisor/user can restart.
		os.Exit(0)
	}()
}
