package main

import (
	"fmt"
	"testing"
	"time"

	autonomouspkg "github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/session"
)

// These tests pin what the launch path does with a worker, from the tracker the
// wait loop builds: who is left alone, who is ended and why, and where the
// clock starts. They run on the fake clock of the tracker tests, so nothing
// reads the wall clock or touches tmux.

// startOutcomeLimit is the bound the wait loop builds when the config leaves
// the start-confirm seconds unset.
var startOutcomeLimit = time.Duration(config.NormalizeSessionStartConfirmSeconds(0)) * time.Second

// startOutcomeFailureReason is the failure reason recorded on a task whose
// worker never started.
const startOutcomeFailureReason = "session never started"

// startOutcomeSteps is one tick every spacing seconds from 0 to last, all
// parked in waiting_input on the same pane with the task delivered.
func startOutcomeSteps(spacing, last int, pane string) []startStep {
	var steps []startStep
	for sec := 0; sec <= last; sec += spacing {
		steps = append(steps, startStep{sec: sec, state: session.StateWaitingInput, pane: pane, delivered: true})
	}
	return steps
}

func TestStartOutcomeDefaultLimitIsTwoMinutes(t *testing.T) {
	if startOutcomeLimit != 120*time.Second {
		t.Fatalf("default start-confirm limit = %s, want 2m0s", startOutcomeLimit)
	}
	if autonomouspkg.SessionStartFailureReason != startOutcomeFailureReason {
		t.Fatalf("failure reason = %q, want %q", autonomouspkg.SessionStartFailureReason, startOutcomeFailureReason)
	}
}

func TestStartOutcomeLaunchedWorkerStartsInTime(t *testing.T) {
	w := session.StateWaitingInput
	for _, tc := range []struct {
		name  string
		pane  string
		other string // pane the worker shows on its other ticks
	}{
		{name: "assistant turn", pane: startAssistantPane, other: startAssistantPane},
		{name: "tool call", pane: startToolPane, other: startToolPane},
	} {
		t.Run(tc.name+" shown at the last second of the limit", func(t *testing.T) {
			// The evidence arrives at 120s, still inside the limit, after a
			// silent start. The worker is never ended, however long it then
			// stays on that pane.
			steps := []startStep{
				{sec: 0, state: w, pane: startSilentPane, delivered: true},
				{sec: 60, state: w, pane: startSilentPane, delivered: true},
				{sec: 120, state: w, pane: tc.pane, delivered: true},
			}
			for sec := 180; sec <= 1200; sec += 60 {
				steps = append(steps, startStep{sec: sec, state: w, pane: tc.other, delivered: true})
			}
			runStartSteps(t, newSessionStartTracker(startOutcomeLimit, time.Time{}, time.Time{}), steps)
		})
		t.Run(tc.name+" from the first tick", func(t *testing.T) {
			runStartSteps(t, newSessionStartTracker(startOutcomeLimit, time.Time{}, time.Time{}), startOutcomeSteps(30, 1200, tc.pane))
		})
	}
}

func TestStartOutcomeWorkerThatWorkedInTimeIsReportedAsEvidence(t *testing.T) {
	// The observation itself carries the evidence, and a worker with evidence
	// is never Stalled however long the clock has run.
	for _, tc := range []struct {
		name         string
		pane         string
		wantAssist   bool
		wantToolCall bool
	}{
		{name: "assistant turn", pane: startAssistantPane, wantAssist: true},
		{name: "tool call", pane: startToolPane, wantToolCall: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tr := newSessionStartTracker(startOutcomeLimit, time.Time{}, time.Time{})
			var obs autonomouspkg.SessionStartObservation
			for sec := 0; sec <= 600; sec += 60 {
				obs = tr.Observe(sessionStartInput{Now: startAt(sec), State: session.StateWaitingInput, Pane: tc.pane})
			}
			if obs.HasAssistantOutput != tc.wantAssist {
				t.Fatalf("HasAssistantOutput = %v, want %v", obs.HasAssistantOutput, tc.wantAssist)
			}
			if obs.Evidence.ToolCallSeen != tc.wantToolCall {
				t.Fatalf("Evidence.ToolCallSeen = %v, want %v", obs.Evidence.ToolCallSeen, tc.wantToolCall)
			}
			if stalled, _ := obs.Stalled(); stalled {
				t.Fatalf("worker with %s evidence was stalled (WaitingFor %s)", tc.name, obs.WaitingFor)
			}
		})
	}
}

func TestStartOutcomeParkedWorkerWithNoEvidenceStallsPastTheLimit(t *testing.T) {
	w := session.StateWaitingInput
	tr := newSessionStartTracker(startOutcomeLimit, time.Time{}, time.Time{})
	runStartSteps(t, tr, []startStep{
		{sec: 0, state: w, pane: startSilentPane, delivered: true},
		{sec: 60, state: w, pane: startSilentPane, delivered: true},
		{sec: 120, state: w, pane: startSilentPane, delivered: true},
		{sec: 121, state: w, pane: startSilentPane, delivered: true, wantStall: true},
	})
}

func TestStartOutcomeStallIsStrictlyPastTheLimit(t *testing.T) {
	cases := []struct {
		name      string
		silentFor time.Duration
		wantStall bool
	}{
		{name: "one nanosecond short", silentFor: startOutcomeLimit - time.Nanosecond},
		{name: "exactly the limit", silentFor: startOutcomeLimit},
		{name: "one nanosecond past", silentFor: startOutcomeLimit + time.Nanosecond, wantStall: true},
		{name: "one second past", silentFor: startOutcomeLimit + time.Second, wantStall: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tr := newSessionStartTracker(startOutcomeLimit, time.Time{}, time.Time{})
			first := tr.Observe(sessionStartInput{Now: startAt(0), State: session.StateWaitingInput, Pane: startSilentPane, Delivered: true})
			if stalled, _ := first.Stalled(); stalled {
				t.Fatalf("first silent tick stalled (WaitingFor %s)", first.WaitingFor)
			}
			obs := tr.Observe(sessionStartInput{Now: startAt(0).Add(tc.silentFor), State: session.StateWaitingInput, Pane: startSilentPane, Delivered: true})
			if obs.WaitingFor != tc.silentFor {
				t.Fatalf("WaitingFor = %s, want %s", obs.WaitingFor, tc.silentFor)
			}
			if obs.Limit != startOutcomeLimit {
				t.Fatalf("Limit = %s, want %s", obs.Limit, startOutcomeLimit)
			}
			stalled, reason := obs.Stalled()
			if stalled != tc.wantStall {
				t.Fatalf("Stalled() = %v at %s of silence, want %v", stalled, tc.silentFor, tc.wantStall)
			}
			wantReason := ""
			if tc.wantStall {
				wantReason = startOutcomeFailureReason
			}
			if reason != wantReason {
				t.Fatalf("reason = %q, want %q", reason, wantReason)
			}
		})
	}
}

func TestStartOutcomeStalledWorkerEndReasonNamesTheSilence(t *testing.T) {
	tr := newSessionStartTracker(startOutcomeLimit, time.Time{}, time.Time{})
	tr.Observe(sessionStartInput{Now: startAt(0), State: session.StateWaitingInput, Pane: startSilentPane, Delivered: true})
	obs := tr.Observe(sessionStartInput{Now: startAt(121), State: session.StateWaitingInput, Pane: startSilentPane, Delivered: true})
	want := "session start detector: waiting_input without assistant output for 121s since its last evidence of work (or first silent tick), limit 120s"
	if got := sessionStartEndReason(obs); got != want {
		t.Fatalf("end reason = %q, want %q", got, want)
	}
}

func TestStartOutcomeNeverStartedWorkerStallsAtTheSameTickWhateverTheSpacing(t *testing.T) {
	w := session.StateWaitingInput
	limit := int(startOutcomeLimit / time.Second)
	for _, spacing := range []int{1, 2, 5, 7, 10, 30, 45, 60, 119, 120, 121, 200} {
		t.Run(fmt.Sprintf("spacing %ds", spacing), func(t *testing.T) {
			// The clock starts at the first tick whatever the spacing, so the
			// first tick strictly past the limit is the first multiple of the
			// spacing above 120s, and no earlier tick ends the worker.
			wantStallAt := (limit/spacing + 1) * spacing
			tr := newSessionStartTracker(startOutcomeLimit, time.Time{}, time.Time{})
			stalledAt := -1
			for sec := 0; sec <= wantStallAt+spacing; sec += spacing {
				obs := tr.Observe(sessionStartInput{Now: startAt(sec), State: w, Pane: startSilentPane, Delivered: true})
				if want := time.Duration(sec) * time.Second; obs.WaitingFor != want {
					t.Fatalf("t=%ds: WaitingFor = %s, want %s", sec, obs.WaitingFor, want)
				}
				if stalled, reason := obs.Stalled(); stalled {
					if reason != startOutcomeFailureReason {
						t.Fatalf("t=%ds: reason = %q, want %q", sec, reason, startOutcomeFailureReason)
					}
					stalledAt = sec
					break
				}
			}
			if stalledAt != wantStallAt {
				t.Fatalf("stalled at %ds, want %ds", stalledAt, wantStallAt)
			}
			if stalledAt <= limit || stalledAt > limit+spacing {
				t.Fatalf("stall at %ds is outside (%ds, %ds]", stalledAt, limit, limit+spacing)
			}
		})
	}
}

func TestStartOutcomeDenseAndSparseTickingEndTheSameWorkerAtTheSameMoment(t *testing.T) {
	// Ticking every second and ticking once at 0s and once at 121s reach the
	// same verdict on the same silence.
	w := session.StateWaitingInput
	dense := newSessionStartTracker(startOutcomeLimit, time.Time{}, time.Time{})
	runStartSteps(t, dense, append(startOutcomeSteps(1, 120, startSilentPane),
		startStep{sec: 121, state: w, pane: startSilentPane, delivered: true, wantStall: true}))

	sparse := newSessionStartTracker(startOutcomeLimit, time.Time{}, time.Time{})
	runStartSteps(t, sparse, []startStep{
		{sec: 0, state: w, pane: startSilentPane, delivered: true},
		{sec: 121, state: w, pane: startSilentPane, delivered: true, wantStall: true},
	})
}

func TestStartOutcomeAdoptedWorkerBusyUpToTheRestartIsNotChargedForTheDowntime(t *testing.T) {
	w := session.StateWaitingInput
	// The worker's pane was still writing when the daemon went down, so the
	// log mtime is the restart moment. The record's own UpdatedAt and the last
	// channel event are older, and the seed takes the latest of the three.
	seed := sessionStartAdoptionSeed(startAt(-900), startAt(-600), startAt(0))
	if !seed.Equal(startAt(0)) {
		t.Fatalf("adoption seed = %v, want %v", seed, startAt(0))
	}
	tr := newSessionStartTracker(startOutcomeLimit, seed, startAt(-600))
	runStartSteps(t, tr, []startStep{
		{sec: 0, state: w, pane: startSilentPane},
		{sec: 60, state: w, pane: startSilentPane},
		{sec: 120, state: w, pane: startSilentPane}, // exactly the limit after the restart
		{sec: 121, state: w, pane: startSilentPane, wantStall: true},
	})
}

func TestStartOutcomeAdoptedWorkerBusyUpToTheRestartHasTheFullLimitAtTheFirstTick(t *testing.T) {
	seed := sessionStartAdoptionSeed(startAt(-900), startAt(-600), startAt(0))
	tr := newSessionStartTracker(startOutcomeLimit, seed, time.Time{})
	obs := tr.Observe(sessionStartInput{Now: startAt(0), State: session.StateWaitingInput, Pane: startSilentPane})
	if obs.WaitingFor != 0 {
		t.Fatalf("WaitingFor at the restart = %s, want 0s", obs.WaitingFor)
	}
	if stalled, _ := obs.Stalled(); stalled {
		t.Fatal("adopted worker busy up to the restart was stalled at the first tick")
	}
}

func TestStartOutcomeAdoptedWorkerWithOldEvidenceIsJudgedAtOnce(t *testing.T) {
	w := session.StateWaitingInput
	// Every sign of life is ten minutes before the restart, so the silence is
	// already past the limit on the first tick.
	seed := sessionStartAdoptionSeed(startAt(-900), startAt(-600), startAt(-700))
	if !seed.Equal(startAt(-600)) {
		t.Fatalf("adoption seed = %v, want %v", seed, startAt(-600))
	}
	tr := newSessionStartTracker(startOutcomeLimit, seed, startAt(-600))
	obs := tr.Observe(sessionStartInput{Now: startAt(0), State: w, Pane: startSilentPane})
	if obs.WaitingFor != 600*time.Second {
		t.Fatalf("WaitingFor = %s, want 10m0s", obs.WaitingFor)
	}
	stalled, reason := obs.Stalled()
	if !stalled || reason != startOutcomeFailureReason {
		t.Fatalf("Stalled() = %v, %q at the first tick, want true, %q", stalled, reason, startOutcomeFailureReason)
	}
}

func TestStartOutcomeAdoptedWorkerWithOldEvidenceAndNoLogIsJudgedAtOnce(t *testing.T) {
	// A log that cannot be read leaves a zero mtime, which must not hide the
	// age of the other evidence.
	w := session.StateWaitingInput
	seed := sessionStartAdoptionSeed(startAt(-300), time.Time{}, time.Time{})
	runStartSteps(t, newSessionStartTracker(startOutcomeLimit, seed, time.Time{}), []startStep{
		{sec: 0, state: w, pane: startSilentPane, wantStall: true},
	})
}

func TestStartOutcomeAdoptedWorkerStaysAliveWhileItKeepsWorkingAfterTheRestart(t *testing.T) {
	// Old evidence, but the pane shows a tool call from the first tick on.
	seed := sessionStartAdoptionSeed(startAt(-900), startAt(-600), startAt(-700))
	runStartSteps(t, newSessionStartTracker(startOutcomeLimit, seed, time.Time{}), startOutcomeSteps(60, 1200, startToolPane))
}
