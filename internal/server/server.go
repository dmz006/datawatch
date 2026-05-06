package server

import (
	"compress/gzip"
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
	"sync"
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

// isLoopbackRemote (v5.18.0) returns true when the request's
// RemoteAddr is a 127.0.0.0/8 / ::1 / ::ffff:127.0.0.0/8 address.
// Used by the dual-mode HTTP listener to bypass the HTTP→HTTPS
// redirect for the MCP channel bridge's loopback-only endpoints
// (the bridge can't follow the redirect through the daemon's
// self-signed TLS cert).
func isLoopbackRemote(remoteAddr string) bool {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// redirectToTLSHandler (v5.18.0, extracted for testability) returns
// the HTTP-port handler that 307s every request to the HTTPS port —
// EXCEPT loopback requests to a small allow-list of paths that the
// daemon's own internal callers (MCP channel bridge, autonomous
// decomposer / verifier / executor / orchestrator guardrail loops)
// hit. Without the bypass these loopback POSTs would be 307'd to the
// HTTPS port, which serves a self-signed cert that the Go HTTP
// client refuses by default. See v5.18.0 release notes +
// redirect_bypass_test.go for the regression coverage.
//
// v5.26.8 — extended bypass to cover /api/ask + /api/sessions/* +
// /api/orchestrator/* + /api/autonomous/*. Operator-reported:
// autonomous PRD decompose was failing with x509 errors because the
// loopback decomposer hit the redirect chain. Same trust-boundary as
// channel: the request originates inside the daemon process.
//
// Each entry is matched as either an exact path (no trailing slash)
// or a prefix (trailing slash). The exact-match form is required so
// /api/asksomething doesn't accidentally bypass via "/api/ask"; the
// prefix form (e.g. /api/sessions/) matches sub-paths like
// /api/sessions/start. Tests in redirect_bypass_test.go cover both
// the bypass cases and the deny-overshoot cases.
var loopbackBypassPaths = []string{
	"/api/channel/",      // prefix
	"/api/ask",           // exact
	"/api/sessions",      // exact (collection)
	"/api/sessions/",     // prefix (per-session sub-paths)
	"/api/orchestrator/", // prefix
	"/api/autonomous/",   // prefix
}

func loopbackPathBypassed(p string) bool {
	for _, entry := range loopbackBypassPaths {
		if strings.HasSuffix(entry, "/") {
			if strings.HasPrefix(p, entry) {
				return true
			}
		} else if p == entry {
			return true
		}
	}
	return false
}

func (s *HTTPServer) redirectToTLSHandler(tlsPort int) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isLoopbackRemote(r.RemoteAddr) && loopbackPathBypassed(r.URL.Path) {
			s.srv.Handler.ServeHTTP(w, r)
			return
		}
		host := r.Host
		if colonIdx := strings.LastIndex(host, ":"); colonIdx > 0 {
			host = host[:colonIdx]
		}
		target := fmt.Sprintf("https://%s:%d%s", host, tlsPort, r.URL.RequestURI())
		http.Redirect(w, r, target, http.StatusTemporaryRedirect)
	})
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
	// BL242 Phase 5c — agent runtime secret access. Pre-auth: auth is
	// the per-agent SecretsToken delivered in the bootstrap response.
	mux.HandleFunc("/api/agents/secrets/", api.handleAgentSecretsGet)
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
	// v5.27.9 (BL213, datawatch#31) — mobile companion BL21 spec
	// names the SSE stream `/api/link/qr`. Alias to handleLinkStream
	// (same event-stream contract) so both old PWA + new mobile
	// flows resolve.
	apiMux.HandleFunc("/api/link/qr", api.handleLinkStream)
	apiMux.HandleFunc("/api/link/status", api.handleLinkStatus)
	// v5.27.9 — DELETE /api/link/{deviceId}. Catch-all on the
	// /api/link/ prefix, dispatched in the handler when the trailing
	// path segment is a device id. Other /api/link/* paths
	// (start, stream, qr, status) are registered first so they win.
	apiMux.HandleFunc("/api/link/", api.handleLinkUnlink)
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
	apiMux.HandleFunc("/api/voice/test", api.handleVoiceTest)             // BL289 — Settings test button
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
	apiMux.HandleFunc("/api/secrets/", api.handleSecrets)              // BL242
	apiMux.HandleFunc("/api/secrets", api.handleSecrets)               // BL242 (list + create)
	apiMux.HandleFunc("/api/skills/registries", api.handleSkillsRegistries)  // BL255
	apiMux.HandleFunc("/api/skills/registries/", api.handleSkillsRegistries) // BL255
	apiMux.HandleFunc("/api/skills", api.handleSkills)                       // BL255 (synced list)
	apiMux.HandleFunc("/api/skills/", api.handleSkills)                      // BL255 (synced get + content)
	apiMux.HandleFunc("/api/identity", api.handleIdentity)                   // BL257 P1 v6.8.0 (GET/PUT/PATCH)
	apiMux.HandleFunc("/api/algorithm", api.handleAlgorithm)                 // BL258 v6.9.0 (list)
	apiMux.HandleFunc("/api/algorithm/", api.handleAlgorithm)                // BL258 v6.9.0 (per-session + actions)
	apiMux.HandleFunc("/api/evals/suites", api.handleEvalsSuites)            // BL259 P1 v6.10.0
	apiMux.HandleFunc("/api/evals/run", api.handleEvalsRun)                  // BL259 P1 v6.10.0
	apiMux.HandleFunc("/api/evals/runs", api.handleEvalsRuns)                // BL259 P1 v6.10.0
	apiMux.HandleFunc("/api/evals/runs/", api.handleEvalsRuns)               // BL259 P1 v6.10.0
	apiMux.HandleFunc("/api/council/personas", api.handleCouncilPersonas)    // BL260 v6.11.0
	apiMux.HandleFunc("/api/council/personas/", api.handleCouncilPersonas)   // v6.12.4 — /name + /name/restore
	apiMux.HandleFunc("/api/council/run", api.handleCouncilRun)              // BL260 v6.11.0
	apiMux.HandleFunc("/api/council/runs", api.handleCouncilRuns)            // BL260 v6.11.0
	apiMux.HandleFunc("/api/council/runs/", api.handleCouncilRuns)           // BL260 v6.11.0
	apiMux.HandleFunc("/api/tailscale/status", api.handleTailscaleStatus)           // BL243
	apiMux.HandleFunc("/api/tailscale/nodes", api.handleTailscaleNodes)             // BL243
	apiMux.HandleFunc("/api/tailscale/acl/push", api.handleTailscaleACLPush)        // BL243
	apiMux.HandleFunc("/api/tailscale/acl/generate", api.handleTailscaleACLGenerate) // BL243 Phase 3
	apiMux.HandleFunc("/api/tailscale/auth/key", api.handleTailscaleAuthKey)        // BL243 Phase 2
	apiMux.HandleFunc("/api/cost", api.handleCostSummary)               // BL6
	apiMux.HandleFunc("/api/cost/usage", api.handleCostUsage)           // BL6
	apiMux.HandleFunc("/api/cost/rates", api.handleCostRates)           // BL6 — operator override
	// Sprint S4 (v3.8.0) — messaging + UI polish.
	apiMux.HandleFunc("/api/splash/logo", api.handleSplashLogo)         // BL69
	apiMux.HandleFunc("/api/splash/info", api.handleSplashInfo)         // BL69
	apiMux.HandleFunc("/api/assist", api.handleAssist)                  // BL42
	apiMux.HandleFunc("/api/device-aliases", api.handleDeviceAliases)   // BL31
	apiMux.HandleFunc("/api/device-aliases/", api.handleDeviceAliases)  // BL31 (with name)
	// Sprint S5 (v3.9.0).
	apiMux.HandleFunc("/api/routing-rules", api.handleRoutingRules)             // BL20
	apiMux.HandleFunc("/api/routing-rules/test", api.handleRoutingRulesTest)    // BL20
	// Sprint S6 (v3.10.0) — BL24+BL25 autonomous PRD decomposition.
	apiMux.HandleFunc("/api/autonomous/config", api.handleAutonomousConfig)
	apiMux.HandleFunc("/api/autonomous/status", api.handleAutonomousStatus)
	apiMux.HandleFunc("/api/autonomous/prds", api.handleAutonomousPRDs)
	apiMux.HandleFunc("/api/autonomous/prds/", api.handleAutonomousPRDs)
	apiMux.HandleFunc("/api/autonomous/learnings", api.handleAutonomousLearnings)
	// BL221 (v6.2.0) — dedicated template endpoints.
	apiMux.HandleFunc("/api/autonomous/templates", api.handleAutonomousTemplates)
	apiMux.HandleFunc("/api/autonomous/templates/", api.handleAutonomousTemplates)
	// BL221 (v6.2.0) Phase 3 — scan config endpoint.
	apiMux.HandleFunc("/api/autonomous/scan/config", api.handleAutonomousScanConfig)
	// BL221 (v6.2.0) Phase 4 — type registry endpoint.
	apiMux.HandleFunc("/api/autonomous/types", api.handleAutonomousTypes)
	// Sprint S7 (v3.11.0) — BL33 plugin framework.
	apiMux.HandleFunc("/api/plugins", api.handlePlugins)
	apiMux.HandleFunc("/api/plugins/", api.handlePlugins)
	// Sprint S8 (v4.0.0) — BL117 PRD-DAG orchestrator.
	apiMux.HandleFunc("/api/orchestrator/config", api.handleOrchestratorConfig)
	apiMux.HandleFunc("/api/orchestrator/graphs", api.handleOrchestratorGraphs)
	apiMux.HandleFunc("/api/orchestrator/graphs/", api.handleOrchestratorGraphs)
	apiMux.HandleFunc("/api/orchestrator/verdicts", api.handleOrchestratorVerdicts)
	// Sprint S9 (v4.1.0) — BL171 datawatch-observer.
	apiMux.HandleFunc("/api/observer/stats", api.handleObserverStats)
	apiMux.HandleFunc("/api/observer/envelopes", api.handleObserverEnvelopes)
	// BL180 Phase 2 cross-host (v5.12.0) — federation-aware envelope
	// view: local + every peer with cross-peer Caller attribution.
	apiMux.HandleFunc("/api/observer/envelopes/all-peers", api.handleObserverEnvelopesAllPeers)
	apiMux.HandleFunc("/api/observer/envelope", api.handleObserverEnvelope)
	apiMux.HandleFunc("/api/observer/config", api.handleObserverConfig)
	// BL172 (S11) — peer registry routes (registered under one prefix
	// because the trailing path varies: "", "{name}", "{name}/stats").
	apiMux.HandleFunc("/api/observer/peers", api.handleObserverPeers)
	apiMux.HandleFunc("/api/observer/peers/", api.handleObserverPeers)
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
	apiMux.HandleFunc("/api/channel/history", api.handleChannelHistory)
	apiMux.HandleFunc("/api/update", api.handleUpdate)
	apiMux.HandleFunc("/api/update/check", api.handleUpdateCheck)
	apiMux.HandleFunc("/api/llm/claude/models", api.handleClaudeModels)
	apiMux.HandleFunc("/api/llm/claude/efforts", api.handleClaudeEfforts)
	apiMux.HandleFunc("/api/llm/claude/permission_modes", api.handleClaudePermissionModes)
	apiMux.HandleFunc("/api/quick_commands", api.handleQuickCommands)
	apiMux.HandleFunc("/api/channel/info", api.handleChannelInfo)
	apiMux.HandleFunc("/api/restart", api.handleRestart)
	apiMux.HandleFunc("/api/mcp/docs", api.handleMCPDocs)
	apiMux.HandleFunc("/api/ollama/models", api.handleOllamaModels)
	apiMux.HandleFunc("/api/openwebui/models", api.handleOpenWebUIModels)
	apiMux.HandleFunc("/api/interfaces", api.handleInterfaces)
	apiMux.HandleFunc("/api/schedules", api.handleSchedules)
	apiMux.HandleFunc("/api/stats", api.handleStats)
	apiMux.HandleFunc("/api/stats/kill-orphans", api.handleKillOrphans)
	apiMux.HandleFunc("/api/rtk/discover", api.handleRTKDiscover)
	// BL85 (v4.0.1) — RTK auto-update surface.
	apiMux.HandleFunc("/api/rtk/version", api.handleRTKVersion)
	apiMux.HandleFunc("/api/rtk/check", api.handleRTKCheck)
	apiMux.HandleFunc("/api/rtk/update", api.handleRTKUpdate)
	// v4.0.3 — mobile-client parity gaps.
	apiMux.HandleFunc("/api/backends/active", api.handleBackendsActive)     // issue #7
	apiMux.HandleFunc("/api/channels", api.handleChannels)                  // issue #8
	apiMux.HandleFunc("/api/channels/", api.handleChannels)                 // issue #8
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
	apiMux.HandleFunc("/api/memory/pin", api.handleMemoryPin)
	apiMux.HandleFunc("/api/memory/wakeup", api.handleMemoryWakeup)
	apiMux.HandleFunc("/api/memory/sweep_stale", api.handleMemorySweepStale)
	apiMux.HandleFunc("/api/memory/spellcheck", api.handleMemorySpellCheck)
	apiMux.HandleFunc("/api/memory/extract_facts", api.handleMemoryExtractFacts)
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
	// BL219 — tooling artifact lifecycle.
	apiMux.HandleFunc("/api/tooling/status", api.handleToolingStatus)
	apiMux.HandleFunc("/api/tooling/gitignore", api.handleToolingGitignore)
	apiMux.HandleFunc("/api/tooling/cleanup", api.handleToolingCleanup)
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

	// Serve PWA static files. Wrap in gzip middleware so JS / CSS /
	// Markdown / SVG ride a Content-Encoding: gzip when the client
	// supports it — typically 60-80% smaller over the wire for text
	// payloads. Doesn't affect binary assets (images already
	// compressed); the middleware skips them by content-type.
	mux.Handle("/", gzipFileServer(http.FileServer(http.FS(webSub))))

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
// SetSkillsManager (BL255 v6.7.0) — delegates to the Server.
func (s *HTTPServer) SetSkillsManager(m skillsManagerImpl) {
	if s.api != nil {
		s.api.SetSkillsManager(m)
	}
}

// SetIdentityManager (BL257 Phase 1 v6.8.0) — delegates to the Server.
func (s *HTTPServer) SetIdentityManager(m identityManager) {
	if s.api != nil {
		s.api.SetIdentityManager(m)
	}
}

// SetAlgorithmTracker (BL258 v6.9.0) — delegates to the Server.
func (s *HTTPServer) SetAlgorithmTracker(t algorithmTracker) {
	if s.api != nil {
		s.api.SetAlgorithmTracker(t)
	}
}

// SetEvalsRunner (BL259 P1 v6.10.0) — delegates to the Server.
func (s *HTTPServer) SetEvalsRunner(r evalsRunner) {
	if s.api != nil {
		s.api.SetEvalsRunner(r)
	}
}

// SetCouncilOrchestrator (BL260 v6.11.0) — delegates to the Server.
func (s *HTTPServer) SetCouncilOrchestrator(o councilOrchestrator) {
	if s.api != nil {
		s.api.SetCouncilOrchestrator(o)
	}
}

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

// RegisterReloader (v5.27.2) registers a subsystem-specific
// reload function reachable via POST /api/reload?subsystem=<name>.
// Used by main.go at startup to wire plugins / comm / memory / etc.
// hot-reloaders without forcing a full daemon restart.
func (s *HTTPServer) RegisterReloader(name string, fn func() error) {
	s.api.RegisterReloader(name, fn)
}

// ReloadSubsystem (v5.27.2) fires the per-subsystem reloader.
// Empty name = full config reload.
func (s *HTTPServer) ReloadSubsystem(name string) ReloadResult {
	return s.api.ReloadSubsystem(name)
}

// SetDeviceStore (issue #1) wires the mobile push device registry.
func (s *HTTPServer) SetDeviceStore(store *devices.Store) {
	s.api.SetDeviceStore(store)
}

// SetAuditLog (BL9) wires the operator audit log for /api/audit.
func (s *HTTPServer) SetAuditLog(l *audit.Log) {
	s.api.SetAuditLog(l)
}

// SetSecretsStore (BL242) wires the centralized secrets store for /api/secrets.
func (s *HTTPServer) SetSecretsStore(st secretsStore) {
	s.api.SetSecretsStore(st)
}

// SetTailscaleClient (BL243) wires the Tailscale client for /api/tailscale/*.
func (s *HTTPServer) SetTailscaleClient(c tailscaleClient) {
	s.api.tailscaleClient = c
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
// installFn receives a progress callback to fan progress out over the
// WebSocket hub so the PWA can render a progress bar (v4.0.6).
func (s *HTTPServer) SetUpdateFuncs(installFn func(version string, progress func(downloaded, total int64)) error, latestFn func() (string, error)) {
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

// SetAutonomousAPI (BL24+BL25) wires the autonomous PRD-decomposition
// manager for REST endpoints. Nil disables /api/autonomous/* (handlers
// return 503).
func (s *HTTPServer) SetAutonomousAPI(api AutonomousAPI) {
	s.api.SetAutonomousAPI(api)
}

// SetGitTokenMinter (v5.26.24) wires the BL113 token broker for
// daemon-side clone. Optional — when nil, clones use the
// DATAWATCH_GIT_TOKEN env / local-creds fallback (v5.26.22 path).
func (s *HTTPServer) SetGitTokenMinter(m GitTokenMinter) {
	s.api.SetGitTokenMinter(m)
}

// BroadcastPRDUpdate (v5.24.0) sends a `MsgPRDUpdate` WS message to
// every connected client. main.go binds this to the autonomous
// Manager's OnPRDUpdate callback so the PWA Autonomous tab reloads
// without manual Refresh on every persist (operator-reported v5.22.0).
// Payload shape: `{prd_id, status?, deleted?}`.
func (s *HTTPServer) BroadcastPRDUpdate(payload map[string]any) {
	if s.hub == nil {
		return
	}
	s.hub.Broadcast(MsgPRDUpdate, payload)
}

// SetPluginsAPI (BL33) wires the plugin registry for REST endpoints.
// Nil disables /api/plugins/* (handlers return 503).
func (s *HTTPServer) SetPluginsAPI(api PluginsAPI) {
	s.api.SetPluginsAPI(api)
}

// SetOrchestratorAPI (BL117) wires the PRD-DAG orchestrator for REST
// endpoints. Nil disables /api/orchestrator/*.
func (s *HTTPServer) SetOrchestratorAPI(api OrchestratorAPI) {
	s.api.SetOrchestratorAPI(api)
}

// SetObserverAPI (BL171) wires the observer subsystem for REST
// endpoints. Also upgrades /api/stats to the v2 shape when the
// caller sends ?v=2 or Accept-Version: 2. Nil leaves /api/stats
// on the v1 collector.
func (s *HTTPServer) SetObserverAPI(api ObserverAPI) {
	s.api.SetObserverAPI(api)
}

// RegisterNativePlugin appends a built-in subsystem entry to
// /api/plugins so the PWA can list it alongside subprocess plugins.
func (s *HTTPServer) RegisterNativePlugin(p NativePlugin) {
	s.api.RegisterNativePlugin(p)
}

// SetPeerRegistry (BL172) wires the Shape B / C peer registry. Nil
// disables /api/observer/peers/* (handlers return 503).
func (s *HTTPServer) SetPeerRegistry(r PeerRegistryAPI) {
	s.api.SetPeerRegistry(r)
}

// SetFederationSelfName — S14a (v4.8.0) loop-prevention: the
// peer-push handler rejects pushes whose chain already contains
// this primary's name. Empty disables the check.
func (s *HTTPServer) SetFederationSelfName(name string) {
	s.api.SetFederationSelfName(name)
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
			redirectSrv := &http.Server{
				Handler:           s.redirectToTLSHandler(s.cfg.TLSPort),
				ReadTimeout:       5 * time.Second,
				ReadHeaderTimeout: 5 * time.Second,
			}
			go func(l net.Listener, a string) {
				errCh <- redirectSrv.Serve(l)
			}(httpListener, httpAddr)
			fmt.Printf("datawatch server listening on http://%s (redirects to TLS port %d)\n", httpAddr, s.cfg.TLSPort)

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

// gzipFileServer wraps a static-file handler so text/* + JSON +
// JavaScript + CSS + SVG + Markdown payloads ride a Content-Encoding:
// gzip when the client supports it. Binary assets (PNG, JPG, fonts)
// pass through untouched — they're already compressed and re-gzipping
// just burns CPU for no win.
//
// The pool keeps gzip writers reusable so per-request allocation stays
// flat. We use the default compression level (6) — the marginal gain
// from level 9 isn't worth the extra CPU on small text payloads.
func gzipFileServer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			next.ServeHTTP(w, r)
			return
		}
		// Skip already-compressed extensions. Cheap suffix check.
		path := r.URL.Path
		switch {
		case strings.HasSuffix(path, ".png"),
			strings.HasSuffix(path, ".jpg"),
			strings.HasSuffix(path, ".jpeg"),
			strings.HasSuffix(path, ".gif"),
			strings.HasSuffix(path, ".webp"),
			strings.HasSuffix(path, ".woff"),
			strings.HasSuffix(path, ".woff2"),
			strings.HasSuffix(path, ".ico"),
			strings.HasSuffix(path, ".gz"):
			next.ServeHTTP(w, r)
			return
		}
		gz := gzipPool.Get().(*gzip.Writer)
		defer gzipPool.Put(gz)
		gz.Reset(w)
		defer gz.Close()
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Set("Vary", "Accept-Encoding")
		// We're rewriting the body; let downstream regenerate
		// Content-Length implicitly via chunked encoding.
		w.Header().Del("Content-Length")
		next.ServeHTTP(&gzipResponseWriter{ResponseWriter: w, Writer: gz}, r)
	})
}

var gzipPool = sync.Pool{
	New: func() any { return gzip.NewWriter(io.Discard) },
}

type gzipResponseWriter struct {
	http.ResponseWriter
	Writer *gzip.Writer
}

func (g *gzipResponseWriter) Write(b []byte) (int, error) { return g.Writer.Write(b) }
