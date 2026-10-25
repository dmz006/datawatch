package main

import (
	"log"
	"time"

	autonomouspkg "github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/session"
)

// workerNudgeWindows returns the claude-code idle-nudge windows from the live
// autonomous config: one nudge after nudgeAfter of idle, a stall after
// stallAfter more. Both the fresh-session wait and the adopted-session wait use
// it, so they nudge on the same schedule.
func workerNudgeWindows(cfg autonomouspkg.Config) (nudgeAfter, stallAfter time.Duration) {
	nudgeAfter = time.Duration(config.NormalizeWorkerIdleNudgeSeconds(cfg.WorkerIdleNudgeSeconds)) * time.Second
	stallAfter = time.Duration(config.NormalizeWorkerIdleNudgeStallSeconds(cfg.WorkerIdleNudgeStallSeconds)) * time.Second
	return nudgeAfter, stallAfter
}

// workerNudgeHooks wires one claude-code worker's idle-nudge decision to the
// daemon through the session manager. The adopted-session wait uses it; the
// fresh-session wait goes through newPostTurnNudge, which builds the same hooks
// with injectable collaborators. recorder may be nil.
func workerNudgeHooks(mgr *session.Manager, recorder *autonomouspkg.Manager, sessionID, prdID, taskID string, idleLimit, stallAfter time.Duration) autonomouspkg.WorkerNudgeHooks {
	return workerNudgeHooksFor(
		managerPostTurnSessions{mgr: mgr},
		postTurnProbes{hasWork: hasLiveChildProcess, logAge: staleLogAge(idleLimit)},
		managerPostTurnRecorder{mgr: recorder}, sessionID, prdID, taskID, stallAfter,
	)
}

// workerNudgeHooksFor builds the hooks of one claude-code worker's idle-nudge
// decision: its observation (state, log quiet time, live child process,
// completion marker), its delivery state, the nudge input, the nudge record on
// the task and the stall kill. Every wait builds its hooks here, so a worker is
// observed, nudged, recorded and stalled the same way whichever wait is watching
// it.
func workerNudgeHooksFor(sessions postTurnSessions, probes postTurnProbes, recorder postTurnRecorder, sessionID, prdID, taskID string, stallAfter time.Duration) autonomouspkg.WorkerNudgeHooks {
	return autonomouspkg.WorkerNudgeHooks{
		Observe: func() autonomouspkg.WorkerNudgeObservation {
			var obs autonomouspkg.WorkerNudgeObservation
			s, ok := sessions.Snapshot(sessionID)
			if !ok {
				return obs
			}
			obs.OutputAge = probes.logAge(s.LogFile)
			obs.State = string(s.State)
			obs.HasLiveChildProcess = probes.hasWork(s.TmuxSession, s.ProjectDir)
			if tail, err := sessions.TailOutput(sessionID, 50); err == nil {
				obs.CompletionSeen = session.ExtractCompletionSummary(tail) != ""
			}
			return obs
		},
		Delivery: func() (bool, string) {
			if s, ok := sessions.Snapshot(sessionID); ok {
				return sessions.TaskDelivered(s.FullID), s.Task
			}
			return false, ""
		},
		SendInput: func(text, source string) error {
			return sessions.SendInput(sessionID, text, source)
		},
		Record: func(at time.Time) (int, error) {
			if err := recorder.RecordTaskNudge(prdID, taskID, sessionID, at); err != nil {
				return 0, err
			}
			return recorder.TaskNudgeCount(prdID, taskID), nil
		},
		Outcome: func(outcome string) {
			if err := recorder.RecordTaskNudgeOutcome(prdID, taskID, sessionID, outcome); err != nil {
				log.Printf("[autonomous] record nudge outcome %s for session %s task %s/%s: %v", outcome, sessionID, prdID, taskID, err)
			}
		},
		End: func(obs autonomouspkg.WorkerNudgeObservation) {
			_ = sessions.EndSession(sessionID, session.EndReason{Caller: endCallerNudgeStall, Reason: nudgeStallEndReason(obs, stallAfter)})
		},
	}
}
