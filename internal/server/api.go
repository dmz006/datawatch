package server

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/dmz006/datawatch/internal/capacity"
	"github.com/dmz006/datawatch/internal/planrecords"
	"github.com/dmz006/datawatch/internal/tagregistry"
	"github.com/dmz006/datawatch/internal/trace"
	"io"
	"log"
	"math"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/dmz006/datawatch/internal/acme"
	"github.com/dmz006/datawatch/internal/agents"
	"github.com/dmz006/datawatch/internal/alerts"
	"github.com/dmz006/datawatch/internal/apns"
	"github.com/dmz006/datawatch/internal/audit"
	"github.com/dmz006/datawatch/internal/auth"
	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/channel"
	"github.com/dmz006/datawatch/internal/compute"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/council"
	"github.com/dmz006/datawatch/internal/devices"
	"github.com/dmz006/datawatch/internal/federation"
	"github.com/dmz006/datawatch/internal/fingerprint"
	"github.com/dmz006/datawatch/internal/gitguard"
	"github.com/dmz006/datawatch/internal/inference"
	"github.com/dmz006/datawatch/internal/llm"
	"github.com/dmz006/datawatch/internal/llm/backends/ollama"
	"github.com/dmz006/datawatch/internal/llm/backends/opencode"
	"github.com/dmz006/datawatch/internal/llm/backends/openwebui"
	"github.com/dmz006/datawatch/internal/memory"
	"github.com/dmz006/datawatch/internal/mergepipeline"
	"github.com/dmz006/datawatch/internal/messaging"
	"github.com/dmz006/datawatch/internal/metrics"
	"github.com/dmz006/datawatch/internal/pipeline"
	"github.com/dmz006/datawatch/internal/plugins"
	"github.com/dmz006/datawatch/internal/profile"
	"github.com/dmz006/datawatch/internal/proxy"
	"github.com/dmz006/datawatch/internal/requestwatch"
	"github.com/dmz006/datawatch/internal/router"
	"github.com/dmz006/datawatch/internal/rtk"
	"github.com/dmz006/datawatch/internal/secrets"
	"github.com/dmz006/datawatch/internal/server/multiserver"
	"github.com/dmz006/datawatch/internal/session"
	"github.com/dmz006/datawatch/internal/shutdowncause"
	"github.com/dmz006/datawatch/internal/stats"
	"github.com/dmz006/datawatch/internal/supervisorloop"
	"github.com/dmz006/datawatch/internal/tooling"
	"github.com/dmz006/datawatch/internal/usagecalc"
	"github.com/dmz006/datawatch/internal/vision"
	"github.com/dmz006/datawatch/internal/websearch"
	"github.com/dmz006/datawatch/internal/workerprofile"
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
	// SetPinned (Mempalace QW#2, v5.26.70) flags a memory as
	// always-surface in L1. Backends that don't support pinning
	// return ErrNamespaceUnsupported (which the handler maps to 501).
	SetPinned(id int64, pinned bool) error
	// SetTags (GH#192 D78a) replaces a memory's comma-separated tag
	// list. Same unsupported-backend contract as SetPinned.
	SetTags(id int64, tags string) error
	// WakeUpBundle (v5.26.71) returns the composed L0+L1+L4+L5
	// wake-up context for the supplied (project, agent) pair.
	// Used by GET /api/memory/wakeup so smoke + operator tooling
	// can read what an agent would see at session start.
	WakeUpBundle(projectDir, selfAgentID, parentAgentID, parentNamespace string) string
	// v6.0.0 — mempalace bundle: maintenance + auxiliary surfaces.
	SweepStale(olderThanDays int, dryRun bool) (map[string]interface{}, error)
	SpellCheckText(text string, extra []string) []map[string]interface{}
	ExtractFactsText(text string) []map[string]interface{}
	SchemaVersion() string
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

// MCPSamplingLogEntry mirrors a single entry from the sampling ring buffer.
type MCPSamplingLogEntry struct {
	MCPSamplingResult
	RequestPreview string `json:"request_preview"`
}

// mcpSamplingAPI is the interface for daemon-initiated sampling (BL302 S3).
// The concrete implementation is *mcp.SamplingDispatcher wired from main.go.
type MCPSamplingAPI interface {
	Sample(ctx context.Context, req MCPSamplingRequest) (*MCPSamplingResult, error)
	Log() []MCPSamplingLogEntry
}

// mcpSamplingRequest mirrors mcp.SamplingRequest without the import cycle.
type MCPSamplingRequest struct {
	Trigger      string               `json:"trigger"`
	Messages     []MCPSamplingMessage `json:"messages"`
	SystemPrompt string               `json:"system_prompt,omitempty"`
	MaxTokens    int                  `json:"max_tokens,omitempty"`
}

// mcpSamplingMessage mirrors mcp.SamplingMessage without the import cycle.
type MCPSamplingMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// mcpSamplingResult mirrors mcp.SamplingResult without the import cycle.
type MCPSamplingResult struct {
	Trigger    string    `json:"trigger"`
	Content    string    `json:"content"`
	Model      string    `json:"model,omitempty"`
	StopReason string    `json:"stop_reason,omitempty"`
	LatencyMs  int64     `json:"latency_ms"`
	Timestamp  time.Time `json:"timestamp"`
	Error      string    `json:"error,omitempty"`
}

// mcpElicitationAPI is the interface for daemon-initiated elicitation (BL302 S3).
// The concrete implementation is *mcp.ElicitationDispatcher wired from main.go.
type MCPElicitationAPI interface {
	Elicit(ctx context.Context, req MCPElicitationRequest) (*MCPElicitationResult, error)
	Schemas() map[string]map[string]any
}

// mcpElicitationRequest mirrors mcp.ElicitationRequest without the import cycle.
type MCPElicitationRequest struct {
	Schema  string   `json:"schema"`
	Message string   `json:"message"`
	Options []string `json:"options,omitempty"`
}

// mcpElicitationResult mirrors mcp.ElicitationResult without the import cycle.
type MCPElicitationResult struct {
	Action  string         `json:"action"`
	Content map[string]any `json:"content,omitempty"`
}

// mcpBridgeAPI is the subset of mcp.Server used by the channel-bridge REST surface.
// The concrete implementation is wired from main.go via SetMCPBridge.
type mcpBridgeAPI interface {
	// MCPToolsJSON returns all registered MCP tools serialised as JSON.
	MCPToolsJSON() ([]byte, error)
	// MCPCallJSON invokes a named tool with the given args and returns the result as JSON.
	MCPCallJSON(ctx context.Context, name string, args map[string]any) ([]byte, error)
	// BL302 S1 — resource surface.
	ResourcesJSON() ([]byte, error)
	ResourceReadJSON(ctx context.Context, uri string) ([]byte, error)
	ResourceTemplatesJSON() ([]byte, error)
	// BL302 S4 — prompt surface.
	PromptsListJSON() []byte
	PromptsGetJSON(ctx context.Context, name string, args map[string]string) ([]byte, error)
}

// startTime records when the daemon started (for uptime calculation).
var startTime = time.Now()

// Version is set at build time. The server package uses this for /api/health and /api/info.
var Version = "9.172.14"

// broadcastSessionsAsync pushes the session-list change to every client
// without blocking the caller. The default-view sessions are selected with
// the cheap state / UpdatedAt check and copied under the store lock here,
// on the caller's goroutine: the monitor goroutines keep writing the live
// sessions, and marshalling them from the broadcast goroutine would race
// with those writes. Nothing outside the default view is copied, and
// nothing at all when no client is connected. The goroutine is tracked in
// broadcastWG so a test can wait for it before mutating or tearing down
// the sessions it marshals.
func (s *Server) broadcastSessionsAsync() {
	if s.hub == nil {
		return
	}
	if s.hub.ClientCount() == 0 {
		s.hub.resetSessionSnapshot()
		return
	}
	view, held := s.hub.defaultViewSnapshot()
	if held == nil {
		return
	}
	s.broadcastWG.Add(1)
	go func() {
		defer s.broadcastWG.Done()
		s.hub.broadcastSessionList(view, held)
	}()
}

// Server holds all HTTP handler dependencies
type Server struct {
	// traceStore feeds the additive trace_usage field of the cost report
	// (nil when tracing is not wired).
	traceStore *trace.Store
	hub        *Hub
	manager    *session.Manager
	hostname   string
	token      string
	// broadcastWG tracks the fire-and-forget sessions broadcasts started by
	// broadcastSessionsAsync so a test can wait for them.
	broadcastWG sync.WaitGroup
	// peerClients caches one TLS-configured HTTP client per federation peer,
	// keyed by peer name; peerClientsMu guards it.
	peerClientsMu sync.Mutex
	peerClients   map[string]peerClientEntry
	// SEC-016 — tokenMu guards token/oldToken/oldTokenExpiry, which
	// POST /api/auth/rotate-token mutates live (hot-swap, no restart)
	// while fedAuthMiddleware reads them on every request. oldToken stays
	// valid until oldTokenExpiry (a 60s grace window from rotation) so a
	// client that already has the previous token isn't cut off mid-flight;
	// after that it's hard-revoked, same as if it had never existed.
	tokenMu           sync.RWMutex
	oldToken          string
	oldTokenExpiry    time.Time
	availableBackends []string // registered LLM backend names
	cfg               *config.Config
	cfgPath           string
	// dataDir is this instance's explicit data-directory override (set by
	// the daemon at startup; empty in bare test fixtures). statePath
	// resolves it first, then cfg.DataDir, then the config default.
	dataDir string
	// configAudit (BL110 extension) — the shared config-write audit sink
	// (surface "rest"); nil means REST config writes produce no audit
	// record. One audited record per PUT /api/config or PUT
	// /api/cost/rates from session or peer callers (admin excluded — see
	// auditConfigPut).
	configAudit        *federation.ConfigAuditor
	schedStore         *session.ScheduleStore
	exitHookStore      *session.ExitHookStore      // BL356
	queueStore         *session.QueueStore         // BL357
	discussionSubStore *session.DiscussionSubStore // BL358
	resultStore        *session.ResultStore        // BL360
	// scheduleResultStore — per-schedule structured result history backing
	// the result panel (POST /api/sessions/{id}/result, GET /api/schedule-results*).
	scheduleResultStore *session.ScheduleResultStore
	cmdLib              *session.CmdLibrary
	alertStore          *alerts.Store
	// mergeShip overrides the merge run route's Ship step (tag/push/deploy
	// and hook commands); nil runs the production ExecShipRunner. Unit tests
	// set it to keep those steps off any real remote.
	mergeShip      mergepipeline.ShipRunner
	filterStore    *session.FilterStore
	statsCollector *stats.Collector
	// F10 sprint 2: profile stores. Wired from main.go via setters so
	// unit tests can leave them nil (handlers return 503).
	projectStore *profile.ProjectStore
	clusterStore *profile.ClusterStore
	// F10 sprint 3: agent lifecycle manager.
	agentMgr *agents.Manager

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

	// BL368 — vision describer for /api/vision/describe.
	visioner visionSurface

	// BL9 — operator audit log.
	auditLog *audit.Log

	// GH#201 — HTTP access / WS lifecycle / auth-failure log.
	accessLog              *audit.Log
	accessLogEventCount    int64 // atomic
	accessLogAuthFailCount int64 // atomic

	// BL242 — centralized secrets store.
	secretsStore secretsStore

	// GH#203 — external-service token store (persistent, operator-minted
	// tokens for independent services that are neither spawned F10 agents
	// nor federation peers).
	serviceTokenStore *secrets.ServiceTokenStore

	// BL243 — Tailscale k8s sidecar client (nil when not configured).
	tailscaleClient tailscaleClient

	// BL255 v6.7.0 — skills manager (nil when not configured).
	skillsMgr skillsManager

	// BL257 Phase 1 v6.8.0 — operator identity / Telos manager (nil
	// when identity is disabled or no on-disk file present).
	identityMgr identityManager

	// BL258 v6.9.0 — Algorithm Mode 7-phase tracker (nil when disabled).
	algorithmTracker algorithmTracker

	// BL259 P1 v6.10.0 — Evals framework runner (nil when disabled).
	evalsRunner evalsRunner

	// BL260 v6.11.0 — Council Mode orchestrator (nil when disabled).
	councilOrch councilOrchestrator

	// BL297 v6.22.3 — Council persona-wizard drafts store (SQLite).
	councilDrafts *council.DraftsStore

	// v7.0.0 S1 — ComputeNode registry (nil when disabled).
	computeReg *compute.Registry
	// headroomStore is the daemon's one verifier headroom-measurement store
	// (nil until wired); the verifier_slots write gate reads it.
	headroomStore *compute.HeadroomStore
	// headroomProbe overrides the on-demand measurement transport (nil = the
	// Ollama probe over the observer). headroomClock fixes the measurement
	// clock for tests (nil = serverNow).
	headroomProbe compute.HeadroomProbe
	headroomClock func() time.Time
	// servedModels reads and caches what each compute node serves; modelHostCheck
	// answers the served-models route's model check. Both are nil until wired.
	servedModels   *compute.ServedModelsCache
	modelHostCheck autonomous.ModelHostCheckFn

	// Per-node usage cache for the node:<name> capacity pools. Lazily
	// created by (s *Server) nodeUsageCache(); see node_usage.go.
	nodeUsageCacheOnce sync.Once
	nodeUsageCacheVal  *nodeUsageCache

	// Shared usage-calculator readings series (keeper/peer federation). Lazily
	// created by (s *Server) usageSeries(); see usage_readings.go.
	usageSeriesOnce     sync.Once
	usageSeriesCacheVal *usageSeriesCache

	// usageCalc is the usage-calculator advice service (nil = not wired);
	// see usage_calculator_advice.go.
	usageCalc *usagecalc.Service
	// autoLanesSpecialCounter counts open items at a priority threshold for
	// the auto-lanes status (nil = 0); see usage_autolanes.go.
	autoLanesSpecialCounter func(threshold int) int
	// burnGuard is the burn guard runner (burn_guard.go); nil until AttachBurnGuard.
	burnGuard *BurnGuard

	// v7.0.0 S2 — LLM-inference registry + dispatcher (nil when disabled).
	inferenceReg   *inference.Registry
	roleMonitor    inference.RoleContentionMonitor
	capacityLedger *capacity.Ledger
	// verifierNodeStatus is the daemon's verifier admission state reader
	// (nil = not wired); see VerifierNodeStatus in compute.go.
	verifierNodeStatus VerifierNodeStatusReader
	// capacityWatchdogThreshold lowers the ledger lock watchdog threshold in
	// tests; zero uses capacity.DefaultLockWatchdogThreshold.
	capacityWatchdogThreshold time.Duration
	// usageBackoff is the per-backend usage back-off registry (BL428 Phase 4).
	usageBackoff *autonomous.Backoff
	// budgetLedger is the per-LLM dollar budget ledger (nil = not wired).
	budgetLedger *autonomous.BudgetLedger
	// capacityAdmit gates an interactive (non-autonomous) session start on
	// the same node:/llm: capacity pools an autonomous task would use for
	// the same backend+model — nil when capacity isn't wired at all.
	// Autonomous task spawns skip this (they already admit via the
	// executor's own m.admit()); see handleStartSession's req.OneShot check.
	capacityAdmit   CapacityAdmitFn
	capacityBind    CapacityBindFn
	capacityRelease CapacityReleaseFn
	inferenceDisp   *inference.Dispatcher

	// summarizerSvc is the response summarizer (nil when disabled).
	// Wired from main.go after daemon init via SetSummarizerSvc.
	summarizerSvc SummarizerSvc

	// v7.0.0 S4 — SSE hub for live-update streams (council events,
	// future automata progress, observer peer state, etc.).
	sseHub *SSEHub

	// v7.0.0 S5 — memory backend for scope-hierarchy operations
	// (recall/borrow/seed/promote). Optional; when nil the
	// /api/memory/scopes/* endpoints return 503.
	memoryBackend memory.Backend

	linkMu      sync.Mutex
	linkStreams map[string]chan string // stream_id -> event channel

	// Backend version cache — avoids slow serial exec calls on every /api/backends request.
	versionCacheMu sync.RWMutex
	versionCache   interface{} // []backendInfo
	versionCacheAt time.Time

	// restartFn is wired from main.go; it restarts the daemon in-place.
	restartFn func()
	// restartForceFn is the force-aware restart wired by SetRestartFuncForce
	// (the restart guard). When set, POST /api/restart and the update restart
	// call it with the operator's force flag; restartFn stays for the callers
	// that have no force concept (router, MCP, ACME).
	restartForceFn func(force bool)

	// acmeManager (BL397) is wired from main.go when acme.enabled is
	// true. nil when ACME is disabled — every acmeManager consumer must
	// nil-check. See docs/plans/historical-plans/2026-10-06-bl397-native-acme-letsencrypt.md.
	acmeManager *acme.Manager

	// apnsDispatcher (BL397 Phase 4 / BL335) is wired from main.go when
	// push.apns.enabled is true. nil when disabled.
	apnsDispatcher *apns.Dispatcher

	// reloaders (v5.27.2) maps subsystem name → reload function.
	// Callers register via Server.RegisterReloader at startup so
	// /api/reload?subsystem=<name> can hot-reload that subsystem
	// without restarting the daemon.
	reloaders map[string]func() error

	// mcpDocsFunc returns MCP tool documentation (wired from main.go when MCP is enabled).
	mcpDocsFunc func() interface{}

	// mcpBridge is the daemon MCP server; wired at startup via SetMCPBridge.
	// Provides /api/mcp/tools and /api/mcp/call for the channel bridge proxy.
	mcpBridge mcpBridgeAPI

	// mcpTokenConfigured is cfg.MCP.Token != "" (SEC-002) — wired from
	// main.go via SetMCPAuthRequired. Used only to compute
	// /api/health's mcp_auth_required; the MCP SSE listener itself falls
	// back to server.token when mcp.token is empty (see internal/mcp).
	mcpTokenConfigured bool

	// mcpSSEStateFn returns the MCP SSE listener state
	// (disabled|retrying|listening|failed). Wired from main.go via
	// SetMCPSSEStateFn; nil when SSE is off or never wired — /api/health
	// reports "disabled" in that case so operators read state from the
	// health endpoint rather than logs.
	mcpSSEStateFn func() string

	// nonces mints/consumes SEC-006 short-lived single-use nonces for
	// browser requests that can't attach an Authorization header.
	nonces *nonceStore

	// sessionTokens (Design A3) resolves a per-session scoped
	// credential to its capability list. nil-safe: a nil store simply
	// means no session has one yet (treated the same as "unknown
	// token" by fedAuthMiddleware, falling through to admin/peer
	// checks). Wired from main.go via SetSessionTokenStore.
	sessionTokens *auth.SessionTokenStore
	// traceRecorder receives session trace events (observe only); nil until wired.
	traceRecorder SessionTraceRecorder

	// BL302 S3 — sampling/elicitation dispatchers (nil when MCP disabled).
	// Wired from main.go via SetMCPSamplingDispatcher / SetMCPElicitationDispatcher.
	mcpSamplingDisp    MCPSamplingAPI
	mcpElicitationDisp MCPElicitationAPI

	// installUpdate is wired from main.go; it downloads and installs a new binary.
	// The progress callback is invoked with (downloaded, total) byte counts
	// while the asset is streaming — total may be 0 if the server didn't
	// send Content-Length.
	installUpdate func(version string, progress func(downloaded, total int64)) error
	// latestVersion returns the latest available release tag (without "v" prefix).
	latestVersion func() (string, error)

	// testMessageHandler is an optional function that routes a simulated incoming
	// message through the router, enabling comm channel testing via the API.
	// The attribution parameter carries the calling credential and its
	// config:write decision (see TestMessageAttribution).
	testMessageHandler func(text string, at TestMessageAttribution) []string

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
	// costReportLedger — spend ledger read by GET /api/cost/report.
	costReportLedger spendLedgerReader

	// Defect queue backend + intake decision log (defect_queue.go); nil until
	// SetDefectQueue wires them, and the defect endpoints answer 503 then.
	defectQueue DefectQueueBackend
	defectLog   autonomous.IntakeLog
	// classifierStatus reads the classifier capability probes and calibration
	// records; nil until the daemon wires it. classifierMu serialises
	// allowlist additions.
	classifierStatus ClassifierStatusSource
	classifierMu     sync.Mutex
	// Requirements engine (internal/autonomous or a test fake) + the
	// automation backend summary provider it reports over. Nil =
	// /api/automation/requirements-report reports 503.
	requirementsReporter requirementsReporter
	automationBackends   automationBackendsSummary

	// requestWatch reads the in-flight model request observations; nil until
	// SetRequestWatch wires it, and the request-watch route answers 503 then.
	requestWatch    requestwatch.Reader
	requestWatchCfg func() config.RequestWatchConfig

	// overlapSearch runs the search-before-create check on the create route.
	// Nil uses the autonomous finder over autonomousMgr's Automata; tests
	// substitute a fake that can fail or panic.
	overlapSearch overlapCreateSearch

	// tagRegistry is the shared Automata tag registry, wired from main.go
	// (SetTagRegistry). Nil keeps the tag routes on their local write path.
	// tagRegistryStart starts its sync loop.
	tagRegistry      *tagregistry.Registry
	tagRegistryStart func()

	// autonomousSettings reads the autonomous settings the tag routes use, by
	// config key. Nil, or a key it does not set, means the built-in default
	// (see autonomous.ResolveAttentionStatuses).
	autonomousSettings autonomous.SettingLookup

	// progressNow is the clock the progress routes read; nil means time.Now.
	// Tests set it to a fixed time.
	progressNow func() time.Time
	// timelineNow is the clock the timeline route reads; nil means time.Now.
	// Tests set it to a fixed time.
	timelineNow func() time.Time

	// BL427 phase 3 — lifecycle event subscription store for
	// /api/autonomous/subscriptions. Wired from main.go alongside the
	// subscription engine; nil (default) makes the handlers return 503.
	autonomousSubs LifecycleSubscriptionsAPI
	// questionStore — the operator question channel (raise + read)
	// backing /api/questions. Wired from main.go alongside the daemon's
	// data; nil (default) makes the handlers return 503.
	questionStore QuestionsAPI
	// decisionFollowUps — records the follow-up an operator asks for when
	// answering a decision record question; wired by main.go via
	// SetDecisionFollowUps; nil (default) = a follow_up in an answer is
	// refused with 503.
	decisionFollowUps DecisionFollowUpAPI
	// decisionRecords — the decision record service behind
	// /api/autonomous/decision-records; wired by main.go via
	// SetDecisionRecords; nil (default) makes the routes return 503.
	decisionRecords DecisionRecordsAPI

	// planRecords — the plans-and-backlog record store behind
	// /api/plan-records. Nil answers 503.
	planRecords *planrecords.Store
	// planPoints caches per-Automaton points for the plans read API;
	// planPointsSource and planTasksSource replace the daemon reads in tests.
	planPoints       planPointsCache
	planPointsSource func() map[string]float64
	planTasksSource  func(automatonID string) ([]planTaskRow, bool)
	// planItemSource replaces the Automaton/Automation lookup of the plans
	// write API in tests.
	planItemSource func(id string) (planrecords.MigrateRef, bool)
	// questionRaiseNotifier — the raise fan-out to the messaging group,
	// wired by main.go via SetQuestionRaiseNotifier; nil (default) = no
	// messaging fan-out (the WS + alert parts do not need it).
	questionRaiseNotifier QuestionRaiseNotifier
	// questionNotifies — counts the raise fan-out goroutines in flight
	// (see WaitQuestionNotifies) so a caller can block until the latest
	// raise's alert-write side effect has completed.
	questionNotifies sync.WaitGroup
	// questionAnswerReturnPath — the per-question opt-in session line on
	// answer (one line of DATAWATCH_ANSWER: into the asking session); wired
	// by main.go via SetQuestionAnswerReturnPath; nil (default) = no
	// session line (the event plus polled GET still carry the answer).
	questionAnswerReturnPath *autonomous.QuestionAnswerReturnPath
	// questionEventsSwitch applies autonomous.questions.enabled to the
	// store's lifecycle-event sink (wired when on, dropped when off). The
	// daemon binds it at start; a config write that changes the switch calls
	// it, so a live enable attaches the sink with no restart. Nil = none.
	questionEventsSwitch func(enabled bool)
	// mcp_queue sink read side for /api/autonomous/events; nil → 503.
	autonomousEvents PendingEventsAPI
	// supervisorLoop serves the supervisor-loop run history and manual runs.
	supervisorLoop SupervisorLoopControl
	// peerEvents backs /api/autonomous/peer-events (nil: 503).
	peerEvents PeerEventControl
	// supervisorWake is the record behind GET /api/supervisor-loop/wake.
	supervisorWake *supervisorloop.WakeRecord
	// BL427 phase 5 — receiving side of peer forwarding for
	// /api/autonomous/events/forward; nil → 503.
	autonomousFwd ForwardedEventsAPI

	// BL328 — in-flight async decompose jobs. key=prdID (string),
	// value=*decomposeJob. Jobs are in-memory only; not persisted across
	// daemon restarts (TS-649 is a future stretch goal).
	decomposeJobs sync.Map

	// BL33 — plugin framework registry (Sprint S7, v3.11.0).
	// Wired from main.go; nil when plugins.enabled=false. Handlers
	// return 503 in that case.
	pluginsAPI PluginsAPI

	// BL403 — plugin widget values. All three are nil until main.go
	// wires them; /api/plugins/{name}/widgets* return 503 in that case.
	pluginWidgetSrc PluginWidgetSource
	widgetStore     *plugins.WidgetStore
	pluginTokens    PluginTokensAPI

	// Per-plugin token-usage listing (GET /api/plugins/{name}/tokens):
	// manifest secret-ref scan + service-token names. Nil until main.go
	// wires it; the handler returns 503 in that case.
	pluginTokensSrc PluginTokensSource

	// Per-service model-use report (GET /api/plugins/{name}/models):
	// models used, Ollama node + memory + context per model, and the
	// two parameterised findings (small_model_large_context,
	// node_overloaded). pluginModelsSrc is a test/override injection;
	// nil uses the default source built from pluginTokensSrc + computeReg
	// + secretsStore. The model-node events task starts in
	// StartModelNodeEvents and owns the model_node_unreachable raise via
	// its recorder (pluginModelNodeRec when wired, else the process
	// default).
	pluginModelsSrc        PluginModelsSource
	pluginModelNodeRec     *plugins.ServiceEventRecorder
	modelNodeEvents        *plugins.ModelNodeEvents
	modelNodeEventInterval time.Duration

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

	// #54 — smoke-run cross-instance forwarding.
	// When non-empty, every smoke progress write is also POSTed to
	// {smokeForwardURL}/api/smoke/progress asynchronously.
	smokeForwardURL   string
	smokeForwardToken string

	// BL312 S1 — runtime-mutable multi-server registry.
	// Nil when not wired; handleBL312Servers falls back to the legacy
	// static-list handler in that case.
	serverStore *multiserver.Store

	// BL316 — custom federation capability groups store.
	// Nil when not wired; group endpoints return builtin-only lists.
	fedGroupStore *federation.GroupStore

	// Observer pull protocol — the PullCollector wired to the federation
	// peer store by NewObserverPullCollector. Nil until wired; while nil
	// syncObserverPullTargets is a no-op.
	observerPull   *observerPull
	observerPullMu sync.Mutex // guards the one-time wiring in StartObserverPull

	// Fingerprint exchange (docs/plans/2026-10-14-concurrency-and
	// budget-design.md §3c) — the exporter behind
	// GET /api/federation/fingerprints, wired from main.go when the
	// compute/inference registries exist. Nil (unwired) answers an
	// empty list.
	fingerprintExporter *fingerprint.Exporter

	// Fingerprint match records surfaced for operator decision
	// (docs/plans/2026-10-14-concurrency-and-budget-design.md §3c) —
	// behind /api/federation/fingerprint-matches. Nil (unwired) answers
	// 503.
	fingerprintMatchStore *fingerprint.MatchStore

	// S14b — per-pod alert rules + observer-driven autoscaling store.
	// Nil when not wired; handlers return 503.
	alertRulesAPI AlertRulesAPI

	// BL334 T43a — derived encryption key from --secure / Argon2id.
	// Non-nil when the daemon is started with --secure. Used by handlers
	// that persist JSON files not managed by the secfile data-store layer
	// (discussion WAL, participants, channel routing). Must be zeroed on
	// daemon exit; main.go owns the zero via defer.
	encKey []byte

	// BL241 — Matrix backend handle (nil when Matrix is not configured).
	// Used by /api/matrix/status and /api/matrix/test.
	matrixBackend matrixBackendAPI

	// BL391 — multi-provider web search registry (nil when web_search is
	// disabled or no providers configured). Wired from main.go via
	// SetWebSearchRegistry; handlers return 503 when nil.
	websearchReg *websearch.Registry

	// BL391 — each WebSearch provider's api_key exactly as it appeared in
	// config.yaml at daemon startup (name -> literal value or an unresolved
	// "${secret:name}" ref), captured BEFORE secrets.ResolveConfig mutates
	// cfg.WebSearch.Providers[i].APIKey to the resolved plaintext secret in
	// place. s.cfg is the same struct the daemon resolves and runs with, so
	// without this snapshot, saving the config after any unrelated provider
	// edit (e.g. a plain enable/disable toggle) would persist the resolved
	// plaintext key to config.yaml, defeating the secrets store. See
	// saveWebSearchConfig in websearch.go, which restores the ref before
	// every save. Wired from main.go via SetWebSearchAPIKeyRefs.
	websearchAPIKeyRefs map[string]string
}

// SetWebSearchRegistry wires the shared *websearch.Registry (BL391) used by
// the /api/websearch/* REST handlers for stats/history/provider-test.
// SetTraceStore wires the trace store whose model-request and token totals
// the cost report carries as an additive field.
func (s *Server) SetTraceStore(st *trace.Store) { s.traceStore = st }

func (s *Server) SetWebSearchRegistry(r *websearch.Registry) { s.websearchReg = r }

// SetWebSearchAPIKeyRefs wires the pre-ResolveConfig snapshot of each
// provider's configured api_key (see the field doc above) so REST-triggered
// config saves never persist a resolved plaintext secret to config.yaml.
func (s *Server) SetWebSearchAPIKeyRefs(refs map[string]string) { s.websearchAPIKeyRefs = refs }

// matrixBackendAPI is the minimal interface the REST handlers need from the
// Matrix backend. Keeping it small avoids a hard dependency on the matrix
// package from the server package.
type matrixBackendAPI interface {
	Name() string
	Send(recipient, message string) error
	SelfID() string
}

// SetEncKey wires the Argon2id-derived key so file-persisted handlers can
// encrypt their data when --secure is active. Pass nil to disable encryption.
func (s *Server) SetEncKey(key []byte) { s.encKey = key }

// SetMatrixBackend wires the Matrix messaging backend for /api/matrix/* endpoints.
// Pass nil to disable (handlers return 503).
func (s *Server) SetMatrixBackend(b matrixBackendAPI) { s.matrixBackend = b }

// SetSmokeForward wires the cross-instance smoke-progress forwarder (#54).
func (s *Server) SetSmokeForward(url, token string) {
	s.smokeForwardURL = url
	s.smokeForwardToken = token
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
	CreatePRD(spec, projectDir, backend, model, effort string) (any, error)
	// CreatePRDWithParent is CreatePRD for an Automaton with a parent id; the
	// child inherits from the parent and explicit values win.
	CreatePRDWithParent(spec, projectDir, backend, model, effort, parentPRDID string) (any, error)
	GetPRD(id string) (any, bool)
	ListPRDs() []any
	Decompose(id string) (any, error)
	// Planning requeue (planning_requeue.go): decompose re-requested while
	// the Automaton is already planning — re-queue the queued planning run
	// on the current pool, or explain why the run was left alone.
	RestartPlanning(id string) (any, error)
	// BL328 — async decompose with per-story callback. Used by the
	// SSE-streaming handler. The callback is called for each story as
	// it becomes available; index is 0-based, total is total story count.
	DecomposeStreaming(id string, cb func(index, total int, story any)) (any, error)
	Run(id string) error
	Cancel(id string) error
	ListLearnings() []any

	// SessionIDsForPRD walks a PRD's tasks and returns every
	// Task.SessionID that's been scheduled. S13 follow-up — used by
	// the orchestrator handler to enrich graph nodes with per-node
	// ObserverSummary. Returns nil for unknown PRDs.
	SessionIDsForPRD(prdID string) []string

	// v8.20.1 — reset a cancelled PRD back to draft for re-run.
	ResetToDraft(id, actor string) (any, error)

	// BL221 (v6.2.0) — archive a terminal PRD (sets status to archived).
	Archive(id string) (any, error)

	// BL191 Q1 (v5.2.0) — review/approve gate.
	Approve(id, actor, note string) (any, error)
	Reject(id, actor, reason string) (any, error)
	RequestRevision(id, actor, note string) (any, error)
	EditTaskSpec(prdID, taskID, newSpec, actor string) (any, error)
	// EditTask is EditTaskSpec plus the read_only flag (nil = unchanged,
	// empty newSpec = spec unchanged), the report path, the verification
	// script and the script exemption reason (each empty = unchanged).
	EditTask(prdID, taskID, newSpec string, readOnly *bool, reportPath, verificationScript, verificationScriptReason, actor string) (any, error)
	// v8.23.0 — reset a failed, blocked or waiting-for-capacity task to
	// pending for retry (a waiting task needs no force — it is re-admitted
	// with a fresh wait budget). BL382 — force=true also requeues
	// completed/cancelled tasks.
	ResetTask(prdID, taskID, actor string, force bool) (any, error)
	// clean_restart — as ResetTask, and cleanRestart=true makes the next
	// attempt skip the restore of preserved attempt work.
	ResetTaskWithOptions(prdID, taskID, actor string, force, cleanRestart bool) (any, error)
	// Re-run the PRD-completion "rules-review" guardrail for a PRDBlocked
	// PRD whose work is fully done: the verdict and a
	// "guardrail_rejudge" Decision for the acting operator are recorded
	// in the decision log, a pass unblocks the PRD, a block keeps it
	// blocked, and a no-verdict failure returns an error.
	RejudgeCompletionGuardrail(prdID, actor, backend string) (any, error)
	// v8.36.6 — repair DependsOn titles to real IDs on an existing PRD.
	RepairDependsOn(prdID, actor string) (any, error)
	// BL382 — cancel an individual story or task without cancelling the whole PRD.
	CancelStory(prdID, storyID, actor, reason string) (any, error)
	CancelTask(prdID, taskID, actor, reason string) (any, error)
	// v5.26.32 — story title + description edit (parallels
	// EditTaskSpec; gate: needs_review / revisions_asked — EditTaskSpec
	// additionally accepts a blocked PRD for a failed/blocked task, BL421).
	EditStory(prdID, storyID, newTitle, newDescription, actor string) (any, error)

	// Structural edits — add/remove a story or task without re-decomposing.
	AddStory(prdID, title, description, actor string) (any, error)
	RemoveStory(prdID, storyID, actor string) (any, error)
	AddTask(prdID, storyID, title, spec, actor string, dependsOn []string) (any, error)
	AddTaskReadOnly(prdID, storyID, title, spec, actor string, dependsOn []string, readOnly bool, reportPath string) (any, error)
	RemoveTask(prdID, storyID, taskID, actor string) (any, error)

	// Phase 3 (v5.26.60) — per-story execution profile + approval.
	SetStoryProfile(prdID, storyID, profile, actor string) (any, error)
	ApproveStory(prdID, storyID, actor string) (any, error)
	RejectStory(prdID, storyID, actor, reason string) (any, error)
	// BL406 Phase 5 — per-task approval gate (GuidedModeSource="operator").
	ApproveTask(prdID, taskID, actor string) (any, error)
	// Phase 4 (v5.26.64) — file association.
	SetStoryFiles(prdID, storyID string, files []string, actor string) (any, error)
	SetTaskFiles(prdID, taskID string, files []string, actor string) (any, error)
	// Operator-reported 2026-10-06 — manual backfill for the
	// post-session-diff FilesTouched hook (existed since Phase 4 but was
	// never wired to a caller until this fix).
	RecordTaskFilesTouched(prdID, taskID string, files []string) (any, error)

	// BL191 Q2 — template instantiation.
	InstantiateTemplate(templateID string, vars map[string]string, actor string) (any, error)

	// BL203 (v5.4.0) — flexible LLM overrides at PRD + task level.
	// BL381 — adds story-level override (per-task → per-story → per-PRD → global).
	SetPRDLLM(prdID string, backend, effort, model, decompositionProfile, decompositionModel *string, actor string) (any, error)
	SetStoryLLM(prdID, storyID, backend, effort, model, actor string) (any, error)
	SetTaskLLM(prdID, taskID, backend, effort, model, actor string) (any, error)
	// BL447 — per-task ProjectDir override. Empty dir clears (inherit PRD dir).
	SetTaskProjectDir(prdID, taskID, dir, actor string) (any, error)
	// BL416 — guardrail judge LLM overrides. nil = leave unchanged, "" = clear.
	SetPRDGuardrailLLM(prdID string, backend, model *string, actor string) (any, error)
	SetTaskGuardrailLLM(prdID, taskID string, backend, model *string, actor string) (any, error)
	// Per-unit verification (judge) LLM overrides. nil = leave unchanged,
	// "" = clear. Precedence task > story > Automaton > global config.
	SetTaskVerificationLLM(prdID, taskID string, backend, model, effort *string, actor string) (any, error)
	SetStoryVerificationLLM(prdID, storyID string, backend, model, effort *string, actor string) (any, error)
	SetPRDVerificationLLM(prdID string, backend, model, effort *string, actor string) (any, error)
	// Verifier policy overrides (mode + backend list). nil = leave unchanged,
	// "" / [] = clear. Precedence task > Automaton > global verifier_policy.
	SetTaskVerifierPolicy(prdID, taskID string, mode *string, backends *[]string, actor string) (any, error)
	SetPRDVerifierPolicy(prdID string, mode *string, backends *[]string, actor string) (any, error)

	// BL386 Phase 1 + BL387 Phase 2b — per-PRD warm-start seed config.
	SetMemorySeed(prdID string, enabled bool, maxPerScope int, roleFilter []string, fromPRDs []string, actor string) (any, error)

	// BL386 Phase 2 — per-PRD harvest-on-completion config.
	SetMemoryHarvest(prdID string, enabled bool, promoteTo string, roleFilter []string, max int, actor string) (any, error)

	// GH#172 D75 — per-PRD permission_mode, inherited by tasks spawned
	// under it as their claude-code --permission-mode default.
	SetPermissionMode(prdID, mode, actor string) (any, error)
	// pi --thinking level overrides (task > story > PRD > pi.thinking_level).
	SetPRDThinkingLevel(prdID, level, actor string) (any, error)
	SetStoryThinkingLevel(prdID, storyID, level, actor string) (any, error)
	SetTaskThinkingLevel(prdID, taskID, level, actor string) (any, error)

	// GH#172 D52 — pause/resume a running PRD without treating it as
	// blocked or failed.
	Pause(prdID, actor string) (any, error)
	Resume(prdID, actor string) (any, error)

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

	// BL221 (v6.2.0) — dedicated TemplateStore CRUD.
	ListTemplates() []any
	CreateTemplate(title, description, spec, typ string, tags []string) (any, error)
	GetTemplate(id string) (any, bool)
	UpdateTemplate(id, title, description, spec, typ string, tags []string) (any, error)
	DeleteTemplate(id string) error
	CloneToTemplate(prdID, description, actor string) (any, error)
	InstantiateFromTemplateStore(templateID string, vars map[string]string, projectDir, backend, effort string) (any, error)

	// BL221 (v6.2.0) Phase 3 — scan framework.
	RunScan(prdID string) (any, error)
	GetScanResult(prdID string) (any, bool)
	GetScanConfig() any
	SetScanConfig(body map[string]any) error
	// decompose-lint — live config read (autonomous.decompose_lint /
	// task_templates / decompose_guidance), validated replace, and the
	// per-PRD read-only dry run. Stored findings ride on GetPRD
	// (PRD.DecomposeLint).
	DecomposeLintConfig() any
	SetDecomposeLintConfig(body map[string]any) error
	DecomposeLintDryRun(prdID string) (any, error)
	CreateFixPRD(prdID string) (any, error)
	ProposeRuleEdits(prdID string) (any, error)

	// BL221 (v6.2.0) Phase 4 — type registry, Guided Mode, skills.
	ListAutomatonTypes() []any
	RegisterAutomatonType(id, label, description, color string) (any, error)
	SetPRDType(prdID, typ string) (any, error)
	SetPRDGuidedMode(prdID string, guided bool) (any, error)
	SetPRDSkills(prdID string, skills []string) (any, error)

	// BL370 — per-PRD max_concurrent_tasks override.
	SetPRDConcurrency(prdID string, n int) (any, error)
	SetPRDDirs(prdID string, readDirs, writeDirs []string) (any, error)
	SetPRDPriority(prdID string, n int) (any, error)
	SetPRDKind(prdID, kind string) (any, error)
	SetPRDReleaseSize(prdID, size string) (any, error)
	SetPRDBreakingChange(prdID string, breaking bool) (any, error)
	// Per-Automaton worker profile name (built-in or user-defined; empty clears it).
	SetPRDWorkerProfile(prdID, name string) (any, error)

	// Per-Automaton lane preference: gpu, claude or any (empty clears it).
	SetPRDLanePreference(prdID, pref string) (any, error)
	// Lane fill status: mode, lanes, evidence, decisions and alerts.
	LaneFillStatus(limit int) autonomous.LaneFillStatus
	// Lane utilization: how the lanes were used over a window ending at now.
	LaneUtilization(window time.Duration, buckets int, now time.Time) autonomous.LaneUtilization

	// BL367 — per-PRD quality gate config.
	SetPRDQualityGates(prdID string, enabled bool, testCommand string, timeout int, blockOnRegression bool, scope string, smokePackages []string) (any, error)

	// Per-PRD override for whether a story failure halts the PRD (default)
	// or the executor continues into later, independent stories.
	SetPRDContinueOnStoryFailure(prdID string, continueOnFailure bool) (any, error)
	// Per-Automaton notify_events list (lifecycle event kinds that produce
	// notifications for this Automaton). A nil or empty list clears the
	// field (inherit from the next tier: profile, then global default set).
	SetPRDNotifyEvents(prdID string, kinds []string) (any, error)
	SetPRDGit(prdID string, autoPR bool, baseBranch string) (any, error)
	// Per-PRD push-on-complete override (never|branch|default_branch; "" clears
	// the override so the daemon's git.push_on_complete applies).
	SetPRDGitPush(prdID, policy string) (any, error)

	// BL303 S2 — guardrail library + profiles + per-Automaton override.
	GuardrailLibrary() []any
	CreateGuardrailProfile(name, description string, guardrails []string, blockOnFileDrift *bool, notifyEvents []string) (any, error)
	UpdateGuardrailProfile(id, name, description string, guardrails []string, blockOnFileDrift *bool, notifyEvents []string) (any, error)
	DeleteGuardrailProfile(id string) error
	ListGuardrailProfiles() []any
	GetGuardrailProfile(id string) (any, bool)
	SetPRDGuardrails(prdID, profile string, perTask, perStory, perPRD []string, blockOnFileDrift *bool) (any, error)

	// BL410 Phase 1 — retry profiles + per-Automaton retry-policy override.
	CreateRetryProfile(name, description string, budgets map[string]int, ceilingTotal, infraRetryEscalateAfter int, contentAutoFixEnabled, autoFixSkipApproval *bool) (any, error)
	UpdateRetryProfile(id, name, description string, budgets map[string]int, ceilingTotal, infraRetryEscalateAfter int, contentAutoFixEnabled, autoFixSkipApproval *bool) (any, error)
	DeleteRetryProfile(id string) error
	ListRetryProfiles() []any
	GetRetryProfile(id string) (any, bool)
	SetPRDRetryPolicy(prdID, profile string, policy map[string]int, ceilingTotal int, contentAutoFixEnabled, autoFixSkipApproval *bool) (any, error)

	// Automation records (data model + CRUD only).
	CreateAutomation(in autonomous.AutomationInput, actor string) (any, error)
	UpdateAutomation(id string, expectedVersion int64, in autonomous.AutomationInput, actor string) (any, error)
	DeleteAutomation(id string) error
	ListAutomations() []any
	GetAutomation(id string) (any, bool)
	ActivateAutomation(id, actor string, expectedVersion int64) (any, error)
	ReturnAutomationToDraft(id, actor string, expectedVersion int64) (any, error)

	// Record shipped state: the release the work was merged into, the merge
	// time (zero = cleared), the merge commit, and the source of the record
	// (merge_step or backfill). Recording never changes the item's Status.
	RecordPRDShipped(id, release string, mergedAt time.Time, commit, source string) (any, error)
	RecordAutomationShipped(id, release string, mergedAt time.Time, commit, source string) (any, error)

	// Shipped backfill — one shipped-state backfill pass over the base
	// repository's main branch, returning the manager's
	// ShippedBackfillReport. dryRun scans and computes the report without
	// writing any shipped record.
	ShippedBackfillOnce(dryRun bool) (any, error)

	// MigrateCompleted moves cancelled Automata and Automations whose work is
	// merged to status Completed, returning the manager's
	// CompletedMigrationReport. dryRun reports without writing; limit caps the
	// records changed (0 = no limit); id restricts the pass to one record. It
	// returns autonomous.ErrCompletedMigrationBusy while a final gate runs.
	MigrateCompleted(dryRun bool, limit int, id string) (any, error)

	// BL303 S3 T15 — on-demand guardrail invocation for a session.
	// Runs the named guardrail against projectDir; result is appended to the
	// session's telemetry verdicts by the HTTP handler.
	InvokeGuardrailByName(name, projectDir string) (any, error)
}

// storeSideFileCapSetter is implemented by autonomous APIs whose PRD store
// takes a live side-file threshold.
type storeSideFileCapSetter interface{ SetStoreSideFileCapKB(kb int) }

// applyStoreConfig pushes the session.store_* settings to the live session
// and PRD stores. The daemon calls it at startup and a config write calls it
// again, so a change takes effect without a restart.
func (s *Server) applyStoreConfig() {
	sc := &s.cfg.Session
	if s.manager != nil {
		s.manager.SetFlushInterval(sc.EffectiveStoreFlushInterval())
		s.manager.SetArchiveAfter(time.Duration(sc.EffectiveStoreArchiveAfterDays()) * 24 * time.Hour)
		s.manager.SetFieldCap(sc.EffectiveStoreFieldCapKB() << 10)
	}
	if setter, ok := s.autonomousMgr.(storeSideFileCapSetter); ok {
		setter.SetStoreSideFileCapKB(sc.EffectiveStoreSideFileCapKB())
	}
}

// SetAutonomousAPI is the wiring entry point used by main.go.
func (s *Server) SetAutonomousAPI(a AutonomousAPI) {
	s.autonomousMgr = a
	s.wireSessionTags()
	// Requirements surface: the concrete manager also implements the
	// requirements-report engine — wire it plus a config-derived backend
	// summary (nil API leaves the route reporting 503).
	if a != nil {
		s.wireRequirementsFromAuto(a)
	}
}

// SummarizerSvc is the interface for the response summarizer.
// The summarizer package implements this with Service.Summarize.
type SummarizerSvc interface {
	Summarize(ctx context.Context, text string) (string, error)
	SummarizeDual(ctx context.Context, text string, prevShort string) (string, string, error)
	ContextLines() int
}

// SetSummarizerSvc wires the response summarizer into the server.
func (s *Server) SetSummarizerSvc(svc SummarizerSvc) { s.summarizerSvc = svc }

func NewServer(hub *Hub, manager *session.Manager, hostname, token string, backends []string, cfg *config.Config, cfgPath string) *Server {
	s := &Server{
		hub:               hub,
		manager:           manager,
		hostname:          hostname,
		token:             token,
		availableBackends: backends,
		cfg:               cfg,
		cfgPath:           cfgPath,
		linkStreams:       make(map[string]chan string),
		channelHist:       make(map[string][]channelHistEntry),
		nonces:            newNonceStore(),
	}
	s.autonomousSettings = s.tagSettingLookup // attention statuses come from autonomous.tags
	if cfg != nil {
		setPushConfig(cfg.Push) // BL394 -- initial push-SSRF-guard config
	}
	// Pre-warm backend version cache in background so first /api/backends is instant.
	go s.warmVersionCache()
	// SEC-006 — reap expired nonces so a flood of minted-but-unused ones
	// (download links opened then never clicked, abandoned EventSource
	// connects) doesn't grow the map unboundedly. Never stopped — lives
	// for the server's process lifetime, same as the server itself.
	s.nonces.startSweeper(nonceTTL, make(chan struct{}))
	s.wireHubSessionStream() // BL432 / GH#242 — live ws_session_list_default + summary length
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
	case "pi":
		return s.cfg.Pi.Enabled
	case "pi-prompt":
		return s.cfg.PiPrompt.Enabled
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

// SetExitHookStore wires an exit hook store into the API server (BL356).
func (s *Server) SetExitHookStore(store *session.ExitHookStore) { s.exitHookStore = store }

// SetQueueStore wires a work queue store into the API server (BL357).
func (s *Server) SetQueueStore(store *session.QueueStore) { s.queueStore = store }

// SetResultStore wires a result store into the API server (BL360).
func (s *Server) SetResultStore(store *session.ResultStore) { s.resultStore = store }

// SetDiscussionSubStore wires a discussion subscription store into the API server (BL358).
func (s *Server) SetDiscussionSubStore(store *session.DiscussionSubStore) {
	s.discussionSubStore = store
}

// SetRestartFunc wires the daemon self-restart function.
func (s *Server) SetRestartFunc(fn func()) { s.restartFn = fn }

// SetRestartFuncForce wires the force-aware restart function (the restart
// guard). It takes precedence over SetRestartFunc's function for /api/restart.
func (s *Server) SetRestartFuncForce(fn func(force bool)) { s.restartForceFn = fn }

// restartAvailable reports whether any restart function is wired.
func (s *Server) restartAvailable() bool { return s.restartFn != nil || s.restartForceFn != nil }

// doRestart runs the wired restart function. The force-aware one takes the
// force flag; the plain one has no force concept and ignores it.
func (s *Server) doRestart(force bool) {
	if s.restartForceFn != nil {
		s.restartForceFn(force)
		return
	}
	if s.restartFn != nil {
		s.restartFn()
	}
}
func (s *Server) SetStatsCollector(c *stats.Collector) { s.statsCollector = c }

// SetACMEManager wires the ACME subsystem (BL397) into the API server.
// The mux's /.well-known/acme-challenge/ route (registered unconditionally
// in server.go) dispatches to s.acmeManager.ChallengeHandler() when set,
// 404s otherwise — so this can be called any time before the first real
// ACME validation request arrives, not strictly before ListenAndServe.
func (s *Server) SetACMEManager(m *acme.Manager) { s.acmeManager = m }

// SetAPNsDispatcher wires the APNs push dispatcher (BL397 Phase 4 / BL335).
func (s *Server) SetAPNsDispatcher(d *apns.Dispatcher) { s.apnsDispatcher = d }

// handleOpenWebUIModels returns available models from the configured OpenWebUI instance.
// Optional ?node=<cn> probes a specific Compute Node's address instead of the default.
func (s *Server) handleOpenWebUIModels(w http.ResponseWriter, r *http.Request) {
	if !s.fedCap(w, r, federation.CapLLMsList) {
		return
	}
	url, apiKey := "", ""
	enabled := false
	if s.cfg != nil {
		url = s.cfg.OpenWebUI.URL
		apiKey = s.cfg.OpenWebUI.APIKey
		enabled = s.cfg.OpenWebUI.Enabled
	}
	nodeOverride := false
	if nodeName := r.URL.Query().Get("node"); nodeName != "" && s.computeReg != nil {
		if n, err := s.computeReg.Get(nodeName); err == nil && n != nil && n.Address != "" {
			addr := n.Address
			if !strings.HasPrefix(addr, "http://") && !strings.HasPrefix(addr, "https://") {
				addr = "http://" + addr
			}
			url = addr
			nodeOverride = true
		}
	}
	// When OpenWebUI is disabled in config and no explicit compute-node
	// override is supplied, return an empty list with 200 instead of
	// dialing a dead URL and emitting 502 to every PWA poll. The PWA
	// dropdowns happily render an empty list.
	if !enabled && !nodeOverride {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]any{})
		return
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

// SetMCPBridge wires the daemon MCP server for /api/mcp/tools and /api/mcp/call.
func (s *Server) SetMCPBridge(b mcpBridgeAPI) { s.mcpBridge = b }

// SetSessionTokenStore wires the Design A3 per-session scoped-credential
// store so fedAuthMiddleware can recognize a session's token as a
// distinct, capability-limited principal.
func (s *Server) SetSessionTokenStore(store *auth.SessionTokenStore) { s.sessionTokens = store }

// SessionTokenStore returns the Design A3 session-token store, or nil
// if none is wired (e.g. test fixtures built as a bare &Server{}).
func (s *Server) SessionTokenStore() *auth.SessionTokenStore { return s.sessionTokens }

// SetMCPAuthRequired records whether mcp.token is configured (SEC-002),
// for /api/health's mcp_auth_required field.
func (s *Server) SetMCPAuthRequired(configured bool) { s.mcpTokenConfigured = configured }

// SetMCPSSEStateFn wires the MCP SSE listener state reporter
// (disabled|retrying|listening|failed) into /api/health's mcp_sse field.
func (s *Server) SetMCPSSEStateFn(fn func() string) { s.mcpSSEStateFn = fn }

// SetMCPSamplingDispatcher wires the sampling dispatcher for POST /api/mcp/sample (BL302 S3).
func (s *Server) SetMCPSamplingDispatcher(d MCPSamplingAPI) { s.mcpSamplingDisp = d }

// SetMCPElicitationDispatcher wires the elicitation dispatcher for POST /api/mcp/elicit (BL302 S3).
func (s *Server) SetMCPElicitationDispatcher(d MCPElicitationAPI) { s.mcpElicitationDisp = d }

// handleMCPTools returns daemon MCP tools as JSON for the channel bridge,
// filtered to the ones the caller's capability set actually covers
// (Design A3) — admin sees the full catalog; a federation peer or
// session token only sees tools federation.RequiredCapForMCPTool grants
// it, so a scoped caller's bridge never even attempts a tool it would
// be refused by handleMCPCall below.
func (s *Server) handleMCPTools(w http.ResponseWriter, r *http.Request) {
	if !s.fedCap(w, r, federation.CapSessionsList) {
		return
	}
	if s.mcpBridge == nil {
		http.Error(w, "MCP not enabled", http.StatusServiceUnavailable)
		return
	}
	data, err := s.mcpBridge.MCPToolsJSON()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	caps, admin := s.callerCaps(r.Context())
	if admin {
		w.Header().Set("Content-Type", "application/json")
		w.Write(data) //nolint:errcheck
		return
	}
	var tools []json.RawMessage
	if err := json.Unmarshal(data, &tools); err != nil {
		http.Error(w, "parse tool list: "+err.Error(), http.StatusInternalServerError)
		return
	}
	filtered := make([]json.RawMessage, 0, len(tools))
	for _, raw := range tools {
		var t struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal(raw, &t); err != nil {
			continue
		}
		required, ok := federation.RequiredCapForMCPTool(t.Name)
		if !ok {
			continue // unknown tool — fail closed, never advertise it
		}
		if federation.Check(caps, required) {
			filtered = append(filtered, raw)
		}
	}
	out, err := json.Marshal(filtered)
	if err != nil {
		http.Error(w, "marshal filtered tool list: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(out) //nolint:errcheck
}

// handleMCPCall dispatches a named MCP tool call and returns the result as
// JSON. Design A3 — each tool requires its own capability
// (federation.MCPToolCap), not the single blanket comm:write check every
// tool used to share (BL316-followup's "per-tool capability mapping is
// tracked" note — this is that follow-up). An unknown tool name is
// treated as admin-only (fail closed).
func (s *Server) handleMCPCall(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.mcpBridge == nil {
		http.Error(w, "MCP not enabled", http.StatusServiceUnavailable)
		return
	}
	var req struct {
		Tool   string         `json:"tool"`
		Args   map[string]any `json:"args"`
		Params map[string]any `json:"params"` // alias accepted for backwards compat
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad json: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.Tool == "" {
		http.Error(w, "tool is required", http.StatusBadRequest)
		return
	}
	required, ok := federation.RequiredCapForMCPTool(req.Tool)
	if !ok {
		http.Error(w, "unknown tool: "+req.Tool, http.StatusNotFound)
		return
	}
	if !s.fedCap(w, r, required) {
		return
	}
	if req.Args == nil && req.Params != nil {
		req.Args = req.Params
	}
	// HLLM-004 — when this call authenticated via a per-session scoped
	// credential, stash the real owning session ID so the shared MCP
	// server's session-lifecycle/memory tool handlers can scope the
	// call to that session's own subtree instead of trusting it blindly
	// (session-default previously had no ownership check at all).
	ctx := r.Context()
	if s.sessionTokens != nil {
		if tok := callerTokenFromContext(ctx); tok != "" {
			if sessID := s.sessionTokens.SessionIDForToken(tok); sessID != "" {
				ctx = federation.WithCallerSessionID(ctx, sessID)
			}
		}
	}
	s.traceMCPCall(r.Context(), req.Tool)
	data, err := s.mcpBridge.MCPCallJSON(ctx, req.Tool, req.Args)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(data) //nolint:errcheck
}

// handleMCPResourcesList returns all registered MCP resources as JSON (BL302 S1).
func (s *Server) handleMCPResourcesList(w http.ResponseWriter, r *http.Request) {
	if !s.fedCap(w, r, federation.CapSessionsList) {
		return
	}
	if s.mcpBridge == nil {
		http.Error(w, "MCP not enabled", http.StatusServiceUnavailable)
		return
	}
	data, err := s.mcpBridge.ResourcesJSON()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(data) //nolint:errcheck
}

// handleMCPResourcesRead reads a resource by URI and returns the result as JSON (BL302 S1).
func (s *Server) handleMCPResourcesRead(w http.ResponseWriter, r *http.Request) {
	if !s.fedCap(w, r, federation.CapSessionsList) {
		return
	}
	if s.mcpBridge == nil {
		http.Error(w, "MCP not enabled", http.StatusServiceUnavailable)
		return
	}
	uri := r.URL.Query().Get("uri")
	if uri == "" {
		http.Error(w, "uri query parameter is required", http.StatusBadRequest)
		return
	}
	data, err := s.mcpBridge.ResourceReadJSON(r.Context(), uri)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(data) //nolint:errcheck
}

// handleMCPResourcesTemplates returns all registered MCP resource templates as JSON (BL302 S1).
func (s *Server) handleMCPResourcesTemplates(w http.ResponseWriter, r *http.Request) {
	if !s.fedCap(w, r, federation.CapSessionsList) {
		return
	}
	if s.mcpBridge == nil {
		http.Error(w, "MCP not enabled", http.StatusServiceUnavailable)
		return
	}
	data, err := s.mcpBridge.ResourceTemplatesJSON()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(data) //nolint:errcheck
}

// handleMCPSample dispatches a daemon-initiated sampling request (BL302 S3).
// POST /api/mcp/sample — body: {trigger, messages, system_prompt, max_tokens}
// Returns {"result":{...}} on success or {"error":"..."} when no MCP client connected.
func (s *Server) handleMCPSample(w http.ResponseWriter, r *http.Request) {
	if !s.fedCap(w, r, federation.CapSessionsList) {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req MCPSamplingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad json: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.Trigger == "" {
		req.Trigger = "manual"
	}
	if len(req.Messages) == 0 {
		http.Error(w, "messages is required", http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if s.mcpSamplingDisp == nil {
		// MCP not enabled or sampling not configured — return structured error.
		json.NewEncoder(w).Encode(map[string]string{"error": "sampling not enabled"}) //nolint:errcheck
		return
	}
	res, err := s.mcpSamplingDisp.Sample(r.Context(), req)
	if err != nil {
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()}) //nolint:errcheck
		return
	}
	json.NewEncoder(w).Encode(map[string]any{"result": res}) //nolint:errcheck
}

// handleMCPSamplingLog returns the sampling ring buffer as a JSON array.
// GET /api/mcp/sampling-log — returns [] when no sampling has occurred or MCP is disabled.
func (s *Server) handleMCPSamplingLog(w http.ResponseWriter, r *http.Request) {
	if !s.fedCap(w, r, federation.CapSessionsList) {
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if s.mcpSamplingDisp == nil {
		json.NewEncoder(w).Encode([]MCPSamplingLogEntry{}) //nolint:errcheck
		return
	}
	entries := s.mcpSamplingDisp.Log()
	json.NewEncoder(w).Encode(entries) //nolint:errcheck
}

// handleMCPElicit dispatches a daemon-initiated elicitation request (BL302 S3).
// POST /api/mcp/elicit — body: {schema, message, options}
// Returns {"result":{...}} on success or {"error":"..."} when no MCP client connected.
func (s *Server) handleMCPElicit(w http.ResponseWriter, r *http.Request) {
	if !s.fedCap(w, r, federation.CapSessionsList) {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req MCPElicitationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad json: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.Schema == "" {
		http.Error(w, "schema is required", http.StatusBadRequest)
		return
	}
	if req.Message == "" {
		req.Message = "Please provide input"
	}
	w.Header().Set("Content-Type", "application/json")
	if s.mcpElicitationDisp == nil {
		json.NewEncoder(w).Encode(map[string]string{"error": "elicitation not enabled"}) //nolint:errcheck
		return
	}
	res, err := s.mcpElicitationDisp.Elicit(r.Context(), req)
	if err != nil {
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()}) //nolint:errcheck
		return
	}
	json.NewEncoder(w).Encode(map[string]any{"result": res}) //nolint:errcheck
}

// handleMCPPromptsList returns all registered MCP prompts as JSON (BL302 S4).
// GET /api/mcp/prompts → {"prompts":[...]}
func (s *Server) handleMCPPromptsList(w http.ResponseWriter, r *http.Request) {
	if !s.fedCap(w, r, federation.CapSessionsList) {
		return
	}
	if s.mcpBridge == nil {
		http.Error(w, "MCP not enabled", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(s.mcpBridge.PromptsListJSON()) //nolint:errcheck
}

// handleMCPPromptsGet renders a prompt by name with given arguments (BL302 S4).
// POST /api/mcp/prompts/get — body: {"name":"<prompt-name>","arguments":{...}}
// Returns {"name","description","messages":[{"role","content"},...]} on success.
func (s *Server) handleMCPPromptsGet(w http.ResponseWriter, r *http.Request) {
	if !s.fedCap(w, r, federation.CapSessionsList) {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.mcpBridge == nil {
		http.Error(w, "MCP not enabled", http.StatusServiceUnavailable)
		return
	}
	var req struct {
		Name      string            `json:"name"`
		Arguments map[string]string `json:"arguments"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad json: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.Name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}
	if req.Arguments == nil {
		req.Arguments = map[string]string{}
	}
	data, err := s.mcpBridge.PromptsGetJSON(r.Context(), req.Name, req.Arguments)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()}) //nolint:errcheck
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(data) //nolint:errcheck
}

// handleMCPDocs returns MCP tool documentation as JSON or HTML.
func (s *Server) handleMCPDocs(w http.ResponseWriter, r *http.Request) {
	if !s.fedCap(w, r, federation.CapDocsRead) {
		return
	}
	if s.mcpDocsFunc == nil {
		http.Error(w, "MCP not enabled", http.StatusServiceUnavailable)
		return
	}
	docs := s.mcpDocsFunc()

	// If Accept header prefers HTML, return a rendered page
	accept := r.Header.Get("Accept")
	if strings.Contains(accept, "text/html") {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = fmt.Fprintf(w, `<!DOCTYPE html><html><head><title>datawatch MCP Tools</title>
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
					_, _ = fmt.Fprintf(w, `<div class="tool"><div class="tool-name">%v</div><p>%v</p>`, m["name"], m["description"])
					if params, ok := m["parameters"].([]interface{}); ok {
						for _, p := range params {
							if pm, ok := p.(map[string]interface{}); ok {
								req := ""
								if r, ok := pm["required"].(bool); ok && r {
									req = ` <span class="required">required</span>`
								}
								_, _ = fmt.Fprintf(w, `<div class="param"><code>%v</code> (%v)%s — %v</div>`, pm["name"], pm["type"], req, pm["description"])
							}
						}
					}
					_, _ = fmt.Fprint(w, `</div>`)
				}
			}
		}
		_, _ = fmt.Fprint(w, `</body></html>`)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(docs) //nolint:errcheck
}

// handleOllamaModels returns available ollama models from the configured host.
// Optional ?node=<cn> probes a specific Compute Node's address instead of the default.
func (s *Server) handleOllamaModels(w http.ResponseWriter, r *http.Request) {
	if !s.fedCap(w, r, federation.CapLLMsList) {
		return
	}
	host := ""
	if s.cfg != nil {
		host = s.cfg.Ollama.Host
	}
	if nodeName := r.URL.Query().Get("node"); nodeName != "" && s.computeReg != nil {
		if n, err := s.computeReg.Get(nodeName); err == nil && n != nil && n.Address != "" {
			addr := n.Address
			if !strings.HasPrefix(addr, "http://") && !strings.HasPrefix(addr, "https://") {
				addr = "http://" + addr
			}
			host = addr
		}
	}
	models, err := ollama.ListModels(host)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(models) //nolint:errcheck
}

// handleOpenCodeModels returns models available for OpenCode sessions.
// Response: {models: [...], default_model: "..."}.
// Order: free built-ins from `opencode models` first, then Ollama from
// local or ?node=<cn> compute node. Cloud provider models (Anthropic,
// OpenAI, Google) appear when the operator has run `opencode providers
// login` — they come from the binary's own model list, not hardcoded here.
// Each entry: {id, label, provider, kind ("builtin"|"ollama"|"cloud")}.
func (s *Server) handleOpenCodeModels(w http.ResponseWriter, r *http.Request) {
	if !s.fedCap(w, r, federation.CapLLMsList) {
		return
	}
	type modelEntry struct {
		ID            string `json:"id"`
		Label         string `json:"label"`
		Provider      string `json:"provider"`
		ProviderLabel string `json:"provider_label"`
		Kind          string `json:"kind"`
		Default       bool   `json:"default,omitempty"`
	}

	// Query the OpenCode binary for all currently available models.
	// This returns free built-ins always, plus any provider models for
	// which the operator has configured credentials.
	binary := ""
	if s.cfg != nil {
		binary = s.cfg.OpenCode.Binary
	}
	builtins := opencode.ListBuiltinModels(binary)
	var models []modelEntry
	for _, id := range builtins {
		parts := strings.SplitN(id, "/", 2)
		provider := id
		if len(parts) == 2 {
			provider = parts[0]
		}
		kind := "builtin"
		if provider != "opencode" {
			kind = "cloud"
		}
		models = append(models, modelEntry{
			ID:            id,
			Label:         opencode.ModelLabel(id),
			Provider:      provider,
			ProviderLabel: opencode.ProviderLabel(provider),
			Kind:          kind,
		})
	}

	// Append Ollama models. Optional ?node=<cn> uses that compute node.
	host := ""
	if s.cfg != nil {
		host = s.cfg.Ollama.Host
	}
	if nodeName := r.URL.Query().Get("node"); nodeName != "" && s.computeReg != nil {
		if n, err := s.computeReg.Get(nodeName); err == nil && n != nil && n.Address != "" {
			addr := n.Address
			if !strings.HasPrefix(addr, "http://") && !strings.HasPrefix(addr, "https://") {
				addr = "http://" + addr
			}
			host = addr
		}
	}
	if ollamaModels, err := ollama.ListModels(host); err == nil {
		for _, name := range ollamaModels {
			models = append(models, modelEntry{
				ID:            "ollama/" + name,
				Label:         name + " (Ollama)",
				Provider:      "ollama",
				ProviderLabel: opencode.ProviderLabel("ollama"),
				Kind:          "ollama",
			})
		}
	}

	// Determine default model: configured default or first in list.
	defaultModel := ""
	if s.cfg != nil && s.cfg.OpenCode.DefaultModel != "" {
		defaultModel = s.cfg.OpenCode.DefaultModel
	}
	if defaultModel == "" && len(models) > 0 {
		defaultModel = models[0].ID
	}
	for i := range models {
		if models[i].ID == defaultModel {
			models[i].Default = true
			break
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
		"models":        models,
		"default_model": defaultModel,
	})
}

// handleOpenCodeProviders manages API keys for OpenCode cloud providers.
//
// GET  /api/opencode/providers — returns which providers have a key configured.
//
//	Response: {providers: {anthropic: {api_key_set: bool}, openai: {...}, google: {...}}}
//	The actual key value is never returned.
//
// PUT  /api/opencode/providers — stores a provider API key in the secrets store.
//
//	Body: {provider: "anthropic"|"openai"|"google", api_key: "sk-..."}
//
// The secret name convention is "opencode_provider_<provider>".
func (s *Server) handleOpenCodeProviders(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		if !s.fedCap(w, r, federation.CapSecretsList) {
			return
		}
	} else {
		if !s.fedCap(w, r, federation.CapSecretsWrite) {
			return
		}
	}
	const knownProviders = "anthropic openai google"
	isKnown := func(p string) bool {
		for _, kp := range strings.Fields(knownProviders) {
			if kp == p {
				return true
			}
		}
		return false
	}
	secretName := func(provider string) string {
		return "opencode_provider_" + provider
	}

	switch r.Method {
	case http.MethodGet:
		providers := map[string]map[string]bool{}
		for _, p := range strings.Fields(knownProviders) {
			set := false
			if s.secretsStore != nil {
				if ok, err := s.secretsStore.Exists(secretName(p)); err == nil {
					set = ok
				}
			}
			providers[p] = map[string]bool{"api_key_set": set}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"providers": providers})

	case http.MethodPut:
		var body struct {
			Provider string `json:"provider"`
			APIKey   string `json:"api_key"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid JSON body", http.StatusBadRequest)
			return
		}
		if !isKnown(body.Provider) {
			http.Error(w, "unknown provider; must be one of: anthropic, openai, google", http.StatusBadRequest)
			return
		}
		if body.APIKey == "" {
			http.Error(w, "api_key must not be empty", http.StatusBadRequest)
			return
		}
		if s.secretsStore == nil {
			http.Error(w, "secrets store not enabled", http.StatusServiceUnavailable)
			return
		}
		if err := s.secretsStore.Set(
			secretName(body.Provider),
			body.APIKey,
			[]string{"opencode", "provider"},
			"OpenCode "+body.Provider+" API key",
			nil,
		); err != nil {
			http.Error(w, "failed to store key: "+err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok", "provider": body.Provider})

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleLSPServers returns the operator-configured LSP server presets.
// Response: {servers: {<lang>: {command: [...], extensions: [...]}}}
func (s *Server) handleLSPServers(w http.ResponseWriter, r *http.Request) {
	if !s.fedCap(w, r, federation.CapConfigRead) {
		return
	}
	servers := map[string]any{}
	if s.cfg != nil {
		for name, srv := range s.cfg.LSP.Servers {
			servers[name] = map[string]any{
				"command":    srv.Command,
				"extensions": srv.Extensions,
			}
		}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"servers": servers}) //nolint:errcheck
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

// semverIsNewer reports whether latest is strictly greater than current
// using a major.minor.patch comparison. Pre-release suffixes are stripped.
// Returns false on parse errors so callers never falsely upgrade.
func semverIsNewer(latest, current string) bool {
	parse := func(s string) ([]int, bool) {
		s = strings.TrimPrefix(strings.TrimSpace(s), "v")
		if i := strings.Index(s, "-"); i >= 0 {
			s = s[:i]
		}
		if i := strings.Index(s, "+"); i >= 0 {
			s = s[:i]
		}
		parts := strings.Split(s, ".")
		out := make([]int, len(parts))
		for i, p := range parts {
			n, err := strconv.Atoi(p)
			if err != nil {
				return nil, false
			}
			out[i] = n
		}
		return out, true
	}
	a, aok := parse(latest)
	b, bok := parse(current)
	if !aok || !bok {
		return false
	}
	for i := 0; i < len(a) || i < len(b); i++ {
		var x, y int
		if i < len(a) {
			x = a[i]
		}
		if i < len(b) {
			y = b[i]
		}
		if x != y {
			return x > y
		}
	}
	return false
}

// handleSessions returns all sessions as JSON.
//
// BL116 — when the schedule store is wired, each session is decorated
// with a `scheduled_count` field so the web UI / comm renderers can
// surface a badge without a follow-up RPC. The decorator is additive
// (wraps the embedded *session.Session) so older clients keep
// parsing without changes.
func (s *Server) handleSessions(w http.ResponseWriter, r *http.Request) {
	if !s.fedCap(w, r, federation.CapSessionsList) {
		return
	}
	q := r.URL.Query()

	// BL361 — structured filter params.
	filter := session.SessionFilter{
		Name:    q.Get("name"),
		State:   q.Get("state"),
		Backend: q.Get("backend"),
		Alive:   q.Get("alive"),
	}
	// Tags repeatable; own, system and inherited tags count. tag_match is any
	// (default) or all; a value outside that set is rejected before anything is
	// read.
	tagSeen := map[string]bool{}
	for _, v := range q["tag"] {
		for _, part := range strings.Split(v, ",") {
			part = strings.TrimSpace(part)
			if part != "" && !tagSeen[part] {
				tagSeen[part] = true
				filter.Tags = append(filter.Tags, part)
			}
		}
	}
	// Axis shortcuts expand to axis:value tag filters and combine with the
	// explicit tag list under tag_match. Empty values are ignored. backend is
	// not expanded here: it stays the BL361 session-backend filter, which
	// also accepts the backend:<value> placement tag (a session matches on
	// its family name OR that tag, see Manager.sessionMatchesBackend), so
	// no earlier result is lost and ?backend=<axis value> works.
	for _, axis := range []string{"project", "automaton", "automation", "stage", "kind", "role", "lane", "compute"} {
		if v := strings.TrimSpace(q.Get(axis)); v != "" {
			tag := axis + ":" + v
			if !tagSeen[tag] {
				tagSeen[tag] = true
				filter.Tags = append(filter.Tags, tag)
			}
		}
	}
	filter.TagMatch = strings.TrimSpace(q.Get("tag_match"))
	if filter.TagMatch != "" && filter.TagMatch != "any" && filter.TagMatch != "all" {
		http.Error(w, `tag_match: must be "any" or "all"`, http.StatusBadRequest)
		return
	}
	sessions := s.manager.ListSessionsFiltered(filter)

	// BL432 — ?view=summary / ?since= / ?states= (session_list_view.go).
	listOpts, err := parseSessionListOptions(q)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	pageOpts, err := parseSessionPageOptions(q)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// — group_by / tag_prefix (grouping read, session_group.go): an unknown
	// axis is refused before anything is served; without group_by the options
	// are inactive and the responses below are unchanged.
	groupOpts, err := parseSessionGroupOptions(q)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// History / search requests also cover the archive (store_archive.go).
	sessions = append(sessions, s.archivedForList(q, filter, pageOpts)...)

	w.Header().Set("Content-Type", "application/json")

	if groupOpts.active {
		s.writeSessionGrouped(w, r, sessions, groupOpts)
		return
	}

	// BL350 — ?orphaned=1 returns only sessions whose parent_id no longer exists.
	if q.Get("orphaned") == "1" {
		byFullID := make(map[string]bool, len(sessions))
		for _, sess := range sessions {
			byFullID[sess.FullID] = true
		}
		var orphans []*session.Session
		for _, sess := range sessions {
			if sess.ParentID != "" && !byFullID[sess.ParentID] {
				orphans = append(orphans, sess)
			}
		}
		_ = json.NewEncoder(w).Encode(s.sessionViews(orphans))
		return
	}

	// BL347 — ?tree=1 returns the lineage tree instead of a flat list.
	if q.Get("tree") == "1" {
		tree := buildSessionTree(sessions)
		s.fillTreeInheritedTags(tree)
		_ = json.NewEncoder(w).Encode(tree)
		return
	}

	// view / since / states apply to the flat list only; without them the
	// legacy response below is emitted unchanged.
	// limit / cursor / status / active / q imply the row-only summary view.
	if listOpts.summary || pageOpts.any() {
		s.writeSessionSummaryList(w, r, sessions, listOpts, pageOpts)
		return
	}
	if listOpts.active() {
		s.writeSessionListView(w, sessions, listOpts)
		return
	}

	if s.schedStore == nil {
		_ = json.NewEncoder(w).Encode(s.sessionViews(sessions))
		return
	}
	type sessionWithCounts struct {
		*session.Session
		ScheduledCount int      `json:"scheduled_count"`
		InheritedTags  []string `json:"inherited_tags"`
		// ModelRequest is the request watch's observation of the session, the
		// same one sessionView adds; omitted when the session has none.
		ModelRequest *requestwatch.Observation `json:"model_request,omitempty"`
	}
	enriched := make([]sessionWithCounts, 0, len(sessions))
	for _, sess := range sessions {
		enriched = append(enriched, sessionWithCounts{
			Session:        sess,
			ScheduledCount: s.schedStore.CountForSession(sess.FullID),
			InheritedTags:  s.manager.InheritedTags(sess),
			ModelRequest:   s.sessionObservation(sess),
		})
	}
	_ = json.NewEncoder(w).Encode(enriched)
}

// SessionTreeNode is one node in the BL347 lineage tree.
type SessionTreeNode struct {
	*session.Session
	Children      []*SessionTreeNode `json:"children,omitempty"`
	InheritedTags []string           `json:"inherited_tags"`
}

// fillTreeInheritedTags sets inherited_tags on every node of a lineage tree.
func (s *Server) fillTreeInheritedTags(nodes []*SessionTreeNode) {
	for _, n := range nodes {
		n.InheritedTags = s.manager.InheritedTags(n.Session)
		s.fillTreeInheritedTags(n.Children)
	}
}

// buildSessionTree converts a flat session list to a forest of trees.
// Root nodes have no ParentID or an unresolved ParentID.
func buildSessionTree(sessions []*session.Session) []*SessionTreeNode {
	byFullID := make(map[string]*SessionTreeNode, len(sessions))
	for _, s := range sessions {
		byFullID[s.FullID] = &SessionTreeNode{Session: s}
	}
	var roots []*SessionTreeNode
	for _, node := range byFullID {
		if node.ParentID == "" {
			roots = append(roots, node)
			continue
		}
		if parent, ok := byFullID[node.ParentID]; ok {
			parent.Children = append(parent.Children, node)
		} else {
			roots = append(roots, node) // parent no longer in store — treat as root
		}
	}
	return roots
}

// handleSessionOutput returns the last N lines of a session's output.
// For agent-bound sessions (F10 sprint 3.6) the request is forwarded
// to the worker through the agent reverse proxy.
func (s *Server) handleSessionOutput(w http.ResponseWriter, r *http.Request) {
	if !s.fedCap(w, r, federation.CapSessionsRead) {
		return
	}
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

// resolveSessionAny looks up a session by ID or by name (BL354).
// idOrPath: the session ID (path param or query param "id").
// nameParam: value of the "session_name" query param.
// Returns (nil, nil) if not found, (nil, err) on ambiguity/error.
func (s *Server) resolveSessionAny(idOrPath, nameParam string) (*session.Session, error) {
	if idOrPath != "" {
		if sess, ok := s.manager.GetSession(idOrPath); ok {
			return sess, nil
		}
	}
	if nameParam != "" {
		if sess, ok := s.manager.FindSessionByName(nameParam); ok {
			return sess, nil
		}
	}
	return nil, nil
}

// handleSessionTimeline returns the structured timeline events for a session as JSON.
func (s *Server) handleSessionTimeline(w http.ResponseWriter, r *http.Request) {
	if !s.fedCap(w, r, federation.CapSessionsRead) {
		return
	}
	id := r.URL.Query().Get("id")
	sessionName := r.URL.Query().Get("session_name")
	if id == "" && sessionName == "" {
		http.Error(w, "missing id or session_name", http.StatusBadRequest)
		return
	}
	sess, resolveErr := s.resolveSessionAny(id, sessionName)
	if resolveErr != nil {
		http.Error(w, resolveErr.Error(), http.StatusConflict)
		return
	}
	if sess == nil {
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
	if !s.fedCap(w, r, federation.CapSessionsInput) {
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
	origin := fmt.Sprintf("REST POST /api/command requested by %s", s.auditActor(r.Context()))
	result := s.executeCommand(cmd, req.Text, origin)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"result": result}) //nolint:errcheck
}

// executeCommand runs a parsed command and returns a response string. origin
// names the surface and actor that sent the command; a kill records it as the
// session's end reason.
func (s *Server) executeCommand(cmd router.Command, raw, origin string) string {
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

	// v6.11.8 — explicit copy-mode page-up / page-down using `send-keys
	// -X`. Operator: "scroll mode page up isn't the same size as page
	// down". The previous PWA path sent raw `PPage` / `NPage` keysyms
	// via the generic `sendkey` command; tmux's key-table resolution
	// for those keysyms in copy-mode (vi vs emacs binding choice + key-
	// table state) made the page sizes drift. `send-keys -X page-up /
	// page-down` invokes the copy-mode commands directly and is
	// guaranteed-symmetric (both move cursor by `window-height-2`).
	if strings.HasPrefix(raw, "tmux-page-up ") {
		sessID := strings.TrimSpace(raw[len("tmux-page-up "):])
		sess, ok := s.manager.GetSession(sessID)
		if !ok {
			return fmt.Sprintf("Session %s not found", sessID)
		}
		if err := exec.Command("tmux", "send-keys", "-t", sess.TmuxSession, "-X", "page-up").Run(); err != nil {
			return fmt.Sprintf("Error: %v", err)
		}
		return fmt.Sprintf("[%s] page-up", sessID)
	}
	if strings.HasPrefix(raw, "tmux-page-down ") {
		sessID := strings.TrimSpace(raw[len("tmux-page-down "):])
		sess, ok := s.manager.GetSession(sessID)
		if !ok {
			return fmt.Sprintf("Session %s not found", sessID)
		}
		if err := exec.Command("tmux", "send-keys", "-t", sess.TmuxSession, "-X", "page-down").Run(); err != nil {
			return fmt.Sprintf("Error: %v", err)
		}
		return fmt.Sprintf("[%s] page-down", sessID)
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
		newOpts := &session.StartOptions{
			ParentID:              cmd.ParentID,
			KillChildren:          cmd.KillChildren,
			KillChildrenRecursive: cmd.KillChildrenRecursive,
		}
		sess, err := s.manager.Start(context.Background(), cmd.Text, "", cmd.ProjectDir, newOpts)
		if err != nil {
			return fmt.Sprintf("Error: %v", err)
		}
		// Broadcast updated session list
		s.broadcastSessionsAsync()
		return fmt.Sprintf("[%s][%s] Started: %s\nTmux: %s", s.hostname, sess.ID, cmd.Text, sess.TmuxSession)

	case router.CmdList:
		sessions := s.manager.ListSessions()
		if len(sessions) == 0 {
			return "No active sessions."
		}
		var sb strings.Builder
		for _, sess := range sessions {
			fmt.Fprintf(&sb, "[%s] %s — %s\n  %s\n", sess.ID, sess.State, sess.UpdatedAt.Format("15:04:05"), truncate(sess.Task, 60))
		}
		return sb.String()

	case router.CmdStatus:
		output, err := s.manager.TailOutput(cmd.SessionID, 20)
		if err != nil {
			return fmt.Sprintf("Error: %v", err)
		}
		return output

	case router.CmdSend:
		err := s.manager.SendInput(cmd.SessionID, s.expandImageTags(cmd.Text), "web")
		if err != nil {
			return fmt.Sprintf("Error: %v", err)
		}
		s.broadcastSessionsAsync()
		return fmt.Sprintf("[%s] Input sent.", cmd.SessionID)

	case router.CmdKill:
		err := s.manager.EndSession(cmd.SessionID, session.EndReason{Caller: endCallerCommandKill, Reason: origin})
		if err != nil {
			return fmt.Sprintf("Error: %v", err)
		}
		s.broadcastSessionsAsync()
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
		return session.TmuxAttachCommand(sess.TmuxSession, s.manager.DataDir())

	case router.CmdHelp:
		return router.HelpText(s.hostname)

	default:
		_ = raw // suppress unused variable warning
		return "Unknown command. Send 'help' for available commands."
	}
}

// wsOffloadImageSend delivers a WebSocket send whose text references images
// via the connection's input worker instead of blocking the read loop:
// expandImageTags calls the vision backend (seconds per image) and returns
// the description for SendInput. The worker sends the same response the
// synchronous path would have sent, and the per-connection FIFO plus the
// awaitInputForSession gate on the synchronous path keep per-session input
// order intact (an earlier image send is never overtaken by a later plain
// send for the same session).
func wsOffloadImageSend(c *client, s *Server, cmd router.Command, raw, origin string) {
	c.enqueueInput(cmd.SessionID, func() {
		result := s.executeCommand(cmd, raw, origin)
		respRaw, _ := json.Marshal(NotificationData{Message: result})
		c.safeSend(mustMarshal(WSMessage{Type: MsgNotification, Data: respRaw, Timestamp: time.Now()}))
	})
}

// handleWS upgrades a connection to WebSocket and registers it with the hub
func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	// BL316 — capture federated peer at upgrade time for per-message cap checks.
	wsPeer := peerFromContext(r.Context())
	// The actor names who opened the socket; a command sent over it records
	// that actor as the end reason of any session it kills.
	wsOrigin := fmt.Sprintf("WebSocket command requested by %s", s.auditActor(r.Context()))
	var wsGranted []string
	if wsPeer != nil {
		var custom map[string]*federation.CapabilityGroup
		if s.fedGroupStore != nil {
			custom = s.fedGroupStore.AsMap()
		}
		wsGranted = federation.Resolve(wsPeer.Capabilities, custom)
	}
	wsHasCap := func(required string) bool {
		if wsPeer == nil {
			return true
		}
		return federation.Check(wsGranted, required)
	}

	// SEC-006 — if the client offered a subprotocol (new WebSocket(url,
	// [token]), carrying its auth token per fedAuthMiddleware above), RFC
	// 6455 requires echoing it back in the 101 response or the browser
	// fails the connection client-side even though the server accepted
	// it.
	var upgradeHeader http.Header
	if proto := r.Header.Get("Sec-WebSocket-Protocol"); proto != "" {
		first := strings.TrimSpace(strings.SplitN(proto, ",", 2)[0])
		upgradeHeader = http.Header{"Sec-WebSocket-Protocol": []string{first}}
	}
	conn, err := upgrader.Upgrade(w, r, upgradeHeader)
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

	// GH#201 — WS connect/disconnect lifecycle. principal/remoteIP/UA are
	// captured now (from the pre-upgrade request) since they're not
	// available once the connection drops to a bare websocket.Conn.
	wsPrincipal := principalFromContext(r.Context())
	wsRemoteIP := remoteIP(r)
	wsUA := r.Header.Get("User-Agent")
	if s.accessLog != nil && (s.cfg == nil || s.cfg.Audit.AccessLogEnabledOrDefault()) {
		metrics.AccessLogEventsTotal.WithLabelValues("ws_connect", principalKind(wsPrincipal)).Inc()
		atomic.AddInt64(&s.accessLogEventCount, 1)
		wsDetails := map[string]any{
			"remote_ip": wsRemoteIP, "user_agent": wsUA,
		}
		// GH#201 Phase 2 — same cross-hop origin-actor surfacing as
		// logAccess, for a peer dialing /ws on behalf of its own actor.
		if chain := federation.ChainFromContext(r.Context()); len(chain) > 0 {
			wsDetails["origin_actor"] = federation.OriginActor(chain)
			wsDetails["hop_chain"] = chain
		}
		_ = s.accessLog.Write(audit.Entry{
			Actor:   wsPrincipal,
			Action:  "ws_connect",
			Details: wsDetails,
		})
	}

	// Send initial session list — unless the configured default is the
	// delta stream, whose clients fetch the list over REST instead (see
	// ws_session_stream.go). The frame is bounded to the default-view
	// sessions; a client that then announces the window form gets its rows
	// frame from handleHello.
	if s.wsSessionListDefault() != sessionListDelta {
		c.safeSend(s.hub.boundedSessionsFrame())
	} else {
		s.hub.seedSnapshotIfIdle()
	}

	go c.writePump()

	// Read pump (blocking)
	defer func() {
		// Deliver any accepted-but-still-expanding inputs and stop the
		// connection's input worker before unregistering, so the worker
		// never runs past connection teardown.
		c.stopInputWorker()
		// Cancel all screen captures for this client
		c.mu.Lock()
		for _, cancel := range c.captureCancels {
			cancel()
		}
		c.mu.Unlock()
		s.hub.unregister <- c
		_ = conn.Close()
		if s.accessLog != nil && (s.cfg == nil || s.cfg.Audit.AccessLogEnabledOrDefault()) {
			metrics.AccessLogEventsTotal.WithLabelValues("ws_disconnect", principalKind(wsPrincipal)).Inc()
			atomic.AddInt64(&s.accessLogEventCount, 1)
			_ = s.accessLog.Write(audit.Entry{
				Actor:  wsPrincipal,
				Action: "ws_disconnect",
				Details: map[string]any{
					"remote_ip": wsRemoteIP, "user_agent": wsUA,
				},
			})
		}
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
			if !wsHasCap(federation.CapSessionsInput) {
				respRaw, _ := json.Marshal(NotificationData{Message: "federation peer lacks capability: " + federation.CapSessionsInput})
				c.safeSend(mustMarshal(WSMessage{Type: MsgNotification, Data: respRaw, Timestamp: time.Now()}))
				continue
			}
			var d CommandData
			json.Unmarshal(inMsg.Data, &d) //nolint:errcheck
			cmd := router.Parse(d.Text)
			if cmd.Type == router.CmdSend && strings.Contains(d.Text, "[image:") {
				// Offload: expandImageTags can take seconds per image while the
				// vision backend describes, so it runs on this connection's
				// input worker instead of blocking the read loop
				// (see client.enqueueInput).
				wsOffloadImageSend(c, s, cmd, d.Text, wsOrigin)
				continue
			}
			// A plain send must not overtake an earlier, still-expanding
			// image send for the same session.
			if cmd.Type == router.CmdSend {
				c.awaitInputForSession(cmd.SessionID)
			}
			result := s.executeCommand(cmd, d.Text, wsOrigin)
			// Send result back to this client
			respRaw, _ := json.Marshal(NotificationData{Message: result})
			resp := WSMessage{Type: MsgNotification, Data: respRaw, Timestamp: time.Now()}
			respPayload, _ := json.Marshal(resp)
			c.safeSend(respPayload)

		case MsgNewSession:
			if !wsHasCap(federation.CapSessionsWrite) {
				respRaw, _ := json.Marshal(NotificationData{Message: "federation peer lacks capability: " + federation.CapSessionsWrite})
				c.safeSend(mustMarshal(WSMessage{Type: MsgNotification, Data: respRaw, Timestamp: time.Now()}))
				continue
			}
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
				s.broadcastSessionsAsync()
			}
			respRaw, _ := json.Marshal(NotificationData{Message: result})
			resp := WSMessage{Type: MsgNotification, Data: respRaw, Timestamp: time.Now()}
			respPayload, _ := json.Marshal(resp)
			c.safeSend(respPayload)

		case MsgSendInput:
			var d SendInputData
			json.Unmarshal(inMsg.Data, &d) //nolint:errcheck
			if !d.Raw && strings.Contains(d.Text, "[image:") {
				// Offload the image expansion to the per-connection input
				// worker so the read loop keeps reading (see
				// client.enqueueInput).
				wsOffloadImageSend(c, s, router.Command{Type: router.CmdSend, SessionID: d.SessionID, Text: d.Text}, "", wsOrigin)
				continue
			}
			// A later plain or raw input must not overtake an earlier
			// still-expanding image input for the same session.
			c.awaitInputForSession(d.SessionID)
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
					_ = s.manager.SendRawKeys(sess.FullID, d.Text)
				}
			} else {
				cmd := router.Command{Type: router.CmdSend, SessionID: d.SessionID, Text: d.Text}
				result := s.executeCommand(cmd, "", wsOrigin)
				respRaw, _ := json.Marshal(NotificationData{Message: result})
				resp := WSMessage{Type: MsgNotification, Data: respRaw, Timestamp: time.Now()}
				respPayload, _ := json.Marshal(resp)
				c.safeSend(respPayload)
			}

		case MsgHello:
			// BL432 / GH#242 — negotiate session-list mode / view.
			s.hub.handleHello(c, inMsg.Data)

		case MsgUnsubscribe:
			// BL432 / GH#242 — stop output delivery + this client's capture.
			var d SubscribeData
			json.Unmarshal(inMsg.Data, &d) //nolint:errcheck
			c.unsubscribe(d.SessionID)

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
			// v6.11.22 — operator: "I was not in the session when it ended,
			// so when i went in it is waiting for screen and blank until i
			// send this commands". Root cause: for sessions in a terminal
			// state whose tmux pane has already been cleaned up,
			// CapturePaneANSI errors and StartScreenCapture returns
			// immediately — so no pane_capture frame ever reaches the PWA
			// and the loading splash never dismisses. Fall back to the
			// TailOutput log lines (which we already fetched above) so the
			// PWA at least gets one frame.
			if (capErr != nil || captured == "") && err == nil && output != "" {
				captured = output
			}
			// v6.11.23 — operator: "nothing on the screen except connecting
			// to session, until i send a command then it works". Even when
			// both capture AND tail are empty (brand-new session, no LLM
			// output yet) the PWA needs at least one pane_capture frame to
			// dismiss the loading splash. Send a single space line so xterm
			// has something to write and the splash clears.
			if captured == "" {
				captured = " "
			}
			{
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
	if !s.fedCap(w, r, federation.CapHealthRead) {
		return
	}
	uptime := int(time.Since(startTime).Seconds())
	encrypted := s.manager.IsEncrypted()
	hasEnvPassword := os.Getenv("DATAWATCH_SECURE_PASSWORD") != ""
	// mcp_sse — live MCP SSE listener state (disabled|retrying|
	// listening|failed). Always present; "disabled" when SSE is off or no
	// reporter was wired, so operators don't need the server logs.
	mcpSSE := "disabled"
	if s.mcpSSEStateFn != nil {
		mcpSSE = s.mcpSSEStateFn()
	}
	// listeners — per-listener auth posture, computed live from the config
	// the server holds (config.ListenerAuthStates) so the answer is never
	// stale. Health is reachable without auth, so the body carries posture
	// only: no bind addresses, and never any token/secret value.
	listeners := []map[string]interface{}{}
	if s.cfg != nil {
		states := config.ListenerAuthStates(s.cfg)
		listeners = make([]map[string]interface{}, 0, len(states))
		for _, st := range states {
			listeners = append(listeners, map[string]interface{}{
				"name":          st.Name,
				"enabled":       st.Enabled,
				"auth_required": st.AuthRequired,
				"loopback":      st.Loopback,
				"acknowledged":  st.Acknowledged,
			})
		}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
		"status":           "ok",
		"hostname":         s.hostname,
		"version":          Version,
		"uptime_seconds":   uptime,
		"encrypted":        encrypted,
		"has_env_password": hasEnvPassword,
		"auth_required":    s.token != "",
		// SEC-002 — MCP SSE's actual auth posture: gated if mcp.token is
		// set, or (since mcp.New falls back) if server.token is set.
		"mcp_auth_required": s.mcpTokenConfigured || s.token != "",
		"mcp_sse":           mcpSSE,
		"listeners":         listeners,
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
	if !s.fedCap(w, r, federation.CapHealthRead) {
		return
	}
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
	if !s.fedCap(w, r, federation.CapHealthRead) {
		return
	}
	sessions := s.manager.ListSessions()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
		"hostname":           s.hostname,
		"version":            Version,
		"llm_backend":        s.manager.ActiveBackend(),
		"available_backends": s.availableBackends,
		"session_count":      len(sessions),
		"whisper_configured": s.transcriber != nil,
		"capabilities":       s.apiCapabilities(),
	})
}

// handleBackends returns available LLM backends with availability status.
// Version checks are cached and refreshed in the background every 60 seconds
// to avoid slow serial exec calls on every request.
func (s *Server) handleBackends(w http.ResponseWriter, r *http.Request) {
	if !s.fedCap(w, r, federation.CapLLMsList) {
		return
	}
	type backendInfo struct {
		Name           string   `json:"name"`
		Available      bool     `json:"available"`
		Enabled        bool     `json:"enabled"`
		PromptRequired bool     `json:"prompt_required,omitempty"`
		SupportsResume bool     `json:"supports_resume,omitempty"`
		Version        string   `json:"version,omitempty"`
		ComputeNodes   []string `json:"compute_nodes,omitempty"`
	}

	// BL305 (v7.0.0) — when the LLM registry is present, it is the
	// source of truth: return all non-disabled registry entries.
	// Old-style availableBackends (YAML-flag-gated adapters) are
	// included only for names not already in the registry, so
	// pre-migration installs don't regress.
	if s.inferenceReg != nil {
		regLLMs := s.inferenceReg.List()
		out := make([]backendInfo, 0, len(regLLMs))
		seen := make(map[string]bool, len(regLLMs))
		for _, l := range regLLMs {
			seen[l.Name] = true
			if l.Disabled {
				continue
			}
			out = append(out, backendInfo{
				Name:           l.Name,
				Enabled:        true,
				Available:      true,
				PromptRequired: s.llmPromptRequired(string(l.Kind)),
				ComputeNodes:   l.ComputeNodes,
			})
		}
		// Include any old-style compiled-in backends not yet migrated.
		for _, name := range s.availableBackends {
			if !seen[name] && s.llmEnabled(name) {
				out = append(out, backendInfo{
					Name:           name,
					Enabled:        true,
					Available:      true,
					PromptRequired: s.llmPromptRequired(name),
				})
			}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"llm": out, "active": s.manager.ActiveBackend()}) //nolint:errcheck
		return
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
// BL333: also handles multipart file upload (POST + multipart Content-Type)
// and file deletion (DELETE method).
func (s *Server) handleFiles(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost || r.Method == http.MethodDelete {
		if !s.fedCap(w, r, federation.CapConfigWrite) {
			return
		}
	} else if !s.fedCap(w, r, federation.CapConfigRead) {
		return
	}
	// BL333 — DELETE method: remove a file.
	if r.Method == http.MethodDelete {
		s.handleFilesDelete(w, r)
		return
	}
	// v4.0.1 — POST for directory creation. Backs the web UI +
	// mobile "create folder" affordance in the directory picker.
	// BL333 — POST with multipart Content-Type: upload a file.
	if r.Method == http.MethodPost {
		if ct := r.Header.Get("Content-Type"); strings.HasPrefix(ct, "multipart/") {
			s.handleFilesUpload(w, r)
			return
		}
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

	// Directory listing uses sessionRoot (RootPath → $HOME) so the project
	// directory browser is not clamped to the narrower file-service upload root.
	root := s.sessionRoot()
	if err := checkPathTraversal(root, path); err != nil {
		path = root
	}

	entries, err := os.ReadDir(path)
	if err != nil {
		http.Error(w, fmt.Sprintf("cannot read dir: %v", err), http.StatusBadRequest)
		return
	}

	type Entry struct {
		Name   string `json:"name"`
		IsDir  bool   `json:"is_dir"`
		Path   string `json:"path"`
		IsLink bool   `json:"is_link,omitempty"`
	}
	result := []Entry{}
	// Add parent directory entry (omit if at root path boundary)
	parent := filepath.Dir(path)
	atRoot := root != "" && filepath.Clean(path) == filepath.Clean(root)
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
		Path   string `json:"path"`
		Name   string `json:"name"`
		Action string `json:"action"` // legacy clients send action:"mkdir" with full path
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
		return
	}
	// Legacy clients (PWA pre-v5.26.47, Android) send {path: fullTarget, action:"mkdir"}.
	// New contract is {path: parent, name: folderName}. Accept both.
	if strings.TrimSpace(req.Name) == "" && strings.TrimSpace(req.Path) != "" {
		req.Name = filepath.Base(req.Path)
		req.Path = filepath.Dir(req.Path)
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

// handleFilesDownload serves a single file for download (BL374).
// GET /api/files/download?path=<abs-path>[&inline=1]
// Returns the file content with Content-Disposition: attachment by default,
// or inline when ?inline=1 is set (e.g. for image preview).
// Enforces the same root-path restriction as handleFiles.
func (s *Server) handleFilesDownload(w http.ResponseWriter, r *http.Request) {
	if !s.fedCap(w, r, federation.CapConfigRead) {
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	path := r.URL.Query().Get("path")
	if path == "" {
		http.Error(w, "path required", http.StatusBadRequest)
		return
	}
	if len(path) > 0 && path[0] == '~' {
		home, _ := os.UserHomeDir()
		path = filepath.Join(home, path[1:])
	}
	cleanPath := filepath.Clean(path)

	// Enforce root-path restriction.
	if s.cfg != nil && s.cfg.Session.RootPath != "" {
		rootPath := s.cfg.Session.RootPath
		if len(rootPath) > 0 && rootPath[0] == '~' {
			home, _ := os.UserHomeDir()
			rootPath = filepath.Join(home, rootPath[1:])
		}
		cleanRoot := filepath.Clean(rootPath)
		if !strings.HasPrefix(cleanPath+string(filepath.Separator), cleanRoot+string(filepath.Separator)) &&
			cleanPath != cleanRoot {
			http.Error(w, "path outside configured root", http.StatusForbidden)
			return
		}
	}

	fi, err := os.Stat(cleanPath)
	if err != nil {
		http.Error(w, "not found: "+err.Error(), http.StatusNotFound)
		return
	}
	if fi.IsDir() {
		http.Error(w, "path is a directory", http.StatusBadRequest)
		return
	}

	disposition := "attachment"
	if r.URL.Query().Get("inline") == "1" {
		disposition = "inline"
	}
	w.Header().Set("Content-Disposition", fmt.Sprintf(`%s; filename="%s"`, disposition, filepath.Base(cleanPath)))
	http.ServeFile(w, r, cleanPath)
}

// handleRenameSession renames a session.
// ── Memory API endpoints ─────────────────────────────────────────────────────

func (s *Server) handleMemoryStats(w http.ResponseWriter, r *http.Request) {
	if !s.fedCap(w, r, federation.CapConfigRead) {
		return
	}
	if s.memoryAPI == nil {
		json.NewEncoder(w).Encode(map[string]interface{}{"enabled": false}) //nolint:errcheck
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(s.memoryAPI.Stats()) //nolint:errcheck
}

func (s *Server) handleMemoryList(w http.ResponseWriter, r *http.Request) {
	if !s.fedCap(w, r, federation.CapConfigRead) {
		return
	}
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
	if !s.fedCap(w, r, federation.CapConfigRead) {
		return
	}
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
			// Return empty JSON array (not text/plain) so clients can parse (#49).
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("X-Datawatch-Warning", err.Error())
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("[]"))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(results)
		return
	}

	results, err := s.memoryAPI.Search(query, 10)
	if err != nil {
		// Return empty JSON array (not text/plain) so clients can parse (#49).
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Datawatch-Warning", err.Error())
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("[]"))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(results) //nolint:errcheck
}

// handleMemorySave saves a new memory via POST /api/memory/save.
// Body: {"content": "text to remember", "project_dir": "/optional/path", "tags": "comma,separated"}
func (s *Server) handleMemorySave(w http.ResponseWriter, r *http.Request) {
	if !s.fedCap(w, r, federation.CapConfigWrite) {
		return
	}
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
		// Tags (GH#192 D78a) — comma-separated. Best-effort: an
		// unsupported-backend error here doesn't fail the save, the
		// memory itself was already created successfully by Remember
		// above. Same graceful-degradation standard as pinning.
		Tags string `json:"tags"`
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
	tagsApplied := true
	if req.Tags != "" {
		if err := s.memoryAPI.SetTags(id, req.Tags); err != nil {
			tagsApplied = false
		}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
		"status": "ok", "id": id, "tags_applied": tagsApplied,
	})
}

func (s *Server) handleMemoryDelete(w http.ResponseWriter, r *http.Request) {
	if !s.fedCap(w, r, federation.CapConfigWrite) {
		return
	}
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

// handleMemorySweepStale evicts rows that have never surfaced in
// any search and are older than the supplied cutoff. POST
// {older_than_days, dry_run}. Manual + pinned rows are exempt.
// (v6.0.0 — sweeper.py port)
func (s *Server) handleMemorySweepStale(w http.ResponseWriter, r *http.Request) {
	if !s.fedCap(w, r, federation.CapConfigWrite) {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.memoryAPI == nil {
		http.Error(w, "memory not enabled", http.StatusServiceUnavailable)
		return
	}
	var req struct {
		OlderThanDays int  `json:"older_than_days"`
		DryRun        bool `json:"dry_run"`
	}
	json.NewDecoder(r.Body).Decode(&req) //nolint:errcheck
	if req.OlderThanDays <= 0 {
		req.OlderThanDays = 90
	}
	res, err := s.memoryAPI.SweepStale(req.OlderThanDays, req.DryRun)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(res) //nolint:errcheck
}

// handleMemorySpellCheck runs the conservative Levenshtein-based
// spellchecker on the supplied text. POST {text, extra_words}.
// Returns a list of {original, proposed, distance}. Never rewrites.
// (v6.0.0 — spellcheck.py port)
func (s *Server) handleMemorySpellCheck(w http.ResponseWriter, r *http.Request) {
	if !s.fedCap(w, r, federation.CapConfigRead) {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.memoryAPI == nil {
		http.Error(w, "memory not enabled", http.StatusServiceUnavailable)
		return
	}
	var req struct {
		Text  string   `json:"text"`
		Extra []string `json:"extra_words,omitempty"`
	}
	json.NewDecoder(r.Body).Decode(&req) //nolint:errcheck
	out := s.memoryAPI.SpellCheckText(req.Text, req.Extra)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
		"suggestions": out, "count": len(out),
	})
}

// handleMemoryExtractFacts runs the heuristic schema-free triple
// extractor on the supplied text. POST {text}. Returns SVO triples.
// (v6.0.0 — general_extractor.py port)
func (s *Server) handleMemoryExtractFacts(w http.ResponseWriter, r *http.Request) {
	if !s.fedCap(w, r, federation.CapConfigWrite) {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.memoryAPI == nil {
		http.Error(w, "memory not enabled", http.StatusServiceUnavailable)
		return
	}
	var req struct {
		Text string `json:"text"`
	}
	json.NewDecoder(r.Body).Decode(&req) //nolint:errcheck
	out := s.memoryAPI.ExtractFactsText(req.Text)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
		"triples": out, "count": len(out),
	})
}

// handleMemoryWakeup returns the composed wake-up bundle for a
// project + (optional) agent. GET /api/memory/wakeup?project_dir=X&agent_id=Y&parent_agent_id=Z&parent_namespace=N.
// Smoke + operator tooling use this to verify what an agent would
// see at start without having to spawn one. (v5.26.71)
func (s *Server) handleMemoryWakeup(w http.ResponseWriter, r *http.Request) {
	if !s.fedCap(w, r, federation.CapConfigRead) {
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.memoryAPI == nil {
		http.Error(w, "memory not enabled", http.StatusServiceUnavailable)
		return
	}
	q := r.URL.Query()
	bundle := s.memoryAPI.WakeUpBundle(
		q.Get("project_dir"),
		q.Get("agent_id"),
		q.Get("parent_agent_id"),
		q.Get("parent_namespace"),
	)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
		"bundle":    bundle,
		"length":    len(bundle),
		"has_l4_l5": q.Get("agent_id") != "" || q.Get("parent_agent_id") != "",
	})
}

// handleMemoryPin flips the pinned flag on a memory (Mempalace QW#2,
// v5.26.70). POST {id, pinned}. Pinned rows always surface in L1
// critical-facts regardless of vector-similarity rank.
func (s *Server) handleMemoryPin(w http.ResponseWriter, r *http.Request) {
	if !s.fedCap(w, r, federation.CapConfigWrite) {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.memoryAPI == nil {
		http.Error(w, "memory not enabled", http.StatusServiceUnavailable)
		return
	}
	var req struct {
		ID     int64 `json:"id"`
		Pinned bool  `json:"pinned"`
	}
	json.NewDecoder(r.Body).Decode(&req) //nolint:errcheck
	if req.ID <= 0 {
		http.Error(w, "missing id", http.StatusBadRequest)
		return
	}
	if err := s.memoryAPI.SetPinned(req.ID, req.Pinned); err != nil {
		// Backends without pinning capability — surface 501 so the
		// PWA can hide the pin button instead of retrying.
		http.Error(w, err.Error(), http.StatusNotImplemented)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
		"status": "ok", "id": req.ID, "pinned": req.Pinned,
	})
}

// handleMemoryReindex triggers re-embedding of all memories.
func (s *Server) handleMemoryReindex(w http.ResponseWriter, r *http.Request) {
	if !s.fedCap(w, r, federation.CapConfigWrite) {
		return
	}
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
	if !s.fedCap(w, r, federation.CapConfigRead) {
		return
	}
	if s.memoryAPI == nil {
		http.Error(w, "memory not enabled", http.StatusServiceUnavailable)
		return
	}
	query := r.URL.Query().Get("q")
	projectDir := r.URL.Query().Get("project_dir")
	n := 20
	if nStr := r.URL.Query().Get("limit"); nStr != "" {
		_, _ = fmt.Sscanf(nStr, "%d", &n)
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
	if !s.fedCap(w, r, federation.CapConfigRead) {
		return
	}
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
		_, _ = fmt.Sscanf(nStr, "%d", &maxResults)
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
	switch r.Method {
	case http.MethodGet:
		if !s.fedCap(w, r, federation.CapPipelinesList) {
			return
		}
		if s.pipelineExec == nil {
			http.Error(w, "pipelines not available", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(s.pipelineExec.ListJSON()) //nolint:errcheck
	case http.MethodPost:
		if !s.fedCap(w, r, federation.CapPipelinesStart) {
			return
		}
		if s.pipelineExec == nil {
			http.Error(w, "pipelines not available", http.StatusServiceUnavailable)
			return
		}
		var req struct {
			Spec        string `json:"spec"` // "task1 -> task2 -> task3"
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
		if !s.fedCap(w, r, federation.CapPipelinesCancel) {
			return
		}
		if err := s.pipelineExec.Cancel(id); err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"}) //nolint:errcheck
	default:
		if !s.fedCap(w, r, federation.CapPipelinesRead) {
			return
		}
		w.Header().Set("Content-Type", "text/plain")
		_, _ = fmt.Fprint(w, s.pipelineExec.GetStatus(id))
	}
}

func (s *Server) handleMemoryExport(w http.ResponseWriter, r *http.Request) {
	if !s.fedCap(w, r, federation.CapConfigRead) {
		return
	}
	if s.memoryAPI == nil {
		http.Error(w, "memory not enabled", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", "attachment; filename=datawatch-memories.json")
	s.memoryAPI.Export(w) //nolint:errcheck
}

func (s *Server) handleMemoryImport(w http.ResponseWriter, r *http.Request) {
	if !s.fedCap(w, r, federation.CapConfigWrite) {
		return
	}
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
	if !s.fedCap(w, r, federation.CapConfigRead) {
		return
	}
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
	if !s.fedCap(w, r, federation.CapConfigRead) {
		return
	}
	if s.kgAPI == nil {
		http.Error(w, "KG not enabled", http.StatusServiceUnavailable)
		return
	}
	entity := r.URL.Query().Get("entity")
	asOf := r.URL.Query().Get("as_of")
	if entity == "" {
		http.Error(w, "missing entity param", http.StatusBadRequest)
		return
	}
	results, err := s.kgAPI.QueryEntity(entity, asOf)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(results) //nolint:errcheck
}

func (s *Server) handleKGAdd(w http.ResponseWriter, r *http.Request) {
	if !s.fedCap(w, r, federation.CapConfigWrite) {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.kgAPI == nil {
		http.Error(w, "KG not enabled", http.StatusServiceUnavailable)
		return
	}
	var req struct {
		Subject   string `json:"subject"`
		Predicate string `json:"predicate"`
		Object    string `json:"object"`
		ValidFrom string `json:"valid_from"`
		Source    string `json:"source"`
	}
	json.NewDecoder(r.Body).Decode(&req) //nolint:errcheck
	if req.Subject == "" || req.Predicate == "" || req.Object == "" {
		http.Error(w, "subject, predicate, object required", http.StatusBadRequest)
		return
	}
	id, err := s.kgAPI.AddTriple(req.Subject, req.Predicate, req.Object, req.ValidFrom, req.Source)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"id": id}) //nolint:errcheck
}

func (s *Server) handleKGInvalidate(w http.ResponseWriter, r *http.Request) {
	if !s.fedCap(w, r, federation.CapConfigWrite) {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.kgAPI == nil {
		http.Error(w, "KG not enabled", http.StatusServiceUnavailable)
		return
	}
	var req struct {
		Subject   string `json:"subject"`
		Predicate string `json:"predicate"`
		Object    string `json:"object"`
		Ended     string `json:"ended"`
	}
	json.NewDecoder(r.Body).Decode(&req) //nolint:errcheck
	if err := s.kgAPI.Invalidate(req.Subject, req.Predicate, req.Object, req.Ended); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"}) //nolint:errcheck
}

func (s *Server) handleKGTimeline(w http.ResponseWriter, r *http.Request) {
	if !s.fedCap(w, r, federation.CapConfigRead) {
		return
	}
	if s.kgAPI == nil {
		http.Error(w, "KG not enabled", http.StatusServiceUnavailable)
		return
	}
	entity := r.URL.Query().Get("entity")
	if entity == "" {
		http.Error(w, "missing entity param", http.StatusBadRequest)
		return
	}
	results, err := s.kgAPI.Timeline(entity)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(results) //nolint:errcheck
}

func (s *Server) handleKGStats(w http.ResponseWriter, r *http.Request) {
	if !s.fedCap(w, r, federation.CapConfigRead) {
		return
	}
	if s.kgAPI == nil {
		json.NewEncoder(w).Encode(map[string]bool{"enabled": false}) //nolint:errcheck
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(s.kgAPI.Stats()) //nolint:errcheck
}

// handleMemoryTest tests Ollama connectivity and embedding capability before enabling memory.
func (s *Server) handleMemoryTest(w http.ResponseWriter, r *http.Request) {
	if !s.fedCap(w, r, federation.CapConfigRead) {
		return
	}
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
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   err.Error(),
			"host":    host,
			"model":   model,
		})
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success":    true,
		"embedder":   "ollama",
		"host":       host,
		"model":      model,
		"dimensions": dims,
	})
}

// handleOllamaStats returns current Ollama server statistics.
func (s *Server) handleOllamaStats(w http.ResponseWriter, r *http.Request) {
	if !s.fedCap(w, r, federation.CapAnalyticsRead) {
		return
	}
	host := s.cfg.Ollama.Host
	if host == "" {
		json.NewEncoder(w).Encode(map[string]bool{"available": false}) //nolint:errcheck
		return
	}
	olStats := stats.FetchOllamaStats(host)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(olStats) //nolint:errcheck
}

// handleWebSearchStats returns web search stats (BL372).
// GET /api/web_search/stats
func (s *Server) handleWebSearchStats(w http.ResponseWriter, r *http.Request) {
	if !s.fedCap(w, r, federation.CapAnalyticsRead) {
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
		"enabled":     s.cfg.WebSearch.Enabled,
		"provider":    s.cfg.WebSearch.Provider,
		"url":         s.cfg.WebSearch.URL,
		"engine":      s.cfg.WebSearch.Engine,
		"num_results": s.cfg.WebSearch.NumResults,
	})
}

// handleSessionResponse returns the last captured LLM response for a session.
// GET /api/sessions/response?id=<session_id>
func (s *Server) handleSessionResponse(w http.ResponseWriter, r *http.Request) {
	if !s.fedCap(w, r, federation.CapSessionsRead) {
		return
	}
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

// handleSessionsOrphaned returns sessions whose parent_id points to a session
// that no longer exists in the store. GET /api/sessions/orphaned — BL350.
func (s *Server) handleSessionsOrphaned(w http.ResponseWriter, r *http.Request) {
	if !s.fedCap(w, r, federation.CapSessionsList) {
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	all := s.manager.ListSessions()
	byFullID := make(map[string]bool, len(all))
	for _, sess := range all {
		byFullID[sess.FullID] = true
	}
	var orphans []*session.Session
	for _, sess := range all {
		if sess.ParentID != "" && !byFullID[sess.ParentID] {
			orphans = append(orphans, sess)
		}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(orphans)
}

// handleSessionSelf resolves the "self" session: the active session whose MCP
// channel is connected (ChannelReady=true). Callers may supply an optional
// ?hint=<id> query param to verify a known ID is still live.
// GET /api/sessions/self — BL349.
func (s *Server) handleSessionSelf(w http.ResponseWriter, r *http.Request) {
	if !s.fedCap(w, r, federation.CapSessionsRead) {
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	hint := r.URL.Query().Get("hint")
	// Hint: resolve and verify it's live.
	if hint != "" {
		sess, ok := s.manager.GetSession(hint)
		if !ok {
			http.Error(w, "session not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(sess) //nolint:errcheck
		return
	}
	// No hint: find the channel-ready active session.
	all := s.manager.ListSessions()
	var candidates []*session.Session
	for _, sess := range all {
		// stalled is not a candidate: redelivery needs an active process to
		// receive it — a stalled session is non-terminal but not reachable.
		if sess.ChannelReady && (sess.State == session.StateRunning || sess.State == session.StateWaitingInput) {
			candidates = append(candidates, sess)
		}
	}
	if len(candidates) == 0 {
		http.Error(w, "no channel-ready session found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(candidates[0]) //nolint:errcheck
}

// handleSessionSelfScope returns the self session's full scope: identity,
// MCP channel server name (per-session), shared/global .mcp.json entry
// name, mode (host vs container/agent), project/tmux surface, channel
// readiness, and the set of every other session's MCP names — the REST
// mirror of the get_my_scope MCP tool (BL463), built from the same
// channel.BuildSelfScope so both surfaces report identical data.
//
// Resolution: ?hint=<id|name> first, then the same channel-ready/active
// rule as handleSessionSelf (the MCP tool additionally consults the
// in-band caller-scope credential, HLLM-004, which only exists on the
// MCP-calls-via-token path). resolved_via always says which one applied.
// GET /api/sessions/self/scope — BL463.
func (s *Server) handleSessionSelfScope(w http.ResponseWriter, r *http.Request) {
	if !s.fedCap(w, r, federation.CapSessionsRead) {
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	all := s.manager.ListSessions()
	var sess *session.Session
	var resolvedVia string
	if hint := r.URL.Query().Get("hint"); hint != "" {
		found, ok := s.manager.GetSession(hint)
		if !ok {
			http.Error(w, "session not found", http.StatusNotFound)
			return
		}
		sess, resolvedVia = found, "hint"
	}
	if sess == nil {
		for _, cand := range all {
			if cand.ChannelReady && (cand.State == session.StateRunning || cand.State == session.StateWaitingInput) {
				sess, resolvedVia = cand, "channel-ready-fallback"
				break
			}
		}
	}
	if sess == nil {
		http.Error(w, "no channel-ready session found", http.StatusNotFound)
		return
	}
	selfScope := channel.BuildSelfScope(sess, all)
	selfScope.ResolvedVia = resolvedVia
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(selfScope) //nolint:errcheck
}

// handleSessionPrompt returns the last user prompt for a session.
func (s *Server) handleSessionPrompt(w http.ResponseWriter, r *http.Request) {
	if !s.fedCap(w, r, federation.CapSessionsRead) {
		return
	}
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

// handleSessionSubpath is the catch-all dispatcher for /api/sessions/{id}/<verb>
// patterns not handled by the exact-match registrations. Covers the api.go set
// (last-summary, summarize, current-status) and falls through to handleSessionsSubpath
// for the rollback.go set (rollback, hook-event, status, telemetry, guardrail, input).
func (s *Server) handleSessionSubpath(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/sessions/")
	// GET /api/sessions/{id} — bare session lookup, no verb suffix.
	if !strings.Contains(path, "/") {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !s.fedCap(w, r, federation.CapSessionsRead) {
			return
		}
		sess, ok := s.manager.GetSessionOrArchived(path)
		if !ok {
			http.NotFound(w, r)
			return
		}
		writeJSONOK(w, s.sessionView(sess))
		return
	}
	if strings.HasSuffix(path, "/last-summary") {
		s.handleSessionLastSummary(w, r)
		return
	}
	if strings.HasSuffix(path, "/summarize") {
		s.handleSessionSummarize(w, r)
		return
	}
	if strings.HasSuffix(path, "/current-status") {
		s.handleSessionCurrentStatus(w, r)
		return
	}
	// GET /api/sessions/{id}/progress — live progress snapshot from the
	// session's own log (turn count, tool-call breakdown, most recent
	// action, tokens). See session_progress.go.
	if strings.HasSuffix(path, "/progress") {
		s.handleSessionProgress(w, r)
		return
	}
	if strings.HasSuffix(path, "/children") {
		s.handleSessionChildren(w, r)
		return
	}
	if strings.HasSuffix(path, "/result") {
		s.handleSessionResultPost(w, r)
		return
	}
	// BL359 — POST /api/sessions/{id}/restart
	if strings.HasSuffix(path, "/restart") {
		s.handleRestartSession(w, r)
		return
	}
	s.handleSessionsSubpath(w, r)
}

// handleSessionChildren returns the direct children of a session.
// GET /api/sessions/{id}/children — BL347.
func (s *Server) handleSessionChildren(w http.ResponseWriter, r *http.Request) {
	if !s.fedCap(w, r, federation.CapSessionsRead) {
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/api/sessions/")
	id := strings.TrimSuffix(path, "/children")
	if id == "" {
		http.Error(w, "session id required", http.StatusBadRequest)
		return
	}
	sess, ok := s.manager.GetSession(id)
	if !ok {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}
	children := s.manager.GetChildren(sess.FullID)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(children) //nolint:errcheck
}

// handleSessionLastSummary returns the stored summary for a session.
// GET /api/sessions/{id}/last-summary
func (s *Server) handleSessionLastSummary(w http.ResponseWriter, r *http.Request) {
	if !s.fedCap(w, r, federation.CapSessionsRead) {
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	// Parse session ID from path: /api/sessions/<id>/last-summary
	path := strings.TrimPrefix(r.URL.Path, "/api/sessions/")
	id := strings.TrimSuffix(path, "/last-summary")
	if id == "" {
		http.Error(w, "session id required", http.StatusBadRequest)
		return
	}
	// Resolve short ID to full ID.
	sess, ok := s.manager.GetSession(id)
	if !ok {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}
	sum, ok := s.manager.GetSummary(sess.FullID)
	if !ok {
		http.Error(w, "no summary available", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(sum) //nolint:errcheck
}

// handleSummarizerTest fires a minimal test prompt through the configured
// summarizer LLM and reports whether it responded.
// POST /api/summarizer/test
// Body (optional): {"llm_ref": "my-ollama", "model": "qwen3:8b"}
// Response: {"ok": true, "latency_ms": 234} or {"ok": false, "error": "..."}
func (s *Server) handleSummarizerTest(w http.ResponseWriter, r *http.Request) {
	if !s.fedCap(w, r, federation.CapConfigRead) {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.summarizerSvc == nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"ok": false, "error": "summarizer not configured"}) //nolint:errcheck
		return
	}
	start := time.Now()
	const testPrompt = "Say: OK"
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	result, err := s.summarizerSvc.Summarize(ctx, testPrompt)
	latency := time.Since(start).Milliseconds()
	w.Header().Set("Content-Type", "application/json")
	if err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{"ok": false, "error": err.Error(), "latency_ms": latency}) //nolint:errcheck
		return
	}
	_ = result
	json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "latency_ms": latency}) //nolint:errcheck
}

// handleSessionSummarize triggers on-demand summarization of a session's output.
// POST /api/sessions/{id}/summarize
// Body (optional): {"lines": 200}
func (s *Server) handleSessionSummarize(w http.ResponseWriter, r *http.Request) {
	if !s.fedCap(w, r, federation.CapSessionsRead) {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	// Parse session ID from path: /api/sessions/<id>/summarize
	path := strings.TrimPrefix(r.URL.Path, "/api/sessions/")
	id := strings.TrimSuffix(path, "/summarize")
	if id == "" {
		http.Error(w, "session id required", http.StatusBadRequest)
		return
	}
	sess, ok := s.manager.GetSession(id)
	if !ok {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}

	var reqBody struct {
		Lines int `json:"lines"`
	}
	reqBody.Lines = 200
	if r.Body != nil {
		json.NewDecoder(r.Body).Decode(&reqBody) //nolint:errcheck
	}
	if reqBody.Lines <= 0 {
		reqBody.Lines = 200
	}

	if s.summarizerSvc == nil {
		http.Error(w, "summarizer not configured", http.StatusServiceUnavailable)
		return
	}

	text, newOffset, err := s.manager.OutputSince(sess.FullID, sess.SummaryLogOffset)
	if err != nil {
		http.Error(w, "failed to read output: "+err.Error(), http.StatusInternalServerError)
		return
	}

	prevShort := ""
	if prev, ok := s.manager.GetSummary(sess.FullID); ok {
		prevShort = prev.Summary
	}
	ctx, cancel := context.WithTimeout(r.Context(), 35*time.Second)
	defer cancel()
	short, long, err := s.summarizerSvc.SummarizeDual(ctx, text, prevShort)
	if err != nil {
		http.Error(w, "summarizer error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	sum := session.SessionSummary{
		SessionID:   sess.FullID,
		Summary:     short,
		LongSummary: long,
		GeneratedAt: time.Now(),
	}
	s.manager.StoreSummary(sess.FullID, sum)
	// Only advance the watermark when we got a real summary.
	if strings.TrimSpace(short) != "" {
		s.manager.UpdateSummaryOffset(sess.FullID, newOffset)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(sum) //nolint:errcheck
}

// handleSessionCurrentStatus returns a live LLM summary of what a running session is doing.
// GET /api/sessions/{id}/current-status
//
// Returns {"current_status":"...","generated_at":"..."} for running sessions.
// When there is no new output to summarize, returns the same 200/JSON shape
// with no_change:true (v8.19.8 — was 204 with an empty body, which surfaced
// as "Failed to execute 'json'" in the PWA's apiFetch helper).
// Returns 409 if the session is not running (caller should show last_response instead).
// Returns 503 if no summarizer is configured.
func (s *Server) handleSessionCurrentStatus(w http.ResponseWriter, r *http.Request) {
	if !s.fedCap(w, r, federation.CapSessionsRead) {
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/api/sessions/")
	id := strings.TrimSuffix(path, "/current-status")
	if id == "" {
		http.Error(w, "session id required", http.StatusBadRequest)
		return
	}
	sess, ok := s.manager.GetSession(id)
	if !ok {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}
	if sess.State != session.StateRunning && sess.State != session.StateRateLimited {
		http.Error(w, "session is not running", http.StatusConflict)
		return
	}
	if s.summarizerSvc == nil {
		http.Error(w, "summarizer not configured", http.StatusServiceUnavailable)
		return
	}
	// Read only output written since the last summary so the LLM sees the
	// current prompt's work, not the entire session history.
	text, newOffset, err := s.manager.OutputSince(sess.FullID, sess.SummaryLogOffset)
	if err != nil {
		http.Error(w, "failed to read output: "+err.Error(), http.StatusInternalServerError)
		return
	}
	// No new output (or too thin to summarize meaningfully): respond with the
	// same JSON contract as the success path, flagged no_change, so PWA + PWS +
	// MCP callers all get a parseable body. 204 was forbidden to carry a body
	// (RFC 7231 §3.3) and Go silently dropped the message, surfacing as a
	// browser JSON.parse exception on the client.
	if strings.TrimSpace(text) == "" || strings.Count(strings.TrimSpace(text), "\n") < 4 {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"current_status":      "no change since last refresh",
			"current_status_long": "",
			"generated_at":        time.Now(),
			"no_change":           true,
		})
		return
	}
	// Pass previous short summary as context so the LLM doesn't repeat it.
	prevShort := ""
	if prev, ok := s.manager.GetSummary(sess.FullID); ok {
		prevShort = prev.Summary
	}
	ctx, cancel := context.WithTimeout(r.Context(), 35*time.Second)
	defer cancel()
	short, long, err := s.summarizerSvc.SummarizeDual(ctx, text, prevShort)
	if err != nil {
		http.Error(w, "summarizer error: "+err.Error(), http.StatusInternalServerError)
		return
	}
	// Only advance the watermark when we got a real summary; if the LLM
	// returned empty (disabled, unconfigured, or model misbehaved) keep the
	// offset where it is so the output is included in the next call.
	if strings.TrimSpace(short) != "" {
		s.manager.UpdateSummaryOffset(sess.FullID, newOffset)
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
		"current_status":      short,
		"current_status_long": long,
		"generated_at":        time.Now(),
	})
}

func (s *Server) handleRenameSession(w http.ResponseWriter, r *http.Request) {
	if !s.fedCap(w, r, federation.CapSessionsWrite) {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		ID          string `json:"id"`
		SessionName string `json:"session_name"` // BL354 — alternative to id
		Name        string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	// BL354 — resolve by name if id is absent.
	targetID := req.ID
	if targetID == "" && req.SessionName != "" {
		sess, resolveErr := s.resolveSessionAny("", req.SessionName)
		if resolveErr != nil {
			http.Error(w, resolveErr.Error(), http.StatusConflict)
			return
		}
		if sess == nil {
			http.Error(w, "session not found", http.StatusNotFound)
			return
		}
		targetID = sess.FullID
	}
	if err := s.manager.Rename(targetID, req.Name); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	s.broadcastSessionsAsync()
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
	if !s.fedCap(w, r, federation.CapSessionsWrite) {
		return
	}
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
		s.broadcastSessionsAsync()
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok", "agent_id": req.AgentID}) //nolint:errcheck
}

// handleSetSessionState allows manual override of a session's state.
// POST /api/sessions/send {"session_id":"...","text":"...","source":"..."}
// Dedicated send-input endpoint for agent-to-agent and CLI/API use.
// Returns {"ok":true,"session_id":"..."} or {"ok":false,"error":"..."}.
// Source defaults to "api"; CLI sets "cli", MCP callers can set "mcp".
func (s *Server) handleSessionSend(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.fedCap(w, r, federation.CapSessionsInput) {
		return
	}
	var body struct {
		SessionID string `json:"session_id"`
		Text      string `json:"text"`
		Source    string `json:"source"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]interface{}{"ok": false, "error": "bad request"}) //nolint:errcheck
		return
	}
	if body.SessionID == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]interface{}{"ok": false, "error": "session_id required"}) //nolint:errcheck
		return
	}
	if body.Text == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]interface{}{"ok": false, "error": "text required"}) //nolint:errcheck
		return
	}
	source := body.Source
	if source == "" {
		source = "api"
	}
	text := s.expandImageTags(body.Text)
	if err := s.manager.SendInput(body.SessionID, text, source); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]interface{}{"ok": false, "error": err.Error()}) //nolint:errcheck
		return
	}
	s.broadcastSessionsAsync()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "session_id": body.SessionID}) //nolint:errcheck
}

// POST /api/sessions/state {"id":"...","state":"running|waiting_input|stalled|complete|killed|failed|rate_limited"}
func (s *Server) handleSetSessionState(w http.ResponseWriter, r *http.Request) {
	if !s.fedCap(w, r, federation.CapSessionsWrite) {
		return
	}
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
	// "stalled" is a valid override target: it is non-terminal, so it belongs on
	// the settable side of the split (a manual stall must be writable, and
	// revertible to running, just like rate_limited).
	validStates := map[string]bool{"running": true, "waiting_input": true, "stalled": true, "complete": true, "killed": true, "failed": true, "rate_limited": true}
	if !validStates[req.State] {
		http.Error(w, "invalid state", http.StatusBadRequest)
		return
	}
	if err := s.manager.SetState(req.ID, session.State(req.State)); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	s.broadcastSessionsAsync()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"}) //nolint:errcheck
}

// POST /api/sessions/set_llm_ref {"id":"...","llm_ref":"..."}
// Updates a session's LLM registry binding in-place (safe while running).
func (s *Server) handleSetSessionLLMRef(w http.ResponseWriter, r *http.Request) {
	if !s.fedCap(w, r, federation.CapSessionsWrite) {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		ID     string `json:"id"`
		LLMRef string `json:"llm_ref"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if req.ID == "" {
		http.Error(w, "id required", http.StatusBadRequest)
		return
	}
	if err := s.manager.UpdateLLMRef(req.ID, req.LLMRef); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	s.broadcastSessionsAsync()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok", "id": req.ID, "llm_ref": req.LLMRef}) //nolint:errcheck
}

// handleSetSessionPriority sets a session's capacity-queue priority.
//
// POST /api/sessions/set_priority {"id":"<session>"|"session_name":"<name>","priority":<int>}
func (s *Server) handleSetSessionPriority(w http.ResponseWriter, r *http.Request) {
	if !s.fedCap(w, r, federation.CapSessionsWrite) {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		ID          string `json:"id"`
		SessionName string `json:"session_name"`
		Priority    any    `json:"priority"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	priority, perr := session.ParsePriority(req.Priority)
	if perr != nil {
		http.Error(w, perr.Error(), http.StatusBadRequest)
		return
	}
	targetID := req.ID
	if targetID == "" && req.SessionName != "" {
		sess, resolveErr := s.resolveSessionAny("", req.SessionName)
		if resolveErr != nil {
			http.Error(w, resolveErr.Error(), http.StatusConflict)
			return
		}
		if sess == nil {
			http.Error(w, "session not found", http.StatusNotFound)
			return
		}
		targetID = sess.FullID
	}
	if targetID == "" {
		http.Error(w, "id or session_name required", http.StatusBadRequest)
		return
	}
	if err := s.manager.SetPriority(targetID, priority); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	s.broadcastSessionsAsync()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"status": "ok", "id": targetID, "priority": priority}) //nolint:errcheck
}

// handleKillSession terminates a running or waiting session.
func (s *Server) handleKillSession(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.fedCap(w, r, federation.CapSessionsKill) {
		return
	}
	var req struct {
		ID          string `json:"id"`
		SessionName string `json:"session_name"` // BL354 — alternative to id
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	// BL354 — resolve by name if id is absent.
	targetID := req.ID
	if targetID == "" && req.SessionName != "" {
		sess, resolveErr := s.resolveSessionAny("", req.SessionName)
		if resolveErr != nil {
			http.Error(w, resolveErr.Error(), http.StatusConflict)
			return
		}
		if sess == nil {
			http.Error(w, "session not found", http.StatusNotFound)
			return
		}
		targetID = sess.FullID
	}
	why := session.EndReason{
		Caller: endCallerRESTSessionKill,
		Reason: fmt.Sprintf("REST POST /api/sessions/kill requested by %s", s.auditActor(r.Context())),
	}
	if err := s.manager.EndSession(targetID, why); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	s.audit(r.Context(), "kill", "session", targetID, nil) // GH#201 Phase 4
	s.broadcastSessionsAsync()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"}) //nolint:errcheck
}

// handleDeleteSession removes a session and optionally its tracking data.
func (s *Server) handleDeleteSession(w http.ResponseWriter, r *http.Request) {
	if !s.fedCap(w, r, federation.CapSessionsKill) {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		ID         string `json:"id"`
		DeleteData bool   `json:"delete_data"` // also remove tracking dir from disk
		// v8.36.0 — memory-strategy on session delete (keep/purge/archive),
		// matching the PRD hard-delete BL386/#175 feature this endpoint
		// never had: a session's memories were always silently kept with
		// no operator choice.
		MemoryStrategy    string `json:"memory_strategy,omitempty"`
		ArchiveRoleFilter string `json:"archive_role_filter,omitempty"`
		ArchiveToScope    string `json:"archive_to_scope,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if (req.MemoryStrategy == "purge" || req.MemoryStrategy == "archive") && s.memoryBackend != nil {
		// Look up the session's project dir BEFORE deleting it (the
		// PurgeScope/ArchiveScope calls only need the SessionID, but
		// ArchiveScope's target ref benefits from Project for scoping the
		// destination correctly, matching the PRD hard-delete pattern).
		var projectDir string
		if sess, ok := s.manager.GetSession(req.ID); ok {
			projectDir = sess.ProjectDir
		}
		sessRef := memory.ScopeRef{Scope: memory.ScopeSessionLocal, Project: projectDir, SessionID: req.ID}
		if req.MemoryStrategy == "archive" {
			archiveFilter := memory.SeedFilter{}
			if req.ArchiveRoleFilter != "" {
				archiveFilter.RolePrefix = req.ArchiveRoleFilter
			}
			archiveTo := req.ArchiveToScope
			if archiveTo == "" {
				archiveTo = "project-shared"
			}
			toRef := memory.ScopeRef{Scope: memory.Scope(archiveTo), Project: projectDir}
			_, _, _ = memory.ArchiveScope(s.memoryBackend, sessRef, toRef, archiveFilter, req.ID, 1000)
		} else { // purge
			_, _ = memory.PurgeScope(s.memoryBackend, sessRef)
		}
	}
	if err := s.manager.Delete(req.ID, req.DeleteData); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	// GH#201 Phase 4 — deleting a session is MORE sensitive than
	// killing one (it can also destroy tracking data and memories),
	// and had no audit entry at all.
	s.audit(r.Context(), "delete", "session", req.ID, map[string]any{
		"delete_data":     req.DeleteData,
		"memory_strategy": req.MemoryStrategy,
	})
	s.broadcastSessionsAsync()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"}) //nolint:errcheck
}

// nodeHostFor resolves the host a session placed on compute node nodeRef talks
// to, from the compute registry. ollamaURL is the Ollama base URL that opencode
// writes into opencode.json (opencode on an Ollama-kind node only). nodeAddress
// is the node's own address, carried on the session as NodeAddress for the pi
// family and opencode, so a resume or restart reuses the same host. Both are
// empty when nodeRef is empty, the registry is not wired, the backend is
// neither family, or the node has no address.
func (s *Server) nodeHostFor(nodeRef, backend string) (ollamaURL, nodeAddress string) {
	if nodeRef == "" || s.computeReg == nil {
		return "", ""
	}
	isOpenCode := backend == string(inference.KindOpenCode) || backend == string(inference.KindOpenCodeACP)
	isPi := backend == string(inference.KindPi) || backend == string(inference.KindPiPrompt)
	if !isOpenCode && !isPi {
		return "", ""
	}
	n, err := s.computeReg.Get(nodeRef)
	if err != nil || n == nil || n.Address == "" {
		return "", ""
	}
	addr := n.Address
	if !strings.HasPrefix(addr, "http://") && !strings.HasPrefix(addr, "https://") {
		addr = "http://" + addr
	}
	if isOpenCode && n.Kind == compute.KindOllama {
		ollamaURL = addr
	}
	return ollamaURL, addr
}

// handleStartSession starts a new session with optional backend and name overrides.
func (s *Server) handleStartSession(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.fedCap(w, r, federation.CapSessionsWrite) {
		return
	}
	var req struct {
		Task       string `json:"task"`
		ProjectDir string `json:"project_dir"`
		Backend    string `json:"backend"`
		Name       string `json:"name"`
		ResumeID   string `json:"resume_id"`
		Profile    string `json:"profile"`
		// Deprecated and ignored: task sessions are committed automatically.
		// Retained so existing clients and templates keep working.
		AutoGitCommit *bool  `json:"auto_git_commit,omitempty"`
		AutoGitInit   *bool  `json:"auto_git_init,omitempty"`
		Effort        string `json:"effort,omitempty"`   // BL41
		Template      string `json:"template,omitempty"` // BL5
		Project       string `json:"project,omitempty"`  // BL27
		// v5.27.5 — claude-code per-session overrides forwarded as
		// `--permission-mode <value>` / `--model <value>` /
		// `--effort <value>` (claude's own --effort enum, not the
		// BL41 thoroughness level above). Empty = use config defaults.
		PermissionMode string `json:"permission_mode,omitempty"`
		Model          string `json:"model,omitempty"`
		ClaudeEffort   string `json:"claude_effort,omitempty"`
		// ThinkingLevel — pi --thinking level override (off|minimal|low|
		// medium|high); the autonomous executor populates it from the
		// resolved per-task → per-story → per-PRD chain. Empty = the
		// session manager's own pi.thinking_level config applies.
		ThinkingLevel string `json:"thinking_level,omitempty"`
		// WorkerProfile — the resolved worker session profile to apply to
		// this one launch (claude-code only; ignored by other backends).
		// Absent = the plain, non-profile launch.
		WorkerProfile *workerprofile.WorkerProfile `json:"worker_profile,omitempty"`
		// v5.26.21 — autonomous PRDs with project_profile but no
		// cluster_profile fall through here. Resolve the profile to a
		// git URL + branch, clone into a per-session workspace, and
		// use that as the worker's project_dir. Cloned with whatever
		// auth the daemon's user has locally (SSH agent or git
		// credential helper); F10 BL113 token-broker integration is a
		// follow-up.
		ProjectProfile string `json:"project_profile,omitempty"`
		// v7.0.0-alpha.21 (#259) — operator picks the LLM + ComputeNode
		// directly from the v7 unified registries. Additive: when LLM is
		// supplied, its Kind becomes the session backend (overrides
		// req.Backend) and ComputeNode is validated against the LLM's
		// compute_nodes list. When ComputeNode alone is supplied without
		// LLM, the request is rejected (we can't pick an LLM for the
		// operator). When neither is supplied, the legacy req.Backend
		// path runs unchanged.
		LLM         string `json:"llm,omitempty"`
		ComputeNode string `json:"compute_node,omitempty"`
		// ComputeNodeAdvisory marks ComputeNode as the scheduler's placement
		// rather than an operator pick. An advisory node outside the entry's
		// compute_nodes list is dropped and placement chooses again.
		ComputeNodeAdvisory bool `json:"compute_node_advisory,omitempty"`
		// OneShot — when true, DATAWATCH_COMPLETE: in pane output terminates
		// the session (StateComplete). Default false keeps sessions alive for
		// interactive use. Autonomous task runners set this to true.
		OneShot bool `json:"one_shot,omitempty"`
		// LSPLanguage (v8.7.0) — language server preset for OpenCode sessions.
		// Must match a key in cfg.LSP.Servers. Empty = no LSP.
		LSPLanguage string `json:"lsp_language,omitempty"`
		// Chrome (v8.8.3) — pass --chrome or --no-chrome to claude-code.
		// Nil/absent = omit flag.
		Chrome *bool `json:"chrome,omitempty"`
		// BL347/BL351 — session lineage.
		// ParentID: FullID (hostname-hex) of the calling session. Auto-set
		// by the MCP start_session tool; pass explicitly from REST callers.
		// KillChildren: when true, killing this session cascades to direct children.
		// KillChildrenRecursive: cascades to all descendants (BL351).
		ParentID              string `json:"parent_id,omitempty"`
		KillChildren          bool   `json:"kill_children,omitempty"`
		KillChildrenRecursive bool   `json:"kill_children_recursive,omitempty"`
		// PRDID, TaskID, StoryID link an autonomous task session back to its executor.
		// StoryID added BL385 Phase 4 for memory scope routing.
		PRDID   string `json:"prd_id,omitempty"`
		TaskID  string `json:"task_id,omitempty"`
		StoryID string `json:"story_id,omitempty"`
		// AutomatonID and SpendRole attribute the session's spend; stored at start.
		AutomatonID string `json:"automaton_id,omitempty"`
		SpendRole   string `json:"spend_role,omitempty"`
		// TaskTitle is the bare autonomous task title (no "autonomous:" prefix).
		TaskTitle string `json:"task_title,omitempty"`
		// WriteScope (auto-commit leak fix) — the session's effective write
		// directories. Automatic commits stage only files under these
		// directories instead of the whole tree. Empty = historical broad
		// `git add -A` behavior (warns).
		WriteScope []string `json:"write_scope,omitempty"`
		// ValidateCommands (validation gate): run in the project dir before a
		// one-shot may commit. Required to auto-commit an ad-hoc one-shot.
		ValidateCommands []string `json:"validate_commands,omitempty"`
		// SkipSnapshot — planning/judgment sessions must never stage or
		// commit the repo. When true, the pre-session snapshot commit and
		// the completion commit are both skipped.
		SkipSnapshot bool `json:"skip_snapshot,omitempty"`
		// RequireExistingWorktree — the project dir is an Automaton-owned
		// linked worktree; the session must fail rather than create or
		// initialise it.
		RequireExistingWorktree bool `json:"require_existing_worktree,omitempty"`
		// ScrubTmuxEnv: PRD-owned launchers ask the pane shell to unset
		// TMUX and TMUX_TMPDIR before the LLM starts, so tmux commands the
		// LLM runs cannot reach the production tmux server.
		ScrubTmuxEnv bool `json:"scrub_tmux_env,omitempty"`
		// Git — true when the PRD that spawned this session has git settings
		// (prd.Git populated or a project profile). The session then runs git
		// activity: pre-session snapshot, checkpoint tag, completion commit.
		// PRD / git-less sessions may omit it; an explicit value always wins;
		// session.git_default config value (default false = existing behavior) when the field is omitted.
		Git *bool `json:"git,omitempty"`
		// BL386 Phase 1 — optional warm-start seeding at session creation.
		// When from_scope is set, memories are copied from that scope into
		// the new session's session-local scope after it starts.
		MemorySeed *struct {
			FromScope  string   `json:"from_scope"`
			Project    string   `json:"project"`
			Max        int      `json:"max,omitempty"`
			RoleFilter []string `json:"role_filter,omitempty"`
		} `json:"memory_seed,omitempty"`
		// Priority — capacity-queue priority for the session (integer,
		// higher first, default 0). Kept untyped so a non-integer value is
		// rejected with a priority-specific error.
		Priority any `json:"priority,omitempty"`
		// Tags (BL461) — the session's own tag IDs. Each must exist in the
		// tag registry; an unknown ID refuses the start.
		Tags []string `json:"tags,omitempty"`
		// Launch tagging (2026-11-06-automata-pages-redesign.md §12) —
		// optional session-launch facts: the role the session plays, the
		// kind of work it does, the Automaton (PRD) it runs for and the
		// Automaton stage it belongs to. Absent = no launch tags, exactly
		// as before. All handling lives in session_launch_tags.go.
		LaunchRole      string `json:"launch_role,omitempty"`
		LaunchKind      string `json:"launch_kind,omitempty"`
		LaunchAutomaton string `json:"launch_automaton,omitempty"`
		LaunchStage     string `json:"launch_stage,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if err := validateSessionLaunch(req.LaunchRole, req.LaunchKind); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	startTags, tagErr := s.manager.ValidateTagIDs(req.Tags)
	if tagErr != nil {
		writeTagError(w, tagErr)
		return
	}
	startPriority := 0
	if req.Priority != nil {
		p, perr := session.ParsePriority(req.Priority)
		if perr != nil {
			http.Error(w, perr.Error(), http.StatusBadRequest)
			return
		}
		startPriority = p
	}
	// v7.0.0-alpha.21 (#259) — resolve operator's {compute_node, llm} pick
	// from the v7 unified registries. Lookup happens BEFORE template/profile
	// resolution so the operator's explicit pick overrides any defaults.
	// Validation rules:
	//   - llm alone → load LLM, set Backend=string(LLM.Kind); first
	//     compute_nodes entry becomes the implicit ComputeNode (preserves
	//     LLM-defined failover order).
	//   - llm + compute_node → as above, plus require compute_node to be
	//     in LLM.ComputeNodes (or LLM.ComputeNodes empty = any node OK).
	//   - compute_node alone → 400 (operator must pick an LLM too; we
	//     don't auto-pick because Kind selection is a deliberate choice).
	var resolvedLLMRef, resolvedComputeNodeRef string
	// resolvedPoolNodes is how many compute nodes the resolved entry lists.
	var resolvedPoolNodes int
	if req.LLM != "" {
		if s.inferenceReg == nil {
			http.Error(w, "llm registry not wired (v7.0.0 inference subsystem disabled)", http.StatusBadRequest)
			return
		}
		llm, err := s.inferenceReg.Get(req.LLM)
		if err != nil {
			http.Error(w, "llm not found: "+req.LLM, http.StatusBadRequest)
			return
		}
		if llm.Disabled {
			http.Error(w, "llm "+req.LLM+" is disabled (toggle on in Settings → Compute → LLMs)", http.StatusBadRequest)
			return
		}
		resolvedLLMRef = llm.Name
		resolvedPoolNodes = len(llm.ComputeNodes)
		// LLM.Kind becomes the session backend (overrides req.Backend).
		req.Backend = string(llm.Kind)
		if req.ComputeNodeAdvisory && len(llm.ComputeNodes) > 0 && !slices.Contains(llm.ComputeNodes, req.ComputeNode) {
			req.ComputeNode = ""
		}
		switch {
		case req.ComputeNode != "":
			// Validate against LLM's compute_nodes list when set.
			if len(llm.ComputeNodes) > 0 {
				found := false
				for _, n := range llm.ComputeNodes {
					if n == req.ComputeNode {
						found = true
						break
					}
				}
				if !found {
					http.Error(w, "compute_node "+req.ComputeNode+" not in llm "+req.LLM+" compute_nodes list", http.StatusBadRequest)
					return
				}
			}
			resolvedComputeNodeRef = req.ComputeNode
		case len(llm.ComputeNodes) > 0:
			resolvedComputeNodeRef = llm.ComputeNodes[0]
		}
		// If a ComputeNode is resolved, sanity-check it exists in the
		// registry. Stale LLM entries that point at a deleted Node should
		// fail loud here rather than silently at dispatch time.
		if resolvedComputeNodeRef != "" && s.computeReg != nil {
			if _, err := s.computeReg.Get(resolvedComputeNodeRef); err != nil {
				http.Error(w, "compute_node "+resolvedComputeNodeRef+" referenced by llm "+req.LLM+" not found in registry", http.StatusBadRequest)
				return
			}
		}
	}
	// v8.19.10 — Named v7 inference-registry LLMs (e.g. "ollama-datawatch")
	// arrive via req.Backend when the autonomous executor spawns task sessions
	// (it sends backend, not llm). The session manager's legacy llm.Get() only
	// knows fixed kind strings ("ollama", "openwebui", "claude-code", …) and
	// silently falls back to its default (claude-code) when the named entry is
	// not found. Translate named inference-registry backends to their kind here
	// so the manager receives the correct kind string.
	if req.LLM == "" && req.Backend != "" && s.inferenceReg != nil {
		if entry, err := s.inferenceReg.Get(req.Backend); err == nil && entry != nil && !entry.Disabled {
			resolvedLLMRef = entry.Name
			resolvedPoolNodes = len(entry.ComputeNodes)
			req.Backend = string(entry.Kind)
			if req.ComputeNodeAdvisory && len(entry.ComputeNodes) > 0 && !slices.Contains(entry.ComputeNodes, req.ComputeNode) {
				req.ComputeNode = ""
			}
			switch {
			case req.ComputeNode != "":
				// A held node (the autonomous lease, or an operator pick) decides
				// where the session runs. It must be one of the entry's
				// compute_nodes when the entry lists any, and it wins over the
				// first entry.
				if len(entry.ComputeNodes) > 0 && !slices.Contains(entry.ComputeNodes, req.ComputeNode) {
					http.Error(w, "compute_node "+req.ComputeNode+" not in llm "+entry.Name+" compute_nodes list", http.StatusBadRequest)
					return
				}
				if s.computeReg != nil {
					if _, err := s.computeReg.Get(req.ComputeNode); err != nil {
						http.Error(w, "compute_node "+req.ComputeNode+" not found in registry", http.StatusBadRequest)
						return
					}
				}
				resolvedComputeNodeRef = req.ComputeNode
			case len(entry.ComputeNodes) > 0 && resolvedComputeNodeRef == "":
				resolvedComputeNodeRef = entry.ComputeNodes[0]
			}
		}
	}
	// compute_node is accepted with a bare backend only when that backend names
	// an enabled inference-registry entry (handled above). Anything else keeps
	// the rule that compute_node needs an llm.
	if req.LLM == "" && req.ComputeNode != "" && resolvedLLMRef == "" {
		http.Error(w, "compute_node requires llm — pick an LLM whose compute_nodes include this node, or omit compute_node", http.StatusBadRequest)
		return
	}
	// v8.7.1 — resolve Ollama URL from compute node for opencode sessions
	// backed by an Ollama-kind node.
	// BL449 incident 2026-10-15: this used to gate purely on req.Model
	// already carrying the "ollama/" prefix. A per-task model override
	// (set_task_llm using the registry's bare fixture name, e.g.
	// "qwen3:1.7b") never had that prefix, so this silently skipped —
	// no Ollama URL, no provider block in opencode.json — and every
	// worker session failed to launch with ProviderModelNotFoundError.
	// Gate on the compute node's own Kind instead (known independent of
	// how the caller spelled the model), and normalize req.Model so
	// opencode.json always gets the form it requires, regardless of
	// which spelling the task override used.
	// A pi session binds to its compute node when the entry is a pool of two or
	// more nodes, or when a one-shot start names the node its lease was granted
	// on: that node is authoritative whatever the entry size. A one-node entry
	// that names no node keeps pi's own provider table, as before.
	hostRef := resolvedComputeNodeRef
	leasedNode := req.OneShot && req.ComputeNode != ""
	if resolvedPoolNodes < 2 && !leasedNode && (req.Backend == string(inference.KindPi) || req.Backend == string(inference.KindPiPrompt)) {
		hostRef = ""
	}
	resolvedOllamaURL, resolvedNodeAddress := s.nodeHostFor(hostRef, req.Backend)
	if resolvedOllamaURL != "" && req.Model != "" && !strings.HasPrefix(req.Model, "ollama/") {
		req.Model = "ollama/" + req.Model
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
	// v6.13.7 — Skills inherited from the resolved project profile (when
	// req.ProjectProfile is set). Empty unless the profile lists any.
	var profileSkills []string
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
		cloneRoot := s.statePath("workspaces")
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
		// v6.13.7 — if the project profile lists Skills, hand them to the
		// session so InjectSkills runs at session start. Operator's
		// confusion in the v6.12.0 unclassified batch: wizard hint pointed
		// to "Settings → Agents → Project Profiles → Skills" but the
		// field didn't exist on the struct. Now it does, and this is
		// the wire-up that gives it effect.
		if len(prof.Skills) > 0 {
			profileSkills = append([]string(nil), prof.Skills...)
		}
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
	// thinking_level (pi --thinking) — reject invalid values outright at the
	// write edge instead of storing a value that trips pi later.
	if req.ThinkingLevel != "" {
		if err := config.ValidatePiThinkingLevel(req.ThinkingLevel); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	}

	gitFlag := false
	if req.Git != nil {
		gitFlag = *req.Git
	} else if s.cfg != nil {
		gitFlag = s.cfg.Session.GitDefault
	}

	var profileFallbackReason string
	workerProfileLaunchFn := s.workerProfileLaunchFn(req.WorkerProfile, req.Backend, &profileFallbackReason)

	opts := &session.StartOptions{
		LaunchFn:              workerProfileLaunchFn,
		Name:                  req.Name,
		Backend:               req.Backend,
		ResumeID:              req.ResumeID,
		AutoGitCommit:         req.AutoGitCommit,
		AutoGitInit:           req.AutoGitInit,
		Effort:                req.Effort,
		PermissionMode:        req.PermissionMode,
		Model:                 req.Model,
		ClaudeEffort:          req.ClaudeEffort,
		ThinkingLevel:         req.ThinkingLevel,
		EphemeralWorkspace:    ephemeralWorkspace,
		Skills:                profileSkills,
		LLMRef:                resolvedLLMRef,
		ComputeNodeRef:        resolvedComputeNodeRef,
		OneShot:               req.OneShot || (s.cfg != nil && s.cfg.Session.OneShotSessions),
		LSPLanguage:           req.LSPLanguage,
		OllamaURL:             resolvedOllamaURL,
		NodeAddress:           resolvedNodeAddress,
		Chrome:                req.Chrome,
		ParentID:              req.ParentID,
		KillChildren:          req.KillChildren,
		KillChildrenRecursive: req.KillChildrenRecursive,
		PRDID:                 req.PRDID,
		TaskID:                req.TaskID,
		StoryID:               req.StoryID,
		AutomatonID:           req.AutomatonID,
		SpendRole:             req.SpendRole,
		TaskTitle:             req.TaskTitle,
		WriteScope:            req.WriteScope,
		ValidateCommands:      req.ValidateCommands,
		SkipSnapshot:          req.SkipSnapshot,
		ScrubTmuxEnv:          req.ScrubTmuxEnv,
		Git:                   gitFlag,
		Priority:              startPriority,
		Tags:                  startTags,
	}
	opts.RequireExistingWorktree = req.RequireExistingWorktree
	// Empty per-request overrides fall through to LLM registry (v7.0.0 clean move).
	if opts.PermissionMode == "" && s.inferenceReg != nil {
		if cl, err := s.inferenceReg.Get("claude-code"); err == nil && cl != nil && cl.PermissionMode != "" {
			opts.PermissionMode = cl.PermissionMode
		}
	}
	// Wire LLM registry session-backend settings into opts so the session
	// manager uses per-named-LLM values instead of global cfg defaults.
	if s.inferenceReg != nil {
		llmName := resolvedLLMRef
		if llmName == "" {
			llmName = opts.Backend
		}
		if llmName == "" {
			llmName = req.Backend
		}
		if llmName != "" {
			if llmEntry, err := s.inferenceReg.Get(llmName); err == nil && llmEntry != nil {
				if llmEntry.OutputMode != "" && opts.OutputMode == "" {
					opts.OutputMode = llmEntry.OutputMode
				}
				if llmEntry.InputMode != "" && opts.InputMode == "" {
					opts.InputMode = llmEntry.InputMode
				}
				if llmEntry.ConsoleCols > 0 && opts.ConsoleCols == 0 {
					opts.ConsoleCols = llmEntry.ConsoleCols
				}
				if llmEntry.ConsoleRows > 0 && opts.ConsoleRows == 0 {
					opts.ConsoleRows = llmEntry.ConsoleRows
				}
			}
		}
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
	// v8.36.0 — gate a genuine interactive session start on the same
	// node:/llm: capacity pools an autonomous task would use for this same
	// backend+model, so a manual session against a shared compute node
	// actually contends with autonomous tasks for its real capacity instead
	// of being invisible to the ledger. Autonomous task spawns (OneShot)
	// skip this — they already admit via the executor's own m.admit()
	// before ever reaching this handler; gating them here too would
	// double-acquire the same pool and could deadlock a node capped at 1.
	// Short timeout (a few seconds, not the autonomous path's hours-long
	// default) because this blocks a synchronous HTTP request.
	capacityHolder := ""
	if !req.OneShot && s.capacityAdmit != nil {
		holder, herr := generateStreamID()
		if herr != nil {
			http.Error(w, "capacity: could not generate holder id: "+herr.Error(), http.StatusInternalServerError)
			return
		}
		waitSecs := 8
		if s.cfg != nil && s.cfg.Session.CapacityWaitSeconds > 0 {
			waitSecs = s.cfg.Session.CapacityWaitSeconds
		}
		admitCtx, cancel := context.WithTimeout(r.Context(), time.Duration(waitSecs)*time.Second)
		grantedNode, aerr := s.capacityAdmit(admitCtx, resolvedLLMRef, req.Model, holder, startPriority)
		cancel()
		if aerr != nil {
			jsonError(w, http.StatusServiceUnavailable, "capacity: "+aerr.Error())
			return
		}
		capacityHolder = holder
		// The ledger placed this start on grantedNode, so the session runs there,
		// not on the backend's first node. A compute_node the operator named is
		// kept as named.
		if grantedNode != "" && req.ComputeNode == "" && grantedNode != resolvedComputeNodeRef {
			resolvedComputeNodeRef = grantedNode
			resolvedOllamaURL, resolvedNodeAddress = s.nodeHostFor(grantedNode, req.Backend)
			if resolvedOllamaURL != "" && req.Model != "" && !strings.HasPrefix(req.Model, "ollama/") {
				req.Model = "ollama/" + req.Model
			}
			opts.ComputeNodeRef = resolvedComputeNodeRef
			opts.OllamaURL = resolvedOllamaURL
			opts.NodeAddress = resolvedNodeAddress
			opts.Model = req.Model
		}
	}
	sess, err := s.manager.Start(context.Background(), req.Task, "", req.ProjectDir, opts)
	if err != nil {
		if capacityHolder != "" && s.capacityRelease != nil {
			s.capacityRelease(capacityHolder)
		}
		status := http.StatusInternalServerError
		if errors.Is(err, gitguard.ErrOutsideAllowedRoots) || errors.Is(err, gitguard.ErrPathEscapes) {
			status = http.StatusBadRequest // the project_dir in the request is not allowed
		}
		http.Error(w, err.Error(), status)
		return
	}
	// The launch ran inside Start, so the fallback reason (if any) is already set.
	sess.ProfileFallbackReason = profileFallbackReason
	if capacityHolder != "" && s.capacityBind != nil {
		s.capacityBind(capacityHolder, sess.FullID)
	}
	// BL386 Phase 1 — optional warm-start seed after session creation.
	if req.MemorySeed != nil && req.MemorySeed.FromScope != "" && s.memoryBackend != nil {
		seedMax := req.MemorySeed.Max
		if seedMax <= 0 {
			seedMax = 30
		}
		rolePrefix := ""
		if len(req.MemorySeed.RoleFilter) > 0 {
			rolePrefix = req.MemorySeed.RoleFilter[0]
		}
		from := memory.ScopeRef{Scope: memory.Scope(req.MemorySeed.FromScope), Project: req.MemorySeed.Project}
		to := memory.ScopeRef{Scope: memory.ScopeSessionLocal, SessionID: sess.FullID}
		_, _ = memory.Seed(s.memoryBackend, from, to, memory.SeedFilter{RolePrefix: rolePrefix}, seedMax)
	}
	// Launch tagging (2026-11-06-automata-pages-redesign.md §12) — the only
	// call api.go makes: no launch fields → no-op, otherwise the placement
	// tags land in the session's system tags before we broadcast.
	s.applySessionLaunchTags(sess, launchFacts{
		Role:      req.LaunchRole,
		Kind:      req.LaunchKind,
		Automaton: req.LaunchAutomaton,
		Stage:     req.LaunchStage,
	})
	s.broadcastSessionsAsync()
	// GH#201 Phase 4 — session lifecycle was the plan's own leading
	// example of a state-changing action access-logging alone can't
	// explain ("a PUT /api/config, session and Automata changes are
	// missing"). task/project_dir are included for operator
	// readability; no credential-shaped fields exist on this request.
	s.audit(r.Context(), "start", "session", sess.FullID, map[string]any{
		"task":        req.Task,
		"project_dir": req.ProjectDir,
		"backend":     req.Backend,
		"parent_id":   req.ParentID,
	})
	w.Header().Set("Content-Type", "application/json")
	snap := s.manager.SessionSnapshot(sess) // the monitor goroutine is already updating sess
	json.NewEncoder(w).Encode(&snap)        //nolint:errcheck
}

// handleRestartSession restarts a session from any state in-place (BL359),
// reusing the same session ID and resuming the LLM conversation.
// Accepts optional "task" to override the session's task, and optional
// "session_name" to resolve the session by name (BL354 pattern).
func (s *Server) handleRestartSession(w http.ResponseWriter, r *http.Request) {
	if !s.fedCap(w, r, federation.CapSessionsWrite) {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		ID          string `json:"id"`
		SessionName string `json:"session_name"` // BL354 — alternative to id
		Task        string `json:"task"`         // BL359 — optional task override
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	// BL354 — resolve by name if id is absent.
	targetID := req.ID
	if targetID == "" && req.SessionName != "" {
		resolved, resolveErr := s.resolveSessionAny("", req.SessionName)
		if resolveErr != nil {
			http.Error(w, resolveErr.Error(), http.StatusConflict)
			return
		}
		if resolved == nil {
			http.Error(w, "session not found", http.StatusNotFound)
			return
		}
		targetID = resolved.FullID
	}
	// Also support path-based ID: POST /api/sessions/{id}/restart
	if targetID == "" {
		path := strings.TrimPrefix(r.URL.Path, "/api/sessions/")
		path = strings.TrimSuffix(path, "/restart")
		if path != "" {
			targetID = path
		}
	}
	sess, err := s.manager.RestartSession(context.Background(), targetID, req.Task)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.broadcastSessionsAsync()
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
	if !s.fedCap(w, r, federation.CapLLMsList) {
		return
	}
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
		scanFn := func(stream interface {
			Scan() bool
			Text() string
		}) {
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
	if !s.fedCap(w, r, federation.CapLLMsList) {
		return
	}
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
//
// v5.27.9 (BL213, datawatch#31) — extended for the mobile companion's
// device-linking flow. Lists every paired device by running
// `signal-cli -a <account> listDevices` so the mobile UI can render
// each as an unlink-target. Falls back to the legacy "running"
// heuristic when the account isn't configured (no signal-cli binary,
// or no signal.account_number) so existing PWA flows that only need
// linked=true keep working.
func (s *Server) handleLinkStatus(w http.ResponseWriter, r *http.Request) {
	if !s.fedCap(w, r, federation.CapLLMsList) {
		return
	}
	w.Header().Set("Content-Type", "application/json")
	resp := map[string]interface{}{
		"linked":         false,
		"account_number": s.cfg.Signal.AccountNumber,
		"device_name":    s.hostname,
		"devices":        []map[string]interface{}{},
	}
	if s.cfg.Signal.AccountNumber == "" {
		// No account configured — operator hasn't run setup signal yet.
		json.NewEncoder(w).Encode(resp) //nolint:errcheck
		return
	}
	cmd := exec.Command("signal-cli", "-a", s.cfg.Signal.AccountNumber, "listDevices") // #nosec G204 -- account_number from config
	out, err := cmd.CombinedOutput()
	if err != nil {
		// Account not registered or signal-cli missing — return
		// linked=false but include the error so the mobile UI can
		// surface it instead of just showing an empty list.
		resp["error"] = err.Error()
		json.NewEncoder(w).Encode(resp) //nolint:errcheck
		return
	}
	resp["linked"] = true
	resp["devices"] = parseListDevicesOutput(string(out))
	json.NewEncoder(w).Encode(resp) //nolint:errcheck
}

// parseListDevicesOutput reads `signal-cli listDevices` text output
// and returns one entry per device. Output shape (one block per device):
//
//	Device 1:
//	 Name: primary
//	 Created: 2024-...
//	 Last seen: ...
//	Device 2:
//	 Name: ...
//
// Conservative parser — falls back to whatever it could extract on
// malformed input rather than erroring (mobile UI handles partial data).
func parseListDevicesOutput(text string) []map[string]interface{} {
	var devices []map[string]interface{}
	var current map[string]interface{}
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "Device") && strings.HasSuffix(trimmed, ":") {
			if current != nil {
				devices = append(devices, current)
			}
			// "Device 1:" → id 1
			parts := strings.Fields(strings.TrimSuffix(trimmed, ":"))
			if len(parts) == 2 {
				if id, err := strconv.Atoi(parts[1]); err == nil {
					current = map[string]interface{}{"id": id}
					continue
				}
			}
			current = map[string]interface{}{}
			continue
		}
		if current == nil {
			continue
		}
		// Lines like "Name: foo" / "Created: 2024-..." / "Last seen: ..."
		if i := strings.Index(trimmed, ":"); i > 0 {
			key := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(trimmed[:i]), " ", "_"))
			val := strings.TrimSpace(trimmed[i+1:])
			current[key] = val
		}
	}
	if current != nil {
		devices = append(devices, current)
	}
	return devices
}

// handleLinkUnlink — DELETE /api/link/{deviceId}. Removes a paired
// Signal device from the primary account. Mobile companion BL21 uses
// this to surface an "Unlink" affordance per device on the Settings
// card.
//
// signal-cli's `removeDevice -d <id>` requires the primary account to
// be the caller (the device being removed must NOT be the primary
// itself). Returns 400 when the operator tries to remove device 1
// (primary) and 503 when signal-cli isn't available.
func (s *Server) handleLinkUnlink(w http.ResponseWriter, r *http.Request) {
	if !s.fedCap(w, r, federation.CapLLMsList) {
		return
	}
	if r.Method != http.MethodDelete {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	// Path is /api/link/<deviceId>
	idStr := strings.TrimPrefix(r.URL.Path, "/api/link/")
	idStr = strings.TrimSuffix(idStr, "/")
	if idStr == "" || strings.Contains(idStr, "/") {
		http.Error(w, "device id required: DELETE /api/link/<deviceId>", http.StatusBadRequest)
		return
	}
	id, err := strconv.Atoi(idStr)
	if err != nil || id <= 0 {
		http.Error(w, "device id must be a positive integer", http.StatusBadRequest)
		return
	}
	if id == 1 {
		http.Error(w, "device 1 is the primary; remove it via signal-cli unregister, not /api/link/", http.StatusBadRequest)
		return
	}
	if s.cfg.Signal.AccountNumber == "" {
		http.Error(w, "signal account_number not configured", http.StatusServiceUnavailable)
		return
	}
	cmd := exec.Command("signal-cli", "-a", s.cfg.Signal.AccountNumber, "removeDevice", "-d", strconv.Itoa(id)) // #nosec G204 -- inputs validated
	out, err := cmd.CombinedOutput()
	if err != nil {
		http.Error(w, fmt.Sprintf("signal-cli removeDevice: %v: %s", err, string(out)), http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status":    "ok",
		"device_id": id,
	})
}

// handleConfig dispatches GET (read config) and PUT (update config) requests.
func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		if !s.fedCap(w, r, federation.CapConfigRead) {
			return
		}
		s.handleGetConfig(w, r)
	case http.MethodPut:
		// BL110 extension — the single REST audit point for config writes:
		// it wraps the capability gate AND the handler, so exactly one
		// record per call is written, allowed or denied (denied records
		// carry the gate's refusal reason). Shared sink with the MCP and
		// comm surfaces.
		s.auditConfigPut("/api/config", func(w http.ResponseWriter, r *http.Request) {
			if !s.fedCap(w, r, federation.CapConfigWrite) {
				return
			}
			s.handlePutConfig(w, r)
		})(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// expandImageTags replaces [image:<path>] references in text with vision descriptions.
// Only paths within the configured file service root are read; others pass through
// unchanged to prevent arbitrary file reads via user-supplied message text.
// If the vision subsystem is not configured, [image:path] is converted to @path so
// that Claude Code sessions can use the @ file-reference notation to read the image
// directly via their own vision pipeline.
// If the file cannot be read or described, the tag is left as-is.
//
// Trailing-space rule: every bare '@<path>' form (no vision backend, and the
// timeout, error and empty-description fallbacks) gets exactly one space appended
// after the path when the matched [image:<path>] tag ends at the very end of the
// input text, so the session's @ file-reference is terminated. A tag followed by
// any text is unchanged (the following text supplies the separation). Successful
// descriptions, unreadable files and path-traversal rejections are unaffected.
//
// Each vision describe request is bounded: it runs under a context derived from
// s.cfg.Vision.EffectiveDescribeTimeout() (5s when s.cfg is nil) and is executed in
// a goroutine so a backend that ignores its context cannot block the caller past the
// deadline. On timeout or error the bare '@<path>' form is returned (the same
// conversion used when there is no vision backend) rather than leaving the [image:]
// tag in place.
func (s *Server) expandImageTags(text string) string {
	if !strings.Contains(text, "[image:") {
		return text
	}
	if s.visioner == nil {
		// No server-side vision backend — convert to @ notation so Claude Code
		// sessions pick up the image via their built-in file-reading capability.
		return replaceImageTags(text, func(match, rawPath string, atEnd bool) string {
			return bareImageRef(rawPath, atEnd)
		})
	}
	root := s.fileServiceRoot()
	return replaceImageTags(text, func(match, rawPath string, atEnd bool) string {
		imgPath := strings.TrimSpace(rawPath)
		if err := checkPathTraversal(root, imgPath); err != nil {
			return match
		}
		data, err := os.ReadFile(imgPath) //nolint:gosec // path validated against fileServiceRoot above
		if err != nil {
			return match
		}
		ct := http.DetectContentType(data)
		timeout := s.visionDescribeTimeout()
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		type describeResult struct {
			desc string
			err  error
		}
		resCh := make(chan describeResult, 1)
		go func() {
			desc, err := s.visioner.Describe(ctx, data, ct, "")
			resCh <- describeResult{desc: desc, err: err}
		}()
		var res describeResult
		select {
		case res = <-resCh:
		case <-ctx.Done():
		}
		cancel()
		if res.err != nil || res.desc == "" {
			// Bounded describe failed, timed out, or produced nothing — fall back
			// to the @ notation so the image is still available to the session.
			return bareImageRef(imgPath, atEnd)
		}
		return "[image: " + res.desc + " | path: " + imgPath + "]"
	})
}

var imageTagRe = regexp.MustCompile(`\[image:([^\]]+)\]`)

// replaceImageTags rewrites each [image:<path>] tag in text using fn, which gets
// the whole match, the raw captured path, and whether the match ends at the very
// end of text.
func replaceImageTags(text string, fn func(match, rawPath string, atEnd bool) string) string {
	locs := imageTagRe.FindAllStringSubmatchIndex(text, -1)
	if len(locs) == 0 {
		return text
	}
	var b strings.Builder
	last := 0
	for _, m := range locs {
		b.WriteString(text[last:m[0]])
		b.WriteString(fn(text[m[0]:m[1]], text[m[2]:m[3]], m[1] == len(text)))
		last = m[1]
	}
	b.WriteString(text[last:])
	return b.String()
}

// bareImageRef returns the bare '@<path>' form, with exactly one trailing space
// when the tag was at the very end of the input.
func bareImageRef(path string, atEnd bool) string {
	if atEnd {
		return "@" + path + " "
	}
	return "@" + path
}

// visionDescribeTimeout returns the per-request describe deadline: the configured
// value from s.cfg.Vision, or the 5s default when s.cfg is nil.
func (s *Server) visionDescribeTimeout() time.Duration {
	if s.cfg != nil {
		return s.cfg.Vision.EffectiveDescribeTimeout()
	}
	return config.DefaultVisionDescribeTimeout
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
		"hostname":       s.cfg.Hostname,
		"notify_exclude": s.cfg.NotifyExclude,
		// Identity backup retention: the effective value (10 when absent).
		"identity": map[string]interface{}{
			"backups_to_keep": s.cfg.Identity.EffectiveBackupsToKeep(),
		},
		// Pin keeper interval (seconds); 0 = keeper off. Per-node pin fields
		// (node_pinned_model, allow_model_swap) live on /api/compute/nodes.
		"pin_check_seconds": s.cfg.PinCheckSeconds,
		// Effective, not raw: an unset compute_placement key reads its
		// default (missing_model via EffectiveMissingModel; assume_model_size_gb
		// reads 0 = size unknown when unset).
		"compute_placement": computePlacementConfigSurface(s.cfg.ComputePlacement),
		// cost_report — the cost report engine knobs (internal/costreport);
		// each key reads its default while absent.
		"cost_report": map[string]interface{}{
			"default_group_by":    s.cfg.CostReport.EffectiveGroupBy(),
			"default_window_days": s.cfg.CostReport.EffectiveWindowDays(),
			"show_usd_estimates":  s.cfg.CostReport.EffectiveShowUSDEstimates(),
			"week_start":          s.cfg.CostReport.EffectiveWeekStart(),
			"include_peer_spend":  s.cfg.CostReport.EffectiveIncludePeerSpend(),
		},
		// resource_limits — the memory ceiling settings.
		"resource_limits": map[string]interface{}{
			"memory_share_percent": s.cfg.ResourceLimits.MemorySharePercent,
			"memory_floor_mb":      s.cfg.ResourceLimits.MemoryFloorMB,
			"memory_cap_mb":        s.cfg.ResourceLimits.MemoryCapMB,
		},
		"server": map[string]interface{}{
			"enabled":                        s.cfg.Server.Enabled,
			"host":                           s.cfg.Server.Host,
			"port":                           s.cfg.Server.Port,
			"public_url":                     s.cfg.Server.PublicURL,
			"token":                          mask(s.cfg.Server.Token),
			"tls":                            s.cfg.Server.TLSEnabled,
			"tls_auto_generate":              s.cfg.Server.TLSAutoGenerate,
			"tls_cert":                       s.cfg.Server.TLSCert,
			"tls_key":                        s.cfg.Server.TLSKey,
			"channel_port":                   s.cfg.Server.ChannelPort,
			"tls_port":                       s.cfg.Server.TLSPort,
			"proxy_sandbox_port":             s.proxySandboxPortFor(""),
			"acknowledge_unauthenticated":    s.cfg.Server.AcknowledgeUnauthenticated,
			"auto_restart_on_config":         s.cfg.Server.AutoRestartOnConfig,
			"allowed_project_roots":          append([]string{}, s.cfg.Server.AllowedProjectRoots...),
			"recent_session_minutes":         s.cfg.Server.RecentSessionMinutes,
			"session_summary_terminal_limit": s.cfg.Server.SessionSummaryTerminalLimit,
			"session_summary_text_chars":     s.cfg.Server.SessionSummaryTextChars,
			"ws_session_list_default":        s.wsSessionListDefault(),
			"ws_output_scope_default":        s.wsOutputScopeDefault(),
			"api_gzip":                       s.cfg.Server.APIGzip,
			"api_gzip_min_bytes":             s.cfg.Server.APIGzipMinBytes,
			"suppress_active_toasts":         s.cfg.Server.SuppressActiveToasts,
		},
		// BL397 — native ACME subsystem.
		"acme": map[string]interface{}{
			"enabled":      s.cfg.Acme.Enabled,
			"endpoint":     s.cfg.Acme.Endpoint,
			"domains":      s.cfg.Acme.Domains,
			"method":       s.cfg.Acme.Method,
			"renewal_days": s.cfg.Acme.RenewalDays,
			"dns01": map[string]interface{}{
				"provider":     s.cfg.Acme.DNS01.Provider,
				"token_secret": mask(s.cfg.Acme.DNS01.TokenSecret), // never echo the ${secret:...} ref or a resolved token
				"zone_id":      s.cfg.Acme.DNS01.ZoneID,
			},
			"apply": map[string]interface{}{
				"hot_swap": s.cfg.Acme.Apply.HotSwap,
			},
		},
		// GH#201 — HTTP access / WS lifecycle / auth-failure log.
		"audit": map[string]interface{}{
			"access_log_enabled": s.cfg.Audit.AccessLogEnabledOrDefault(),
			"retention_days":     s.cfg.Audit.RetentionDaysOrDefault(),
			"cef_mirror_enabled": s.cfg.Audit.CEFMirrorEnabled,
		},
		// dashboard.hide_inactive_cards + the operator console switches
		// (dashboard.console.*); an absent key reads its default, true.
		"dashboard": map[string]interface{}{
			"hide_inactive_cards": s.cfg.Dashboard.EffectiveHideInactiveCards(),
			"console": map[string]interface{}{
				"enabled":           s.cfg.Dashboard.Console.EffectiveEnabled(),
				"show_questions":    s.cfg.Dashboard.Console.EffectiveShowQuestions(),
				"show_held_blocked": s.cfg.Dashboard.Console.EffectiveShowHeld(),
				"show_lanes":        s.cfg.Dashboard.Console.EffectiveShowLanes(),
				"show_usage":        s.cfg.Dashboard.Console.EffectiveShowUsage(),
				"show_ci_release":   s.cfg.Dashboard.Console.EffectiveShowCIRelease(),
			},
		},
		"signal": map[string]interface{}{
			"enabled":        s.cfg.Signal.AccountNumber != "",
			"account_number": s.cfg.Signal.AccountNumber,
			"group_id":       s.cfg.Signal.GroupID,
			"config_dir":     s.cfg.Signal.ConfigDir,
			"device_name":    s.cfg.Signal.DeviceName,
		},
		"telegram": map[string]interface{}{
			"enabled":           s.cfg.Telegram.Enabled,
			"token":             mask(s.cfg.Telegram.Token),
			"chat_id":           s.cfg.Telegram.ChatID,
			"auto_manage_group": s.cfg.Telegram.AutoManageGroup,
		},
		"discord": map[string]interface{}{
			"enabled":             s.cfg.Discord.Enabled,
			"token":               mask(s.cfg.Discord.Token),
			"channel_id":          s.cfg.Discord.ChannelID,
			"auto_manage_channel": s.cfg.Discord.AutoManageChannel,
		},
		"slack": map[string]interface{}{
			"enabled":             s.cfg.Slack.Enabled,
			"token":               mask(s.cfg.Slack.Token),
			"channel_id":          s.cfg.Slack.ChannelID,
			"auto_manage_channel": s.cfg.Slack.AutoManageChannel,
		},
		"matrix": map[string]interface{}{
			"enabled":          s.cfg.Matrix.Enabled,
			"homeserver":       s.cfg.Matrix.Homeserver,
			"user_id":          s.cfg.Matrix.UserID,
			"access_token":     mask(s.cfg.Matrix.AccessToken),
			"room_id":          s.cfg.Matrix.RoomID,
			"auto_manage_room": s.cfg.Matrix.AutoManageRoom,
		},
		"ntfy": map[string]interface{}{
			"enabled":    s.cfg.Ntfy.Enabled,
			"server_url": s.cfg.Ntfy.ServerURL,
			"topic":      s.cfg.Ntfy.Topic,
			"token":      mask(s.cfg.Ntfy.Token),
		},
		// Observer pull protocol (pull mode) — global knobs; each key reads
		// its code default while absent (default off, 30s interval, 5s
		// timeout). Per-peer opt-in (observer_pull, observer_pull_interval_s,
		// observer_pull_timeout_s) lives on /api/federation/peers entries;
		// live per-peer state is on /api/observer/pull.
		"observer": map[string]interface{}{
			"peers": map[string]interface{}{
				"silent_alert_minutes": s.cfg.Observer.Peers.SilentAlertMinutes,
			},
			"pull": map[string]interface{}{
				"enabled":            s.cfg.Observer.Pull.Enabled,
				"default_interval_s": s.cfg.Observer.Pull.DefaultIntervalS,
				"default_timeout_s":  s.cfg.Observer.Pull.DefaultTimeoutS,
			},
		},
		"imap_mcp": map[string]interface{}{
			"enabled":        s.cfg.ImapMcp.Enabled,
			"url":            s.cfg.ImapMcp.URL,
			"account":        s.cfg.ImapMcp.Account,
			"subject_prefix": s.cfg.ImapMcp.SubjectPrefix,
			"to":             s.cfg.ImapMcp.To,
			"token":          mask(s.cfg.ImapMcp.Token),
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
			"enabled":      s.cfg.Twilio.Enabled,
			"account_sid":  mask(s.cfg.Twilio.AccountSID),
			"auth_token":   mask(s.cfg.Twilio.AuthToken),
			"from_number":  s.cfg.Twilio.FromNumber,
			"to_number":    s.cfg.Twilio.ToNumber,
			"webhook_addr": s.cfg.Twilio.WebhookAddr,
		},
		"github_webhook": map[string]interface{}{
			"enabled": s.cfg.GitHubWebhook.Enabled,
			"addr":    s.cfg.GitHubWebhook.Addr,
			"secret":  mask(s.cfg.GitHubWebhook.Secret),
		},
		"webhook": map[string]interface{}{
			"enabled":   s.cfg.Webhook.Enabled,
			"addr":      s.cfg.Webhook.Addr,
			"token":     mask(s.cfg.Webhook.Token),
			"image_dir": s.cfg.Webhook.ImageDir,
		},
		"session": func() map[string]interface{} {
			m := map[string]interface{}{
				"llm_backend":                     s.cfg.Session.LLMBackend,
				"max_sessions":                    s.cfg.Session.MaxSessions,
				"reserved_interactive":            s.cfg.Session.EffectiveReservedInteractive(),
				"input_idle_timeout":              s.cfg.Session.InputIdleTimeout,
				"liveness_window_seconds":         s.cfg.Session.LivenessWindowSeconds,
				"liveness_idle_timeout_overrides": livenessOverridesForDisplay(s.cfg.Session.LivenessIdleTimeoutOverrides),
				"tail_lines":                      s.cfg.Session.TailLines,
				"alert_context_lines":             s.cfg.Session.AlertContextLines,
				"task_delivery_window_seconds":    int(s.cfg.Session.EffectiveTaskDeliveryWindow().Seconds()),
				"task_delivery_attempts":          s.cfg.Session.EffectiveTaskDeliveryAttempts(),
				"dismiss_ui_prompts":              s.cfg.Session.EffectiveDismissUIPrompts(),
				"ui_prompt_dismiss_delay_seconds": s.cfg.Session.EffectiveUIPromptDismissDelaySeconds(),
				"ui_prompt_clear_bound_seconds":   s.cfg.Session.EffectiveUIPromptClearBoundSeconds(),
				"ui_prompt_max_attempts":          s.cfg.Session.EffectiveUIPromptMaxAttempts(),
				"store_flush_interval":            int(s.cfg.Session.EffectiveStoreFlushInterval().Milliseconds()),
				"store_archive_after_days":        s.cfg.Session.EffectiveStoreArchiveAfterDays(),
				"store_field_cap_kb":              s.cfg.Session.EffectiveStoreFieldCapKB(),
				"store_side_file_cap_kb":          s.cfg.Session.EffectiveStoreSideFileCapKB(),
				"default_project_dir":             s.cfg.Session.DefaultProjectDir,
				"workspace_root":                  s.cfg.Session.WorkspaceRoot,
				"claude_enabled":                  s.cfg.Session.ClaudeEnabled,
				"auto_git_commit":                 s.cfg.Session.AutoGitCommit,
				"auto_git_init":                   s.cfg.Session.AutoGitInit,
				"git_default":                     s.cfg.Session.GitDefault,
				"kill_sessions_on_exit":           s.cfg.Session.KillSessionsOnExit,
				"one_shot_sessions":               s.cfg.Session.OneShotSessions,
				"root_path":                       s.cfg.Session.RootPath,
				"mcp_max_retries":                 s.cfg.Session.MCPMaxRetries,
				"schedule_settle_ms":              s.cfg.Session.ScheduleSettleMs,
				"console_cols":                    s.cfg.Session.ConsoleCols,
				"console_rows":                    s.cfg.Session.ConsoleRows,
				"log_level":                       s.cfg.Session.LogLevel,
				"summarizer": map[string]interface{}{
					"enabled": s.cfg.Session.Summarizer.Enabled,
					"llm_ref": s.cfg.Session.Summarizer.LLMRef,
					"prompt":  s.cfg.Session.Summarizer.Prompt,
				},
			}
			// v7.0.0: claude-code settings moved to LLM registry.
			// Read from registry when available for API back-compat.
			if s.inferenceReg != nil {
				if cl, err := s.inferenceReg.Get("claude-code"); err == nil && cl != nil {
					m["skip_permissions"] = cl.SkipPermissions
					m["channel_enabled"] = cl.ChannelEnabled
					m["claude_auto_accept_disclaimer"] = cl.AutoAcceptDisclaimer
					m["permission_mode"] = cl.PermissionMode
					m["default_effort"] = cl.DefaultEffort
				}
			}
			return m
		}(),
		// UI display settings — the ui.* splash minimum-dwell seconds, EFFECTIVE
		// values, so an unset config shows its defaults (6 / 9).
		"ui": map[string]interface{}{
			"splash_min_seconds":             s.cfg.UI.SplashMinSecondsEffective(),
			"splash_min_seconds_new_version": s.cfg.UI.SplashMinSecondsNewVersionEffective(),
		},
		// Usage calculator settings — EFFECTIVE values, so an unset config
		// shows its defaults.
		"usage": map[string]interface{}{
			"calculator": usageCalculatorForDisplay(s.cfg.Usage.Calculator),
			"burn_guard": s.cfg.Usage.BurnGuard,
			// Billing profiles the cost report and the usage calculator price
			// from. {} when none are set — the assumed api_metered default
			// then applies per backend and the report labels it as assumed —
			// never null, so a fresh daemon's config has the same shape.
			"billing": billingProfilesForDisplay(s.cfg.Usage.Billing),
		},
		"tracing": s.cfg.Tracing.WithDefaults(),

		"classifier": classifierForDisplay(s.cfg.Classifier),
		"git": map[string]interface{}{
			"push_on_complete":          s.cfg.Git.EffectivePushOnComplete(),
			"push_remote":               s.cfg.Git.EffectivePushRemote(),
			"release_ci_window_minutes": s.cfg.Git.ReleaseCIWindowMinutes,
			// git.secret_scan — pre-push secret scan (v9.0.0). Effective
			// values, mirroring push_* above: an unset config shows its
			// defaults (mode shadow, all built-in detectors, empty allow-list).
			"secret_scan": map[string]interface{}{
				"mode":      s.cfg.Git.EffectiveSecretScanMode(),
				"detectors": s.cfg.Git.EffectiveSecretScanDetectors(),
				"allow_list": map[string]interface{}{
					"paths":        s.cfg.Git.SecretScan.AllowList.Paths,
					"fingerprints": s.cfg.Git.SecretScan.AllowList.Fingerprints,
				},
			},
		},
		"mcp": map[string]interface{}{
			"enabled":           s.cfg.MCP.Enabled,
			"sse_enabled":       s.cfg.MCP.SSEEnabled,
			"sse_host":          s.cfg.MCP.SSEHost,
			"sse_port":          s.cfg.MCP.SSEPort,
			"public_url":        s.cfg.MCP.PublicURL,
			"token":             mask(s.cfg.MCP.Token),
			"tls_enabled":       s.cfg.MCP.TLSEnabled,
			"tls_auto_generate": s.cfg.MCP.TLSAutoGenerate,
			"tls_cert":          s.cfg.MCP.TLSCert,
			"tls_key":           s.cfg.MCP.TLSKey,
			// BL412 — self-config group (alias: mcp.allow_self_config). The
			// YAML field is tri-state (absent = enabled); GET reports the
			// effective value so a read-modify-write round trip preserves the
			// enabled/disabled state. Audit path and denied-recording flag are
			// read-only here (mutable via PUT, same as every other mcp.* key).
			"allow_self_config": s.cfg.MCP.SelfConfigEnabled(),
			// allow_sensitive_writes gates the sensitive write tool class for
			// model sessions; same tri-state (absent = enabled), so GET
			// reports the effective value.
			"allow_sensitive_writes":   s.cfg.MCP.SensitiveWritesEnabled(),
			"self_config_audit_path":   s.cfg.MCP.SelfConfigAuditPath,
			"self_config_audit_denied": s.cfg.MCP.SelfConfigAuditDeniedOn(),
			// Lifecycle events resource subscriptions (a restart applies a change).
			"resource_subscriptions": s.cfg.MCP.ResourceSubscriptions,
		},
		"detection": map[string]interface{}{
			"prompt_patterns":       s.cfg.Detection.PromptPatterns,
			"completion_patterns":   s.cfg.Detection.CompletionPatterns,
			"rate_limit_patterns":   s.cfg.Detection.RateLimitPatterns,
			"input_needed_patterns": s.cfg.Detection.InputNeededPatterns,
			"prompt_debounce":       s.cfg.Detection.PromptDebounce,
			"notify_cooldown":       s.cfg.Detection.NotifyCooldown,
			"alert_settle":          s.cfg.GetDetection("").AlertSettle,
			"alert_repeat":          s.cfg.GetDetection("").AlertRepeat,
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
		"push": map[string]interface{}{
			"allow_insecure_endpoints": s.cfg.Push.AllowInsecureEndpoints,
			"block_private_endpoints":  s.cfg.Push.BlockPrivateEndpoints,
			// BL397 Phase 4 / BL335.
			"apns": map[string]interface{}{
				"enabled":    s.cfg.Push.APNs.Enabled,
				"key_id":     s.cfg.Push.APNs.KeyID,
				"team_id":    s.cfg.Push.APNs.TeamID,
				"bundle_id":  s.cfg.Push.APNs.BundleID,
				"key_path":   s.cfg.Push.APNs.KeyPath,
				"key_secret": mask(s.cfg.Push.APNs.KeySecret), // never echo the ${secret:...} ref or a resolved key
				"sandbox":    s.cfg.Push.APNs.Sandbox,
			},
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
			"enabled":                   s.cfg.OpenCode.Enabled,
			"binary":                    s.cfg.OpenCode.Binary,
			"default_model":             s.cfg.OpenCode.DefaultModel,
			"console_cols":              s.cfg.OpenCode.ConsoleCols,
			"console_rows":              s.cfg.OpenCode.ConsoleRows,
			"output_mode":               s.cfg.OpenCode.OutputMode,
			"input_mode":                s.cfg.OpenCode.InputMode,
			"ollama_chunk_timeout_sec":  s.cfg.OpenCode.OllamaChunkTimeoutSec,
			"ollama_header_timeout_sec": s.cfg.OpenCode.OllamaHeaderTimeoutSec,
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
			"enabled":      s.cfg.Aider.Enabled,
			"binary":       s.cfg.Aider.Binary,
			"console_cols": s.cfg.Aider.ConsoleCols,
			"console_rows": s.cfg.Aider.ConsoleRows,
			"output_mode":  s.cfg.Aider.OutputMode,
			"input_mode":   s.cfg.Aider.InputMode,
		},
		"pi": map[string]interface{}{
			"enabled":        s.cfg.Pi.Enabled,
			"binary":         s.cfg.Pi.Binary,
			"model":          s.cfg.Pi.Model,
			"thinking_level": s.cfg.Pi.ThinkingLevel,
			"console_cols":   s.cfg.Pi.ConsoleCols,
			"console_rows":   s.cfg.Pi.ConsoleRows,
			"output_mode":    s.cfg.Pi.OutputMode,
			"input_mode":     s.cfg.Pi.InputMode,
		},
		"pi_prompt": map[string]interface{}{
			"enabled":      s.cfg.PiPrompt.Enabled,
			"binary":       s.cfg.PiPrompt.Binary,
			"model":        s.cfg.PiPrompt.Model,
			"console_cols": s.cfg.PiPrompt.ConsoleCols,
			"console_rows": s.cfg.PiPrompt.ConsoleRows,
			"output_mode":  s.cfg.PiPrompt.OutputMode,
			"input_mode":   s.cfg.PiPrompt.InputMode,
		},
		"goose": map[string]interface{}{
			"enabled":         s.cfg.Goose.Enabled,
			"binary":          s.cfg.Goose.Binary,
			"console_cols":    s.cfg.Goose.ConsoleCols,
			"console_rows":    s.cfg.Goose.ConsoleRows,
			"output_mode":     s.cfg.Goose.OutputMode,
			"input_mode":      s.cfg.Goose.InputMode,
			"provider":        s.cfg.Goose.Provider,
			"model":           s.cfg.Goose.Model,
			"api_key_ref":     s.cfg.Goose.APIKeyRef,
			"channel_enabled": s.cfg.Goose.ChannelEnabled,
		},
		"gemini": map[string]interface{}{
			"enabled":      s.cfg.Gemini.Enabled,
			"binary":       s.cfg.Gemini.Binary,
			"console_cols": s.cfg.Gemini.ConsoleCols,
			"console_rows": s.cfg.Gemini.ConsoleRows,
			"output_mode":  s.cfg.Gemini.OutputMode,
			"input_mode":   s.cfg.Gemini.InputMode,
		},
		"openwebui": map[string]interface{}{
			"enabled":      s.cfg.OpenWebUI.Enabled,
			"url":          s.cfg.OpenWebUI.URL,
			"model":        s.cfg.OpenWebUI.Model,
			"api_key":      mask(s.cfg.OpenWebUI.APIKey),
			"console_cols": s.cfg.OpenWebUI.ConsoleCols,
			"console_rows": s.cfg.OpenWebUI.ConsoleRows,
			"output_mode":  s.cfg.OpenWebUI.OutputMode,
			"input_mode":   s.cfg.OpenWebUI.InputMode,
		},
		"shell_backend": map[string]interface{}{
			"enabled":      s.cfg.Shell.Enabled,
			"script_path":  s.cfg.Shell.ScriptPath,
			"console_cols": s.cfg.Shell.ConsoleCols,
			"console_rows": s.cfg.Shell.ConsoleRows,
			"output_mode":  s.cfg.Shell.OutputMode,
			"input_mode":   s.cfg.Shell.InputMode,
		},
		"rtk": map[string]interface{}{
			"enabled":               s.cfg.RTK.Enabled,
			"binary":                s.cfg.RTK.Binary,
			"show_savings":          s.cfg.RTK.ShowSavings,
			"auto_init":             s.cfg.RTK.AutoInit,
			"discover_interval":     s.cfg.RTK.DiscoverInterval,
			"auto_update":           s.cfg.RTK.AutoUpdate,
			"update_check_interval": s.cfg.RTK.UpdateCheckInterval,
		},
		"pipeline": map[string]interface{}{
			"max_parallel":    s.cfg.Pipeline.MaxParallel,
			"default_backend": s.cfg.Pipeline.DefaultBackend,
		},
		"web_search": map[string]interface{}{
			"enabled":     s.cfg.WebSearch.Enabled,
			"provider":    s.cfg.WebSearch.Provider,
			"url":         s.cfg.WebSearch.URL,
			"engine":      s.cfg.WebSearch.Engine,
			"num_results": s.cfg.WebSearch.NumResults,
		},
		// v4.0.8 (B38) — autonomous / plugins / orchestrator must be
		// in the GET response too. Without them the PWA and mobile
		// Settings cards render empty fields on reload even though
		// the PUT path now persists correctly.
		// Daemon-level project defaults (projects_defaults.*), effective values.
		"projects_defaults": projectsDefaultsForDisplay(s.cfg.ProjectsDefaults),
		"autonomous": map[string]interface{}{
			"enabled":               s.cfg.Autonomous.Enabled,
			"push_and_tag":          s.cfg.Autonomous.PushAndTag,
			"release_path":          effectiveReleasePath(s.cfg.Autonomous.ReleasePath),
			"poll_interval_seconds": s.cfg.Autonomous.PollIntervalSeconds,
			"max_parallel_tasks":    s.cfg.Autonomous.MaxParallelTasks, // deprecated, ignored; removal deferred one release
			// Daemon-wide default for how many independent tasks of one
			// Automaton run at once (an Automaton's own setting overrides).
			"max_concurrent_tasks": s.cfg.Autonomous.MaxConcurrentTasks,
			// B165 — boot-resume age limit in seconds (default 3600). 0 =
			// never resume, -1 = no age limit: both survive, so no
			// omitempty on the struct tag.
			"resume_max_age_seconds": s.cfg.Autonomous.ResumeMaxAgeSeconds,
			// Attempt-cost settings: which worker (backend+model) runs each
			// task kind's first attempt, how many failures push a task off
			// that pair, and the cap on prior context a retry may resume
			// with (0 = every retry starts a fresh session).
			"first_attempt":                         firstAttemptDisplay(s.cfg.Autonomous.FirstAttempt),
			"first_attempt_escalate_after_failures": s.cfg.Autonomous.FirstAttemptEscalateAfterFailures,
			"retry_resume_max_context_tokens":       s.cfg.Autonomous.RetryResumeMaxContextTokens,
			// Between-task branch refresh: off by default; the behind limit's
			// 0 reads as 5 (config.AutonomousConfig.BranchRefreshLimit).
			"branch_refresh_enabled": s.cfg.Autonomous.BranchRefreshEnabled,
			// One-time project link backfill at start: false = dry-run report
			// only, true = apply once, then a done marker stops re-runs.
			"project_link_backfill_apply": s.cfg.Autonomous.ProjectLinkBackfillApply,
			"branch_refresh_behind_limit": s.cfg.Autonomous.BranchRefreshBehindLimit,
			// Snapshot-never-stage list (path patterns the autonomous
			// snapshot must never `git add`): nil (unset) reads the built-in
			// defaults (config.DefaultSnapshotNeverStage), so render the
			// effective list.
			"snapshot_never_stage": s.cfg.Autonomous.EffectiveSnapshotNeverStage(),
			// Local-model engine sessions allowed per compute node (1 = default).
			"max_engines_per_node": s.cfg.Autonomous.MaxEnginesPerNode,
			// BL304: new key. Also emit legacy key for back-compat with old clients.
			"planning_backend":      s.cfg.Autonomous.PlanningBackend,
			"decomposition_backend": s.cfg.Autonomous.PlanningBackend,
			"verification_backend":  s.cfg.Autonomous.VerificationBackend,
			"verification_backends": s.cfg.Autonomous.VerificationBackends,
			// Verifier policy: mode, backend list, Claude session cap, and the
			// stronger-verifier settings. Rendered whole so an unset policy
			// shows its zero values rather than a missing object.
			"verifier_policy": verifierPolicyForDisplay(s.cfg.Autonomous.VerifierPolicy),
			// Worker session profiles (autonomous.worker_profiles / .worker_profile_defaults
			// / .worker_profile) — the user profiles keyed by name, the role/kind
			// defaults and the global profile name; unset tables read as empty
			// objects and the unset global name reads as "". Built-in profile
			// names (minimal, digest, full, datawatch-tools) are reserved and
			// never appear in worker_profiles.
			"worker_profiles":         workerProfilesForDisplay(s.cfg.Autonomous.WorkerProfiles),
			"worker_profile_defaults": workerProfileDefaultsForDisplay(s.cfg.Autonomous.WorkerProfileDefaults),
			"worker_profile":          s.cfg.Autonomous.WorkerProfile,
			// in_force: the profile each worker role and task kind starts with,
			// from the live values (read-only; see workerProfileInForceForDisplay).
			"in_force": workerProfileInForceForDisplay(s.cfg.Autonomous),
			// Host load check: effective mode, multiple and minutes.
			"host_load_alert": hostLoadAlertForDisplay(s.cfg.Autonomous.HostLoadAlert),
			// v9.5.5 — render the EFFECTIVE list so an unset value shows the
			// 4-item default in the PWA rather than an empty field.
			"task_override_statuses": autonomous.EffectiveTaskOverrideStatuses(s.cfg.Autonomous.TaskOverrideStatuses),
			// Render the EFFECTIVE value: unset (nil) reads as true — the
			// new queued-task override behavior — only an explicit false in
			// the config restores the pre-change refusal.
			"override_queued_tasks": autonomous.EffectiveOverrideQueuedTasks(s.cfg.Autonomous.OverrideQueuedTasks),
			// Plan requirement coverage gate — render the EFFECTIVE value
			// so the unset state (empty mode / zero ratio) shows its safe
			// defaults ("block", 1.0) rather than blanks.
			"plan_coverage_check":     effectivePlanCoverageCheckForDisplay(s.cfg.Autonomous.PlanCoverageCheck),
			"plan_coverage_min_ratio": config.NormalizePlanCoverageRatio(s.cfg.Autonomous.PlanCoverageMinRatio),
			// Plan-coverage finding filters — render the EFFECTIVE values
			// so the unset state shows its safe defaults (both filters on,
			// the scope,constraints section list, the 30-character minimum)
			// rather than zero values.
			"plan_coverage_suppress_created_files": s.cfg.Autonomous.EffectivePlanCoverageFilters().SuppressCreatedFiles,
			"plan_coverage_scoped_requirements":    s.cfg.Autonomous.EffectivePlanCoverageFilters().ScopedRequirements,
			"plan_coverage_requirement_sections":   strings.Join(s.cfg.Autonomous.EffectivePlanCoverageFilters().RequirementSections, ","),
			"plan_coverage_min_requirement_chars":  s.cfg.Autonomous.EffectivePlanCoverageFilters().MinRequirementChars,
			// Generated artefacts — the effective switches (unset = on) so
			// a fresh config shows every artefact enabled.
			"generated_artefacts": s.cfg.Autonomous.EffectiveGeneratedArtefacts(),
			// Tag browser settings (autonomous.tags) — the effective values, so an
			// unset block shows its defaults. Validated by ValidateTagSetting.
			"tags":      tagSettingsForDisplay(s.cfg.Autonomous.Tags),
			"auto_tags": autoTagsSettingsForDisplay(s.cfg.Autonomous.AutoTags),
			// Housekeeping repairs (autonomous.housekeeping_repairs) — the effective
			// mode per group (off|report|on) and the grace seconds.
			"housekeeping_repairs": housekeepingRepairsSettingsForDisplay(s.cfg.Autonomous.HousekeepingRepairs),
			// Defect queue (autonomous.defect_queue) — effective block values and
			// per-project overrides. Validated by ValidateDefectQueueSetting.
			"defect_queue": defectQueueSettingsForDisplay(s.cfg.Autonomous.DefectQueue),
			// Per-backend usage back-off — render the EFFECTIVE values so an
			// unset config shows its defaults (pause_backend, 60).
			"usage_backoff":            usageBackoffModeForDisplay(s.cfg.Autonomous.UsageBackoff),
			"backoff_max_wait_minutes": usageBackoffMaxWaitForDisplay(s.cfg.Autonomous.BackoffMaxWaitMinutes),
			// tier -> max-inflight table; a nil table renders as an empty
			// object (rather than null) so clients treat "absent tier" and
			// "empty table" the same way: kind defaults apply.
			"plan_tier_max_inflight": planTierTableForDisplay(s.cfg.Autonomous.PlanTierMaxInflight),
			// tier -> notional dollar allocation per budget window; empty
			// object when nothing is declared.
			"plan_tier_allocation_usd": planTierAllocationForDisplay(s.cfg.Autonomous.PlanTierAllocationUSD),
			// kind -> capacity-priority table — the EFFECTIVE entries, so an
			// unset table shows the shipped defaults (bug=80, maintenance=70,
			// feature=20, other=0) instead of blanks. An entry a PRD kind has
			// no row of its own for falls back to the "other" row.
			"priority_by_kind": priorityByKindForDisplay(s.cfg.Autonomous),
			// Automation that new kind:bug filings are recorded under; an
			// empty string means not configured (recorded, not guessed).
			// Deliberately empty by default: each installation chooses its own.
			"defect_intake_automation": s.cfg.Autonomous.DefectIntakeAutomation,
			"execution_backend":        s.cfg.Autonomous.ExecutionBackend,
			// BL304: new key + legacy alias.
			"planning_effort":              s.cfg.Autonomous.PlanningEffort,
			"decomposition_effort":         s.cfg.Autonomous.PlanningEffort,
			"verification_effort":          s.cfg.Autonomous.VerificationEffort,
			"planning_timeout_seconds":     s.cfg.Autonomous.PlanningTimeoutSeconds,
			"verification_timeout_seconds": s.cfg.Autonomous.VerificationTimeoutSeconds,
			// Judge routing: an empty judge_policy means the judges follow the verifier routing policy;
			// the time limit shows its 30-minute default; the size threshold is the stored value (0 = off).
			"judge_policy":               judgePolicyForDisplay(s.cfg.Autonomous.JudgePolicy),
			"judge_timeout_seconds":      config.NormalizeJudgeTimeoutSeconds(s.cfg.Autonomous.JudgeTimeoutSeconds),
			"judge_size_threshold_lines": s.cfg.Autonomous.JudgeSizeThresholdLines,
			// Judge prompt delivery: the stored values (0 = always send the full prompt / no extra wait).
			"judge_prompt_file_threshold_lines":              s.cfg.Autonomous.JudgePromptFileThresholdLines,
			"judge_delivery_wait_per_thousand_lines_seconds": s.cfg.Autonomous.JudgeDeliveryWaitPerThousandLinesSeconds,
			// Strong-first judge routing: the named strong backend (empty =
			// the last judge-policy entry) and the per-commit verdict cache
			// flag (unset reads the default, true).
			"judge_strong_backend":         s.cfg.Autonomous.JudgeStrongBackend,
			"judge_verdict_cache_enabled":  config.NormalizeJudgeVerdictCacheEnabled(s.cfg.Autonomous.JudgeVerdictCacheEnabled),
			"session_idle_timeout_seconds": s.cfg.Autonomous.SessionIdleTimeoutSeconds,
			// Effective value (nil -> default 3, so the key is never absent from
			// the read surface); 0 renders as 0 = loop detection off.
			"loop_detect_turns":                    config.LoopDetectTurnsEffective(s.cfg.Autonomous.LoopDetectTurns),
			"descendant_stall_seconds":             s.cfg.Autonomous.DescendantStallSeconds,
			"session_start_confirm_seconds":        s.cfg.Autonomous.SessionStartConfirmSeconds,
			"orphan_grace_seconds":                 s.cfg.Autonomous.OrphanGraceSeconds,
			"duplicate_session_keep":               config.NormalizeDuplicateSessionKeep(s.cfg.Autonomous.DuplicateSessionKeep),
			"restart_policy":                       config.NormalizeRestartPolicy(s.cfg.Autonomous.RestartPolicy),
			"planning_restart_policy":              config.NormalizePlanningRestartPolicy(s.cfg.Autonomous.PlanningRestartPolicy),
			"restart_quiet_timeout_seconds":        config.NormalizeRestartQuietTimeoutSeconds(s.cfg.Autonomous.RestartQuietTimeoutSeconds),
			"restart_on_timeout":                   config.NormalizeRestartOnTimeout(s.cfg.Autonomous.RestartOnTimeout),
			"stuck_running_seconds":                s.cfg.Autonomous.StuckRunningSeconds,
			"worker_idle_nudge_seconds":            s.cfg.Autonomous.WorkerIdleNudgeSeconds,
			"worker_idle_nudge_stall_seconds":      s.cfg.Autonomous.WorkerIdleNudgeStallSeconds,
			"capacity_enabled":                     s.cfg.Autonomous.CapacityEnabled == nil || *s.cfg.Autonomous.CapacityEnabled,
			"capacity_wait_timeout_seconds":        s.cfg.Autonomous.CapacityWaitTimeoutSeconds,
			"archive_grace_minutes":                s.cfg.Autonomous.ArchiveGraceMinutesOrDefault(),
			"capacity_fallthrough":                 s.cfg.Autonomous.CapacityFallthrough,
			"capacity_strict_node_limit":           s.cfg.Autonomous.CapacityStrictNodeLimitOn(),
			"capacity_count_pool_by_node":          s.cfg.Autonomous.CapacityCountPoolByNodeOn(),
			"capacity_adopt_before_admit":          s.cfg.Autonomous.CapacityAdoptBeforeAdmitOn(),
			"capacity_adoption_wait_seconds":       int(s.cfg.Autonomous.CapacityAdoptionWait() / time.Second),
			"capacity_gpu_util_pct":                s.cfg.Autonomous.CapacityGPUUtilPct,
			"capacity_rate_requests":               s.cfg.Autonomous.CapacityRateRequests,
			"capacity_rate_tokens":                 s.cfg.Autonomous.CapacityRateTokens,
			"capacity_rate_window_seconds":         s.cfg.Autonomous.CapacityRateWindowSeconds,
			"ci_watch_timeout_seconds":             s.cfg.Autonomous.CIWatchTimeoutSeconds,
			"checks":                               checksShipConfigDisplay(s.cfg)["checks"],
			"ship":                                 checksShipConfigDisplay(s.cfg)["ship"],
			"detail_text_cap_bytes":                s.cfg.Autonomous.DetailTextCapBytes,
			"inherited_tags_cache_seconds":         s.cfg.Autonomous.InheritedTagsCacheSeconds, // 0 = default (15s), negative = off
			"per_prd_guardrails":                   s.cfg.Autonomous.PerPRDGuardrails,          // rules-review, ci-watch
			"block_on_file_drift":                  s.cfg.Autonomous.BlockOnFileDrift,          // strict foreign-file-drift enforcement
			"gate_disagreement":                    s.cfg.Autonomous.GateDisagreement,          // verifier OK + gate regression: "warn" (default) or "block"
			"failure_escalation":                   failureEscalationConfigForDisplay(s.cfg.Autonomous.FailureEscalation),
			"verifier_infra_escalation_threshold":  s.cfg.Autonomous.VerifierInfraEscalationThreshold,
			"verification_record_scores":           autonomous.RecordScoresEnabled(s.cfg.Autonomous.VerificationRecordScores),
			"scope_breadth_enabled":                s.cfg.Autonomous.ScopeBreadthEnabled,
			"fallback_verification_backend":        s.cfg.Autonomous.FallbackVerificationBackend,
			"verifier_no_progress_timeout_seconds": s.cfg.Autonomous.VerifierNoProgressTimeoutSeconds,
			"verifier_max_run_seconds":             s.cfg.Autonomous.VerifierMaxRunSeconds, // 0 = no cap

			// Read-only: the idle-nudge windows the daemon applies after the
			// unset or non-positive fallback (180s nudge, 600s stall).
			"worker_idle_nudge_effective_seconds":       config.NormalizeWorkerIdleNudgeSeconds(s.cfg.Autonomous.WorkerIdleNudgeSeconds),
			"worker_idle_nudge_stall_effective_seconds": config.NormalizeWorkerIdleNudgeStallSeconds(s.cfg.Autonomous.WorkerIdleNudgeStallSeconds),

			// Plan review gate + smoke verification settings.
			"plan_review_backend":        s.cfg.Autonomous.PlanReviewBackend,
			"plan_review_model":          s.cfg.Autonomous.PlanReviewModel,
			"plan_review_mode":           s.cfg.Autonomous.PlanReviewMode,
			"smoke_verification_backend": s.cfg.Autonomous.SmokeVerificationBackend,
			"smoke_verification_model":   s.cfg.Autonomous.SmokeVerificationModel,
			// Pre-existing foreign-file-drift enforcement mode: "block"
			// (default) or "warn". Show the effective mode so an unset
			// value renders as "block" (same as the executor's
			// resolveDriftMode), matching EffectiveTaskOverrideStatuses above.
			"drift_mode": effectiveDriftMode(s.cfg.Autonomous.DriftMode),
			// Undeclared-edit enforcement: off|warn|block; unset/unknown renders "warn".
			"undeclared_edit_mode":            effectiveUndeclaredEditMode(s.cfg.Autonomous.UndeclaredEditMode),
			"stale_task_seconds":              s.cfg.Autonomous.StaleTaskSeconds,
			"task_max_attempt_seconds":        s.cfg.Autonomous.TaskMaxAttemptSeconds,
			"task_no_progress_seconds":        s.cfg.Autonomous.TaskNoProgressSeconds,
			"preserve_attempt_work":           s.cfg.Autonomous.PreserveAttemptWorkEnabled(),
			"auto_fix_retries":                s.cfg.Autonomous.AutoFixRetries,
			"verifier_diff_max_bytes":         s.cfg.Autonomous.VerifierDiffMaxBytes,
			"verifier_min_free_mem_mb":        s.cfg.Autonomous.EffectiveVerifierMinFreeMB(),
			"verifier_headroom_max_age_hours": int(s.cfg.Autonomous.EffectiveVerifierHeadroomMaxAge() / time.Hour),
			"security_scan":                   s.cfg.Autonomous.SecurityScan,
			"per_story_approval":              s.cfg.Autonomous.PerStoryApproval, // Phase 3 (v5.26.61)
			"continue_on_story_failure":       s.cfg.Autonomous.ContinueOnStoryFailure,
			"scope_lint":                      s.cfg.Autonomous.ScopeLintMode(), // off, warn or enforce
			"planning_routing":                s.cfg.Autonomous.PlanningRouting,
			"task_routing":                    s.cfg.Autonomous.TaskRouting,
			// BL367 — default quality gate config (GET surface; PUT cases in applyConfigPatch).
			"default_quality_gates": map[string]interface{}{
				"enabled":             s.cfg.Autonomous.DefaultQualityGates.Enabled,
				"test_command":        s.cfg.Autonomous.DefaultQualityGates.TestCommand,
				"timeout":             s.cfg.Autonomous.DefaultQualityGates.Timeout,
				"block_on_regression": s.cfg.Autonomous.DefaultQualityGates.BlockOnRegression,
				"scope":               s.cfg.Autonomous.DefaultQualityGates.Scope,
				"smoke_packages":      s.cfg.Autonomous.DefaultQualityGates.SmokePackages,
			},
			// BL369 — prompt injection guard.
			"injection_guard":    s.cfg.Autonomous.InjectionGuard,
			"block_on_injection": s.cfg.Autonomous.BlockOnInjection,
			// BL406 — project-rules enforcement config. "scan" is
			// read-only here (richer write path is the dedicated
			// GET/PUT /api/autonomous/scan/config, same split as
			// default_quality_gates above being GET-visible with its
			// own PUT cases).
			"rules_file":     s.cfg.Autonomous.RulesFile,
			"context_file":   s.cfg.Autonomous.ContextFile,
			"upstream_repos": s.cfg.Autonomous.UpstreamRepos,
			// BL407 Phase 1 — local git-worktree isolation config.
			"worktree_base_repo": s.cfg.Autonomous.WorktreeBaseRepo,
			"worktree_dir":       s.cfg.Autonomous.WorktreeDir,
			// BL407 Phase 5 — daemon-wide default for a new PRD's Git.AutoPR.
			"default_auto_pr": s.cfg.Autonomous.DefaultAutoPR,
			// Default event kinds the built-in lifecycle sinks subscribe to.
			// Always emitted (even when empty) so the PWA settings card
			// renders the field; the zero (nil) value is emitted as `[]`
			// rather than null. The field itself has no omitempty for the
			// same reason (see config.AutonomousNotifyConfig).
			"notify": defaultEventsNotifySurface(s.cfg.Autonomous.Notify),
			// Operator-question switch and its notify toggles. Always emitted
			// (even when false) so the PWA settings card renders the fields;
			// the fields have no omitempty for the same reason (see
			// config.AutonomousQuestionsConfig). Surface keys:
			// autonomous.questions.enabled, .notify_messaging, .notify_alert,
			// .return_path (the toggles report their effective value, which
			// defaults on when the key is absent; an explicit false shows
			// false).
			"questions": map[string]interface{}{
				"enabled":          s.cfg.Autonomous.Questions.Enabled,
				"notify_messaging": s.cfg.Autonomous.Questions.EffectiveNotifyMessaging(),
				"notify_alert":     s.cfg.Autonomous.Questions.EffectiveNotifyAlert(),
				"return_path":      s.cfg.Autonomous.Questions.EffectiveReturnPath(),
				// Decision gate: effective values (on, and shadow, when absent).
				"decision_gate":      s.cfg.Autonomous.Questions.EffectiveDecisionGate(),
				"decision_gate_mode": s.cfg.Autonomous.Questions.EffectiveDecisionGateMode(),
				// Held question: effective values — held_mode reads shadow
				// when the key is absent; held_states reads the five held
				// state ids when the list is absent, and stays an explicit
				// empty list (none) when one was set (no omitempty on the
				// field, so the clear survives the round trip).
				"held_mode":   s.cfg.Autonomous.Questions.EffectiveHeldMode(),
				"held_states": s.cfg.Autonomous.Questions.EffectiveHeldStates(),
				// Worker question marker: effective value (off when absent).
				"worker_marker": s.cfg.Autonomous.Questions.EffectiveWorkerMarker(),
				// Question quality check and queue row preview: effective values
				// (warn, 60, the default pattern and 3 when absent).
				"quality_mode":               s.cfg.Autonomous.Questions.EffectiveQualityMode(),
				"quality_min_text_chars":     s.cfg.Autonomous.Questions.EffectiveQualityMinTextChars(),
				"quality_tracker_id_pattern": s.cfg.Autonomous.Questions.EffectiveQualityTrackerIDPattern(),
				"queue_preview_lines":        s.cfg.Autonomous.Questions.EffectiveQueuePreviewLines(),
			},
			"parallel": parallelSurface(s.cfg.Autonomous.Parallel),
			// Task template modes: the stored {template id: mode} map (nil
			// renders as {}, not null); an unset id reads its shipped default
			// (llm) via EffectiveTaskTemplateMode.
			"task_templates": map[string]interface{}{
				"mode": taskTemplateModeSurface(s.cfg.Autonomous.TaskTemplates.Mode),
			},
			// Script gate: raw stored values; an unset timeout reads the 900-
			// second default and an unset command stays empty, both via the
			// Effective* accessors.
			"script_gate_timeout_seconds": s.cfg.Autonomous.ScriptGateTimeoutSeconds,
			"gate_drain_minutes":          config.NormalizeGateDrainMinutes(s.cfg.Autonomous.GateDrainMinutes),
			"project_gate_command":        s.cfg.Autonomous.ProjectGateCommand,
			// Decision record keys: effective values (the shipped defaults when a
			// key is absent).
			"decisions":    decisionRecordSurface(s.cfg.Autonomous.Decisions),
			"plan_records": planRecordsSurface(s.cfg.Autonomous.PlanRecords),
			"automation": map[string]interface{}{
				"idle_alert_minutes": s.cfg.Autonomous.Automation.IdleAlertMinutes,
				// Effective, not raw: an unset (nil) value reads the 30-minute
				// default, an explicit 0 stays 0 (the triage alert off).
				"triage_alert_minutes": s.cfg.Autonomous.Automation.EffectiveTriageAlertMinutes(),
				// Merge-pending check: the mode reads off when unset, the alert
				// minutes read the 30-minute default when unset and an explicit
				// 0 stays 0 (the merge-pending alert off).
				"child_inherit":               s.cfg.Autonomous.Automation.EffectiveChildInherit(),
				"merge_pending_mode":          s.cfg.Autonomous.Automation.EffectiveMergePendingMode(),
				"merge_pending_alert_minutes": s.cfg.Autonomous.Automation.EffectiveMergePendingAlertMinutes(),
				// Effective, not raw: an empty value reads the "completed"
				// default via the accessor.
				"merged_member_policy": s.cfg.Autonomous.Automation.EffectiveMergedMemberPolicy(),
				"agent_insert_policy":  s.cfg.Autonomous.Automation.EffectiveAgentInsertPolicy(),
				// Effective values: unset fields read their defaults in one place
				// (Effective* accessors), so GET never emits a zero that reads
				// a different default.
				"repo_ci_watch": repoCIWatchSurface(s.cfg.Autonomous.Automation.RepoCIWatch),
				// Effective, not raw: unset enabled reads true, an empty schedule
				// reads the default, and unset max_majors reads its default.
				"dependency_update": dependencyUpdateSurface(s.cfg.Autonomous.Automation.DependencyUpdate),
				// Effective, not raw: an unset or empty mode reads shadow (the
				// first-release default) via the Effective* accessor.
				"security_failure": securityFailureSurface(s.cfg.Autonomous.Automation.SecurityFailure),
				// Effective, not raw: an unset or empty mode reads shadow (the
				// release default — reports only, never acts) and the unset
				// minute thresholds read their one defaults, all via the
				// Effective* accessors.
				"supervisor_watch": supervisorWatchSurface(s.cfg.Autonomous.Automation.SupervisorWatch),
				// Effective, not raw: an unset value reads the "shadow" default,
				// an explicit off/on stays.
				"create_search_mode": s.cfg.Autonomous.Automation.EffectiveCreateSearchMode(),
				// Effective, not raw: an unset (nil) value reads the 0.5 default.
				"create_search_threshold": s.cfg.Autonomous.Automation.EffectiveCreateSearchThreshold(),
				// Effective, not raw: an unset key or an unrecognized value
				// reads report; zero/negative values read the 24-hour and 10
				// defaults; the alert percent reads 85 when unset and is
				// clamped to 1..100 otherwise.
				"cleanup_mode":                  s.cfg.Autonomous.Automation.EffectiveCleanupMode(),
				"cleanup_retention_hours":       s.cfg.Autonomous.Automation.EffectiveCleanupRetentionHours(),
				"cleanup_max_removals_per_pass": s.cfg.Autonomous.Automation.EffectiveCleanupMaxRemovalsPerPass(),
				"disk_alert_percent":            s.cfg.Autonomous.Automation.EffectiveDiskAlertPercent(),
				// Effective, not raw: an unset key reads hold and task_files.
				"uncommitted_work_policy": s.cfg.Autonomous.Automation.EffectiveUncommittedWorkPolicy(),
				"uncommitted_work_scope":  s.cfg.Autonomous.Automation.EffectiveUncommittedWorkScope(),
				// Effective, not raw: an unset key reads the default list, hold
				// and base_commit.
				"protected_paths":        protectedPathsEffectiveList(s.cfg),
				"protected_paths_action": s.cfg.Autonomous.Automation.EffectiveProtectedPathsAction(),
				"rules_review_source":    s.cfg.Autonomous.Automation.EffectiveRulesReviewSource(),
				// Effective, not raw: an unset policy reads hold, an unset
				// auto-resume reads true, and the file registry block shows
				// every default in effect (no path means no file registry).
				"pending_item_policy":      s.cfg.Autonomous.Automation.EffectivePendingItemPolicy(),
				"pending_item_auto_resume": s.cfg.Autonomous.Automation.EffectivePendingItemAutoResume(),
				"pending_item_source":      pendingItemSourceSurface(s.cfg.Autonomous.Automation.PendingItemSource),
			},
			"lane_fill": laneFillConfigSurface(s.cfg.Autonomous.LaneFill),
			// Effective, not raw: an unset enabled switch reads on, the unset
			// gate timeout reads 20 minutes and the unset plan keep reads 200.
			"merge_shadow": mergeShadowConfigSurface(s.cfg.Autonomous.MergeShadow),
			// Effective, not raw: every supervisor_loop key with its default
			// filled in, keyed without the supervisor_loop. prefix.
			"supervisor_loop": supervisorLoopConfigSurface(s.cfg.Autonomous.SupervisorLoop),
			"infra_retry":     infraRetryConfigSurface(s.cfg.Autonomous.InfraRetry),
			// Effective, not raw: the merge pipeline values with defaults filled in.
			"merge_pipeline": mergePipelineConfigSurface(s.cfg.Autonomous.MergePipeline),
			// Effective, not raw: all eleven request watch values with the
			// defaults filled in; remediation_options is the list in effect.
			"request_watch": requestWatchConfigSurface(s.cfg.Autonomous.RequestWatch),
			// Effective, not raw: an unset key shows its default.
			"model_routing": modelRoutingConfigSurface(s.cfg.Autonomous.ModelRouting),
			// Model-pull consent: the per-model allow flags plus the publisher
			// allow-list. Rendered whole (nil lists become empty arrays, not
			// null) so an unset consent visibly allows nothing — the
			// fail-closed default, not a missing setting.
			"model_pull_consent": modelPullConsentForDisplay(s.cfg.Autonomous.ModelPullConsent),
			// Requirements min-Ollama overrides: the stored per-capability
			// minimums (empty = the in-code table applies) plus the known
			// capability list, so the web settings, CLI and comm render the
			// same fields in the same order.
			"requirements": requirementsConfigGetSurface(s.cfg.Autonomous.Requirements),
			"progress": map[string]interface{}{
				"lanes":                   s.cfg.Autonomous.Progress.Lanes,
				"inactive_gap_hours":      s.cfg.Autonomous.Progress.InactiveGapHours,
				"backlog_path":            s.cfg.Autonomous.Progress.EffectiveBacklogPath(),
				"since":                   s.cfg.Autonomous.Progress.Since,
				"snapshot_retention_days": s.cfg.Autonomous.Progress.SnapshotRetentionDays,
			},
			"scan": map[string]interface{}{
				"enabled":              s.cfg.Autonomous.Scan.Enabled,
				"sast_enabled":         s.cfg.Autonomous.Scan.SASTEnabled,
				"secrets_enabled":      s.cfg.Autonomous.Scan.SecretsEnabled,
				"deps_enabled":         s.cfg.Autonomous.Scan.DepsEnabled,
				"fail_on_severity":     s.cfg.Autonomous.Scan.FailOnSeverity,
				"max_findings":         s.cfg.Autonomous.Scan.MaxFindings,
				"rules_grader_enabled": s.cfg.Autonomous.Scan.RulesGraderEnabled,
				"fix_loop_enabled":     s.cfg.Autonomous.Scan.FixLoopEnabled,
				"fix_loop_max_retries": s.cfg.Autonomous.Scan.FixLoopMaxRetries,
				"project_rules":        s.cfg.Autonomous.Scan.ProjectRules,
			},
		},
		"plugins": map[string]interface{}{
			"enabled":    s.cfg.Plugins.Enabled,
			"dir":        s.cfg.Plugins.Dir,
			"timeout_ms": s.cfg.Plugins.TimeoutMs,
			"disabled":   s.cfg.Plugins.Disabled,
			// plugins.services — service-plugin watch settings; each field is
			// its stored value (0 = unset; the Effective* accessors apply the
			// documented defaults).
			"services": pluginServicesSurface(s.cfg),
		},
		"orchestrator": map[string]interface{}{
			"enabled":              s.cfg.Orchestrator.Enabled,
			"default_guardrails":   s.cfg.Orchestrator.DefaultGuardrails,
			"guardrail_timeout_ms": s.cfg.Orchestrator.GuardrailTimeoutMs,
			"guardrail_backend":    s.cfg.Orchestrator.GuardrailBackend,
			"max_parallel_prds":    s.cfg.Orchestrator.MaxParallelPRDs,
		},
		"profiles": s.cfg.Profiles,
		"fallback_chain": func() []string {
			if s.inferenceReg != nil {
				if cl, err := s.inferenceReg.Get("claude-code"); err == nil && cl != nil {
					return cl.FallbackChain
				}
			}
			return nil
		}(),
		"whisper": map[string]interface{}{
			"enabled": s.cfg.Whisper.Enabled,
			// v7.0.0-alpha.20 #253 — backend was missing here, so the
			// PWA dropdown silently defaulted to "whisper" while the
			// daemon was honoring the YAML value (e.g. "ollama" → BL201
			// inheritance silently routed through OpenWebUI's audio API).
			// Operator-flagged after Chrome PWA audit.
			"backend":       s.cfg.Whisper.Backend,
			"model":         s.cfg.Whisper.Model,
			"language":      s.cfg.Whisper.Language,
			"venv_path":     s.cfg.Whisper.VenvPath,
			"chunk_seconds": s.cfg.Whisper.ChunkSeconds,
		},
		"vision": map[string]interface{}{
			"enabled":         s.cfg.Vision.Enabled,
			"backend":         s.cfg.Vision.Backend,
			"endpoint":        s.cfg.Vision.Endpoint,
			"api_key":         mask(s.cfg.Vision.APIKey),
			"model":           s.cfg.Vision.Model,
			"default_prompt":  s.cfg.Vision.DefaultPrompt,
			"max_image_bytes": s.cfg.Vision.MaxImageBytes,
			// Effective, not raw: an unset (0 or negative) stored value shows
			// the default (5s), never 0.
			"describe_timeout_seconds": int(s.cfg.Vision.EffectiveDescribeTimeout().Seconds()),
		},
		"memory": map[string]interface{}{
			"enabled":            s.cfg.Memory.Enabled,
			"backend":            s.cfg.Memory.Backend,
			"db_path":            s.cfg.Memory.DBPath,
			"postgres_url":       mask(s.cfg.Memory.PostgresURL),
			"fallback_sqlite":    s.cfg.Memory.FallbackSQLite,
			"embedder":           s.cfg.Memory.Embedder,
			"embedder_model":     s.cfg.Memory.EmbedderModel,
			"embedder_host":      s.cfg.Memory.EmbedderHost,
			"openai_key":         mask(s.cfg.Memory.OpenAIKey),
			"dimensions":         s.cfg.Memory.Dimensions,
			"top_k":              s.cfg.Memory.TopK,
			"auto_save":          s.cfg.Memory.IsAutoSave(),
			"learnings_enabled":  s.cfg.Memory.IsLearningsEnabled(),
			"retention_days":     s.cfg.Memory.RetentionDays,
			"storage_mode":       s.cfg.Memory.StorageMode,
			"entity_detection":   s.cfg.Memory.EntityDetection,
			"auto_hooks":         s.cfg.Memory.IsAutoHooks(),
			"hook_save_interval": s.cfg.Memory.EffectiveHookInterval(),
			"session_awareness":  s.cfg.Memory.IsSessionAwareness(),
			"session_broadcast":  s.cfg.Memory.IsSessionBroadcast(),
		},
		// Platform context: only the toggle and the operator note are exposed.
		// The platform context text itself is never returned for editing.
		"platform_context": map[string]interface{}{
			"enabled": s.cfg.PlatformContext.IsEnabled(),
			"note":    s.cfg.PlatformContext.Note,
		},
		"cross_session": map[string]interface{}{
			"enabled": s.cfg.CrossSession.IsEnabled(),
		},
		"secrets": map[string]interface{}{
			"vault": map[string]interface{}{
				"unreachable_alert_after_seconds": s.cfg.Secrets.Vault.EffectiveUnreachableAlertAfterSeconds(),
			},
		},
		"proxy": map[string]interface{}{
			"enabled":                   s.cfg.Proxy.Enabled,
			"health_interval":           s.cfg.Proxy.HealthInterval,
			"request_timeout":           s.cfg.Proxy.RequestTimeout,
			"offline_queue_size":        s.cfg.Proxy.OfflineQueueSize,
			"circuit_breaker_threshold": s.cfg.Proxy.CircuitBreakerThreshold,
			"circuit_breaker_reset":     s.cfg.Proxy.CircuitBreakerReset,
		},
		// F10 sprint 3: agent layer configuration.
		"agents": map[string]interface{}{
			"image_prefix":                      s.cfg.Agents.ImagePrefix,
			"image_tag":                         s.cfg.Agents.ImageTag,
			"docker_bin":                        s.cfg.Agents.DockerBin,
			"kubectl_bin":                       s.cfg.Agents.KubectlBin,
			"callback_url":                      s.cfg.Agents.CallbackURL,
			"bootstrap_token_ttl_seconds":       s.cfg.Agents.BootstrapTokenTTLSeconds,
			"worker_bootstrap_deadline_seconds": s.cfg.Agents.WorkerBootstrapDeadlineSeconds,
		},
		// BL297 v6.22.4 — Council subsystem configuration.
		"council": map[string]interface{}{
			"draft_retention_days": s.cfg.Council.DraftRetentionDays,
			"llm_ref":              s.cfg.Council.LLMRef,
			"max_parallel":         s.cfg.Council.MaxParallel,
			"comm_firehose":        s.cfg.Council.CommFirehose,
		},
		// scheduled-jobs results: per-schedule result history store.
		"results": map[string]interface{}{
			"schedule_history_limit": s.cfg.Results.EffectiveScheduleHistoryLimit(),
		},
	}
	// Model-to-node collisions: two different models configured on one
	// ollama node. Omitted when there are none, so the PWA shows the banner
	// only when it has something to say.
	if warnings := s.modelNodeWarnings(); len(warnings) > 0 {
		out["model_node_warnings"] = warnings
	}
	out["llms"] = s.llmBillingModes()
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
	if code, verr := s.firstLLMBillingModeError(patch); verr != nil {
		http.Error(w, verr.Error(), code)
		return
	}
	// Tag browser settings (autonomous.tags.*): every key goes through the
	// shared validator before anything is applied, so a bad value rejects the
	// whole PUT and the comm, MCP and CLI surfaces show the same message.
	if verr := firstTagSettingError(patch); verr != nil {
		http.Error(w, verr.Error(), http.StatusBadRequest)
		return
	}
	// Defect queue settings (autonomous.defect_queue.*): same shared validator.
	if verr := firstDefectQueueSettingError(patch); verr != nil {
		http.Error(w, verr.Error(), http.StatusBadRequest)
		return
	}
	// Project defaults (projects_defaults.*): refused before anything is applied.
	if verr := firstProjectsDefaultsSettingError(patch); verr != nil {
		http.Error(w, verr.Error(), http.StatusBadRequest)
		return
	}
	// Auto tagging (autonomous.auto_tags.*): every key goes through the
	// shared validator before anything is applied, so a bad value rejects the
	// whole PUT and the comm, MCP and CLI surfaces show the same message.
	if verr := firstAutoTagsSettingError(patch); verr != nil {
		http.Error(w, verr.Error(), http.StatusBadRequest)
		return
	}
	// Housekeeping repairs (autonomous.housekeeping_repairs.*): same shared
	// validator, so a bad mode rejects the whole PUT with one message on every
	// surface and nothing is written.
	if verr := firstHousekeepingRepairsSettingError(patch); verr != nil {
		http.Error(w, verr.Error(), http.StatusBadRequest)
		return
	}
	// v9.5.5 — validate task_override_statuses before anything is applied,
	// so a bad name rejects the whole PUT instead of storing a partial one.
	// Accepts the array form and the comma-separated string form the CLI
	// and comm channel send, mirroring applyConfigPatch.
	if v, ok := patch["autonomous.task_override_statuses"]; ok {
		var candidate []string
		if arr, ok := toStringArray(v); ok {
			candidate = arr
		} else if str, ok := v.(string); ok {
			candidate = splitCSV(str)
		}
		if verr := autonomous.ValidateTaskOverrideStatuses(candidate); verr != nil {
			http.Error(w, "autonomous."+verr.Error(), http.StatusBadRequest)
			return
		}
	}
	// override_queued_tasks — a boolean (or a parseable boolean string from
	// the comm channel); anything else rejects the whole PUT so a typo
	// never stores a value that later resolves to the wrong behavior.
	if v, ok := patch["autonomous.override_queued_tasks"]; ok {
		valid := false
		if _, ok := v.(bool); ok {
			valid = true
		} else if str, ok := v.(string); ok {
			if _, perr := strconv.ParseBool(strings.TrimSpace(str)); perr == nil {
				valid = true
			}
		}
		if !valid {
			http.Error(w, "autonomous.override_queued_tasks: must be a boolean (true or false)", http.StatusBadRequest)
			return
		}
	}
	// scope_lint — refuse an unknown mode before anything is applied, so a
	// typo rejects the whole PUT instead of storing a value that quietly
	// resolves to enforce.
	if v, ok := patch["autonomous.scope_lint"]; ok {
		mode, _ := v.(string)
		if verr := config.ValidateScopeLint(mode); verr != nil {
			http.Error(w, "autonomous."+verr.Error(), http.StatusBadRequest)
			return
		}
	}
	// verifier_max_run_seconds — a whole, non-negative JSON number (0 = no
	// cap). A negative or fractional value rejects the whole PUT so the
	// stored cap is never silently truncated or ignored.
	if v, ok := patch["autonomous.verifier_max_run_seconds"]; ok {
		f, isNum := v.(float64)
		if !isNum || f != math.Trunc(f) {
			http.Error(w, "autonomous.verifier_max_run_seconds: must be a whole number of seconds (0 = no cap)", http.StatusBadRequest)
			return
		}
		if verr := config.ValidateVerifierRunCapSeconds(int(f)); verr != nil {
			http.Error(w, verr.Error(), http.StatusBadRequest)
			return
		}
	}
	// notify default events: the operator pre-selects which lifecycle
	// kinds the built-in sinks subscribe to. The new opt-in warning
	// kinds (scope_warning, guardrail_warn) and the task session
	// lifecycle kinds are valid choices; an unknown kind (or an
	// empty entry) rejects the whole PUT instead of silently storing a
	// list the sinks will never match — same rule as
	// task_override_statuses above. An empty list is valid: it clears
	// the pre-selection so the global default set applies.
	if v, ok := patch["autonomous.notify.default_events"]; ok {
		var candidate []string
		if arr, ok := toStringArray(v); ok {
			candidate = arr
		} else if str, ok := v.(string); ok {
			candidate = splitCSV(str)
		} else {
			http.Error(w, "autonomous.notify.default_events must be an array of event kinds or a comma-separated list", http.StatusBadRequest)
			return
		}
		if verr := autonomous.ValidateNotifyEvents(candidate); verr != nil {
			http.Error(w, verr.Error(), http.StatusBadRequest)
			return
		}
	}
	// Plan requirement coverage gate — validate both knobs before
	// anything is applied, so a bad mode or ratio rejects the whole
	// PUT instead of storing a half-set or malformed pair (same rule
	// as the task_override_statuses block above).
	if v, ok := patch["autonomous.plan_coverage_check"]; ok {
		if _, verr := config.NormalizePlanCoverageCheck(fmt.Sprintf("%v", v)); verr != nil {
			http.Error(w, "autonomous."+verr.Error(), http.StatusBadRequest)
			return
		}
	}
	if v, ok := patch["autonomous.plan_coverage_min_ratio"]; ok {
		if _, verr := configPatchFloat(v); verr != nil {
			http.Error(w, "autonomous."+verr.Error(), http.StatusBadRequest)
			return
		}
	}
	// Plan-coverage finding filters (plan_coverage) — same whole-PUT
	// rejection rule as the two plan-coverage keys above: a bad type
	// refuses the whole PUT instead of storing a half-applied set.
	for _, k := range []string{
		"autonomous.plan_coverage_suppress_created_files",
		"autonomous.plan_coverage_scoped_requirements",
	} {
		if v, ok := patch[k]; ok {
			if _, isBool := v.(bool); !isBool {
				http.Error(w, k+": must be a boolean (true or false)", http.StatusBadRequest)
				return
			}
		}
	}
	if v, ok := patch["autonomous.plan_coverage_requirement_sections"]; ok {
		if _, isStr := v.(string); !isStr {
			http.Error(w, "autonomous.plan_coverage_requirement_sections: must be a comma-separated string of spec section names (empty = the scope,constraints default)", http.StatusBadRequest)
			return
		}
	}
	if v, ok := patch["autonomous.plan_coverage_min_requirement_chars"]; ok {
		f, err := configPatchFloat(v)
		if err != nil || f != math.Trunc(f) || f < 0 {
			http.Error(w, "autonomous.plan_coverage_min_requirement_chars: must be a whole number >= 0 (0 = the 30-character default)", http.StatusBadRequest)
			return
		}
	}
	// autonomous.generated_artefacts.<kind>: boolean switches only.
	for _, k := range generatedArtefactConfigKeys() {
		if v, ok := patch[k]; ok {
			if _, isBool := v.(bool); !isBool {
				http.Error(w, k+": must be a boolean (true or false)", http.StatusBadRequest)
				return
			}
		}
	}
	// mcp.public_url is advertised verbatim to MCP SSE clients, so a value
	// that is not an absolute http(s) URL is rejected before anything is
	// applied and the config is left unchanged.
	if v, ok := patch["mcp.public_url"]; ok {
		if _, verr := config.NormalizePublicURL(toString(v)); verr != nil {
			http.Error(w, verr.Error(), http.StatusBadRequest)
			return
		}
	}
	// platform_context.note is appended to every session's context text, so an
	// over-long value is rejected before anything is applied.
	if v, ok := patch["platform_context.note"]; ok {
		if n := len(toString(v)); n > config.PlatformContextNoteMaxLen {
			http.Error(w, fmt.Sprintf("platform_context.note: too long (%d bytes, max %d)", n, config.PlatformContextNoteMaxLen), http.StatusBadRequest)
			return
		}
	}
	// Usage back-off knobs: an unrecognized mode would otherwise normalize to
	// "off" and silently disable the automatic back-off, and a non-whole or
	// sub-1 maximum would be clamped; reject the whole PUT instead.
	if v, ok := patch["autonomous.usage_backoff"]; ok {
		if verr := validateUsageBackoffMode(v); verr != nil {
			http.Error(w, "autonomous."+verr.Error(), http.StatusBadRequest)
			return
		}
	}
	if v, ok := patch["autonomous.backoff_max_wait_minutes"]; ok {
		if _, verr := parseBackoffMaxWaitMinutes(v); verr != nil {
			http.Error(w, "autonomous."+verr.Error(), http.StatusBadRequest)
			return
		}
	}
	// Usage calculator keys: any invalid value (unknown variant or policy,
	// out-of-range number, bad clock, unknown or fixed-offset zone) rejects
	// the whole PUT.
	if verr := validateUsageCalculatorPatch(patch); verr != nil {
		http.Error(w, verr.Error(), http.StatusBadRequest)
		return
	}
	// Classifier keys (classifier.*): any invalid value rejects the whole PUT.
	if verr := validateClassifierPatch(patch); verr != nil {
		http.Error(w, verr.Error(), http.StatusBadRequest)
		return
	}
	// Burn guard keys (usage.burn_guard.*): any invalid value rejects the whole PUT.
	if verr := validateBurnGuardPatch(patch); verr != nil {
		http.Error(w, verr.Error(), http.StatusBadRequest)
		return
	}
	// Billing profiles (usage.billing.<provider>.<field>): any invalid value
	// (unknown mode or allowance unit, a negative or non-finite monthly
	// price, a bad reset weekday or time, an unknown field) rejects the
	// whole PUT, the same way every other usage.* key does.
	if verr := validateBillingPatch(patch); verr != nil {
		http.Error(w, verr.Error(), http.StatusBadRequest)
		return
	}
	// Verifier policy: an unknown mode, an empty backend entry or a negative
	// cap rejects the whole PUT.
	if verr := validateVerifierPolicyPatch(patch); verr != nil {
		http.Error(w, "autonomous."+verr.Error(), http.StatusBadRequest)
		return
	}
	// Outbound webhook sink and peer forwarding (autonomous.notify.webhook.*,
	// autonomous.notify.forwarding.*). A bad host, a non-numeric number or a
	// bad delay schedule rejects the whole PUT before anything is applied.
	if verr := validateLifecycleNotifyPatch(patch); verr != nil {
		http.Error(w, verr.Error(), http.StatusBadRequest)
		return
	}
	// Automation idle-alert threshold: whole minutes, 0 = off. A negative or
	// non-integer value rejects the whole PUT rather than being skipped.
	if v, ok := patch["autonomous.automation.idle_alert_minutes"]; ok {
		if verr := validateIdleAlertMinutes(v); verr != nil {
			http.Error(w, verr.Error(), http.StatusBadRequest)
			return
		}
	}
	// Vault-unreachable alert threshold: whole seconds >= 0 (0 = off). A
	// negative, fractional or non-numeric value rejects the whole PUT.
	if v, ok := patch["secrets.vault.unreachable_alert_after_seconds"]; ok {
		if verr := validateVaultUnreachableAlertAfterSeconds(v); verr != nil {
			http.Error(w, verr.Error(), http.StatusBadRequest)
			return
		}
	}
	// Automation triage-alert threshold: whole minutes >= 0. Unset reads the
	// 30-minute default; an explicit 0 turns the triage alert off and is
	// preserved. A negative, fractional or non-numeric value rejects the
	// whole PUT rather than being skipped.
	if v, ok := patch["autonomous.automation.triage_alert_minutes"]; ok {
		if verr := validateTriageAlertMinutes(v); verr != nil {
			http.Error(w, verr.Error(), http.StatusBadRequest)
			return
		}
	}
	// Host load alert: mode off | alert | reap, multiple >= 0, whole minutes
	// >= 0. Any bad value rejects the whole PUT.
	for _, hk := range hostLoadAlertKeys {
		if v, ok := patch[hk]; ok {
			if verr := validateHostLoadAlertPatchKey(hk, v); verr != nil {
				http.Error(w, verr.Error(), http.StatusBadRequest)
				return
			}
		}
	}
	// Child inherit: a boolean. Any other type rejects the whole PUT.
	if v, ok := patch["autonomous.automation.child_inherit"]; ok {
		if _, isBool := v.(bool); !isBool {
			http.Error(w, "autonomous.automation.child_inherit: must be true or false", http.StatusBadRequest)
			return
		}
	}
	// Merge-pending mode: off | shadow | on. Any other value rejects the whole
	// PUT. The alert threshold is whole minutes >= 0; unset reads the 30-minute
	// default and an explicit 0 turns the merge-pending alert off.
	if v, ok := patch["autonomous.automation.merge_pending_mode"]; ok {
		if verr := validateMergePendingMode(v); verr != nil {
			http.Error(w, verr.Error(), http.StatusBadRequest)
			return
		}
	}
	if v, ok := patch["autonomous.automation.merge_pending_alert_minutes"]; ok {
		if verr := validateMergePendingAlertMinutes(v); verr != nil {
			http.Error(w, verr.Error(), http.StatusBadRequest)
			return
		}
	}
	// Merged-member policy: completed | manual | block. Any other value
	// rejects the whole PUT, same as merge_pending_mode.
	if v, ok := patch["autonomous.automation.merged_member_policy"]; ok {
		if verr := validateMergedMemberPolicy(v); verr != nil {
			http.Error(w, verr.Error(), http.StatusBadRequest)
			return
		}
	}
	// Agent insert policy: allow | require_operator. Any other value rejects
	// the whole PUT, same as idle_alert_minutes.
	if v, ok := patch["autonomous.automation.agent_insert_policy"]; ok {
		if verr := validateAgentInsertPolicy(v); verr != nil {
			http.Error(w, verr.Error(), http.StatusBadRequest)
			return
		}
	}
	// Repo CI watch (autonomous.automation.repo_ci_watch.*): every key is
	// checked before anything is applied, so one bad value (a mode other
	// than off|shadow|on, a delete_superseded other than report|delete, a
	// non-absolute repository path, a poll interval below 60, a negative
	// alert/tag threshold, a hung threshold below 1) rejects the whole
	// PUT rather than being skipped.
	if verr := validateRepoCIWatchPatch(patch); verr != nil {
		http.Error(w, verr.Error(), http.StatusBadRequest)
		return
	}
	// Dependency update (autonomous.automation.dependency_update.*): the
	// merged block is run through the config package's Validate, so an
	// invalid schedule or max_majors rejects the whole PUT with the
	// allowed form.
	if verr := validateDependencyUpdatePatch(s.cfg.Autonomous.Automation.DependencyUpdate, patch); verr != nil {
		http.Error(w, verr.Error(), http.StatusBadRequest)
		return
	}
	// Search-before-create mode: off | shadow | on. Any other value rejects
	// the whole PUT, same as agent_insert_policy.
	if v, ok := patch["autonomous.automation.create_search_mode"]; ok {
		if verr := validateCreateSearchModeValue(v); verr != nil {
			http.Error(w, verr.Error(), http.StatusBadRequest)
			return
		}
	}
	// Search-before-create threshold: finite number in (0, 1]. Zero, a
	// negative, an above-1, a non-finite or a non-numeric value rejects the
	// whole PUT rather than being skipped.
	if v, ok := patch["autonomous.automation.create_search_threshold"]; ok {
		if verr := validateCreateSearchThresholdValue(v); verr != nil {
			http.Error(w, verr.Error(), http.StatusBadRequest)
			return
		}
	}
	// Model-pull consent (autonomous.model_pull_consent): the whole block is
	// checked before anything is applied — an entry the gate could not
	// interpret (empty model name, duplicated model, empty publisher) or a
	// wrong shape rejects the whole PUT, same rule as the
	// PUT /api/autonomous/config surface.
	if v, ok := patch["autonomous.model_pull_consent"]; ok {
		var mpc config.ModelPullConsent
		if b, err := json.Marshal(v); err == nil {
			if json.Unmarshal(b, &mpc) != nil {
				http.Error(w, `autonomous.model_pull_consent: must be an object with "models" and/or "publishers"`, http.StatusBadRequest)
				return
			}
		} else {
			http.Error(w, `autonomous.model_pull_consent: must be an object with "models" and/or "publishers"`, http.StatusBadRequest)
			return
		}
		if verr := config.ValidateModelPullConsent(mpc); verr != nil {
			http.Error(w, "autonomous.model_pull_consent: "+verr.Error(), http.StatusBadRequest)
			return
		}
	}
	// Cleanup keys (autonomous.automation.cleanup_mode,
	// cleanup_retention_hours, cleanup_max_removals_per_pass,
	// disk_alert_percent): only the value type is checked here. An unknown
	// cleanup_mode word is legal — it reads as report through
	// EffectiveCleanupMode — and zero/negative hours and caps plus out-of-
	// range percents are not refusals either (they read their defaults or
	// clamp to 1..100), unlike the merge-pending and search-before-create
	// keys above.
	if v, ok := patch["autonomous.automation.cleanup_mode"]; ok {
		if verr := validateCleanupModeValue(v); verr != nil {
			http.Error(w, verr.Error(), http.StatusBadRequest)
			return
		}
	}
	for _, key := range []string{
		"autonomous.automation.cleanup_retention_hours",
		"autonomous.automation.cleanup_max_removals_per_pass",
		"autonomous.automation.disk_alert_percent",
	} {
		if v, ok := patch[key]; ok {
			if verr := validateCleanupWholeNumber(key, v); verr != nil {
				http.Error(w, verr.Error(), http.StatusBadRequest)
				return
			}
		}
	}
	// Uncommitted-work keys (autonomous.automation.uncommitted_work_policy and
	// uncommitted_work_scope): strict, unlike the cleanup mode above. A present
	// key must be hold|warn|off (policy) or task_files|worktree (scope), and
	// any other value rejects the whole PUT with a message naming the allowed
	// words, so a read back always shows the word that was written.
	for _, key := range []string{
		"autonomous.automation.uncommitted_work_policy",
		"autonomous.automation.uncommitted_work_scope",
	} {
		if v, ok := patch[key]; ok {
			if verr := validateUncommittedWorkValue(key, v); verr != nil {
				http.Error(w, verr.Error(), http.StatusBadRequest)
				return
			}
		}
	}
	// Protected-paths keys (autonomous.automation.protected_paths,
	// protected_paths_action and rules_review_source): strict. The action must
	// be hold|warn|block|off and the source base_commit|worktree, each refused
	// with a message naming the allowed words; a list entry that is absolute,
	// has a .. segment or is over 256 bytes, or a list over 100 entries, is
	// refused naming it. One bad key rejects the whole PUT.
	for _, key := range []string{protectedPathsActionKey, rulesReviewSourceKey} {
		if v, ok := patch[key]; ok {
			if verr := validateProtectedPathsWord(key, v); verr != nil {
				http.Error(w, verr.Error(), http.StatusBadRequest)
				return
			}
		}
	}
	if v, ok := patch[protectedPathsKey]; ok {
		if _, verr := parseProtectedPathsValue(v); verr != nil {
			http.Error(w, verr.Error(), http.StatusBadRequest)
			return
		}
	}
	// Pending item keys (autonomous.automation.pending_item_policy,
	// pending_item_auto_resume and pending_item_source.*): strict. A bad value
	// rejects the whole PUT with a message naming the allowed words.
	if verr := validatePendingItemPatch(patch); verr != nil {
		http.Error(w, verr.Error(), http.StatusBadRequest)
		return
	}
	// Security failure (autonomous.automation.security_failure.*): the only
	// key is mode, which must be off, shadow or on (empty = unset =
	// shadow). Any other value rejects the whole PUT rather than being
	// skipped.
	if verr := validateSecurityFailurePatch(patch); verr != nil {
		http.Error(w, verr.Error(), http.StatusBadRequest)
		return
	}
	// Supervisor watch (autonomous.automation.supervisor_watch.*): the mode
	// must be off, shadow or on (empty = unset = shadow), the minute
	// thresholds must be whole numbers >= 0, and the path/baseline keys
	// must be strings. Any other value rejects the whole PUT rather than
	// being skipped.
	if verr := validateSupervisorWatchPatch(patch); verr != nil {
		http.Error(w, verr.Error(), http.StatusBadRequest)
		return
	}
	// Parallel (autonomous.parallel.*): read_only_tasks must be a boolean.
	if verr := validateParallelPatch(patch); verr != nil {
		http.Error(w, verr.Error(), http.StatusBadRequest)
		return
	}
	// Lane fill: every autonomous.lane_fill.* value is checked before
	// anything is applied, so one bad value rejects the whole PUT.
	for k, v := range patch {
		if field, ok := strings.CutPrefix(k, laneFillKeyPrefix); ok {
			if verr := validateLaneFillValue(field, v); verr != nil {
				http.Error(w, verr.Error(), http.StatusBadRequest)
				return
			}
		}
	}
	// Merge shadow: every autonomous.merge_shadow.* value is checked before
	// anything is applied, so one bad value (or an unknown key) rejects the
	// whole PUT with an error naming the key.
	for k, v := range patch {
		if field, ok := strings.CutPrefix(k, mergeShadowKeyPrefix); ok {
			if verr := ValidateMergeShadowValue(field, v); verr != nil {
				http.Error(w, verr.Error(), http.StatusBadRequest)
				return
			}
		}
	}
	// Supervisor loop: every autonomous.supervisor_loop.* value is applied to a
	// copy of the block first, so one bad value or unknown key rejects the whole
	// PUT with an error naming the key.
	if verr := validateSupervisorLoopPatch(s.cfg.Autonomous.SupervisorLoop, patch); verr != nil {
		http.Error(w, verr.Error(), http.StatusBadRequest)
		return
	}
	// Archive grace: a whole number of minutes >= 0; anything else rejects
	// the whole PUT.
	if v, ok := patch["autonomous.archive_grace_minutes"]; ok {
		if _, verr := config.ParseArchiveGraceMinutes(v); verr != nil {
			http.Error(w, verr.Error(), http.StatusBadRequest)
			return
		}
	}
	// Progress engine parameters: every autonomous.progress.* value is checked
	// before anything is applied, so one bad value rejects the whole PUT.
	if verr := validateProgressPatch(patch); verr != nil {
		http.Error(w, verr.Error(), http.StatusBadRequest)
		return
	}
	// tier -> max-inflight table: validate the whole value before anything
	// is applied, so a bad tier key or negative limit rejects the PUT
	// instead of being stored (same rule as the plan-coverage pair above).
	if v, ok := patch["autonomous.plan_tier_max_inflight"]; ok {
		if _, verr := parsePlanTierTable(v); verr != nil {
			http.Error(w, "autonomous."+verr.Error(), http.StatusBadRequest)
			return
		}
	}
	// kind -> capacity-priority table: same whole-PUT rejection. Integer
	// values only (non-numeric, fractional and boolean values are refused
	// with a clear message) and non-empty kind keys; any kind name is
	// accepted — the shipped kinds plus an installation's own.
	if v, ok := patch["autonomous.priority_by_kind"]; ok {
		if _, verr := autonomous.ParsePriorityByKindValue(v); verr != nil {
			http.Error(w, "autonomous."+verr.Error(), http.StatusBadRequest)
			return
		}
	}
	// defect_intake_automation: an Automation name (or the empty string to
	// clear it); anything else is a client error rather than a stored junk
	// value.
	if v, ok := patch["autonomous.defect_intake_automation"]; ok {
		if _, okS := v.(string); !okS && v != nil {
			http.Error(w, "autonomous.defect_intake_automation: must be a string (the Automation name; the empty string clears it)", http.StatusBadRequest)
			return
		}
	}
	// tier -> notional allocation table: same whole-PUT rejection, for both
	// the whole-object form and the single autonomous.plan_tier_allocation_usd.<tier> pair.
	for k, v := range patch {
		if k == planTierAllocationKey {
			if _, verr := parsePlanTierAllocationTable(v); verr != nil {
				http.Error(w, "autonomous."+verr.Error(), http.StatusBadRequest)
				return
			}
		} else if tier, ok := planTierAllocationPairTier(k); ok {
			if _, verr := parsePlanTierAllocationPair(tier, v); verr != nil {
				http.Error(w, "autonomous."+verr.Error(), http.StatusBadRequest)
				return
			}
		}
	}
	// pin_check_seconds: whole seconds >= 0, 0 = pin keeper off. A negative
	// or non-integer value rejects the whole PUT, same as idle_alert_minutes.
	if v, ok := patch["pin_check_seconds"]; ok {
		if _, verr := parsePinCheckSeconds(v); verr != nil {
			http.Error(w, verr.Error(), http.StatusBadRequest)
			return
		}
	}
	// plan_review_mode must be a known mode (off/warn/block). A typo would
	// otherwise normalize to off and silently disable the gate, so reject
	// the whole PUT as a client error.
	if mv, ok := patch["autonomous.plan_review_mode"]; ok {
		mode := toString(mv)
		if !autonomous.ValidPlanReviewMode(mode) {
			http.Error(w, fmt.Sprintf("autonomous.plan_review_mode: %q is not a valid mode (expected off, warn or block)", mode), http.StatusBadRequest)
			return
		}
	}
	// drift_mode accepts only "block" or "warn"; reject the whole PUT
	// before anything is applied (same discipline as
	// task_override_statuses above).
	if v, ok := patch["autonomous.drift_mode"]; ok {
		if verr := config.ValidateDriftMode(toString(v)); verr != nil {
			http.Error(w, verr.Error(), http.StatusBadRequest)
			return
		}
	}
	// undeclared_edit_mode accepts only "off", "warn" or "block"; same
	// whole-PUT rejection as drift_mode above.
	if v, ok := patch["autonomous.undeclared_edit_mode"]; ok {
		if verr := config.ValidateUndeclaredEditMode(toString(v)); verr != nil {
			http.Error(w, verr.Error(), http.StatusBadRequest)
			return
		}
	}
	// gate_disagreement accepts only "warn" or "block"; the whole PUT is
	// rejected before anything is applied (the default is "warn", so an
	// explicit empty string is also invalid).
	if v, ok := patch["autonomous.gate_disagreement"]; ok {
		if str, ok := v.(string); !ok || (str != "warn" && str != "block") {
			http.Error(w, `autonomous.gate_disagreement: must be "warn" or "block"`, http.StatusBadRequest)
			return
		}
	}
	// default_quality_gates.scope accepts only full / touched-plus-smoke /
	// touched (the empty string is valid and means inherit the built-in
	// default); the whole PUT is rejected before anything is applied.
	if v, ok := patch["autonomous.default_quality_gates.scope"]; ok {
		if _, err := pipeline.ParseGateScope(toString(v)); err != nil {
			http.Error(w, `autonomous.default_quality_gates.scope: `+err.Error(), http.StatusBadRequest)
			return
		}
	}
	// server.allowed_project_roots is checked before anything is applied: a
	// relative path or a filesystem root refuses the whole PUT.
	if v, ok := patch["server.allowed_project_roots"]; ok {
		if _, err := parseAllowedProjectRoots(v); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	}
	// The failure escalation block is checked as a whole, the same way: a bad
	// max_failures, steps value or enabled flag refuses the whole PUT.
	if err := validateFailureEscalationPatch(patch); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	// The infrastructure retry block is checked as a whole too: one bad value
	// among its seven keys refuses the whole PUT.
	if err := validateInfraRetryPatch(patch); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	// The merge pipeline block is checked as a whole too: an unknown key, an
	// absolute or escaping file name, an unknown source, fallback or policy
	// refuses the whole PUT with a message naming the key. The check reads the
	// live block first, so a post_ship hook whose built-in target the block
	// already holds is accepted, as is a single PUT that sets the target and
	// the hook together.
	if err := validateMergePipelinePatchBase(s.cfg.Autonomous.MergePipeline, patch); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	// The request watch block is checked as a whole too: poll_seconds and
	// no_progress_minutes of at least 1, gpu_busy_percent from 1 to 100, only
	// known remediation option ids and real booleans. One bad value among its
	// eleven keys refuses the whole PUT with a message naming the key.
	if err := validateRequestWatchPatch(patch); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	// The model routing block is checked as a whole too: one bad value among
	// its six keys refuses the whole PUT. A whole-block object is first
	// expanded into the dotted keys.
	if err := expandModelRoutingBlock(patch); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := validateModelRoutingPatch(patch); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	// The compute placement block is checked before any key is applied:
	// a known missing_model, a memory headroom in 0-100 and the
	// rest non-negative whole numbers. One bad key rejects the
	// whole PUT; the error names the key.
	if err := validateComputePlacementPatch(patch); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	// judge_size_threshold_lines is a whole number of 0 or more; the whole PUT
	// is refused before anything is applied.
	if err := validateJudgeSizeThresholdPatch(patch); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	// The two judge prompt delivery keys are whole numbers within fixed ranges.
	if err := validateJudgePromptDeliveryPatch(patch); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	// judge_strong_backend must be a string (empty clears strong-first
	// routing) and judge_verdict_cache_enabled accepts only a JSON boolean
	// (JSON null is not a boolean); each refusal names the key and the
	// whole PUT is refused before anything is applied.
	if err := validateJudgeStrongRoutingPatch(patch); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	// Questions switch: autonomous.questions.enabled accepts only a JSON
	// boolean. The CLI and the comm configure verb both send true/false as
	// JSON literals, and a quoted string or number is not a boolean, so it
	// rejects the whole PUT (400) rather than being stored.
	if v, ok := patch["autonomous.questions.enabled"]; ok {
		if _, isBool := v.(bool); !isBool {
			http.Error(w, "autonomous.questions.enabled: must be a boolean (true or false)", http.StatusBadRequest)
			return
		}
	}
	// The question notify toggles accept only a JSON boolean too: the same
	// whole-PUT rejection as the switch, so a quoted string or number 400s
	// rather than being stored.
	for _, k := range []string{
		"autonomous.questions.notify_messaging",
		"autonomous.questions.notify_alert",
		"autonomous.questions.return_path",
		"autonomous.questions.decision_gate",
		"autonomous.questions.worker_marker",
	} {
		if v, ok := patch[k]; ok {
			if _, isBool := v.(bool); !isBool {
				http.Error(w, k+": must be a boolean (true or false)", http.StatusBadRequest)
				return
			}
		}
	}
	// The decision gate mode is shadow or on. Anything else rejects the whole
	// PUT before a value is applied, by the same rule the config file load
	// uses.
	if v, ok := patch["autonomous.questions.decision_gate_mode"]; ok {
		mode, isStr := v.(string)
		if !isStr {
			http.Error(w, "autonomous.questions.decision_gate_mode: must be \"shadow\" or \"on\"", http.StatusBadRequest)
			return
		}
		if verr := config.ValidateDecisionGateMode(mode); verr != nil {
			http.Error(w, verr.Error(), http.StatusBadRequest)
			return
		}
	}
	// The held question mode is off, shadow or on (empty reads as shadow).
	// Same whole-PUT rejection, by the same rule the config file load uses.
	if v, ok := patch["autonomous.questions.held_mode"]; ok {
		mode, isStr := v.(string)
		if !isStr {
			http.Error(w, "autonomous.questions.held_mode: must be \"off\", \"shadow\" or \"on\"", http.StatusBadRequest)
			return
		}
		if verr := config.ValidateHeldMode(mode); verr != nil {
			http.Error(w, verr.Error(), http.StatusBadRequest)
			return
		}
	}
	// The task template modes (autonomous.task_templates.mode as a whole
	// {template id: mode} object, and the dotted form
	// autonomous.task_templates.mode.<id>) are llm, daemon or off. The merged
	// map is validated by the same rule the config file load uses
	// (TaskTemplatesConfig.Validate), so an unknown mode, daemon on a
	// non-daemon-capable template, or daemon on final_gate rejects the whole
	// PUT before a value is applied.
	if taskTemplateModePatchTouched(patch) {
		candidate, cerr := taskTemplateModePatchCandidate(s.cfg.Autonomous.TaskTemplates.Mode, patch)
		if cerr != nil {
			http.Error(w, cerr.Error(), http.StatusBadRequest)
			return
		}
		tc := config.TaskTemplatesConfig{Mode: candidate}
		if verr := tc.Validate(); verr != nil {
			http.Error(w, verr.Error(), http.StatusBadRequest)
			return
		}
	}
	// The script gate timeout (autonomous.script_gate_timeout_seconds) is a
	// whole number of seconds >= 0 (0 = no cap). A negative or non-whole
	// value rejects the whole PUT by the same rule the config file load
	// uses (validateScriptGateTimeout), so nothing is stored.
	if v, ok := patch["autonomous.script_gate_timeout_seconds"]; ok {
		if _, verr := parseProgressWhole("autonomous.script_gate_timeout_seconds", v); verr != nil {
			http.Error(w, "autonomous.script_gate_timeout_seconds: must be a whole number of seconds >= 0 (0 = no cap)", http.StatusBadRequest)
			return
		}
	}
	// The gate drain wait (autonomous.gate_drain_minutes) is a whole number
	// of minutes >= 0 (0 = do not wait). A negative or non-whole value
	// rejects the whole PUT by the same rule the config file load uses.
	if v, ok := patch["autonomous.gate_drain_minutes"]; ok {
		if _, verr := config.ParseGateDrainMinutes(v); verr != nil {
			http.Error(w, verr.Error(), http.StatusBadRequest)
			return
		}
	}
	// The project gate command (autonomous.project_gate_command) is a single
	// line of shell text (empty = unset). Same whole-PUT rejection, by the
	// same rule the config file load uses (ValidateProjectGateCommand).
	if v, ok := patch["autonomous.project_gate_command"]; ok {
		cmd, isStr := v.(string)
		if !isStr {
			http.Error(w, "autonomous.project_gate_command: must be a string (empty = unset)", http.StatusBadRequest)
			return
		}
		if verr := config.ValidateProjectGateCommand("autonomous", cmd); verr != nil {
			http.Error(w, verr.Error(), http.StatusBadRequest)
			return
		}
	}
	// The held state ids cover tasks parked in one of the five known states.
	// An absent key means all five, an explicit empty list means none; an
	// unknown id names the valid ones and rejects the whole PUT before
	// anything is applied.
	if v, ok := patch["autonomous.questions.held_states"]; ok {
		var candidate []string
		if arr, ok := toStringArray(v); ok {
			candidate = arr
		} else if str, ok := v.(string); ok {
			candidate = splitCSV(str)
		} else {
			http.Error(w, "autonomous.questions.held_states: must be a list of state ids or a comma-separated list", http.StatusBadRequest)
			return
		}
		if verr := config.ValidateHeldStates(candidate); verr != nil {
			http.Error(w, verr.Error(), http.StatusBadRequest)
			return
		}
	}
	// The question quality and queue preview keys: a bad value rejects the
	// whole PUT before anything is applied, by the same rules the config file
	// load uses.
	if err := validateQuestionQualityPatch(patch); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	// The decision record keys: a bad value rejects the whole PUT before
	// anything is applied, by the same rules the config file load uses.
	if err := validateDecisionRecordPatch(patch); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	// The plan-records keys, same rule.
	if err := validatePlanRecordsPatch(patch); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	// identity.backups_to_keep: a bad value rejects the whole PUT before
	// anything is applied, by the same rule the config file load uses.
	if err := validateIdentityBackupsPatch(patch); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	// resource_limits.*: share 1 to 100, floor not above cap; a bad value
	// rejects the whole PUT before anything is applied.
	if err := validateResourceLimitsPatch(s.cfg.ResourceLimits, patch); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	// session.dismiss_ui_prompts and the session.ui_prompt_* keys: a bad value
	// rejects the whole PUT before anything is applied, by the same rules the
	// config file load uses.
	if err := validateUIPromptPatch(patch); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	// session.liveness_window_seconds / session.liveness_idle_timeout_overrides:
	// a bad value or an unknown backend name rejects the whole PUT.
	if err := validateLivenessPatch(patch, s.availableBackends); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	// ui.splash_min_seconds and ui.splash_min_seconds_new_version: a bad value
	// rejects the whole PUT before anything is applied, by the same rules the
	// config file load uses.
	if err := validateUISplashPatch(patch); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	// pi.thinking_level — pre-validate like task_override_statuses above so an
	// unknown level rejects the whole PUT instead of silently no-op'ing in
	// applyConfigPatch. Empty string is valid (clears the flag).
	if raw, ok := patch["pi.thinking_level"]; ok {
		if ls, isStr := raw.(string); !isStr || !config.IsPiThinkingLevel(ls) {
			http.Error(w, "pi.thinking_level: use one of "+strings.Join(config.PiThinkingLevels, ", ")+" (empty clears it)", http.StatusBadRequest)
			return
		}
	}
	// autonomous.loop_detect_turns — only the integer form is accepted; 0
	// (loop detection off) is valid.
	if raw, ok := patch["autonomous.loop_detect_turns"]; ok {
		if n, okInt := toInt(raw); !okInt || n < 0 {
			http.Error(w, "autonomous.loop_detect_turns: must be a non-negative integer (0 turns loop detection off; default 3)", http.StatusBadRequest)
			return
		}
	}
	// autonomous.snapshot_never_stage — the snapshot's never-stage list.
	// Blank patterns and a wrong type reject the whole PUT (same rule as
	// the /api/autonomous/config surface); a CSV blank value is nil and
	// passes (unset → the built-in defaults apply).
	if rawNS, ok := patch["autonomous.snapshot_never_stage"]; ok {
		var patterns []string
		if arr, isArr := toStringArray(rawNS); isArr {
			patterns = arr
		} else if str, isStr := rawNS.(string); isStr {
			patterns = splitCSV(str)
		} else {
			http.Error(w, "autonomous.snapshot_never_stage: must be a list of path patterns or a comma-separated string", http.StatusBadRequest)
			return
		}
		if verr := config.ValidateSnapshotNeverStage(patterns); verr != nil {
			http.Error(w, verr.Error(), http.StatusBadRequest)
			return
		}
	}
	// autonomous.requirements (min-Ollama overrides) — a bad capability, or
	// a value that is not a version string, rejects the whole PUT like the
	// keys above, by the one shared rule set; the per-capability
	// autonomous.requirements.min_ollama.<capability> keys are validated by
	// the same helper.
	if raw, ok := patch["autonomous.requirements"]; ok {
		if _, verr := validateRequirementsConfigBlock(raw); verr != nil {
			http.Error(w, verr.Error(), http.StatusBadRequest)
			return
		}
	}
	if err := validateChecksShipPatch(patch); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := validateRestartPolicyPatch(patch); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	// tracing.* — pre-validate like git.secret_scan below: a bad mode, cap,
	// retention or backend map rejects the whole PUT.
	if terr := validateTracingPatch(s.cfg.Tracing, patch); terr != nil {
		http.Error(w, terr.Error(), http.StatusBadRequest)
		return
	}
	// autonomous.release_path and git.release_ci_window_minutes — refused
	// with the config layer's own message so every surface shows the same text.
	if raw, ok := patch["autonomous.release_path"]; ok {
		rs, isStr := raw.(string)
		if !isStr {
			http.Error(w, "autonomous.release_path must be \"direct_push\" or \"pull_request\"", http.StatusBadRequest)
			return
		}
		if verr := config.ValidateReleasePath(strings.TrimSpace(rs)); verr != nil {
			http.Error(w, verr.Error(), http.StatusBadRequest)
			return
		}
	}
	if raw, ok := patch["git.release_ci_window_minutes"]; ok {
		n, isInt := toInt(raw)
		if !isInt {
			http.Error(w, "git.release_ci_window_minutes must be greater than 0 (a whole number of minutes)", http.StatusBadRequest)
			return
		}
		if verr := config.ValidateReleaseCIWindowMinutes(n); verr != nil {
			http.Error(w, verr.Error(), http.StatusBadRequest)
			return
		}
	}
	// git.secret_scan.mode — pre-validate like pi.thinking_level above: an
	// unknown mode (a security-critical setting) rejects the whole PUT
	// instead of silently no-op'ing in applyConfigPatch.
	if raw, ok := patch["git.secret_scan.mode"]; ok {
		ms, isStr := raw.(string)
		if !isStr || config.ValidateGitSecretScanMode(strings.ToLower(strings.TrimSpace(ms))) != nil {
			http.Error(w, "git.secret_scan.mode: use one of "+config.GitSecretScanShadow+", "+config.GitSecretScanEnforce+", "+config.GitSecretScanOff, http.StatusBadRequest)
			return
		}
	}
	// git.secret_scan.detectors — an unknown detector name rejects the whole
	// PUT: a typo would otherwise make the push-time scan fail and, in
	// enforce mode, let the push through with only a log line.
	if raw, ok := patch["git.secret_scan.detectors"]; ok {
		var names []string
		if arr, okArr := toStringArray(raw); okArr {
			names = arr
		} else if str, okStr := raw.(string); okStr {
			names = splitCSV(str)
		} else {
			http.Error(w, "git.secret_scan.detectors: must be a list of detector names or a comma-separated list", http.StatusBadRequest)
			return
		}
		if verr := config.ValidateGitSecretScanDetectors(names); verr != nil {
			http.Error(w, verr.Error(), http.StatusBadRequest)
			return
		}
	}
	for k := range patch {
		if !strings.HasPrefix(k, "autonomous.requirements.min_ollama.") {
			continue
		}
		if _, verr := validateRequirementsMinOllamaKey(strings.TrimPrefix(k, "autonomous.requirements.min_ollama."), patch[k]); verr != nil {
			http.Error(w, verr.Error(), http.StatusBadRequest)
			return
		}
	}
	// git.secret_scan.allow_list.fingerprints — 16 or 64 lowercase hex
	// chars each (sha256 of the secret, first 16 = short form). A bad
	// entry rejects the whole PUT; allow-list fingerprints never appear
	// in findings, so an unparseable one is always an operator error.
	if raw, ok := patch["git.secret_scan.allow_list.fingerprints"]; ok {
		var candidate []string
		if arr, okArr := toStringArray(raw); okArr {
			candidate = arr
		} else if s, okStr := raw.(string); okStr {
			candidate = splitCSV(s)
		} else {
			http.Error(w, "git.secret_scan.allow_list.fingerprints: must be a list of fingerprints or a comma-separated list", http.StatusBadRequest)
			return
		}
		for _, fp := range candidate {
			if !isSecretFingerprint(fp) {
				http.Error(w, "git.secret_scan.allow_list.fingerprints: "+strings.TrimSpace(fp)+" is not a 16- or 64-char hex fingerprint (sha256 of the secret)", http.StatusBadRequest)
				return
			}
		}
	}
	// Verifier headroom keys: a negative value is a validation error (400), the
	// same refusal config load gives. 0 or unset means the default.
	if raw, ok := patch["autonomous.verifier_min_free_mem_mb"]; ok {
		if v, isInt := toInt(raw); !isInt || v < 0 {
			http.Error(w, "autonomous.verifier_min_free_mem_mb: must be a whole number >= 0 (0 turns the check off; unset = 1024)", http.StatusBadRequest)
			return
		}
	}
	if raw, ok := patch["autonomous.verifier_headroom_max_age_hours"]; ok {
		if v, isInt := toInt(raw); !isInt || v < 0 {
			http.Error(w, "autonomous.verifier_headroom_max_age_hours: must be a whole number >= 0 (0 or unset = default 24 hours)", http.StatusBadRequest)
			return
		}
	}
	if err := validateRulesContextPatch(s.cfg.Autonomous.RulesContext, patch); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	// plugins.services.* validates as a block (every value is also checked
	// on its own, but the combined state is what Load re-validates at the
	// next startup), so the patch is simulated onto the stored block and the
	// load-time validator is run over the result before anything is applied.
	if err := validatePluginServicesPatch(s.cfg.Plugins.Services, patch); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	// Worker session profile settings (autonomous.worker_profiles,
	// autonomous.worker_profile_defaults, autonomous.worker_profile):
	// simulate the patch onto a copy of the current config and run the
	// load-time validator over the result, so a built-in profile name, an
	// unknown profile reference or an unknown role/kind key rejects the whole
	// PUT (400 with the validation message) before anything is applied — the
	// same rule the config file load uses.
	if err := validateWorkerProfilePatch(s.cfg, patch); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	// Does this PUT touch the tags.placement block at all? The sweep trigger
	// after the save keys off this, so a request that changes no placement
	// key never sweeps.
	placementChanged := false
	for k := range patch {
		if strings.HasPrefix(k, "autonomous.tags.placement") {
			placementChanged = true
			break
		}
	}
	// Attempt-cost settings: when the patch sends any of the three keys,
	// validate the merged candidate (sent values layered over the stored
	// ones) with the shared config.ValidateAttemptCostSettings BEFORE
	// anything is applied. An unknown task kind, a number outside 0..max,
	// or a backend that needs a model without one names the key in a 400
	// and leaves every stored value untouched — the whole PUT is refused
	// and nothing is written. Keys the patch does not send keep their
	// stored values (the PUT's normal patch semantics).
	if err := expandAttemptCostDottedKeys(patch, s.cfg.Autonomous.FirstAttempt); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if attemptCostPatchTouched(patch) {
		next := s.cfg.Autonomous
		next.FirstAttempt = cloneAttemptChoices(next.FirstAttempt)
		if raw, ok := patch["autonomous.first_attempt"]; ok {
			fa, shapeOK := parseFirstAttempt(raw)
			if !shapeOK {
				http.Error(w, "autonomous.first_attempt: expected an object mapping a task kind (implementation, docs, note, final_gate) to {backend, model}", http.StatusBadRequest)
				return
			}
			next.FirstAttempt = fa
		}
		if raw, ok := patch["autonomous.first_attempt_escalate_after_failures"]; ok {
			n, numOK := toInt(raw)
			if !numOK {
				http.Error(w, "autonomous.first_attempt_escalate_after_failures: expected a whole number", http.StatusBadRequest)
				return
			}
			next.FirstAttemptEscalateAfterFailures = n
		}
		if raw, ok := patch["autonomous.retry_resume_max_context_tokens"]; ok {
			n, numOK := toInt(raw)
			if !numOK {
				http.Error(w, "autonomous.retry_resume_max_context_tokens: expected a whole number", http.StatusBadRequest)
				return
			}
			next.RetryResumeMaxContextTokens = n
		}
		if verr := config.ValidateAttemptCostSettings(next); verr != nil {
			http.Error(w, verr.Error(), http.StatusBadRequest)
			return
		}
	}
	// Branch refresh settings: a non-boolean enabled or a negative or
	// non-numeric behind limit refuses the whole PUT (branch_refresh_api.go).
	if err := validateBranchRefreshPatch(patch); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	// Release approval modes go through SetReleaseApprovalMode, which stores
	// them with an audit event and a decision record, so they are taken out of
	// the patch here and never reach applyConfigPatch.
	if err := s.routeReleaseModeKeys(releaseModeSurface(r), patch); err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, errReleaseModeUnavailable) {
			status = http.StatusServiceUnavailable
		}
		http.Error(w, err.Error(), status)
		return
	}
	if err := s.applyLLMBillingModePatch(patch); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	// A task routing block is checked against the live LLM registry before it
	// is applied, so a row naming an unknown backend is refused and the reply
	// names the row. applyConfigPatch repeats the registry-free checks.
	var routingProblems []string
	routingRefused := false
	if raw, ok := patch["autonomous.task_routing"]; ok {
		var in config.TaskRoutingConfig
		if b, err := json.Marshal(raw); err == nil && json.Unmarshal(b, &in) == nil {
			if errs := config.ValidateTaskRouting(in, s.taskRoutingChecks()); len(errs) > 0 {
				routingProblems = taskRoutingProblems(errs)
				routingRefused = true
				delete(patch, "autonomous.task_routing")
			}
		}
	}
	skipped := applyConfigPatch(s.cfg, patch)
	if routingRefused {
		skipped = append(skipped, "autonomous.task_routing")
	}
	// Keep the ship step's policy in step with the live autonomous.ship config.
	autonomous.SetShipConfig(s.cfg.Autonomous.Ship)
	autonomous.SetChecksConfig(s.cfg.Autonomous.Checks)
	// Same for the per-project rules/context names: resolution reads the
	// installed snapshot, so this keeps planning in step without a restart.
	autonomous.SetRulesContextConfig(s.cfg.Autonomous.RulesContext)
	// observer.pull.* — the pull collector reads these globals when it
	// derives targets (a per-peer interval/timeout of 0 falls back here),
	// so after a PUT that touches them re-sync the target set; no restart
	// needed.
	for k := range patch {
		if strings.HasPrefix(k, "observer.pull.") {
			s.syncObserverPullTargets()
			break
		}
	}
	// The question lifecycle events follow the switch at runtime: a flip
	// attaches (or drops) the store's event sink, so no restart is needed.
	if _, ok := patch["autonomous.questions.enabled"]; ok && s.questionEventsSwitch != nil {
		s.questionEventsSwitch(s.cfg.Autonomous.Questions.Enabled)
	}
	// The shared tag registry follows its keeper and sync interval at runtime,
	// so a change made here needs no restart.
	for _, k := range []string{"autonomous.tags.registry_keeper", "autonomous.tags.registry_sync_seconds"} {
		if _, ok := patch[k]; ok {
			s.applyTagRegistrySettings()
			break
		}
	}
	// Auto tagging (autonomous.auto_tags.*) follows the live block without
	// a restart: the engine-tag sync reads it at sync time (auto_tags.go),
	// and a whole-PUT re-sync of every existing Automation applies a
	// flip-off (strip engine tags, reclaim un-referenced records) or a
	// template change at once.
	for k := range patch {
		if strings.HasPrefix(k, config.AutoTagsSettingPrefix) {
			if a, ok := s.autonomousMgr.(autoTagsSyncAPI); ok {
				a.SyncAutoTagsAll()
			}
			break
		}
	}
	// BL368 — re-initialize visioner when vision config changes via PUT.
	for k := range patch {
		if strings.HasPrefix(k, "vision.") {
			if s.cfg.Vision.Enabled {
				v, verr := vision.New(vision.Config{
					Backend:       s.cfg.Vision.Backend,
					Endpoint:      s.cfg.Vision.Endpoint,
					APIKey:        s.cfg.Vision.APIKey,
					Model:         s.cfg.Vision.Model,
					DefaultPrompt: s.cfg.Vision.DefaultPrompt,
					MaxImageBytes: s.cfg.Vision.MaxImageBytes,
				})
				if verr != nil {
					fmt.Fprintf(os.Stderr, "[vision] config update warning: %v\n", verr)
					s.visioner = nil
				} else {
					s.visioner = v
				}
			} else {
				s.visioner = nil
			}
			break
		}
	}
	// B30: apply hot-reloadable session knobs to live manager.
	if s.manager != nil {
		s.manager.SetScheduleSettleMs(s.cfg.Session.ScheduleSettleMs)
	}
	// The inherited-tags cache lifetime is hot-reloadable too: a write
	// applies to the live session manager immediately, no restart.
	if _, touched := patch["autonomous.inherited_tags_cache_seconds"]; touched && s.manager != nil {
		s.manager.SetInheritedTagsCacheTTL(time.Duration(s.cfg.Autonomous.InheritedTagsCacheSeconds) * time.Second)
	}
	for k := range patch {
		if strings.HasPrefix(k, "session.store_") {
			s.applyStoreConfig()
			break
		}
	}
	// BL394 -- re-sync the live push-SSRF-guard config on any push.* change.
	for k := range patch {
		if strings.HasPrefix(k, "push.") {
			setPushConfig(s.cfg.Push)
			break
		}
	}
	// v7.0.0: BL41 default_effort and claude-code fields moved to LLM registry.
	// Apply registry patches for the moved fields.
	if s.inferenceReg != nil {
		s.applyLLMRegistryPatch(patch)
		// Apply default_effort to live manager from registry.
		if s.manager != nil {
			if cl, err := s.inferenceReg.Get("claude-code"); err == nil && cl != nil {
				effort := cl.DefaultEffort
				if effort == "" {
					effort = "normal"
				}
				s.manager.SetDefaultEffort(effort)
			}
		}
	}
	if err := config.Save(s.cfg, s.cfgPath); err != nil {
		http.Error(w, "save failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	// The tags.placement block (autonomous.tags.placement.*) follows the
	// live config without a restart: new placements already read it on every
	// derivation (SetPlacementTagSettings), and one tag-sweep pass
	// re-derives the existing sessions' and records' system tags under the
	// new block — turning an axis off strips its tag, a template change
	// re-stamps it where the fact exists; the operator's own tags are a
	// separate field the sweep never touches. The pass runs in the
	// background after the config is saved, so the PUT answers without
	// waiting on the whole-history sweep; a request that changed no
	// placement key never reaches this point.
	if placementChanged && s.autonomousMgr != nil {
		if a, ok := s.autonomousMgr.(tagSweepAPI); ok {
			go func() {
				res := a.SweepTags(false)
				log.Printf("[autonomous] tag sweep (placement settings changed): sessions %d/%d changed, automata %d/%d, automations %d/%d, tasks %d/%d, templates %d/%d, decision records %d/%d, questions %d/%d",
					res.Sessions.Changed, res.Sessions.Examined,
					res.Automata.Changed, res.Automata.Examined,
					res.Automations.Changed, res.Automations.Examined,
					res.Tasks.Changed, res.Tasks.Examined,
					res.Templates.Changed, res.Templates.Examined,
					res.DecisionRecords.Changed, res.DecisionRecords.Examined,
					res.Questions.Changed, res.Questions.Examined)
			}()
		}
	}
	// A config write can create a model-to-node collision, so re-check and log
	// every one that now exists.
	for _, warning := range s.modelNodeWarnings() {
		log.Printf("model-node collision: %s", warning.Message)
	}
	// SEC-017 — every config write, logged with the key names touched and
	// a masked value for each (credential-shaped keys are masked outright;
	// skipped keys, per applyConfigPatch, never reach here at all).
	s.auditConfigPatch(r.Context(), patch, skipped)
	// Model-pull consent changed: record the consent itself (which models
	// are allowed, which are refused, which publishers are allow-listed),
	// not just the generic config-patch entry.
	if _, touched := patch["autonomous.model_pull_consent"]; touched {
		sel := s.cfg.Autonomous.ModelPullConsent
		names := make([]string, 0, len(sel.Models))
		for i := range sel.Models {
			if sel.Models[i].Allowed {
				names = append(names, sel.Models[i].Name+"=pull_allowed")
			} else {
				names = append(names, sel.Models[i].Name+"=pull_refused")
			}
		}
		s.auditAs(s.auditActor(r.Context()), audit.ActionModelPullConsentChanged, "model_pull_consent", "autonomous",
			map[string]any{"models": names, "publishers": sel.Publishers})
	}
	// BL369 — propagate autonomous.* config changes to the live manager so
	// injection_guard and other knobs take effect without a daemon restart.
	// Forward only the top-level JSON field(s) this PUT actually touched
	// (keyed by the dot-path's first segment after "autonomous."), read
	// from the already-type-converted s.cfg.Autonomous rather than the
	// raw patch values (which may still be wire-format strings/CSV for
	// array fields). This used to marshal the WHOLE s.cfg.Autonomous
	// struct on every autonomous.* write: API.SetConfig merges onto the
	// live config (v8.36.9), but a handful of fields are deliberately
	// "no omitempty" (AutoApproveChildren, PerStoryApproval,
	// ContinueOnStoryFailure, InjectionGuard, BlockOnInjection) so an
	// explicit false isn't silently dropped — which means they're ALWAYS
	// present in a full-struct marshal, so any unrelated autonomous.*
	// write re-applied s.cfg.Autonomous's current (possibly still
	// YAML-zero) value and clobbered a live-only true back to false.
	// Found live 2026-10-11 via autonomous.auto_approve_children.
	// Scoping the propagated JSON to only the touched top-level field(s)
	// lets every untouched field, no-omitempty or not, pass through
	// SetConfig's merge untouched.
	touchedTop := map[string]struct{}{}
	for k := range patch {
		if rest, ok := strings.CutPrefix(k, "autonomous."); ok {
			if i := strings.IndexByte(rest, '.'); i >= 0 {
				rest = rest[:i]
			}
			touchedTop[rest] = struct{}{}
		}
	}
	if len(touchedTop) > 0 && s.autonomousMgr != nil {
		if full, err := json.Marshal(s.cfg.Autonomous); err == nil {
			var fullMap map[string]json.RawMessage
			if json.Unmarshal(full, &fullMap) == nil {
				partial := make(map[string]json.RawMessage, len(touchedTop))
				for top := range touchedTop {
					if v, ok := fullMap[top]; ok {
						partial[top] = v
					} else if v, ok := autonomousFieldJSONByTag(s.cfg.Autonomous, top); ok {
						// Found live 2026-10-11: a touched field whose
						// CURRENT value is its zero value (an explicit
						// "clear this back to empty" PUT, e.g.
						// autonomous.planning_model: "") is omitted from
						// fullMap entirely by that very field's own
						// omitempty tag — so the clear never reached the
						// live manager, which kept serving its stale
						// value. Encode the zero value directly instead
						// of skipping it.
						partial[top] = v
					}
				}
				if raw, ok := partial["automation"]; ok && patchTouchesSupervisorWatch(patch) {
					partial["automation"] = withSupervisorWatchClears(raw, s.cfg.Autonomous.Automation.SupervisorWatch)
				}
				if _, touched := patch[protectedPathsKey]; touched {
					if raw, ok := partial["automation"]; ok {
						partial["automation"] = withProtectedPathsList(raw, s.cfg.Autonomous.Automation.ProtectedPaths)
					}
				}
				if raw, ok := partial["automation"]; ok && patchTouchesPendingItem(patch) {
					partial["automation"] = withPendingItemClears(raw, s.cfg.Autonomous.Automation)
				}
				if b, err := json.Marshal(partial); err == nil {
					_ = s.autonomousMgr.SetConfig(json.RawMessage(b))
				}
			}
		}
	}
	// Invalidate backend version cache so next /api/backends reflects changes.
	s.versionCacheMu.Lock()
	s.versionCacheAt = time.Time{}
	s.versionCacheMu.Unlock()
	go s.warmVersionCache()

	w.Header().Set("Content-Type", "application/json")
	resp := map[string]interface{}{"status": "ok"}
	if len(skipped) > 0 {
		// SEC-016 — loud, not silent: a caller still trying to set
		// server.token/mcp.token through the generic patch path finds out
		// immediately rather than assuming it took effect.
		resp["skipped"] = skipped
		reason := "use POST /api/auth/rotate-token to change the admin bearer token"
		if allTaskRoutingKeys(skipped) {
			reason = "not stored: the active profile must name an existing profile, and the task routing block must pass validation"
		}
		resp["skipped_reason"] = reason
		if len(routingProblems) > 0 {
			resp["skipped_problems"] = routingProblems
		}
	}
	if _, ok := patch["autonomous.max_parallel_tasks"]; ok {
		resp["deprecated_fields"] = map[string]string{
			"autonomous.max_parallel_tasks": maxParallelTasksDeprecatedNote,
		}
	}
	json.NewEncoder(w).Encode(resp) //nolint:errcheck
}

// maxParallelTasksDeprecatedNote is the deprecated_fields note returned by
// PUT /api/config and PUT /api/autonomous/config when a caller still sends
// max_parallel_tasks.
const maxParallelTasksDeprecatedNote = "deprecated and ignored; use autonomous.max_concurrent_tasks"

// applyLLMRegistryPatch applies config patch keys that now live in the LLM registry
// (v7.0.0 clean move from SessionConfig). Upserts the claude-code entry.
func (s *Server) applyLLMRegistryPatch(patch map[string]interface{}) {
	// Collect registry-bound keys from the patch.
	keys := []string{
		"session.skip_permissions", "session.channel_enabled",
		"session.claude_auto_accept_disclaimer", "session.permission_mode",
		"session.default_effort", "session.fallback_chain",
	}
	hasRegistryKey := false
	for _, k := range keys {
		if _, ok := patch[k]; ok {
			hasRegistryKey = true
			break
		}
	}
	if !hasRegistryKey {
		return
	}

	var cl *inference.LLM
	if existing, err := s.inferenceReg.Get("claude-code"); err == nil && existing != nil {
		cl = existing
	} else {
		cl = &inference.LLM{Name: "claude-code", Kind: inference.KindClaudeCode, ChannelEnabled: true, AutoCreated: true}
	}

	for k, v := range patch {
		switch k {
		case "session.skip_permissions":
			cl.SkipPermissions = toBool(v)
		case "session.channel_enabled":
			cl.ChannelEnabled = toBool(v)
		case "session.claude_auto_accept_disclaimer":
			cl.AutoAcceptDisclaimer = toBool(v)
		case "session.permission_mode":
			cl.PermissionMode = toString(v)
		case "session.default_effort":
			if s2 := toString(v); s2 != "" {
				cl.DefaultEffort = s2
			}
		case "session.fallback_chain":
			s2 := toString(v)
			if s2 == "" {
				cl.FallbackChain = nil
			} else {
				parts := strings.Split(s2, ",")
				chain := make([]string, 0, len(parts))
				for _, p := range parts {
					p = strings.TrimSpace(p)
					if p != "" {
						chain = append(chain, p)
					}
				}
				cl.FallbackChain = chain
			}
		}
	}

	if _, err := s.inferenceReg.Get("claude-code"); err != nil {
		_ = s.inferenceReg.Add(cl)
	} else {
		_ = s.inferenceReg.Update(cl)
	}
}

// autonomousFieldJSONByTag returns the JSON encoding of cfg's field
// whose "json" struct tag's name segment matches tag, regardless of
// that field's own omitempty option — used as a fallback by the
// autonomous.* config-write propagation above when a touched field's
// current value happens to be its zero value: a normal marshal of
// the whole struct omits it (that's what omitempty is for), which
// would otherwise make an explicit "clear this back to empty" PUT
// silently never reach the live manager. Found live 2026-10-11.
func autonomousFieldJSONByTag(cfg config.AutonomousConfig, tag string) (json.RawMessage, bool) {
	rv := reflect.ValueOf(cfg)
	rt := rv.Type()
	for i := 0; i < rt.NumField(); i++ {
		name, _, _ := strings.Cut(rt.Field(i).Tag.Get("json"), ",")
		if name == "" || name == "-" || name != tag {
			continue
		}
		b, err := json.Marshal(rv.Field(i).Interface())
		if err != nil {
			return nil, false
		}
		return b, true
	}
	return nil, false
}

// applyConfigPatch applies dot-path key/value pairs from patch to cfg.
// Returns the keys that were present in patch but silently skipped (SEC-016:
// server.token/mcp.token specifically — these are the admin/MCP bearer
// credentials; changing them here would write a new value to disk that the
// live process never picks up (no restart happens from this path) with no
// grace window on the old value, so any client still using it would be
// stuck guessing when it stops working. Use POST /api/auth/rotate-token
// instead, which hot-swaps the live credential with a 60s overlap).
func applyConfigPatch(cfg *config.Config, patch map[string]interface{}) []string {
	var skipped []string
	for k, v := range patch {
		if strings.HasPrefix(k, "tracing.backends.") {
			_ = applyTracingKey(&cfg.Tracing, k, v)
			continue
		}
		// projects_defaults.* — the daemon-level project defaults. Validated in
		// handlePutConfig; a direct applyConfigPatch caller's bad value is left
		// unchanged.
		if strings.HasPrefix(k, config.ProjectsDefaultsSettingPrefix) {
			_ = cfg.ProjectsDefaults.ApplyProjectsDefaultsSetting(k, v)
			continue
		}
		if _, ok := supervisorLoopPatchKey(k); ok {
			// Validated in handlePutConfig; stored under its plan key.
			applySupervisorLoopPatch(cfg, map[string]interface{}{k: v})
			continue
		}
		// autonomous.requirements.min_ollama.<capability> — the dynamic
		// per-capability min-Ollama overrides (one key per capability),
		// handled before the switch so future capabilities need no switch
		// case. Validated in handlePutConfig by the shared rule set; a
		// direct applyConfigPatch caller's bad value is left unchanged.
		if capability, ok := strings.CutPrefix(k, "autonomous.requirements.min_ollama."); ok && capability != "" {
			if version, verr := validateRequirementsMinOllamaKey(capability, v); verr == nil {
				if cfg.Autonomous.Requirements.MinOllama == nil {
					cfg.Autonomous.Requirements.MinOllama = map[string]string{}
				}
				cfg.Autonomous.Requirements.MinOllama[capability] = version
			}
			continue
		}
		// plugins.services.per_service.<name> — the dynamic per-plugin map,
		// handled before the switch like its sibling dynamic map above; the
		// lifecycle mode is validated by validatePluginServicesPatch (the
		// shared load-time rule) before anything is applied here.
		if service, ok := strings.CutPrefix(k, "plugins.services.per_service."); ok && service != "" {
			if ov, verr := normalizePluginServiceOverride(service, v); verr == nil {
				applyPluginServiceOverride(cfg, service, ov)
			}
			continue
		}
		// autonomous.task_templates.mode.<id> — the per-template mode in
		// dotted key form (one key per template id), handled before the
		// switch like its dynamic-map siblings above. Validated in
		// handlePutConfig by the shared rule set; a direct applyConfigPatch
		// caller's bad value is left unchanged.
		if id, ok := strings.CutPrefix(k, "autonomous.task_templates.mode."); ok && id != "" {
			if mode, isStr := v.(string); isStr {
				tc := config.TaskTemplatesConfig{Mode: map[string]string{id: mode}}
				if tc.Validate() == nil {
					if cfg.Autonomous.TaskTemplates.Mode == nil {
						cfg.Autonomous.TaskTemplates.Mode = map[string]string{}
					}
					cfg.Autonomous.TaskTemplates.Mode[id] = mode
				}
			}
			continue
		}
		switch k {
		case "server.token", "mcp.token":
			skipped = append(skipped, k)
			continue
		case "notify_exclude":
			if arr, ok := toStringArray(v); ok {
				cfg.NotifyExclude = arr
			} else if s, ok := v.(string); ok {
				cfg.NotifyExclude = splitCSV(s)
			}
		// cost_report.* — the cost report engine knobs (internal/costreport);
		// each sticks as the pointer field, so an absent key reads the default.
		case "cost_report.default_group_by":
			if str := toString(v); str != "" {
				cfg.CostReport.DefaultGroupBy = &str
			}
		case "cost_report.default_window_days":
			if n, ok := toInt(v); ok {
				cfg.CostReport.DefaultWindowDays = &n
			}
		case "cost_report.show_usd_estimates":
			b := toBool(v)
			cfg.CostReport.ShowUSDEstimates = &b
		case "cost_report.week_start":
			if str := toString(v); str != "" {
				cfg.CostReport.WeekStart = &str
			}
		case "cost_report.include_peer_spend":
			b := toBool(v)
			cfg.CostReport.IncludePeerSpend = &b
		// dashboard.hide_inactive_cards — explicit true/false both stick;
		// an absent key (never PUT) reads the default, true.
		case "dashboard.hide_inactive_cards":
			b := toBool(v)
			cfg.Dashboard.HideInactiveCards = &b
		// dashboard.console.* — the operator console switches behind
		// GET /api/dashboard/console: same pattern, explicit true/false both
		// stick, an absent subkey (never PUT) reads the default, true.
		case "dashboard.console.enabled":
			b := toBool(v)
			cfg.Dashboard.Console.Enabled = &b
		case "dashboard.console.show_questions":
			b := toBool(v)
			cfg.Dashboard.Console.ShowQuestions = &b
		case "dashboard.console.show_held_blocked":
			b := toBool(v)
			cfg.Dashboard.Console.ShowHeld = &b
		case "dashboard.console.show_lanes":
			b := toBool(v)
			cfg.Dashboard.Console.ShowLanes = &b
		case "dashboard.console.show_usage":
			b := toBool(v)
			cfg.Dashboard.Console.ShowUsage = &b
		case "dashboard.console.show_ci_release":
			b := toBool(v)
			cfg.Dashboard.Console.ShowCIRelease = &b
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
			if s := toString(v); s != "" {
				cfg.Ntfy.ServerURL = s
			}
		case "ntfy.topic":
			cfg.Ntfy.Topic = toString(v)
		case "ntfy.token":
			if s := toString(v); s != "" {
				cfg.Ntfy.Token = s
			}
		// BL404 — imap_mcp config parity.
		case "imap_mcp.enabled":
			cfg.ImapMcp.Enabled = toBool(v)
		case "imap_mcp.url":
			cfg.ImapMcp.URL = toString(v)
		case "imap_mcp.account":
			cfg.ImapMcp.Account = toString(v)
		case "imap_mcp.subject_prefix":
			cfg.ImapMcp.SubjectPrefix = toString(v)
		case "imap_mcp.to":
			cfg.ImapMcp.To = toString(v)
		case "imap_mcp.token":
			// Ignore empty and the GET redaction placeholder so a client
			// echoing back the masked value never clobbers the real token.
			if s := toString(v); s != "" && s != "***" {
				cfg.ImapMcp.Token = s
			}
		case "email.enabled":
			cfg.Email.Enabled = toBool(v)
		case "email.host":
			if s := toString(v); s != "" {
				cfg.Email.Host = s
			}
		case "email.port":
			if n, ok := toInt(v); ok {
				cfg.Email.Port = n
			}
		case "email.username":
			cfg.Email.Username = toString(v)
		case "email.password":
			if s := toString(v); s != "" {
				cfg.Email.Password = s
			}
		case "email.from":
			cfg.Email.From = toString(v)
		case "email.to":
			cfg.Email.To = toString(v)
		case "twilio.enabled":
			cfg.Twilio.Enabled = toBool(v)
		case "twilio.account_sid":
			if s := toString(v); s != "" {
				cfg.Twilio.AccountSID = s
			}
		case "twilio.auth_token":
			if s := toString(v); s != "" {
				cfg.Twilio.AuthToken = s
			}
		case "twilio.from_number":
			cfg.Twilio.FromNumber = toString(v)
		case "twilio.to_number":
			cfg.Twilio.ToNumber = toString(v)
		case "twilio.webhook_addr":
			if s := toString(v); s != "" {
				cfg.Twilio.WebhookAddr = s
			}
		case "github_webhook.enabled":
			cfg.GitHubWebhook.Enabled = toBool(v)
		case "github_webhook.addr":
			if s := toString(v); s != "" {
				cfg.GitHubWebhook.Addr = s
			}
		case "github_webhook.secret":
			if s := toString(v); s != "" {
				cfg.GitHubWebhook.Secret = s
			}
		case "webhook.enabled":
			cfg.Webhook.Enabled = toBool(v)
		case "webhook.addr":
			if s := toString(v); s != "" {
				cfg.Webhook.Addr = s
			}
		case "webhook.token":
			if s := toString(v); s != "" {
				cfg.Webhook.Token = s
			}
		case "webhook.image_dir":
			// BL394 -- empty string must be settable (disables the
			// local-file-path feature), so unlike most string fields
			// here, don't skip an empty value.
			cfg.Webhook.ImageDir = toString(v)
		case "telegram.token":
			if s := toString(v); s != "" {
				cfg.Telegram.Token = s
			}
		case "telegram.chat_id":
			if n, ok := toInt(v); ok {
				cfg.Telegram.ChatID = int64(n)
			}
		case "discord.token":
			if s := toString(v); s != "" {
				cfg.Discord.Token = s
			}
		case "discord.channel_id":
			cfg.Discord.ChannelID = toString(v)
		case "slack.token":
			if s := toString(v); s != "" {
				cfg.Slack.Token = s
			}
		case "slack.channel_id":
			cfg.Slack.ChannelID = toString(v)
		case "matrix.homeserver":
			if s := toString(v); s != "" {
				cfg.Matrix.Homeserver = s
			}
		case "matrix.user_id":
			cfg.Matrix.UserID = toString(v)
		case "matrix.access_token":
			if s := toString(v); s != "" {
				cfg.Matrix.AccessToken = s
			}
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
			// v7.0.0: moved to LLM registry — handled by applyLLMRegistryPatch.
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
		case "session.liveness_window_seconds":
			if n, ok := wholeNumber(v); ok {
				cfg.Session.LivenessWindowSeconds = n
			}
		case "session.liveness_idle_timeout_overrides":
			if m, ok := livenessOverridesFromValue(v); ok {
				if len(m) == 0 {
					m = nil
				}
				cfg.Session.LivenessIdleTimeoutOverrides = m
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
			// v7.0.0: moved to LLM registry — handled by applyLLMRegistryPatch.
		case "session.claude_auto_accept_disclaimer":
			// v7.0.0: moved to LLM registry — handled by applyLLMRegistryPatch.
		case "session.permission_mode":
			// v7.0.0: moved to LLM registry — handled by applyLLMRegistryPatch.
		case "session.auto_git_init":
			cfg.Session.AutoGitInit = toBool(v)
		case "session.git_default":
			cfg.Session.GitDefault = toBool(v)
		case "git.push_on_complete":
			// Accept only the three known policies (case-insensitive,
			// trimmed). Invalid values are ignored — the stored value is
			// left untouched — the same way unknown keys are no-ops here.
			if s := strings.ToLower(strings.TrimSpace(toString(v))); s == config.GitPushNever || s == config.GitPushBranch || s == config.GitPushDefaultBranch {
				cfg.Git.PushOnComplete = s
			} else {
				fmt.Fprintf(os.Stderr, "[config] git.push_on_complete: invalid value %q (allowed: never, branch, default_branch); ignored\n", toString(v))
			}
		case "git.push_remote":
			// Trimmed; empty resets to the built-in default (origin),
			// resolved via GitConfig.EffectivePushRemote.
			cfg.Git.PushRemote = strings.TrimSpace(toString(v))
		case "tracing.mode", "tracing.arg_cap_bytes", "tracing.retention_days", "tracing.backends":
			// Pre-validated by the PUT handler (validateTracingPatch).
			_ = applyTracingKey(&cfg.Tracing, k, v)
		case "git.secret_scan.mode":
			// Pre-validated by the PUT handler (unknown modes reject the
			// whole PUT); empty clears the flag back to the shadow default.
			if s := strings.ToLower(strings.TrimSpace(toString(v))); config.ValidateGitSecretScanMode(s) == nil {
				cfg.Git.SecretScan.Mode = s
			}
		case "git.secret_scan.detectors":
			// Empty (either form) resets to all built-in detectors, resolved
			// via EffectiveSecretScanDetectors at scan time.
			if arr, ok := toStringArray(v); ok {
				cfg.Git.SecretScan.Detectors = arr
			} else if s, ok := v.(string); ok {
				cfg.Git.SecretScan.Detectors = splitCSV(s)
			}
		case "git.secret_scan.allow_list.paths":
			if arr, ok := toStringArray(v); ok {
				cfg.Git.SecretScan.AllowList.Paths = arr
			} else if s, ok := v.(string); ok {
				cfg.Git.SecretScan.AllowList.Paths = splitCSV(s)
			}
		case "git.secret_scan.allow_list.fingerprints":
			// Pre-validated by the PUT handler (each entry 16/64 hex).
			if arr, ok := toStringArray(v); ok {
				cfg.Git.SecretScan.AllowList.Fingerprints = arr
			} else if s, ok := v.(string); ok {
				cfg.Git.SecretScan.AllowList.Fingerprints = splitCSV(s)
			}
		case "session.kill_sessions_on_exit":
			cfg.Session.KillSessionsOnExit = toBool(v)
		case "session.one_shot_sessions":
			cfg.Session.OneShotSessions = toBool(v)
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
			// v7.0.0: moved to LLM registry — handled by applyLLMRegistryPatch.
		case "session.console_cols":
			if n, ok := toInt(v); ok {
				cfg.Session.ConsoleCols = n
			}
		case "session.console_rows":
			if n, ok := toInt(v); ok {
				cfg.Session.ConsoleRows = n
			}
		case "session.summarizer.enabled":
			cfg.Session.Summarizer.Enabled = toBool(v)
		case "session.summarizer.llm_ref":
			cfg.Session.Summarizer.LLMRef = toString(v)
		case "session.summarizer.prompt":
			cfg.Session.Summarizer.Prompt = toString(v)
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
		case "server.tls_auto_generate":
			cfg.Server.TLSAutoGenerate = toBool(v)
		case "server.tls_cert":
			cfg.Server.TLSCert = toString(v)
		case "server.tls_key":
			cfg.Server.TLSKey = toString(v)
		case "server.channel_port":
			if n, ok := toInt(v); ok {
				cfg.Server.ChannelPort = n
			}
		case "server.tls_port":
			if n, ok := toInt(v); ok {
				cfg.Server.TLSPort = n
			}
		case "server.proxy_sandbox_port":
			if n, ok := toInt(v); ok {
				cfg.Server.ProxySandboxPort = n
			}
		case "server.acknowledge_unauthenticated":
			cfg.Server.AcknowledgeUnauthenticated = toBool(v)
		case "server.auto_restart_on_config":
			cfg.Server.AutoRestartOnConfig = toBool(v)
		case "server.allowed_project_roots":
			// Validated before the patch is applied; an empty value clears it.
			if roots, err := parseAllowedProjectRoots(v); err == nil {
				cfg.Server.AllowedProjectRoots = roots
			}
		case "server.recent_session_minutes":
			if n, ok := toInt(v); ok {
				cfg.Server.RecentSessionMinutes = n
			}
		case "server.session_summary_terminal_limit":
			// 0 = no cap; negative values are ignored.
			if n, ok := toInt(v); ok && n >= 0 {
				cfg.Server.SessionSummaryTerminalLimit = n
			}
		case "server.session_summary_text_chars":
			// Values below the minimum are clamped up to it.
			if n, ok := toInt(v); ok {
				if n < minSessionSummaryTextChars {
					n = minSessionSummaryTextChars
				}
				cfg.Server.SessionSummaryTextChars = n
			}
		case "server.ws_session_list_default":
			// Only "full" or "delta"; anything else is ignored.
			if m := normalizeSessionListDefault(strings.TrimSpace(strings.ToLower(toString(v)))); m != "" {
				cfg.Server.WSSessionListDefault = m
			}
		case "server.ws_output_scope_default":
			// Only "all" or "subscribed"; anything else is ignored.
			if m := normalizeOutputScope(strings.TrimSpace(strings.ToLower(toString(v)))); m != "" {
				cfg.Server.WSOutputScopeDefault = m
			}
		case "server.api_gzip":
			cfg.Server.APIGzip = toBool(v)
		case "server.api_gzip_min_bytes":
			// Negative values are clamped to 0 (compress any non-empty body).
			if n, ok := toInt(v); ok {
				if n < 0 {
					n = 0
				}
				cfg.Server.APIGzipMinBytes = n
			}
		case "server.suppress_active_toasts":
			cfg.Server.SuppressActiveToasts = toBool(v)
		// BL397 — native ACME subsystem.
		case "acme.enabled":
			cfg.Acme.Enabled = toBool(v)
		case "acme.endpoint":
			if s := toString(v); s != "" {
				cfg.Acme.Endpoint = s
			}
		case "acme.domains":
			if s := toString(v); s != "" {
				domains := make([]string, 0)
				for _, d := range strings.Split(s, ",") {
					if d = strings.TrimSpace(d); d != "" {
						domains = append(domains, d)
					}
				}
				cfg.Acme.Domains = domains
			}
		case "acme.renewal_days":
			if n, ok := toInt(v); ok {
				cfg.Acme.RenewalDays = n
			}
		case "acme.method":
			if s := toString(v); s == "http01" || s == "dns01" {
				cfg.Acme.Method = s
			}
		case "acme.dns01.provider":
			cfg.Acme.DNS01.Provider = toString(v)
		case "acme.dns01.token_secret":
			if s := toString(v); s != "" {
				cfg.Acme.DNS01.TokenSecret = s
			}
		case "acme.dns01.zone_id":
			cfg.Acme.DNS01.ZoneID = toString(v)
		case "acme.apply.hot_swap":
			cfg.Acme.Apply.HotSwap = toBool(v)
		// GH#201 — HTTP access / WS lifecycle / auth-failure log.
		case "audit.access_log_enabled":
			b := toBool(v)
			cfg.Audit.AccessLogEnabled = &b
		case "audit.retention_days":
			if n, ok := toInt(v); ok {
				cfg.Audit.RetentionDays = n
			}
		case "audit.cef_mirror_enabled":
			cfg.Audit.CEFMirrorEnabled = toBool(v)
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
		case "mcp.public_url":
			// handlePutConfig has already rejected invalid values; an invalid
			// one reaching here (a direct caller) leaves the field unchanged.
			if u, err := config.NormalizePublicURL(toString(v)); err == nil {
				cfg.MCP.PublicURL = u
			}
		case "mcp.sse_enabled":
			cfg.MCP.SSEEnabled = toBool(v)
		case "mcp.resource_subscriptions":
			cfg.MCP.ResourceSubscriptions = toBool(v)
		case "mcp.tls_enabled":
			cfg.MCP.TLSEnabled = toBool(v)
		case "mcp.tls_auto_generate":
			cfg.MCP.TLSAutoGenerate = toBool(v)
		case "mcp.tls_cert":
			cfg.MCP.TLSCert = toString(v)
		case "mcp.tls_key":
			cfg.MCP.TLSKey = toString(v)
		// BL412 — self-config group. allow_self_config is stored as an
		// explicit bool (the tri-state "absent = on" default is resolved on
		// read via SelfConfigEnabled; PUT makes the operator's intent stick).
		case "mcp.allow_self_config":
			b := toBool(v)
			cfg.MCP.AllowSelfConfig = &b
		// allow_sensitive_writes is stored as an explicit bool, like
		// allow_self_config, so the operator's choice sticks.
		case "mcp.allow_sensitive_writes":
			b := toBool(v)
			cfg.MCP.AllowSensitiveWrites = &b
		case "mcp.self_config_audit_path":
			if s := toString(v); s != "" {
				cfg.MCP.SelfConfigAuditPath = s
			}
		case "mcp.self_config_audit_denied":
			b := toBool(v)
			cfg.MCP.SelfConfigAuditDenied = &b
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
			if s := toString(v); s != "" {
				cfg.DNSChannel.Mode = s
			}
		case "dns_channel.domain":
			if s := toString(v); s != "" {
				cfg.DNSChannel.Domain = s
			}
		case "dns_channel.listen":
			if s := toString(v); s != "" {
				cfg.DNSChannel.Listen = s
			}
		case "dns_channel.upstream":
			if s := toString(v); s != "" {
				cfg.DNSChannel.Upstream = s
			}
		case "dns_channel.secret":
			if s := toString(v); s != "" {
				cfg.DNSChannel.Secret = s
			}
		case "dns_channel.ttl":
			if n, ok := toInt(v); ok {
				cfg.DNSChannel.TTL = n
			}
		case "dns_channel.max_response_size":
			if n, ok := toInt(v); ok {
				cfg.DNSChannel.MaxResponseSize = n
			}
		case "dns_channel.poll_interval":
			if s := toString(v); s != "" {
				cfg.DNSChannel.PollInterval = s
			}
		case "dns_channel.rate_limit":
			if n, ok := toInt(v); ok {
				cfg.DNSChannel.RateLimit = n
			}
		// Push endpoint validation (BL394 SSRF fix) — see push.go header comment.
		case "push.allow_insecure_endpoints":
			cfg.Push.AllowInsecureEndpoints = toBool(v)
		case "push.block_private_endpoints":
			cfg.Push.BlockPrivateEndpoints = toBool(v)
		// BL397 Phase 4 / BL335 — APNs dispatch config.
		case "push.apns.enabled":
			cfg.Push.APNs.Enabled = toBool(v)
		case "push.apns.key_id":
			cfg.Push.APNs.KeyID = toString(v)
		case "push.apns.team_id":
			cfg.Push.APNs.TeamID = toString(v)
		case "push.apns.bundle_id":
			cfg.Push.APNs.BundleID = toString(v)
		case "push.apns.key_path":
			cfg.Push.APNs.KeyPath = toString(v)
		case "push.apns.key_secret":
			if s := toString(v); s != "" {
				cfg.Push.APNs.KeySecret = s
			}
		case "push.apns.sandbox":
			cfg.Push.APNs.Sandbox = toBool(v)

		// Memory config
		case "memory.enabled":
			cfg.Memory.Enabled = toBool(v)
		case "memory.backend":
			if s := toString(v); s == "sqlite" || s == "postgres" {
				cfg.Memory.Backend = s
			}
		case "memory.db_path":
			cfg.Memory.DBPath = toString(v)
		case "memory.postgres_url":
			cfg.Memory.PostgresURL = toString(v)
		case "memory.fallback_sqlite":
			cfg.Memory.FallbackSQLite = toBool(v)
		case "memory.embedder":
			if s := toString(v); s == "ollama" || s == "openai" {
				cfg.Memory.Embedder = s
			}
		case "memory.embedder_model":
			if s := toString(v); s != "" {
				cfg.Memory.EmbedderModel = s
			}
		case "memory.embedder_host":
			cfg.Memory.EmbedderHost = toString(v)
		case "memory.openai_key":
			cfg.Memory.OpenAIKey = toString(v)
		case "memory.dimensions":
			if n, ok := toInt(v); ok && n >= 0 {
				cfg.Memory.Dimensions = n
			}
		case "memory.top_k":
			if n, ok := toInt(v); ok && n > 0 {
				cfg.Memory.TopK = n
			}
		case "memory.auto_save":
			val := toBool(v)
			cfg.Memory.AutoSave = &val
		case "memory.learnings_enabled":
			val := toBool(v)
			cfg.Memory.LearningsEnabled = &val
		case "memory.retention_days":
			if n, ok := toInt(v); ok && n >= 0 {
				cfg.Memory.RetentionDays = n
			}
		case "memory.session_awareness":
			val := toBool(v)
			cfg.Memory.SessionAwareness = &val
		case "platform_context.enabled":
			val := toBool(v)
			cfg.PlatformContext.Enabled = &val
		case "platform_context.note":
			if n := toString(v); len(n) <= config.PlatformContextNoteMaxLen {
				cfg.PlatformContext.Note = n
			}
		case "cross_session.enabled":
			val := toBool(v)
			cfg.CrossSession.Enabled = &val
		case "memory.session_broadcast":
			val := toBool(v)
			cfg.Memory.SessionBroadcast = &val
		case "memory.auto_hooks":
			val := toBool(v)
			cfg.Memory.AutoHooks = &val
		case "memory.hook_save_interval":
			if n, ok := toInt(v); ok && n > 0 {
				cfg.Memory.HookSaveInterval = n
			}
		case "memory.storage_mode":
			if s := toString(v); s == "summary" || s == "verbatim" {
				cfg.Memory.StorageMode = s
			}
		case "memory.entity_detection":
			cfg.Memory.EntityDetection = toBool(v)

		// Proxy resilience config
		case "proxy.enabled":
			cfg.Proxy.Enabled = toBool(v)
		case "proxy.health_interval":
			if n, ok := toInt(v); ok && n > 0 {
				cfg.Proxy.HealthInterval = n
			}
		case "proxy.request_timeout":
			if n, ok := toInt(v); ok && n > 0 {
				cfg.Proxy.RequestTimeout = n
			}
		case "proxy.offline_queue_size":
			if n, ok := toInt(v); ok && n > 0 {
				cfg.Proxy.OfflineQueueSize = n
			}
		case "proxy.circuit_breaker_threshold":
			if n, ok := toInt(v); ok && n > 0 {
				cfg.Proxy.CircuitBreakerThreshold = n
			}
		case "proxy.circuit_breaker_reset":
			if n, ok := toInt(v); ok && n > 0 {
				cfg.Proxy.CircuitBreakerReset = n
			}

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

		// BL297 v6.22.4 — Council subsystem.
		case "council.draft_retention_days":
			if n, ok := toInt(v); ok && n >= 0 {
				cfg.Council.DraftRetentionDays = n
			}
		case "council.llm_ref":
			if s, ok := v.(string); ok {
				cfg.Council.LLMRef = s
			}
		case "council.max_parallel":
			if n, ok := toInt(v); ok && n >= 0 {
				cfg.Council.MaxParallel = n
			}
		case "council.comm_firehose":
			cfg.Council.CommFirehose = toBool(v)

		// Detection patterns
		case "detection.prompt_patterns":
			if arr, ok := toStringArray(v); ok {
				cfg.Detection.PromptPatterns = arr
			}
		case "detection.completion_patterns":
			if arr, ok := toStringArray(v); ok {
				cfg.Detection.CompletionPatterns = arr
			}
		case "detection.rate_limit_patterns":
			if arr, ok := toStringArray(v); ok {
				cfg.Detection.RateLimitPatterns = arr
			}
		case "detection.input_needed_patterns":
			if arr, ok := toStringArray(v); ok {
				cfg.Detection.InputNeededPatterns = arr
			}
		case "detection.prompt_debounce":
			if n, ok := toInt(v); ok && n >= 0 {
				cfg.Detection.PromptDebounce = n
			}
		case "detection.notify_cooldown":
			if n, ok := toInt(v); ok && n >= 0 {
				cfg.Detection.NotifyCooldown = n
			}
		case "detection.alert_settle":
			if n, ok := toInt(v); ok && n >= 0 {
				cfg.Detection.AlertSettle = n
			}
		case "detection.alert_repeat":
			if n, ok := toInt(v); ok && n >= 0 {
				cfg.Detection.AlertRepeat = n
			}

		// Signal config
		case "signal.config_dir":
			if s := toString(v); s != "" {
				cfg.Signal.ConfigDir = s
			}
		case "signal.device_name":
			if s := toString(v); s != "" {
				cfg.Signal.DeviceName = s
			}
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
			if s := toString(v); s != "" {
				cfg.Aider.Binary = s
			}
		case "pi.enabled":
			cfg.Pi.Enabled = toBool(v)
		case "pi.binary":
			if s := toString(v); s != "" {
				cfg.Pi.Binary = s
			}
		case "pi.model":
			cfg.Pi.Model = toString(v)
		// pi.thinking_level — an empty string is a VALID value (clears the
		// flag → pi uses its own model default). Unknown levels are rejected
		// by handlePutConfig's pre-validation above (400), so if we get here
		// the string is either "" or one of config.PiThinkingLevels.
		case "pi.thinking_level":
			if s, ok := v.(string); ok && config.IsPiThinkingLevel(s) {
				cfg.Pi.ThinkingLevel = s
			}
		case "pi_prompt.enabled":
			cfg.PiPrompt.Enabled = toBool(v)
		case "pi_prompt.binary":
			if s := toString(v); s != "" {
				cfg.PiPrompt.Binary = s
			}
		case "pi_prompt.model":
			cfg.PiPrompt.Model = toString(v)
		case "goose.enabled":
			cfg.Goose.Enabled = toBool(v)
		case "goose.binary":
			if s := toString(v); s != "" {
				cfg.Goose.Binary = s
			}
		case "goose.provider":
			cfg.Goose.Provider = toString(v)
		case "goose.model":
			cfg.Goose.Model = toString(v)
		case "goose.api_key_ref":
			cfg.Goose.APIKeyRef = toString(v)
		case "goose.channel_enabled":
			cfg.Goose.ChannelEnabled = toBool(v)
		case "gemini.enabled":
			cfg.Gemini.Enabled = toBool(v)
		case "gemini.binary":
			if s := toString(v); s != "" {
				cfg.Gemini.Binary = s
			}
		case "ollama.enabled":
			cfg.Ollama.Enabled = toBool(v)
		case "ollama.model":
			if s := toString(v); s != "" {
				cfg.Ollama.Model = s
			}
		case "ollama.host":
			if s := toString(v); s != "" {
				cfg.Ollama.Host = s
			}
		case "opencode.enabled":
			cfg.OpenCode.Enabled = toBool(v)
		case "opencode.binary":
			if s := toString(v); s != "" {
				cfg.OpenCode.Binary = s
			}
		case "opencode.default_model":
			cfg.OpenCode.DefaultModel = toString(v)
		case "opencode.ollama_chunk_timeout_sec":
			if n, ok := toInt(v); ok {
				cfg.OpenCode.OllamaChunkTimeoutSec = n
			}
		case "opencode.ollama_header_timeout_sec":
			if n, ok := toInt(v); ok {
				cfg.OpenCode.OllamaHeaderTimeoutSec = n
			}
		case "opencode_acp.enabled":
			cfg.OpenCodeACP.Enabled = toBool(v)
		case "opencode_acp.binary":
			if s := toString(v); s != "" {
				cfg.OpenCodeACP.Binary = s
			}
		case "opencode_acp.acp_startup_timeout":
			if n, ok := toInt(v); ok {
				cfg.OpenCodeACP.ACPStartupTimeout = n
			}
		case "opencode_acp.acp_health_interval":
			if n, ok := toInt(v); ok {
				cfg.OpenCodeACP.ACPHealthInterval = n
			}
		case "opencode_acp.acp_message_timeout":
			if n, ok := toInt(v); ok {
				cfg.OpenCodeACP.ACPMessageTimeout = n
			}
		case "opencode_prompt.enabled":
			cfg.OpenCodePrompt.Enabled = toBool(v)
		case "opencode_prompt.binary":
			if s := toString(v); s != "" {
				cfg.OpenCodePrompt.Binary = s
			}
		case "openwebui.enabled":
			cfg.OpenWebUI.Enabled = toBool(v)
		case "openwebui.url":
			if s := toString(v); s != "" {
				cfg.OpenWebUI.URL = s
			}
		case "openwebui.model":
			if s := toString(v); s != "" {
				cfg.OpenWebUI.Model = s
			}
		case "openwebui.api_key":
			if s := toString(v); s != "" {
				cfg.OpenWebUI.APIKey = s
			}
		case "shell_backend.enabled", "shell.enabled":
			cfg.Shell.Enabled = toBool(v)
		case "shell_backend.script_path":
			cfg.Shell.ScriptPath = toString(v)

		// Per-LLM console size
		case "aider.console_cols":
			if n, ok := toInt(v); ok {
				cfg.Aider.ConsoleCols = n
			}
		case "aider.console_rows":
			if n, ok := toInt(v); ok {
				cfg.Aider.ConsoleRows = n
			}
		case "pi.console_cols":
			if n, ok := toInt(v); ok {
				cfg.Pi.ConsoleCols = n
			}
		case "pi.console_rows":
			if n, ok := toInt(v); ok {
				cfg.Pi.ConsoleRows = n
			}
		case "pi_prompt.console_cols":
			if n, ok := toInt(v); ok {
				cfg.PiPrompt.ConsoleCols = n
			}
		case "pi_prompt.console_rows":
			if n, ok := toInt(v); ok {
				cfg.PiPrompt.ConsoleRows = n
			}
		case "goose.console_cols":
			if n, ok := toInt(v); ok {
				cfg.Goose.ConsoleCols = n
			}
		case "goose.console_rows":
			if n, ok := toInt(v); ok {
				cfg.Goose.ConsoleRows = n
			}
		case "gemini.console_cols":
			if n, ok := toInt(v); ok {
				cfg.Gemini.ConsoleCols = n
			}
		case "gemini.console_rows":
			if n, ok := toInt(v); ok {
				cfg.Gemini.ConsoleRows = n
			}
		case "ollama.console_cols":
			if n, ok := toInt(v); ok {
				cfg.Ollama.ConsoleCols = n
			}
		case "ollama.console_rows":
			if n, ok := toInt(v); ok {
				cfg.Ollama.ConsoleRows = n
			}
		case "opencode.console_cols":
			if n, ok := toInt(v); ok {
				cfg.OpenCode.ConsoleCols = n
			}
		case "opencode.console_rows":
			if n, ok := toInt(v); ok {
				cfg.OpenCode.ConsoleRows = n
			}
		case "opencode_acp.console_cols":
			if n, ok := toInt(v); ok {
				cfg.OpenCodeACP.ConsoleCols = n
			}
		case "opencode_acp.console_rows":
			if n, ok := toInt(v); ok {
				cfg.OpenCodeACP.ConsoleRows = n
			}
		case "opencode_prompt.console_cols":
			if n, ok := toInt(v); ok {
				cfg.OpenCodePrompt.ConsoleCols = n
			}
		case "opencode_prompt.console_rows":
			if n, ok := toInt(v); ok {
				cfg.OpenCodePrompt.ConsoleRows = n
			}
		case "openwebui.console_cols":
			if n, ok := toInt(v); ok {
				cfg.OpenWebUI.ConsoleCols = n
			}
		case "openwebui.console_rows":
			if n, ok := toInt(v); ok {
				cfg.OpenWebUI.ConsoleRows = n
			}
		case "shell_backend.console_cols":
			if n, ok := toInt(v); ok {
				cfg.Shell.ConsoleCols = n
			}
		case "shell_backend.console_rows":
			if n, ok := toInt(v); ok {
				cfg.Shell.ConsoleRows = n
			}
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
		case "pi.output_mode":
			cfg.Pi.OutputMode = toString(v)
		case "pi_prompt.output_mode":
			cfg.PiPrompt.OutputMode = toString(v)
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
		case "pi.input_mode":
			cfg.Pi.InputMode = toString(v)
		case "pi_prompt.input_mode":
			cfg.PiPrompt.InputMode = toString(v)
		case "goose.input_mode":
			cfg.Goose.InputMode = toString(v)
		case "gemini.input_mode":
			cfg.Gemini.InputMode = toString(v)
		case "shell_backend.input_mode":
			cfg.Shell.InputMode = toString(v)
		// Profiles & Fallback
		case "session.fallback_chain":
			// v7.0.0: moved to LLM registry — handled by applyLLMRegistryPatch.
		// RTK config
		case "rtk.enabled":
			cfg.RTK.Enabled = toBool(v)
		case "rtk.binary":
			if s := toString(v); s != "" {
				cfg.RTK.Binary = s
			}
		case "rtk.show_savings":
			cfg.RTK.ShowSavings = toBool(v)
		case "rtk.auto_init":
			cfg.RTK.AutoInit = toBool(v)
		case "rtk.discover_interval":
			if n, ok := toInt(v); ok {
				cfg.RTK.DiscoverInterval = n
			}
		case "rtk.auto_update":
			cfg.RTK.AutoUpdate = toBool(v)
		case "rtk.update_check_interval":
			if n, ok := toInt(v); ok && n >= 0 {
				cfg.RTK.UpdateCheckInterval = n
			}
		case "pipeline.max_parallel":
			if n, ok := toInt(v); ok && n >= 0 {
				cfg.Pipeline.MaxParallel = n
			}
		case "pipeline.default_backend":
			cfg.Pipeline.DefaultBackend = toString(v)
		// BL372 — web search config
		case "web_search.enabled":
			cfg.WebSearch.Enabled = toBool(v)
		case "web_search.url":
			cfg.WebSearch.URL = toString(v)
		case "web_search.engine":
			if s := toString(v); s != "" {
				cfg.WebSearch.Engine = s
			}
		case "web_search.num_results":
			if n, ok := toInt(v); ok && n > 0 {
				cfg.WebSearch.NumResults = n
			}
		case "whisper.enabled":
			cfg.Whisper.Enabled = toBool(v)
		case "whisper.model":
			if s := toString(v); s != "" {
				cfg.Whisper.Model = s
			}
		case "whisper.language":
			cfg.Whisper.Language = toString(v)
		case "whisper.venv_path":
			if s := toString(v); s != "" {
				cfg.Whisper.VenvPath = s
			}
		case "whisper.chunk_seconds":
			// 0 disables chunking; whisper's own window is 30 s, so
			// anything from 1 to 29 is skipped, not stored.
			if n, ok := toInt(v); ok && (n == 0 || n >= 30) {
				cfg.Whisper.ChunkSeconds = n
			}
		// v4.0.8 (B38) — autonomous / plugins / orchestrator keys.
		// Without these cases the PWA + mobile-client save forms
		// for these sections silently no-op: the handler returns
		// 200 but nothing lands in config.yaml or the live Config.
		// See https://github.com/dmz006/datawatch/issues/19.
		case "autonomous.enabled":
			cfg.Autonomous.Enabled = toBool(v)
		case "autonomous.project_link_backfill_apply":
			cfg.Autonomous.ProjectLinkBackfillApply = toBool(v)
		// Planning routing switches; the rules, ladders and classes go
		// through PUT /api/autonomous/planning-routing.
		case "autonomous.planning_routing.enabled":
			cfg.Autonomous.PlanningRouting.Enabled = toBool(v)
		case "autonomous.planning_routing.shadow":
			cfg.Autonomous.PlanningRouting.Shadow = toBool(v)
		// Task routing table: the on switch, the active profile (it must
		// exist; a name that does not is skipped) and the whole block
		// (checked before it replaces the live one).
		case "autonomous.task_routing.enabled":
			cfg.Autonomous.TaskRouting.Enabled = toBool(v)
		case "autonomous.task_routing.active_profile":
			if name := toString(v); name != "" {
				if _, ok := cfg.Autonomous.TaskRouting.Profiles[name]; ok {
					cfg.Autonomous.TaskRouting.ActiveProfile = name
				} else {
					skipped = append(skipped, k)
				}
			}
		case "autonomous.task_routing":
			var in config.TaskRoutingConfig
			if b, err := json.Marshal(v); err == nil && json.Unmarshal(b, &in) == nil &&
				len(config.ValidateTaskRouting(in, autonomous.TaskRoutingChecksFor(nil))) == 0 {
				cfg.Autonomous.TaskRouting = in
			} else {
				skipped = append(skipped, k)
			}
		case "autonomous.poll_interval_seconds":
			if n, ok := toInt(v); ok && n >= 0 {
				cfg.Autonomous.PollIntervalSeconds = n
			}
		case "autonomous.max_parallel_tasks":
			// Deprecated: no executor code ever read it. Accepted and
			// IGNORED (never stored) so existing clients don't error;
			// handlePutConfig reports it under deprecated_fields. The
			// key itself is removed one release later.
		case "autonomous.max_concurrent_tasks":
			// 1 = sequential; values below 1 are skipped (config load
			// normalizes an unset value to 1 the same way).
			if n, ok := toInt(v); ok && n >= 1 {
				cfg.Autonomous.MaxConcurrentTasks = n
			}
			// B165 — boot-resume age limit. 0 (never resume) and -1 (no age
			// limit) are meaningful sentinels, so the threshold is >= -1,
			// not >= 0. Values below -1 are skipped, not stored (same
			// treatment as max_concurrent_tasks below 1).
		case "autonomous.resume_max_age_seconds":
			if n, ok := toInt(v); ok && n >= -1 {
				cfg.Autonomous.ResumeMaxAgeSeconds = n
			}
		// Attempt-cost settings: handlePutConfig ran these through the
		// shared config.ValidateAttemptCostSettings before this switch, so
		// the switch only stores what was allowed; an unreadable value is
		// left as-is (such a PUT would have been refused anyway).
		case "autonomous.first_attempt":
			if fa, ok := parseFirstAttempt(v); ok {
				cfg.Autonomous.FirstAttempt = fa
			}
		case "autonomous.first_attempt_escalate_after_failures":
			// 0 is meaningful: escalate on the first failure.
			if n, ok := toInt(v); ok {
				cfg.Autonomous.FirstAttemptEscalateAfterFailures = n
			}
		case "autonomous.retry_resume_max_context_tokens":
			// 0 is meaningful: always start a fresh session.
			if n, ok := toInt(v); ok {
				cfg.Autonomous.RetryResumeMaxContextTokens = n
			}
		case "autonomous.max_engines_per_node":
			// Clamped to 1 like config load does: values below 1 become 1
			// rather than being skipped, so the stored value is always valid.
			if n, ok := toInt(v); ok {
				if n < 1 {
					n = 1
				}
				cfg.Autonomous.MaxEnginesPerNode = n
			}
		// BL304: accept both new planning_* and legacy decomposition_* keys.
		case "autonomous.planning_backend", "autonomous.decomposition_backend":
			cfg.Autonomous.PlanningBackend = toString(v)
		case "autonomous.verification_backend":
			cfg.Autonomous.VerificationBackend = toString(v)
		// Found live 2026-10-11 with zero config surface at all (not
		// YAML, not here, not the daemon-startup bridge) despite
		// existing as a live field since BL203.
		case "autonomous.execution_backend":
			cfg.Autonomous.ExecutionBackend = toString(v)
		case "autonomous.verification_backends":
			if arr, ok := toStringArray(v); ok {
				cfg.Autonomous.VerificationBackends = arr
			} else if s, ok := v.(string); ok {
				cfg.Autonomous.VerificationBackends = splitCSV(s)
			}
		case "autonomous.judge_policy":
			// Empty clears it, so the judges follow the verifier routing policy again.
			if arr, ok := toStringArray(v); ok {
				cfg.Autonomous.JudgePolicy = config.NormalizeJudgePolicy(arr)
			} else if s, ok := v.(string); ok {
				cfg.Autonomous.JudgePolicy = config.NormalizeJudgePolicy(splitCSV(s))
			}
		case "autonomous.task_override_statuses":
			if arr, ok := toStringArray(v); ok {
				cfg.Autonomous.TaskOverrideStatuses = arr
			} else if s, ok := v.(string); ok {
				cfg.Autonomous.TaskOverrideStatuses = splitCSV(s)
			}
		// Worker session profile settings — the PUT /api/config path
		// pre-validates the combined state with validateWorkerProfilePatch,
		// so a direct applyConfigPatch caller's badly-shaped value is left
		// unchanged rather than stored.
		case "autonomous.worker_profiles":
			if m, ok := parseWorkerProfilesValue(v); ok {
				if len(m) == 0 {
					m = nil
				}
				cfg.Autonomous.WorkerProfiles = m
			}
		case "autonomous.worker_profile_defaults":
			if d, ok := parseWorkerProfileDefaultsValue(v); ok {
				cfg.Autonomous.WorkerProfileDefaults = d
			}
		case "autonomous.worker_profile":
			if s, ok := v.(string); ok {
				cfg.Autonomous.WorkerProfile = s
			}
		case "autonomous.override_queued_tasks":
			// Boolean: true enables overriding the LLM of a not-yet-started
			// task while the Automaton is at a locked (typically "running")
			// status; false restores the pre-change refusal. Only a real
			// boolean (or an "true"/"false" string, matching how comm-channel
			// scalars land here) changes the value; anything else leaves
			// the current setting alone.
			if b, ok := v.(bool); ok {
				cfg.Autonomous.OverrideQueuedTasks = &b
			} else if s, ok := v.(string); ok {
				if bs, err := strconv.ParseBool(strings.TrimSpace(s)); err == nil {
					cfg.Autonomous.OverrideQueuedTasks = &bs
				}
			}
		case "autonomous.snapshot_never_stage":
			// Path patterns the autonomous snapshot must never `git add`.
			// Same dual-form handling as task_override_statuses above: JSON
			// array first, comma-separated string fallback (the comm channel
			// sends CSV). Blank patterns are rejected earlier in
			// handlePutConfig; a CSV blank value lands as nil (unset → the
			// built-in defaults apply).
			if arr, ok := toStringArray(v); ok {
				trimmed := make([]string, 0, len(arr))
				for _, p := range arr {
					if t := strings.TrimSpace(p); t != "" {
						trimmed = append(trimmed, t)
					}
				}
				cfg.Autonomous.SnapshotNeverStage = trimmed
			} else if s, ok := v.(string); ok {
				cfg.Autonomous.SnapshotNeverStage = splitCSV(s)
			}
		case "autonomous.model_pull_consent":
			// Model-pull consent (allowed models + publisher allow-list).
			// handlePutConfig rejects a malformed block before this runs;
			// anything still unparseable here is reported as skipped rather
			// than stored — the zero value (allow nothing) is fail-closed.
			if b, err := json.Marshal(v); err == nil {
				var mpc config.ModelPullConsent
				if json.Unmarshal(b, &mpc) == nil {
					cfg.Autonomous.ModelPullConsent = mpc
				} else {
					skipped = append(skipped, k)
				}
			} else {
				skipped = append(skipped, k)
			}
		case "autonomous.tags.default_view", "autonomous.tags.attention_statuses",
			"autonomous.tags.default_match_mode", "autonomous.tags.direct_counts_visible",
			"autonomous.tags.registry_keeper", "autonomous.tags.registry_sync_seconds":
			// Validated in handlePutConfig before this runs; Set stores the
			// normalized value and leaves the block unchanged on a refusal.
			_ = cfg.Autonomous.Tags.Set(strings.TrimPrefix(k, config.TagSettingPrefix), v)
		case "autonomous.housekeeping_repairs.task_chain", "autonomous.housekeeping_repairs.advance_judge",
			"autonomous.housekeeping_repairs.lane_fill", "autonomous.housekeeping_repairs.leftovers",
			"autonomous.housekeeping_repairs.planning", "autonomous.housekeeping_repairs.unarchive",
			"autonomous.housekeeping_repairs.grace_seconds":
			// Validated in handlePutConfig before this runs. The Repairer reads
			// the block live through its Settings hook, so no restart is needed.
			applyHousekeepingRepairsKey(cfg, k, v)
		case "autonomous.auto_tags.enabled", "autonomous.auto_tags.automation_template",
			"autonomous.auto_tags.stage_template", "autonomous.auto_tags.kind_template",
			"autonomous.auto_tags.kind_labels":
			// Validated in handlePutConfig before this runs; Set stores the
			// normalized value and leaves the block unchanged on a refusal.
			if updated, ok := cfg.Autonomous.AutoTags.Set(strings.TrimPrefix(k, config.AutoTagsSettingPrefix), v); ok {
				cfg.Autonomous.AutoTags = updated
			}
		// autonomous.tags.placement: the placement tag axes (internal/
		// placementtags), per-key like the tag settings above.
		case "autonomous.tags.placement.enabled",
			"autonomous.tags.placement.compute.enabled", "autonomous.tags.placement.compute.template",
			"autonomous.tags.placement.peer.enabled", "autonomous.tags.placement.peer.template",
			"autonomous.tags.placement.backend.enabled", "autonomous.tags.placement.backend.template",
			"autonomous.tags.placement.lane.enabled", "autonomous.tags.placement.lane.template",
			"autonomous.tags.placement.role.enabled", "autonomous.tags.placement.role.template",
			"autonomous.tags.placement.project.enabled", "autonomous.tags.placement.project.template",
			"autonomous.tags.placement.automaton.enabled", "autonomous.tags.placement.automaton.template",
			"autonomous.tags.placement.automation.enabled", "autonomous.tags.placement.automation.template",
			"autonomous.tags.placement.stage.enabled", "autonomous.tags.placement.stage.template",
			"autonomous.tags.placement.kind.enabled", "autonomous.tags.placement.kind.template":
			_ = cfg.Autonomous.Tags.Set(strings.TrimPrefix(k, config.TagSettingPrefix), v)
		case "autonomous.plan_coverage_check":
			// off|warn|block plan-coverage gate (empty normalizes to
			// "block"). handlePutConfig rejects invalid modes loudly; this
			// re-normalizes defensively so a direct applyConfigPatch caller
			// still can't store a malformed value.
			if str, ok := v.(string); ok {
				if mode, err := config.NormalizePlanCoverageCheck(str); err == nil {
					cfg.Autonomous.PlanCoverageCheck = mode
				}
			}
		case "autonomous.plan_coverage_min_ratio":
			// Stored normalized into (0,1]; zero/negative and >1 both
			// clamp to the 1.0 safe default. handlePutConfig rejects
			// non-numeric values loudly before applyConfigPatch runs.
			if ratio, err := configPatchFloat(v); err == nil {
				cfg.Autonomous.PlanCoverageMinRatio = config.NormalizePlanCoverageRatio(ratio)
			}
		case "autonomous.plan_coverage_suppress_created_files":
			// Plan-coverage "file the plan itself creates" suppression
			// filter. Stored as an explicit pointer value: a nil pointer
			// would read back as ON (the default), so the written value
			// must be stored verbatim. handlePutConfig rejects non-bool
			// values before applyConfigPatch runs.
			if b, ok := v.(bool); ok {
				cfg.Autonomous.PlanCoverageSuppressCreatedFiles = &b
			}
		case "autonomous.plan_coverage_scoped_requirements":
			// Scoped requirement-extraction filter (default on when nil);
			// same explicit-pointer storage rule as the suppress filter.
			if b, ok := v.(bool); ok {
				cfg.Autonomous.PlanCoverageScopedRequirements = &b
			}
		case "autonomous.plan_coverage_requirement_sections":
			// Validated (string) in handlePutConfig; stored trimmed as
			// written — empty clears it back to the scope,constraints
			// default, and entries are normalized again by
			// EffectivePlanCoverageFilters at read time.
			if str, ok := v.(string); ok {
				cfg.Autonomous.PlanCoverageRequirementSections = strings.TrimSpace(str)
			}
		case "autonomous.plan_coverage_min_requirement_chars":
			// Validated (whole number >= 0) in handlePutConfig; 0 is
			// stored as 0 and reads back as the 30-character default.
			if n, ok := toInt(v); ok && n >= 0 {
				cfg.Autonomous.PlanCoverageMinRequirementChars = n
			}
		case "autonomous.generated_artefacts.change_record", "autonomous.generated_artefacts.gate_report",
			"autonomous.generated_artefacts.changelog_draft", "autonomous.generated_artefacts.parity_draft",
			"autonomous.generated_artefacts.tracker_rows", "autonomous.generated_artefacts.registry_row",
			"autonomous.generated_artefacts.combined_docs_task":
			// Validated (boolean) in handlePutConfig. Stored as an explicit
			// pointer so an explicit off survives; the manager reads the
			// live config through its settings provider, so no restart.
			if b, ok := v.(bool); ok {
				cfg.Autonomous.GeneratedArtefacts.Set(strings.TrimPrefix(k, "autonomous.generated_artefacts."), b)
			}
		case "autonomous.usage_backoff":
			// Validated in handlePutConfig; stored lower-cased and trimmed.
			if validateUsageBackoffMode(v) == nil {
				cfg.Autonomous.UsageBackoff = strings.ToLower(strings.TrimSpace(toString(v)))
			}
		case "autonomous.requirements":
			// Whole min-Ollama override block (capability → minimum version
			// map). Validated in handlePutConfig by the one shared rule set;
			// re-normalized defensively here so a direct applyConfigPatch
			// caller's bad value is left unchanged.
			if req, verr := validateRequirementsConfigBlock(v); verr == nil {
				cfg.Autonomous.Requirements = req
			}
		case "usage.calculator.variant", "usage.calculator.policy", "usage.calculator.safety_points", "usage.calculator.cost_per_task_points", "usage.calculator.baseline_per_24h", "usage.calculator.max_lanes", "usage.calculator.auto_lanes", "usage.calculator.auto_lanes_max", "usage.calculator.auto_lanes_special_priority", "usage.calculator.reset_weekday", "usage.calculator.reset_time", "usage.calculator.reset_zone", "usage.calculator.keeper":
			// Validated in handlePutConfig; stored normalized.
			applyUsageCalculatorPatchKey(&cfg.Usage.Calculator, k, v)
		case "classifier.timeout_seconds", "classifier.top_logprobs", "classifier.abstain_margin", "classifier.min_probability", "classifier.min_option_mass", "classifier.rotate_options", "classifier.thinking_off_models", "classifier.probe_ttl_seconds", "classifier.probe_timeout_seconds", "classifier.calibration.test_fraction", "classifier.calibration.split_seed", "classifier.calibration.min_fit_samples", "classifier.calibration.false_pass_weight", "classifier.calibration.rotation_bias_threshold", "classifier.calibration.rotation_flip_threshold":
			// Validated in handlePutConfig; stored normalized.
			applyClassifierPatchKey(&cfg.Classifier, k, v)
		case "usage.burn_guard.enabled", "usage.burn_guard.interval_minutes", "usage.burn_guard.margin_points", "usage.burn_guard.ladder", "usage.burn_guard.step_back", "usage.burn_guard.cache_write_points_per_million", "usage.burn_guard.cache_read_points_per_million", "usage.burn_guard.refit_on_reading", "usage.burn_guard.retry_limit_claude", "usage.burn_guard.ask_on_change", "usage.burn_guard.max_projection_gap_points":
			// Validated in handlePutConfig; stored normalized.
			applyBurnGuardPatchKey(&cfg.Usage.BurnGuard, k, v)
		case "autonomous.backoff_max_wait_minutes":
			if n, err := parseBackoffMaxWaitMinutes(v); err == nil {
				cfg.Autonomous.BackoffMaxWaitMinutes = n
			}
		case "autonomous.verifier_policy.mode", "autonomous.verifier_policy.backends",
			"autonomous.verifier_policy.claude_session_cap", "autonomous.verifier_policy.claude_cap_window_minutes",
			"autonomous.verifier_policy.force_stronger_on_retry", "autonomous.verifier_policy.final_gate_stronger",
			"autonomous.verifier_policy.stronger_backend", "autonomous.verifier_policy.stronger_model":
			// Validated in handlePutConfig; a value that fails the check here
			// is left unchanged.
			applyVerifierPolicyPatchKey(&cfg.Autonomous.VerifierPolicy, k, v)
		case "autonomous.host_load_alert.mode", "autonomous.host_load_alert.multiple",
			"autonomous.host_load_alert.minutes":
			// Validated in handlePutConfig; a value that fails the check here
			// is left unchanged.
			applyHostLoadAlertPatchKey(&cfg.Autonomous.HostLoadAlert, k, v)
		case "autonomous.automation.idle_alert_minutes":
			// 0 = off; negative values are skipped, not stored.
			if n, ok := toInt(v); ok && n >= 0 {
				cfg.Autonomous.Automation.IdleAlertMinutes = n
			}
		case "secrets.vault.unreachable_alert_after_seconds":
			// Validated above in handlePutConfig; an explicit 0 (alert off)
			// is preserved.
			if n, ok := toInt(v); ok && n >= 0 {
				cfg.Secrets.Vault.UnreachableAlertAfterSeconds = &n
			}
		case "autonomous.automation.triage_alert_minutes":
			// Validated above in handlePutConfig; a value that fails the
			// check here is left unchanged. n >= 0 is re-checked for safety;
			// an explicit 0 (0 = triage alert off) is preserved.
			if n, ok := toInt(v); ok && n >= 0 {
				cfg.Autonomous.Automation.TriageAlertMinutes = &n
			}
		case "autonomous.automation.child_inherit":
			// Validated above in handlePutConfig; a non-bool is left unchanged.
			if b, ok := v.(bool); ok {
				cfg.Autonomous.Automation.ChildInherit = &b
			}
		case "autonomous.automation.merge_pending_mode":
			// Validated above in handlePutConfig; a value that fails the
			// check here is left unchanged.
			if validateMergePendingMode(v) == nil {
				cfg.Autonomous.Automation.MergePendingMode = toString(v)
			}
		case "autonomous.automation.merge_pending_alert_minutes":
			// Validated above in handlePutConfig; n >= 0 is re-checked for
			// safety, and an explicit 0 (merge-pending alert off) is preserved.
			if n, ok := toInt(v); ok && n >= 0 {
				cfg.Autonomous.Automation.MergePendingAlertMinutes = &n
			}
		case "autonomous.automation.merged_member_policy":
			// Validated above in handlePutConfig; a value that fails the
			// check here is left unchanged.
			if validateMergedMemberPolicy(v) == nil {
				cfg.Autonomous.Automation.MergedMemberPolicy = toString(v)
			}
		case "autonomous.automation.agent_insert_policy":
			// Validated above in handlePutConfig; a value that fails the
			// check here is left unchanged.
			if validateAgentInsertPolicy(v) == nil {
				cfg.Autonomous.Automation.AgentInsertPolicy = toString(v)
			}
		case "autonomous.automation.create_search_mode":
			// Validated above in handlePutConfig; a value that fails the
			// check here is left unchanged.
			if validateCreateSearchModeValue(v) == nil {
				cfg.Autonomous.Automation.CreateSearchMode = toString(v)
			}
		case "autonomous.automation.create_search_threshold":
			// Validated above in handlePutConfig; a value that fails the
			// check here is left unchanged.
			if f, err := parseCreateSearchThreshold(v); err == nil {
				cfg.Autonomous.Automation.CreateSearchThreshold = &f
			}
		case "autonomous.automation.cleanup_mode":
			// String; stored as written — off | report | on are the intended
			// words and an empty/unknown one reads as report through
			// EffectiveCleanupMode.
			if validateCleanupModeValue(v) == nil {
				cfg.Autonomous.Automation.CleanupMode = toString(v)
			}
		case "autonomous.automation.cleanup_retention_hours":
			// Stored as written (a whole number, checked above); zero or a
			// negative value reads the 24-hour default through
			// EffectiveCleanupRetentionHours.
			if n, ok := toInt(v); ok {
				cfg.Autonomous.Automation.CleanupRetentionHours = n
			}
		case "autonomous.automation.cleanup_max_removals_per_pass":
			// Stored as written (a whole number, checked above); zero or a
			// negative value reads the 10 default through
			// EffectiveCleanupMaxRemovalsPerPass.
			if n, ok := toInt(v); ok {
				cfg.Autonomous.Automation.CleanupMaxRemovalsPerPass = n
			}
		case "autonomous.automation.disk_alert_percent":
			// Stored as written (a whole number, checked above); the accessor
			// surfaces it clamped to 1..100 (unset reads the 85 default).
			if n, ok := toInt(v); ok {
				cfg.Autonomous.Automation.DiskAlertPercent = n
			}
		case "autonomous.automation.uncommitted_work_policy":
			// Validated above in handlePutConfig; a value that fails the
			// check here is left unchanged. The change reaches the live
			// manager through the autonomous.* forward after the apply.
			if validateUncommittedWorkValue(k, v) == nil {
				cfg.Autonomous.Automation.UncommittedWorkPolicy = toString(v)
			}
		case "autonomous.automation.uncommitted_work_scope":
			if validateUncommittedWorkValue(k, v) == nil {
				cfg.Autonomous.Automation.UncommittedWorkScope = toString(v)
			}
		case protectedPathsKey:
			// Validated above in handlePutConfig. An empty list clears the key
			// back to the default list; a value that fails the check here is
			// left unchanged. The change reaches the live manager through the
			// autonomous.* forward after the apply.
			if list, err := parseProtectedPathsValue(v); err == nil {
				cfg.Autonomous.Automation.ProtectedPaths = list
			}
		case protectedPathsActionKey, rulesReviewSourceKey:
			if validateProtectedPathsWord(k, v) == nil {
				if k == rulesReviewSourceKey {
					cfg.Autonomous.Automation.RulesReviewSource = toString(v)
				} else {
					cfg.Autonomous.Automation.ProtectedPathsAction = toString(v)
				}
			}
		case config.PendingItemKeyPolicy, config.PendingItemKeyAutoResume, config.PendingItemKeyPath,
			config.PendingItemKeyIDPattern, config.PendingItemKeyIDColumn, config.PendingItemKeyTitleColumn,
			config.PendingItemKeyStateColumn, config.PendingItemKeyDoneValues, config.PendingItemKeyOnMissingRow:
			// Validated above in handlePutConfig; a value that fails the
			// check here is left unchanged. The change reaches the live
			// manager through the autonomous.* forward after the apply.
			applyPendingItemValue(&cfg.Autonomous.Automation, k, v)
		case "autonomous.automation.repo_ci_watch.mode",
			"autonomous.automation.repo_ci_watch.repos",
			"autonomous.automation.repo_ci_watch.poll_seconds",
			"autonomous.automation.repo_ci_watch.alert_minutes",
			"autonomous.automation.repo_ci_watch.tag_count",
			"autonomous.automation.repo_ci_watch.hung_after_minutes",
			"autonomous.automation.repo_ci_watch.security_workflows",
			"autonomous.automation.repo_ci_watch.delete_superseded":
			// Validated above in handlePutConfig; a value that fails the
			// check here is left unchanged.
			applyRepoCIWatchValue(&cfg.Autonomous.Automation.RepoCIWatch,
				strings.TrimPrefix(k, repoCIWatchKeyPrefix), v)
		case dependencyUpdateKeyPrefix + "enabled",
			dependencyUpdateKeyPrefix + "schedule",
			dependencyUpdateKeyPrefix + "max_majors":
			// Validated above in handlePutConfig; a value that fails the
			// check here is left unchanged.
			applyDependencyUpdateValue(&cfg.Autonomous.Automation.DependencyUpdate,
				strings.TrimPrefix(k, dependencyUpdateKeyPrefix), v)
		case "autonomous.automation.security_failure.mode":
			// Validated above in handlePutConfig; a value that fails the
			// check here is left unchanged.
			applySecurityFailureValue(&cfg.Autonomous.Automation.SecurityFailure, v)
		case supervisorWatchKeyPrefix + "mode",
			supervisorWatchKeyPrefix + "alert_minutes",
			supervisorWatchKeyPrefix + "silent_minutes",
			supervisorWatchKeyPrefix + "poll_seconds",
			supervisorWatchKeyPrefix + "script_output_path",
			supervisorWatchKeyPrefix + "tmux_restart_baseline",
			supervisorWatchKeyPrefix + "main_checkout",
			supervisorWatchKeyPrefix + "working_model":
			// Validated above in handlePutConfig; a value that fails the
			// check here is left unchanged.
			applySupervisorWatchValue(&cfg.Autonomous.Automation.SupervisorWatch,
				strings.TrimPrefix(k, supervisorWatchKeyPrefix), v)
		case laneFillKeyPrefix + "mode", laneFillKeyPrefix + "gpu_lanes", laneFillKeyPrefix + "claude_lanes",
			laneFillKeyPrefix + "idle_alert_minutes", laneFillKeyPrefix + "decompose_ahead_min",
			laneFillKeyPrefix + "poll_seconds", laneFillKeyPrefix + "gpu_backend",
			laneFillKeyPrefix + "gpu_model", laneFillKeyPrefix + "claude_backend",
			laneFillKeyPrefix + "count_verifier_wait", laneFillKeyPrefix + "stale_after_hours",
			laneFillKeyPrefix + "host_down_after_minutes",
			laneFillKeyPrefix + "require_confirmed_proposal", laneFillKeyPrefix + "check_main_carries",
			laneFillKeyPrefix + "require_worktree", laneFillKeyPrefix + "lane_counts_from_caps",
			laneFillKeyPrefix + "gpu_mode", laneFillKeyPrefix + "claude_mode":
			// Validated above in handlePutConfig; a value that fails the
			// check here is left unchanged.
			applyLaneFillValue(&cfg.Autonomous.LaneFill, strings.TrimPrefix(k, laneFillKeyPrefix), v)
		case mergeShadowKeyPrefix + "enabled", mergeShadowKeyPrefix + "gate_timeout",
			mergeShadowKeyPrefix + "plan_keep":
			// Validated above in handlePutConfig; a value that fails the
			// check here is left unchanged.
			applyMergeShadowValue(&cfg.Autonomous.MergeShadow, strings.TrimPrefix(k, mergeShadowKeyPrefix), v)
		case "autonomous.progress.lanes":
			// Validated in handlePutConfig; 0 = the default of 5.
			if n, err := parseProgressWhole("autonomous.progress.lanes", v); err == nil {
				cfg.Autonomous.Progress.Lanes = n
			}
		case "autonomous.progress.inactive_gap_hours":
			// Validated in handlePutConfig; 0 = the default of 3 hours.
			if f, err := parseProgressGapHours(v); err == nil {
				cfg.Autonomous.Progress.InactiveGapHours = f
			}
		case "autonomous.progress.backlog_path":
			// Validated in handlePutConfig; empty = docs/plans/README.md.
			if str, ok := v.(string); ok && config.ValidateProgressBacklogPath(str) == nil {
				cfg.Autonomous.Progress.BacklogPath = strings.TrimSpace(str)
			}
		case "autonomous.progress.since":
			// Validated in handlePutConfig; empty = the first record's creation date.
			if str, ok := v.(string); ok && config.ValidateProgressSince(str) == nil {
				cfg.Autonomous.Progress.Since = strings.TrimSpace(str)
			}
		case "autonomous.progress.snapshot_retention_days":
			// Validated in handlePutConfig; 0 = the default of 90 days.
			if n, err := parseProgressWhole("autonomous.progress.snapshot_retention_days", v); err == nil {
				cfg.Autonomous.Progress.SnapshotRetentionDays = n
			}
		case "pin_check_seconds":
			// Validated above in handlePutConfig; a value that fails the
			// check here is left unchanged.
			if n, err := parsePinCheckSeconds(v); err == nil {
				cfg.PinCheckSeconds = n
			}
		case "autonomous.plan_tier_max_inflight":
			// tier -> max-inflight table (autonomous.plan_tier_max_inflight).
			// Accepted shapes: a JSON object ({"pro":1}), the same object
			// arriving as a string (CLI `config set` and the comm-channel
			// `configure` verb quote their values), or `tier=limit,tier=limit`.
			// Unknown tier keys and negative limits are rejected loudly by
			// parsePlanTierTable; handlePutConfig runs the same check before
			// apply, so a bad value never reaches here in the REST path.
			if table, verr := parsePlanTierTable(v); verr == nil {
				cfg.Autonomous.PlanTierMaxInflight = table
			}
		case "autonomous.priority_by_kind":
			// kind -> capacity-priority table (autonomous.priority_by_kind).
			// Accepted shapes: a JSON object ({"bug":80}), the same object
			// arriving as a string (CLI `config set` and the comm-channel
			// `configure` verb quote their values), or `kind=priority` pairs.
			// Non-integer values and empty kind keys are rejected loudly by
			// ParsePriorityByKindValue; handlePutConfig runs the same check
			// before apply, so a bad value never reaches here. An empty table
			// is a valid clear (it round-trips: the field has no omitempty).
			if table, verr := autonomous.ParsePriorityByKindValue(v); verr == nil {
				cfg.Autonomous.PriorityByKind = table
			}
		case "autonomous.defect_intake_automation":
			// Automation name for new kind:bug filings; the empty string
			// clears it so filings are recorded, not guessed into an
			// Automation.
			if str, okS := v.(string); okS {
				cfg.Autonomous.DefectIntakeAutomation = strings.TrimSpace(str)
			}
		case planTierAllocationKey:
			// tier -> notional dollar allocation (whole table). Validated in
			// handlePutConfig; a value that fails the check here is left
			// unchanged.
			if table, verr := parsePlanTierAllocationTable(v); verr == nil {
				cfg.Autonomous.PlanTierAllocationUSD = table
			}
		case "autonomous.notify.default_events":
			// Default event kinds the built-in lifecycle sinks subscribe to.
			// Accept array or comma-separated string, same as
			// verification_backends above; an empty array really clears it
			// (no omitempty on the field, so the clear survives round-trip).
			if arr, ok := toStringArray(v); ok {
				cfg.Autonomous.Notify.DefaultEvents = arr
			} else if s, ok := v.(string); ok {
				cfg.Autonomous.Notify.DefaultEvents = splitCSV(s)
			}
		case "autonomous.notify.pending_events_max":
			// Per-queue retention for the mcp_queue sink; < 1 stores the
			// default. Takes effect on the next daemon start.
			if n, ok := toInt(v); ok {
				if n < 1 {
					n = config.DefaultPendingEventsMax
				}
				cfg.Autonomous.Notify.PendingEventsMax = n
			}
		case "autonomous.notify.webhook.enabled", "autonomous.notify.webhook.allow_hosts",
			"autonomous.notify.webhook.max_attempts", "autonomous.notify.webhook.backoff_seconds",
			"autonomous.notify.webhook.jitter_percent", "autonomous.notify.webhook.attempt_timeout_seconds",
			"autonomous.notify.webhook.max_body_bytes", "autonomous.notify.webhook.signature_tolerance_seconds",
			"autonomous.notify.forwarding.enabled", "autonomous.notify.forwarding.peer_events", "autonomous.notify.forwarding.max_hops",
			"autonomous.notify.forwarding.max_attempts", "autonomous.notify.forwarding.backoff_seconds",
			"autonomous.notify.forwarding.jitter_percent", "autonomous.notify.forwarding.attempt_timeout_seconds":
			// Validated in handlePutConfig (validateLifecycleNotifyPatch). The
			// webhook sink and peer forwarding read these live on every call,
			// so no restart is needed.
			applyLifecycleNotifyKey(&cfg.Autonomous.Notify, k, v)
		case "autonomous.checks.pre_start_verifier", "autonomous.checks.plan_rules",
			"autonomous.checks.after_done", "autonomous.checks.registry_readme",
			"autonomous.checks.block_when_missing", "autonomous.ship.push",
			"autonomous.ship.tag", "autonomous.ship.ci_watch", "autonomous.ship.tag_format",
			"autonomous.ship.remote", "autonomous.ship.ci_workflows":
			// Validated in handlePutConfig (validateChecksShipPatch).
			applyChecksShipKey(cfg, k, v)
		case "autonomous.questions.enabled":
			// Validated in handlePutConfig to a strict boolean (a non-boolean
			// value 400s before reaching here). The /api/questions handlers
			// read the live config on every call, so the change takes effect
			// immediately with no daemon restart.
			if b, ok := v.(bool); ok {
				cfg.Autonomous.Questions.Enabled = b
			}
		case "autonomous.questions.notify_messaging",
			"autonomous.questions.notify_alert",
			"autonomous.questions.return_path":
			// Validated in handlePutConfig to a strict boolean (a non-boolean
			// value 400s before reaching here). Stored as *bool so an explicit
			// false is distinguishable from an absent key (which stays on);
			// readers use the Effective* helpers.
			if b, ok := v.(bool); ok {
				switch k {
				case "autonomous.questions.notify_messaging":
					cfg.Autonomous.Questions.NotifyMessaging = &b
				case "autonomous.questions.notify_alert":
					cfg.Autonomous.Questions.NotifyAlert = &b
				case "autonomous.questions.return_path":
					cfg.Autonomous.Questions.ReturnPath = &b
				}
			}
		case "autonomous.questions.decision_gate":
			// Validated in handlePutConfig to a strict boolean. Stored as *bool
			// so an explicit false is distinguishable from an absent key
			// (which stays on). The gate reads the live config on every pass,
			// so no restart is needed.
			if b, ok := v.(bool); ok {
				cfg.Autonomous.Questions.DecisionGate = &b
			}
		case parallelKeyPrefix + "read_only_tasks",
			parallelKeyPrefix + "worktree_per_task",
			parallelKeyPrefix + "merge_conflict_policy",
			parallelKeyPrefix + "merge_conflict_model",
			parallelKeyPrefix + "merge_retry_seconds":
			// Validated above in handlePutConfig; a value that fails the
			// check here is left unchanged.
			applyParallelValue(&cfg.Autonomous.Parallel,
				strings.TrimPrefix(k, parallelKeyPrefix), v)
		case "autonomous.questions.worker_marker":
			// Validated in handlePutConfig to a strict boolean. Stored as *bool
			// so an explicit value is distinguishable from an absent key
			// (which stays off).
			if b, ok := v.(bool); ok {
				cfg.Autonomous.Questions.WorkerMarker = &b
			}
		case "autonomous.questions.decision_gate_mode":
			// Validated in handlePutConfig to shadow or on.
			if mode, ok := v.(string); ok {
				cfg.Autonomous.Questions.DecisionGateMode = mode
			}
		// autonomous.task_templates.mode — the whole {template id: mode}
		// object (llm, daemon, off). Validated in handlePutConfig against
		// the merged map by the shared load-time rule; a direct
		// applyConfigPatch caller's bad value is left unchanged.
		case "autonomous.task_templates.mode":
			if m, ok := v.(map[string]interface{}); ok {
				next := make(map[string]string, len(m))
				for id, mv := range m {
					if s, isStr := mv.(string); isStr {
						next[id] = s
					}
				}
				tc := config.TaskTemplatesConfig{Mode: next}
				if tc.Validate() == nil {
					cfg.Autonomous.TaskTemplates.Mode = next
				}
			}
		// The script gate time cap in whole seconds (0 = no cap). Validated
		// in handlePutConfig by the shared load-time rule; a value that
		// fails here is left unchanged.
		case "autonomous.script_gate_timeout_seconds":
			if n, verr := parseProgressWhole("autonomous.script_gate_timeout_seconds", v); verr == nil {
				cfg.Autonomous.ScriptGateTimeoutSeconds = n
			}
		// The gate drain wait in whole minutes (0 = do not wait). Validated
		// in handlePutConfig by config.ParseGateDrainMinutes.
		case "autonomous.gate_drain_minutes":
			if n, verr := config.ParseGateDrainMinutes(v); verr == nil {
				cfg.Autonomous.GateDrainMinutes = n
			}
		// The project gate command: a single line of shell text (empty =
		// unset). Validated in handlePutConfig by the shared load-time rule;
		// a value that fails here is left unchanged.
		case "autonomous.project_gate_command":
			if s, ok := v.(string); ok {
				if config.ValidateProjectGateCommand("autonomous", s) == nil {
					cfg.Autonomous.ProjectGateCommand = s
				}
			}
		case "autonomous.questions.held_mode":
			// Validated in handlePutConfig to off, shadow or on. The gate
			// reads the live config on every pass, so no restart is needed.
			if mode, ok := v.(string); ok {
				cfg.Autonomous.Questions.HeldMode = mode
			}
		case "autonomous.questions.held_states":
			// Validated in handlePutConfig to the five known state ids.
			// Accept an array or a comma-separated string, same as
			// default_events above; an explicit empty list really means
			// none (no omitempty on the field, so the clear survives
			// round-trip and an absent key keeps reading all five).
			if arr, ok := toStringArray(v); ok {
				cfg.Autonomous.Questions.HeldStates = arr
			} else if s, ok := v.(string); ok {
				cfg.Autonomous.Questions.HeldStates = splitCSV(s)
			}
		case "autonomous.questions.quality_mode":
			// Validated in handlePutConfig to off, warn or refuse. Stored as
			// *string so an absent key is distinguishable from an explicit one.
			if mode, ok := v.(string); ok {
				cfg.Autonomous.Questions.QualityMode = &mode
			}
		case "autonomous.questions.quality_tracker_id_pattern":
			// Validated in handlePutConfig to a pattern that compiles.
			if pat, ok := v.(string); ok {
				cfg.Autonomous.Questions.QualityTrackerIDPattern = &pat
			}
		case "autonomous.questions.quality_min_text_chars":
			// Validated in handlePutConfig to a whole number >= 0.
			if n, ok := toInt(v); ok {
				cfg.Autonomous.Questions.QualityMinTextChars = &n
			}
		case "autonomous.questions.queue_preview_lines":
			// Validated in handlePutConfig to a whole number >= 1.
			if n, ok := toInt(v); ok {
				cfg.Autonomous.Questions.QueuePreviewLines = &n
			}
		case "autonomous.decisions.enabled", "autonomous.decisions.folder", "autonomous.decisions.folder_glob", "autonomous.decisions.front_matter_delimiter", "autonomous.decisions.front_matter_separator", "autonomous.decisions.front_matter_field_separator", "autonomous.decisions.front_matter_keys", "autonomous.decisions.folder_withdraw_on_delete", "autonomous.decisions.clarity_mode", "autonomous.decisions.clarity_min_text_chars", "autonomous.decisions.clarity_require_tradeoffs", "autonomous.decisions.clarity_require_label", "autonomous.decisions.clarity_tracker_id_pattern", "autonomous.decisions.doc_root", "autonomous.decisions.doc_status_mode", "autonomous.decisions.doc_status_pattern", "autonomous.decisions.doc_status_template", "autonomous.decisions.follow_ups":
			// Validated in handlePutConfig (validateDecisionRecordPatch).
			applyDecisionRecordKey(&cfg.Autonomous.Decisions, k, v)
		case "autonomous.plan_records.enabled", "autonomous.plan_records.write_through", "autonomous.plan_records.import_conflict_policy":
			// Validated in handlePutConfig (validatePlanRecordsPatch).
			applyPlanRecordsKey(&cfg.Autonomous.PlanRecords, k, v)
		case "resource_limits.memory_share_percent":
			// Validated in handlePutConfig (validateResourceLimitsPatch).
			if n, ok := toInt(v); ok {
				cfg.ResourceLimits.MemorySharePercent = n
			}
		case "resource_limits.memory_floor_mb":
			if n, ok := toInt(v); ok {
				cfg.ResourceLimits.MemoryFloorMB = n
			}
		case "resource_limits.memory_cap_mb":
			if n, ok := toInt(v); ok {
				cfg.ResourceLimits.MemoryCapMB = n
			}
		case "identity.backups_to_keep":
			// Validated in handlePutConfig to a whole number >= 1.
			if n, ok := toInt(v); ok {
				cfg.Identity.BackupsToKeep = &n
			}
		case "session.dismiss_ui_prompts":
			// Bool (a string such as "true" is no different from 1 in JSON —
			// only a real boolean is accepted, matching session.skip_permissions).
			b, isBool := v.(bool)
			if isBool {
				cfg.Session.DismissUIPrompts = &b
			}
		case "session.ui_prompt_dismiss_delay_seconds":
			// Validated in handlePutConfig to a whole number >= 0.
			if n, ok := toInt(v); ok {
				cfg.Session.UIPromptDismissDelaySeconds = &n
			}
		case "session.ui_prompt_clear_bound_seconds":
			// Validated in handlePutConfig to a whole number >= 1.
			if n, ok := toInt(v); ok {
				cfg.Session.UIPromptClearBoundSeconds = &n
			}
		case "session.ui_prompt_max_attempts":
			// Validated in handlePutConfig to a whole number >= 1.
			if n, ok := toInt(v); ok {
				cfg.Session.UIPromptMaxAttempts = &n
			}
		case "ui.splash_min_seconds":
			// Validated in handlePutConfig to a whole number from 0 to 60
			// (0 = keep the legacy timing).
			if n, ok := toInt(v); ok {
				cfg.UI.SplashMinSeconds = &n
			}
		case "ui.splash_min_seconds_new_version":
			// Validated in handlePutConfig to a whole number from 0 to 60
			// (0 = keep the legacy timing).
			if n, ok := toInt(v); ok {
				cfg.UI.SplashMinSecondsNewVersion = &n
			}
		case "autonomous.planning_effort", "autonomous.decomposition_effort":
			cfg.Autonomous.PlanningEffort = toString(v)
		case "autonomous.verification_effort":
			cfg.Autonomous.VerificationEffort = toString(v)
		// v5.26.16 — operator-pinned model overrides. BL304: accept both new and legacy keys.
		case "autonomous.planning_model", "autonomous.decomposition_model":
			cfg.Autonomous.PlanningModel = toString(v)
		case "autonomous.verification_model":
			cfg.Autonomous.VerificationModel = toString(v)
		// v9.10.18 — pinned guardrail judge model (tier 3 of
		// ResolveGuardrailModel, internal/autonomous/guardrail_model.go).
		case "autonomous.guardrail_model":
			cfg.Autonomous.GuardrailModel = toString(v)
		case "autonomous.planning_timeout_seconds":
			if n, ok := toInt(v); ok && n >= 0 {
				cfg.Autonomous.PlanningTimeoutSeconds = n
			}
		case "autonomous.verification_timeout_seconds":
			// Stored as-is; zero/negative fall back to the default via
			// config.NormalizeVerificationTimeoutSeconds at read time.
			if n, ok := toInt(v); ok {
				cfg.Autonomous.VerificationTimeoutSeconds = n
			}
		case "autonomous.judge_timeout_seconds":
			// Stored as-is; zero/negative fall back to the 30m default via
			// config.NormalizeJudgeTimeoutSeconds at read time.
			if n, ok := toInt(v); ok {
				cfg.Autonomous.JudgeTimeoutSeconds = n
			}
		case "autonomous.judge_size_threshold_lines":
			// Stored as-is: 0 turns size-aware judge routing off. Negative and
			// fractional values are refused up front by
			// validateJudgeSizeThresholdPatch.
			if n, ok := toInt(v); ok && n >= 0 {
				cfg.Autonomous.JudgeSizeThresholdLines = n
			}
		case "autonomous.judge_prompt_file_threshold_lines":
			// Range checked up front by validateJudgePromptDeliveryPatch.
			if n, ok := toInt(v); ok && n >= 0 && n <= 100000 {
				cfg.Autonomous.JudgePromptFileThresholdLines = n
			}
		case "autonomous.judge_delivery_wait_per_thousand_lines_seconds":
			// Range checked up front by validateJudgePromptDeliveryPatch.
			if n, ok := toInt(v); ok && n >= 0 && n <= 3600 {
				cfg.Autonomous.JudgeDeliveryWaitPerThousandLinesSeconds = n
			}
		case "autonomous.judge_strong_backend":
			// Stored trimmed; empty clears the strong-first routing. Type
			// checked up front by validateJudgeStrongRoutingPatch.
			if str, ok := v.(string); ok {
				cfg.Autonomous.JudgeStrongBackend = strings.TrimSpace(str)
			}
		case "autonomous.judge_verdict_cache_enabled":
			// Stored as a pointer so an explicit false keeps meaning false
			// while an unset key reads the default (true) via
			// config.NormalizeJudgeVerdictCacheEnabled. Type-checked up front
			// by validateJudgeStrongRoutingPatch.
			if b, ok := v.(bool); ok {
				cfg.Autonomous.JudgeVerdictCacheEnabled = &b
			}
		case "autonomous.session_idle_timeout_seconds":
			// Stored as-is; zero falls back to the default via
			// config.NormalizeSessionIdleTimeoutSeconds at read time.
			if n, ok := toInt(v); ok && n >= 0 {
				cfg.Autonomous.SessionIdleTimeoutSeconds = n
			}
		case "autonomous.descendant_stall_seconds":
			// Stored as-is; zero falls back to the default via
			// config.NormalizeDescendantStallSeconds at read time.
			if n, ok := toInt(v); ok && n >= 0 {
				cfg.Autonomous.DescendantStallSeconds = n
			}
		case "autonomous.verifier_infra_escalation_threshold":
			// Zero falls back to the default via
			// config.NormalizeVerifierInfraEscalationThreshold at read time.
			if n, ok := toInt(v); ok && n >= 0 {
				cfg.Autonomous.VerifierInfraEscalationThreshold = n
			}
		case "autonomous.scope_breadth_enabled":
			cfg.Autonomous.ScopeBreadthEnabled = toBool(v)
		case "autonomous.verification_record_scores":
			b := toBool(v)
			cfg.Autonomous.VerificationRecordScores = &b
		case "autonomous.fallback_verification_backend":
			cfg.Autonomous.FallbackVerificationBackend = toString(v)
		case "autonomous.verifier_max_run_seconds":
			// Validated in handlePutConfig; 0 means no cap and is stored as zero.
			if n, ok := toInt(v); ok && config.ValidateVerifierRunCapSeconds(n) == nil {
				cfg.Autonomous.VerifierMaxRunSeconds = n
			}
		case "autonomous.verifier_no_progress_timeout_seconds":
			// Zero falls back to the default via
			// config.NormalizeVerifierNoProgressTimeoutSeconds at read time.
			if n, ok := toInt(v); ok && n >= 0 {
				cfg.Autonomous.VerifierNoProgressTimeoutSeconds = n
			}
		case "autonomous.worker_idle_nudge_seconds":
			// Zero falls back to the default via
			// config.NormalizeWorkerIdleNudgeSeconds at read time.
			if n, ok := toInt(v); ok && n >= 0 {
				cfg.Autonomous.WorkerIdleNudgeSeconds = n
			}
		case "autonomous.worker_idle_nudge_stall_seconds":
			// Zero falls back to the default via
			// config.NormalizeWorkerIdleNudgeStallSeconds at read time.
			if n, ok := toInt(v); ok && n >= 0 {
				cfg.Autonomous.WorkerIdleNudgeStallSeconds = n
			}
		case "autonomous.session_start_confirm_seconds":
			// Stored as-is; zero falls back to the default via
			// config.NormalizeSessionStartConfirmSeconds at read time.
			if n, ok := toInt(v); ok && n >= 0 {
				cfg.Autonomous.SessionStartConfirmSeconds = n
			}
		case "autonomous.orphan_grace_seconds":
			// Stored as-is; zero falls back to the default via
			// config.NormalizeOrphanGraceSeconds at read time.
			if n, ok := toInt(v); ok && n >= 0 {
				cfg.Autonomous.OrphanGraceSeconds = n
			}
		case "autonomous.restart_policy", "autonomous.restart_on_timeout",
			"autonomous.restart_quiet_timeout_seconds", "autonomous.planning_restart_policy":
			// Validated in handlePutConfig (validateRestartPolicyPatch).
			applyRestartPolicyKey(cfg, k, v)
		case "autonomous.duplicate_session_keep":
			// "older" (default) or "newer"; anything else is rejected.
			if str, ok := v.(string); ok {
				switch strings.ToLower(strings.TrimSpace(str)) {
				case config.DuplicateSessionKeepOlder, config.DuplicateSessionKeepNewer:
					cfg.Autonomous.DuplicateSessionKeep = config.NormalizeDuplicateSessionKeep(str)
				}
			}
		case "autonomous.stuck_running_seconds":
			// Stored as-is; zero falls back to the default via
			// config.NormalizeStuckRunningSeconds at read time.
			if n, ok := toInt(v); ok && n >= 0 {
				cfg.Autonomous.StuckRunningSeconds = n
			}
		// autonomous.loop_detect_turns — 0 is meaningful (loop detection
		// OFF) and must survive the no-omitempty round-trip as 0, not be
		// confused with "unset". A bare int would let 0 mean BOTH off and
		// unset, hence the *int (nil = unset → default 3 via
		// config.LoopDetectTurnsEffective). Negative values are rejected by
		// handlePutConfig's pre-validation above (400).
		case "autonomous.loop_detect_turns":
			if n, ok := toInt(v); ok && n >= 0 {
				nv := n
				cfg.Autonomous.LoopDetectTurns = &nv
			}
		case "autonomous.capacity_enabled":
			if b, ok := v.(bool); ok {
				cfg.Autonomous.CapacityEnabled = &b
			}
		case "autonomous.capacity_strict_node_limit":
			if b, ok := v.(bool); ok {
				cfg.Autonomous.CapacityStrictNodeLimit = &b
			}
		case "autonomous.capacity_count_pool_by_node":
			if b, ok := v.(bool); ok {
				cfg.Autonomous.CapacityCountPoolByNode = &b
			}
		case "autonomous.capacity_adopt_before_admit":
			if b, ok := v.(bool); ok {
				cfg.Autonomous.CapacityAdoptBeforeAdmit = &b
			}
		case "autonomous.capacity_adoption_wait_seconds":
			if n, ok := toInt(v); ok && n >= 0 {
				cfg.Autonomous.CapacityAdoptionWaitSeconds = n
			}
		case "autonomous.capacity_fallthrough":
			if b, ok := v.(bool); ok {
				cfg.Autonomous.CapacityFallthrough = b
			}
		case "autonomous.archive_grace_minutes":
			if n, err := config.ParseArchiveGraceMinutes(v); err == nil {
				cfg.Autonomous.ArchiveGraceMinutes = &n
			}
		case "autonomous.capacity_wait_timeout_seconds":
			if n, ok := toInt(v); ok && n >= 0 {
				cfg.Autonomous.CapacityWaitTimeoutSeconds = n
			}
		case "autonomous.detail_text_cap_bytes":
			if n, ok := toInt(v); ok {
				cfg.Autonomous.DetailTextCapBytes = n
			}
		case "autonomous.inherited_tags_cache_seconds":
			if n, ok := toInt(v); ok {
				cfg.Autonomous.InheritedTagsCacheSeconds = n
			}
		case "autonomous.ci_watch_timeout_seconds":
			if n, ok := toInt(v); ok && n >= 0 {
				cfg.Autonomous.CIWatchTimeoutSeconds = n
			}
		case "autonomous.capacity_gpu_util_pct":
			if n, ok := toInt(v); ok && n >= 0 && n <= 100 {
				cfg.Autonomous.CapacityGPUUtilPct = n
			}
		case "autonomous.capacity_rate_requests":
			if n, ok := toInt(v); ok && n >= 0 {
				cfg.Autonomous.CapacityRateRequests = n
			}
		case "autonomous.capacity_rate_tokens":
			if n, ok := toInt(v); ok && n >= 0 {
				cfg.Autonomous.CapacityRateTokens = n
			}
		case "autonomous.capacity_rate_window_seconds":
			if n, ok := toInt(v); ok && n >= 0 {
				cfg.Autonomous.CapacityRateWindowSeconds = n
			}
		case "session.reserved_interactive":
			if n, ok := toInt(v); ok && n >= 0 {
				cfg.Session.ReservedInteractive = &n
			}
		case "session.capacity_wait_seconds":
			if n, ok := toInt(v); ok && n >= 0 {
				cfg.Session.CapacityWaitSeconds = n
			}
		case "session.task_delivery_window_seconds":
			// Delivered through SessionConfig.EffectiveTaskDeliveryWindow, which the delivery
			// path reads live, so a patched value takes effect on the next one-shot task —
			// no restart needed.
			if n, ok := toInt(v); ok && n >= 1 {
				cfg.Session.TaskDeliveryWindowSeconds = n
			}
		case "session.task_delivery_attempts":
			// Delivered through SessionConfig.EffectiveTaskDeliveryAttempts, which the
			// delivery path reads live, so a patched value takes effect on the next one-shot
			// task — no restart needed.
			if n, ok := toInt(v); ok && n >= 1 {
				cfg.Session.TaskDeliveryAttempts = n
			}
		case "session.store_flush_interval":
			// Milliseconds; 0 writes through. Applied to the live store by the daemon's
			// config-apply hook, so no restart is needed.
			if n, ok := toInt(v); ok && n >= 0 {
				cfg.Session.StoreFlushInterval = &n
			}
		case "session.store_archive_after_days":
			if n, ok := toInt(v); ok && n >= 0 {
				cfg.Session.StoreArchiveAfterDays = &n
			}
		case "session.store_field_cap_kb":
			if n, ok := toInt(v); ok && n >= 1 {
				cfg.Session.StoreFieldCapKB = n
			}
		case "session.store_side_file_cap_kb":
			if n, ok := toInt(v); ok && n >= 1 {
				cfg.Session.StoreSideFileCapKB = n
			}
		case "autonomous.stale_task_seconds":
			if n, ok := toInt(v); ok && n >= 0 {
				cfg.Autonomous.StaleTaskSeconds = n
			}
		case "autonomous.task_max_attempt_seconds":
			if n, ok := toInt(v); ok && n >= 0 {
				cfg.Autonomous.TaskMaxAttemptSeconds = n
			}
		case "autonomous.task_no_progress_seconds":
			if n, ok := toInt(v); ok && n >= 0 {
				cfg.Autonomous.TaskNoProgressSeconds = n
			}
		case "autonomous.preserve_attempt_work":
			if b, ok := v.(bool); ok {
				cfg.Autonomous.PreserveAttemptWork = &b
			}
		case "autonomous.auto_fix_retries":
			if n, ok := toInt(v); ok && n >= 0 {
				cfg.Autonomous.AutoFixRetries = n
			}
		case "autonomous.verifier_diff_max_bytes":
			if n, ok := toInt(v); ok && n >= 0 {
				cfg.Autonomous.VerifierDiffMaxBytes = n
			}
		case "autonomous.verifier_min_free_mem_mb":
			// Unset (nil) = the default of 1024; an explicit 0 turns the check off.
			if n, ok := toInt(v); ok && n >= 0 {
				cfg.Autonomous.VerifierMinFreeMemMB = &n
			}
		case "autonomous.verifier_headroom_max_age_hours":
			// 0 or unset = the default of 24 hours.
			if n, ok := toInt(v); ok && n >= 0 {
				cfg.Autonomous.VerifierHeadroomMaxAgeHours = n
			}
		case "autonomous.security_scan":
			cfg.Autonomous.SecurityScan = toBool(v)
		case "autonomous.per_story_approval":
			// Phase 3 (v5.26.61) — toggles the per-story approval gate.
			// When set to true, PRD-level approval transitions stories
			// to "awaiting_approval"; the runner skips those until the
			// operator approves each via POST .../approve_story.
			cfg.Autonomous.PerStoryApproval = toBool(v)
		case branchRefreshEnabledKey, branchRefreshLimitKey:
			// Between-task branch refresh (branch_refresh_api.go): handlePutConfig
			// refuses a bad value first; one that reaches here is skipped.
			if _, applied := applyBranchRefreshConfigKey(cfg, k, v); !applied {
				skipped = append(skipped, k)
			}
		case "autonomous.continue_on_story_failure":
			// Daemon-wide default: false (halt) so a story failure stops
			// the PRD for operator re-edit/rerun instead of continuing
			// into later, independent stories. Per-PRD override via
			// POST .../set_continue_on_story_failure takes precedence.
			cfg.Autonomous.ContinueOnStoryFailure = toBool(v)
		case "autonomous.scope_lint":
			// Pre-spawn scope lint mode: off, warn or enforce. handlePutConfig
			// rejects anything else before this runs; an invalid value that
			// reaches here is reported as skipped rather than stored.
			mode, _ := v.(string)
			if config.ValidateScopeLint(mode) == nil {
				cfg.Autonomous.ScopeLint = mode
			} else {
				skipped = append(skipped, k)
			}
		case "autonomous.default_auto_pr":
			// BL407 Phase 5 -- daemon-wide default for a new PRD's
			// Git.AutoPR, applied once at creation time. Per-PRD
			// override via POST .../set_git takes precedence after
			// that (this is NOT resolved at use-time).
			cfg.Autonomous.DefaultAutoPR = toBool(v)
		// v5.17.0 — BL191 Q4 (recursion) + Q6 (guardrails) config
		// surface. Pre-v5.17.0 these keys silently no-op'd through
		// the PUT /api/config path because the case wasn't here.
		case "autonomous.push_and_tag":
			if b, ok := v.(bool); ok {
				cfg.Autonomous.PushAndTag = b
			} else if str, ok := v.(string); ok {
				if bs, err := strconv.ParseBool(strings.TrimSpace(str)); err == nil {
					cfg.Autonomous.PushAndTag = bs
				}
			}
		case "autonomous.release_path":
			// Pre-validated by the PUT handler.
			if str, ok := v.(string); ok && config.ValidateReleasePath(strings.TrimSpace(str)) == nil {
				cfg.Autonomous.ReleasePath = strings.TrimSpace(str)
			}
		case "git.release_ci_window_minutes":
			// Pre-validated by the PUT handler.
			if n, ok := toInt(v); ok && config.ValidateReleaseCIWindowMinutes(n) == nil {
				cfg.Git.ReleaseCIWindowMinutes = n
			}
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
		case "autonomous.per_prd_guardrails": // BL418
			if arr, ok := toStringArray(v); ok {
				cfg.Autonomous.PerPRDGuardrails = arr
			} else if s, ok := v.(string); ok {
				cfg.Autonomous.PerPRDGuardrails = splitCSV(s)
			}
		// BL410 Phase 1 — retry-policy config.
		case "autonomous.retry_policy":
			if obj, ok := v.(map[string]any); ok {
				m := make(map[string]int, len(obj))
				for k, raw := range obj {
					if n, ok := toInt(raw); ok {
						m[k] = n
					}
				}
				cfg.Autonomous.RetryPolicy = m
			}
		case "autonomous.retry_ceiling_total":
			if n, ok := toInt(v); ok {
				cfg.Autonomous.RetryCeilingTotal = n
			}
		case "autonomous.infra_retry_escalate_after":
			if n, ok := toInt(v); ok {
				cfg.Autonomous.InfraRetryEscalateAfter = n
			}
		case "autonomous.content_auto_fix_enabled":
			cfg.Autonomous.ContentAutoFixEnabled = toBool(v)
		case "autonomous.auto_fix_skip_approval":
			cfg.Autonomous.AutoFixSkipApproval = toBool(v)
		case "autonomous.block_on_file_drift":
			cfg.Autonomous.BlockOnFileDrift = toBool(v)
		// Verifier/quality-gate disagreement policy. "warn" (default)
		// records the disagreement and completes the task; "block" fails
		// it with a "gate_disagreement: …" error. Invalid values are
		// rejected upstream in handlePutConfig with a 400.
		case "autonomous.gate_disagreement":
			if gd := toString(v); gd == "warn" || gd == "block" {
				cfg.Autonomous.GateDisagreement = gd
			}
		// Plan review gate (plan_review_mode is pre-validated in
		// handlePutConfig) and smoke-verification reviewer override.
		case "autonomous.plan_review_backend":
			cfg.Autonomous.PlanReviewBackend = toString(v)
		case "autonomous.plan_review_model":
			cfg.Autonomous.PlanReviewModel = toString(v)
		case "autonomous.plan_review_mode":
			cfg.Autonomous.PlanReviewMode = toString(v)
		case "autonomous.smoke_verification_backend":
			cfg.Autonomous.SmokeVerificationBackend = toString(v)
		case "autonomous.smoke_verification_model":
			cfg.Autonomous.SmokeVerificationModel = toString(v)
		case "autonomous.drift_mode": // "block" (default) or "warn"; validated in handlePutConfig
			if mode := toString(v); config.ValidateDriftMode(mode) == nil {
				cfg.Autonomous.DriftMode = mode
			}
		case "autonomous.undeclared_edit_mode": // "off", "warn" (default) or "block"; validated in handlePutConfig
			if mode := toString(v); config.ValidateUndeclaredEditMode(mode) == nil {
				cfg.Autonomous.UndeclaredEditMode = strings.ToLower(strings.TrimSpace(mode))
			}
		// BL428 §3a ii — refuse tasks on a dirty tree.
		case "autonomous.refuse_dirty_tree_start":
			cfg.Autonomous.RefuseDirtyTreeStart = toBool(v)
		// BL406 — project-rules enforcement config.
		case "autonomous.rules_file":
			cfg.Autonomous.RulesFile = toString(v)
		case "autonomous.context_file":
			cfg.Autonomous.ContextFile = toString(v)
		// Per-project rules/context names + digest sub-configuration.
		// Validated in handlePutConfig (validateRulesContextPatch).
		case "autonomous.rules_context.rules_file":
			cfg.Autonomous.RulesContext.RulesFile = strings.TrimSpace(toString(v))
		case "autonomous.rules_context.context_files":
			if files, ok := contextFilesFromValue(v); ok {
				cfg.Autonomous.RulesContext.ContextFiles = files
			}
		case "autonomous.rules_context.digest_mode":
			cfg.Autonomous.RulesContext.DigestMode = strings.TrimSpace(toString(v))
		case "autonomous.rules_context.digest_max_bytes":
			if n, ok := toInt(v); ok {
				cfg.Autonomous.RulesContext.DigestMaxBytes = n
			}
		case "autonomous.rules_context":
			if raw, err := json.Marshal(v); err == nil {
				if uerr := json.Unmarshal(raw, &cfg.Autonomous.RulesContext); uerr != nil {
					// already rejected by validateRulesContextPatch
					cfg.Autonomous.RulesContext = config.AutonomousRulesContextConfig{}
				}
			} else {
				cfg.Autonomous.RulesContext = config.AutonomousRulesContextConfig{}
			}
		case "autonomous.rules_context.projects":
			if prjs := parseRulesContextProjectList(v); prjs != nil {
				cfg.Autonomous.RulesContext.Projects = prjs
			}
		// BL407 Phase 1 — local git-worktree isolation config.
		case "autonomous.worktree_base_repo":
			cfg.Autonomous.WorktreeBaseRepo = toString(v)
		case "autonomous.worktree_dir":
			cfg.Autonomous.WorktreeDir = toString(v)
		case "autonomous.upstream_repos":
			if arr, ok := v.([]any); ok {
				repos := make([]config.UpstreamRepoConfig, 0, len(arr))
				for _, item := range arr {
					m, ok := item.(map[string]any)
					if !ok {
						continue
					}
					repos = append(repos, config.UpstreamRepoConfig{
						Name:      toString(m["name"]),
						OwnerRepo: toString(m["owner_repo"]),
					})
				}
				cfg.Autonomous.UpstreamRepos = repos
			}
		// BL367 — default quality gate config for all PRDs.
		case "autonomous.default_quality_gates.enabled":
			cfg.Autonomous.DefaultQualityGates.Enabled = toBool(v)
		case "autonomous.default_quality_gates.test_command":
			cfg.Autonomous.DefaultQualityGates.TestCommand = toString(v)
		case "autonomous.default_quality_gates.timeout":
			if n, ok := toInt(v); ok && n >= 0 {
				cfg.Autonomous.DefaultQualityGates.Timeout = n
			}
		case "autonomous.default_quality_gates.block_on_regression":
			cfg.Autonomous.DefaultQualityGates.BlockOnRegression = toBool(v)
		// Gate scope (full | touched-plus-smoke | touched; pre-validated in
		// handlePutConfig) and smoke package patterns for the scopes that
		// use them. Accepts a JSON string list or a comma-separated string
		// (the comm/CLI wire form).
		case "autonomous.default_quality_gates.scope":
			cfg.Autonomous.DefaultQualityGates.Scope = toString(v)
		case "autonomous.default_quality_gates.smoke_packages":
			cfg.Autonomous.DefaultQualityGates.SmokePackages = parseGateSmokePackagesValue(v)
		// BL369 — prompt injection guard config.
		case "autonomous.injection_guard":
			cfg.Autonomous.InjectionGuard = toBool(v)
		case "autonomous.block_on_injection":
			cfg.Autonomous.BlockOnInjection = toBool(v)
		case "plugins.enabled":
			cfg.Plugins.Enabled = toBool(v)
		case "plugins.dir":
			cfg.Plugins.Dir = toString(v)
		case "plugins.timeout_ms":
			if n, ok := toInt(v); ok && n >= 0 {
				cfg.Plugins.TimeoutMs = n
			}
			// plugins.services.* — the service-plugin watch settings (the attach-only
			// probe cadence, the backlog-growth streak, the model-use thresholds),
			// validated as a block in handlePutConfig so a PUT can never store a
			// state Load would reject at next startup; the guards below are the
			// same rules applied key by key.
		case "plugins.services.health_interval_seconds":
			if n, ok := toInt(v); ok && n >= config.MinPluginServicesHealthInterval {
				cfg.Plugins.Services.HealthIntervalSeconds = n
			}
		case "plugins.services.backlog_growth_pulls":
			if n, ok := toInt(v); ok && n >= 1 {
				cfg.Plugins.Services.BacklogGrowthPulls = n
			}
		case "plugins.services.node_load_ratio":
			if f, err := configPatchFloat(v); err == nil && f > 0 {
				cfg.Plugins.Services.NodeLoadRatio = f
			}
		case "plugins.services.small_model_max_params_billions":
			if f, err := configPatchFloat(v); err == nil && f > 0 {
				cfg.Plugins.Services.SmallModelMaxParamsBillions = f
			}
		case "plugins.services.small_model_context_tokens":
			if n, ok := toInt(v); ok && n >= 1 {
				cfg.Plugins.Services.SmallModelContextTokens = n
			}
		case "plugins.services.per_service":
			if obj, ok := v.(map[string]interface{}); ok {
				for name, raw := range obj {
					if ov, verr := normalizePluginServiceOverride(name, raw); verr == nil {
						applyPluginServiceOverride(cfg, name, ov)
					}
				}
			}
		case "orchestrator.enabled":
			cfg.Orchestrator.Enabled = toBool(v)
		case "orchestrator.guardrail_backend":
			cfg.Orchestrator.GuardrailBackend = toString(v)
		case "orchestrator.guardrail_model":
			cfg.Orchestrator.GuardrailModel = toString(v)
		case "orchestrator.guardrail_timeout_ms":
			if n, ok := toInt(v); ok && n >= 0 {
				cfg.Orchestrator.GuardrailTimeoutMs = n
			}
		case "orchestrator.max_parallel_prds":
			if n, ok := toInt(v); ok && n >= 0 {
				cfg.Orchestrator.MaxParallelPRDs = n
			}
		// v5.21.0 — observer.* config-parity sweep. Pre-v5.21.0 every
		// observer.* key silently no-op'd through PUT /api/config because
		// applyConfigPatch had zero observer cases. Operators using
		// `datawatch config set observer.foo …` got 200 with no effect.
		case "observer.plugin_enabled":
			b := toBool(v)
			cfg.Observer.PluginEnabled = &b
		case "observer.tick_interval_ms":
			if n, ok := toInt(v); ok && n >= 0 {
				cfg.Observer.TickIntervalMs = n
			}
		case "observer.process_tree_enabled":
			b := toBool(v)
			cfg.Observer.ProcessTreeEnabled = &b
		case "observer.top_n_broadcast":
			if n, ok := toInt(v); ok && n >= 0 {
				cfg.Observer.TopNBroadcast = n
			}
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
			if n, ok := toInt(v); ok && n >= 0 {
				cfg.Observer.Federation.PushIntervalSeconds = n
			}
		case "observer.federation.token_path":
			cfg.Observer.Federation.TokenPath = toString(v)
		case "observer.federation.insecure":
			cfg.Observer.Federation.Insecure = toBool(v)
		case "observer.ollama_tap.endpoint":
			cfg.Observer.OllamaTap.Endpoint = toString(v)
		case "observer.peers.allow_register":
			cfg.Observer.Peers.AllowRegister = toBool(v)
		case "observer.peers.token_ttl_rotation_grace_s":
			if n, ok := toInt(v); ok && n >= 0 {
				cfg.Observer.Peers.TokenRotationGraceS = n
			}
		case "observer.peers.push_interval_seconds":
			if n, ok := toInt(v); ok && n >= 0 {
				cfg.Observer.Peers.PushIntervalSeconds = n
			}
		case "observer.peers.silent_alert_minutes":
			if n, ok := toInt(v); ok && n >= 0 {
				cfg.Observer.Peers.SilentAlertMinutes = n
			}
		case "observer.peers.listen_addr":
			cfg.Observer.Peers.ListenAddr = toString(v)
		// Observer pull protocol (pull mode) globals. A per-peer
		// interval/timeout of 0 falls back to these defaults, so they read
		// the code defaults (DefaultObserverPullIntervalS /
		// DefaultObserverPullTimeoutS) while unset; an interval below the
		// 5-second floor or a negative timeout is rejected (listed in
		// "skipped") to match config.ValidateObserverPullConfig.
		case "observer.pull.enabled":
			cfg.Observer.Pull.Enabled = toBool(v)
		case "observer.pull.default_interval_s":
			if n, ok := toInt(v); ok && n >= config.ObserverPullMinIntervalS {
				cfg.Observer.Pull.DefaultIntervalS = n
			} else {
				skipped = append(skipped, k)
			}
		case "observer.pull.default_timeout_s":
			if n, ok := toInt(v); ok && n >= 0 {
				cfg.Observer.Pull.DefaultTimeoutS = n
			} else {
				skipped = append(skipped, k)
			}
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
		// BL368 — vision image description config keys.
		case "vision.enabled":
			cfg.Vision.Enabled = toBool(v)
		case "vision.backend":
			cfg.Vision.Backend = toString(v)
		case "vision.endpoint":
			cfg.Vision.Endpoint = toString(v)
		case "vision.api_key":
			cfg.Vision.APIKey = toString(v)
		case "vision.model":
			if s := toString(v); s != "" {
				cfg.Vision.Model = s
			}
		case "vision.default_prompt":
			cfg.Vision.DefaultPrompt = toString(v)
		// Image-describe request timeout in seconds. Unset or below 1 is
		// rejected here; the getter always reports the effective value
		// (default 5, min 1) — same rule as results.schedule_history_limit.
		case "vision.describe_timeout_seconds":
			if n, ok := toInt(v); ok && n >= 1 {
				cfg.Vision.DescribeTimeoutSeconds = n
			}
		case "session.quick_commands":
			if cmds, ok := toQuickCommands(v); ok {
				cfg.Session.QuickCommands = cmds
			}
		// Results — scheduled-jobs per-schedule result history. Unset or
		// below 1 is rejected here; the getter always reports the
		// effective value (default 20, min 1).
		case "results.schedule_history_limit":
			if n, ok := toInt(v); ok && n >= 1 {
				cfg.Results.ScheduleHistoryLimit = n
			}
		default:
			// autonomous.defect_queue.*; validated in handlePutConfig, Set
			// leaves the block unchanged on a refusal.
			if name, ok := strings.CutPrefix(k, config.DefectQueueSettingPrefix); ok {
				_ = cfg.Autonomous.DefectQueue.Set(name, v)
				continue
			}
			// autonomous.failure_escalation.{enabled,max_failures,steps}; the
			// values were checked by validateFailureEscalationPatch.
			if applyFailureEscalationPatch(cfg, k, v) {
				continue
			}
			// autonomous.infra_retry.*; the values were checked by
			// validateInfraRetryPatch.
			if applyInfraRetryPatch(cfg, k, v) {
				continue
			}
			// autonomous.merge_pipeline.*; the values were checked by
			// validateMergePipelinePatch. The merge run reads the live block,
			// so the change applies with no restart.
			if applyMergePipelinePatch(cfg, k, v) {
				continue
			}
			// autonomous.request_watch.*; the values were checked by
			// validateRequestWatchPatch. The watcher reads the live block on
			// every tick, so the change applies with no restart.
			if applyRequestWatchPatch(cfg, k, v) {
				continue
			}
			// usage.billing.<provider>.<field>; the values were checked by
			// validateBillingPatch. The cost report and the usage calculator
			// read the live block, so the change applies with no restart.
			if applyBillingPatchKey(cfg, k, v) {
				continue
			}
			// autonomous.model_routing.refusal_mode,
			// .served_models_cache_seconds, autonomous.model_routing.verifier_follows_worker_node,
			// .unknown_node_policy, .probe_timeout_seconds and .alert_on_refusal;
			// the values were checked by validateModelRoutingPatch.
			if applyModelRoutingPatch(cfg, k, v) {
				continue
			}

			// compute_placement.* — already validated in
			// validateComputePlacementPatch. Placement reads the live
			// block on every decision, so the new value applies with
			// no restart.
			if applyComputePlacementPatch(cfg, k, v) {
				continue
			}
			// Single-tier pair form: autonomous.plan_tier_allocation_usd.<tier>
			// sets one tier and leaves the rest of the table alone.
			if tier, ok := planTierAllocationPairTier(k); ok {
				if usd, verr := parsePlanTierAllocationPair(tier, v); verr == nil {
					if cfg.Autonomous.PlanTierAllocationUSD == nil {
						cfg.Autonomous.PlanTierAllocationUSD = map[string]float64{}
					}
					cfg.Autonomous.PlanTierAllocationUSD[tier] = usd
				}
				continue
			}
			// Unknown keys are logged so future mobile/PWA schema
			// drift surfaces instead of silent no-op'ing again.
			// Clients still get 200 for now — returning 4xx would
			// break existing saves that mix known+unknown keys;
			// a follow-up (issue #19 Option C) can add a stricter
			// mode behind a flag.
			fmt.Fprintf(os.Stderr, "[config] applyConfigPatch: unknown key %q (no-op)\n", k)
		}
	}
	return skipped
}

// SetTestMessageHandler wires a function that routes simulated messages through
// the router. Used by POST /api/test/message for comm channel testing. The
// handler receives the request's TestMessageAttribution (calling credential +
// config:write decision) so a session-attributed configure is audited once
// (surface "comm") and refused without the capability.
func (s *Server) SetTestMessageHandler(fn func(text string, at TestMessageAttribution) []string) {
	s.testMessageHandler = fn
}

// handleTestMessage simulates an incoming messaging backend message.
// POST /api/test/message { "text": "help" }
// Returns the responses the router would send back.
func (s *Server) handleTestMessage(w http.ResponseWriter, r *http.Request) {
	if !s.fedCap(w, r, federation.CapSessionsList) {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	if s.testMessageHandler == nil {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "test message handler not wired"})
		return
	}
	var req struct {
		Text string `json:"text"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Text == "" {
		http.Error(w, "need {\"text\":\"...\"}", http.StatusBadRequest)
		return
	}
	responses := s.testMessageHandler(req.Text, s.configureWriteAttribution(r))
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"input":     req.Text,
		"responses": responses,
		"count":     len(responses),
	})
}

// effectiveDriftMode renders the configured autonomous.drift_mode the way
// the executor enforces it, so GET /api/config shows the effective mode:
// an unset/empty value renders as "block" (matching
// resolveDriftMode in internal/autonomous/file_drift.go), as does any
// "block" spelling, and "warn" stays "warn".
func effectiveDriftMode(mode string) string {
	if strings.EqualFold(strings.TrimSpace(mode), "warn") {
		return "warn"
	}
	return "block"
}

// effectiveUndeclaredEditMode renders autonomous.undeclared_edit_mode the
// way the executor resolves it (Manager.EffectiveUndeclaredEditMode):
// lowercased off|warn|block, anything else renders "warn".
func effectiveUndeclaredEditMode(mode string) string {
	switch m := strings.ToLower(strings.TrimSpace(mode)); m {
	case "off", "warn", "block":
		return m
	}
	return "warn"
}

// parseGateSmokePackagesValue accepts a JSON string list or a
// comma-separated string (the comm/CLI wire form) and returns the trimmed,
// non-empty entries. Anything else yields nil ("use the default smoke
// set").
func parseGateSmokePackagesValue(v interface{}) []string {
	switch val := v.(type) {
	case []interface{}:
		out := []string{}
		for _, item := range val {
			if s := strings.TrimSpace(toString(item)); s != "" {
				out = append(out, s)
			}
		}
		return out
	case string:
		out := []string{}
		for _, part := range strings.Split(val, ",") {
			if p := strings.TrimSpace(part); p != "" {
				out = append(out, p)
			}
		}
		return out
	}
	return nil
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

// effectivePlanCoverageCheckForDisplay renders the plan-coverage
// check mode for the /api/config display surface: the unset state
// (empty) shows its safe default "block", a valid raw value passes
// through, and a malformed raw value (written via raw YAML) is shown
// as-is rather than turning a config read into an error.
func effectivePlanCoverageCheckForDisplay(raw string) string {
	mode, err := config.NormalizePlanCoverageCheck(raw)
	if err != nil {
		return raw
	}
	return mode
}

// judgePolicyForDisplay renders autonomous.judge_policy as a list even when
// unset, so the field reads as an empty array (follow the verifier policy)
// rather than null.
func judgePolicyForDisplay(backends []string) []string {
	return config.NormalizeJudgePolicy(append([]string{}, backends...))
}

// configPatchFloat coerces a /api/config patch value for
// autonomous.plan_coverage_min_ratio to a float64. Accepts a JSON
// number and a numeric string (the CLI and comm channel commonly send
// strings); booleans and unparseable values are errors. NaN/±Inf are
// rejected because they would defeat the (0,1] clamping.
// pluginServicesSurface renders plugins.services for GET /api/config: each
// field as stored (0 when unset; the Effective* accessors apply the
// documented defaults on the reading side, and the reference documents the
// defaults themselves).
func pluginServicesSurface(cfg *config.Config) map[string]interface{} {
	svc := cfg.Plugins.Services
	per := map[string]interface{}{}
	for name, ov := range svc.PerService {
		per[name] = map[string]interface{}{"lifecycle_mode": ov.LifecycleMode}
	}
	return map[string]interface{}{
		"health_interval_seconds":         svc.HealthIntervalSeconds,
		"backlog_growth_pulls":            svc.BacklogGrowthPulls,
		"node_load_ratio":                 svc.NodeLoadRatio,
		"small_model_max_params_billions": svc.SmallModelMaxParamsBillions,
		"small_model_context_tokens":      svc.SmallModelContextTokens,
		"per_service":                     per,
	}
}

// applyPluginServiceOverride merges one validated per-service entry into the
// config (the map is created on first write, like the other dynamic maps).
func applyPluginServiceOverride(cfg *config.Config, service string, ov config.PluginServiceOverride) {
	if cfg.Plugins.Services.PerService == nil {
		cfg.Plugins.Services.PerService = map[string]config.PluginServiceOverride{}
	}
	cfg.Plugins.Services.PerService[service] = ov
}

// normalizePluginServiceOverride accepts a per_service value as a mode
// string or as an object with a lifecycle_mode member, lower-cases and
// trims it, and checks it against the load-time rule: "" (unset = the only
// supported mode, or attach_only when the manifest supports both),
// attach_only, or managed. A mis-spelled mode is rejected naming the
// service and listing the valid values.
func normalizePluginServiceOverride(service string, v interface{}) (config.PluginServiceOverride, error) {
	mode := ""
	switch t := v.(type) {
	case string:
		mode = t
	case map[string]interface{}:
		if raw, ok := t["lifecycle_mode"]; ok {
			mode = toString(raw)
		}
	}
	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode != "" && mode != config.PluginServiceLifecycleAttachOnly && mode != config.PluginServiceLifecycleManaged {
		return config.PluginServiceOverride{}, config.ValidatePluginServiceMode(service, mode)
	}
	return config.PluginServiceOverride{LifecycleMode: mode}, nil
}

// validatePluginServicesPatch simulates the patch's plugins.services.* keys
// on a copy of the stored block and runs the same validator Load runs
// (config.PluginServicesConfig.Validate) over the result — so a single PUT
// cannot store a block the next startup would reject. Reports the first
// violation; keys are visited in sorted order.
func validatePluginServicesPatch(current config.PluginServicesConfig, patch map[string]interface{}) error {
	next := current
	if next.PerService == nil {
		next.PerService = map[string]config.PluginServiceOverride{}
	}
	keys := make([]string, 0, len(patch))
	for k := range patch {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		v := patch[k]
		switch {
		case k == "plugins.services.health_interval_seconds":
			if n, ok := toInt(v); ok {
				next.HealthIntervalSeconds = n
			}
		case k == "plugins.services.backlog_growth_pulls":
			if n, ok := toInt(v); ok {
				next.BacklogGrowthPulls = n
			}
		case k == "plugins.services.node_load_ratio":
			if f, err := configPatchFloat(v); err == nil {
				next.NodeLoadRatio = f
			}
		case k == "plugins.services.small_model_max_params_billions":
			if f, err := configPatchFloat(v); err == nil {
				next.SmallModelMaxParamsBillions = f
			}
		case k == "plugins.services.small_model_context_tokens":
			if n, ok := toInt(v); ok {
				next.SmallModelContextTokens = n
			}
		case k == "plugins.services.per_service":
			if obj, ok := v.(map[string]interface{}); ok {
				for name, raw := range obj {
					ov, verr := normalizePluginServiceOverride(name, raw)
					if verr != nil {
						return fmt.Errorf("plugins.services: %v", verr)
					}
					next.PerService[name] = ov
				}
			}
		case strings.HasPrefix(k, "plugins.services.per_service."):
			name, _ := strings.CutPrefix(k, "plugins.services.per_service.")
			if name == "" {
				continue
			}
			ov, verr := normalizePluginServiceOverride(name, v)
			if verr != nil {
				return fmt.Errorf("plugins.services: %v", verr)
			}
			next.PerService[name] = ov
		}
	}
	return next.Validate()
}

func configPatchFloat(v interface{}) (float64, error) {
	rejectNonFinite := func(f float64, lit string) error {
		if math.IsNaN(f) || math.IsInf(f, 0) {
			return fmt.Errorf("invalid plan_coverage_min_ratio %s: must be a finite number in (0,1]", lit)
		}
		return nil
	}
	switch n := v.(type) {
	case float64:
		if err := rejectNonFinite(n, fmt.Sprintf("%v", v)); err != nil {
			return 0, err
		}
		return n, nil
	case json.Number:
		f, err := n.Float64()
		if err != nil {
			return 0, fmt.Errorf("invalid plan_coverage_min_ratio %s: %v", n, err)
		}
		if err := rejectNonFinite(f, n.String()); err != nil {
			return 0, err
		}
		return f, nil
	case bool:
		return 0, fmt.Errorf("invalid plan_coverage_min_ratio %v: must be a number in (0,1]", v)
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(n), 64)
		if err != nil {
			return 0, fmt.Errorf("invalid plan_coverage_min_ratio %q: must be a number in (0,1]", n)
		}
		if err := rejectNonFinite(f, n); err != nil {
			return 0, err
		}
		return f, nil
	default:
		return 0, fmt.Errorf("invalid plan_coverage_min_ratio %v: must be a number in (0,1]", v)
	}
}

func toString(v interface{}) string {
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", v)
}

// parsePinCheckSeconds reads pin_check_seconds from a PUT /api/config
// body. It accepts a JSON number, a numeric string (the CLI and comm
// channel send strings), and must be a whole number >= 0 (0 = keeper off).
func parsePinCheckSeconds(v interface{}) (int, error) {
	const msg = "pin_check_seconds: must be a whole number of seconds >= 0 (0 = pin keeper off)"
	var n int
	switch x := v.(type) {
	case float64:
		if x != math.Trunc(x) || x > math.MaxInt32 {
			return 0, errors.New(msg)
		}
		n = int(x)
	case int:
		n = x
	case string:
		i, err := strconv.Atoi(strings.TrimSpace(x))
		if err != nil {
			return 0, errors.New(msg)
		}
		n = i
	default:
		return 0, errors.New(msg)
	}
	if n < 0 {
		return 0, errors.New(msg)
	}
	return n, nil
}

// parseProgressWhole reads a whole-number autonomous.progress.* value from a
// PUT /api/config body. It accepts a JSON number and a numeric string (the CLI
// and comm channel send strings); the value must be >= 0 (0 = the default).
func parseProgressWhole(key string, v interface{}) (int, error) {
	msg := key + ": must be a whole number >= 0 (0 = default)"
	switch x := v.(type) {
	case float64:
		if x != math.Trunc(x) || x < 0 || x > math.MaxInt32 {
			return 0, errors.New(msg)
		}
		return int(x), nil
	case int:
		if x < 0 || x > math.MaxInt32 {
			return 0, errors.New(msg)
		}
		return x, nil
	case string:
		if strings.TrimSpace(x) == "" {
			return 0, nil // a cleared Settings field means the default
		}
		n, err := strconv.Atoi(strings.TrimSpace(x))
		if err != nil || n < 0 || n > math.MaxInt32 {
			return 0, errors.New(msg)
		}
		return n, nil
	}
	return 0, errors.New(msg)
}

// parseProgressGapHours reads autonomous.progress.inactive_gap_hours: a finite
// number of hours >= 0 (0 = the default), as a JSON number or numeric string.
func parseProgressGapHours(v interface{}) (float64, error) {
	const msg = "autonomous.progress.inactive_gap_hours: must be a number of hours >= 0 (0 = default)"
	var f float64
	switch x := v.(type) {
	case float64:
		f = x
	case int:
		f = float64(x)
	case string:
		if strings.TrimSpace(x) == "" {
			return 0, nil // a cleared Settings field means the default
		}
		parsed, err := strconv.ParseFloat(strings.TrimSpace(x), 64)
		if err != nil {
			return 0, errors.New(msg)
		}
		f = parsed
	default:
		return 0, errors.New(msg)
	}
	if math.IsNaN(f) || math.IsInf(f, 0) || f < 0 || f > 24*366 {
		return 0, errors.New(msg)
	}
	return f, nil
}

// validateProgressPatch checks every autonomous.progress.* key present in a
// PUT /api/config body and returns the first problem.
func validateProgressPatch(patch map[string]interface{}) error {
	if v, ok := patch["autonomous.progress.lanes"]; ok {
		if _, err := parseProgressWhole("autonomous.progress.lanes", v); err != nil {
			return err
		}
	}
	if v, ok := patch["autonomous.progress.inactive_gap_hours"]; ok {
		if _, err := parseProgressGapHours(v); err != nil {
			return err
		}
	}
	if v, ok := patch["autonomous.progress.snapshot_retention_days"]; ok {
		if _, err := parseProgressWhole("autonomous.progress.snapshot_retention_days", v); err != nil {
			return err
		}
	}
	if v, ok := patch["autonomous.progress.backlog_path"]; ok {
		str, isStr := v.(string)
		if !isStr {
			return errors.New("autonomous.progress.backlog_path: must be a string (empty = docs/plans/README.md)")
		}
		if err := config.ValidateProgressBacklogPath(str); err != nil {
			return err
		}
	}
	if v, ok := patch["autonomous.progress.since"]; ok {
		str, isStr := v.(string)
		if !isStr {
			return errors.New("autonomous.progress.since: must be a YYYY-MM-DD date string (empty = first record's creation date)")
		}
		if err := config.ValidateProgressSince(str); err != nil {
			return err
		}
	}
	return nil
}

// validateIdleAlertMinutes checks autonomous.automation.idle_alert_minutes:
// a whole number >= 0 (0 turns the idle alert off).
func validateIdleAlertMinutes(v interface{}) error {
	f, ok := v.(float64)
	if i, isInt := v.(int); isInt {
		f, ok = float64(i), true
	}
	if !ok || f != math.Trunc(f) || f < 0 {
		return fmt.Errorf("autonomous.automation.idle_alert_minutes: must be a whole number >= 0 (0 = off)")
	}
	return nil
}

// validateVaultUnreachableAlertAfterSeconds checks
// secrets.vault.unreachable_alert_after_seconds: a whole number >= 0
// (unset reads the 120-second default; 0 turns the alert off).
func validateVaultUnreachableAlertAfterSeconds(v interface{}) error {
	f, ok := v.(float64)
	if i, isInt := v.(int); isInt {
		f, ok = float64(i), true
	}
	if !ok || f != math.Trunc(f) || f < 0 {
		return fmt.Errorf("secrets.vault.unreachable_alert_after_seconds: must be a whole number >= 0 (0 = off, default %d)", config.DefaultVaultUnreachableAlertAfterSeconds)
	}
	return nil
}

// validateTriageAlertMinutes checks autonomous.automation.triage_alert_minutes:
// a whole number >= 0 (an unset key reads the 30-minute default;
// an explicit 0 turns the triage alert off).
func validateTriageAlertMinutes(v interface{}) error {
	f, ok := v.(float64)
	if i, isInt := v.(int); isInt {
		f, ok = float64(i), true
	}
	if !ok || f != math.Trunc(f) || f < 0 {
		return fmt.Errorf("autonomous.automation.triage_alert_minutes: must be a whole number >= 0 (0 = off, default %d)", config.DefaultAutomationTriageAlertMinutes)
	}
	return nil
}

// dependencyUpdateKeyPrefix — the REST key prefix for the dependency
// update knobs (autonomous.automation.dependency_update.*).
const dependencyUpdateKeyPrefix = "autonomous.automation.dependency_update."

// dependencyUpdateSurface builds the GET /api/config surface for the
// dependency update block, emitting effective values.
func dependencyUpdateSurface(c config.DependencyUpdateConfig) map[string]interface{} {
	return map[string]interface{}{
		"enabled":    c.EffectiveEnabled(),
		"schedule":   c.EffectiveSchedule(),
		"max_majors": c.EffectiveMaxMajors(),
	}
}

// parseDependencyUpdateValue folds one patch value into a copy of the
// block. It returns an error naming the key when the value has the wrong
// type (a non-boolean enabled, a non-string schedule, a fractional or
// non-numeric max_majors).
func parseDependencyUpdateValue(c *config.DependencyUpdateConfig, field string, v interface{}) error {
	switch field {
	case "enabled":
		b, ok := v.(bool)
		if !ok {
			return fmt.Errorf("%senabled: must be true or false", dependencyUpdateKeyPrefix)
		}
		c.Enabled = &b
	case "schedule":
		str, ok := v.(string)
		if !ok {
			return fmt.Errorf("%sschedule: must be a string 'weekday HH:MM' (a weekday name monday..sunday followed by a 24-hour time, for example 'monday 05:00')", dependencyUpdateKeyPrefix)
		}
		c.Schedule = str
	case "max_majors":
		f, ok := v.(float64)
		if i, isInt := v.(int); isInt {
			f, ok = float64(i), true
		}
		if !ok || f != math.Trunc(f) {
			return fmt.Errorf("%smax_majors: must be a whole number from 0 to %d (0 = no majors in a run, unset reads %d)",
				dependencyUpdateKeyPrefix, config.DependencyUpdateMaxMajorsLimit, config.DependencyUpdateDefaultMaxMajors)
		}
		n := int(f)
		c.MaxMajors = &n
	}
	return nil
}

// validateDependencyUpdatePatch applies every dependency_update key in the
// patch to a copy of the stored block and runs the config package's
// Validate on the result, so a bad value is refused before anything is
// applied and on the same rules the config-load path uses.
func validateDependencyUpdatePatch(cur config.DependencyUpdateConfig, patch map[string]interface{}) error {
	touched := false
	for _, field := range []string{"enabled", "schedule", "max_majors"} {
		v, ok := patch[dependencyUpdateKeyPrefix+field]
		if !ok {
			continue
		}
		touched = true
		if err := parseDependencyUpdateValue(&cur, field, v); err != nil {
			return err
		}
	}
	if !touched {
		return nil
	}
	return cur.Validate()
}

// applyDependencyUpdateValue stores one validated value; a value that
// fails to parse is left unchanged.
func applyDependencyUpdateValue(c *config.DependencyUpdateConfig, field string, v interface{}) {
	next := *c
	if err := parseDependencyUpdateValue(&next, field, v); err == nil && next.Validate() == nil {
		*c = next
	}
}

// repoCIWatchKeyPrefix — the REST key prefix for the repo CI watch knobs
// (autonomous.automation.repo_ci_watch.*).
const repoCIWatchKeyPrefix = "autonomous.automation.repo_ci_watch."

// repoCIWatchSurface builds the GET /api/config surface for the repo CI
// watch, emitting effective values: unset (nil / zero / empty) fields read
// their defaults from the Effective* accessors, and an explicit alert 0
// stays 0 (the alert off).
func repoCIWatchSurface(c config.RepoCIWatchConfig) map[string]interface{} {
	return map[string]interface{}{
		"mode":               c.EffectiveMode(),
		"repos":              c.EffectiveRepos(),
		"poll_seconds":       c.EffectivePollSeconds(),
		"alert_minutes":      c.EffectiveAlertMinutes(),
		"tag_count":          c.EffectiveTagCount(),
		"hung_after_minutes": c.EffectiveHungAfterMinutes(),
		"security_workflows": c.EffectiveSecurityWorkflows(),
		"delete_superseded":  c.EffectiveDeleteSuperseded(),
	}
}

// parseRepoCIWatchWhole accepts a whole number for one repo CI watch key
// (the CLI and the comm channel may send plain ints or JSON numbers;
// fractional, string and missing values are refused with a clear error).
func parseRepoCIWatchWhole(field string, v interface{}) (int, error) {
	f, ok := v.(float64)
	if i, isInt := v.(int); isInt {
		f, ok = float64(i), true
	}
	if !ok || f != math.Trunc(f) {
		return 0, fmt.Errorf("%s%s: must be a whole number", repoCIWatchKeyPrefix, field)
	}
	return int(f), nil
}

// validateRepoCIWatchPatch checks every autonomous.automation.repo_ci_watch.*
// value present in the patch before anything is applied, so one bad value
// rejects the whole PUT rather than being silently skipped. It runs the
// same rules the config-load path runs (config.validateRepoCIWatch), so a
// bad value is refused consistently on every write path.
func validateRepoCIWatchPatch(patch map[string]interface{}) error {
	if v, ok := patch[repoCIWatchKeyPrefix+"mode"]; ok {
		if str, isStr := v.(string); !isStr || config.ValidateRepoCIWatchMode(str) != nil {
			return fmt.Errorf("%smode: must be %q, %q or %q",
				repoCIWatchKeyPrefix, config.RepoCIWatchModeOff, config.RepoCIWatchModeShadow, config.RepoCIWatchModeOn)
		}
	}
	if v, ok := patch[repoCIWatchKeyPrefix+"delete_superseded"]; ok {
		if str, isStr := v.(string); !isStr || config.ValidateRepoCIWatchDeleteSuperseded(str) != nil {
			return fmt.Errorf("%sdelete_superseded: must be %q or %q",
				repoCIWatchKeyPrefix, config.RepoCIWatchDeleteSupersededReport, config.RepoCIWatchDeleteSupersededDelete)
		}
	}
	if v, ok := patch[repoCIWatchKeyPrefix+"repos"]; ok {
		arr, isArr := v.([]interface{})
		if !isArr {
			return fmt.Errorf("%srepos: must be a list of absolute repository paths (unset means nothing is watched)", repoCIWatchKeyPrefix)
		}
		for i, item := range arr {
			path, isStr := item.(string)
			p := strings.TrimSpace(path)
			if !isStr || len(p) <= 1 || !strings.HasPrefix(p, "/") {
				return fmt.Errorf("%srepos[%d]: %v must be an absolute path (repositories are never inferred from the working directory)",
					repoCIWatchKeyPrefix, i, item)
			}
		}
	}
	if v, ok := patch[repoCIWatchKeyPrefix+"poll_seconds"]; ok {
		n, err := parseRepoCIWatchWhole("poll_seconds", v)
		if err != nil {
			return err
		}
		if n < config.RepoCIWatchMinPollSeconds {
			return fmt.Errorf("%spoll_seconds: %d must be a whole number >= %d (unset reads the %d-second default)",
				repoCIWatchKeyPrefix, n, config.RepoCIWatchMinPollSeconds, config.RepoCIWatchDefaultPollSeconds)
		}
	}
	if v, ok := patch[repoCIWatchKeyPrefix+"alert_minutes"]; ok {
		n, err := parseRepoCIWatchWhole("alert_minutes", v)
		if err != nil {
			return err
		}
		if n < 0 {
			return fmt.Errorf("%salert_minutes: %d must be a whole number >= 0 (0 = alert off, unset reads the %d-minute default)",
				repoCIWatchKeyPrefix, n, config.DefaultAutomationTriageAlertMinutes)
		}
	}
	if v, ok := patch[repoCIWatchKeyPrefix+"tag_count"]; ok {
		n, err := parseRepoCIWatchWhole("tag_count", v)
		if err != nil {
			return err
		}
		if n < 0 {
			return fmt.Errorf("%stag_count: %d must be a whole number >= 0 (0 = none, unset reads the %d default)",
				repoCIWatchKeyPrefix, n, config.RepoCIWatchDefaultTagCount)
		}
	}
	if v, ok := patch[repoCIWatchKeyPrefix+"hung_after_minutes"]; ok {
		n, err := parseRepoCIWatchWhole("hung_after_minutes", v)
		if err != nil {
			return err
		}
		if n < config.RepoCIWatchMinHungAfterMinutes {
			return fmt.Errorf("%shung_after_minutes: %d must be a whole number >= %d (unset reads the %d-minute default)",
				repoCIWatchKeyPrefix, n, config.RepoCIWatchMinHungAfterMinutes, config.RepoCIWatchDefaultHungAfterMinutes)
		}
	}
	if v, ok := patch[repoCIWatchKeyPrefix+"security_workflows"]; ok {
		if _, isArr := v.([]interface{}); !isArr {
			return fmt.Errorf("%ssecurity_workflows: must be a list of workflow names (an empty list means none)", repoCIWatchKeyPrefix)
		}
	}
	return nil
}

// applyRepoCIWatchValue stores one repo CI watch value (validated above in
// handlePutConfig; a value that fails the check here is left unchanged).
func applyRepoCIWatchValue(rcw *config.RepoCIWatchConfig, field string, v interface{}) {
	switch field {
	case "mode":
		str := toString(v)
		if config.ValidateRepoCIWatchMode(str) == nil {
			rcw.Mode = str
		}
	case "delete_superseded":
		str := toString(v)
		if config.ValidateRepoCIWatchDeleteSuperseded(str) == nil {
			rcw.DeleteSuperseded = str
		}
	case "repos":
		if arr, ok := toStringArray(v); ok {
			rcw.Repos = arr
		}
	case "security_workflows":
		if arr, ok := toStringArray(v); ok {
			rcw.SecurityWorkflows = arr
		}
	case "poll_seconds":
		if n, err := parseRepoCIWatchWhole("poll_seconds", v); err == nil && n >= config.RepoCIWatchMinPollSeconds {
			rcw.PollSeconds = n
		}
	case "hung_after_minutes":
		if n, err := parseRepoCIWatchWhole("hung_after_minutes", v); err == nil && n >= config.RepoCIWatchMinHungAfterMinutes {
			rcw.HungAfterMinutes = n
		}
	case "alert_minutes":
		// A pointer on purpose: an explicit 0 (the alert off) must be stored
		// as such, not dropped as a zero.
		if n, err := parseRepoCIWatchWhole("alert_minutes", v); err == nil && n >= 0 {
			rcw.AlertMinutes = &n
		}
	case "tag_count":
		// A pointer on purpose: an explicit 0 (no tags watched) must be
		// stored as such, not dropped as a zero.
		if n, err := parseRepoCIWatchWhole("tag_count", v); err == nil && n >= 0 {
			rcw.TagCount = &n
		}
	}
}

// securityFailureKeyPrefix — the REST key prefix for the security failure
// knob (autonomous.automation.security_failure.mode).
const securityFailureKeyPrefix = "autonomous.automation.security_failure."

// securityFailureSurface renders the security failure block on the GET
// /api/config surface as effective values (unset or empty mode reads
// shadow, the first-release default), read from the Effective* accessors
// — never a literal.
func securityFailureSurface(c config.SecurityFailureConfig) map[string]interface{} {
	return map[string]interface{}{
		"mode": c.EffectiveMode(),
	}
}

// validateSecurityFailurePatch checks every autonomous.automation
// .security_failure.* value present in the patch before anything is
// applied, so one bad value rejects the whole PUT rather than being
// silently skipped. It runs the same rules the config-load path runs
// (config.validateSecurityFailure), so a bad value is refused
// consistently on every write path.
func validateSecurityFailurePatch(patch map[string]interface{}) error {
	v, ok := patch[securityFailureKeyPrefix+"mode"]
	if !ok {
		return nil
	}
	str, isStr := v.(string)
	if !isStr || config.ValidateSecurityFailureMode(str) != nil {
		return fmt.Errorf("%smode: must be %q, %q or %q (empty is unset and reads the %q first-release default)",
			securityFailureKeyPrefix, config.SecurityFailureModeOff, config.SecurityFailureModeShadow, config.SecurityFailureModeOn, config.SecurityFailureModeShadow)
	}
	return nil
}

// applySecurityFailureValue stores the security failure value (validated
// above in handlePutConfig; a value that fails the check here is left
// unchanged).
func applySecurityFailureValue(sfc *config.SecurityFailureConfig, v interface{}) {
	if str, isStr := v.(string); isStr && config.ValidateSecurityFailureMode(str) == nil {
		sfc.Mode = str
	}
}

// supervisorWatchKeyPrefix — the REST key prefix for the supervisor
// watch knobs (autonomous.automation.supervisor_watch.*).
const supervisorWatchKeyPrefix = "autonomous.automation.supervisor_watch."

// supervisorWatchSurface renders the supervisor watch block on the GET
// /api/config surface as effective values (an unset or empty mode reads
// shadow, the release default — reports only, never acts — and the unset
// minute thresholds read their one defaults), read from the Effective*
// accessors — never a literal.
func supervisorWatchSurface(c config.SupervisorWatchConfig) map[string]interface{} {
	return map[string]interface{}{
		"mode":                  c.EffectiveMode(),
		"alert_minutes":         c.EffectiveAlertMinutes(),
		"silent_minutes":        c.EffectiveSilentMinutes(),
		"poll_seconds":          c.EffectivePollSeconds(),
		"script_output_path":    c.ScriptOutputPath,
		"tmux_restart_baseline": c.TmuxRestartBaseline,
		"main_checkout":         c.MainCheckout,
		"working_model":         strings.TrimSpace(c.WorkingModel),
	}
}

// patchTouchesSupervisorWatch reports whether a PUT /api/config patch
// carries any autonomous.automation.supervisor_watch.* key.
func patchTouchesSupervisorWatch(patch map[string]interface{}) bool {
	for k := range patch {
		if strings.HasPrefix(k, supervisorWatchKeyPrefix) {
			return true
		}
	}
	return false
}

// withSupervisorWatchClears makes the supervisor watch block of the
// automation JSON carry its string keys and poll_seconds explicitly. Those
// fields are omitempty (or empty strings the live merge cannot tell from
// absent), so a PUT that clears one back to empty was dropped from the
// pushed block and the live watch kept serving the stale value until a
// restart. Writing the current value explicitly lets the live merge apply
// the clear. A block that cannot be decoded is returned unchanged.
func withSupervisorWatchClears(automation json.RawMessage, swc config.SupervisorWatchConfig) json.RawMessage {
	var auto map[string]json.RawMessage
	if json.Unmarshal(automation, &auto) != nil {
		return automation
	}
	block := map[string]json.RawMessage{}
	if raw, ok := auto["supervisor_watch"]; ok {
		if json.Unmarshal(raw, &block) != nil {
			return automation
		}
	}
	for key, val := range map[string]interface{}{
		"script_output_path":    swc.ScriptOutputPath,
		"tmux_restart_baseline": swc.TmuxRestartBaseline,
		"main_checkout":         swc.MainCheckout,
		"working_model":         swc.WorkingModel,
		"poll_seconds":          swc.PollSeconds,
	} {
		b, err := json.Marshal(val)
		if err != nil {
			return automation
		}
		block[key] = b
	}
	nb, err := json.Marshal(block)
	if err != nil {
		return automation
	}
	auto["supervisor_watch"] = nb
	out, err := json.Marshal(auto)
	if err != nil {
		return automation
	}
	return out
}

// validateSupervisorWatchWhole checks one supervisor watch minute
// threshold: a whole number >= 0 (0 is legal — the operator said zero;
// an unset key reads its default, which is not this value's business).
func validateSupervisorWatchWhole(field string, v interface{}) error {
	f, ok := v.(float64)
	if i, isInt := v.(int); isInt {
		f, ok = float64(i), true
	}
	if !ok || f != math.Trunc(f) || f < 0 {
		return fmt.Errorf("%s%s: must be a whole number >= 0 (an unset key reads its default)",
			supervisorWatchKeyPrefix, field)
	}
	return nil
}

// parseSupervisorWatchWhole accepts a whole number for one supervisor watch
// key (the CLI and the comm channel may send plain ints or JSON numbers;
// fractional, string and missing values are refused with a clear error).
func parseSupervisorWatchWhole(field string, v interface{}) (int, error) {
	f, ok := v.(float64)
	if i, isInt := v.(int); isInt {
		f, ok = float64(i), true
	}
	if !ok || f != math.Trunc(f) {
		return 0, fmt.Errorf("%s%s: must be a whole number", supervisorWatchKeyPrefix, field)
	}
	return int(f), nil
}

// validateSupervisorWatchPatch checks every autonomous.automation
// .supervisor_watch.* value present in the patch before anything is
// applied, so one bad value rejects the whole PUT rather than being
// silently skipped. It runs the same rules the config-load path runs
// (config.validateSupervisorWatch), so a bad value is refused
// consistently on every write path.
func validateSupervisorWatchPatch(patch map[string]interface{}) error {
	if v, ok := patch[supervisorWatchKeyPrefix+"mode"]; ok {
		str, isStr := v.(string)
		if !isStr || config.ValidateSupervisorWatchMode(str) != nil {
			return fmt.Errorf("%smode: must be %q, %q or %q (empty is unset and reads the %q release default)",
				supervisorWatchKeyPrefix, config.SupervisorWatchModeOff, config.SupervisorWatchModeShadow, config.SupervisorWatchModeOn, config.SupervisorWatchModeShadow)
		}
	}
	for _, field := range []string{"alert_minutes", "silent_minutes"} {
		if v, ok := patch[supervisorWatchKeyPrefix+field]; ok {
			if verr := validateSupervisorWatchWhole(field, v); verr != nil {
				return verr
			}
		}
	}
	if v, ok := patch[supervisorWatchKeyPrefix+"poll_seconds"]; ok {
		n, err := parseSupervisorWatchWhole("poll_seconds", v)
		if err != nil {
			return err
		}
		if n < 0 || (n > 0 && n < config.SupervisorWatchMinPollSeconds) {
			return fmt.Errorf("%spoll_seconds: %d must be 0 (unset) or a whole number >= %d (unset reads the %d-second default)",
				supervisorWatchKeyPrefix, n, config.SupervisorWatchMinPollSeconds, config.DefaultSupervisorWatchPollSeconds)
		}
	}
	for _, field := range []string{"script_output_path", "tmux_restart_baseline", "main_checkout", "working_model"} {
		if v, ok := patch[supervisorWatchKeyPrefix+field]; ok {
			if _, isStr := v.(string); !isStr {
				return fmt.Errorf("%s%s: must be a string", supervisorWatchKeyPrefix, field)
			}
		}
	}
	return nil
}

// applySupervisorWatchValue stores one supervisor watch value (validated
// above in handlePutConfig; a value that fails the check here is left
// unchanged). The minute thresholds are pointers on purpose: an explicit
// 0 must be stored as such, not dropped as a zero.
func applySupervisorWatchValue(swc *config.SupervisorWatchConfig, field string, v interface{}) {
	switch field {
	case "mode":
		if str, ok := v.(string); ok && config.ValidateSupervisorWatchMode(str) == nil {
			swc.Mode = str
		}
	case "alert_minutes":
		if n, ok := toInt(v); ok && n >= 0 {
			swc.AlertMinutes = &n
		}
	case "silent_minutes":
		if n, ok := toInt(v); ok && n >= 0 {
			swc.SilentMinutes = &n
		}
	case "poll_seconds":
		// 0 is unset (it reads the default), the same as on the config-load
		// path. An explicitly set value below the minimum is already refused
		// by validateSupervisorWatchPatch and by config.load validation; a
		// value that slips past those checks is left unchanged rather than
		// stored.
		if n, err := parseSupervisorWatchWhole("poll_seconds", v); err == nil && (n == 0 || n >= config.SupervisorWatchMinPollSeconds) {
			swc.PollSeconds = n
		}
	case "script_output_path":
		if str, ok := v.(string); ok {
			swc.ScriptOutputPath = strings.TrimSpace(str)
		}
	case "tmux_restart_baseline":
		if str, ok := v.(string); ok {
			swc.TmuxRestartBaseline = strings.TrimSpace(str)
		}
	case "main_checkout":
		// Empty (after trimming) disables the dirty main-checkout finding.
		if str, ok := v.(string); ok {
			swc.MainCheckout = strings.TrimSpace(str)
		}
	case "working_model":
		// Empty (after trimming) turns the model_not_resident check off.
		if str, ok := v.(string); ok {
			swc.WorkingModel = strings.TrimSpace(str)
		}
	}
}

// validateMergePendingMode checks autonomous.automation.merge_pending_mode: a
// string that is "off", "shadow" or "on" (empty is not accepted over the wire;
// the default applies when the key is absent).
func validateMergePendingMode(v interface{}) error {
	str, ok := v.(string)
	if !ok || str == "" {
		return fmt.Errorf("autonomous.automation.merge_pending_mode: must be %q, %q or %q",
			config.MergePendingModeOff, config.MergePendingModeShadow, config.MergePendingModeOn)
	}
	return config.ValidateMergePendingMode(str)
}

// validateMergedMemberPolicy checks autonomous.automation.
// merged_member_policy: a string that is "completed", "manual" or "block"
// (empty is not accepted over the wire; the default applies when the key is
// absent).
func validateMergedMemberPolicy(v interface{}) error {
	str, ok := v.(string)
	if !ok || str == "" {
		return fmt.Errorf("autonomous.automation.merged_member_policy: must be %q, %q or %q",
			config.MergedMemberPolicyCompleted, config.MergedMemberPolicyManual, config.MergedMemberPolicyBlock)
	}
	return config.ValidateMergedMemberPolicy(str)
}

// validateMergePendingAlertMinutes checks
// autonomous.automation.merge_pending_alert_minutes: a whole number >= 0 (an
// unset key reads the 30-minute default; an explicit 0 turns the merge-pending
// alert off).
func validateMergePendingAlertMinutes(v interface{}) error {
	f, ok := v.(float64)
	if i, isInt := v.(int); isInt {
		f, ok = float64(i), true
	}
	if !ok || f != math.Trunc(f) || f < 0 {
		return fmt.Errorf("autonomous.automation.merge_pending_alert_minutes: must be a whole number >= 0 (0 = off, default %d)", config.DefaultAutomationMergePendingAlertMinutes)
	}
	return nil
}

// validateAgentInsertPolicy checks autonomous.automation.agent_insert_policy:
// a string that is "allow" or "require_operator" (empty is not accepted over
// the wire; the default applies when the key is absent).
func validateAgentInsertPolicy(v interface{}) error {
	str, ok := v.(string)
	if !ok || str == "" {
		return fmt.Errorf("autonomous.automation.agent_insert_policy: must be %q or %q", config.AgentInsertPolicyAllow, config.AgentInsertPolicyRequireOperator)
	}
	return config.ValidateAgentInsertPolicy(str)
}

// validateCreateSearchModeValue checks
// autonomous.automation.create_search_mode: a string that is "off", "shadow"
// or "on" (empty is not accepted over the wire; the "shadow" default applies
// when the key is absent).
func validateCreateSearchModeValue(v interface{}) error {
	str, ok := v.(string)
	if !ok || str == "" {
		return fmt.Errorf("autonomous.automation.create_search_mode: must be %q, %q or %q (default %q)", config.CreateSearchModeOff, config.CreateSearchModeShadow, config.CreateSearchModeOn, config.DefaultAutomationCreateSearchMode)
	}
	return config.ValidateCreateSearchMode(str)
}

// validateCreateSearchThresholdValue checks
// autonomous.automation.create_search_threshold: a finite number in the
// open-closed interval (0, 1] (0.5 is the default when the key is absent).
func validateCreateSearchThresholdValue(v interface{}) error {
	f, err := createSearchThresholdNumber(v)
	if err != nil {
		return fmt.Errorf("autonomous.automation.create_search_threshold: must be a finite number in the open-closed interval (0, 1], default %v", config.DefaultAutomationCreateSearchThreshold)
	}
	return config.ValidateCreateSearchThreshold(f)
}

// createSearchThresholdNumber coerces a patch value to a float64; it is the
// shared numeric coercion for the create_search_threshold validation in
// handlePutConfig and applyConfigPatch.
func createSearchThresholdNumber(v interface{}) (float64, error) {
	switch n := v.(type) {
	case float64:
		return n, nil
	case int:
		return float64(n), nil
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(n), 64)
		if err != nil {
			return 0, err
		}
		return f, nil
	default:
		return 0, fmt.Errorf("non-numeric value")
	}
}

// parseCreateSearchThreshold is the parse-and-validate pair used by
// applyConfigPatch for autonomous.automation.create_search_threshold.
func parseCreateSearchThreshold(v interface{}) (float64, error) {
	f, err := createSearchThresholdNumber(v)
	if err != nil {
		return 0, err
	}
	if verr := config.ValidateCreateSearchThreshold(f); verr != nil {
		return 0, verr
	}
	return f, nil
}

// validateCleanupModeValue checks autonomous.automation.cleanup_mode: it
// must hold a string. Unlike validateMergePendingMode and
// validateCreateSearchModeValue, unknown words (and the empty string) are
// not errors: they resolve to report through EffectiveCleanupMode, so the
// write path only refuses a type mismatch.
func validateCleanupModeValue(v interface{}) error {
	if _, ok := v.(string); !ok {
		return fmt.Errorf("autonomous.automation.cleanup_mode: must be a string (off, report or on; an empty or unrecognized value reads as report)")
	}
	return nil
}

// validateCleanupWholeNumber checks one of the three whole-number cleanup
// keys (cleanup_retention_hours, cleanup_max_removals_per_pass,
// disk_alert_percent): it must hold a whole number (a JSON number or a
// numeric string). Out-of-range values are not refused here — the
// Effective* accessors resolve zero/negative values to the defaults and
// clamp disk_alert_percent to 1..100.
func validateCleanupWholeNumber(key string, v interface{}) error {
	if _, ok := toInt(v); !ok {
		return fmt.Errorf("%s: must be a whole number (zero or a negative value reads the default; disk_alert_percent is otherwise clamped to 1..100)", key)
	}
	return nil
}

// planTierTableForDisplay renders the tier -> max-inflight table for the
// config surface. A nil table (test servers, pre-default configs) renders
// as an empty object rather than null.
func planTierTableForDisplay(table map[string]int) map[string]int {
	if table == nil {
		return map[string]int{}
	}
	return table
}

// attemptCostPatchTouched reports whether the PUT patch carries any of the
// three autonomous attempt-cost keys. Only then does handlePutConfig run the
// shared config.ValidateAttemptCostSettings check — so a patch that changes
// unrelated keys is never refused because of values it did not send.
func attemptCostPatchTouched(patch map[string]interface{}) bool {
	for _, k := range []string{
		"autonomous.first_attempt",
		"autonomous.first_attempt_escalate_after_failures",
		"autonomous.retry_resume_max_context_tokens",
	} {
		if _, ok := patch[k]; ok {
			return true
		}
	}
	return false
}

// parseFirstAttempt turns a decoded JSON value into the first_attempt table.
// ok is false when the value is not an object; each entry is decoded as an
// optional backend and an optional model string (a non-object entry becomes
// an empty choice, which the shared validator then vets for kind and model
// rules, so parsing here only checks the top-level shape).
func parseFirstAttempt(v interface{}) (map[string]config.FirstAttemptChoice, bool) {
	obj, ok := v.(map[string]interface{})
	if !ok {
		return nil, false
	}
	out := make(map[string]config.FirstAttemptChoice, len(obj))
	for kind, entry := range obj {
		choice := config.FirstAttemptChoice{}
		if e, ok := entry.(map[string]interface{}); ok {
			choice.Backend, _ = e["backend"].(string)
			choice.Model, _ = e["model"].(string)
		}
		out[kind] = choice
	}
	return out, true
}

// cloneAttemptChoices copies one kind→choice table so a candidate config can
// be validated in handlePutConfig without the live s.cfg sharing the map.
func cloneAttemptChoices(m map[string]config.FirstAttemptChoice) map[string]config.FirstAttemptChoice {
	out := make(map[string]config.FirstAttemptChoice, len(m))
	for kind, choice := range m {
		out[kind] = choice
	}
	return out
}

// firstAttemptDisplay renders autonomous.first_attempt for GET /api/config:
// a nil table (nothing configured for any kind) becomes an empty object
// rather than null, so the JSON is always an object.
func firstAttemptDisplay(m map[string]config.FirstAttemptChoice) map[string]config.FirstAttemptChoice {
	if m == nil {
		return map[string]config.FirstAttemptChoice{}
	}
	return m
}

// priorityByKindForDisplay renders the kind → capacity-priority table in
// GET /api/config. It displays the EFFECTIVE entries — the operator's
// overrides layered onto the shipped defaults — so an unset table shows its
// safe defaults (bug=80, maintenance=70, feature=20, other=0) instead of
// blanks. Never returns nil, so the JSON is an object, not null.
func priorityByKindForDisplay(a config.AutonomousConfig) map[string]int {
	m := a.EffectivePriorityByKind()
	if m == nil {
		return map[string]int{}
	}
	return m
}

// parsePlanTierTable validates a value destined for
// autonomous.plan_tier_max_inflight and returns the table it represents.
// Accepted shapes:
//
//   - an object: {"pro": 1, "max_5x": 2} (REST/PWA raw JSON),
//   - that object as a string: '{"pro":1}' (CLI `config set` and the
//     comm-channel `configure` verb quote their values),
//   - a compact list: "pro=1,max_5x=2".
//
// Every key must be a known tier (pro|max_5x|max_20x|api); the empty
// tier name is rejected because it would shadow EffectiveMaxInflight's
// unset-tier fallback. Every limit must be an integer >= 0 (0 =
// unlimited for that tier). Empty input is a valid "clear the table" edit
// and returns an empty non-nil map so the clear round-trips (the config
// struct field has no omitempty on purpose).
func parsePlanTierTable(v any) (map[string]int, error) {
	var raw map[string]any
	switch x := v.(type) {
	case map[string]any:
		raw = x
	case nil:
		return map[string]int{}, nil
	case string:
		s := strings.TrimSpace(x)
		if strings.HasPrefix(s, "{") {
			if err := json.Unmarshal([]byte(s), &raw); err != nil {
				return nil, fmt.Errorf("plan_tier_max_inflight: malformed JSON %q: %v", s, err)
			}
		} else if s == "" {
			return map[string]int{}, nil
		} else {
			raw = map[string]any{}
			for _, part := range strings.Split(s, ",") {
				kv := strings.SplitN(strings.TrimSpace(part), "=", 2)
				if len(kv) != 2 || strings.TrimSpace(kv[0]) == "" || strings.TrimSpace(kv[1]) == "" {
					return nil, fmt.Errorf("plan_tier_max_inflight: expected an object, a JSON object string, or \"tier=limit,tier=limit\"; got %q", strings.TrimSpace(part))
				}
				raw[kv[0]] = strings.TrimSpace(kv[1])
			}
		}
	default:
		return nil, fmt.Errorf("plan_tier_max_inflight: expected an object (or a JSON object string); got %T", v)
	}
	out := make(map[string]int, len(raw))
	for k, val := range raw {
		if !inference.ValidPlanTier(inference.PlanTier(k)) {
			return nil, fmt.Errorf("plan_tier_max_inflight: unknown tier %q (allowed: %s, %s, %s, %s)",
				k, inference.PlanTierPro, inference.PlanTierMax5x, inference.PlanTierMax20x, inference.PlanTierAPI)
		}
		switch n := val.(type) {
		case int:
			if n < 0 {
				return nil, fmt.Errorf("plan_tier_max_inflight: limit for %q must be an integer >= 0", k)
			}
			out[k] = n
		case json.Number:
			if iv, err := n.Int64(); err == nil {
				if iv < 0 {
					return nil, fmt.Errorf("plan_tier_max_inflight: limit for %q must be an integer >= 0", k)
				}
				out[k] = int(iv)
				continue
			}
			return nil, fmt.Errorf("plan_tier_max_inflight: limit for %q must be an integer >= 0; got %v", k, val)
		case float64:
			if n < 0 || n != math.Trunc(n) {
				return nil, fmt.Errorf("plan_tier_max_inflight: limit for %q must be an integer >= 0", k)
			}
			out[k] = int(n)
		case string:
			iv, err := strconv.Atoi(strings.TrimSpace(n))
			if err != nil || iv < 0 {
				return nil, fmt.Errorf("plan_tier_max_inflight: limit for %q must be an integer >= 0", k)
			}
			out[k] = iv
		case bool:
			return nil, fmt.Errorf("plan_tier_max_inflight: limit for %q must be an integer >= 0, not a boolean", k)
		default:
			return nil, fmt.Errorf("plan_tier_max_inflight: limit for %q must be an integer >= 0; got %v", k, val)
		}
	}
	return out, nil
}

// validateJudgeSizeThresholdPatch checks autonomous.judge_size_threshold_lines
// in a config patch: a whole number of 0 or more. Zero is valid and turns
// size-aware judge routing off.
func validateJudgeSizeThresholdPatch(patch map[string]interface{}) error {
	const key = "autonomous.judge_size_threshold_lines"
	v, ok := patch[key]
	if !ok {
		return nil
	}
	switch x := v.(type) {
	case float64:
		if x == math.Trunc(x) && x >= 0 && x <= math.MaxInt32 {
			return nil
		}
	case int:
		if x >= 0 {
			return nil
		}
	}
	return fmt.Errorf("%s: %v is invalid; use a whole number of 0 or more (0 turns size-aware judge routing off)", key, v)
}

// validateJudgePromptDeliveryPatch checks the judge prompt delivery keys in a
// config patch: autonomous.judge_prompt_file_threshold_lines is a whole number
// from 0 to 100000, autonomous.judge_delivery_wait_per_thousand_lines_seconds a
// whole number from 0 to 3600.
func validateJudgePromptDeliveryPatch(patch map[string]interface{}) error {
	for key, max := range map[string]float64{
		"autonomous.judge_prompt_file_threshold_lines":              100000,
		"autonomous.judge_delivery_wait_per_thousand_lines_seconds": 3600,
	} {
		v, ok := patch[key]
		if !ok {
			continue
		}
		var f float64
		switch x := v.(type) {
		case float64:
			f = x
		case int:
			f = float64(x)
		default:
			return fmt.Errorf("%s: %v is invalid; use a whole number from 0 to %d", key, v, int(max))
		}
		if f != math.Trunc(f) || f < 0 || f > max {
			return fmt.Errorf("%s: %v is invalid; use a whole number from 0 to %d", key, v, int(max))
		}
	}
	return nil
}

// validateJudgeStrongRoutingPatch checks the two strong-first judge routing
// keys in a config patch: autonomous.judge_strong_backend must be a string
// (empty clears the strong-first routing), and
// autonomous.judge_verdict_cache_enabled must be a JSON boolean (JSON null is
// not a boolean). Each refusal names the key.
func validateJudgeStrongRoutingPatch(patch map[string]interface{}) error {
	if v, ok := patch["autonomous.judge_strong_backend"]; ok {
		if _, isStr := v.(string); !isStr {
			return fmt.Errorf("autonomous.judge_strong_backend: %v is invalid; use a backend name, or an empty string to turn the strong-first routing off", v)
		}
	}
	if v, ok := patch["autonomous.judge_verdict_cache_enabled"]; ok {
		if _, isBool := v.(bool); !isBool {
			return fmt.Errorf("autonomous.judge_verdict_cache_enabled: %v is invalid; use a boolean (true or false)", v)
		}
	}
	return nil
}

// validateQuestionQualityPatch checks the four autonomous.questions quality and
// queue preview keys in a PUT /api/config body. Each refusal names the key and
// the valid values; a number must be a whole JSON number (a quoted number is
// not one).
func validateQuestionQualityPatch(patch map[string]interface{}) error {
	if v, ok := patch["autonomous.questions.quality_mode"]; ok {
		mode, isStr := v.(string)
		if !isStr {
			return fmt.Errorf("autonomous.questions.quality_mode: %v is invalid; use %q, %q or %q",
				v, config.QuestionQualityModeOff, config.QuestionQualityModeWarn, config.QuestionQualityModeRefuse)
		}
		if err := config.ValidateQuestionQualityMode(mode); err != nil {
			return err
		}
	}
	if v, ok := patch["autonomous.questions.quality_tracker_id_pattern"]; ok {
		pat, isStr := v.(string)
		if !isStr {
			return fmt.Errorf("autonomous.questions.quality_tracker_id_pattern: %v is invalid; use a regular expression as text (empty for the default)", v)
		}
		if err := config.ValidateQuestionQualityTrackerIDPattern(pat); err != nil {
			return err
		}
	}
	if v, ok := patch["autonomous.questions.quality_min_text_chars"]; ok {
		n, whole := wholeNumber(v)
		if !whole {
			return fmt.Errorf("autonomous.questions.quality_min_text_chars: %v is invalid; use a whole number >= 0 (default %d)",
				v, config.DefaultQuestionQualityMinTextChars)
		}
		if err := config.ValidateQuestionQualityMinTextChars(n); err != nil {
			return err
		}
	}
	if v, ok := patch["autonomous.questions.queue_preview_lines"]; ok {
		n, whole := wholeNumber(v)
		if !whole {
			return fmt.Errorf("autonomous.questions.queue_preview_lines: %v is invalid; use a whole number >= 1 (default %d)",
				v, config.DefaultQuestionQueuePreviewLines)
		}
		if err := config.ValidateQuestionQueuePreviewLines(n); err != nil {
			return err
		}
	}
	return nil
}

// decisionRecordBoolKeys, decisionRecordStringKeys and the keys named in
// validateDecisionRecordPatch are the autonomous.decisions.* keys a PUT
// /api/config body may carry. Every other autonomous.decisions.* key is not a
// key at all; the generic patch path ignores it like any unknown key.
var decisionRecordBoolKeys = []string{
	"autonomous.decisions.enabled",
	"autonomous.decisions.folder_withdraw_on_delete",
	"autonomous.decisions.clarity_require_tradeoffs",
	"autonomous.decisions.clarity_require_label",
}

var decisionRecordStringKeys = []string{
	"autonomous.decisions.folder",
	"autonomous.decisions.folder_glob",
	"autonomous.decisions.doc_root",
	"autonomous.decisions.doc_status_template",
}

// parseDecisionFollowUps reads autonomous.decisions.follow_ups from a PUT
// /api/config body: a list of {name, kind, target} objects, or a JSON text of
// such a list (the comm channel and the CLI send text). It checks the rules
// the config file load checks.
func parseDecisionFollowUps(v interface{}) ([]config.AutonomousDecisionFollowUp, error) {
	const key = "autonomous.decisions.follow_ups"
	var raw []byte
	switch x := v.(type) {
	case string:
		raw = []byte(strings.TrimSpace(x))
		if len(raw) == 0 {
			return nil, nil
		}
	case []interface{}:
		b, err := json.Marshal(x)
		if err != nil {
			return nil, fmt.Errorf("%s: %v", key, err)
		}
		raw = b
	case nil:
		return nil, nil
	default:
		return nil, fmt.Errorf("%s: expected a list of {name, kind, target} objects (or its JSON text); got %T", key, v)
	}
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	var list []config.AutonomousDecisionFollowUp
	if err := dec.Decode(&list); err != nil {
		return nil, fmt.Errorf("%s: expected a list of {name, kind, target} objects: %v", key, err)
	}
	if err := config.ValidateDecisionFollowUps(list); err != nil {
		return nil, err
	}
	return list, nil
}

// parseDecisionFrontMatterKeys reads autonomous.decisions.front_matter_keys
// from a PUT /api/config body: an object of text values, or its JSON text.
func parseDecisionFrontMatterKeys(v interface{}) (map[string]string, error) {
	const key = "autonomous.decisions.front_matter_keys"
	switch x := v.(type) {
	case nil:
		return map[string]string{}, nil
	case string:
		if strings.TrimSpace(x) == "" {
			return map[string]string{}, nil
		}
		out := map[string]string{}
		if err := json.Unmarshal([]byte(x), &out); err != nil {
			return nil, fmt.Errorf("%s: expected an object of canonical name to file name (or its JSON text): %v", key, err)
		}
		return out, nil
	case map[string]interface{}:
		out := make(map[string]string, len(x))
		for k, val := range x {
			s, ok := val.(string)
			if !ok {
				return nil, fmt.Errorf("%s: the name for %q must be text; got %T", key, k, val)
			}
			out[k] = s
		}
		return out, nil
	}
	return nil, fmt.Errorf("%s: expected an object of canonical name to file name (or its JSON text); got %T", key, v)
}

// validateDecisionRecordPatch checks the autonomous.decisions keys in a PUT
// /api/config body. A bad value rejects the whole PUT before anything is
// applied, by the same rules the config file load uses; each refusal names the
// key and the valid values.
func validateDecisionRecordPatch(patch map[string]interface{}) error {
	for _, k := range decisionRecordBoolKeys {
		if v, ok := patch[k]; ok {
			if _, isBool := v.(bool); !isBool {
				return fmt.Errorf("%s: must be a boolean (true or false)", k)
			}
		}
	}
	for _, k := range decisionRecordStringKeys {
		if v, ok := patch[k]; ok {
			if _, isStr := v.(string); !isStr {
				return fmt.Errorf("%s: %v is invalid; use text", k, v)
			}
		}
	}
	textRules := []struct {
		key    string
		hint   string
		verify func(string) error
	}{
		{"autonomous.decisions.clarity_mode", `"off", "warn" or "refuse"`, config.ValidateDecisionClarityMode},
		{"autonomous.decisions.doc_status_mode", `"off", "report" or "on"`, config.ValidateDecisionDocStatusMode},
		{"autonomous.decisions.clarity_tracker_id_pattern", "a regular expression as text (empty for the built-in pattern)", config.ValidateDecisionClarityTrackerIDPattern},
		{"autonomous.decisions.doc_status_pattern", "a regular expression as text (empty for the default)", config.ValidateDecisionDocStatusPattern},
		{"autonomous.decisions.front_matter_delimiter", "non-empty text", config.ValidateDecisionFrontMatterDelimiter},
		{"autonomous.decisions.front_matter_separator", "non-empty text", config.ValidateDecisionFrontMatterSeparator},
		{"autonomous.decisions.front_matter_field_separator", "non-empty text", config.ValidateDecisionFrontMatterFieldSeparator},
	}
	for _, r := range textRules {
		v, ok := patch[r.key]
		if !ok {
			continue
		}
		s, isStr := v.(string)
		if !isStr {
			return fmt.Errorf("%s: %v is invalid; use %s", r.key, v, r.hint)
		}
		if err := r.verify(s); err != nil {
			return err
		}
	}
	if v, ok := patch["autonomous.decisions.clarity_min_text_chars"]; ok {
		n, whole := wholeNumber(v)
		if !whole {
			return fmt.Errorf("autonomous.decisions.clarity_min_text_chars: %v is invalid; use a whole number >= 0 (default %d)",
				v, config.DefaultDecisionClarityMinTextChars)
		}
		if err := config.ValidateDecisionClarityMinTextChars(n); err != nil {
			return err
		}
	}
	if v, ok := patch["autonomous.decisions.front_matter_keys"]; ok {
		if _, err := parseDecisionFrontMatterKeys(v); err != nil {
			return err
		}
	}
	if v, ok := patch["autonomous.decisions.follow_ups"]; ok {
		if _, err := parseDecisionFollowUps(v); err != nil {
			return err
		}
	}
	return nil
}

// applyDecisionRecordKey stores one validated autonomous.decisions key. Keys
// whose default is not the zero value are stored as pointers, so an explicit
// false, 0 or empty text is told apart from an absent key.
func applyDecisionRecordKey(d *config.AutonomousDecisionsConfig, k string, v interface{}) {
	str := func() (string, bool) { s, ok := v.(string); return s, ok }
	switch k {
	case "autonomous.decisions.enabled":
		if b, ok := v.(bool); ok {
			d.Enabled = &b
		}
	case "autonomous.decisions.folder_withdraw_on_delete":
		if b, ok := v.(bool); ok {
			d.FolderWithdrawOnDelete = &b
		}
	case "autonomous.decisions.clarity_require_tradeoffs":
		if b, ok := v.(bool); ok {
			d.ClarityRequireTradeoffs = &b
		}
	case "autonomous.decisions.clarity_require_label":
		if b, ok := v.(bool); ok {
			d.ClarityRequireLabel = &b
		}
	case "autonomous.decisions.folder":
		if s, ok := str(); ok {
			d.Folder = s
		}
	case "autonomous.decisions.doc_root":
		if s, ok := str(); ok {
			d.DocRoot = s
		}
	case "autonomous.decisions.folder_glob":
		if s, ok := str(); ok {
			d.FolderGlob = &s
		}
	case "autonomous.decisions.doc_status_template":
		if s, ok := str(); ok {
			d.DocStatusTemplate = &s
		}
	case "autonomous.decisions.front_matter_delimiter":
		if s, ok := str(); ok {
			d.FrontMatterDelimiter = &s
		}
	case "autonomous.decisions.front_matter_separator":
		if s, ok := str(); ok {
			d.FrontMatterSeparator = &s
		}
	case "autonomous.decisions.front_matter_field_separator":
		if s, ok := str(); ok {
			d.FrontMatterFieldSeparator = &s
		}
	case "autonomous.decisions.clarity_mode":
		if s, ok := str(); ok {
			d.ClarityMode = &s
		}
	case "autonomous.decisions.doc_status_mode":
		if s, ok := str(); ok {
			d.DocStatusMode = &s
		}
	case "autonomous.decisions.clarity_tracker_id_pattern":
		if s, ok := str(); ok {
			d.ClarityTrackerIDPattern = &s
		}
	case "autonomous.decisions.doc_status_pattern":
		if s, ok := str(); ok {
			d.DocStatusPattern = &s
		}
	case "autonomous.decisions.clarity_min_text_chars":
		if n, ok := toInt(v); ok {
			d.ClarityMinTextChars = &n
		}
	case "autonomous.decisions.front_matter_keys":
		if m, err := parseDecisionFrontMatterKeys(v); err == nil {
			d.FrontMatterKeys = m
		}
	case "autonomous.decisions.follow_ups":
		if list, err := parseDecisionFollowUps(v); err == nil {
			d.FollowUps = list
		}
	}
}

// decisionRecordSurface is the effective autonomous.decisions values GET
// /api/config returns.
func decisionRecordSurface(d config.AutonomousDecisionsConfig) map[string]interface{} {
	return map[string]interface{}{
		"enabled":                      d.EffectiveEnabled(),
		"folder":                       d.EffectiveFolder(),
		"folder_glob":                  d.EffectiveFolderGlob(),
		"front_matter_delimiter":       d.EffectiveFrontMatterDelimiter(),
		"front_matter_separator":       d.EffectiveFrontMatterSeparator(),
		"front_matter_field_separator": d.EffectiveFrontMatterFieldSeparator(),
		"front_matter_keys":            d.EffectiveFrontMatterKeys(),
		"folder_withdraw_on_delete":    d.EffectiveFolderWithdrawOnDelete(),
		"clarity_mode":                 d.EffectiveClarityMode(),
		"clarity_min_text_chars":       d.EffectiveClarityMinTextChars(),
		"clarity_require_tradeoffs":    d.EffectiveClarityRequireTradeoffs(),
		"clarity_require_label":        d.EffectiveClarityRequireLabel(),
		"clarity_tracker_id_pattern":   d.EffectiveClarityTrackerIDPattern(),
		"doc_root":                     d.EffectiveDocRoot(),
		"doc_status_mode":              d.EffectiveDocStatusMode(),
		"doc_status_pattern":           d.EffectiveDocStatusPattern(),
		"doc_status_template":          d.EffectiveDocStatusTemplate(),
		"follow_ups":                   d.EffectiveFollowUps(),
	}
}

// validateResourceLimitsPatch checks the resource_limits.* keys in a PUT:
// each is a whole number, share is 1 to 100, floor and cap are >= 1, and the
// resulting floor (patched or current) is not above the resulting cap.
func validateResourceLimitsPatch(cur config.ResourceLimitsConfig, patch map[string]interface{}) error {
	next := cur
	fields := []struct {
		key string
		dst *int
	}{
		{"resource_limits.memory_share_percent", &next.MemorySharePercent},
		{"resource_limits.memory_floor_mb", &next.MemoryFloorMB},
		{"resource_limits.memory_cap_mb", &next.MemoryCapMB},
	}
	touched := false
	for _, f := range fields {
		v, ok := patch[f.key]
		if !ok {
			continue
		}
		touched = true
		n, whole := wholeNumber(v)
		if !whole {
			return fmt.Errorf("%s: %v is invalid; use a whole number", f.key, v)
		}
		*f.dst = n
	}
	if !touched {
		return nil
	}
	if _, ok := patch["resource_limits.memory_share_percent"]; ok &&
		(next.MemorySharePercent < 1 || next.MemorySharePercent > 100) {
		return fmt.Errorf("resource_limits.memory_share_percent: %d is invalid; use 1 to 100", next.MemorySharePercent)
	}
	for _, f := range fields[1:] {
		if _, ok := patch[f.key]; ok && *f.dst < 1 {
			return fmt.Errorf("%s: %d is invalid; use a whole number >= 1", f.key, *f.dst)
		}
	}
	if next.MemoryFloorMB > next.MemoryCapMB {
		return fmt.Errorf("resource_limits.memory_floor_mb (%d) must not be above resource_limits.memory_cap_mb (%d)",
			next.MemoryFloorMB, next.MemoryCapMB)
	}
	return nil
}

// validateIdentityBackupsPatch checks identity.backups_to_keep: a whole
// number >= 1.
func validateIdentityBackupsPatch(patch map[string]interface{}) error {
	v, ok := patch["identity.backups_to_keep"]
	if !ok {
		return nil
	}
	n, whole := wholeNumber(v)
	if !whole {
		return fmt.Errorf("identity.backups_to_keep: %v is invalid; use a whole number >= 1 (default %d)",
			v, config.DefaultIdentityBackupsToKeep)
	}
	return config.ValidateIdentityBackupsToKeep(n)
}

// validateUIPromptPatch checks session.dismiss_ui_prompts and the three
// session.ui_prompt_* keys: real types and whole-number bounds, by the same
// rules the config file load uses.
func validateUIPromptPatch(patch map[string]interface{}) error {
	var firstErr error
	err := func(e error) {
		if firstErr == nil && e != nil {
			firstErr = e
		}
	}
	if v, ok := patch["session.dismiss_ui_prompts"]; ok {
		if _, isBool := v.(bool); !isBool {
			firstErr = fmt.Errorf("session.dismiss_ui_prompts: %v is invalid; use a boolean (true or false)", v)
		}
	}
	if v, ok := patch["session.ui_prompt_dismiss_delay_seconds"]; ok {
		n, whole := wholeNumber(v)
		if !whole {
			err(fmt.Errorf("session.ui_prompt_dismiss_delay_seconds: %v is invalid; use a whole number >= 0 (0 = send at once; default %d)",
				v, config.DefaultUIPromptDismissDelaySeconds))
		} else {
			err(config.ValidateUIPromptDismissDelaySeconds(n))
		}
	}
	if v, ok := patch["session.ui_prompt_clear_bound_seconds"]; ok {
		n, whole := wholeNumber(v)
		if !whole {
			err(fmt.Errorf("session.ui_prompt_clear_bound_seconds: %v is invalid; use a whole number >= 1 (default %d)",
				v, config.DefaultUIPromptClearBoundSeconds))
		} else {
			err(config.ValidateUIPromptClearBoundSeconds(n))
		}
	}
	if v, ok := patch["session.ui_prompt_max_attempts"]; ok {
		n, whole := wholeNumber(v)
		if !whole {
			err(fmt.Errorf("session.ui_prompt_max_attempts: %v is invalid; use a whole number >= 1 (default %d)",
				v, config.DefaultUIPromptMaxAttempts))
		} else {
			err(config.ValidateUIPromptMaxAttempts(n))
		}
	}
	return firstErr
}

// validateUISplashPatch checks the ui.* splash minimum-dwell keys: a whole
// number from 0 to 60 inclusive, by the same rules the config file load uses.
func validateUISplashPatch(patch map[string]interface{}) error {
	var firstErr error
	err := func(e error) {
		if firstErr == nil && e != nil {
			firstErr = e
		}
	}
	if v, ok := patch["ui.splash_min_seconds"]; ok {
		n, whole := wholeNumber(v)
		if !whole {
			err(fmt.Errorf("ui.splash_min_seconds: %v is invalid; use a whole number from 0 to %d (0 = keep the legacy timing; default %d)",
				v, config.MaxUISplashSeconds, config.DefaultUISplashMinSeconds))
		} else {
			err(config.ValidateUISplashMinSeconds(n))
		}
	}
	if v, ok := patch["ui.splash_min_seconds_new_version"]; ok {
		n, whole := wholeNumber(v)
		if !whole {
			err(fmt.Errorf("ui.splash_min_seconds_new_version: %v is invalid; use a whole number from 0 to %d (0 = keep the legacy timing; default %d)",
				v, config.MaxUISplashSeconds, config.DefaultUISplashMinSecondsNewVersion))
		} else {
			err(config.ValidateUISplashMinSecondsNewVersion(n))
		}
	}
	return firstErr
}

// wholeNumber reports v as an int when it is a JSON number with no fractional
// part that fits an int32; anything else (a string, a fraction, a bool) is not.
func wholeNumber(v interface{}) (int, bool) {
	switch x := v.(type) {
	case float64:
		if x == math.Trunc(x) && x >= math.MinInt32 && x <= math.MaxInt32 {
			return int(x), true
		}
	case int:
		return x, true
	}
	return 0, false
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
	if !s.fedCap(w, r, federation.CapAnalyticsRead) {
		return
	}
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
	if r.Method == http.MethodGet {
		if !s.fedCap(w, r, federation.CapConfigRead) {
			return
		}
	} else if !s.fedCap(w, r, federation.CapConfigWrite) {
		return
	}
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
	if !s.fedCap(w, r, federation.CapAnalyticsRead) {
		return
	}
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
	if !s.fedCap(w, r, federation.CapSessionsKill) {
		return
	}
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

// handleFinishedOneShots lists ad-hoc one-shot sessions whose command has
// finished but that never reached complete (GET), or completes them (POST).
// Settings → About → Finished one-shot sessions uses both.
func (s *Server) handleFinishedOneShots(w http.ResponseWriter, r *http.Request) {
	if s.manager == nil {
		http.Error(w, "session manager not available", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	switch r.Method {
	case http.MethodGet:
		ids := []string{}
		for _, sess := range s.manager.FinishedOneShots() {
			ids = append(ids, sess.FullID)
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"count": len(ids), "ids": ids}) //nolint:errcheck
	case http.MethodPost:
		if !s.fedCap(w, r, federation.CapSessionsKill) {
			return
		}
		reaped := s.manager.ReapFinishedOneShots()
		json.NewEncoder(w).Encode(map[string]interface{}{"reaped": reaped}) //nolint:errcheck
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
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

func toQuickCommands(v interface{}) ([]config.QuickCommand, bool) {
	arr, ok := v.([]interface{})
	if !ok {
		return nil, false
	}
	result := make([]config.QuickCommand, 0, len(arr))
	for _, item := range arr {
		m, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		label := toString(m["label"])
		value := toString(m["value"])
		if label == "" || value == "" || label == "<nil>" || value == "<nil>" {
			continue
		}
		category := ""
		if c, ok := m["category"]; ok && c != nil {
			category = toString(c)
			if category == "<nil>" {
				category = ""
			}
		}
		cmd := config.QuickCommand{
			Label:    label,
			Value:    value,
			Category: category,
		}
		result = append(result, cmd)
	}
	return result, len(result) > 0
}

// isSecretFingerprint reports whether fp is a 16- or 64-character hex
// fingerprint (the sha256 of a secret, short or full form).
func isSecretFingerprint(fp string) bool {
	hex := strings.TrimSpace(fp)
	if len(hex) != 16 && len(hex) != 64 {
		return false
	}
	for _, c := range hex {
		if (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F') {
			continue
		}
		return false
	}
	return true
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

// defaultEventsNotifySurface builds the autonomous.notify GET surface.
// The key must ALWAYS be present (even for the zero value) so the PWA
// settings card renders the field; a nil DefaultEvents slice is emitted
// as an empty JSON array rather than null. The config field itself has
// no omitempty for the same reason (see config.AutonomousNotifyConfig
// and the B124 no-omitempty pattern).
func defaultEventsNotifySurface(n config.AutonomousNotifyConfig) map[string]interface{} {
	ev := n.DefaultEvents
	if ev == nil {
		ev = []string{}
	}
	return map[string]interface{}{
		"default_events":     ev,
		"pending_events_max": n.EffectivePendingEventsMax(),
		"webhook":            webhookNotifySurface(n.Webhook),
		"forwarding":         forwardingNotifySurface(n.Forwarding),
	}
}

// ---- Proxy endpoint --------------------------------------------------------

// handleProxy forwards requests to a named remote datawatch server,
// or — when the path starts with "agent/" — to a spawned worker
// container's HTTP API (F10 sprint 3.5).
//
// Route forms:
//
//	/api/proxy/{serverName}/{...path}    → F16 remote-server proxy
//	/api/proxy/agent/{worker_id}/{...}   → S3.5 agent-worker proxy
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
	// BL394 §6 iframe-embed follow-up: moved below serverName extraction
	// so a scoped proxy token can be checked against the specific peer
	// being requested (see checkProxyAuth) — same admin/federation check
	// as before for every other caller.
	if !s.checkProxyAuth(w, r, serverName) {
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

	// Find server config — cfg.Servers first, then runtime store (BL312 S4)
	remote := s.findServer(serverName)
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
	// Forward headers. BL394 §6 iframe-embed follow-up: drop the client's
	// own Accept-Encoding, same reasoning as handleRemotePWA's identical
	// fix -- this path is a raw io.Copy passthrough today so it isn't
	// corrupted by it the way that rewrite path was, but leaving it
	// forwarded here would silently become the same bug the moment any
	// future change adds text-rewriting to this path too. Cheap to close
	// now rather than rely on remembering why later.
	for k, vals := range r.Header {
		if strings.EqualFold(k, "Accept-Encoding") {
			continue
		}
		for _, v := range vals {
			proxyReq.Header.Add(k, v)
		}
	}
	// Inject remote token
	if remote.Token != "" {
		proxyReq.Header.Set("Authorization", "Bearer "+remote.Token)
	}

	client := multiserver.HTTPClient(remote.TLSSkipVerify, 30*time.Second)
	resp, err := client.Do(proxyReq)
	if err != nil {
		http.Error(w, fmt.Sprintf("proxy error: %v", err), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close() //nolint:errcheck

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
	if !s.fedCap(w, r, federation.CapFederationList) {
		return
	}
	type serverInfo struct {
		Name     string `json:"name"`
		URL      string `json:"url"`
		HasAuth  bool   `json:"has_auth"`
		Enabled  bool   `json:"enabled"`
		Hostname string `json:"hostname,omitempty"`
	}
	result := make([]serverInfo, 0, len(s.cfg.Servers)+1)
	// Always include implicit local entry. "local" stays the stable name
	// used for routing/comparison throughout the client (apiFetch's
	// isRemote check, selectServer, etc.) — Hostname is a separate,
	// display-only field so the PWA can show the real machine name
	// instead of the literal word "local" (GH#194: never label the
	// connected server "local").
	result = append(result, serverInfo{
		Name:     "local",
		URL:      fmt.Sprintf("http://localhost:%d", s.cfg.Server.Port),
		HasAuth:  s.cfg.Server.Token != "",
		Enabled:  true,
		Hostname: s.hostname,
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
	if !s.fedCap(w, r, federation.CapHealthRead) {
		return
	}
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
	if r.Method == http.MethodGet {
		if !s.fedCap(w, r, federation.CapConfigRead) {
			return
		}
	} else if !s.fedCap(w, r, federation.CapConfigWrite) {
		return
	}
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
		SessionID    string `json:"session_id"`
		SessionName  string `json:"session_name"`
		Command      string `json:"command"`
		RunAt        string `json:"run_at,omitempty"`       // RFC3339 or "" for on-input
		RunAfterID   string `json:"run_after_id,omitempty"` // chain after another scheduled command
		CronExpr     string `json:"cron_expr"`
		ScheduleName string `json:"schedule_name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	// Resolve session: by name if session_id not provided
	sessionID := req.SessionID
	if sessionID == "" && req.SessionName != "" {
		if sess, ok := s.manager.FindSessionByName(req.SessionName); ok {
			sessionID = sess.FullID
		}
	}
	if sessionID == "" && req.SessionName == "" {
		http.Error(w, "session_id or session_name required", http.StatusBadRequest)
		return
	}
	if req.Command == "" {
		http.Error(w, "command is required", http.StatusBadRequest)
		return
	}
	// Validate cron if provided
	if req.CronExpr != "" {
		if err := session.ValidateCron(req.CronExpr); err != nil {
			http.Error(w, "invalid cron_expr: "+err.Error(), http.StatusBadRequest)
			return
		}
	}
	var runAt time.Time
	if req.CronExpr != "" && req.RunAt == "" {
		var err error
		runAt, err = session.CronNext(req.CronExpr, time.Now())
		if err != nil {
			http.Error(w, "cron next: "+err.Error(), http.StatusInternalServerError)
			return
		}
	} else if req.RunAt != "" {
		var err error
		runAt, err = time.Parse(time.RFC3339, req.RunAt)
		if err != nil {
			http.Error(w, "invalid run_at format (use RFC3339)", http.StatusBadRequest)
			return
		}
	}
	sc, err := s.schedStore.AddFull(session.AddOptions{
		SessionID:    sessionID,
		SessionName:  req.SessionName,
		ScheduleName: req.ScheduleName,
		Command:      req.Command,
		RunAt:        runAt,
		RunAfterID:   req.RunAfterID,
		CronExpr:     req.CronExpr,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.audit(r.Context(), "create", "schedule", sc.ID, map[string]any{"session_id": sessionID, "command": req.Command}) // GH#201 Phase 4
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
	name := r.URL.Query().Get("name")
	if id == "" && name == "" {
		http.Error(w, "id or name query param required", http.StatusBadRequest)
		return
	}
	if name != "" {
		n := s.schedStore.CancelByScheduleName(name)
		if n == 0 {
			http.Error(w, "no pending schedules with that name", http.StatusNotFound)
			return
		}
		s.audit(r.Context(), "cancel", "schedule", name, map[string]any{"count": n}) // GH#201 Phase 4
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"status": "cancelled", "count": n}) //nolint:errcheck
		return
	}
	if err := s.schedStore.Cancel(id); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	s.audit(r.Context(), "cancel", "schedule", id, nil) // GH#201 Phase 4
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "cancelled"}) //nolint:errcheck
}

// handleSchedules provides the enhanced /api/schedules endpoint with deferred session
// support, editing, natural language time parsing, and session filtering.
func (s *Server) handleSchedules(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		if !s.fedCap(w, r, federation.CapConfigRead) {
			return
		}
	} else if !s.fedCap(w, r, federation.CapConfigWrite) {
		return
	}
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
			Type         string `json:"type"`         // "command", "new_session", or "spawn"
			SessionID    string `json:"session_id"`   // for command type
			SessionName  string `json:"session_name"` // BL353 — resolve by name at fire time
			Command      string `json:"command"`      // text to send or task for new session / spawn
			RunAt        string `json:"run_at"`       // natural language or RFC3339
			RunAfterID   string `json:"run_after_id"`
			CronExpr     string `json:"cron_expr"`     // BL353 — 5-field cron expression
			ScheduleName string `json:"schedule_name"` // BL353 — human-readable label
			// For deferred sessions and spawns
			Name       string `json:"name"` // spawned session name
			ProjectDir string `json:"project_dir"`
			Backend    string `json:"backend"`
			// GH#128 — spawn-specific fields
			LLMRef     string `json:"llm_ref"`
			Model      string `json:"model"`
			Effort     string `json:"effort"`
			OneShot    bool   `json:"one_shot"`
			Ephemeral  bool   `json:"ephemeral"`
			Subprocess bool   `json:"subprocess"` // run via bash -c, exit code = completion
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid JSON", http.StatusBadRequest)
			return
		}
		// Validate cron if provided
		if req.CronExpr != "" {
			if err := session.ValidateCron(req.CronExpr); err != nil {
				http.Error(w, "invalid cron_expr: "+err.Error(), http.StatusBadRequest)
				return
			}
		}
		// Parse time (supports natural language)
		var runAt time.Time
		if req.CronExpr != "" && req.RunAt == "" {
			var err error
			runAt, err = session.CronNext(req.CronExpr, time.Now())
			if err != nil {
				http.Error(w, "cron next: "+err.Error(), http.StatusInternalServerError)
				return
			}
		} else if req.RunAt != "" {
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
		switch req.Type {
		case session.SchedTypeSpawn:
			// GH#128 — ephemeral one-shot session spawn, session-independent.
			if req.Command == "" {
				http.Error(w, "command (task) required for spawn", http.StatusBadRequest)
				return
			}
			sc, err = s.schedStore.AddSpawn(session.AddSpawnOptions{
				Task:         req.Command,
				ProjectDir:   req.ProjectDir,
				Backend:      req.Backend,
				LLMRef:       req.LLMRef,
				Model:        req.Model,
				Effort:       req.Effort,
				SessionName:  req.Name,
				ScheduleName: req.ScheduleName,
				CronExpr:     req.CronExpr,
				RunAt:        runAt,
				OneShot:      req.OneShot,
				Ephemeral:    req.Ephemeral,
				Subprocess:   req.Subprocess,
			})
		case session.SchedTypeNewSession:
			if req.Command == "" && req.Name == "" {
				http.Error(w, "task or name required for new session", http.StatusBadRequest)
				return
			}
			sc, err = s.schedStore.AddDeferredSession(req.Name, req.Command, req.ProjectDir, req.Backend, runAt)
		default:
			// "command" type (default when type is empty or "command")
			// Resolve session by name if session_id not provided
			sessionID := req.SessionID
			if sessionID == "" && req.SessionName != "" {
				if sess, ok := s.manager.FindSessionByName(req.SessionName); ok {
					sessionID = sess.FullID
				}
			}
			if sessionID == "" && req.SessionName == "" {
				http.Error(w, "session_id or session_name required", http.StatusBadRequest)
				return
			}
			if req.Command == "" {
				http.Error(w, "command required", http.StatusBadRequest)
				return
			}
			sc, err = s.schedStore.AddFull(session.AddOptions{
				SessionID:    sessionID,
				SessionName:  req.SessionName,
				ScheduleName: req.ScheduleName,
				Command:      req.Command,
				RunAt:        runAt,
				RunAfterID:   req.RunAfterID,
				CronExpr:     req.CronExpr,
			})
		}
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		s.audit(r.Context(), "create", "schedule", sc.ID, map[string]any{"type": req.Type, "command": req.Command}) // GH#201 Phase 4
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
		s.audit(r.Context(), "update", "schedule", req.ID, nil) // GH#201 Phase 4
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "updated"}) //nolint:errcheck

	case http.MethodDelete:
		id := r.URL.Query().Get("id")
		name := r.URL.Query().Get("name")
		all := r.URL.Query().Get("all")
		if id == "" && name == "" && all == "" {
			http.Error(w, "id, name, or all required", http.StatusBadRequest)
			return
		}
		if name != "" {
			// BL353: cancel-by-schedule-name
			n := s.schedStore.CancelByScheduleName(name)
			if n == 0 {
				http.Error(w, fmt.Sprintf("no pending schedules with name %q", name), http.StatusNotFound)
				return
			}
		} else if id != "" {
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
		s.audit(r.Context(), "delete", "schedule", id+name, nil) // GH#201 Phase 4
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "deleted"}) //nolint:errcheck

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// ---- /api/commands --------------------------------------------------------

func (s *Server) handleCommands(w http.ResponseWriter, r *http.Request) {
	if !s.fedCap(w, r, federation.CapSessionsList) {
		return
	}
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
	if r.Method == http.MethodGet {
		if !s.fedCap(w, r, federation.CapConfigRead) {
			return
		}
	} else if !s.fedCap(w, r, federation.CapConfigWrite) {
		return
	}
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
		if !s.fedCap(w, r, federation.CapAlertsList) {
			return
		}
		if alertsPagingRequested(r) {
			s.serveAlertsPage(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		type alertsResponse struct {
			Alerts      []*alerts.Alert `json:"alerts"`
			UnreadCount int             `json:"unread_count"`
		}
		var list []*alerts.Alert
		if src := r.URL.Query().Get("source"); src != "" {
			list = s.alertStore.ListBySource(src)
		} else {
			list = s.alertStore.List()
		}
		json.NewEncoder(w).Encode(alertsResponse{ //nolint:errcheck
			Alerts:      list,
			UnreadCount: s.alertStore.UnreadCount(),
		})
	case http.MethodPost:
		if !s.fedCap(w, r, federation.CapAlertsRead) {
			return
		}
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
		s.broadcastAlertsRead(body.ID, body.All)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"}) //nolint:errcheck
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleAlertCreate serves POST /api/alerts/create — GH#201 Phase 5.
// Gives an external monitor or webhook (anything holding a token with
// CapAlertsWrite — not granted to any built-in group by default, see
// capabilities.go) a real way to raise a high-priority, separately
// routable alert, instead of the two existing dead ends: POST
// /api/alerts only marks alerts read, and /api/channel/notify is
// WS-broadcast only (no push fan-out).
//
// Body: {"level": "info"|"warn"|"error", "title": "...", "body": "..."}
// level defaults to "info" when empty or unrecognized. Delegates to
// alertStore.AddSystem, the SAME call every other system-sourced
// alert (peer health, alert-rule firings) already goes through — its
// existing AddListener callback (main.go) already fans this out to
// SSE/ntfy/UnifiedPush/APNs for every alert regardless of source, so
// no new dispatch plumbing is needed here.
func (s *Server) handleAlertCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.fedCap(w, r, federation.CapAlertsWrite) {
		return
	}
	if s.alertStore == nil {
		http.Error(w, "alert store not available", http.StatusServiceUnavailable)
		return
	}
	var req struct {
		Level string `json:"level"`
		Title string `json:"title"`
		Body  string `json:"body"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	if req.Title == "" {
		http.Error(w, "title required", http.StatusBadRequest)
		return
	}
	var level alerts.Level
	switch alerts.Level(req.Level) {
	case alerts.LevelInfo, "":
		level = alerts.LevelInfo
	case alerts.LevelWarn:
		level = alerts.LevelWarn
	case alerts.LevelError:
		level = alerts.LevelError
	default:
		http.Error(w, "level must be info, warn, or error", http.StatusBadRequest)
		return
	}
	a := s.alertStore.AddSystem(level, req.Title, req.Body)
	s.audit(r.Context(), "create", "alert", a.ID, map[string]any{"level": string(level), "title": req.Title}) // GH#201 Phase 5
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(a) //nolint:errcheck
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
	if !s.fedCap(w, r, federation.CapCommRead) {
		return
	}
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
	// BL264 / v6.11.18 + content-aware in BL265 / v6.11.19 — parse the
	// channel message text for completion / input-needed / blocked
	// signals; transition session state accordingly.
	if s.manager != nil {
		s.manager.MarkChannelActivityFromText(sessionID, text)
	}
	replyData := map[string]interface{}{
		"text":       text,
		"session_id": sessionID,
	}
	raw, _ := json.Marshal(replyData)
	outMsg := WSMessage{Type: MsgChannelReply, Data: raw, Timestamp: time.Now()}
	payload, _ := json.Marshal(outMsg)
	s.hub.broadcastRaw(payload)
}

// handleChannelReply receives replies from claude (via the datawatch MCP channel server)
// and broadcasts them to all connected WebSocket clients and messaging backends.
func (s *Server) handleChannelReply(w http.ResponseWriter, r *http.Request) {
	if !s.fedCap(w, r, federation.CapCommWrite) {
		return
	}
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
	// BL264 / v6.11.18 + content-aware in BL265 / v6.11.19 — parse the
	// channel message text for completion / input-needed / blocked
	// signals; transition session state accordingly.
	if s.manager != nil {
		s.manager.MarkChannelActivityFromText(body.SessionID, body.Text)
	}
	// Broadcast channel_reply to all WS clients.
	replyData := map[string]interface{}{
		"text":       body.Text,
		"session_id": body.SessionID,
	}
	raw, _ := json.Marshal(replyData)
	outMsg := WSMessage{Type: MsgChannelReply, Data: raw, Timestamp: time.Now()}
	payload, _ := json.Marshal(outMsg)
	s.hub.broadcastRaw(payload)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"}) //nolint:errcheck
}

// handleChannelNotify receives notifications from the MCP channel server
// (e.g. permission relay requests) and broadcasts to WS clients.
func (s *Server) handleChannelNotify(w http.ResponseWriter, r *http.Request) {
	if !s.fedCap(w, r, federation.CapCommWrite) {
		return
	}
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
	s.tracePermissionRelay(r.Context(), body.Type, body.RequestID)
	raw, _ := json.Marshal(notifyData)
	outMsg := WSMessage{Type: MsgChannelNotify, Data: raw, Timestamp: time.Now()}
	payload, _ := json.Marshal(outMsg)
	s.hub.broadcastRaw(payload)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"}) //nolint:errcheck
}

// handleChannelReady is called by the MCP channel server once it has connected to
// Claude Code and is ready to receive messages. datawatch uses this callback to
// send the session's initial task (if any) as the first channel message.
// POST /api/channel/ready {"session_id":"...", "port":7433}
func (s *Server) handleChannelReady(w http.ResponseWriter, r *http.Request) {
	if !s.fedCap(w, r, federation.CapCommRead) {
		return
	}
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
	if readySess == nil && body.SessionID == "" {
		// GH#202 — fallback for a session-less registration (e.g. a
		// project-level `.mcp.json` "datawatch" entry with no
		// CLAUDE_SESSION_ID). Only claim a session that doesn't already
		// have a live channel — never guess one away from a session whose
		// bridge is still answering.
		//
		// Sorted newest-first: ListSessions() returns map iteration order
		// (unordered), so the "most recently started" comment this fallback
		// has always carried was never actually true before this sort was
		// added — candidate selection was effectively random among matches.
		sessions := s.manager.ListSessions()
		sort.Slice(sessions, func(i, j int) bool {
			return sessions[i].CreatedAt.After(sessions[j].CreatedAt)
		})
		for _, sess := range sessions {
			// stalled is excluded: it is non-terminal but the process is not
			// reachable, so it can't accept a redelivered message.
			if sess.BackendFamily == "claude-code" &&
				(sess.State == session.StateRunning || sess.State == session.StateWaitingInput) &&
				sess.Hostname == s.hostname &&
				!channelStillAlive(sess) {
				readySess = sess
				break
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")

	// GH#202 — session channel bridges register at `claude mcp add --scope
	// user` (GH#128/v8.12.0: a per-session CLAUDE_CONFIG_DIR broke auth and
	// onboarding worse than this, so --scope user stayed). That means every
	// Claude process on the host loads every session's channel entry, and
	// a stray duplicate still carries the real session_id when it calls
	// ready — only its port is wrong. Only move a session to a new port
	// when its current one has actually gone quiet; otherwise whichever
	// duplicate registers last would silently steal the session out from
	// under its real, still-running bridge.
	if readySess != nil && readySess.ChannelReady && readySess.ChannelPort != 0 &&
		readySess.ChannelPort != port && channelStillAlive(readySess) {
		fmt.Printf("[channel] ignoring ready for %s on port %d — existing port %d is still alive (likely a stray duplicate bridge)\n",
			readySess.FullID, port, readySess.ChannelPort)
		json.NewEncoder(w).Encode(map[string]string{"status": "ignored_existing_alive"}) //nolint:errcheck
		return
	}

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

	// GH#128: ALL OneShot sessions — use send_input to type the task directly at
	// the claude terminal. Channel notifications are injected as LLM conversation
	// messages but empirically do not reliably trigger Claude's LLM; typing via
	// send_input (tmux send-keys) hits the TUI input handler and fires immediately.
	//   !cmd tasks:  wrap with "; echo DATAWATCH_COMPLETE:..." so the reaper fires
	//                when the shell command exits (no LLM involvement needed).
	//   LLM tasks:   type the task text directly; Claude processes it as a user
	//                message, runs the work, and emits DATAWATCH_COMPLETE per the
	//                session CLAUDE.md ("When done: output DATAWATCH_COMPLETE: ...").
	if targetSess.OneShot {
		// Deliver a OneShot task at most once per session. The channel
		// bridge re-POSTs ready every 60 s (readyHeartbeat, so a restarted
		// daemon re-learns its port); without this claim every heartbeat
		// that found the session idle at ❯ re-pasted the whole task
		// (sessions 348b/258d, 2026-10-14: ~80 "[Pasted text #N]" chips).
		// The claim is persisted on the session so it also holds across
		// daemon restarts. ChannelReady/ChannelPort healing above still
		// runs on every heartbeat.
		//
		// Deliberately NOT gated on Claude Code's "Update installed ·
		// /restart to apply" footer: it is a passive notice that stays
		// on screen until the CLI restarts and does not block input, so
		// waiting for it to clear would strand a fresh session's task.
		// See docs/plans/2026-10-14-cli-self-update-dialog-recovery.md.
		if !s.manager.ClaimTaskDelivery(targetSess.FullID) {
			fmt.Printf("[channel] task for %s already delivered — ready re-announce, not re-sending\n", targetSess.FullID)
			json.NewEncoder(w).Encode(map[string]string{"session_id": targetSess.FullID, "status": "already_delivered"}) //nolint:errcheck
			return
		}
		var input string
		if strings.HasPrefix(targetSess.Task, "!") {
			input = targetSess.Task + `; echo "DATAWATCH_COMPLETE: shell task done"`
		} else {
			input = targetSess.Task
		}
		sessID := targetSess.FullID
		go func() {
			// Wait for the strict main-prompt check (❯ input line, no active
			// choice dialog) and type the task. Each attempt waits up to the
			// configured delivery window (60 s default); DeliverOneShotTask
			// marks the task delivered only on success and releases the claim
			// after its final failed attempt.
			attempts := 3 // production defaults used when s.cfg is nil
			window := 60 * time.Second
			if s.cfg != nil {
				attempts = s.cfg.Session.EffectiveTaskDeliveryAttempts()
				window = s.cfg.Session.EffectiveTaskDeliveryWindow()
			}
			if err := s.manager.DeliverOneShotTask(sessID, input, attempts, window, config.TaskDeliveryBackoff); err != nil {
				if errors.Is(err, session.ErrTaskUndelivered) {
					fmt.Printf("[channel] task delivery failed for %s: no main prompt within %s across %d attempts\n", sessID, window, attempts)
				} else {
					fmt.Printf("[channel] task delivery failed for %s after %d attempts (window %s): %v\n", sessID, attempts, window, err)
				}
				return
			}
			fmt.Printf("[channel] task delivered to %s via send_input\n", sessID)
		}()
		// Broadcast task delivery to WS clients for the channel tab
		s.recordChannelHistory(targetSess.FullID, targetSess.Task, "outgoing")
		taskData := map[string]interface{}{
			"text":       targetSess.Task,
			"session_id": targetSess.FullID,
			"direction":  "outgoing",
		}
		taskRaw, _ := json.Marshal(taskData)
		taskMsg := WSMessage{Type: MsgChannelReply, Data: taskRaw, Timestamp: time.Now()}
		taskPayload, _ := json.Marshal(taskMsg)
		s.hub.broadcastRaw(taskPayload)
		json.NewEncoder(w).Encode(map[string]string{"status": "ok", "session_id": targetSess.FullID}) //nolint:errcheck
		return
	}

	// Non-OneShot tasks: forward to the channel bridge via MCP notification so the
	// LLM receives and processes the instruction.
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
	defer resp.Body.Close() //nolint:errcheck

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
	s.hub.broadcastRaw(taskPayload)

	json.NewEncoder(w).Encode(map[string]string{"status": "ok", "session_id": targetSess.FullID}) //nolint:errcheck
}

// handleChannelSend sends a message to the MCP channel server (forwards to claude).
// POST /api/channel/send {"text":"...", "session_id":"..."}
func (s *Server) handleChannelSend(w http.ResponseWriter, r *http.Request) {
	if !s.fedCap(w, r, federation.CapCommWrite) {
		return
	}
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
	// Serialize sends per session: queue the message and wait for typing idle
	// (30 s gap, 90 s deadline) before injecting. Multiple messages that arrive
	// while the operator is typing are batched and sent in arrival order once
	// idle is confirmed; messages after the first in a batch skip the idle wait.
	doSend := func() error {
		url := fmt.Sprintf("http://127.0.0.1:%d/send", channelPort)
		payload, _ := json.Marshal(map[string]string{
			"text":       body.Text,
			"source":     "datawatch",
			"session_id": body.SessionID,
		})
		resp, err := (&http.Client{Timeout: 5 * time.Second}).Post(url, "application/json", strings.NewReader(string(payload)))
		if err != nil {
			return err
		}
		resp.Body.Close() //nolint:errcheck
		return nil
	}

	var sendErr error
	if body.SessionID != "" {
		sendErr = s.manager.QueueChannelSend(body.SessionID, doSend)
	} else {
		sendErr = doSend()
	}
	if sendErr != nil {
		http.Error(w, fmt.Sprintf("channel server unreachable: %v", sendErr), http.StatusBadGateway)
		return
	}

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
	s.hub.broadcastRaw(outPayload)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"}) //nolint:errcheck
}

// handleRestart restarts the daemon in-place via syscall.Exec.
// POST /api/restart
func (s *Server) handleRestart(w http.ResponseWriter, r *http.Request) {
	if !s.fedCap(w, r, federation.CapConfigWrite) {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.restartAvailable() {
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
	force := restartForceRequested(r)
	rep, hasRep := s.restartInterruptions()
	if hasRep && rep.NeedsForce && !force {
		// Running work would be interrupted and the operator did not pass
		// force: refuse, and answer with the read-only report.
		writeRestartRefused(w, rep)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	resp := map[string]interface{}{"status": "restarting"}
	if hasRep {
		// What this restart interrupts, counted before the restart policy runs.
		resp["interrupts"] = rep
		// The wait for running gates happens after this answer, in the restart
		// function. Report what it will wait for, or leave running at once.
		waited, left := 0, 0
		if s.cfg != nil && config.NormalizeGateDrainMinutes(s.cfg.Autonomous.GateDrainMinutes) > 0 {
			waited = rep.RunningGateCnt
		} else {
			left = rep.RunningGateCnt
		}
		resp["gates_waited"] = waited
		resp["gates_interrupted"] = left
	}
	json.NewEncoder(w).Encode(resp) //nolint:errcheck
	s.hub.Broadcast(MsgNotification, map[string]string{"message": "Daemon restarting…"})
	go func() {
		time.Sleep(500 * time.Millisecond)
		// Declare the shutdown as API-initiated so the restarted process
		// records source=api-restart with via in the audit entry +
		// last-shutdown.json (picked up by the daemon's Capture).
		shutdowncause.MarkAPIInitiated("api-restart")
		s.doRestart(force)
	}()
}

// restartForceRequested reports whether a restart request carries the force
// flag, as ?force=true or a JSON body {"force": true}.
func restartForceRequested(r *http.Request) bool {
	switch strings.ToLower(r.URL.Query().Get("force")) {
	case "1", "true", "yes":
		return true
	}
	if r.Body == nil || r.ContentLength == 0 {
		return false
	}
	var body struct {
		Force bool `json:"force"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		return false
	}
	return body.Force
}

// writeRestartRefused answers 409 Conflict with the full read-only interruption
// report, the policy, and how to pass force. Nothing is restarted.
func writeRestartRefused(w http.ResponseWriter, rep autonomous.RestartInterruption) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusConflict)
	json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
		"status":       "refused",
		"error":        "restart refused: running work would be interrupted; pass force to restart anyway",
		"needs_force":  true,
		"how_to_force": `POST /api/restart?force=true, or POST /api/restart with {"force": true}`,
		"interrupts":   rep,
	})
}

// handleQuickCommands (BL209, v5.27.7 — datawatch#28) returns the
// operator-editable quick-commands list. Clients (PWA + Android)
// render the buttons from this response so the set is server-driven
// instead of hardcoded. Falls back to a baseline list when the
// operator hasn't customised `session.quick_commands`.
//
// GET /api/quick_commands
//
//	{
//	  "commands": [
//	    {"label":"yes","value":"yes","category":"system"},
//	    {"label":"Esc","value":"key:Escape","category":"system"},
//	    ...
//	  ],
//	  "source": "config" | "default"
//	}
func (s *Server) handleQuickCommands(w http.ResponseWriter, r *http.Request) {
	if !s.fedCap(w, r, federation.CapSessionsList) {
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	cmds := s.cfg.Session.QuickCommands
	source := "config"
	if len(cmds) == 0 {
		cmds = defaultQuickCommands()
		source = "default"
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"commands": cmds,
		"source":   source,
	})
}

// defaultQuickCommands returns the baseline list — the same set the
// PWA + Android shell hardcoded pre-v5.27.7. Kept in sync with the
// AGENT.md "Major release alias refresh" rule (alongside claude
// models / efforts) so additions land before the next major.
func defaultQuickCommands() []config.QuickCommand {
	return []config.QuickCommand{
		// Common LLM-prompt answers
		{Label: "yes", Value: "yes\n", Category: "system"},
		{Label: "no", Value: "no\n", Category: "system"},
		{Label: "continue", Value: "continue\n", Category: "system"},
		{Label: "skip", Value: "skip\n", Category: "system"},
		{Label: "/exit", Value: "/exit\n", Category: "system"},
		// Tmux navigation keys
		{Label: "Esc", Value: "key:Escape", Category: "keys"},
		{Label: "Tab", Value: "key:Tab", Category: "keys"},
		{Label: "Enter", Value: "key:Enter", Category: "keys"},
		{Label: "↑", Value: "key:Up", Category: "keys"},
		{Label: "↓", Value: "key:Down", Category: "keys"},
		{Label: "←", Value: "key:Left", Category: "keys"},
		{Label: "→", Value: "key:Right", Category: "keys"},
		{Label: "PgUp", Value: "key:PageUp", Category: "keys"},
		{Label: "PgDn", Value: "key:PageDown", Category: "keys"},
		{Label: "Ctrl-b", Value: "key:C-b", Category: "keys"},
	}
}

// handleClaudeModels (v5.27.5) returns the list of model aliases +
// known full names that the operator can pass to `--model`. Static
// list maintained in this handler — operator decision 2026-04-29:
// don't query Anthropic /v1/models at runtime (BL206 frozen). Each
// major datawatch release is responsible for refreshing this list
// against Anthropic's current alias set per the AGENT.md "Major
// release alias refresh" rule.
func (s *Server) handleClaudeModels(w http.ResponseWriter, r *http.Request) {
	if !s.fedCap(w, r, federation.CapLLMsList) {
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"aliases": []map[string]string{
			{"value": "opus", "label": "opus", "description": "Opus 5.5 — most capable, slowest, most expensive"},
			{"value": "sonnet", "label": "sonnet", "description": "Sonnet 5.5 — balanced speed/capability"},
			{"value": "haiku", "label": "haiku", "description": "Haiku 5.5 — fastest + cheapest"},
			{"value": "fable", "label": "fable", "description": "Fable 5.1 — latest, fast-output variant"},
		},
		"full_names": []map[string]string{
			{"value": "claude-opus-5-5", "label": "claude-opus-5-5"},
			{"value": "claude-sonnet-5-5", "label": "claude-sonnet-5-5"},
			{"value": "claude-haiku-5-5", "label": "claude-haiku-5-5"},
			{"value": "claude-fable-5-1", "label": "claude-fable-5-1"},
		},
		"source":          "hardcoded",
		"refresh_cadence": "major-release",
		"note":            "Anthropic /v1/models query is frozen — pass any alias or full model name; claude validates at launch.",
	})
}

// handleClaudeEfforts (v5.27.5) returns the effort levels claude-code
// accepts. Hardcoded list mirroring `claude --help`'s `--effort
// <level>` enum (low | medium | high | xhigh | max).
func (s *Server) handleClaudeEfforts(w http.ResponseWriter, r *http.Request) {
	if !s.fedCap(w, r, federation.CapLLMsList) {
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"levels": []map[string]string{
			{"value": "low", "label": "low — quickest, lightest reasoning"},
			{"value": "medium", "label": "medium — default for most tasks"},
			{"value": "high", "label": "high — extended thinking"},
			{"value": "xhigh", "label": "xhigh — deep reasoning, longer waits"},
			{"value": "max", "label": "max — maximum reasoning depth"},
		},
		"source": "hardcoded",
	})
}

// handleClaudePermissionModes (v5.27.5) returns the permission modes
// claude-code accepts. Mirrors `claude --help`'s --permission-mode
// enum: default | plan | acceptEdits | auto | bypassPermissions | dontAsk.
func (s *Server) handleClaudePermissionModes(w http.ResponseWriter, r *http.Request) {
	if !s.fedCap(w, r, federation.CapLLMsList) {
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"modes": []map[string]string{
			{"value": "", "label": "(default) — claude picks its built-in default"},
			{"value": "plan", "label": "plan — design-only; claude won't write files (use for PRD design / decomposition)"},
			{"value": "acceptEdits", "label": "acceptEdits — auto-approve edits"},
			{"value": "auto", "label": "auto — auto-approve safer ops"},
			{"value": "bypassPermissions", "label": "bypassPermissions — skip every permission check"},
			{"value": "dontAsk", "label": "dontAsk — don't prompt; refuse risky ops"},
			{"value": "default", "label": "default — explicit default mode"},
		},
		"source": "hardcoded",
	})
}

// handleUpdateCheck (datawatch#25, v5.27.4) — read-only update check.
// GET /api/update/check
// Response shape:
//
//	{
//	  "status":          "up_to_date" | "update_available",
//	  "current_version": "5.27.4",
//	  "latest_version":  "5.27.5",
//	}
//
// No download, no install, no restart — exists so mobile + PWA
// clients can implement a "check → confirm → install" UX without
// firing the install on the first call. POST /api/update keeps its
// existing check-and-install-atomically behaviour.
func (s *Server) handleUpdateCheck(w http.ResponseWriter, r *http.Request) {
	if !s.fedCap(w, r, federation.CapConfigRead) {
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.latestVersion == nil {
		http.Error(w, "update not available", http.StatusNotImplemented)
		return
	}
	latest, err := s.latestVersion()
	if err != nil {
		http.Error(w, fmt.Sprintf("version check failed: %v", err), http.StatusInternalServerError)
		return
	}
	status := "update_available"
	if latest == "" || !semverIsNewer(latest, Version) {
		status = "up_to_date"
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{
		"status":          status,
		"current_version": Version,
		"latest_version":  latest,
	})
}

// handleUpdate installs the latest release in the background and restarts the daemon.
// POST /api/update
// Response: {"status":"checking"} immediately; the process restarts on success.
func (s *Server) handleUpdate(w http.ResponseWriter, r *http.Request) {
	if !s.fedCap(w, r, federation.CapConfigWrite) {
		return
	}
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

	if !semverIsNewer(latest, Version) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "up_to_date", "version": Version}) //nolint:errcheck
		return
	}

	// Respond immediately; the goroutine restarts the process after install.
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
		"status":  "installing",
		"version": latest,
		"message": "Downloading v" + latest + "… daemon will restart automatically.",
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
			"message": "[update] Installed v" + latest + ". Daemon restarting…",
		})
		// Give clients 800ms to receive the message before the process exits.
		s.hub.Broadcast(MsgUpdateProgress, map[string]any{
			"version": latest,
			"phase":   "restarting",
		})
		time.Sleep(800 * time.Millisecond)
		if s.restartAvailable() {
			// The restart guard refuses a restart that would interrupt running
			// work without force. The new binary is installed either way; leave
			// the daemon up and say so instead of exiting below.
			if rep, ok := s.restartInterruptions(); ok && rep.NeedsForce {
				s.hub.Broadcast(MsgNotification, map[string]string{
					"message": "[update] Installed v" + latest + ". Restart deferred: running work would be interrupted. Restart with force to apply.",
				})
				return
			}
			// Mark as update-initiated so the cause record shows
			// source=api-restart via=update-restart.
			shutdowncause.MarkAPIInitiated("update-restart")
			s.doRestart(false) // calls os.Exit(0) when it proceeds
		}
		// Fallback if restartFn is nil.
		fmt.Printf("[update] installed v%s — run `datawatch start` to bring the daemon back up\n", latest)
		os.Exit(0)
	}()
}

// ---- /api/tooling — BL219 artifact lifecycle ------------------------------------

// handleToolingStatus — GET /api/tooling/status
//
// Query params:
//
//	project_dir  — absolute path to scan (defaults to session.default_project_dir)
//	backend      — specific backend name; omit for all backends
func (s *Server) handleToolingStatus(w http.ResponseWriter, r *http.Request) {
	if !s.fedCap(w, r, federation.CapConfigRead) {
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	projectDir := r.URL.Query().Get("project_dir")
	if projectDir == "" && s.cfg != nil {
		projectDir = s.cfg.Session.DefaultProjectDir
	}
	if projectDir == "" {
		http.Error(w, "project_dir required", http.StatusBadRequest)
		return
	}

	backend := r.URL.Query().Get("backend")
	if backend != "" {
		st := tooling.QueryStatus(projectDir, backend)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"project_dir": projectDir, "status": st}) //nolint:errcheck
		return
	}
	statuses := tooling.QueryAllStatus(projectDir)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"project_dir": projectDir, "backends": statuses}) //nolint:errcheck
}

// handleToolingGitignore — POST /api/tooling/gitignore
//
// Body (JSON): {"project_dir": "...", "backend": "aider"}
// Appends backend artifact patterns to .gitignore (+ .cfignore/.dockerignore if present).
func (s *Server) handleToolingGitignore(w http.ResponseWriter, r *http.Request) {
	if !s.fedCap(w, r, federation.CapConfigWrite) {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		ProjectDir string `json:"project_dir"`
		Backend    string `json:"backend"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.ProjectDir == "" && s.cfg != nil {
		req.ProjectDir = s.cfg.Session.DefaultProjectDir
	}
	if req.ProjectDir == "" {
		http.Error(w, "project_dir required", http.StatusBadRequest)
		return
	}
	if req.Backend == "" {
		http.Error(w, "backend required", http.StatusBadRequest)
		return
	}
	added, err := tooling.EnsureIgnored(req.ProjectDir, req.Backend)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
		"project_dir":    req.ProjectDir,
		"backend":        req.Backend,
		"patterns_added": added,
	})
}

// handleToolingCleanup — POST /api/tooling/cleanup
//
// Body (JSON): {"project_dir": "...", "backend": "aider"}
// Removes ephemeral backend artifacts from project_dir.
func (s *Server) handleToolingCleanup(w http.ResponseWriter, r *http.Request) {
	if !s.fedCap(w, r, federation.CapConfigWrite) {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		ProjectDir string `json:"project_dir"`
		Backend    string `json:"backend"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.ProjectDir == "" && s.cfg != nil {
		req.ProjectDir = s.cfg.Session.DefaultProjectDir
	}
	if req.ProjectDir == "" {
		http.Error(w, "project_dir required", http.StatusBadRequest)
		return
	}
	if req.Backend == "" {
		http.Error(w, "backend required", http.StatusBadRequest)
		return
	}
	removed := tooling.CleanupArtifacts(req.ProjectDir, req.Backend)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
		"project_dir": req.ProjectDir,
		"backend":     req.Backend,
		"removed":     removed,
	})
}

// ---------------------------------------------------------------------------
// BL241 — Matrix status + test endpoints
// ---------------------------------------------------------------------------

// handleMatrixStatus handles GET /api/matrix/status.
// Returns the current Matrix backend state: enabled, connected MXID,
// homeserver, room, and whether the backend is active.
func (s *Server) handleMatrixStatus(w http.ResponseWriter, r *http.Request) {
	if !s.fedCap(w, r, federation.CapCommRead) {
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	status := map[string]interface{}{
		"enabled":    false,
		"connected":  false,
		"homeserver": "",
		"user_id":    "",
		"room_id":    "",
		"mode":       "bot",
	}
	if s.cfg != nil {
		status["enabled"] = s.cfg.Matrix.Enabled
		status["homeserver"] = s.cfg.Matrix.Homeserver
		status["user_id"] = s.cfg.Matrix.UserID
		status["room_id"] = s.cfg.Matrix.RoomID
		if s.cfg.Matrix.AS.Enabled {
			status["mode"] = "application-service"
		}
	}
	if s.matrixBackend != nil {
		status["connected"] = true
		status["self_id"] = s.matrixBackend.SelfID()
	}
	writeJSONOK(w, status)
}

// handleExitHooks handles GET/POST /api/exit-hooks and PUT/DELETE /api/exit-hooks/{id}.
// BL356 — session crash/exit hooks (restart, notify, cooldown).
func (s *Server) handleExitHooks(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		if !s.fedCap(w, r, federation.CapConfigRead) {
			return
		}
	} else {
		if !s.fedCap(w, r, federation.CapConfigWrite) {
			return
		}
	}
	if s.exitHookStore == nil {
		http.Error(w, `{"error":"exit hook store not available"}`, http.StatusServiceUnavailable)
		return
	}
	// Determine if there is an ID in the path: /api/exit-hooks/{id}
	path := r.URL.Path
	const prefix = "/api/exit-hooks"
	id := ""
	if len(path) > len(prefix)+1 {
		id = path[len(prefix)+1:]
	}

	switch r.Method {
	case http.MethodGet:
		entries := s.exitHookStore.List()
		writeJSONOK(w, entries)

	case http.MethodPost:
		var req struct {
			Name            string `json:"name"`
			Action          string `json:"action"`
			NotifySession   string `json:"notify_session,omitempty"`
			NotifyMessage   string `json:"notify_message,omitempty"`
			CooldownSeconds int    `json:"cooldown_seconds,omitempty"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, `{"error":"invalid body"}`, http.StatusBadRequest)
			return
		}
		if req.Name == "" || req.Action == "" {
			http.Error(w, `{"error":"name and action are required"}`, http.StatusBadRequest)
			return
		}
		if req.Action != "restart" && req.Action != "notify" {
			http.Error(w, `{"error":"action must be 'restart' or 'notify'"}`, http.StatusBadRequest)
			return
		}
		e, err := s.exitHookStore.Add(req.Name, session.ExitHookAction(req.Action),
			req.NotifySession, req.NotifyMessage, req.CooldownSeconds)
		if err != nil {
			http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(e)

	case http.MethodPut, http.MethodPatch:
		if id == "" {
			http.Error(w, `{"error":"id required in path"}`, http.StatusBadRequest)
			return
		}
		var updates map[string]any
		if err := json.NewDecoder(r.Body).Decode(&updates); err != nil {
			http.Error(w, `{"error":"invalid body"}`, http.StatusBadRequest)
			return
		}
		if err := s.exitHookStore.Update(id, updates); err != nil {
			http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusNotFound)
			return
		}
		e, _ := s.exitHookStore.Get(id)
		writeJSONOK(w, e)

	case http.MethodDelete:
		if id == "" {
			http.Error(w, `{"error":"id required in path"}`, http.StatusBadRequest)
			return
		}
		if err := s.exitHookStore.Delete(id); err != nil {
			http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusNotFound)
			return
		}
		writeJSONOK(w, map[string]interface{}{"ok": true})

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleMatrixTest handles POST /api/matrix/test.
// Sends a test message to the configured room and reports success/failure.
func (s *Server) handleMatrixTest(w http.ResponseWriter, r *http.Request) {
	if !s.fedCap(w, r, federation.CapCommWrite) {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.matrixBackend == nil {
		http.Error(w, `{"error":"matrix backend not configured"}`, http.StatusServiceUnavailable)
		return
	}
	var body struct {
		Room    string `json:"room"`
		Message string `json:"message"`
	}
	if r.ContentLength > 0 {
		_ = json.NewDecoder(r.Body).Decode(&body)
	}
	if body.Message == "" {
		body.Message = "datawatch test message"
	}
	if err := s.matrixBackend.Send(body.Room, body.Message); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		_, _ = fmt.Fprintf(w, `{"error":%q}`, err.Error())
		return
	}
	writeJSONOK(w, map[string]interface{}{"ok": true, "message": body.Message})
}

// handleQueue dispatches /api/queue* endpoints (BL357 — durable role-based work queue).
//
// Routes:
//
//	GET    /api/queue           — list items (query: role=, state=)
//	POST   /api/queue/push      — push new item
//	POST   /api/queue/claim     — claim item
//	POST   /api/queue/complete  — complete item
//	POST   /api/queue/fail      — fail item
//	DELETE /api/queue/{id}      — delete item
func (s *Server) handleQueue(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		if !s.fedCap(w, r, federation.CapQueueRead) {
			return
		}
	} else {
		if !s.fedCap(w, r, federation.CapQueueWrite) {
			return
		}
	}
	if s.queueStore == nil {
		http.Error(w, `{"error":"queue store not available"}`, http.StatusServiceUnavailable)
		return
	}

	// Determine sub-path after /api/queue
	sub := strings.TrimPrefix(r.URL.Path, "/api/queue")
	sub = strings.TrimPrefix(sub, "/")

	switch {
	case r.Method == http.MethodGet && sub == "":
		// GET /api/queue — list
		role := r.URL.Query().Get("role")
		state := r.URL.Query().Get("state")
		items := s.queueStore.List(role, state)
		writeJSONOK(w, items)

	case r.Method == http.MethodPost && sub == "push":
		var req struct {
			Role    string         `json:"role"`
			Payload map[string]any `json:"payload"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, `{"error":"invalid JSON"}`, http.StatusBadRequest)
			return
		}
		if req.Role == "" {
			http.Error(w, `{"error":"role is required"}`, http.StatusBadRequest)
			return
		}
		it, err := s.queueStore.Push(req.Role, req.Payload)
		if err != nil {
			http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusInternalServerError)
			return
		}
		writeJSONOK(w, it)

	case r.Method == http.MethodPost && sub == "claim":
		var req struct {
			Role         string `json:"role"`
			ClaimedBy    string `json:"claimed_by"`
			LeaseSeconds int    `json:"lease_seconds"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, `{"error":"invalid JSON"}`, http.StatusBadRequest)
			return
		}
		if req.Role == "" {
			http.Error(w, `{"error":"role is required"}`, http.StatusBadRequest)
			return
		}
		it, err := s.queueStore.Claim(req.Role, req.ClaimedBy, req.LeaseSeconds)
		if err != nil {
			http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusInternalServerError)
			return
		}
		writeJSONOK(w, it)

	case r.Method == http.MethodPost && sub == "complete":
		var req struct {
			ID     string         `json:"id"`
			Result map[string]any `json:"result"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, `{"error":"invalid JSON"}`, http.StatusBadRequest)
			return
		}
		if req.ID == "" {
			http.Error(w, `{"error":"id is required"}`, http.StatusBadRequest)
			return
		}
		if err := s.queueStore.Complete(req.ID, req.Result); err != nil {
			http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusInternalServerError)
			return
		}
		writeJSONOK(w, map[string]interface{}{"ok": true})

	case r.Method == http.MethodPost && sub == "fail":
		var req struct {
			ID    string `json:"id"`
			Error string `json:"error"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, `{"error":"invalid JSON"}`, http.StatusBadRequest)
			return
		}
		if req.ID == "" {
			http.Error(w, `{"error":"id is required"}`, http.StatusBadRequest)
			return
		}
		if err := s.queueStore.Fail(req.ID, req.Error); err != nil {
			http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusInternalServerError)
			return
		}
		writeJSONOK(w, map[string]interface{}{"ok": true})

	case r.Method == http.MethodDelete && sub != "":
		// DELETE /api/queue/{id}
		id := sub
		if err := s.queueStore.Delete(id); err != nil {
			http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusNotFound)
			return
		}
		writeJSONOK(w, map[string]interface{}{"ok": true})

	default:
		http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
	}
}

// handleResultStore dispatches /api/result-store* endpoints (BL360 — structured agent result store).
//
// Routes:
//
//	GET    /api/result-store           — list entries (query: prefix=)
//	GET    /api/result-store/{name}    — get one entry
//	POST   /api/result-store           — put/upsert entry {name, payload, ttl_seconds}
//	DELETE /api/result-store/{name}    — delete entry
func (s *Server) handleResultStore(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/api/result-store":
		if !s.fedCap(w, r, federation.CapResultsList) {
			return
		}
	case r.Method == http.MethodGet:
		if !s.fedCap(w, r, federation.CapResultsRead) {
			return
		}
	default:
		if !s.fedCap(w, r, federation.CapResultsWrite) {
			return
		}
	}
	if s.resultStore == nil {
		http.Error(w, `{"error":"result store not available"}`, http.StatusServiceUnavailable)
		return
	}

	// Determine sub-path after /api/result-store
	sub := strings.TrimPrefix(r.URL.Path, "/api/result-store")
	sub = strings.TrimPrefix(sub, "/")

	switch {
	case r.Method == http.MethodGet && sub == "":
		// GET /api/result-store — list
		prefix := r.URL.Query().Get("prefix")
		entries := s.resultStore.List(prefix)
		writeJSONOK(w, entries)

	case r.Method == http.MethodGet && sub != "":
		// GET /api/result-store/{name}
		name := sub
		entry, ok := s.resultStore.Get(name)
		if !ok {
			http.Error(w, fmt.Sprintf(`{"error":"not found: %s"}`, name), http.StatusNotFound)
			return
		}
		writeJSONOK(w, entry)

	case r.Method == http.MethodPost && sub == "":
		// POST /api/result-store — upsert
		var req struct {
			Name       string         `json:"name"`
			Payload    map[string]any `json:"payload"`
			TTLSeconds int            `json:"ttl_seconds"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, `{"error":"invalid JSON"}`, http.StatusBadRequest)
			return
		}
		if req.Name == "" {
			http.Error(w, `{"error":"name is required"}`, http.StatusBadRequest)
			return
		}
		if req.Payload == nil {
			http.Error(w, `{"error":"payload is required"}`, http.StatusBadRequest)
			return
		}
		entry, err := s.resultStore.Put(req.Name, req.Payload, req.TTLSeconds)
		if err != nil {
			http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusInternalServerError)
			return
		}
		writeJSONOK(w, entry)

	case r.Method == http.MethodDelete && sub != "":
		// DELETE /api/result-store/{name}
		name := sub
		if err := s.resultStore.Delete(name); err != nil {
			http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusNotFound)
			return
		}
		writeJSONOK(w, map[string]interface{}{"ok": true, "deleted": name})

	default:
		http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
	}
}

// handleDiscussionSubs handles the /api/discussion-subs REST surface (BL358).
//
//	GET    /api/discussion-subs           — list all subscriptions
//	POST   /api/discussion-subs           — subscribe {discussion_id, session_name}
//	DELETE /api/discussion-subs/{disc}/{sess} — unsubscribe
func (s *Server) handleDiscussionSubs(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		if !s.fedCap(w, r, federation.CapCommRead) {
			return
		}
	} else {
		if !s.fedCap(w, r, federation.CapCommWrite) {
			return
		}
	}
	if s.discussionSubStore == nil {
		http.Error(w, `{"error":"discussion subscription store not available"}`, http.StatusServiceUnavailable)
		return
	}

	// Determine sub-path after /api/discussion-subs
	sub := strings.TrimPrefix(r.URL.Path, "/api/discussion-subs")
	sub = strings.TrimPrefix(sub, "/")

	switch {
	case r.Method == http.MethodGet && sub == "":
		// GET /api/discussion-subs — list all subscriptions
		subs := s.discussionSubStore.List()
		if subs == nil {
			subs = []session.DiscussionSub{}
		}
		writeJSONOK(w, subs)

	case r.Method == http.MethodPost && sub == "":
		// POST /api/discussion-subs — subscribe
		var req struct {
			DiscussionID string `json:"discussion_id"`
			SessionName  string `json:"session_name"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, `{"error":"invalid JSON"}`, http.StatusBadRequest)
			return
		}
		if req.DiscussionID == "" {
			http.Error(w, `{"error":"discussion_id is required"}`, http.StatusBadRequest)
			return
		}
		if req.SessionName == "" {
			http.Error(w, `{"error":"session_name is required"}`, http.StatusBadRequest)
			return
		}
		if err := s.discussionSubStore.Subscribe(req.DiscussionID, req.SessionName); err != nil {
			http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusInternalServerError)
			return
		}
		writeJSONOK(w, map[string]interface{}{
			"ok":            true,
			"discussion_id": req.DiscussionID,
			"session_name":  req.SessionName,
			"status":        "subscribed",
		})

	case r.Method == http.MethodDelete && sub != "":
		// DELETE /api/discussion-subs/{disc}/{sess}
		// sub has form: {discussion_id}/{session_name}
		parts := strings.SplitN(sub, "/", 2)
		if len(parts) != 2 {
			http.Error(w, `{"error":"path must be /api/discussion-subs/{discussion_id}/{session_name}"}`, http.StatusBadRequest)
			return
		}
		discID := parts[0]
		sessName := parts[1]
		if err := s.discussionSubStore.Unsubscribe(discID, sessName); err != nil {
			http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusInternalServerError)
			return
		}
		writeJSONOK(w, map[string]interface{}{
			"ok":            true,
			"discussion_id": discID,
			"session_name":  sessName,
			"status":        "unsubscribed",
		})

	default:
		http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
	}
}

// parallelKeyPrefix is the REST key prefix for the parallel execution
// knobs (autonomous.parallel.*).
const parallelKeyPrefix = "autonomous.parallel."

// parallelSurface renders the parallel block on the GET /api/config
// surface as effective values, read from the Effective* accessor, never a
// literal.
func parallelSurface(c config.AutonomousParallelConfig) map[string]interface{} {
	return map[string]interface{}{
		"read_only_tasks":       autonomous.EffectiveReadOnlyTasks(c),
		"worktree_per_task":     autonomous.EffectiveWorktreePerTask(c),
		"merge_conflict_policy": autonomous.EffectiveMergeConflictPolicy(c),
		"merge_conflict_model":  autonomous.EffectiveMergeConflictModel(c),
		"merge_retry_seconds":   autonomous.EffectiveMergeRetrySeconds(c),
	}
}

// taskTemplateModeSurface renders the stored task template mode map on the
// GET /api/config surface: nil becomes {} (an empty object, not null), so
// an unset mode map visibly has no per-template overrides and an absent id
// reads its shipped default (llm) via EffectiveTaskTemplateMode.
func taskTemplateModeSurface(m map[string]string) map[string]interface{} {
	out := make(map[string]interface{}, len(m))
	for id, mode := range m {
		out[id] = mode
	}
	return out
}

// taskTemplateModePatchTouched reports whether a PUT /api/config patch sets
// any task template mode key: the whole map under
// autonomous.task_templates.mode, or a dotted form under
// autonomous.task_templates.mode.<id>.
func taskTemplateModePatchTouched(patch map[string]interface{}) bool {
	if _, ok := patch["autonomous.task_templates.mode"]; ok {
		return true
	}
	for k := range patch {
		if id, ok := strings.CutPrefix(k, "autonomous.task_templates.mode."); ok && id != "" {
			return true
		}
	}
	return false
}

// taskTemplateModePatchCandidate merges the stored task template mode map
// with a PUT's entries: the whole object under
// autonomous.task_templates.mode (each value a string) and each dotted key
// under autonomous.task_templates.mode.<id>. A non-object mode map, a
// non-string entry, or an empty template id is an error; the result is the
// map to validate (TaskTemplatesConfig.Validate) and store.
func taskTemplateModePatchCandidate(stored map[string]string, patch map[string]interface{}) (map[string]string, error) {
	candidate := make(map[string]string, len(stored))
	for id, mode := range stored {
		candidate[id] = mode
	}
	if v, ok := patch["autonomous.task_templates.mode"]; ok {
		obj, isObj := v.(map[string]interface{})
		if !isObj {
			return nil, errors.New(`autonomous.task_templates.mode: must be an object of template id to mode ("llm", "daemon" or "off")`)
		}
		for id, mv := range obj {
			mode, isStr := mv.(string)
			if !isStr || id == "" {
				return nil, fmt.Errorf(`autonomous.task_templates.mode[%q]: template id and mode must be non-empty strings ("llm", "daemon" or "off")`, id)
			}
			candidate[id] = mode
		}
	}
	for k, v := range patch {
		id, ok := strings.CutPrefix(k, "autonomous.task_templates.mode.")
		if !ok || id == "" {
			continue
		}
		mode, isStr := v.(string)
		if !isStr {
			return nil, fmt.Errorf(`autonomous.task_templates.mode[%q]: must be a string ("llm", "daemon" or "off")`, id)
		}
		candidate[id] = mode
	}
	return candidate, nil
}

// validateParallelPatch checks every autonomous.parallel.* value present
// in the patch before anything is applied, so one bad value rejects the
// whole PUT rather than being silently skipped.
func validateParallelPatch(patch map[string]interface{}) error {
	if v, ok := patch[parallelKeyPrefix+"read_only_tasks"]; ok {
		if _, isBool := v.(bool); !isBool {
			return fmt.Errorf("%sread_only_tasks: must be a boolean", parallelKeyPrefix)
		}
	}
	if v, ok := patch[parallelKeyPrefix+"worktree_per_task"]; ok {
		if _, isBool := v.(bool); !isBool {
			return fmt.Errorf("%sworktree_per_task: must be a boolean", parallelKeyPrefix)
		}
	}
	if v, ok := patch[parallelKeyPrefix+"merge_conflict_policy"]; ok {
		str, isStr := v.(string)
		if !isStr {
			return fmt.Errorf("%smerge_conflict_policy: must be a string", parallelKeyPrefix)
		}
		if err := config.ValidateMergeConflictPolicy(str); err != nil {
			return err
		}
	}
	if v, ok := patch[parallelKeyPrefix+"merge_conflict_model"]; ok {
		if _, isStr := v.(string); !isStr {
			return fmt.Errorf("%smerge_conflict_model: must be a string", parallelKeyPrefix)
		}
	}
	if v, ok := patch[parallelKeyPrefix+"merge_retry_seconds"]; ok {
		n, whole := wholeNumber(v)
		if !whole {
			return fmt.Errorf("%smerge_retry_seconds: must be a whole number of seconds >= 0 (0 = timer off, default %d)",
				parallelKeyPrefix, config.DefaultMergeRetrySeconds)
		}
		if err := config.ValidateMergeRetrySeconds(&n); err != nil {
			return err
		}
	}
	return nil
}

// applyParallelValue stores one parallel value (validated above in
// handlePutConfig; a value that fails the check here is left unchanged).
// read_only_tasks is a pointer so an explicit false is distinguishable
// from an absent key.
func applyParallelValue(pc *config.AutonomousParallelConfig, field string, v interface{}) {
	switch field {
	case "read_only_tasks":
		if b, ok := v.(bool); ok {
			pc.ReadOnlyTasks = &b
		}
	case "worktree_per_task":
		if b, ok := v.(bool); ok {
			pc.WorktreePerTask = &b
		}
	case "merge_conflict_policy":
		if str, ok := v.(string); ok && config.ValidateMergeConflictPolicy(str) == nil {
			pc.MergeConflictPolicy = str
		}
	case "merge_conflict_model":
		if str, ok := v.(string); ok {
			pc.MergeConflictModel = str
		}
	case "merge_retry_seconds":
		if n, ok := wholeNumber(v); ok && config.ValidateMergeRetrySeconds(&n) == nil {
			pc.MergeRetrySeconds = &n
		}
	}
}

const livenessOverridesKey = "session.liveness_idle_timeout_overrides"

// livenessOverridesForDisplay returns a copy of the overrides map for GET
// /api/config; it is never nil so clients always see an object.
func livenessOverridesForDisplay(m map[string]int) map[string]int {
	out := make(map[string]int, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// livenessOverridesFromValue converts a JSON object with whole-number values
// into the overrides map. ok is false when v is not such an object.
func livenessOverridesFromValue(v interface{}) (map[string]int, bool) {
	switch obj := v.(type) {
	case map[string]interface{}:
		m := make(map[string]int, len(obj))
		for k, raw := range obj {
			n, whole := wholeNumber(raw)
			if !whole {
				return nil, false
			}
			m[k] = n
		}
		return m, true
	case map[string]int:
		m := make(map[string]int, len(obj))
		for k, n := range obj {
			m[k] = n
		}
		return m, true
	}
	return nil, false
}

// validateLivenessPatch checks the two liveness keys in a config patch. It
// also folds the nested form "session.liveness_idle_timeout_overrides.<name>"
// into the object form, so applyConfigPatch only sees one shape. Called
// before anything is applied.
func validateLivenessPatch(patch map[string]interface{}, known []string) error {
	window := 0
	if v, ok := patch["session.liveness_window_seconds"]; ok {
		n, whole := wholeNumber(v)
		if !whole {
			return fmt.Errorf("session.liveness_window_seconds: %v is invalid; use a whole number of seconds >= 0 (0 = default %d)",
				v, config.DefaultLivenessWindowSeconds)
		}
		window = n
	}
	var overrides map[string]int
	prefix := livenessOverridesKey + "."
	var nested []string
	for k := range patch {
		if strings.HasPrefix(k, prefix) && len(k) > len(prefix) {
			nested = append(nested, k)
		}
	}
	if v, ok := patch[livenessOverridesKey]; ok {
		m, isObj := livenessOverridesFromValue(v)
		if !isObj {
			return fmt.Errorf("%s: %v is invalid; use an object of backend name to whole seconds (0 = off)", livenessOverridesKey, v)
		}
		overrides = m
	}
	if len(nested) > 0 {
		if overrides == nil {
			overrides = map[string]int{}
		}
		for _, k := range nested {
			name := strings.TrimPrefix(k, prefix)
			n, whole := wholeNumber(patch[k])
			if !whole {
				return fmt.Errorf("%s: %v is invalid; use whole seconds >= 0 (0 = off)", k, patch[k])
			}
			overrides[name] = n
			delete(patch, k)
		}
		out := make(map[string]interface{}, len(overrides))
		for k, n := range overrides {
			out[k] = float64(n)
		}
		patch[livenessOverridesKey] = out
	}
	return config.ValidateLivenessSettings(window, overrides, known)
}

// SessionMemoryCeiling is the effective memory ceiling shown on the session
// status: the limit in MB, where it came from, and how it is enforced (scope,
// watchdog or none). Warning is true and Reason says why when nothing caps
// verification runs.
type SessionMemoryCeiling struct {
	EffectiveMB     int    `json:"effective_mb"`
	Source          string `json:"source"`
	EnforcementPath string `json:"enforcement_path"`
	Warning         bool   `json:"warning,omitempty"`
	// Reason is "no_settings" (no ceiling settings installed) or
	// "no_enforcement" (host has neither a user scope nor /proc); empty
	// when a ceiling is enforced.
	Reason string `json:"reason,omitempty"`
}

// memoryCeilingProvider is implemented by the autonomous Manager.
type memoryCeilingProvider interface {
	MemoryCeiling() autonomous.MemoryCeilingStatus
}

// sessionMemoryCeiling builds the ceiling block of the session status. It
// returns nil when no autonomous manager is wired, so a status with nothing to
// say carries no block.
func (s *Server) sessionMemoryCeiling() *SessionMemoryCeiling {
	p, ok := s.autonomousMgr.(memoryCeilingProvider)
	if !ok || p == nil {
		return nil
	}
	st := p.MemoryCeiling()
	out := &SessionMemoryCeiling{EffectiveMB: st.EffectiveMB, Source: st.Source, EnforcementPath: st.EnforcementPath}
	switch {
	case st.Source == autonomous.MemoryCeilingSourceNone || st.EffectiveMB <= 0:
		out.Warning, out.Reason = true, "no_settings"
	case st.EnforcementPath == autonomous.VerifyCeilingNone:
		out.Warning, out.Reason = true, "no_enforcement"
	}
	return out
}

// effectiveReleasePath shows the default when autonomous.release_path is unset.
func effectiveReleasePath(v string) string {
	if v == "" {
		return config.ReleasePathDirectPush
	}
	return v
}

// validatePlanRecordsPatch checks the autonomous.plan_records keys in a PUT
// /api/config body. A bad value rejects the whole PUT before anything is
// applied, by the same rules the config file load uses; each refusal names the
// key and the valid values.
func validatePlanRecordsPatch(patch map[string]interface{}) error {
	for _, k := range []string{"autonomous.plan_records.enabled", "autonomous.plan_records.write_through"} {
		if v, ok := patch[k]; ok {
			if _, isBool := v.(bool); !isBool {
				return fmt.Errorf("%s: must be a boolean (true or false)", k)
			}
		}
	}
	if v, ok := patch["autonomous.plan_records.import_conflict_policy"]; ok {
		str, isStr := v.(string)
		if !isStr {
			return fmt.Errorf("autonomous.plan_records.import_conflict_policy: %v is invalid; use \"suffix\", \"rename\" or \"skip\"", v)
		}
		if err := config.ValidatePlanRecordsImportConflictPolicy(str); err != nil {
			return err
		}
	}
	return nil
}

// applyPlanRecordsKey stores one validated autonomous.plan_records key as a
// pointer, so an explicit false or empty text is told apart from an absent key.
func applyPlanRecordsKey(p *config.AutonomousPlanRecordsConfig, k string, v interface{}) {
	switch k {
	case "autonomous.plan_records.enabled":
		if b, ok := v.(bool); ok {
			p.Enabled = &b
		}
	case "autonomous.plan_records.write_through":
		if b, ok := v.(bool); ok {
			p.WriteThrough = &b
		}
	case "autonomous.plan_records.import_conflict_policy":
		if s, ok := v.(string); ok {
			p.ImportConflictPolicy = &s
		}
	}
}

// planRecordsSurface is the effective autonomous.plan_records values GET
// /api/config returns.
func planRecordsSurface(p config.AutonomousPlanRecordsConfig) map[string]interface{} {
	return map[string]interface{}{
		"enabled":                p.EffectiveEnabled(),
		"write_through":          p.EffectiveWriteThrough(),
		"import_conflict_policy": p.EffectiveImportConflictPolicy(),
	}
}
