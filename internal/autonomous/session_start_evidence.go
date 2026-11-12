package autonomous

import (
	"strings"
	"time"
	"unicode"
)

// claudeCodeToolLineMarker opens a Claude Code tool-use line, e.g.
// "⏺ Bash(go test ./...)" or "⏺ Reading internal/session/tmux.go". The same
// marker appears in internal/session/testdata/paste/submitted_running.txt.
const claudeCodeToolLineMarker = "⏺"

// claudeCodeLegacyToolMarker opens the older tool-use line shape, e.g.
// "● Read(docs/plans/x.md)". Only a "●" followed by a tool label counts as a
// tool call; a bare "●" is a TUI indicator used elsewhere.
const claudeCodeLegacyToolMarker = "●"

// claudeCodeToolResultMarker opens a tool result or running line nested under
// a tool-use line, e.g. "  ⎿  Running…" or "  ⎿  Read 120 lines".
const claudeCodeToolResultMarker = "⎿"

// claudeCodeSpinnerGlyphs are the thinking/working spinner frames Claude Code
// renders at the start of a status line, e.g. "✻ Running… (6m 12s · esc to
// interrupt)". A spinner means the agent is busy, so it counts as work.
var claudeCodeSpinnerGlyphs = []string{"✢", "✶", "✺", "✻", "✽", "✳"}

// StartEvidence collects the signals that a worker session has begun doing
// work. Any one of them rules out a never-started stall. AssistantOutput
// alone undercounts real work: a worker whose first action is a long silent
// tool call shows a running-tool line and a spinner but no assistant bullet.
//
// The type is pure data. internal/autonomous does not import internal/session
// (import cycle), so the daemon fills each field from its own state.
type StartEvidence struct {
	AssistantOutput           bool      // a Claude Code assistant response bullet appeared after the prompt
	ToolCallSeen              bool      // a tool call is in progress or finished in the pane (see paneShowsToolCall)
	AgentChildProcess         bool      // the agent's pane process has a live child, i.e. a command it started
	PaneChangedAt             time.Time // when the pane content last changed after delivery; zero when it has not
	ChannelEventAfterDelivery bool      // a channel event arrived after the task was delivered
	TaskDelivered             bool      // the task prompt was delivered; context only, never evidence by itself
	OutputAfterDelivery       bool      // pane output was produced after the task was delivered
}

// Latest reports whether any evidence of work is present, and the time of the
// most recent evidence. Only PaneChangedAt carries a timestamp, so the time is
// PaneChangedAt when evidence is present and the zero time otherwise. The
// boolean kinds carry no time of their own. TaskDelivered is context, not
// evidence: a delivered prompt alone never reports true, because a worker that
// was handed a task and never started is exactly the stall being detected.
func (e StartEvidence) Latest() (bool, time.Time) {
	present := e.AssistantOutput ||
		e.ToolCallSeen ||
		e.AgentChildProcess ||
		e.ChannelEventAfterDelivery ||
		e.OutputAfterDelivery ||
		!e.PaneChangedAt.IsZero()
	if !present {
		return false, time.Time{}
	}
	return true, e.PaneChangedAt
}

// paneShowsToolCall reports whether the pane shows a Claude Code tool call in
// progress or already finished: a tool-use line, a tool result or running
// line, or a working spinner. Every line is checked, not just the lines after
// the prompt, because a tool line can scroll above the prompt once the call
// returns. Pure: pane in, bool out.
func paneShowsToolCall(pane string) bool {
	for _, raw := range strings.Split(pane, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, claudeCodeToolLineMarker) ||
			strings.HasPrefix(line, claudeCodeToolResultMarker) {
			return true
		}
		if strings.HasPrefix(line, claudeCodeLegacyToolMarker) &&
			isToolLabel(strings.TrimSpace(strings.TrimPrefix(line, claudeCodeLegacyToolMarker))) {
			return true
		}
		if isSpinnerLine(line) {
			return true
		}
	}
	return false
}

// PaneShowsToolCall is the exported form of paneShowsToolCall for callers
// outside the autonomous package (the daemon's wait loop reads the pane and
// sets StartEvidence.ToolCallSeen from it).
func PaneShowsToolCall(pane string) bool { return paneShowsToolCall(pane) }

// isToolLabel reports whether s starts with a tool name immediately followed
// by "(", as in "Read(docs/plans/x.md)". The name must be letters only.
func isToolLabel(s string) bool {
	open := strings.Index(s, "(")
	if open <= 0 {
		return false
	}
	for _, r := range s[:open] {
		if !unicode.IsLetter(r) {
			return false
		}
	}
	return true
}

// isSpinnerLine reports whether the trimmed line opens with a spinner glyph
// followed by a space and some status text.
func isSpinnerLine(line string) bool {
	for _, glyph := range claudeCodeSpinnerGlyphs {
		if strings.HasPrefix(line, glyph+" ") {
			return strings.TrimSpace(strings.TrimPrefix(line, glyph)) != ""
		}
	}
	return false
}
