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
	"io"
	"net"
	"net/http"
	"os"
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
	// memoryAPI provides memory operations (nil when memory disabled)
	memoryAPI MemoryMCP
	// kgAPI provides knowledge graph operations (nil when memory disabled)
	kgAPI KGMCP
	// ollamaHost is the Ollama API URL for stats
	ollamaHost string
	// webPort for internal API calls (config, stats)
	webPort int
	// pipelineAPI provides pipeline operations (nil when not available)
	pipelineAPI PipelineMCP
	// agentAuditPath (BL107) — when set, agent_audit tool reads from
	// this file; when CEF or unset, agent_audit returns an
	// "unavailable" message.
	agentAuditPath string
	agentAuditCEF  bool
}

// SetAgentAuditPath wires the audit file path for the agent_audit
// MCP tool. cef=true marks the file as CEF-formatted (in which case
// the tool refuses to query it — operators should use their SIEM).
func (s *Server) SetAgentAuditPath(path string, cef bool) {
	s.agentAuditPath = path
	s.agentAuditCEF = cef
}

// PipelineMCP is the interface for pipeline operations from MCP tools.
type PipelineMCP interface {
	StartPipeline(spec, projectDir string, taskSpecs []string, maxParallel int) (string, error)
	GetStatus(id string) string
	Cancel(id string) error
	ListAll() string
}

// MemoryMCP is the interface for memory operations from MCP tools.
type MemoryMCP interface {
	Remember(projectDir, text string) (int64, error)
	Search(query string, topK int) ([]map[string]interface{}, error)
	ListRecent(projectDir string, n int) ([]map[string]interface{}, error)
	ListFiltered(projectDir, role, since string, n int) ([]map[string]interface{}, error)
	Delete(id int64) error
	Stats() map[string]interface{}
	Export(w io.Writer) error
	Import(r io.Reader) (int, error)
	WALRecent(n int) []map[string]interface{}
	ListLearnings(projectDir, query string, n int) ([]map[string]interface{}, error)
	// v5.27.0 — mempalace alignment surfaces over stdio MCP.
	SetPinned(id int64, pinned bool) error
	SweepStale(olderThanDays int, dryRun bool) (map[string]interface{}, error)
	SpellCheckText(text string, extra []string) []map[string]interface{}
	ExtractFactsText(text string) []map[string]interface{}
	SchemaVersion() string
}

// KGMCP is the interface for knowledge graph operations from MCP tools.
type KGMCP interface {
	AddTriple(subject, predicate, object, validFrom, source string) (int64, error)
	Invalidate(subject, predicate, object, ended string) error
	QueryEntity(name, asOf string) ([]map[string]interface{}, error)
	Timeline(name string) ([]map[string]interface{}, error)
	Stats() map[string]interface{}
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
	// BL93/BL94 — orphan session reconciliation + import.
	mcpSrv.AddTool(s.toolSessionReconcile(), tracked(s.handleSessionReconcile))
	mcpSrv.AddTool(s.toolSessionImport(), tracked(s.handleSessionImport))
	// BL107 — agent audit query.
	mcpSrv.AddTool(s.toolAgentAudit(), tracked(s.handleAgentAudit))
	mcpSrv.AddTool(s.toolGetAlerts(), tracked(s.handleGetAlerts))
	mcpSrv.AddTool(s.toolMarkAlertRead(), tracked(s.handleMarkAlertRead))
	mcpSrv.AddTool(s.toolRestartDaemon(), tracked(s.handleRestartDaemon))
	mcpSrv.AddTool(s.toolGetVersion(), tracked(s.handleGetVersion))
	mcpSrv.AddTool(s.toolListSavedCommands(), tracked(s.handleListSavedCommands))
	mcpSrv.AddTool(s.toolSendSavedCommand(), tracked(s.handleSendSavedCommand))
	mcpSrv.AddTool(s.toolScheduleAdd(), tracked(s.handleScheduleAdd))
	mcpSrv.AddTool(s.toolScheduleList(), tracked(s.handleScheduleList))
	mcpSrv.AddTool(s.toolScheduleCancel(), tracked(s.handleScheduleCancel))

	// Memory import, learnings, config set
	mcpSrv.AddTool(s.toolMemoryImport(), tracked(s.handleMemoryImport))
	mcpSrv.AddTool(s.toolMemoryLearnings(), tracked(s.handleMemoryLearnings))
	mcpSrv.AddTool(s.toolConfigSet(), tracked(s.handleConfigSet))

	// v5.26.71 — register the rest of the memory surface always-on so
	// `datawatch mcp` (stdio subcommand) surfaces every memory tool
	// in tools/list, not just import + learnings. Closes the mempalace
	// audit partial: "memory_recall / kg_query not in stdio surface".
	// Handlers nil-guard on memoryAPI so subprocess MCP servers
	// without a wired backend return "Memory not enabled." instead
	// of crashing.
	mcpSrv.AddTool(s.toolMemoryRemember(), tracked(s.handleMemoryRemember))
	mcpSrv.AddTool(s.toolMemoryRecall(), tracked(s.handleMemoryRecall))
	mcpSrv.AddTool(s.toolMemoryList(), tracked(s.handleMemoryList))
	mcpSrv.AddTool(s.toolMemoryForget(), tracked(s.handleMemoryForget))
	mcpSrv.AddTool(s.toolMemoryStats(), tracked(s.handleMemoryStats))
	// v5.27.0 — mempalace alignment MCP tools.
	mcpSrv.AddTool(s.toolMemoryPin(), tracked(s.handleMemoryPin))
	mcpSrv.AddTool(s.toolMemorySweep(), tracked(s.handleMemorySweep))
	mcpSrv.AddTool(s.toolMemorySpellCheck(), tracked(s.handleMemorySpellCheck))
	mcpSrv.AddTool(s.toolMemoryExtractFacts(), tracked(s.handleMemoryExtractFacts))
	mcpSrv.AddTool(s.toolMemorySchemaVersion(), tracked(s.handleMemorySchemaVersion))

	// v5.27.8 (BL210) — daemon-MCP coverage gap closures.
	// Operator-flagged 2026-04-29 audit identified ~12 daemon REST
	// endpoints with no MCP equivalent. This block lands the
	// priority subset: memory_wal / memory_test_embedder /
	// memory_wakeup (the three flagged memory gaps) + claude
	// listing endpoints + RTK quartet + daemon_logs. All forward
	// to /api/* via proxyJSON. Bodies live in v5278_gap_closures.go.
	mcpSrv.AddTool(s.toolMemoryWAL(), tracked(s.handleMemoryWAL))
	mcpSrv.AddTool(s.toolMemoryTestEmbedder(), tracked(s.handleMemoryTestEmbedder))
	mcpSrv.AddTool(s.toolMemoryWakeup(), tracked(s.handleMemoryWakeup))
	mcpSrv.AddTool(s.toolClaudeModels(), tracked(s.handleClaudeModels))
	mcpSrv.AddTool(s.toolClaudeEfforts(), tracked(s.handleClaudeEfforts))
	mcpSrv.AddTool(s.toolClaudePermissionModes(), tracked(s.handleClaudePermissionModes))
	mcpSrv.AddTool(s.toolRTKVersion(), tracked(s.handleRTKVersion))
	mcpSrv.AddTool(s.toolRTKCheck(), tracked(s.handleRTKCheck))
	mcpSrv.AddTool(s.toolRTKUpdate(), tracked(s.handleRTKUpdate))
	mcpSrv.AddTool(s.toolRTKDiscover(), tracked(s.handleRTKDiscover))
	mcpSrv.AddTool(s.toolDaemonLogs(), tracked(s.handleDaemonLogs))

	// BL220 (G11/G12/G13) — detection / dns_channel / proxy MCP surface parity.
	// These features were only reachable via the generic config_set tool.
	// Dedicated tools add typed parameters and discoverability.
	mcpSrv.AddTool(s.toolDetectionStatus(), tracked(s.handleDetectionStatus))
	mcpSrv.AddTool(s.toolDetectionConfigGet(), tracked(s.handleDetectionConfigGet))
	mcpSrv.AddTool(s.toolDetectionConfigSet(), tracked(s.handleDetectionConfigSet))
	mcpSrv.AddTool(s.toolDNSChannelConfigGet(), tracked(s.handleDNSChannelConfigGet))
	mcpSrv.AddTool(s.toolDNSChannelConfigSet(), tracked(s.handleDNSChannelConfigSet))
	mcpSrv.AddTool(s.toolProxyConfigGet(), tracked(s.handleProxyConfigGet))
	mcpSrv.AddTool(s.toolProxyConfigSet(), tracked(s.handleProxyConfigSet))

	// v5.27.10 (BL216) — channel bridge introspection.
	mcpSrv.AddTool(s.toolChannelInfo(), tracked(s.handleChannelInfo))

	// F10 sprint 2: Profile management tools.
	// Each takes a `kind` arg ("project"|"cluster") so we share one
	// set of 6 tools instead of 12 near-duplicates.
	mcpSrv.AddTool(s.toolProfileList(), tracked(s.handleProfileList))
	mcpSrv.AddTool(s.toolProfileGet(), tracked(s.handleProfileGet))
	mcpSrv.AddTool(s.toolProfileCreate(), tracked(s.handleProfileCreate))
	mcpSrv.AddTool(s.toolProfileUpdate(), tracked(s.handleProfileUpdate))
	mcpSrv.AddTool(s.toolProfileDelete(), tracked(s.handleProfileDelete))
	mcpSrv.AddTool(s.toolProfileSmoke(), tracked(s.handleProfileSmoke))
	mcpSrv.AddTool(s.toolProfileSetAgentSettings(), tracked(s.handleProfileSetAgentSettings)) // BL251

	// F10 sprint 3: Agent lifecycle tools.
	mcpSrv.AddTool(s.toolAgentSpawn(), tracked(s.handleAgentSpawn))
	mcpSrv.AddTool(s.toolAgentList(), tracked(s.handleAgentList))
	mcpSrv.AddTool(s.toolAgentGet(), tracked(s.handleAgentGet))
	mcpSrv.AddTool(s.toolAgentLogs(), tracked(s.handleAgentLogs))
	mcpSrv.AddTool(s.toolAgentTerminate(), tracked(s.handleAgentTerminate))
	// F10 sprint 3.6: session-on-worker binding.
	mcpSrv.AddTool(s.toolSessionBindAgent(), tracked(s.handleSessionBindAgent))

	// Sprint Sx (v3.7.2) — parity backfill for v3.5–v3.7 endpoints.
	mcpSrv.AddTool(s.toolAsk(), tracked(s.handleAsk))                              // BL34
	mcpSrv.AddTool(s.toolProjectSummary(), tracked(s.handleProjectSummary))        // BL35
	mcpSrv.AddTool(s.toolTemplateList(), tracked(s.handleTemplateList))            // BL5
	mcpSrv.AddTool(s.toolTemplateUpsert(), tracked(s.handleTemplateUpsert))        // BL5
	mcpSrv.AddTool(s.toolTemplateDelete(), tracked(s.handleTemplateDelete))        // BL5
	mcpSrv.AddTool(s.toolProjectList(), tracked(s.handleProjectList))              // BL27
	mcpSrv.AddTool(s.toolProjectUpsert(), tracked(s.handleProjectUpsert))          // BL27
	mcpSrv.AddTool(s.toolProjectAliasDelete(), tracked(s.handleProjectAliasDelete))// BL27
	mcpSrv.AddTool(s.toolSessionRollback(), tracked(s.handleSessionRollback))      // BL29
	mcpSrv.AddTool(s.toolCooldownStatus(), tracked(s.handleCooldownStatus))        // BL30
	mcpSrv.AddTool(s.toolCooldownSet(), tracked(s.handleCooldownSet))              // BL30
	mcpSrv.AddTool(s.toolCooldownClear(), tracked(s.handleCooldownClear))          // BL30
	mcpSrv.AddTool(s.toolSessionsStale(), tracked(s.handleSessionsStale))          // BL40
	mcpSrv.AddTool(s.toolCostSummary(), tracked(s.handleCostSummary))              // BL6
	mcpSrv.AddTool(s.toolCostUsage(), tracked(s.handleCostUsage))                  // BL6
	mcpSrv.AddTool(s.toolCostRates(), tracked(s.handleCostRates))                  // BL6
	mcpSrv.AddTool(s.toolAuditQuery(), tracked(s.handleAuditQuery))                // BL9
	mcpSrv.AddTool(s.toolDiagnose(), tracked(s.handleDiagnose))                    // BL37
	mcpSrv.AddTool(s.toolReload(), tracked(s.handleReload))                        // BL17
	mcpSrv.AddTool(s.toolAnalytics(), tracked(s.handleAnalytics))                  // BL12
	// Sprint S4 (v3.8.0).
	mcpSrv.AddTool(s.toolAssist(), tracked(s.handleAssist))                        // BL42
	mcpSrv.AddTool(s.toolDeviceAliasList(), tracked(s.handleDeviceAliasList))      // BL31
	mcpSrv.AddTool(s.toolDeviceAliasUpsert(), tracked(s.handleDeviceAliasUpsert))  // BL31
	mcpSrv.AddTool(s.toolDeviceAliasDelete(), tracked(s.handleDeviceAliasDelete))  // BL31
	mcpSrv.AddTool(s.toolSplashInfo(), tracked(s.handleSplashInfo))                // BL69
	// Sprint S5 (v3.9.0).
	mcpSrv.AddTool(s.toolRoutingRulesList(), tracked(s.handleRoutingRulesList))    // BL20
	mcpSrv.AddTool(s.toolRoutingRulesTest(), tracked(s.handleRoutingRulesTest))    // BL20
	// Sprint S6 (v3.10.0) — BL24+BL25 autonomous PRD decomposition.
	mcpSrv.AddTool(s.toolAutonomousStatus(), tracked(s.handleAutonomousStatus))
	mcpSrv.AddTool(s.toolAutonomousConfigGet(), tracked(s.handleAutonomousConfigGet))
	mcpSrv.AddTool(s.toolAutonomousConfigSet(), tracked(s.handleAutonomousConfigSet))
	mcpSrv.AddTool(s.toolAutonomousPRDList(), tracked(s.handleAutonomousPRDList))
	mcpSrv.AddTool(s.toolAutonomousPRDCreate(), tracked(s.handleAutonomousPRDCreate))
	mcpSrv.AddTool(s.toolAutonomousPRDGet(), tracked(s.handleAutonomousPRDGet))
	mcpSrv.AddTool(s.toolAutonomousPRDDecompose(), tracked(s.handleAutonomousPRDDecompose))
	mcpSrv.AddTool(s.toolAutonomousPRDRun(), tracked(s.handleAutonomousPRDRun))
	mcpSrv.AddTool(s.toolAutonomousPRDCancel(), tracked(s.handleAutonomousPRDCancel))
	// BL191 (v5.2.0) review/approve gate + templates.
	mcpSrv.AddTool(s.toolAutonomousPRDApprove(), tracked(s.handleAutonomousPRDApprove))
	mcpSrv.AddTool(s.toolAutonomousPRDReject(), tracked(s.handleAutonomousPRDReject))
	mcpSrv.AddTool(s.toolAutonomousPRDRequestRevision(), tracked(s.handleAutonomousPRDRequestRevision))
	mcpSrv.AddTool(s.toolAutonomousPRDEditTask(), tracked(s.handleAutonomousPRDEditTask))
	mcpSrv.AddTool(s.toolAutonomousPRDInstantiate(), tracked(s.handleAutonomousPRDInstantiate))
	mcpSrv.AddTool(s.toolAutonomousPRDSetLLM(), tracked(s.handleAutonomousPRDSetLLM))
	mcpSrv.AddTool(s.toolAutonomousPRDSetTaskLLM(), tracked(s.handleAutonomousPRDSetTaskLLM))
	mcpSrv.AddTool(s.toolAutonomousLearnings(), tracked(s.handleAutonomousLearnings))
	mcpSrv.AddTool(s.toolAutonomousPRDChildren(), tracked(s.handleAutonomousPRDChildren))
	// BL221 (v6.2.0) Phase 3 — scan framework tools.
	mcpSrv.AddTool(s.toolAutonomousScanConfigGet(), tracked(s.handleAutonomousScanConfigGet))
	mcpSrv.AddTool(s.toolAutonomousScanConfigSet(), tracked(s.handleAutonomousScanConfigSet))
	mcpSrv.AddTool(s.toolAutonomousPRDScan(), tracked(s.handleAutonomousPRDScan))
	mcpSrv.AddTool(s.toolAutonomousPRDScanResults(), tracked(s.handleAutonomousPRDScanResults))
	mcpSrv.AddTool(s.toolAutonomousPRDScanFix(), tracked(s.handleAutonomousPRDScanFix))
	mcpSrv.AddTool(s.toolAutonomousPRDScanRules(), tracked(s.handleAutonomousPRDScanRules))
	// BL221 (v6.2.0) Phase 4 — type registry, Guided Mode, skills tools.
	mcpSrv.AddTool(s.toolAutonomousTypeList(), tracked(s.handleAutonomousTypeList))
	mcpSrv.AddTool(s.toolAutonomousTypeRegister(), tracked(s.handleAutonomousTypeRegister))
	mcpSrv.AddTool(s.toolAutonomousPRDSetType(), tracked(s.handleAutonomousPRDSetType))
	mcpSrv.AddTool(s.toolAutonomousPRDSetGuidedMode(), tracked(s.handleAutonomousPRDSetGuidedMode))
	mcpSrv.AddTool(s.toolAutonomousPRDSetSkills(), tracked(s.handleAutonomousPRDSetSkills))
	// BL221 (v6.2.0) Phase 5 — template store CRUD tools.
	mcpSrv.AddTool(s.toolAutonomousTemplateList(), tracked(s.handleAutonomousTemplateList))
	mcpSrv.AddTool(s.toolAutonomousTemplateCreate(), tracked(s.handleAutonomousTemplateCreate))
	mcpSrv.AddTool(s.toolAutonomousTemplateGet(), tracked(s.handleAutonomousTemplateGet))
	mcpSrv.AddTool(s.toolAutonomousTemplateUpdate(), tracked(s.handleAutonomousTemplateUpdate))
	mcpSrv.AddTool(s.toolAutonomousTemplateDelete(), tracked(s.handleAutonomousTemplateDelete))
	mcpSrv.AddTool(s.toolAutonomousTemplateInstantiate(), tracked(s.handleAutonomousTemplateInstantiate))
	mcpSrv.AddTool(s.toolAutonomousPRDCloneToTemplate(), tracked(s.handleAutonomousPRDCloneToTemplate))
	// Sprint S7 (v3.11.0) — BL33 plugin framework.
	mcpSrv.AddTool(s.toolPluginsList(), tracked(s.handlePluginsList))
	mcpSrv.AddTool(s.toolPluginsReload(), tracked(s.handlePluginsReload))
	mcpSrv.AddTool(s.toolPluginGet(), tracked(s.handlePluginGet))
	mcpSrv.AddTool(s.toolPluginEnable(), tracked(s.handlePluginEnable))
	mcpSrv.AddTool(s.toolPluginDisable(), tracked(s.handlePluginDisable))
	mcpSrv.AddTool(s.toolPluginTest(), tracked(s.handlePluginTest))
	// BL244 (v6.3.0) — Manifest v2.1 CLI subcommand runner.
	mcpSrv.AddTool(s.toolPluginRunSubcommand(), tracked(s.handlePluginRunSubcommand))
	// BL242 (v6.4.0) — centralized secrets manager.
	mcpSrv.AddTool(s.toolSecretList(), tracked(s.handleSecretList))
	mcpSrv.AddTool(s.toolSecretGet(), tracked(s.handleSecretGet))
	mcpSrv.AddTool(s.toolSecretSet(), tracked(s.handleSecretSet))
	mcpSrv.AddTool(s.toolSecretDelete(), tracked(s.handleSecretDelete))
	mcpSrv.AddTool(s.toolSecretExists(), tracked(s.handleSecretExists))
	// BL255 (v6.7.0) — skills registry + sync (PAI default).
	mcpSrv.AddTool(s.toolSkillsRegistryList(), tracked(s.handleSkillsRegistryList))
	mcpSrv.AddTool(s.toolSkillsRegistryGet(), tracked(s.handleSkillsRegistryGet))
	mcpSrv.AddTool(s.toolSkillsRegistryCreate(), tracked(s.handleSkillsRegistryCreate))
	mcpSrv.AddTool(s.toolSkillsRegistryUpdate(), tracked(s.handleSkillsRegistryUpdate))
	mcpSrv.AddTool(s.toolSkillsRegistryDelete(), tracked(s.handleSkillsRegistryDelete))
	mcpSrv.AddTool(s.toolSkillsRegistryAddDefault(), tracked(s.handleSkillsRegistryAddDefault))
	mcpSrv.AddTool(s.toolSkillsRegistryConnect(), tracked(s.handleSkillsRegistryConnect))
	mcpSrv.AddTool(s.toolSkillsRegistryAvailable(), tracked(s.handleSkillsRegistryAvailable))
	mcpSrv.AddTool(s.toolSkillsRegistrySync(), tracked(s.handleSkillsRegistrySync))
	mcpSrv.AddTool(s.toolSkillsRegistryUnsync(), tracked(s.handleSkillsRegistryUnsync))
	mcpSrv.AddTool(s.toolSkillsList(), tracked(s.handleSkillsList))
	mcpSrv.AddTool(s.toolSkillsGet(), tracked(s.handleSkillsGet))
	mcpSrv.AddTool(s.toolSkillLoad(), tracked(s.handleSkillLoad))
	// BL257 P1 (v6.8.0) — operator identity / Telos.
	mcpSrv.AddTool(s.toolIdentityGet(), tracked(s.handleIdentityGet))
	mcpSrv.AddTool(s.toolIdentitySet(), tracked(s.handleIdentitySet))
	mcpSrv.AddTool(s.toolIdentityUpdate(), tracked(s.handleIdentityUpdate))
	mcpSrv.AddTool(s.toolIdentityConfigure(), tracked(s.handleIdentityConfigure)) // BL257 P2 v6.8.1
	// BL243 (v6.5.0+) — Tailscale k8s sidecar.
	mcpSrv.AddTool(s.toolTailscaleStatus(), tracked(s.handleTailscaleStatus))
	mcpSrv.AddTool(s.toolTailscaleNodes(), tracked(s.handleTailscaleNodes))
	mcpSrv.AddTool(s.toolTailscaleACLPush(), tracked(s.handleTailscaleACLPush))
	mcpSrv.AddTool(s.toolTailscaleACLGenerate(), tracked(s.handleTailscaleACLGenerate)) // Phase 3
	mcpSrv.AddTool(s.toolTailscaleAuthKey(), tracked(s.handleTailscaleAuthKey))         // Phase 2
	// Sprint S9 (v4.1.0) — BL171 datawatch-observer.
	mcpSrv.AddTool(s.toolObserverStats(), tracked(s.handleObserverStats))
	mcpSrv.AddTool(s.toolObserverEnvelopes(), tracked(s.handleObserverEnvelopesMCP))
	mcpSrv.AddTool(s.toolObserverEnvelopesAllPeers(), tracked(s.handleObserverEnvelopesAllPeers))
	mcpSrv.AddTool(s.toolObserverEnvelope(), tracked(s.handleObserverEnvelope))
	mcpSrv.AddTool(s.toolObserverConfigGet(), tracked(s.handleObserverConfigGet))
	mcpSrv.AddTool(s.toolObserverConfigSet(), tracked(s.handleObserverConfigSet))
	// BL172 (S11) — peer registry parity.
	mcpSrv.AddTool(s.toolObserverPeersList(), tracked(s.handleObserverPeersList))
	mcpSrv.AddTool(s.toolObserverPeerGet(), tracked(s.handleObserverPeerGet))
	mcpSrv.AddTool(s.toolObserverPeerStats(), tracked(s.handleObserverPeerStats))
	mcpSrv.AddTool(s.toolObserverPeerRegister(), tracked(s.handleObserverPeerRegister))
	mcpSrv.AddTool(s.toolObserverPeerDelete(), tracked(s.handleObserverPeerDelete))
	// S13 — agent-flavoured aliases.
	mcpSrv.AddTool(s.toolObserverAgentStats(), tracked(s.handleObserverAgentStats))
	mcpSrv.AddTool(s.toolObserverAgentList(), tracked(s.handleObserverAgentList))
	// Sprint S8 (v4.0.0) — BL117 PRD-DAG orchestrator.
	mcpSrv.AddTool(s.toolOrchestratorConfigGet(), tracked(s.handleOrchestratorConfigGet))
	mcpSrv.AddTool(s.toolOrchestratorConfigSet(), tracked(s.handleOrchestratorConfigSet))
	mcpSrv.AddTool(s.toolOrchestratorGraphList(), tracked(s.handleOrchestratorGraphList))
	mcpSrv.AddTool(s.toolOrchestratorGraphCreate(), tracked(s.handleOrchestratorGraphCreate))
	mcpSrv.AddTool(s.toolOrchestratorGraphGet(), tracked(s.handleOrchestratorGraphGet))
	mcpSrv.AddTool(s.toolOrchestratorGraphPlan(), tracked(s.handleOrchestratorGraphPlan))
	mcpSrv.AddTool(s.toolOrchestratorGraphRun(), tracked(s.handleOrchestratorGraphRun))
	mcpSrv.AddTool(s.toolOrchestratorGraphCancel(), tracked(s.handleOrchestratorGraphCancel))
	mcpSrv.AddTool(s.toolOrchestratorVerdicts(), tracked(s.handleOrchestratorVerdicts))

	// Pipeline tools
	mcpSrv.AddTool(s.toolPipelineStart(), tracked(s.handlePipelineStart))
	mcpSrv.AddTool(s.toolPipelineStatus(), tracked(s.handlePipelineStatus))
	mcpSrv.AddTool(s.toolPipelineCancel(), tracked(s.handlePipelineCancel))
	mcpSrv.AddTool(s.toolPipelineList(), tracked(s.handlePipelineList))

	// Management tools (always available)
	mcpSrv.AddTool(s.toolDeleteSession(), tracked(s.handleDeleteSession))
	mcpSrv.AddTool(s.toolRestartSession(), tracked(s.handleRestartSession))
	mcpSrv.AddTool(s.toolGetStats(), tracked(s.handleGetStats))
	mcpSrv.AddTool(s.toolGetConfig(), tracked(s.handleGetConfig))

	// v6.0.4 (BL210-remaining) — detection filters, backends, session state,
	// federation sessions, device registry, file browser.
	mcpSrv.AddTool(s.toolFilterList(), tracked(s.handleFilterList))
	mcpSrv.AddTool(s.toolFilterAdd(), tracked(s.handleFilterAdd))
	mcpSrv.AddTool(s.toolFilterDelete(), tracked(s.handleFilterDelete))
	mcpSrv.AddTool(s.toolFilterToggle(), tracked(s.handleFilterToggle))
	mcpSrv.AddTool(s.toolBackendsList(), tracked(s.handleBackendsList))
	mcpSrv.AddTool(s.toolBackendsActive(), tracked(s.handleBackendsActive))
	mcpSrv.AddTool(s.toolSessionSetState(), tracked(s.handleSessionSetState))
	mcpSrv.AddTool(s.toolFederationSessions(), tracked(s.handleFederationSessions))
	mcpSrv.AddTool(s.toolDeviceRegister(), tracked(s.handleDeviceRegister))
	mcpSrv.AddTool(s.toolDeviceList(), tracked(s.handleDeviceList))
	mcpSrv.AddTool(s.toolDeviceDelete(), tracked(s.handleDeviceDelete))
	mcpSrv.AddTool(s.toolFilesList(), tracked(s.handleFilesList))
	// v6.0.8 (BL219) — tooling artifact lifecycle.
	mcpSrv.AddTool(s.toolToolingStatus(), tracked(s.handleToolingStatus))
	mcpSrv.AddTool(s.toolToolingGitignore(), tracked(s.handleToolingGitignore))
	mcpSrv.AddTool(s.toolToolingCleanup(), tracked(s.handleToolingCleanup))

	s.srv = mcpSrv
	return s
}

// SetWebPort sets the web server port for internal API calls.
func (s *Server) SetWebPort(port int) { s.webPort = port }

// SetPipelineAPI wires the pipeline executor for MCP tools.
func (s *Server) SetPipelineAPI(api PipelineMCP) { s.pipelineAPI = api }

// SetMemoryAPI wires the memory system into the MCP server and registers memory tools.
func (s *Server) SetMemoryAPI(api MemoryMCP) {
	s.memoryAPI = api
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
				if err != nil { s.chanStats.RecordError() }
			}
			return result, err
		}
	}
	// v5.26.71 — these are now registered in mcp.New() so the stdio
	// subcommand surfaces them in tools/list. Re-registering here is
	// a no-op (mcp-go's AddTool overwrites; same handlers either way).
	// Kept as a guarded re-wire so future adapter swaps (e.g. an
	// HTTP-backed memoryMCP for stdio subprocesses) take effect on
	// SetMemoryAPI without losing the always-on surface.
	s.srv.AddTool(s.toolMemoryRemember(), tracked(s.handleMemoryRemember))
	s.srv.AddTool(s.toolMemoryRecall(), tracked(s.handleMemoryRecall))
	s.srv.AddTool(s.toolMemoryList(), tracked(s.handleMemoryList))
	s.srv.AddTool(s.toolMemoryForget(), tracked(s.handleMemoryForget))
	s.srv.AddTool(s.toolMemoryStats(), tracked(s.handleMemoryStats))
	s.srv.AddTool(s.toolCopyResponse(), tracked(s.handleCopyResponse))
	s.srv.AddTool(s.toolGetPrompt(), tracked(s.handleGetPrompt))
	s.srv.AddTool(s.toolMemoryReindex(), tracked(s.handleMemoryReindex))
	s.srv.AddTool(s.toolResearchSessions(), tracked(s.handleResearchSessions))
	s.srv.AddTool(s.toolMemoryExport(), tracked(s.handleMemoryExport))
}

// SetKGAPI wires the knowledge graph into the MCP server and registers KG tools.
func (s *Server) SetKGAPI(api KGMCP) {
	s.kgAPI = api
	tracked := func(fn func(context.Context, mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error)) func(context.Context, mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
		return func(ctx context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
			result, err := fn(ctx, req)
			if s.chanStats != nil {
				reqSize := len(fmt.Sprintf("%v", req.Params.Arguments))
				respSize := 0
				if result != nil {
					for _, c := range result.Content {
						if tc, ok := c.(mcpsdk.TextContent); ok { respSize += len(tc.Text) }
					}
				}
				s.chanStats.RecordRecv(reqSize)
				s.chanStats.RecordSent(respSize)
				if err != nil { s.chanStats.RecordError() }
			}
			return result, err
		}
	}
	s.srv.AddTool(s.toolKGQuery(), tracked(s.handleKGQuery))
	s.srv.AddTool(s.toolKGAdd(), tracked(s.handleKGAdd))
	s.srv.AddTool(s.toolKGInvalidate(), tracked(s.handleKGInvalidate))
	s.srv.AddTool(s.toolKGTimeline(), tracked(s.handleKGTimeline))
	s.srv.AddTool(s.toolKGStats(), tracked(s.handleKGStats))
}

// SetOllamaHost enables the ollama_stats MCP tool.
func (s *Server) SetOllamaHost(host string) {
	s.ollamaHost = host
	if host != "" {
		s.srv.AddTool(s.toolOllamaStats(), func(ctx context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
			return s.handleOllamaStats(ctx, req)
		})
	}
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
		{s.toolSessionReconcile, "session_reconcile"},
		{s.toolSessionImport, "session_import"},
		{s.toolAgentAudit, "agent_audit"},
		{s.toolGetAlerts, "get_alerts"},
		{s.toolMarkAlertRead, "mark_alert_read"},
		{s.toolRestartDaemon, "restart_daemon"},
		{s.toolGetVersion, "get_version"},
		{s.toolListSavedCommands, "list_saved_commands"},
		{s.toolSendSavedCommand, "send_saved_command"},
		{s.toolScheduleAdd, "schedule_add"},
		{s.toolScheduleList, "schedule_list"},
		{s.toolScheduleCancel, "schedule_cancel"},
		{s.toolMemoryImport, "memory_import"},
		{s.toolMemoryLearnings, "memory_learnings"},
		{s.toolConfigSet, "config_set"},
		{s.toolProfileList, "profile_list"},
		{s.toolProfileGet, "profile_get"},
		{s.toolProfileCreate, "profile_create"},
		{s.toolProfileUpdate, "profile_update"},
		{s.toolProfileDelete, "profile_delete"},
		{s.toolProfileSmoke, "profile_smoke"},
		{s.toolAgentSpawn, "agent_spawn"},
		{s.toolAgentList, "agent_list"},
		{s.toolAgentGet, "agent_get"},
		{s.toolAgentLogs, "agent_logs"},
		{s.toolAgentTerminate, "agent_terminate"},
		{s.toolSessionBindAgent, "session_bind_agent"},
		{s.toolPipelineStart, "pipeline_start"},
		{s.toolPipelineStatus, "pipeline_status"},
		{s.toolPipelineCancel, "pipeline_cancel"},
		{s.toolPipelineList, "pipeline_list"},
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
//
// v5.26.71 — passing nil readers/writers panicked in mark3labs/mcp-go's
// bufio.NewReader path. Pass os.Stdin/os.Stdout explicitly so the
// stdio entrypoint handles its first message instead of segfaulting
// on EOF. Caught by scripts/release-smoke-stdio-mcp.sh.
func (s *Server) ServeStdio(ctx context.Context) error {
	return server.NewStdioServer(s.srv).Listen(ctx, os.Stdin, os.Stdout)
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
		mcpsdk.WithString("source",
			mcpsdk.Description("Filter alerts by source (e.g. 'system' for pipeline/plugin/ebpf failures)"),
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
	filterSrc := req.GetString("source", "")

	var all []*alerts.Alert
	if filterSrc != "" {
		all = s.alertStore.ListBySource(filterSrc)
	} else {
		all = s.alertStore.List()
	}
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
		srcLabel := ""
		if a.Source != "" {
			srcLabel = fmt.Sprintf(" [%s]", a.Source)
		}
		sessLabel := ""
		if a.SessionID != "" {
			sessLabel = fmt.Sprintf(" [%s]", a.SessionID)
		}
		sb.WriteString(fmt.Sprintf("[%s] %s %s%s%s%s — %s\n  %s\n\n",
			a.ID,
			a.CreatedAt.Format("15:04:05"),
			strings.ToUpper(string(a.Level)),
			srcLabel,
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

// ---- Pipeline tools --------------------------------------------------------

func (s *Server) toolPipelineStart() mcpsdk.Tool {
	return mcpsdk.NewTool("pipeline_start",
		mcpsdk.WithDescription("Start a pipeline of chained tasks. Tasks run in dependency order with parallelism."),
		mcpsdk.WithString("spec",
			mcpsdk.Required(),
			mcpsdk.Description("Pipeline spec: 'task1 -> task2 -> task3'"),
		),
		mcpsdk.WithString("project_dir",
			mcpsdk.Description("Project directory (defaults to configured default)"),
		),
		mcpsdk.WithNumber("max_parallel",
			mcpsdk.Description("Max parallel tasks (default: 3)"),
		),
	)
}

func (s *Server) toolPipelineStatus() mcpsdk.Tool {
	return mcpsdk.NewTool("pipeline_status",
		mcpsdk.WithDescription("Get status of a pipeline by ID."),
		mcpsdk.WithString("id",
			mcpsdk.Required(),
			mcpsdk.Description("Pipeline ID"),
		),
	)
}

func (s *Server) toolPipelineCancel() mcpsdk.Tool {
	return mcpsdk.NewTool("pipeline_cancel",
		mcpsdk.WithDescription("Cancel a running pipeline."),
		mcpsdk.WithString("id",
			mcpsdk.Required(),
			mcpsdk.Description("Pipeline ID"),
		),
	)
}

func (s *Server) toolPipelineList() mcpsdk.Tool {
	return mcpsdk.NewTool("pipeline_list",
		mcpsdk.WithDescription("List all pipelines and their status."),
	)
}

func (s *Server) handlePipelineStart(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	if s.pipelineAPI == nil {
		return mcpsdk.NewToolResultText("Pipeline system not available."), nil
	}
	spec := req.GetString("spec", "")
	if spec == "" {
		return mcpsdk.NewToolResultText("Error: spec is required"), nil
	}
	projectDir := req.GetString("project_dir", "")
	maxParallel := req.GetInt("max_parallel", 3)
	id, err := s.pipelineAPI.StartPipeline(spec, projectDir, nil, maxParallel)
	if err != nil {
		return mcpsdk.NewToolResultText(fmt.Sprintf("Error: %v", err)), nil
	}
	return mcpsdk.NewToolResultText(fmt.Sprintf("Pipeline started: %s", id)), nil
}

func (s *Server) handlePipelineStatus(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	if s.pipelineAPI == nil {
		return mcpsdk.NewToolResultText("Pipeline system not available."), nil
	}
	id := req.GetString("id", "")
	if id == "" {
		return mcpsdk.NewToolResultText("Error: id is required"), nil
	}
	return mcpsdk.NewToolResultText(s.pipelineAPI.GetStatus(id)), nil
}

func (s *Server) handlePipelineCancel(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	if s.pipelineAPI == nil {
		return mcpsdk.NewToolResultText("Pipeline system not available."), nil
	}
	id := req.GetString("id", "")
	if id == "" {
		return mcpsdk.NewToolResultText("Error: id is required"), nil
	}
	if err := s.pipelineAPI.Cancel(id); err != nil {
		return mcpsdk.NewToolResultText(fmt.Sprintf("Error: %v", err)), nil
	}
	return mcpsdk.NewToolResultText(fmt.Sprintf("Pipeline %s cancelled.", id)), nil
}

func (s *Server) handlePipelineList(_ context.Context, _ mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	if s.pipelineAPI == nil {
		return mcpsdk.NewToolResultText("Pipeline system not available."), nil
	}
	return mcpsdk.NewToolResultText(s.pipelineAPI.ListAll()), nil
}
