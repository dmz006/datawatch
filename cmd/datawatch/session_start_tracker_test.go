package main

import (
	"fmt"
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
	children  []int     // pids the runner has live on this tick (its own children only)
	failProbe bool      // the process tree could not be read on this tick
	wantStall bool
}

// runStartSteps feeds the steps to tr in order and checks each tick's verdict.
func runStartSteps(t *testing.T, tr *autonomouspkg.StartTracker, steps []startStep) {
	t.Helper()
	for _, st := range steps {
		obs := tr.Observe(autonomouspkg.StartInput{
			Now:       startAt(st.sec),
			State:     string(st.state),
			Pane:      st.pane,
			Delivered: st.delivered,
			ChannelAt: st.channel,
			ChildPIDs: childPIDSet(st),
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
	tr := autonomouspkg.NewStartTracker(startTrackerLimit, time.Time{}, time.Time{})
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
	runStartSteps(t, autonomouspkg.NewStartTracker(startTrackerLimit, time.Time{}, time.Time{}), steps)
}

func TestSessionStartTrackerPaneChangeRefreshesClock(t *testing.T) {
	w := session.StateWaitingInput
	runStartSteps(t, autonomouspkg.NewStartTracker(startTrackerLimit, time.Time{}, time.Time{}), []startStep{
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

// childPIDSet is the probe result a step reports: nil when the probe failed,
// otherwise the set of pids the runner has live (empty when it has none).
func childPIDSet(st startStep) map[int]bool {
	if st.failProbe {
		return nil
	}
	pids := make(map[int]bool, len(st.children))
	for _, pid := range st.children {
		pids[pid] = true
	}
	return pids
}

func TestSessionStartTrackerNewChildProcessKeepsClockRunning(t *testing.T) {
	w := session.StateWaitingInput
	// The command the runner starts at 100 is evidence. It is still running at
	// 200, but a process that merely persists is not new work, so the clock
	// keeps running from 100 and the silence ends at 221.
	runStartSteps(t, autonomouspkg.NewStartTracker(startTrackerLimit, time.Time{}, time.Time{}), []startStep{
		{sec: 0, state: w, pane: startSilentPane},
		{sec: 100, state: w, pane: startSilentPane, children: []int{7002}},
		{sec: 200, state: w, pane: startSilentPane, children: []int{7002}},
		{sec: 220, state: w, pane: startSilentPane, children: []int{7002}},
		{sec: 221, state: w, pane: startSilentPane, children: []int{7002}, wantStall: true},
	})
}

func TestSessionStartTrackerPersistentHelperDoesNotKeepDialogAlive(t *testing.T) {
	w := session.StateWaitingInput
	// An MCP helper started before the dialog is alive from the first tick to
	// the end. It was in the baseline, so it is not evidence and the bound runs
	// from the first silent tick.
	helper := []int{7001}
	runStartSteps(t, autonomouspkg.NewStartTracker(startTrackerLimit, time.Time{}, time.Time{}), []startStep{
		{sec: 0, state: w, pane: startSilentPane, children: helper},
		{sec: 60, state: w, pane: startSilentPane, children: helper},
		{sec: 120, state: w, pane: startSilentPane, children: helper},
		{sec: 121, state: w, pane: startSilentPane, children: helper, wantStall: true},
	})
}

func TestSessionStartTrackerFailedProbeIsNoObservation(t *testing.T) {
	w := session.StateWaitingInput
	// A failed tree read at 60 is neither evidence nor a new baseline. The helper
	// seen at 0 is still the baseline at 100, when a second process appears and
	// counts as work.
	runStartSteps(t, autonomouspkg.NewStartTracker(startTrackerLimit, time.Time{}, time.Time{}), []startStep{
		{sec: 0, state: w, pane: startSilentPane, children: []int{7001}},
		{sec: 60, state: w, pane: startSilentPane, failProbe: true},
		{sec: 100, state: w, pane: startSilentPane, children: []int{7001, 7002}},
		{sec: 220, state: w, pane: startSilentPane, children: []int{7001, 7002}},
		{sec: 221, state: w, pane: startSilentPane, children: []int{7001, 7002}, wantStall: true},
	})
}

func TestSessionStartTrackerDialogStuckRunnerFromProcessTreeIsEnded(t *testing.T) {
	// The probe's own walker feeds the tracker, as the wait loop does. Pane shell
	// 100 runs claude 200, and claude's startup MCP helper 300 is alive. The
	// runner is not work, and the helper was there from the first tick, so the
	// dialog-stuck worker is ended at the limit.
	roots := []int{100}
	tree := map[int][]int{100: {200}, 200: {300}}
	names := map[int]string{100: "bash", 200: "claude", 300: "node"}
	pids := processTreeRunnerDescendants(roots, tree, fakeProcessNames(names))
	tr := autonomouspkg.NewStartTracker(startTrackerLimit, time.Time{}, time.Time{})
	w := session.StateWaitingInput
	for _, sec := range []int{0, 60, 120, 121} {
		obs := tr.Observe(autonomouspkg.StartInput{Now: startAt(sec), State: string(w), Pane: startSilentPane, ChildPIDs: pids})
		stalled, reason := obs.Stalled()
		if want := sec == 121; stalled != want {
			t.Fatalf("t=%ds: Stalled() = %v (WaitingFor %s), want %v", sec, stalled, obs.WaitingFor, want)
		}
		if stalled && reason != autonomouspkg.SessionStartFailureReason {
			t.Fatalf("t=%ds: stall reason = %q, want %q", sec, reason, autonomouspkg.SessionStartFailureReason)
		}
	}
}

func TestSessionStartTrackerRunnerSpawnedCommandFromProcessTreeIsWork(t *testing.T) {
	// Claude 200 runs under the pane shell. A tool call at 100 starts bash 400
	// under it, which is new since the baseline, so the silence restarts there.
	roots := []int{100}
	names := map[int]string{100: "bash", 200: "claude", 400: "bash"}
	base := processTreeRunnerDescendants(roots, map[int][]int{100: {200}}, fakeProcessNames(names))
	spawned := processTreeRunnerDescendants(roots, map[int][]int{100: {200}, 200: {400}}, fakeProcessNames(names))
	tr := autonomouspkg.NewStartTracker(startTrackerLimit, time.Time{}, time.Time{})
	w := session.StateWaitingInput
	steps := []struct {
		sec   int
		pids  map[int]bool
		stall bool
	}{
		{sec: 0, pids: base},
		{sec: 100, pids: spawned},
		{sec: 220, pids: spawned},
		{sec: 221, pids: spawned, stall: true},
	}
	for _, st := range steps {
		obs := tr.Observe(autonomouspkg.StartInput{Now: startAt(st.sec), State: string(w), Pane: startSilentPane, ChildPIDs: st.pids})
		if stalled, _ := obs.Stalled(); stalled != st.stall {
			t.Fatalf("t=%ds: Stalled() = %v (WaitingFor %s), want %v", st.sec, stalled, obs.WaitingFor, st.stall)
		}
	}
}

func TestSessionStartTrackerChannelEventAfterDeliveryRefreshesClock(t *testing.T) {
	w := session.StateWaitingInput
	runStartSteps(t, autonomouspkg.NewStartTracker(startTrackerLimit, time.Time{}, time.Time{}), []startStep{
		{sec: 0, state: w, pane: startSilentPane, delivered: true},
		{sec: 90, state: w, pane: startSilentPane, delivered: true, channel: startAt(90)},
		{sec: 210, state: w, pane: startSilentPane, delivered: true, channel: startAt(90)},
		{sec: 211, state: w, pane: startSilentPane, delivered: true, channel: startAt(90), wantStall: true},
	})
}

func TestSessionStartTrackerChannelEventBeforeDeliveryIsNotEvidence(t *testing.T) {
	w := session.StateWaitingInput
	runStartSteps(t, autonomouspkg.NewStartTracker(startTrackerLimit, time.Time{}, time.Time{}), []startStep{
		{sec: 0, state: w, pane: startSilentPane, channel: startAt(0)},
		{sec: 120, state: w, pane: startSilentPane, channel: startAt(0)},
		{sec: 121, state: w, pane: startSilentPane, channel: startAt(0), wantStall: true},
	})
}

func TestSessionStartTrackerAssistantTurnIsEvidence(t *testing.T) {
	w := session.StateWaitingInput
	// The assistant turn at 60 is evidence, so the silence runs from 60 until
	// the pane changes back to the silent prompt at 150, which is evidence too.
	runStartSteps(t, autonomouspkg.NewStartTracker(startTrackerLimit, time.Time{}, time.Time{}), []startStep{
		{sec: 0, state: w, pane: startSilentPane},
		{sec: 60, state: w, pane: startAssistantPane},
		{sec: 150, state: w, pane: startSilentPane},
		{sec: 270, state: w, pane: startSilentPane},
		{sec: 271, state: w, pane: startSilentPane, wantStall: true},
	})
}

func TestSessionStartTrackerRunningStateDoesNotStartTheClock(t *testing.T) {
	w, r := session.StateWaitingInput, session.StateRunning
	runStartSteps(t, autonomouspkg.NewStartTracker(startTrackerLimit, time.Time{}, time.Time{}), []startStep{
		{sec: 0, state: r, pane: startSilentPane},
		{sec: 300, state: r, pane: startSilentPane},
		{sec: 301, state: w, pane: startSilentPane},
		{sec: 421, state: w, pane: startSilentPane},
		{sec: 422, state: w, pane: startSilentPane, wantStall: true},
	})
}

func TestSessionStartTrackerFailedCaptureRearmsClock(t *testing.T) {
	w := session.StateWaitingInput
	runStartSteps(t, autonomouspkg.NewStartTracker(startTrackerLimit, time.Time{}, time.Time{}), []startStep{
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
	runStartSteps(t, autonomouspkg.NewStartTracker(startTrackerLimit, startAt(-30), time.Time{}), []startStep{
		{sec: 0, state: w, pane: startSilentPane},
		{sec: 90, state: w, pane: startSilentPane},
		{sec: 91, state: w, pane: startSilentPane, wantStall: true},
	})
}

func TestSessionStartTrackerAdoptedSessionWithOldEvidenceIsJudgedAtOnce(t *testing.T) {
	w := session.StateWaitingInput
	runStartSteps(t, autonomouspkg.NewStartTracker(startTrackerLimit, startAt(-600), time.Time{}), []startStep{
		{sec: 0, state: w, pane: startSilentPane, wantStall: true},
	})
}

func TestSessionStartTrackerAdoptedSessionToolCallStillKeepsItAlive(t *testing.T) {
	w := session.StateWaitingInput
	// Seeded with ten-minute-old evidence, but the pane shows a live tool call
	// on the first tick: that is fresh evidence and the worker is not ended.
	runStartSteps(t, autonomouspkg.NewStartTracker(startTrackerLimit, startAt(-600), time.Time{}), []startStep{
		{sec: 0, state: w, pane: startToolPane},
		{sec: 120, state: w, pane: startToolPane},
		{sec: 240, state: w, pane: startToolPane},
	})
}
