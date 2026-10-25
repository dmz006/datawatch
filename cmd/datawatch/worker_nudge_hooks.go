package main

import (
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
// daemon: its observation (state, log quiet time, live child process, completion
// marker), its delivery state, the nudge input, the nudge record on the task and
// the stall kill. The fresh-session wait and the adopted-session wait both build
// their hooks here, so a worker is observed, nudged, recorded and stalled the
// same way whichever wait is watching it. recorder may be nil.
func workerNudgeHooks(mgr *session.Manager, recorder *autonomouspkg.Manager, sessionID, prdID, taskID string, idleLimit, stallAfter time.Duration) autonomouspkg.WorkerNudgeHooks {
	return autonomouspkg.WorkerNudgeHooks{
		Observe: func() autonomouspkg.WorkerNudgeObservation {
			var obs autonomouspkg.WorkerNudgeObservation
			s, ok := mgr.GetSession(sessionID)
			if !ok {
				return obs
			}
			_, obs.OutputAge, _ = staleWorkerCheck(s.LogFile, idleLimit, time.Now())
			obs.State = string(s.State)
			obs.HasLiveChildProcess = hasLiveChildProcess(s.TmuxSession, s.ProjectDir)
			if tail, err := mgr.TailOutput(sessionID, 50); err == nil {
				obs.CompletionSeen = session.ExtractCompletionSummary(tail) != ""
			}
			return obs
		},
		Delivery: func() (bool, string) {
			if s, ok := mgr.GetSession(sessionID); ok {
				return mgr.TaskDelivered(s.FullID), s.Task
			}
			return false, ""
		},
		SendInput: func(text, source string) error {
			return mgr.SendInput(sessionID, text, source)
		},
		Record: func(at time.Time) error {
			if recorder == nil {
				return nil
			}
			return recorder.RecordTaskNudge(prdID, taskID, sessionID, at)
		},
		End: func(obs autonomouspkg.WorkerNudgeObservation) {
			_ = mgr.EndSession(sessionID, session.EndReason{Caller: endCallerNudgeStall, Reason: nudgeStallEndReason(obs, stallAfter)})
		},
	}
}
