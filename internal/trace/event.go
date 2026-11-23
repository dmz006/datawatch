// Package trace defines the normalized, backend-independent trace event
// that every agent backend maps its tool calls, model requests and state
// changes onto, plus the tracing settings (tracing.* config keys).
package trace

import "time"

// Kind classifies a trace event.
type Kind string

// Event kinds.
const (
	KindToolCall           Kind = "tool_call"
	KindToolResult         Kind = "tool_result"
	KindModelRequest       Kind = "model_request"
	KindStateChange        Kind = "state_change"
	KindCapabilityCheck    Kind = "capability_check"
	KindPermissionDecision Kind = "permission_decision"
	KindLoopWarning        Kind = "loop_warning"
	KindError              Kind = "error"
)

// Kinds lists every known Kind in a stable order.
var Kinds = []Kind{
	KindToolCall, KindToolResult, KindModelRequest, KindStateChange,
	KindCapabilityCheck, KindPermissionDecision, KindLoopWarning, KindError,
}

// Valid reports whether k is a known kind.
func (k Kind) Valid() bool {
	for _, known := range Kinds {
		if k == known {
			return true
		}
	}
	return false
}

// Coverage states how much of a backend's activity its trace captures.
type Coverage string

// Coverage levels.
const (
	// CoverageFull: every tool call, result and model request is traced.
	CoverageFull Coverage = "full"
	// CoveragePartial: some event kinds are traced, others are not visible.
	CoveragePartial Coverage = "partial"
	// CoverageEventsOnly: only coarse state-change events are available.
	CoverageEventsOnly Coverage = "events-only"
)

// Valid reports whether c is a known coverage level.
func (c Coverage) Valid() bool {
	switch c {
	case CoverageFull, CoveragePartial, CoverageEventsOnly:
		return true
	}
	return false
}

// Event is one normalized trace record.
type Event struct {
	SessionID   string    `json:"session_id,omitempty"`
	TaskID      string    `json:"task_id,omitempty"`
	StoryID     string    `json:"story_id,omitempty"`
	AutomatonID string    `json:"automaton_id,omitempty"`
	Agent       string    `json:"agent,omitempty"`
	Backend     string    `json:"backend,omitempty"`
	Kind        Kind      `json:"kind"`
	Name        string    `json:"name,omitempty"`
	ArgSummary  string    `json:"arg_summary,omitempty"`
	Outcome     string    `json:"outcome,omitempty"`
	DurationMS  int64     `json:"duration_ms,omitempty"`
	TokensIn    int64     `json:"tokens_in,omitempty"`
	TokensOut   int64     `json:"tokens_out,omitempty"`
	Timestamp   time.Time `json:"timestamp"`

	// Pack attribution. These fields record which installable pack, unit,
	// step and runner produced an event. All are optional and omitted from
	// JSON when empty, so events written before they existed decode and
	// re-encode unchanged.
	PackName       string            `json:"pack_name,omitempty"`
	PackVersion    string            `json:"pack_version,omitempty"`
	Unit           string            `json:"unit,omitempty"`
	Step           string            `json:"step,omitempty"`
	Project        string            `json:"project,omitempty"`
	Automation     string            `json:"automation,omitempty"`
	Runner         string            `json:"runner,omitempty"` // "daemon" or "session"
	Tier           string            `json:"tier,omitempty"`
	Entry          string            `json:"entry,omitempty"`
	Escalation     bool              `json:"escalation,omitempty"`
	Cost           float64           `json:"cost,omitempty"`
	ResolvedInputs map[string]string `json:"resolved_inputs,omitempty"`
}
