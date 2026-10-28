package autonomous

import (
	"time"

	"github.com/dmz006/datawatch/internal/pilog"
)

// TaskProgress mirrors pilog.Progress for the wire: the same fields, but
// TaskProgress is what an autonomous.Task carries and what prd-get
// serializes, so it lives in this package (not the reverse — pilog must
// stay free of autonomous so it stays reusable by any reader of a pi log).
type TaskProgress struct {
	// LastEventAt — the timestamp of the newest event seen in the log,
	// RFC3339, as it will appear in JSON. Zero time = log had no
	// timestamped events (should not happen for a live session).
	LastEventAt string `json:"last_event_at"`
	// AssistantTurns — number of assistant-role messages (one per LLM
	// reply, tool-calling ones included).
	AssistantTurns int `json:"assistant_turns"`
	// ToolCalls — per-tool-name call counts across the whole log so far.
	// Empty for a fresh log.
	ToolCalls map[string]int `json:"tool_calls,omitempty"`
	// LastToolName / LastToolArgs — the single most recent tool call the
	// agent made; LastToolArgs is that call's arguments as compact JSON
	// (truncated to 500 chars by pilog), so the snapshot shows exactly
	// what the worker was doing last.
	LastToolName string `json:"last_tool_name,omitempty"`
	LastToolArgs string `json:"last_tool_args,omitempty"`
	// LastSummary — the first 200 chars of the most recent thinking or text
	// block, i.e. the agent's own last-stated intent.
	LastSummary string `json:"last_summary,omitempty"`
	// Tokens — cumulative usage reported by the log's assistant messages.
	Tokens int `json:"tokens,omitempty"`
}

// FromPilog converts a raw pilog.Read result into the TaskProgress a task
// carries. The conversion is by value; LastEventAt is formatted RFC3339
// (pilogs record wall-clock UTC, so the output is "...Z").
func FromPilog(p pilog.Progress) *TaskProgress {
	toolCalls := make(map[string]int, len(p.ToolCalls))
	for name, n := range p.ToolCalls {
		toolCalls[name] = n
	}
	at := ""
	if !p.LastEventAt.IsZero() {
		at = p.LastEventAt.Format(time.RFC3339)
	}
	return &TaskProgress{
		LastEventAt:    at,
		AssistantTurns: p.AssistantTurns,
		ToolCalls:      toolCalls,
		LastToolName:   p.LastToolName,
		LastToolArgs:   p.LastToolArgs,
		LastSummary:    p.LastSummary,
		Tokens:         p.Tokens,
	}
}

// progressActive reports whether a task with this status may legitimately
// carry a live Progress snapshot. In-progress and verifying are the two
// states where the worker is actively running (verifying is the phase the
// periodic session poll lives in — the poll ticks while the executor
// awaits the session's completion). Any other status means "not
// genuinely in progress right now", and progress is stale there.
func progressActive(status TaskStatus) bool {
	switch status {
	case TaskInProgress, TaskVerifying:
		return true
	}
	return false
}

// SetProgress records a fresh snapshot (and any looping warning) on the
// task. Called under the store lock by RefreshTaskProgress / MutateTask.
func (t *Task) SetProgress(p *TaskProgress, warning string) {
	t.Progress = p
	t.LoopingWarning = warning
}

// ClearProgress wipes a stale snapshot — the invariant this package's
// Store enforces at persist time so "in progress only" is guaranteed
// regardless of which code path last saved the task.
func (t *Task) ClearProgress() {
	t.Progress = nil
	t.LoopingWarning = ""
}
