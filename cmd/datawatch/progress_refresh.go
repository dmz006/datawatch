package main

import (
	"fmt"
	"sync"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/pilog"
	"github.com/dmz006/datawatch/internal/session"
)

// Progress visibility helper. The autonomous waitLoop (main.go) periodically
// re-reads the worker session's own log and hands the resulting snapshot to
// the autonomous manager so Task.Progress / Task.LoopingWarning stay current
// while the task is in progress. This file owns the log-locate + read +
// looping-detection half; the manager owns the store half.

// loopState remembers the worker's most recent action signature and how many
// consecutive refreshes have seen that same signature — the "is this agent
// repeating itself" detector.
type loopState struct {
	sig    string
	streak int
	name   string
}

var (
	progressLoopMu sync.Mutex
	progressLoop   = map[string]loopState{} // keyed by session FullID
)

// refreshTaskProgress reads the worker session's log (only pi-backed
// sessions have one; for any other backend the locate fails and this is a
// no-op), converts it to an autonomous.TaskProgress snapshot, and evaluates
// the looping detector. Returns (nil, "") when there is nothing to
// record — callers treat that as "leave both fields as-is".
func refreshTaskProgress(s *session.Session, workDir string) (*autonomous.TaskProgress, string) {
	if s == nil {
		return nil, ""
	}
	path, err := pilog.LocateLog(piSessionsRoot(), workDir, s.CreatedAt)
	if err != nil {
		return nil, ""
	}
	p, err := pilog.Read(path)
	if err != nil {
		return nil, ""
	}
	snap := autonomous.FromPilog(p)

	// Looping: the single most recent action (tool name + compact args) has
	// been observed unchanged across several consecutive refreshes. Args
	// that evolve (a growing offset, new content) change the signature and
	// reset the streak, so only genuine repetition trips this.
	sig := p.LastToolName + "\x00" + p.LastToolArgs
	warn := ""
	progressLoopMu.Lock()
	if p.LastToolName == "" {
		delete(progressLoop, s.FullID)
	} else {
		e := progressLoop[s.FullID]
		if e.sig == sig {
			e.streak++
		} else {
			e.sig, e.streak = sig, 1
		}
		e.name = p.LastToolName
		progressLoop[s.FullID] = e
		if e.streak >= 3 { // 3 × ~30s refresh ≈ repeatedly spinning for a minute+
			warn = fmt.Sprintf("looping suspected: tool %q call with identical arguments repeated across %d consecutive progress refreshes", e.name, e.streak)
		}
	}
	progressLoopMu.Unlock()
	return snap, warn
}

// forgetLoopState discards the looping-detection state for one session so a
// future run under the same ID does not inherit an old streak.
func forgetLoopState(fullID string) {
	progressLoopMu.Lock()
	delete(progressLoop, fullID)
	progressLoopMu.Unlock()
}
