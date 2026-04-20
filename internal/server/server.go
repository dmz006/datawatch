package server

import (
	"context"
	"crypto/tls"
	"embed"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/agents"
	"github.com/dmz006/datawatch/internal/alerts"
	"github.com/dmz006/datawatch/internal/audit"
	"github.com/dmz006/datawatch/internal/devices"
	"github.com/dmz006/datawatch/internal/messaging"
	"github.com/dmz006/datawatch/internal/metrics"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/profile"
	"github.com/dmz006/datawatch/internal/proxy"
	"github.com/dmz006/datawatch/internal/session"
	"github.com/dmz006/datawatch/internal/stats"
	"github.com/dmz006/datawatch/internal/tlsutil"
)

//go:embed web
var webFS embed.FS

// HTTPServer wraps the HTTP server and hub
type HTTPServer struct {
	cfg     *config.ServerConfig
	dataDir string
	hub     *Hub
	srv     *http.Server
	manager *session.Manager
	api     *Server
}

// Reload (BL17) is the public SIGHUP/api/reload entry-point;
// delegates to the underlying api Server.
func (h *HTTPServer) Reload() ReloadResult {
	if h.api == nil {
		return ReloadResult{Error: "api not initialised"}
	}
	return h.api.Reload()
}

// New creates a new HTTPServer
func New(cfg *config.ServerConfig, fullCfg *config.Config, cfgPath string, dataDir string, manager *session.Manager, hostname string, backends []string) *HTTPServer {
	hub := NewHub()
	api := NewServer(hub, manager, hostname, cfg.Token, backends, fullCfg, cfgPath)
	metrics.Register()

	webSub, _ := fs.Sub(webFS, "web")

	mux := http.NewServeMux()

	// Public routes (no auth)
	mux.HandleFunc("/api/health", api.handleHealth)
	mux.HandleFunc("/healthz", api.handleHealthz)
	mux.HandleFunc("/readyz", api.handleReadyz)
	// F10 sprint 3: bootstrap is unauthenticated because the worker
	// doesn't have the bearer token at startup. Security comes from
	// the single-use bootstrap token minted at spawn time.
	mux.HandleFunc("/api/agents/bootstrap", api.handleAgentBootstrap)
	mux.HandleFunc("/api/agents/ca.pem", api.handleAgentCAPEM)
	mux.Handle("/metrics", metrics.Handler())

	// Docs routes (no auth required, served directly)
	mux.HandleFunc("/api/docs", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/api-docs.html", http.StatusMovedPermanently)
	})
	mux.HandleFunc("/api/openapi.yaml", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/yaml")
		f, err := webSub.Open("openapi.yaml")
		if err != nil {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		defer f.Close()
		io.Copy(w, f) //nolint:errcheck
	})

	// Authenticated API routes
	apiMux := http.NewServeMux()
	apiMux.HandleFunc("/api/sessions", api.handleSessions)
	apiMux.HandleFunc("/api/output", api.handleSessionOutput)
	apiMux.HandleFunc("/api/command", api.handleCommand)
	apiMux.HandleFunc("/api/info", api.handleInfo)
	apiMux.HandleFunc("/api/backends", api.handleBackends)
	apiMux.HandleFunc("/api/files", api.handleFiles)
	apiMux.HandleFunc("/api/sessions/timeline", api.handleSessionTimeline)
	apiMux.HandleFunc("/api/sessions/rename", api.handleRenameSession)
	apiMux.HandleFunc("/api/sessions/bind", api.handleBindSessionAgent)
	apiMux.HandleFunc("/api/sessions/kill", api.handleKillSession)
	apiMux.HandleFunc("/api/sessions/delete", api.handleDeleteSession)
	apiMux.HandleFunc("/api/sessions/start", api.handleStartSession)
	apiMux.HandleFunc("/api/sessions/restart", api.handleRestartSession)
	apiMux.HandleFunc("/api/sessions/state", api.handleSetSessionState)
	apiMux.HandleFunc("/api/sessions/response", api.handleSessionResponse)
	apiMux.HandleFunc("/api/sessions/prompt", api.handleSessionPrompt)
	apiMux.HandleFunc("/api/sessions/reconcile", api.handleSessionReconcile) // BL93
	apiMux.HandleFunc("/api/sessions/import", api.handleSessionImport)       // BL94
	apiMux.HandleFunc("/api/link/start", api.handleLinkStart)
	apiMux.HandleFunc("/api/link/stream", api.handleLinkStream)
	apiMux.HandleFunc("/api/link/status", api.handleLinkStatus)
	apiMux.HandleFunc("/api/config", api.handleConfig)
	// F10 sprint 2 — Project + Cluster profile CRUD + smoke.
	// Trailing slashes let the handler parse {name}[/smoke] subpaths.
	apiMux.HandleFunc("/api/profiles/projects", api.handleProjectProfiles)
	apiMux.HandleFunc("/api/profiles/projects/", api.handleProjectProfiles)
	apiMux.HandleFunc("/api/profiles/clusters", api.handleClusterProfiles)
	apiMux.HandleFunc("/api/profiles/clusters/", api.handleClusterProfiles)

	// F10 sprint 3 — agent lifecycle routes.
	apiMux.HandleFunc("/api/agents/audit", api.handleAgentAudit)         // BL107
	apiMux.HandleFunc("/api/agents/peer/send", api.handlePeerSend)       // BL104
	apiMux.HandleFunc("/api/agents/peer/inbox", api.handlePeerInbox)     // BL104 (registered before catch-all)
	apiMux.HandleFunc("/api/agents", api.handleAgents)
	apiMux.HandleFunc("/api/agents/", api.handleAgents)
	// Bootstrap is the only unauthenticated path; registered on the
	// public (pre-auth) mux below.
	apiMux.HandleFunc("/api/servers", api.handleListServers)
	apiMux.HandleFunc("/api/servers/health", api.handleServerHealth)
	apiMux.HandleFunc("/api/proxy/comm/", api.handleCommProxySend)       // BL102
	apiMux.HandleFunc("/api/devices/register", api.handleDevicesRegister) // issue #1
	apiMux.HandleFunc("/api/devices", api.handleDevicesList)              // issue #1 (list)
	apiMux.HandleFunc("/api/devices/", api.handleDevicesList)             // issue #1 (delete by id)
	apiMux.HandleFunc("/api/voice/transcribe", api.handleVoiceTranscribe) // issue #2
	apiMux.HandleFunc("/api/federation/sessions", api.handleFederationSessions) // issue #3
	apiMux.HandleFunc("/api/analytics", api.handleAnalytics) // BL12
	apiMux.HandleFunc("/api/diagnose", api.handleDiagnose)   // BL37
	apiMux.HandleFunc("/api/reload", api.handleReload)       // BL17
	apiMux.HandleFunc("/api/ask", api.handleAsk)             // BL34
	apiMux.HandleFunc("/api/project/summary", api.handleProjectSummary) // BL35
	apiMux.HandleFunc("/api/projects", api.handleProjects)              // BL27
	apiMux.HandleFunc("/api/projects/", api.handleProjects)             // BL27 (with name)
	apiMux.HandleFunc("/api/sessions/stale", api.handleSessionsStale)   // BL40
	apiMux.HandleFunc("/api/cooldown", api.handleCooldown)              // BL30
	apiMux.HandleFunc("/api/audit", api.handleAudit)                    // BL9
	apiMux.HandleFunc("/api/cost", api.handleCostSummary)               // BL6
	apiMux.HandleFunc("/api/cost/usage", api.handleCostUsage)           // BL6
	apiMux.HandleFunc("/api/cost/rates", api.handleCostRates)           // BL6 — operator override
	apiMux.HandleFunc("/api/sessions/", api.handleSessionsSubpath)      // BL29 + future
	apiMux.HandleFunc("/api/templates", api.handleTemplates)            // BL5
	apiMux.HandleFunc("/api/templates/", api.handleTemplates)           // BL5 (with name)
	apiMux.HandleFunc("/api/proxy/", api.handleProxy)
	apiMux.HandleFunc("/api/schedule", api.handleSchedule)
	apiMux.HandleFunc("/api/commands", api.handleCommands)
	apiMux.HandleFunc("/api/filters", api.handleFilters)
	apiMux.HandleFunc("/api/alerts", api.handleAlerts)
	apiMux.HandleFunc("/api/channel/reply", api.handleChannelReply)
	apiMux.HandleFunc("/api/channel/notify", api.handleChannelNotify)
	apiMux.HandleFunc("/api/channel/send", api.handleChannelSend)
	apiMux.HandleFunc("/api/channel/ready", api.handleChannelReady)
	apiMux.HandleFunc("/api/update", api.handleUpdate)
	apiMux.HandleFunc("/api/restart", api.handleRestart)
	apiMux.HandleFunc("/api/mcp/docs", api.handleMCPDocs)
	apiMux.HandleFunc("/api/ollama/models", api.handleOllamaModels)
	apiMux.HandleFunc("/api/openwebui/models", api.handleOpenWebUIModels)
	apiMux.HandleFunc("/api/interfaces", api.handleInterfaces)
	apiMux.HandleFunc("/api/schedules", api.handleSchedules)
	apiMux.HandleFunc("/api/stats", api.handleStats)
	apiMux.HandleFunc("/api/stats/kill-orphans", api.handleKillOrphans)
	apiMux.HandleFunc("/api/rtk/discover", api.handleRTKDiscover)
	apiMux.HandleFunc("/api/profiles", api.handleProfiles)
	apiMux.HandleFunc("/api/test/message", api.handleTestMessage)
	apiMux.HandleFunc("/api/ollama/stats", api.handleOllamaStats)
	apiMux.HandleFunc("/api/sessions/aggregated", api.handleAggregatedSessions)
	apiMux.HandleFunc("/api/memory/stats", api.handleMemoryStats)
	apiMux.HandleFunc("/api/memory/list", api.handleMemoryList)
	apiMux.HandleFunc("/api/memory/search", api.handleMemorySearch)
	apiMux.HandleFunc("/api/pipelines", api.handlePipelines)
	apiMux.HandleFunc("/api/pipeline", api.handlePipelineAction)
	apiMux.HandleFunc("/api/memory/save", api.handleMemorySave)
	apiMux.HandleFunc("/api/memory/delete", api.handleMemoryDelete)
	apiMux.HandleFunc("/api/memory/reindex", api.handleMemoryReindex)
	apiMux.HandleFunc("/api/memory/learnings", api.handleMemoryLearnings)
	apiMux.HandleFunc("/api/memory/research", api.handleMemoryResearch)
	apiMux.HandleFunc("/api/memory/export", api.handleMemoryExport)
	apiMux.HandleFunc("/api/memory/import", api.handleMemoryImport)
	apiMux.HandleFunc("/api/memory/wal", api.handleMemoryWAL)
	apiMux.HandleFunc("/api/memory/test", api.handleMemoryTest)
	apiMux.HandleFunc("/api/memory/kg/query", api.handleKGQuery)
	apiMux.HandleFunc("/api/memory/kg/add", api.handleKGAdd)
	apiMux.HandleFunc("/api/memory/kg/invalidate", api.handleKGInvalidate)
	apiMux.HandleFunc("/api/memory/kg/timeline", api.handleKGTimeline)
	apiMux.HandleFunc("/api/memory/kg/stats", api.handleKGStats)
	// Serve TLS certificate for easy install on mobile devices.
	// ?format=der returns DER-encoded .crt (preferred by Android).
	// Default returns PEM.
	apiMux.HandleFunc("/api/cert", func(w http.ResponseWriter, r *http.Request) {
		certPath := cfg.TLSCert
		if certPath == "" {
			certPath = filepath.Join(dataDir, "tls", "server", "cert.pem")
		}
		pemData, err := os.ReadFile(certPath)
		if err != nil {
			http.Error(w, "No certificate found. Enable TLS first.", http.StatusNotFound)
			return
		}
		if r.URL.Query().Get("format") == "der" {
			// Convert PEM to DER for Android
			block, _ := pem.Decode(pemData)
			if block == nil {
				http.Error(w, "Invalid certificate", http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/x-x509-ca-cert")
			w.Header().Set("Content-Disposition", "attachment; filename=datawatch-ca.crt")
			w.Write(block.Bytes) //nolint:errcheck
			return
		}
		w.Header().Set("Content-Type", "application/x-pem-file")
		w.Header().Set("Content-Disposition", "attachment; filename=datawatch-ca.pem")
		w.Write(pemData) //nolint:errcheck
	})

	logDataDir := dataDir // capture for closure
	apiMux.HandleFunc("/api/logs", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		logPath := filepath.Join(logDataDir, "daemon.log")
		data, err := os.ReadFile(logPath)
		if err != nil {
			http.Error(w, "log unavailable", http.StatusNotFound)
			return
		}
		lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
		nLines := 50
		if n := r.URL.Query().Get("lines"); n != "" {
			if v, e := strconv.Atoi(n); e == nil && v > 0 && v <= 500 { nLines = v }
		}
		offset := 0
		if o := r.URL.Query().Get("offset"); o != "" {
			if v, e := strconv.Atoi(o); e == nil && v >= 0 { offset = v }
		}
		total := len(lines)
		start := total - offset - nLines
		if start < 0 { start = 0 }
		end := total - offset
		if end < 0 { end = 0 }
		if end > total { end = total }
		page := lines[start:end]
		for i, j := 0, len(page)-1; i < j; i, j = i+1, j-1 { page[i], page[j] = page[j], page[i] }
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"lines": page, "total": total, "offset": offset})
	})

	// Apply auth middleware to API routes
	mux.Handle("/api/", api.authMiddleware(apiMux))
	mux.Handle("/ws", api.authMiddleware(http.HandlerFunc(api.handleWS)))

	// Remote PWA proxy: /remote/{server}/... serves the full PWA from a remote instance
	mux.Handle("/remote/", api.authMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Redirect /remote/name to /remote/name/ for correct relative paths
		trimmed := strings.TrimPrefix(r.URL.Path, "/remote/")
		if !strings.Contains(trimmed, "/") {
			api.handleRemotePWARedirect(w, r)
			return
		}
		api.handleRemotePWA(w, r)
	})))

	// Serve PWA static files
	mux.Handle("/", http.FileServer(http.FS(webSub)))

	addr := joinHostPort(cfg.Host, cfg.Port) // BL1 — IPv6-safe bracketing
	srv := &http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:       15 * time.Second,
		ReadHeaderTimeout: 10 * time.Second,
		WriteTimeout:      0, // 0 = no timeout for WebSocket
		IdleTimeout:       60 * time.Second,
	}

	return &HTTPServer{
		cfg:     cfg,
		dataDir: dataDir,
		hub:     hub,
		srv:     srv,
		manager: manager,
		api:     api,
	}
}

// SetScheduleStore wires a schedule store into the server for /api/schedule.
func (s *HTTPServer) SetScheduleStore(store *session.ScheduleStore) {
	s.api.SetScheduleStore(store)
}

// SetCmdLibrary wires a command library into the server for /api/commands.
func (s *HTTPServer) SetCmdLibrary(lib *session.CmdLibrary) {
	s.api.cmdLib = lib
}

// SetProjectStore wires the Project Profile store for /api/profiles/projects.
func (s *HTTPServer) SetProjectStore(p *profile.ProjectStore) {
	s.api.SetProjectStore(p)
}

// SetClusterStore wires the Cluster Profile store for /api/profiles/clusters.
func (s *HTTPServer) SetClusterStore(c *profile.ClusterStore) {
	s.api.SetClusterStore(c)
}

// SetAgentManager wires the agent lifecycle manager for /api/agents.
func (s *HTTPServer) SetAgentManager(m *agents.Manager) {
	s.api.SetAgentManager(m)
}

// SetAgentAuditPath (BL107) wires the on-disk audit file path so the
// /api/agents/audit query handler can read it. cef=true marks the
// file as CEF-formatted; the handler refuses to query CEF files
// (the operator should query their SIEM instead).
func (s *HTTPServer) SetAgentAuditPath(path string, cef bool) {
	s.api.agentAuditPath = path
	s.api.agentAuditCEF = cef
}

// SetPeerBroker (BL104) wires the worker peer-broker so the parent's
// /api/agents/peer/{send,inbox} endpoints can route messages.
func (s *HTTPServer) SetPeerBroker(b *agents.PeerBroker) {
	s.api.SetPeerBroker(b)
}

// SetCommBackends (BL102) wires the comm-backend registry so workers
// can post outbound alerts via /api/proxy/comm/{channel}/send.
func (s *HTTPServer) SetCommBackends(b map[string]messaging.Backend) {
	s.api.SetCommBackends(b)
}

// SetCommDefaults wires the per-channel default recipient.
func (s *HTTPServer) SetCommDefaults(d map[string]string) {
	s.api.SetCommDefaults(d)
}

// SetDeviceStore (issue #1) wires the mobile push device registry.
func (s *HTTPServer) SetDeviceStore(store *devices.Store) {
	s.api.SetDeviceStore(store)
}

// SetAuditLog (BL9) wires the operator audit log for /api/audit.
func (s *HTTPServer) SetAuditLog(l *audit.Log) {
	s.api.SetAuditLog(l)
}

// SetVoiceTranscriber (issue #2) wires the Whisper transcriber for
// /api/voice/transcribe. Accepts any type with Transcribe(ctx, path)
// so main.go can pass the existing transcribe.Transcriber directly.
func (s *HTTPServer) SetVoiceTranscriber(t interface {
	Transcribe(ctx context.Context, audioPath string) (string, error)
}) {
	s.api.SetTranscriber(t)
}

// SetAlertStore wires an alert store into the server for /api/alerts.
func (s *HTTPServer) SetAlertStore(store *alerts.Store) {
	s.api.alertStore = store
}

// SetRestartFunc wires the restart function into the server for /api/restart.
func (s *HTTPServer) SetRestartFunc(fn func()) {
	s.api.SetRestartFunc(fn)
}

// SetUpdateFuncs wires update functions into the server for /api/update.
func (s *HTTPServer) SetUpdateFuncs(installFn func(string) error, latestFn func() (string, error)) {
	s.api.SetUpdateFuncs(installFn, latestFn)
}

// SetFilterStore wires a filter store into the server for /api/filters.
func (s *HTTPServer) SetFilterStore(store *session.FilterStore) {
	s.api.filterStore = store
}

// SetKGAPI wires the knowledge graph for REST endpoints.
func (s *HTTPServer) SetKGAPI(api KGAPI) {
	s.api.kgAPI = api
}

// SetMemoryTestFunc wires the Ollama embedding test for B28.
func (s *HTTPServer) SetMemoryTestFunc(fn func(host, model string) (int, error)) {
	s.api.memoryTestFn = fn
}

// SetMemoryAPI wires the memory system for REST endpoints.
func (s *HTTPServer) SetMemoryAPI(api MemoryAPI) {
	s.api.memoryAPI = api
}

// SetPipelineAPI wires the pipeline executor for REST endpoints.
func (s *HTTPServer) SetPipelineAPI(api PipelineAPI) {
	s.api.pipelineExec = api
}

// SetProxyPool wires the connection pool for remote server health tracking.
func (s *HTTPServer) SetProxyPool(pool interface{ Health() []proxy.ServerHealth; IsHealthy(string) bool }) {
	s.api.proxyPool = pool
}

// SetOfflineQueue wires the offline command queue for pending count display.
func (s *HTTPServer) SetOfflineQueue(queue interface{ PendingAll() map[string]int }) {
	s.api.offlineQueue = queue
}

// SetMCPDocsFunc wires a function that returns MCP tool documentation.
func (s *HTTPServer) SetMCPDocsFunc(fn func() interface{}) { s.api.mcpDocsFunc = fn }
func (s *HTTPServer) SetStatsCollector(c *stats.Collector) { s.api.statsCollector = c }
func (s *HTTPServer) SetTestMessageHandler(fn func(string) []string) { s.api.SetTestMessageHandler(fn) }

// NotifyAlert broadcasts a new alert to all WebSocket clients.
func (s *HTTPServer) NotifyAlert(a *alerts.Alert) {
	s.hub.BroadcastAlert(a)
}

// NotifyStateChange broadcasts a session state change to all WS clients
func (s *HTTPServer) NotifyStateChange(sess *session.Session, oldState session.State) {
	s.hub.BroadcastSessions(s.manager.ListSessions())
}

// NotifyNeedsInput broadcasts a needs-input event to all WS clients
func (s *HTTPServer) NotifyNeedsInput(sess *session.Session, prompt string) {
	s.hub.BroadcastNeedsInput(sess.FullID, prompt)
	s.hub.BroadcastSessions(s.manager.ListSessions())
}

// NotifyOutput broadcasts new output lines for a session
func (s *HTTPServer) NotifyOutput(sessionID string, lines []string) {
	s.hub.BroadcastOutput(sessionID, lines)
}

// NotifyRawOutput broadcasts raw output (ANSI preserved) for log-mode sessions
func (s *HTTPServer) NotifyRawOutput(sessionID string, lines []string) {
	s.hub.BroadcastRawOutput(sessionID, lines)
}

// NotifyPaneCapture broadcasts a clean pane capture for terminal-mode display
func (s *HTTPServer) NotifyPaneCapture(sessionID string, lines []string) {
	s.hub.Broadcast("pane_capture", OutputData{SessionID: sessionID, Lines: lines})
}

// NotifySessionAwareness broadcasts a session summary for cross-session awareness.
func (s *HTTPServer) NotifySessionAwareness(sessionID, summary, task, state string) {
	s.hub.BroadcastSessionAwareness(sessionID, summary, task, state)
}

// NotifyResponse broadcasts a captured LLM response to all WS clients.
func (s *HTTPServer) NotifyResponse(sessionID, response string) {
	s.hub.BroadcastResponse(sessionID, response)
}

// NotifyChatMessage broadcasts a structured chat message for chat-mode sessions.
func (s *HTTPServer) NotifyChatMessage(sessionID, role, content string, streaming bool) {
	s.hub.BroadcastChatMessage(sessionID, role, content, streaming)
}

// BroadcastChannelReply sends an ACP/MCP channel reply to all WS clients.
// Used to route opencode ACP SSE text replies through the same WS path as
// claude MCP channel replies.
func (s *HTTPServer) BroadcastChannelReply(sessionID, text string) {
	s.api.BroadcastChannelReply(sessionID, text)
}

// Start begins serving. Blocks until ctx is cancelled.
// The host field supports comma-separated addresses for multi-interface binding
// (e.g. "127.0.0.1,203.0.113.5"). Each address gets its own listener.
func (s *HTTPServer) Start(ctx context.Context) error {
	go s.hub.Run()

	// Wire real-time stats broadcast — fires on every collection (every 5s).
	// Chain with any existing onCollect (e.g. Prometheus metrics update).
	if s.api.statsCollector != nil {
		hub := s.hub
		existingFn := s.api.statsCollector.GetOnCollect()
		s.api.statsCollector.SetOnCollect(func(data stats.SystemStats) {
			hub.BroadcastStats(data)
			if existingFn != nil {
				existingFn(data)
			}
		})
	}

	hosts := strings.Split(s.cfg.Host, ",")
	if len(hosts) == 0 {
		hosts = []string{"0.0.0.0"}
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
			Name:         "server",
		})
		if err != nil {
			return fmt.Errorf("TLS setup: %w", err)
		}
		s.srv.TLSConfig = tlsCfg
	}

	// Dual-interface mode: when TLSPort is set, run HTTP on main port and HTTPS on TLSPort.
	// When TLSPort is 0, TLS replaces the main port (original behavior).
	dualMode := tlsCfg != nil && s.cfg.TLSPort > 0

	errCh := make(chan error, len(hosts)*2)
	for _, host := range hosts {
		host = strings.TrimSpace(host)
		if host == "" {
			continue
		}

		if dualMode {
			// HTTP on main port with redirect to HTTPS
			httpAddr := joinHostPort(host, s.cfg.Port) // BL1
			httpListener, err := net.Listen("tcp", httpAddr)
			if err != nil {
				return fmt.Errorf("listen %s: %w", httpAddr, err)
			}
			tlsPort := s.cfg.TLSPort
			redirectSrv := &http.Server{
				Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					host := r.Host
				if colonIdx := strings.LastIndex(host, ":"); colonIdx > 0 {
					host = host[:colonIdx] // strip port from Host header
				}
				target := fmt.Sprintf("https://%s:%d%s", host, tlsPort, r.URL.RequestURI())
					http.Redirect(w, r, target, http.StatusTemporaryRedirect)
				}),
				ReadTimeout:       5 * time.Second,
				ReadHeaderTimeout: 5 * time.Second,
			}
			go func(l net.Listener, a string) {
				errCh <- redirectSrv.Serve(l)
			}(httpListener, httpAddr)
			fmt.Printf("datawatch server listening on http://%s (redirects to TLS port %d)\n", httpAddr, tlsPort)

			// HTTPS on TLS port
			tlsAddr := joinHostPort(host, s.cfg.TLSPort) // BL1
			tlsListener, err := net.Listen("tcp", tlsAddr)
			if err != nil {
				return fmt.Errorf("listen TLS %s: %w", tlsAddr, err)
			}
			go func(l net.Listener, a string) {
				errCh <- s.srv.ServeTLS(l, "", "")
			}(tlsListener, tlsAddr)
			fmt.Printf("datawatch server listening on https://%s (TLS 1.3+)\n", tlsAddr)
		} else {
			// Single interface: TLS replaces main port, or plain HTTP
			addr := joinHostPort(host, s.cfg.Port) // BL1
			listener, err := net.Listen("tcp", addr)
			if err != nil {
				return fmt.Errorf("listen %s: %w", addr, err)
			}
			if tlsCfg != nil {
				go func(l net.Listener, a string) {
					errCh <- s.srv.ServeTLS(l, "", "")
				}(listener, addr)
				fmt.Printf("datawatch server listening on https://%s (TLS 1.3+)\n", addr)
			} else {
				go func(l net.Listener, a string) {
					errCh <- s.srv.Serve(l)
				}(listener, addr)
				fmt.Printf("datawatch server listening on http://%s\n", addr)
			}
		}
	}

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return s.srv.Shutdown(shutdownCtx)
	case err := <-errCh:
		return err
	}
}

// Hub returns the WebSocket hub (for wiring session manager callbacks)
func (s *HTTPServer) Hub() *Hub {
	return s.hub
}

// tlsAuthMiddleware wraps an http.Handler with bearer token auth and TLS enforcement.
// It is used by the MCP SSE server to gate remote connections.
func tlsAuthMiddleware(token string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if token != "" {
			auth := r.Header.Get("Authorization")
			if auth != "Bearer "+token {
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// BuildTLSConfig is a convenience wrapper for building a *tls.Config from a ServerConfig.
func BuildTLSConfig(cfg *config.ServerConfig, dataDir string) (*tls.Config, error) {
	return tlsutil.Build(tlsutil.Config{
		Enabled:      cfg.TLSEnabled,
		CertFile:     cfg.TLSCert,
		KeyFile:      cfg.TLSKey,
		AutoGenerate: cfg.TLSAutoGenerate,
		DataDir:      dataDir,
		Name:         "server",
	})
}
