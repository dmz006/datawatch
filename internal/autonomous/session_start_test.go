package autonomous

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

const (
	waitingInputState = "waiting_input"
	runningState      = "running"
)

// loadFixture reads one of the pane snapshots under testdata/start.
func loadFixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "start", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return string(b)
}

func TestStalledBoundary(t *testing.T) {
	limit := 2 * time.Hour
	// WaitingFor exactly equal to Limit must NOT be a stall — the bound is
	// a bound, not a deadline tripped at equality.
	equal := SessionStartObservation{
		State:              waitingInputState,
		WaitingFor:         limit,
		HasAssistantOutput: false,
		Limit:              limit,
	}
	if stalled, reason := equal.Stalled(); stalled {
		t.Fatalf("WaitingFor == Limit reported stalled (with reason %q); equality must not trip", reason)
	}
	// One nanosecond past the bound is a stall.
	past := equal
	past.WaitingFor += time.Nanosecond
	if stalled, reason := past.Stalled(); !stalled || reason != SessionStartFailureReason {
		t.Fatalf("WaitingFor > Limit: Stalled() = (%v, %q), want (true, %q)", stalled, reason, SessionStartFailureReason)
	}
}

func TestStalledTable(t *testing.T) {
	limit := time.Minute
	running := SessionStartObservation{State: runningState, WaitingFor: 10 * time.Minute, HasAssistantOutput: true, Limit: limit}
	tests := []struct {
		name      string
		obs       SessionStartObservation
		wantStall bool
	}{
		{name: "running with assistant output never stalls", obs: running, wantStall: false},
		{name: "waiting_input with assistant output is started, not stalled", obs: SessionStartObservation{
			State:              waitingInputState,
			WaitingFor:         10 * time.Minute,
			HasAssistantOutput: true,
			Limit:              limit,
		}, wantStall: false},
		{name: "waiting_input without output past the bound stalls", obs: SessionStartObservation{
			State:              waitingInputState,
			WaitingFor:         10 * time.Minute,
			HasAssistantOutput: false,
			Limit:              limit,
		}, wantStall: true},
		{name: "waiting_input without output at the boundary does not stall", obs: SessionStartObservation{
			State:      waitingInputState,
			WaitingFor: limit,
			Limit:      limit,
		}, wantStall: false},
		{name: "zero limit disables the check", obs: SessionStartObservation{
			State:      waitingInputState,
			WaitingFor: 10 * time.Minute,
			Limit:      0,
		}, wantStall: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stalled, reason := tt.obs.Stalled()
			if stalled != tt.wantStall {
				t.Fatalf("Stalled() = %v, want %v", stalled, tt.wantStall)
			}
			if (stalled && reason == "") || (!stalled && reason != "") {
				t.Fatalf("Stalled() reason consistency broken: stalled=%v reason=%q", stalled, reason)
			}
		})
	}
}

func TestPaneHasAssistantOutput(t *testing.T) {
	silent := loadFixture(t, "silent_waiting_input.txt")
	if paneHasAssistantOutput(silent) {
		t.Fatalf("silent fixture: paneHasAssistantOutput = true, want false (no assistant line in the pane)")
	}
	simulated := "❯ summarize the latest PR\n• I will start by reading the PR diff and the linked issues.\n"
	if !paneHasAssistantOutput(simulated) {
		t.Fatalf("pane with \"❯ prompt\" then \"• response\" must be recognized as assistant output")
	}
	// A bullet BEFORE the prompt (e.g. an earlier transcript line, or the
	// "·" dot of "Enter to confirm · Esc to cancel") is not an assistant
	// turn for THIS prompt.
	preOnly := "• previous turn\n❯ summarize the latest PR\n"
	if paneHasAssistantOutput(preOnly) {
		t.Fatalf("bullet above the prompt line must not count as assistant output after the prompt")
	}
	// No prompt line at all: nothing to anchor after.
	if paneHasAssistantOutput("• some old line\n") {
		t.Fatalf("bullet with no prompt selector line must not count")
	}
}

func TestLastLines(t *testing.T) {
	silent := loadFixture(t, "silent_waiting_input.txt")
	got := lastLines(silent, 20)
	if len(got) != 20 {
		t.Fatalf("lastLines(silent, 20) returned %d lines, want exactly 20 (the silent pane must have at least 20 non-empty lines)", len(got))
	}
	// The footer line must be preserved exactly.
	wantFooter := "  Enter to confirm · Esc to cancel"
	found := false
	for _, line := range got {
		if line == wantFooter {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("lastLines(silent, 20) missing the %q footer line; got %q", wantFooter, got[len(got)-1])
	}

	// Order is top-to-bottom and blank lines are skipped.
	sample := "a\n\nb\n\nc\n"
	got2 := lastLines(sample, 2)
	if len(got2) != 2 || got2[0] != "b" || got2[1] != "c" {
		t.Fatalf("lastLines(%q, 2) = %q, want [b c]", sample, got2)
	}
	// Fewer non-empty lines than n returns all of them.
	got3 := lastLines("\n\n\n", 20)
	if len(got3) != 0 {
		t.Fatalf("lastLines(all blank, 20) = %q, want empty slice", got3)
	}
	// n <= 0 returns nothing.
	if got4 := lastLines("x\ny\n", 0); len(got4) != 0 {
		t.Fatalf("lastLines(x, 0) = %q, want empty", got4)
	}
}

func TestErrSentinels(t *testing.T) {
	if ErrSessionNeverStarted == nil {
		t.Fatal("ErrSessionNeverStarted must be a non-nil sentinel")
	}
	if SessionStartFailureReason != "session never started" {
		t.Fatalf("SessionStartFailureReason = %q, want %q", SessionStartFailureReason, "session never started")
	}
}
