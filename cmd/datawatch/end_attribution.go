package main

import (
	"fmt"
	"os"
	"time"

	autonomouspkg "github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/session"
)

// Callers name the daemon code path that ends a session. They are recorded on
// every end as the "by=" field of the journal line and on the session timeline.
const (
	endCallerDescendantStall = "autonomous.verifier.descendant-stall"
	endCallerNudgeStall      = "autonomous.verifier.nudge-stall"
	endCallerSessionStart    = "autonomous.verifier.session-start"
	endCallerWaitingInput    = "autonomous.verifier.waiting-input"
	endCallerIdleDetector    = "autonomous.verifier.idle-detector"
	endCallerExitSilence     = "autonomous.verifier.exit-silence"
	endCallerSSEStall        = "autonomous.verifier.sse-stall"
	endCallerActivityStall   = "autonomous.verifier.activity-stall"
	endCallerDoneCheckpoint  = "autonomous.verifier.done-checkpoint"
	endCallerPostWait        = "autonomous.verifier.post-wait"
	endCallerLoopRescue      = "autonomous.worker.loop-rescue"
	endCallerAttemptLimit    = "autonomous.worker.attempt-limit"
	endCallerSSEWatchdog     = "autonomous.watchdog.sse-stall"
	endCallerReconcileHost   = "autonomous.verifier-reconcile"
	endCallerJudgeReconcile  = "autonomous.judge-reconcile"
	endCallerKillOnExit      = "daemon.kill-sessions-on-exit"
	endCallerShutdown        = "daemon.shutdown"
	endCallerExitHook        = "daemon.exit-hook-restart"
	endCallerCLIStopAll      = "cli.session-stop-all"
	endCallerCLIKill         = "cli.session-kill"
)

// sessionEnder is the slice of the session manager that ends sessions. The end
// paths take it as an interface so a test can record each call.
type sessionEnder interface {
	EndSession(fullID string, why session.EndReason) error
}

// endSecs formats a duration as whole seconds, the unit the detectors' limits
// are configured in.
func endSecs(d time.Duration) string {
	return fmt.Sprintf("%ds", int(d.Round(time.Second).Seconds()))
}

// nudgeStallEndReason names the claude-code idle-nudge stall: the worker stayed
// silent after the one completion nudge for the whole stall window.
func nudgeStallEndReason(obs autonomouspkg.WorkerNudgeObservation, stallAfter time.Duration) string {
	return fmt.Sprintf("nudge stall detector: state %s, no output for %s after the completion nudge, stall window %s, live child process %t, completion marker seen %t",
		obs.State, endSecs(obs.OutputAge), endSecs(stallAfter), obs.HasLiveChildProcess, obs.CompletionSeen)
}

// sessionStartEndReason names the start-confirm stall: a claude-code session
// sat in waiting_input without ever producing an assistant turn, and the silence
// is counted from its last evidence of work (pane change, tool call, new process
// or channel event), not from the first silent tick.
func sessionStartEndReason(obs autonomouspkg.SessionStartObservation) string {
	return fmt.Sprintf("session start detector: %s without assistant output for %s since its last evidence of work (or first silent tick), limit %s",
		obs.State, endSecs(obs.WaitingFor), endSecs(obs.Limit))
}

// waitingInputEndReason names the debounced waiting_input stall.
func waitingInputEndReason(ticks int) string {
	return fmt.Sprintf("waiting_input stall detector: waiting_input on %d consecutive ticks, threshold %d",
		ticks, waitingInputStallThreshold)
}

// idleDetectorEndReason names the session idle detector with the numbers it
// measured: the parked run, its limit, and the output age. It ends with the
// nudge history of the session, so a nudged worker that stayed silent past its
// limit says the nudge came first.
func idleDetectorEndReason(obs autonomouspkg.SessionIdleObservation) string {
	live := "no live child process"
	if obs.HasLiveChildProcess {
		live = "live child process"
	}
	reason := fmt.Sprintf("session idle detector: %s for %s, limit %s, %s, output age %s",
		obs.State, endSecs(obs.IdleFor), endSecs(obs.Limit), live, endSecs(obs.OutputAge))
	if obs.Nudge != "" {
		reason += ", " + obs.Nudge
	}
	return reason
}

// nudgeHistoryNote names the nudge history of one session from its recorded
// nudge: "no nudge sent" when there is none, otherwise the nudge's number, the
// time it was sent and its outcome. A nudge with no outcome yet was sent and not
// answered, so it reads as sent.
func nudgeHistoryNote(ev autonomouspkg.NudgeEvent, nudged bool) string {
	if !nudged {
		return "no nudge sent"
	}
	outcome := ev.Outcome
	if outcome == "" {
		outcome = autonomouspkg.NudgeOutcomeSent
	}
	return fmt.Sprintf("nudge %d sent at %s, outcome %s", ev.Count, ev.At.Format(time.RFC3339), outcome)
}

// sessionNudgeNote reads the nudge history of one session of a task from the
// autonomous manager. A nil manager has recorded no nudges.
func sessionNudgeNote(mgr *autonomouspkg.Manager, prdID, taskID, sessionID string) string {
	if mgr == nil {
		return nudgeHistoryNote(autonomouspkg.NudgeEvent{}, false)
	}
	ev, ok := mgr.NudgeEventForSession(prdID, taskID, sessionID)
	return nudgeHistoryNote(ev, ok)
}

// exitSilenceEndReason names the pi-prompt exit-silence detector around the
// reason its tracker already measured.
func exitSilenceEndReason(trackerReason string) string {
	return "exit-silence detector: " + trackerReason
}

// descendantStallEndReason names the pi-prompt descendant-only stall detector
// around the reason its tracker already measured (the measured silence, the
// remaining work descendant and its observed identity).
func descendantStallEndReason(trackerReason string) string {
	return "descendant-only stall detector: " + trackerReason
}

// sseEndReasonLineCap bounds the matched line quoted in an end reason so
// the journal stays readable even if the line limit in sse_stall.go
// changes.
const sseEndReasonLineCap = 120

// sseEndReasonLine quotes the matched line for an end reason, truncated to
// sseEndReasonLineCap runes with an ellipsis when cut.
func sseEndReasonLine(line string) string {
	r := []rune(line)
	if len(r) <= sseEndReasonLineCap {
		return line
	}
	return string(r[:sseEndReasonLineCap]) + "…"
}

// sseStallEndReason names the SSE stall scan of the verifier wait loop,
// recording the matched line, backend and scan source.
func sseStallEndReason(m sseStallMatch) string {
	return fmt.Sprintf("SSE stall detector: backend %s, source %s, matched line %q (pattern %q)",
		m.Backend, m.Source, sseEndReasonLine(m.Line), m.Pattern)
}

// sseWatchdogEndReason names the SSE stall watchdog, which probes sessions
// running longer than its five-minute floor, recording the matched line,
// backend and scan source.
func sseWatchdogEndReason(m sseStallMatch, state session.State, age time.Duration) string {
	return fmt.Sprintf("SSE stall watchdog: backend %s, source %s, matched line %q (pattern %q); session %s running for %s, probe floor 5m0s",
		m.Backend, m.Source, sseEndReasonLine(m.Line), m.Pattern, state, endSecs(age))
}

// activityStallEndReason names the no-activity stall, around the evidence the
// activity check already recorded.
func activityStallEndReason(evidence string) string {
	return "activity stall detector: " + evidence
}

// doneCheckpointEndReason names the checkpoint override that accepts an attempt
// in place of a stall kill. It is recorded when the session is ended for it.
func doneCheckpointEndReason() string {
	return "done-checkpoint verifier: CHECKPOINT.md reports all steps done and the verification script passed; attempt accepted in place of a stall kill"
}

// postWaitEndReason names the cleanup that ends a worker the verifier wait left
// running.
func postWaitEndReason(state session.State) string {
	return fmt.Sprintf("verifier wait ended with the session %s; the worker is ended so it does not outlive its verification", state)
}

// killOnExitEndReason names the end of a finished session's pane when the
// kill_sessions_on_exit setting is on.
func killOnExitEndReason(state session.State) string {
	return fmt.Sprintf("kill_sessions_on_exit: session already %s; pane closed after the 2s output flush", state)
}

// shutdownEndReason names the end of every live session when the daemon is
// shutting down on a signal.
func shutdownEndReason(sig os.Signal) string {
	return fmt.Sprintf("daemon shutdown on signal %v; kill_sessions_on_exit is set", sig)
}

// exitHookRestartEndReason names the kill of a zombie session before an exit
// hook relaunches it with the same task.
func exitHookRestartEndReason(hookID string) string {
	return fmt.Sprintf("exit hook %s restart: zombie session killed before relaunch with the same task", hookID)
}

// cliStopAllEndReason names the stop-all CLI path. It runs outside the daemon,
// so it has no manager to record the end on: the reason is printed.
func cliStopAllEndReason(tmuxName string, state session.State) string {
	return fmt.Sprintf("stop-all CLI: tmux kill-session on %s, state was %s", tmuxName, state)
}

// cliKillEndReason names the single-session CLI kill, which also runs outside
// the daemon and prints its reason.
func cliKillEndReason(id, tmuxName string) string {
	return fmt.Sprintf("kill CLI: session %s, tmux kill-session on %s", id, tmuxName)
}

// sessionEndFromAutonomous adapts a session ender to the killer the autonomous
// package calls, passing its caller and reason through unchanged.
func sessionEndFromAutonomous(ender sessionEnder) func(sessionID, caller, reason string) error {
	return func(sessionID, caller, reason string) error {
		return ender.EndSession(sessionID, session.EndReason{Caller: caller, Reason: reason})
	}
}

// endVerifierSession ends a verifier session for the reconcile host and forwards
// the reason the reconcile gives.
func endVerifierSession(ender sessionEnder, sessionID, reason string) error {
	return ender.EndSession(sessionID, session.EndReason{Caller: endCallerReconcileHost, Reason: reason})
}
