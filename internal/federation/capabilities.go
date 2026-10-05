// Package federation implements capability-based access control (CBAC) for
// federation peers. A peer's Capabilities list (stored in multiserver.Entry)
// is a mix of built-in group names (e.g. "session-operator") and/or
// individual surface:action strings (e.g. "sessions:input"). Resolve()
// expands groups to their constituent actions; Check() tests a single action
// against the resolved set.
//
// The same capability model is intentionally designed to extend to multi-user
// support in a future release — UserEntry will carry the same Capabilities
// field.
package federation

import "strings"

// Capability constants follow the surface:action pattern.
// Surfaces map to logical API areas; actions are coarse-grained by design.
const (
	// sessions
	CapSessionsList  = "sessions:list"
	CapSessionsRead  = "sessions:read"
	CapSessionsWrite = "sessions:write"
	CapSessionsKill  = "sessions:kill"
	CapSessionsInput = "sessions:input"

	// agents
	CapAgentsList      = "agents:list"
	CapAgentsRead      = "agents:read"
	CapAgentsSpawn     = "agents:spawn"
	CapAgentsTerminate = "agents:terminate"

	// observers / peers
	CapObserversList  = "observers:list"
	CapObserversRead  = "observers:read"
	CapObserversWrite = "observers:write"

	// LLM registry
	CapLLMsList  = "llms:list"
	CapLLMsRead  = "llms:read"
	CapLLMsWrite = "llms:write"

	// compute nodes
	CapComputeList  = "compute:list"
	CapComputeRead  = "compute:read"
	CapComputeWrite = "compute:write"

	// analytics + cost
	CapAnalyticsRead = "analytics:read"
	// BL394 (docs/plans/2026-10-03-bl394-security-findings-review.md §3b
	// addendum) — smoke-run reporting (POST/PUT/DELETE
	// /api/smoke/progress, PUT /api/smoke/forward-url) was previously
	// gated by CapAnalyticsRead alone, a capability handed to broadly-
	// distributed read-only presets (monitor, analytics-viewer,
	// read-only). This is the write-side split those write paths now use.
	CapAnalyticsWrite = "analytics:write"

	// health
	CapHealthRead = "health:read"

	// config
	CapConfigRead  = "config:read"
	CapConfigWrite = "config:write"

	// secrets
	CapSecretsList  = "secrets:list"
	CapSecretsRead  = "secrets:read"
	CapSecretsWrite = "secrets:write"

	// pipelines
	CapPipelinesList   = "pipelines:list"
	CapPipelinesRead   = "pipelines:read"
	CapPipelinesStart  = "pipelines:start"
	CapPipelinesCancel = "pipelines:cancel"

	// autonomous PRDs
	CapAutonomousList  = "autonomous:list"
	CapAutonomousRead  = "autonomous:read"
	CapAutonomousWrite = "autonomous:write"
	CapAutonomousRun   = "autonomous:run"

	// council
	CapCouncilList = "council:list"
	CapCouncilRead = "council:read"
	CapCouncilRun  = "council:run"

	// federation (note: write intentionally excluded from peer defaults)
	CapFederationList  = "federation:list"
	CapFederationRead  = "federation:read"
	CapFederationWrite = "federation:write"
	// CapFederationSelf (SEC-009) — a peer reading only its own registered
	// entry (GET /api/federation/peers/self), distinct from CapFederationList/
	// Read which can enumerate every other peer's entry too. Part of the
	// federation-peer default group; List/Read are not, as of v9.0.0.
	CapFederationSelf = "federation:self"

	// docs
	CapDocsRead = "docs:read"

	// audit log
	CapAuditRead = "audit:read"

	// comm channels
	CapCommRead  = "comm:read"
	CapCommWrite = "comm:write"

	// alerts
	CapAlertsList = "alerts:list"
	CapAlertsRead = "alerts:read"

	// dashboard
	CapDashboardRead  = "dashboard:read"
	CapDashboardWrite = "dashboard:write"

	// durable work queue (BL357) — Design A2 audit (v8.39.24): handleQueue
	// had zero capability check of any kind until this was added.
	CapQueueRead  = "queue:read"
	CapQueueWrite = "queue:write"

	// structured agent result store (BL360) — Design A2 audit (v8.39.24):
	// handleResultStore had zero capability check of any kind until this
	// was added.
	CapResultsList  = "results:list"
	CapResultsRead  = "results:read"
	CapResultsWrite = "results:write"

	// memory system — Design A3 (v8.39.25): the memory_* MCP tools (recall,
	// remember, scopes, discussion log, etc.) had no capability surface of
	// any kind before this; needed to scope the session-default group.
	CapMemoryRead  = "memory:read"
	CapMemoryWrite = "memory:write"
)

// allCaps is every individual capability for the full-control group.
var allCaps = []string{
	CapSessionsList, CapSessionsRead, CapSessionsWrite, CapSessionsKill, CapSessionsInput,
	CapAgentsList, CapAgentsRead, CapAgentsSpawn, CapAgentsTerminate,
	CapObserversList, CapObserversRead, CapObserversWrite,
	CapLLMsList, CapLLMsRead, CapLLMsWrite,
	CapComputeList, CapComputeRead, CapComputeWrite,
	CapAnalyticsRead, CapAnalyticsWrite,
	CapHealthRead,
	CapConfigRead, CapConfigWrite,
	CapSecretsList, CapSecretsRead, CapSecretsWrite,
	CapPipelinesList, CapPipelinesRead, CapPipelinesStart, CapPipelinesCancel,
	CapAutonomousList, CapAutonomousRead, CapAutonomousWrite, CapAutonomousRun,
	CapCouncilList, CapCouncilRead, CapCouncilRun,
	CapFederationList, CapFederationRead, CapFederationWrite, CapFederationSelf,
	CapDocsRead,
	CapAuditRead,
	CapCommRead, CapCommWrite,
	CapAlertsList, CapAlertsRead,
	CapDashboardRead, CapDashboardWrite,
	CapQueueRead, CapQueueWrite,
	CapResultsList, CapResultsRead, CapResultsWrite,
	CapMemoryRead, CapMemoryWrite,
}

// CapabilityGroup is a named set of capabilities.
type CapabilityGroup struct {
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Caps        []string `json:"caps"`
	Builtin     bool     `json:"builtin,omitempty"`
}

// BuiltinGroups are the predefined CBAC role presets.
var BuiltinGroups = map[string]*CapabilityGroup{
	"monitor": {
		Name:        "monitor",
		Description: "Read-only observability: health, analytics, session list, alert list",
		Builtin:     true,
		Caps: []string{
			CapHealthRead,
			CapAnalyticsRead,
			CapSessionsList,
			CapAgentsList,
			CapObserversList,
			CapAlertsList, CapAlertsRead,
			CapDashboardRead,
		},
	},
	"session-viewer": {
		Name:        "session-viewer",
		Description: "Read access to sessions and agents",
		Builtin:     true,
		Caps: []string{
			CapSessionsList, CapSessionsRead,
			CapAgentsList, CapAgentsRead,
		},
	},
	"session-operator": {
		Name:        "session-operator",
		Description: "Full session + agent lifecycle control",
		Builtin:     true,
		Caps: []string{
			CapSessionsList, CapSessionsRead, CapSessionsWrite,
			CapSessionsKill, CapSessionsInput,
			CapAgentsList, CapAgentsRead, CapAgentsSpawn, CapAgentsTerminate,
			CapPipelinesList, CapPipelinesRead, CapPipelinesStart, CapPipelinesCancel,
		},
	},
	"inference-admin": {
		Name:        "inference-admin",
		Description: "Full control over LLM registry and compute nodes",
		Builtin:     true,
		Caps: []string{
			CapLLMsList, CapLLMsRead, CapLLMsWrite,
			CapComputeList, CapComputeRead, CapComputeWrite,
		},
	},
	"config-reader": {
		Name:        "config-reader",
		Description: "Read configuration and documentation",
		Builtin:     true,
		Caps:        []string{CapConfigRead, CapDocsRead},
	},
	"config-admin": {
		Name:        "config-admin",
		Description: "Read and write configuration",
		Builtin:     true,
		Caps:        []string{CapConfigRead, CapConfigWrite},
	},
	"analytics-viewer": {
		Name:        "analytics-viewer",
		Description: "Cost analytics, dashboard, and audit log",
		Builtin:     true,
		Caps:        []string{CapAnalyticsRead, CapDashboardRead, CapAuditRead},
	},
	"autonomous-operator": {
		Name:        "autonomous-operator",
		Description: "Full autonomous PRD lifecycle",
		Builtin:     true,
		Caps: []string{
			CapAutonomousList, CapAutonomousRead, CapAutonomousWrite, CapAutonomousRun,
		},
	},
	"council-operator": {
		Name:        "council-operator",
		Description: "Run council sessions and view results",
		Builtin:     true,
		Caps:        []string{CapCouncilList, CapCouncilRead, CapCouncilRun},
	},
	// SEC-009 (v9.0.0, breaking) — narrowed from a broad read-everything
	// default (sessions, agents, observers, alerts, dashboard, and every
	// OTHER peer's entry via federation:list/read) to just health:read +
	// federation:self (its own entry only, via GET
	// /api/federation/peers/self). A registered peer that genuinely needs
	// session visibility, agent/observer reads, or to enumerate other
	// peers must be granted those explicitly via a custom group — this is
	// no longer handed out by default to every newly registered peer.
	"federation-peer": {
		Name:        "federation-peer",
		Description: "Minimal default for newly registered federation peers — health + its own entry only; grant more via a custom group",
		Builtin:     true,
		Caps: []string{
			CapHealthRead,
			CapFederationSelf,
		},
	},
	"comm-bridge": {
		Name:        "comm-bridge",
		Description: "Peer that drives comm channels on behalf of the operator",
		Builtin:     true,
		Caps: []string{
			CapSessionsList, CapSessionsRead, CapSessionsInput,
			CapCommRead, CapCommWrite,
			CapAlertsList, CapAlertsRead,
		},
	},
	"comms-channel-agent": {
		Name:        "comms-channel-agent",
		Description: "Peer that acts as a channel address agent — receives channel commands, starts sessions, and reports back",
		Builtin:     true,
		Caps: []string{
			CapSessionsList, CapSessionsRead, CapSessionsInput, CapSessionsWrite,
			CapCommRead, CapCommWrite,
			CapAlertsList, CapAlertsRead,
			CapAutonomousList, CapAutonomousRead, CapAutonomousWrite,
		},
	},
	"read-only": {
		Name:        "read-only",
		Description: "All read/list capabilities across every surface",
		Builtin:     true,
		Caps: []string{
			CapSessionsList, CapSessionsRead,
			CapAgentsList, CapAgentsRead,
			CapObserversList, CapObserversRead,
			CapLLMsList, CapLLMsRead,
			CapComputeList, CapComputeRead,
			CapAnalyticsRead,
			CapHealthRead,
			CapConfigRead,
			CapSecretsList,
			CapPipelinesList, CapPipelinesRead,
			CapAutonomousList, CapAutonomousRead,
			CapCouncilList, CapCouncilRead,
			CapFederationList, CapFederationRead,
			CapDocsRead,
			CapAuditRead,
			CapCommRead,
			CapAlertsList, CapAlertsRead,
			CapDashboardRead,
			CapQueueRead,
			CapResultsList, CapResultsRead,
			CapMemoryRead,
		},
	},
	"smoke-reporter": {
		Name: "smoke-reporter",
		Description: "Reports smoke-test run progress, including cross-" +
			"instance forwarding (POST/PUT/DELETE /api/smoke/progress, " +
			"PUT /api/smoke/forward-url) — narrower than full-control for " +
			"a peer instance whose only job is reporting its own smoke runs",
		Builtin: true,
		Caps:    []string{CapAnalyticsRead, CapAnalyticsWrite},
	},
	// session-default (Design A3, v8.39.25) — the default grant for a
	// per-session scoped credential (internal/auth.SessionTokenStore),
	// which replaces the real admin token a spawned session's
	// bridge/MCP connection used to hold (HLLM-001/002's root cause).
	// Covers coordination + observability a session legitimately needs
	// (its own + sibling sessions/agents, the work queue, result store,
	// memory, comm/discussion, docs, autonomous-read, pipelines,
	// council) — excludes anything that can change datawatch's own
	// security posture or reach other operator-trust-boundary systems
	// (secrets, config writes, federation/tailscale, LLM/compute
	// registry, plugin/skill-registry install, profile/project/
	// template/schedule/device management, identity, daemon control).
	// Operators can grant a session MORE via `session.capabilities` in
	// config (any custom group works, same mechanism as federation
	// peers) if an integration genuinely needs a wider default.
	"session-default": {
		Name:        "session-default",
		Description: "Default per-session scoped-credential grant (Design A3) — coordination + observability, not admin",
		Builtin:     true,
		Caps: []string{
			CapSessionsList, CapSessionsRead, CapSessionsWrite, CapSessionsKill, CapSessionsInput,
			CapAgentsList, CapAgentsRead, CapAgentsSpawn, CapAgentsTerminate,
			CapObserversList, CapObserversRead,
			CapHealthRead,
			CapAnalyticsRead,
			CapDocsRead,
			CapAlertsList, CapAlertsRead,
			CapAutonomousList, CapAutonomousRead,
			CapCouncilList, CapCouncilRead, CapCouncilRun,
			CapPipelinesList, CapPipelinesRead, CapPipelinesStart, CapPipelinesCancel,
			CapCommRead, CapCommWrite,
			CapQueueRead, CapQueueWrite,
			CapResultsList, CapResultsRead, CapResultsWrite,
			CapMemoryRead, CapMemoryWrite,
		},
	},
	"full-control": {
		Name:        "full-control",
		Description: "All capabilities — equivalent to the admin token",
		Builtin:     true,
		Caps:        allCaps,
	},
}

// Resolve expands a slice of capability strings (group names and/or individual
// cap strings) into a deduplicated set of individual capability strings.
// custom is the operator-defined group map (may be nil).
func Resolve(caps []string, custom map[string]*CapabilityGroup) []string {
	seen := make(map[string]struct{}, len(caps)*4)
	for _, c := range caps {
		expand(c, custom, seen, 0)
	}
	out := make([]string, 0, len(seen))
	for c := range seen {
		out = append(out, c)
	}
	return out
}

func expand(c string, custom map[string]*CapabilityGroup, seen map[string]struct{}, depth int) {
	if depth > 8 {
		return // guard against cycles in custom groups
	}
	// Try builtin group first.
	if g, ok := BuiltinGroups[c]; ok {
		for _, gc := range g.Caps {
			expand(gc, custom, seen, depth+1)
		}
		return
	}
	// Try custom group.
	if custom != nil {
		if g, ok := custom[c]; ok {
			for _, gc := range g.Caps {
				expand(gc, custom, seen, depth+1)
			}
			return
		}
	}
	// Treat as individual capability string.
	if strings.Contains(c, ":") {
		seen[c] = struct{}{}
	}
}

// Check reports whether required is present in the granted capability set.
// granted must already be resolved (output of Resolve).
func Check(granted []string, required string) bool {
	for _, g := range granted {
		if g == required {
			return true
		}
	}
	return false
}

// ListBuiltinGroups returns all builtin groups in a deterministic order.
func ListBuiltinGroups() []*CapabilityGroup {
	order := []string{
		"monitor", "session-viewer", "session-operator",
		"inference-admin", "config-reader", "config-admin",
		"analytics-viewer", "autonomous-operator", "council-operator",
		"federation-peer", "comm-bridge", "comms-channel-agent", "session-default", "read-only", "full-control",
	}
	out := make([]*CapabilityGroup, 0, len(order))
	for _, name := range order {
		if g, ok := BuiltinGroups[name]; ok {
			out = append(out, g)
		}
	}
	return out
}
