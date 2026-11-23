// Shared live judgement: the one definition of whether a session record is
// still live. Every caller that needs to know asks LifeOf (or feeds its own
// facts to ClassifyLife) instead of re-deriving the answer from one signal.

package session

import "strings"

// Life is the judged liveness of one session record.
type Life string

const (
	// LifeLive means something still drives the session.
	LifeLive Life = "live"
	// LifeGone means the record is not in a final state but nothing drives it.
	LifeGone Life = "gone"
	// LifeForeign means the record belongs to another host. This daemon
	// cannot judge it, so it never finalises or re-labels it.
	LifeForeign Life = "foreign"
	// LifeEnded means the record is already in a final state.
	LifeEnded Life = "ended"
)

// LifeFacts are the observations ClassifyLife judges.
type LifeFacts struct {
	// FinalState is true when the record's state is terminal.
	FinalState bool
	// ForeignHost is true when the record belongs to another host.
	ForeignHost bool
	// TerminalExists is true when the record's terminal session is present.
	TerminalExists bool
	// LaunchInFlight is true while the backend launch is still running.
	LaunchInFlight bool
	// RunnerHeld is true when the daemon holds a runner for the session.
	RunnerHeld bool
	// SubprocessAlive is true when the recorded subprocess pid still runs
	// the session's task.
	SubprocessAlive bool
	// DaemonOwned is true for sessions the daemon's own report path ends.
	DaemonOwned bool
}

// ClassifyLife is the pure judgement over the facts. A final state is ended;
// a record from another host is foreign; otherwise any sign of a driver
// makes the session live, and no sign at all makes it gone.
func ClassifyLife(f LifeFacts) Life {
	switch {
	case f.FinalState:
		return LifeEnded
	case f.ForeignHost:
		return LifeForeign
	case f.TerminalExists, f.LaunchInFlight, f.RunnerHeld, f.SubprocessAlive, f.DaemonOwned:
		return LifeLive
	}
	return LifeGone
}

// TerminalSnapshot lists the live terminal session names with one call to the
// terminal client. ok is false when the listing failed with anything other
// than the plain no-server answer, so a broken listing is never read as every
// terminal being gone.
func (m *Manager) TerminalSnapshot() (map[string]bool, bool) {
	names, err := m.tmux.ListSessions("")
	if err != nil && !isNoTerminalServer(err) {
		return nil, false
	}
	set := make(map[string]bool, len(names))
	for _, n := range names {
		set[n] = true
	}
	return set, true
}

// isNoTerminalServer reports whether err is the plain answer a terminal
// client gives when no server is running, which means no terminal sessions.
func isNoTerminalServer(err error) bool {
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "no server running") ||
		strings.Contains(msg, "no sessions") ||
		strings.Contains(msg, "error connecting to")
}

// LifeOf gathers the facts for one record and judges it. terminals is the
// snapshot from TerminalSnapshot, taken once by the caller for a whole sweep.
func (m *Manager) LifeOf(sess *Session, terminals map[string]bool) Life {
	return ClassifyLife(m.lifeFacts(sess, terminals))
}

func (m *Manager) lifeFacts(sess *Session, terminals map[string]bool) LifeFacts {
	f := LifeFacts{
		FinalState:  sess.State.IsTerminal(),
		ForeignHost: sess.Hostname != m.hostname,
		RunnerHeld:  m.HasRunner(sess.FullID),
		DaemonOwned: strings.HasSuffix(sess.BackendFamily, "-virtual") || sess.AgentID != "",
	}
	if sess.TmuxSession != "" {
		f.TerminalExists = terminals[sess.TmuxSession]
	}
	m.mu.Lock()
	_, f.LaunchInFlight = m.launching[sess.FullID]
	m.mu.Unlock()
	if sess.LogFile != "" {
		if pid, ok := readSubprocessPID(sess); ok {
			f.SubprocessAlive = subprocessAlive(pid, sess.Task)
		}
	}
	return f
}
