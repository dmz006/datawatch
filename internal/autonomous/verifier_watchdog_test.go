package autonomous

import (
	"strings"
	"testing"
	"time"
)

func TestVerifierWatchdogSilentSessionEndsAtLimit(t *testing.T) {
	start := time.Unix(1000, 0)
	limit := 10 * time.Minute
	w := NewVerifierWatchdog(start, limit)
	if w.Observe(start.Add(limit-time.Second), false, "same") {
		t.Fatal("exceeded before the limit")
	}
	if !w.Observe(start.Add(limit), false, "same") {
		t.Fatal("silent session not ended at the limit")
	}
	if msg := VerifierWatchdogMessage(limit); !strings.Contains(msg, "verifier no-progress watchdog") || !strings.Contains(msg, "10m0s") {
		t.Fatalf("message %q does not name the watchdog and limit", msg)
	}
}

func TestVerifierWatchdogSparesOutputAndChangingPane(t *testing.T) {
	start := time.Unix(1000, 0)
	limit := 10 * time.Minute
	w := NewVerifierWatchdog(start, limit)
	for i := 1; i <= 100; i++ {
		if w.Observe(start.Add(time.Duration(i)*3*time.Minute), false, string(rune('a'+i%26))+string(rune(i))) {
			t.Fatalf("changing pane output ended at poll %d", i)
		}
	}
	w2 := NewVerifierWatchdog(start, limit)
	if w2.Observe(start.Add(3*limit), true, "x") {
		t.Fatal("session with an output file was ended")
	}
}

func TestVerifierWatchdogTimerResetsOnActivity(t *testing.T) {
	start := time.Unix(1000, 0)
	limit := 10 * time.Minute
	w := NewVerifierWatchdog(start, limit)
	w.Observe(start, false, "a")
	if w.Observe(start.Add(9*time.Minute), false, "a") {
		t.Fatal("ended early")
	}
	if w.Observe(start.Add(9*time.Minute+time.Second), false, "b") {
		t.Fatal("ended on activity")
	}
	if w.Observe(start.Add(18*time.Minute), false, "b") {
		t.Fatal("timer did not reset on pane change")
	}
	if !w.Observe(start.Add(19*time.Minute+2*time.Second), false, "b") {
		t.Fatal("not ended a full limit after the last activity")
	}
}

func TestVerifierWatchdogDisabledWhenLimitNotPositive(t *testing.T) {
	w := NewVerifierWatchdog(time.Unix(0, 0), 0)
	if w.Observe(time.Unix(1<<30, 0), false, "") {
		t.Fatal("zero limit must disable the watchdog")
	}
}

// decideStep is one poll in a table-driven Decide scenario: the offset from
// the watchdog start time, the signals seen at that poll, and whether the
// no-progress limit must be exceeded there.
type decideStep struct {
	at   time.Duration
	sig  ProgressSignals
	want bool
}

// TestVerifierWatchdogDecideRules checks the confirmed progress rule with
// injected time (proposal 2026-10-24 verifier watchdog confirm, answers 1
// and 3): a live runner process or a held capacity slot counts as progress
// and spares a silent session even past the old 600s limit; a dead runner
// with no activity still ends exactly at the limit; time spent waiting for
// a capacity slot never counts toward the limit and never trips it; and
// any activity signal resets the timer.
func TestVerifierWatchdogDecideRules(t *testing.T) {
	limit := 10 * time.Minute // comfortably above the old 600s default

	cases := []struct {
		name  string
		steps []decideStep
	}{
		{
			// A print-mode verifier can be silent for the whole run while its
			// runner process is alive; silence past even the old limit must
			// not end it (confirmed rule: a live runner counts as progress).
			name: "silent-live-runner-survives-past-old-limit",
			steps: []decideStep{
				{at: 0, sig: ProgressSignals{Marker: "a"}, want: false},
				{at: 6 * time.Minute, sig: ProgressSignals{Marker: "a", RunnerAlive: true}, want: false},
				{at: 9 * time.Minute, sig: ProgressSignals{Marker: "a", RunnerAlive: true}, want: false},
				{at: 12 * time.Minute, sig: ProgressSignals{Marker: "a", RunnerAlive: true}, want: false},
			},
		},
		{
			// With no live runner, no slot involvement, no output file and no
			// pane change, the session is still ended exactly at the limit,
			// as before the rule change.
			name: "dead-runner_without-activity-ends-at-limit",
			steps: []decideStep{
				{at: 0, sig: ProgressSignals{Marker: "a"}, want: false},
				{at: limit - time.Second, sig: ProgressSignals{Marker: "a"}, want: false},
				{at: limit, sig: ProgressSignals{Marker: "a"}, want: true},
			},
		},
		{
			// Holding the capacity slot is itself progress: the run survives
			// while the slot is held, and ends once the slot is released with
			// no other activity.
			name: "held-slot-counts-as-progress",
			steps: []decideStep{
				{at: 0, sig: ProgressSignals{Marker: "a"}, want: false},
				{at: limit + time.Minute, sig: ProgressSignals{Marker: "a", Slot: SlotHeld}, want: false},
				{at: 2*limit + time.Minute, sig: ProgressSignals{Marker: "a", Slot: SlotHeld}, want: false},
				// Slot released with nothing else: the timer restarted at the
				// last held poll (21m), so a full limit from there must end it.
				{at: 2*limit + time.Minute + time.Second, sig: ProgressSignals{Marker: "a", Slot: SlotNone}, want: false},
				{at: 31*time.Minute - time.Second, sig: ProgressSignals{Marker: "a", Slot: SlotNone}, want: false},
				{at: 31 * time.Minute, sig: ProgressSignals{Marker: "a", Slot: SlotNone}, want: true},
			},
		},
		{
			// Confirmed answer 3: time waiting for a slot is excluded from the
			// limit and the wait itself never trips it. Silence before the
			// wait still counts. Timeline (limit 10m): baseline at 0; queued
			// from 4m; 27m of queue time excluded on exit at 31m+1s, leaving
			// only the 4m pre-wait silence counted. Trip when counted silence
			// reaches 10m, at 37m+1s.
			name: "slot-wait-time-excluded-from-limit",
			steps: []decideStep{
				{at: 0, sig: ProgressSignals{Marker: "a"}, want: false},
				{at: 4 * time.Minute, sig: ProgressSignals{Marker: "a", Slot: SlotWaiting}, want: false},
				{at: 30 * time.Minute, sig: ProgressSignals{Marker: "a", Slot: SlotWaiting}, want: false},
				{at: 31*time.Minute + time.Second, sig: ProgressSignals{Marker: "a"}, want: false},
				{at: 36 * time.Minute, sig: ProgressSignals{Marker: "a"}, want: false},
				{at: 37 * time.Minute, sig: ProgressSignals{Marker: "a"}, want: false},
				{at: 37*time.Minute + time.Second, sig: ProgressSignals{Marker: "a"}, want: true},
			},
		},
	}

	// Every confirmed activity signal resets the timer: output file present,
	// pane marker change, live runner, held slot.
	resetSignals := []struct {
		name string
		sig  ProgressSignals
	}{
		{"output-file", ProgressSignals{OutputPresent: true, Marker: "a"}},
		{"pane-change", ProgressSignals{Marker: "b"}},
		{"live-runner", ProgressSignals{RunnerAlive: true, Marker: "a"}},
		{"held-slot", ProgressSignals{Slot: SlotHeld, Marker: "a"}},
	}
	for _, rs := range resetSignals {
		cases = append(cases, struct {
			name  string
			steps []decideStep
		}{
			name: "reset-on-" + rs.name,
			steps: []decideStep{
				{at: 0, sig: ProgressSignals{Marker: "a"}, want: false},
				// 9m of silence, then the activity signal resets the baseline.
				{at: 9 * time.Minute, sig: rs.sig, want: false},
				// Shorter than a full limit after the reset: still spare.
				{at: 9*time.Minute + 9*time.Minute + time.Second, sig: ProgressSignals{Marker: rs.sig.Marker}, want: false},
				// A full limit with nothing since the reset: end.
				{at: 9*time.Minute + limit + time.Second, sig: ProgressSignals{Marker: rs.sig.Marker}, want: true},
			},
		})
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			start := time.Unix(1000, 0)
			w := NewVerifierWatchdog(start, limit)
			for i, step := range tc.steps {
				got := w.Decide(start.Add(step.at), step.sig)
				if got != step.want {
					t.Fatalf("step %d at +%v with %+v: Decide = %v, want %v", i, step.at, step.sig, got, step.want)
				}
			}
		})
	}
}

// TestVerifierWatchdogActiveRequestNotProgressYet pins the deferred signal
// (confirmed answer 1 defers an active model-server request until a
// per-session probe exists): ActiveRequest alone must not count as
// progress, so a dead-runner session still ends at the limit.
func TestVerifierWatchdogActiveRequestNotProgressYet(t *testing.T) {
	limit := 10 * time.Minute
	start := time.Unix(1000, 0)
	w := NewVerifierWatchdog(start, limit)
	w.Decide(start, ProgressSignals{Marker: "a"})
	if got := w.Decide(start.Add(limit), ProgressSignals{Marker: "a", ActiveRequest: true}); !got {
		t.Fatal("active request alone must not spare the session until the probe exists")
	}
}

// TestVerifierWatchdogMessageNamesWatchdogAndLimit pins the error text so
// it keeps naming the watchdog and the limit after the new signals.
func TestVerifierWatchdogMessageNamesWatchdogAndLimit(t *testing.T) {
	msg := VerifierWatchdogMessage(15 * time.Minute)
	if !strings.Contains(msg, "verifier no-progress watchdog") || !strings.Contains(msg, "15m0s") {
		t.Fatalf("message %q does not name the watchdog and limit", msg)
	}
}
