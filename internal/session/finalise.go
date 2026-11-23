package session

import (
	"os"
	"time"
)

// goneTailBytes is how much of a session's output log is read to decide its
// final state.
const goneTailBytes = 64 << 10

// FinaliseResult reports one pass of FinaliseGoneSessions.
type FinaliseResult struct {
	// Finalised holds the full IDs of the records finalised in this pass.
	Finalised []string
	// Skipped counts the records left as they were: final, live, foreign, or
	// no longer gone when re-checked just before writing.
	Skipped int
	// Folders counts the finalised records whose project folder still exists
	// afterwards. Folders are never touched by the finaliser.
	Folders int
	// Unparseable counts saved session files (session.json) that could not be
	// read or parsed during a folder pass. They are also counted in Skipped.
	Unparseable int
	// At is the time the pass ran.
	At time.Time
	// TerminalListOK is false when the terminal listing failed, in which case
	// nothing was finalised.
	TerminalListOK bool
}

// FinaliseGoneSessions finalises every record that is still marked running,
// waiting_input, stalled or rate_limited but that nothing drives any more. It
// takes one terminal snapshot; when that listing fails it finalises nothing
// and reports TerminalListOK=false. Each gone record is re-checked against the
// terminal client immediately before it is written, and is written as a copy
// so the record the store holds is never mutated in place. The record, its
// output log and its folder are kept; only its state and end fields change.
func (m *Manager) FinaliseGoneSessions(now time.Time) FinaliseResult {
	res := FinaliseResult{At: now}
	terminals, ok := m.TerminalSnapshot()
	if !ok {
		return res
	}
	res.TerminalListOK = true

	for _, rec := range m.store.List() {
		if !finalisableState(rec.State) || m.LifeOf(rec, terminals) != LifeGone {
			res.Skipped++
			continue
		}
		// Re-check right before writing: a terminal may have appeared since
		// the snapshot was taken.
		if rec.TmuxSession != "" && m.tmux.SessionExists(rec.TmuxSession) {
			res.Skipped++
			continue
		}
		current, found := m.store.Get(rec.FullID)
		if !found || !finalisableState(current.State) {
			res.Skipped++
			continue
		}
		snap := *current
		oldState := snap.State
		state, note := GoneFinalState(ReadOutputTail(snap.LogFile, goneTailBytes))
		ended := now
		snap.State = state
		snap.EndedAt = &ended
		snap.EndNote = note
		snap.UpdatedAt = now
		snap.ClaudeAlive = nil
		if err := m.store.Save(&snap); err != nil {
			res.Skipped++
			continue
		}
		if m.onStateChange != nil {
			m.onStateChange(&snap, oldState)
		}
		m.mu.Lock()
		if cancel, has := m.monitors[snap.FullID]; has {
			cancel()
			delete(m.monitors, snap.FullID)
		}
		m.mu.Unlock()

		res.Finalised = append(res.Finalised, snap.FullID)
		if snap.ProjectDir != "" {
			if info, err := os.Stat(snap.ProjectDir); err == nil && info.IsDir() {
				res.Folders++
			}
		}
	}
	return res
}

// finalisableState reports whether a record in this state is a candidate for
// the gone-session finaliser.
func finalisableState(s State) bool {
	switch s {
	case StateRunning, StateWaitingInput, StateStalled, StateRateLimited:
		return true
	}
	return false
}
