// Package orchestrator (BL117, Sprint S8 → v4.0.0) composes BL24
// autonomous PRDs into a graph and runs each PRD under a set of
// guardrail sub-agents (rules, security, release-readiness, docs).
//
// Design doc: docs/plans/2026-04-20-bl117-prd-dag-orchestrator.md.

package orchestrator

import "time"

// NodeKind separates PRD execution nodes from guardrail attestation nodes.
type NodeKind string

const (
	NodeKindPRD       NodeKind = "prd"
	NodeKindGuardrail NodeKind = "guardrail"
)

type NodeStatus string

const (
	NodePending    NodeStatus = "pending"
	NodeQueued     NodeStatus = "queued"
	NodeInProgress NodeStatus = "in_progress"
	NodeCompleted  NodeStatus = "completed"
	NodeBlocked    NodeStatus = "blocked"
	NodeFailed     NodeStatus = "failed"
	NodeCancelled  NodeStatus = "cancelled"
)

type GraphStatus string

const (
	GraphDraft      GraphStatus = "draft"
	GraphActive     GraphStatus = "active"
	GraphCompleted  GraphStatus = "completed"
	GraphBlocked    GraphStatus = "blocked"
	GraphCancelled  GraphStatus = "cancelled"
)

// Verdict is a guardrail's pass/warn/block judgment. A node-level
// `block` halts the graph and requires operator intervention.
type Verdict struct {
	Outcome     string    `json:"outcome"`               // pass | warn | block
	Severity    string    `json:"severity,omitempty"`    // info | low | medium | high | critical
	Summary     string    `json:"summary"`
	Issues      []string  `json:"issues,omitempty"`
	VerdictAt   time.Time `json:"verdict_at"`
	ValidatorID string    `json:"validator_id,omitempty"` // session ID of the validator worker, if any
}

// Node is one vertex in the PRD-DAG. Kind=prd nodes wrap a BL24 PRD
// run; Kind=guardrail nodes wrap one guardrail attestation that fires
// after a specific PRD node completes.
type Node struct {
	ID        string     `json:"id"`         // 8-hex
	GraphID   string     `json:"graph_id"`
	Kind      NodeKind   `json:"kind"`
	PRDID     string     `json:"prd_id,omitempty"`     // required when Kind==prd
	Guardrail string     `json:"guardrail,omitempty"`  // name, when Kind==guardrail
	DependsOn []string   `json:"depends_on,omitempty"` // node IDs
	Status    NodeStatus `json:"status"`
	Verdict   *Verdict   `json:"verdict,omitempty"`
	Error     string     `json:"error,omitempty"`
	StartedAt *time.Time `json:"started_at,omitempty"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`

	// ObserverSummary — server-side enrichment, never persisted on
	// the graph. Populated only on read by the orchestrator REST
	// handler when the matching PRD's tasks have running sessions and
	// the observer can find their envelopes. S13 follow — currently
	// always nil pending the AutonomousAPI / observer join described
	// in `2026-04-25-s13-followup-orchestrator-observer.md`.
	ObserverSummary *ObserverSummary `json:"observer_summary,omitempty"`
}

// ObserverSummary is the per-node read-time observer attribution.
// Aggregated across the PRD's running sessions; nil when no sessions
// have non-zero envelopes yet.
type ObserverSummary struct {
	CPUPct        float64 `json:"cpu_pct"`
	RSSMB         int     `json:"rss_mb"`
	EnvelopeCount int     `json:"envelope_count"`
	LastPushAt    string  `json:"last_push_at,omitempty"` // RFC3339
}

// Graph is a set of Nodes composing one orchestrated run.
type Graph struct {
	ID         string      `json:"id"`           // 8-hex
	Title      string      `json:"title"`
	ProjectDir string      `json:"project_dir,omitempty"`
	PRDIDs     []string    `json:"prd_ids"`
	Nodes      []Node      `json:"nodes,omitempty"`
	Status     GraphStatus `json:"status"`
	CreatedAt  time.Time   `json:"created_at"`
	UpdatedAt  time.Time   `json:"updated_at"`
}
