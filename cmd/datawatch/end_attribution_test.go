package main

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	autonomouspkg "github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/capacity"
	"github.com/dmz006/datawatch/internal/session"
)

// recordingEnder records every EndSession call with the caller and reason it
// was given. It stands in for the session manager on the end paths.
type recordingEnder struct {
	ids  []string
	whys []session.EndReason
}

func (r *recordingEnder) EndSession(fullID string, why session.EndReason) error {
	r.ids = append(r.ids, fullID)
	r.whys = append(r.whys, why)
	return nil
}

func TestEndAttributionCallersAreNamed(t *testing.T) {
	callers := []string{
		endCallerNudgeStall, endCallerSessionStart, endCallerWaitingInput,
		endCallerIdleDetector, endCallerExitSilence, endCallerSSEStall,
		endCallerActivityStall, endCallerDoneCheckpoint, endCallerPostWait,
		endCallerSSEWatchdog, endCallerReconcileHost, endCallerKillOnExit,
		endCallerShutdown, endCallerExitHook, endCallerCLIStopAll, endCallerCLIKill,
	}
	seen := map[string]bool{}
	for _, c := range callers {
		if c == "" {
			t.Fatal("an end caller constant is empty")
		}
		if seen[c] {
			t.Errorf("end caller %q is used by two paths", c)
		}
		seen[c] = true
		if !strings.HasPrefix(c, "autonomous.") && !strings.HasPrefix(c, "daemon.") && !strings.HasPrefix(c, "cli.") {
			t.Errorf("end caller %q has no autonomous., daemon. or cli. prefix", c)
		}
	}
}

func TestEndAttributionIdleDetectorReasonNamesMeasuredNumbers(t *testing.T) {
	obs := autonomouspkg.SessionIdleObservation{
		State:               "waiting_input",
		IdleFor:             912 * time.Second,
		OutputAge:           905 * time.Second,
		HasLiveChildProcess: false,
		Limit:               900 * time.Second,
	}
	want := "session idle detector: waiting_input for 912s, limit 900s, no live child process, output age 905s"
	if got := idleDetectorEndReason(obs); got != want {
		t.Errorf("idleDetectorEndReason() = %q\nwant %q", got, want)
	}
	obs.HasLiveChildProcess = true
	if got := idleDetectorEndReason(obs); !strings.Contains(got, "live child process,") {
		t.Errorf("live child process not named: %q", got)
	}
}

func TestEndAttributionDetectorReasonsCarryTheirNumbers(t *testing.T) {
	cases := []struct {
		name   string
		reason string
		parts  []string
	}{
		{
			name: "nudge stall",
			reason: nudgeStallEndReason(autonomouspkg.WorkerNudgeObservation{
				State: "waiting_input", OutputAge: 120 * time.Second, CompletionSeen: true,
			}, 90*time.Second),
			parts: []string{"nudge stall detector", "no output for 120s", "stall window 90s", "completion marker seen true"},
		},
		{
			name: "session start",
			reason: sessionStartEndReason(autonomouspkg.SessionStartObservation{
				State: "waiting_input", WaitingFor: 75 * time.Second, Limit: 60 * time.Second,
			}),
			parts: []string{"session start detector", "waiting_input without assistant output for 75s", "limit 60s"},
		},
		{
			name:   "waiting input",
			reason: waitingInputEndReason(3),
			parts:  []string{"waiting_input stall detector", "3 consecutive ticks", "threshold 3"},
		},
		{
			name: "sse watchdog",
			reason: sseWatchdogEndReason(sseStallMatch{
				Pattern: "rate limit",
				Line:    "AI_APICallError: rate limit",
				Backend: "opencode-acp",
				Source:  sseStallSourceWatchdog,
			}, session.StateRunning, 6*time.Minute),
			parts: []string{"SSE stall watchdog", `"rate limit"`, "running for 360s"},
		},
		{
			name:   "shutdown",
			reason: shutdownEndReason(os.Interrupt),
			parts:  []string{"daemon shutdown on signal interrupt"},
		},
		{
			name:   "exit hook",
			reason: exitHookRestartEndReason("hook-7"),
			parts:  []string{"exit hook hook-7 restart"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if strings.TrimSpace(tc.reason) == "" {
				t.Fatal("reason is empty")
			}
			for _, p := range tc.parts {
				if !strings.Contains(tc.reason, p) {
					t.Errorf("reason %q does not contain %q", tc.reason, p)
				}
			}
		})
	}
}

func TestEndAttributionAdapterForwardsCallerAndReason(t *testing.T) {
	rec := &recordingEnder{}
	kill := sessionEndFromAutonomous(rec)
	if err := kill("sess-1", "autonomous.verifier.idle-detector", "session idle detector: waiting_input for 912s"); err != nil {
		t.Fatalf("kill: %v", err)
	}
	if len(rec.ids) != 1 || rec.ids[0] != "sess-1" {
		t.Fatalf("ended ids = %v, want [sess-1]", rec.ids)
	}
	got := rec.whys[0]
	if got.Caller != "autonomous.verifier.idle-detector" || got.Reason != "session idle detector: waiting_input for 912s" {
		t.Errorf("recorded end = %+v", got)
	}
}

func TestEndAttributionReconcileHostForwardsReason(t *testing.T) {
	rec := &recordingEnder{}
	h := &verifierReconcileHost{ender: rec}
	reason := "verifier reconcile: lease holder has no live session"
	if err := h.End("verify-sess", reason); err != nil {
		t.Fatalf("End: %v", err)
	}
	if len(rec.ids) != 1 || rec.ids[0] != "verify-sess" {
		t.Fatalf("ended ids = %v, want [verify-sess]", rec.ids)
	}
	if got := rec.whys[0]; got.Reason != reason {
		t.Errorf("reason = %q, want the reconcile's reason %q", got.Reason, reason)
	}
	if got := rec.whys[0]; got.Caller != endCallerReconcileHost {
		t.Errorf("caller = %q, want %q", got.Caller, endCallerReconcileHost)
	}
}

func TestEndAttributionStalledWorkerKillRecordsReason(t *testing.T) {
	var events []string
	sessions := &fakeStallSessions{events: &events}
	store := &fakeStallStore{events: &events}
	task := &autonomouspkg.Task{ID: "task-1", PRDID: "prd-1"}
	why := session.EndReason{
		Caller: endCallerNudgeStall,
		Reason: nudgeStallEndReason(autonomouspkg.WorkerNudgeObservation{State: "waiting_input", OutputAge: 200 * time.Second}, 180*time.Second),
	}

	killStalledWorker(context.Background(), sessions, store, false, "", task, "sess-1", why)

	if len(sessions.whys) != 1 {
		t.Fatalf("EndSession calls = %d, want 1", len(sessions.whys))
	}
	if sessions.whys[0] != why {
		t.Errorf("recorded end = %+v, want %+v", sessions.whys[0], why)
	}
}

func TestEndAttributionDoneCheckpointAcceptRecordsReason(t *testing.T) {
	dir := t.TempDir()
	script := writeDoneScript(t, dir, "verify.sh", "#!/usr/bin/env bash\nexit 0\n")
	killer := &fakeDoneKiller{}

	_, _, passed := doneCheckpointVerify(killer, "sess1", doneCheckpointAllDone, script, dir, nil)
	if !passed {
		t.Fatal("passed = false, want true")
	}
	if len(killer.whys) != 1 {
		t.Fatalf("EndSession calls = %d, want 1", len(killer.whys))
	}
	got := killer.whys[0]
	if got.Caller != endCallerDoneCheckpoint || got.Reason != doneCheckpointEndReason() {
		t.Errorf("recorded end = %+v", got)
	}
}

func TestEndAttributionLeaseVerdictNamesEvidence(t *testing.T) {
	now := time.Date(2026, 10, 24, 2, 0, 0, 0, time.UTC)
	notRunning := func(string) bool { return false }
	running := func(string) bool { return true }

	t.Run("bound session running is live", func(t *testing.T) {
		live, why := leaseLiveVerdict(capacity.Lease{Holder: "worker", SessionID: "sess-a", Acquired: now}, nil, running, now)
		if !live || why != "" {
			t.Errorf("live=%v why=%q, want live with no reason", live, why)
		}
	})

	t.Run("bound session gone names the session", func(t *testing.T) {
		live, why := leaseLiveVerdict(capacity.Lease{Holder: "worker", SessionID: "sess-a", Acquired: now}, nil, notRunning, now)
		if live {
			t.Fatal("lease live with a dead session")
		}
		if !strings.Contains(why, "bound session sess-a is not running") {
			t.Errorf("why = %q, want it to name sess-a", why)
		}
	})

	t.Run("unbound lease past spawn grace names the age and grace", func(t *testing.T) {
		acquired := now.Add(-(spawnGrace + 90*time.Second))
		live, why := leaseLiveVerdict(capacity.Lease{Holder: "worker", Acquired: acquired}, nil, notRunning, now)
		if live {
			t.Fatal("unbound lease past grace judged live")
		}
		if !strings.Contains(why, "acquired 690s ago") || !strings.Contains(why, "600s spawn grace") {
			t.Errorf("why = %q, want the acquired age and the grace", why)
		}
	})

	t.Run("unbound lease inside spawn grace is live", func(t *testing.T) {
		live, _ := leaseLiveVerdict(capacity.Lease{Holder: "worker", Acquired: now.Add(-time.Minute)}, nil, notRunning, now)
		if !live {
			t.Fatal("unbound lease inside grace judged dead")
		}
	})
}

func TestEndAttributionCLIReasonsNameTheTmuxSession(t *testing.T) {
	kill := cliKillEndReason("sess-1", "datawatch-sess-1")
	if !strings.Contains(kill, "session sess-1") || !strings.Contains(kill, "tmux kill-session on datawatch-sess-1") {
		t.Errorf("cliKillEndReason() = %q", kill)
	}
	stop := cliStopAllEndReason("datawatch-sess-2", session.StateRunning)
	if !strings.Contains(stop, "datawatch-sess-2") || !strings.Contains(stop, "state was running") {
		t.Errorf("cliStopAllEndReason() = %q", stop)
	}
}
