package main

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	autonomouspkg "github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/session"
)

// startTrackerLimit is the start-confirm bound the tracker tests run with.
const startTrackerLimit = 120 * time.Second

// startTrackerOrigin is the fake clock's zero. Every tick is an offset from it
// in seconds, so no test reads the wall clock or touches tmux.
var startTrackerOrigin = time.Date(2026, 10, 24, 10, 0, 0, 0, time.UTC)

func startAt(sec int) time.Time {
	return startTrackerOrigin.Add(time.Duration(sec) * time.Second)
}

// Fake pane captures. The silent pane is a prompt with nothing after it; the
// tool pane shows a tool-use line; the assistant pane shows a response bullet
// after the prompt.
const (
	startSilentPane    = "❯ start the task\n"
	startToolPane      = "❯ start the task\n⏺ Bash(go test ./...)\n"
	startAssistantPane = "❯ start the task\n• I will read the diff first.\n"
)

// startStatusPane changes with n but shows neither a tool call nor an
// assistant turn, so only the pane change itself is evidence.
func startStatusPane(n int) string {
	return fmt.Sprintf("❯ start the task\nstatus %d\n", n)
}

// startStep is one tick fed to the tracker. wantStall is what Stalled()
// must report for that tick.
type startStep struct {
	sec       int
	state     session.State
	pane      string
	delivered bool
	channel   time.Time // session LastChannelEventAt on this tick
	child     bool      // a live runner child process on this tick
	wantStall bool
}

// runStartSteps feeds the steps to tr in order and checks each tick's verdict.
func runStartSteps(t *testing.T, tr *sessionStartTracker, steps []startStep) {
	t.Helper()
	for _, st := range steps {
		obs := tr.Observe(sessionStartInput{
			Now:        startAt(st.sec),
			State:      st.state,
			Pane:       st.pane,
			Delivered:  st.delivered,
			ChannelAt:  st.channel,
			ChildAlive: st.child,
		})
		stalled, reason := obs.Stalled()
		if stalled != st.wantStall {
			t.Fatalf("t=%ds: Stalled() = %v (WaitingFor %s), want %v", st.sec, stalled, obs.WaitingFor, st.wantStall)
		}
		if stalled && reason != autonomouspkg.SessionStartFailureReason {
			t.Fatalf("t=%ds: stall reason = %q, want %q", st.sec, reason, autonomouspkg.SessionStartFailureReason)
		}
	}
}

func TestSessionStartTrackerSilentStartStallsOnlyPastLimit(t *testing.T) {
	w := session.StateWaitingInput
	tr := newSessionStartTracker(startTrackerLimit, time.Time{}, time.Time{})
	runStartSteps(t, tr, []startStep{
		{sec: 0, state: w, pane: startSilentPane},
		{sec: 120, state: w, pane: startSilentPane}, // equal to the limit does not stall
		{sec: 121, state: w, pane: startSilentPane, wantStall: true},
	})
}

func TestSessionStartTrackerLiveToolCallIsNeverEnded(t *testing.T) {
	w := session.StateWaitingInput
	var steps []startStep
	for sec := 0; sec <= 300; sec += 60 {
		steps = append(steps, startStep{sec: sec, state: w, pane: startToolPane})
	}
	// The tool line finishes at 360: the pane change is the last evidence, so
	// the silence is measured from there, not from the start of the tool call.
	steps = append(steps,
		startStep{sec: 360, state: w, pane: startSilentPane},
		startStep{sec: 480, state: w, pane: startSilentPane},
		startStep{sec: 481, state: w, pane: startSilentPane, wantStall: true},
	)
	runStartSteps(t, newSessionStartTracker(startTrackerLimit, time.Time{}, time.Time{}), steps)
}

func TestSessionStartTrackerPaneChangeRefreshesClock(t *testing.T) {
	w := session.StateWaitingInput
	runStartSteps(t, newSessionStartTracker(startTrackerLimit, time.Time{}, time.Time{}), []startStep{
		{sec: 0, state: w, pane: startSilentPane},
		{sec: 60, state: w, pane: startStatusPane(1)},
		{sec: 120, state: w, pane: startStatusPane(2)},
		{sec: 180, state: w, pane: startStatusPane(3)},
		{sec: 240, state: w, pane: startStatusPane(4)},
		{sec: 300, state: w, pane: startStatusPane(4)},
		{sec: 360, state: w, pane: startStatusPane(4)},
		{sec: 361, state: w, pane: startStatusPane(4), wantStall: true},
	})
}

func TestSessionStartTrackerLiveChildProcessKeepsClockRunning(t *testing.T) {
	w := session.StateWaitingInput
	runStartSteps(t, newSessionStartTracker(startTrackerLimit, time.Time{}, time.Time{}), []startStep{
		{sec: 0, state: w, pane: startSilentPane},
		{sec: 100, state: w, pane: startSilentPane, child: true},
		{sec: 200, state: w, pane: startSilentPane, child: true},
		{sec: 300, state: w, pane: startSilentPane},
		{sec: 321, state: w, pane: startSilentPane, wantStall: true},
	})
}

func TestSessionStartTrackerChannelEventAfterDeliveryRefreshesClock(t *testing.T) {
	w := session.StateWaitingInput
	runStartSteps(t, newSessionStartTracker(startTrackerLimit, time.Time{}, time.Time{}), []startStep{
		{sec: 0, state: w, pane: startSilentPane, delivered: true},
		{sec: 90, state: w, pane: startSilentPane, delivered: true, channel: startAt(90)},
		{sec: 210, state: w, pane: startSilentPane, delivered: true, channel: startAt(90)},
		{sec: 211, state: w, pane: startSilentPane, delivered: true, channel: startAt(90), wantStall: true},
	})
}

func TestSessionStartTrackerChannelEventBeforeDeliveryIsNotEvidence(t *testing.T) {
	w := session.StateWaitingInput
	runStartSteps(t, newSessionStartTracker(startTrackerLimit, time.Time{}, time.Time{}), []startStep{
		{sec: 0, state: w, pane: startSilentPane, channel: startAt(0)},
		{sec: 120, state: w, pane: startSilentPane, channel: startAt(0)},
		{sec: 121, state: w, pane: startSilentPane, channel: startAt(0), wantStall: true},
	})
}

func TestSessionStartTrackerAssistantTurnIsEvidence(t *testing.T) {
	w := session.StateWaitingInput
	// The assistant turn at 60 is evidence, so the silence runs from 60 until
	// the pane changes back to the silent prompt at 150, which is evidence too.
	runStartSteps(t, newSessionStartTracker(startTrackerLimit, time.Time{}, time.Time{}), []startStep{
		{sec: 0, state: w, pane: startSilentPane},
		{sec: 60, state: w, pane: startAssistantPane},
		{sec: 150, state: w, pane: startSilentPane},
		{sec: 270, state: w, pane: startSilentPane},
		{sec: 271, state: w, pane: startSilentPane, wantStall: true},
	})
}

func TestSessionStartTrackerRunningStateDoesNotStartTheClock(t *testing.T) {
	w, r := session.StateWaitingInput, session.StateRunning
	runStartSteps(t, newSessionStartTracker(startTrackerLimit, time.Time{}, time.Time{}), []startStep{
		{sec: 0, state: r, pane: startSilentPane},
		{sec: 300, state: r, pane: startSilentPane},
		{sec: 301, state: w, pane: startSilentPane},
		{sec: 421, state: w, pane: startSilentPane},
		{sec: 422, state: w, pane: startSilentPane, wantStall: true},
	})
}

func TestSessionStartTrackerFailedCaptureRearmsClock(t *testing.T) {
	w := session.StateWaitingInput
	runStartSteps(t, newSessionStartTracker(startTrackerLimit, time.Time{}, time.Time{}), []startStep{
		{sec: 0, state: w, pane: startSilentPane},
		{sec: 100, state: w, pane: ""},
		{sec: 150, state: w, pane: startSilentPane},
		{sec: 271, state: w, pane: startSilentPane, wantStall: true},
	})
}

func TestSessionStartTrackerAdoptedSessionCountsFromLastEvidence(t *testing.T) {
	w := session.StateWaitingInput
	// The adopted session's last evidence of life was 30s before adoption, so
	// the bound has 90s left at the first tick.
	runStartSteps(t, newSessionStartTracker(startTrackerLimit, startAt(-30), time.Time{}), []startStep{
		{sec: 0, state: w, pane: startSilentPane},
		{sec: 90, state: w, pane: startSilentPane},
		{sec: 91, state: w, pane: startSilentPane, wantStall: true},
	})
}

func TestSessionStartTrackerAdoptedSessionWithOldEvidenceIsJudgedAtOnce(t *testing.T) {
	w := session.StateWaitingInput
	runStartSteps(t, newSessionStartTracker(startTrackerLimit, startAt(-600), time.Time{}), []startStep{
		{sec: 0, state: w, pane: startSilentPane, wantStall: true},
	})
}

func TestSessionStartTrackerAdoptedSessionToolCallStillKeepsItAlive(t *testing.T) {
	w := session.StateWaitingInput
	// Seeded with ten-minute-old evidence, but the pane shows a live tool call
	// on the first tick: that is fresh evidence and the worker is not ended.
	runStartSteps(t, newSessionStartTracker(startTrackerLimit, startAt(-600), time.Time{}), []startStep{
		{sec: 0, state: w, pane: startToolPane},
		{sec: 120, state: w, pane: startToolPane},
		{sec: 240, state: w, pane: startToolPane},
	})
}

func TestSessionStartAdoptionSeedUsesLatestEvidence(t *testing.T) {
	updated := startAt(-300)
	channel := startAt(-120)
	logMtime := startAt(-200)
	tests := []struct {
		name                           string
		updatedAt, channelAt, logMtime time.Time
		want                           time.Time
	}{
		{name: "nothing known", want: time.Time{}},
		{name: "only the update time", updatedAt: updated, want: updated},
		{name: "channel event is newest", updatedAt: updated, channelAt: channel, logMtime: logMtime, want: channel},
		{name: "log mtime is newest", updatedAt: updated, channelAt: channel, logMtime: startAt(-10), want: startAt(-10)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := sessionStartAdoptionSeed(tt.updatedAt, tt.channelAt, tt.logMtime); !got.Equal(tt.want) {
				t.Fatalf("sessionStartAdoptionSeed() = %v, want %v", got, tt.want)
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
	mtime := startAt(-45)
	if err := os.Chtimes(path, mtime, mtime); err != nil {
		t.Fatal(err)
	}
	if got := logFileModTime(path); !got.Equal(mtime) {
		t.Errorf("logFileModTime(%s) = %v, want %v", path, got, mtime)
	}
	if got := logFileModTime(filepath.Join(dir, "missing.log")); !got.IsZero() {
		t.Errorf("logFileModTime(missing) = %v, want zero", got)
	}
}
