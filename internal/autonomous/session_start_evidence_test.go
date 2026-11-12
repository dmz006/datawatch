package autonomous

import (
	"testing"
	"time"
)

func TestStartEvidenceLatest(t *testing.T) {
	changed := time.Date(2026, 10, 24, 9, 30, 0, 0, time.UTC)
	tests := []struct {
		name        string
		ev          StartEvidence
		wantPresent bool
		wantAt      time.Time
	}{
		{name: "no evidence", ev: StartEvidence{}, wantPresent: false},
		{name: "task delivered alone is context, not evidence", ev: StartEvidence{TaskDelivered: true}, wantPresent: false},
		{name: "assistant output", ev: StartEvidence{AssistantOutput: true}, wantPresent: true},
		{name: "tool call seen", ev: StartEvidence{ToolCallSeen: true}, wantPresent: true},
		{name: "agent child process", ev: StartEvidence{AgentChildProcess: true}, wantPresent: true},
		{name: "channel event after delivery", ev: StartEvidence{ChannelEventAfterDelivery: true}, wantPresent: true},
		{name: "output after delivery", ev: StartEvidence{OutputAfterDelivery: true}, wantPresent: true},
		{name: "pane changed with a timestamp", ev: StartEvidence{PaneChangedAt: changed}, wantPresent: true, wantAt: changed},
		{
			name:        "delivered task plus pane change reports the change time",
			ev:          StartEvidence{TaskDelivered: true, PaneChangedAt: changed},
			wantPresent: true,
			wantAt:      changed,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			present, at := tt.ev.Latest()
			if present != tt.wantPresent {
				t.Fatalf("Latest() present = %v, want %v", present, tt.wantPresent)
			}
			if !at.Equal(tt.wantAt) {
				t.Fatalf("Latest() time = %v, want %v", at, tt.wantAt)
			}
		})
	}
}

func TestStalledWithEvidence(t *testing.T) {
	limit := 2 * time.Minute
	past := 6 * time.Minute
	changed := time.Date(2026, 10, 24, 9, 30, 0, 0, time.UTC)
	tests := []struct {
		name      string
		evidence  StartEvidence
		wantStall bool
	}{
		{name: "no evidence past the limit stalls", evidence: StartEvidence{}, wantStall: true},
		{name: "assistant output rules out a stall", evidence: StartEvidence{AssistantOutput: true}, wantStall: false},
		{name: "tool call rules out a stall", evidence: StartEvidence{ToolCallSeen: true}, wantStall: false},
		{name: "agent child process rules out a stall", evidence: StartEvidence{AgentChildProcess: true}, wantStall: false},
		{name: "pane change rules out a stall", evidence: StartEvidence{PaneChangedAt: changed}, wantStall: false},
		{name: "channel event after delivery rules out a stall", evidence: StartEvidence{ChannelEventAfterDelivery: true}, wantStall: false},
		{name: "output after delivery rules out a stall", evidence: StartEvidence{OutputAfterDelivery: true}, wantStall: false},
		{name: "task delivered alone still stalls", evidence: StartEvidence{TaskDelivered: true}, wantStall: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			obs := SessionStartObservation{
				State:      waitingInputState,
				WaitingFor: past,
				Limit:      limit,
				Evidence:   tt.evidence,
			}
			stalled, reason := obs.Stalled()
			if stalled != tt.wantStall {
				t.Fatalf("Stalled() = %v, want %v", stalled, tt.wantStall)
			}
			if (stalled && reason != SessionStartFailureReason) || (!stalled && reason != "") {
				t.Fatalf("Stalled() reason = %q, stalled = %v", reason, stalled)
			}
		})
	}
}

func TestStalledNoEvidenceBound(t *testing.T) {
	limit := 2 * time.Minute
	base := SessionStartObservation{State: waitingInputState, Limit: limit}

	atLimit := base
	atLimit.WaitingFor = limit
	if stalled, _ := atLimit.Stalled(); stalled {
		t.Fatalf("no evidence, WaitingFor == Limit: stalled, want not stalled (equality must not trip)")
	}

	pastLimit := base
	pastLimit.WaitingFor = limit + time.Nanosecond
	if stalled, _ := pastLimit.Stalled(); !stalled {
		t.Fatalf("no evidence, WaitingFor > Limit: not stalled, want stalled")
	}

	belowLimit := base
	belowLimit.WaitingFor = time.Minute
	if stalled, _ := belowLimit.Stalled(); stalled {
		t.Fatalf("no evidence, WaitingFor < Limit: stalled, want not stalled")
	}
}

func TestStalledLongSilentToolCall(t *testing.T) {
	pane := loadFixture(t, "long_silent_tool_call.txt")
	if paneHasAssistantOutput(pane) {
		t.Fatalf("long silent tool call fixture has no assistant bullet; paneHasAssistantOutput = true")
	}
	if !PaneShowsToolCall(pane) {
		t.Fatalf("long silent tool call fixture: PaneShowsToolCall = false, want true (tool line and spinner are visible)")
	}
	// The worker has been silent for six minutes against a two-minute bound.
	// Without evidence it is judged never started; with the pane's tool call
	// it is not.
	obs := SessionStartObservation{
		State:      waitingInputState,
		WaitingFor: 6 * time.Minute,
		Limit:      2 * time.Minute,
	}
	if stalled, _ := obs.Stalled(); !stalled {
		t.Fatalf("baseline without evidence: not stalled, want stalled")
	}
	obs.Evidence = StartEvidence{ToolCallSeen: PaneShowsToolCall(pane)}
	if stalled, reason := obs.Stalled(); stalled {
		t.Fatalf("long silent tool call with ToolCallSeen: stalled (reason %q), want not stalled", reason)
	}
}

func TestPaneShowsToolCall(t *testing.T) {
	tests := []struct {
		name string
		pane string
		want bool
	}{
		{name: "tool-use line", pane: "⏺ Bash(go build ./...)\n", want: true},
		{name: "tool-use line without parens", pane: "⏺ Reading internal/session/tmux.go\n", want: true},
		{name: "tool result running line", pane: "  ⎿  Running…\n", want: true},
		{name: "tool result line", pane: "  ⎿  Read 120 lines\n", want: true},
		{name: "legacy tool line", pane: "● Read(docs/plans/x.md)\n", want: true},
		{name: "spinner with status", pane: "✻ Thinking…\n", want: true},
		{name: "spinner with elapsed time", pane: "✻ Running… (6m 12s · esc to interrupt)\n", want: true},
		{name: "bare legacy bullet is not a tool call", pane: "● \n", want: false},
		{name: "assistant bullet is not a tool call", pane: "❯ summarize\n• I will start by reading the diff.\n", want: false},
		{name: "prompt line alone", pane: "❯ summarize the latest PR\n", want: false},
		{name: "bare asterisk is not a spinner", pane: "* a markdown bullet\n", want: false},
		{name: "empty pane", pane: "", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := paneShowsToolCall(tt.pane); got != tt.want {
				t.Fatalf("paneShowsToolCall(%q) = %v, want %v", tt.pane, got, tt.want)
			}
		})
	}
}

func TestPaneShowsToolCallSilentStartupDialog(t *testing.T) {
	silent := loadFixture(t, "silent_waiting_input.txt")
	if PaneShowsToolCall(silent) {
		t.Fatalf("silent startup dialog fixture: PaneShowsToolCall = true, want false (no tool activity on screen)")
	}
}
