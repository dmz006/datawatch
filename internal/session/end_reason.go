// Session end attribution.
//
// Every path that ends a session, or closes its tmux pane, records who did it
// and why. The record goes to the daemon journal and to the session's
// timeline.md, and nowhere else: no session field, config key or endpoint
// carries it.
//
// A terminal state is sticky. Once a session is complete, failed or killed,
// a later end keeps that state and is recorded in the journal only, so the
// timeline shows one "session ended" line per session.

package session

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// EndReason says who ended a session and why. Caller names the function that
// ended it as "function@file:line", or a fixed name such as "channel-event".
// Reason is free text for a person reading the timeline.
type EndReason struct {
	Caller string
	Reason string
}

// noReasonGiven is recorded for an end that comes in through Kill, which
// predates end reasons. Such an end is still attributed by its call site.
const noReasonGiven = "no reason given by caller"

// callerAt returns "function@file:line" for the frame skip levels above the
// function that calls callerAt. Zero means the calling function itself.
func callerAt(skip int) string {
	pc, file, line, ok := runtime.Caller(skip + 1)
	if !ok {
		return "unknown"
	}
	name := "unknown"
	if fn := runtime.FuncForPC(pc); fn != nil {
		name = fn.Name()
		if i := strings.LastIndex(name, "/"); i >= 0 {
			name = name[i+1:]
		}
	}
	return fmt.Sprintf("%s@%s:%d", name, filepath.Base(file), line)
}

// Kill ends a session with no reason. It is kept only as a compatibility
// wrapper: new code must call EndSession with an EndReason. Code outside this
// package that calls Kill, KillTmuxSession or KillAll without an end-reason-ok
// marker fails TestSessionEndGuardNoBareKillCallers. The recorded caller is the
// immediate caller of Kill.
func (m *Manager) Kill(fullID string) error {
	return m.EndSession(fullID, EndReason{Caller: callerAt(1), Reason: noReasonGiven})
}

// EndSession ends a session by full ID or short ID, and records who ended it
// and why. It cancels the monitor, closes the tmux pane and, when the session
// asks for it, ends its children. The state becomes killed unless the session
// is already terminal.
func (m *Manager) EndSession(fullID string, why EndReason) error {
	sess, ok := m.store.Get(fullID)
	if !ok {
		// Accept short 4-char hex IDs (e.g. from Task.SessionID).
		if sess, ok = m.store.GetByShortID(fullID); ok {
			fullID = sess.FullID
		} else {
			return fmt.Errorf("session %s not found", fullID)
		}
	}

	// Cancel the monitor goroutine and clean up encrypting FIFO
	m.mu.Lock()
	if cancel, ok := m.monitors[fullID]; ok {
		cancel()
		delete(m.monitors, fullID)
	}
	if fifo, ok := m.encFIFOs[fullID]; ok {
		_ = fifo.Close()
		delete(m.encFIFOs, fullID)
	}
	if sp, ok := m.streamPipes[fullID]; ok {
		sp.Close()
		delete(m.streamPipes, fullID)
	}
	tracker := m.trackers[fullID]
	m.mu.Unlock()

	// Cancel any pending scheduled commands (e.g. rate-limit auto-resume)
	if m.schedStore != nil {
		if n := m.schedStore.CancelBySession(fullID); n > 0 {
			fmt.Printf("[session] cancelled %d scheduled command(s) for killed session %s\n", n, sess.ID)
		}
	}

	// Kill the tmux session — but ONLY if there is one. Virtual
	// sessions (council-virtual, etc.) have no tmux pane; calling
	// `tmux kill-session -t ""` without a target munches arbitrary
	// tmux sessions, which is how stopping one virtual council
	// persona killed a bunch of operator-attached real sessions
	// (operator-flagged 2026-05-09 as "stopped 4169ux and it killed
	// all sessions"). killPane skips the tmux call when there's no pane.
	m.killPane(fullID, m.store.Snapshot(sess).TmuxSession, why.Reason)

	m.releaseCommitGuard(fullID, m.store.Snapshot(sess).ProjectDir)

	var oldState, newState State
	m.store.Mutate(func() {
		oldState = sess.State
		if !oldState.IsTerminal() {
			sess.State = StateKilled
			sess.UpdatedAt = time.Now()
		}
		newState = sess.State
	})
	if err := m.store.Save(sess); err != nil {
		return fmt.Errorf("save session: %w", err)
	}

	m.recordEnd(sess, tracker, oldState, newState, why)
	if oldState != newState {
		if m.onStateChange != nil {
			m.onStateChange(sess, oldState)
		}
		m.emitSessionEnd(sess)
		m.triggerSummarize(sess.FullID, 200)
	}

	// Deep recursive cascade: kill all descendants regardless of their own settings.
	if sess.KillChildrenRecursive {
		var killAll func(parent *Session)
		killAll = func(parent *Session) {
			for _, child := range m.GetChildren(parent.FullID) {
				killAll(child)
				if child.State == StateRunning || child.State == StateWaitingInput || child.State == StateRateLimited {
					if err := m.EndSession(child.FullID, cascadeReason(why, parent)); err != nil {
						fmt.Printf("[warn] recursive kill %s: %v\n", child.ID, err)
					}
				}
			}
		}
		killAll(sess)
	} else if sess.KillChildren {
		// Shallow cascade: direct children only.
		for _, child := range m.GetChildren(sess.FullID) {
			if child.State == StateRunning || child.State == StateWaitingInput || child.State == StateRateLimited {
				if err := m.EndSession(child.FullID, cascadeReason(why, sess)); err != nil {
					fmt.Printf("[warn] cascade kill child %s: %v\n", child.ID, err)
				}
			}
		}
	}

	return nil
}

// cascadeReason is the reason a child carries when its parent's end takes it
// down too. The caller is the parent's caller.
func cascadeReason(parentWhy EndReason, parent *Session) EndReason {
	return EndReason{Caller: parentWhy.Caller, Reason: fmt.Sprintf("parent session %s ended", parent.ID)}
}

// recordEnd is the one writer of an end. It always writes one journal line.
// It writes the timeline "session ended" line only when the state changed, so
// a repeated end of a finished session does not duplicate that line.
func (m *Manager) recordEnd(sess *Session, tracker *Tracker, from, to State, why EndReason) {
	m.journalf("[session] ended %s: %s -> %s by=%s reason=%s\n", sess.ID, from, to, why.Caller, why.Reason)
	if from == to {
		return
	}
	if tracker == nil {
		tracker = m.diskTracker(sess)
	}
	if tracker == nil {
		return
	}
	if err := tracker.RecordCompleteWithReason(to, why); err != nil {
		m.journalf("[warn] tracker.RecordComplete(%s): %v\n", to, err)
	}
}

// diskTracker opens the tracking folder of a session that has no live tracker,
// so its end still reaches timeline.md. It returns nil when the folder is gone.
func (m *Manager) diskTracker(sess *Session) *Tracker {
	if m.dataDir == "" {
		return nil
	}
	if _, err := os.Stat(filepath.Join(m.dataDir, "sessions", sess.FullID)); err != nil {
		return nil
	}
	return ResumeTracker(m.dataDir, sess)
}

// killPane closes a session's tmux pane. It is the only place manager.go closes
// a pane: the session, the pane and the reason are journaled first, so no pane
// is killed anonymously. A session with no pane is skipped.
func (m *Manager) killPane(fullID, tmuxName, why string) {
	if strings.TrimSpace(tmuxName) == "" {
		return
	}
	m.journalf("[session] closing pane %s for %s: %s\n", tmuxName, m.shortID(fullID), why)
	_ = m.tmux.KillSession(tmuxName)
}

// shortID returns a session's short ID, or fullID when the store does not know it.
func (m *Manager) shortID(fullID string) string {
	if sess, ok := m.store.Get(fullID); ok {
		return sess.ID
	}
	return fullID
}

// SetJournal sends the daemon journal lines to w instead of stdout. Callers
// that need to read what the daemon logged for an end or a hook event use it.
func (m *Manager) SetJournal(w io.Writer) { m.journal = w }

// NoteHookBoardEvent journals one Stop or SubagentStop hook event for a
// session. Those events change only the session's status board, never the
// session, so the line says so: the event, whether it was a SubagentStop, the
// session state at the time, and action=none. It is written only in verbose
// mode and does nothing for any other event.
func (m *Manager) NoteHookBoardEvent(fullID, event string) {
	if m == nil || !m.verbose || (event != "Stop" && event != "SubagentStop") {
		return
	}
	id, state := fullID, "unknown"
	sess, ok := m.store.Get(fullID)
	if !ok {
		sess, ok = m.store.GetByShortID(fullID)
	}
	if ok {
		snap := m.store.Snapshot(sess)
		id, state = snap.ID, string(snap.State)
	}
	m.journalf("[session:debug] hook event %s subagent=%t session=%s state=%s action=none (board only)\n",
		event, event == "SubagentStop", id, state)
}

// journalf writes one line to the daemon journal. The journal is stdout unless
// a test has set Manager.journal.
func (m *Manager) journalf(format string, args ...any) {
	var out io.Writer = os.Stdout
	if m.journal != nil {
		out = m.journal
	}
	_, _ = fmt.Fprintf(out, format, args...)
}
