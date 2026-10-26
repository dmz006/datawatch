package autonomous

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Direct tests of StartTracker.Observe on a fake clock. Each tick is ten
// seconds after the origin, so tick k is at origin+10k seconds.

var startTrackerOrigin = time.Date(2026, 10, 25, 8, 0, 0, 0, time.UTC)

func trackerTickAt(k int) time.Time {
	return startTrackerOrigin.Add(time.Duration(k*10) * time.Second)
}

// trackerSilentPane is a parked pane with no assistant turn and no tool call.
const trackerSilentPane = "Do you trust this folder?\n❯ 1. Yes\n  2. No\n"

// trackerToolPane shows a tool call in progress.
const trackerToolPane = "❯ run the task\n⏺ Bash(go test ./...)\n  ⎿  Running…\n"

// trackerFirstTurnPane shows the assistant's first turn after the prompt.
const trackerFirstTurnPane = "❯ run the task\n• Reading the spec now.\n"

func TestStartTrackerLaunchStallsOnlyPastLimit(t *testing.T) {
	tr := NewStartTracker(60*time.Second, time.Time{}, time.Time{}, time.Time{})
	var stalledAt int
	for k := 1; k <= 10; k++ {
		obs := tr.Observe(StartInput{Now: trackerTickAt(k), State: sessionStateWaitingInput, Pane: trackerSilentPane})
		if stalled, _ := obs.Stalled(); stalled && stalledAt == 0 {
			stalledAt = k
		}
	}
	// The first silent waiting tick anchors the window at tick 1, so tick 8 is
	// the first observation 70s past it.
	if stalledAt != 8 {
		t.Fatalf("launch tracker stalled at tick %d, want 8", stalledAt)
	}
	if tail := tr.FailureTail(); len(tail) == 0 {
		t.Fatal("failure tail not recorded when the bound fired")
	}
}

func TestStartTrackerLaunchAnchorsWindowAtLaunchTime(t *testing.T) {
	// The wait loop first sees the worker at tick 5, already silent. With a
	// recorded launch at the origin the window runs from the launch, so the
	// bound fires at tick 7 (70s) rather than tick 12 (70s after tick 5).
	tr := NewStartTracker(60*time.Second, startTrackerOrigin, time.Time{}, time.Time{})
	var stalledAt int
	for k := 5; k <= 14; k++ {
		obs := tr.Observe(StartInput{Now: trackerTickAt(k), State: sessionStateWaitingInput, Pane: trackerSilentPane})
		if stalled, _ := obs.Stalled(); stalled && stalledAt == 0 {
			stalledAt = k
		}
	}
	if stalledAt != 7 {
		t.Fatalf("launch tracker with a launch anchor stalled at tick %d, want 7", stalledAt)
	}
}

func TestStartTrackerLaunchToolCallKeepsClockRunning(t *testing.T) {
	tr := NewStartTracker(60*time.Second, time.Time{}, time.Time{}, time.Time{})
	for k := 1; k <= 20; k++ {
		pane := trackerSilentPane
		if k%5 == 0 {
			pane = trackerToolPane
		}
		obs := tr.Observe(StartInput{Now: trackerTickAt(k), State: sessionStateWaitingInput, Pane: pane})
		if stalled, _ := obs.Stalled(); stalled {
			t.Fatalf("launch tracker ended a session with a tool call every 50s at tick %d", k)
		}
	}
	if tail := tr.FailureTail(); tail != nil {
		t.Fatalf("failure tail recorded without a stall: %v", tail)
	}
}

func TestStartTrackerAdoptedRestartsWhenLeavingWaitingInput(t *testing.T) {
	// Waiting for five silent ticks, then running, then waiting again. The
	// adopted window restarts on the running tick, so the second waiting run
	// never exceeds the 60s limit.
	tr := NewAdoptedStartTracker(60*time.Second, trackerTickAt(0), time.Time{})
	states := []string{
		sessionStateWaitingInput, sessionStateWaitingInput, sessionStateWaitingInput,
		sessionStateWaitingInput, sessionStateWaitingInput,
		"running", "running",
		sessionStateWaitingInput, sessionStateWaitingInput, sessionStateWaitingInput,
	}
	for i, state := range states {
		k := i + 1
		obs := tr.Observe(StartInput{Now: trackerTickAt(k), State: state, Pane: trackerSilentPane})
		if stalled, _ := obs.Stalled(); stalled {
			t.Fatalf("adopted tracker stalled at tick %d (state %s) after leaving waiting_input", k, state)
		}
	}
}

func TestStartTrackerAdoptedFirstTurnLatchesStarted(t *testing.T) {
	// The first turn shows at tick 1; the pane then scrolls it out of view and
	// the session sits silent far past the limit. The latch never fails it.
	tr := NewAdoptedStartTracker(60*time.Second, trackerTickAt(0), time.Time{})
	obs := tr.Observe(StartInput{Now: trackerTickAt(1), State: sessionStateWaitingInput, Pane: trackerFirstTurnPane})
	if !obs.HasAssistantOutput {
		t.Fatal("first assistant turn not reported")
	}
	for k := 2; k <= 40; k++ {
		obs := tr.Observe(StartInput{Now: trackerTickAt(k), State: sessionStateWaitingInput, Pane: trackerSilentPane})
		if stalled, _ := obs.Stalled(); stalled {
			t.Fatalf("started session failed at tick %d after its first turn", k)
		}
	}
	if tail := tr.FailureTail(); tail != nil {
		t.Fatalf("failure tail recorded for a started session: %v", tail)
	}
}

func TestStartTrackerAdoptedSeedAnchorsWindow(t *testing.T) {
	// A last evidence of life ten seconds before tick 1 anchors the window, so
	// the bound fires on tick 6 (70s after that evidence), not on the first tick.
	lastLife := trackerTickAt(0).Add(-10 * time.Second)
	tr := NewAdoptedStartTracker(60*time.Second, trackerTickAt(0).Add(-time.Hour), lastLife)
	var stalledAt int
	for k := 1; k <= 10; k++ {
		obs := tr.Observe(StartInput{Now: trackerTickAt(k), State: sessionStateWaitingInput, Pane: trackerSilentPane})
		if stalled, _ := obs.Stalled(); stalled {
			stalledAt = k
			break
		}
	}
	if stalledAt != 6 {
		t.Fatalf("adopted tracker stalled at tick %d, want 6", stalledAt)
	}
}

// trackerStartAt is a fixture time the given number of seconds from the origin.
func trackerStartAt(sec int) time.Time {
	return startTrackerOrigin.Add(time.Duration(sec) * time.Second)
}

func TestSessionStartAdoptionSeedUsesLatestEvidence(t *testing.T) {
	updated := trackerStartAt(-300)
	channel := trackerStartAt(-120)
	logMtime := trackerStartAt(-200)
	tests := []struct {
		name                           string
		updatedAt, channelAt, logMtime time.Time
		want                           time.Time
	}{
		{name: "nothing known", want: time.Time{}},
		{name: "only the update time", updatedAt: updated, want: updated},
		{name: "channel event is newest", updatedAt: updated, channelAt: channel, logMtime: logMtime, want: channel},
		{name: "log mtime is newest", updatedAt: updated, channelAt: channel, logMtime: trackerStartAt(-10), want: trackerStartAt(-10)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := SessionStartAdoptionSeed(tt.updatedAt, tt.channelAt, tt.logMtime); !got.Equal(tt.want) {
				t.Fatalf("SessionStartAdoptionSeed() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestLogFileModTimeReadsTheLogMtime(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.log")
	if err := os.WriteFile(path, []byte("out\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	mtime := trackerStartAt(-45)
	if err := os.Chtimes(path, mtime, mtime); err != nil {
		t.Fatal(err)
	}
	if got := LogFileModTime(path); !got.Equal(mtime) {
		t.Errorf("LogFileModTime(%s) = %v, want %v", path, got, mtime)
	}
	if got := LogFileModTime(filepath.Join(dir, "missing.log")); !got.IsZero() {
		t.Errorf("LogFileModTime(missing) = %v, want zero", got)
	}
}
