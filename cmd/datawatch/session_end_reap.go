package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	auditpkg "github.com/dmz006/datawatch/internal/audit"
	autonomouspkg "github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/session"
)

// sessionEndReapRecorder records a reap on the task bound to a session. It is
// set once the Automata manager exists; nil means no task is recorded.
var sessionEndReapRecorder atomic.Pointer[func(sessionID string, count int, commands []string) bool]

// sessionEndReapEvidence builds the proof that a process belongs to sess: the
// session id in the process environment (every process the daemon starts for a
// session carries it) and the folders made for this one session alone. A
// shared project folder is never evidence: other sessions and the operator's
// own scripts live there too. peers lists the daemon's sessions (nil when none
// are known): a linked worktree another live session also works in counts as
// shared, and a process naming another live session is never matched.
func sessionEndReapEvidence(sess *session.Session, peers func() []*session.Session) autonomouspkg.OrphanEvidence {
	ev := autonomouspkg.OrphanEvidence{
		SessionID: sess.FullID,
		AdoptedBy: []int{os.Getppid()},
		Dirs:      sessionOwnedDirs(sess),
	}
	if peers == nil {
		return ev
	}
	var inUse []string
	for _, p := range peers() {
		if otherLiveSession(p, sess) && p.ProjectDir != "" {
			inUse = append(inUse, p.ProjectDir)
		}
	}
	kept := ev.Dirs[:0]
	for _, d := range ev.Dirs {
		if d == sess.TrackingDir || !autonomouspkg.DirInUse(d, inUse) {
			kept = append(kept, d)
		}
	}
	ev.Dirs = kept
	ev.OtherLiveSession = func(id string) bool {
		for _, p := range peers() { // read again: a session may have started since
			if otherLiveSession(p, sess) && (p.FullID == id || p.ID == id) {
				return true
			}
		}
		return false
	}
	return ev
}

// otherLiveSession reports whether p is a session other than sess that has not
// finished.
func otherLiveSession(p, sess *session.Session) bool {
	return p != nil && p.FullID != sess.FullID && !p.State.IsTerminal()
}

// isLinkedWorktree reports whether dir is a linked git worktree: its .git is a
// file pointing at the repository, not the repository itself.
func isLinkedWorktree(dir string) bool {
	if dir == "" {
		return false
	}
	fi, err := os.Lstat(filepath.Join(dir, ".git"))
	return err == nil && fi.Mode().IsRegular()
}

// sessionOwnedDirs returns the folders that exist for sess alone: its
// tracking folder, and its project folder only when that is a linked git
// worktree (a .git file, made per session or task). A plain folder or a
// repository checkout is shared, so it is not listed.
func sessionOwnedDirs(sess *session.Session) []string {
	var dirs []string
	if isLinkedWorktree(sess.ProjectDir) {
		dirs = append(dirs, sess.ProjectDir)
	}
	if sess.TrackingDir != "" {
		dirs = append(dirs, sess.TrackingDir)
	}
	return dirs
}

// reapSessionEnd ends every process proven to belong to sess, records one
// decision on its task (count and short commands) and writes one audit entry.
// Only the session's own processes are ended (see sessionEndReapEvidence); a
// process that merely runs or sits in the shared project folder survives.
// Nothing is looked up by name.
func reapSessionEnd(sess *session.Session, reaper *autonomouspkg.OrphanReaper,
	record func(sessionID string, count int, commands []string) bool,
	audit func(auditpkg.Entry), peers ...func() []*session.Session) autonomouspkg.OrphanReapResult {
	if sess == nil || sess.FullID == "" {
		return autonomouspkg.OrphanReapResult{}
	}
	var list func() []*session.Session
	if len(peers) > 0 {
		list = peers[0]
	}
	ev := sessionEndReapEvidence(sess, list)
	res := reaper.EndOrphans(ev)
	if res.Count == 0 && len(res.Survivors) == 0 {
		return res
	}
	cmds := make([]string, 0, len(res.Ended))
	for _, p := range res.Ended {
		cmds = append(cmds, p.Command)
	}
	if record != nil {
		record(sess.FullID, res.Count, cmds)
	}
	if audit != nil {
		audit(auditpkg.Entry{
			Timestamp: time.Now(),
			Actor:     "autonomous",
			Action:    "session_end_reap",
			SessionID: sess.FullID,
			Subject:   fmt.Sprintf("%d ended, %d survived", res.Count, len(res.Survivors)),
			Details: map[string]any{
				"ended":     res.Count,
				"survivors": len(res.Survivors),
				"commands":  strings.Join(cmds, "; "),
			},
		})
	}
	return res
}

// wireSessionEndReap routes every session-ending path to the reaper.
func wireSessionEndReap(mgr *session.Manager) {
	mgr.SetOnSessionReap(func(sess *session.Session) {
		defer func() { _ = recover() }()
		var record func(string, int, []string) bool
		if fn := sessionEndReapRecorder.Load(); fn != nil {
			record = *fn
		}
		var audit func(auditpkg.Entry)
		if fn := auditEntrySink.Load(); fn != nil {
			audit = *fn
		}
		reapSessionEnd(sess, &autonomouspkg.OrphanReaper{}, record, audit, sessionSnapshots(mgr))
	})
}

// sessionSnapshots lists copies of the daemon's sessions, taken under the
// store lock: the reap runs while other goroutines update the records.
func sessionSnapshots(mgr *session.Manager) func() []*session.Session {
	return func() []*session.Session {
		list := mgr.ListSessions()
		out := make([]*session.Session, 0, len(list))
		for _, s := range list {
			cp := mgr.Store().Snapshot(s)
			out = append(out, &cp)
		}
		return out
	}
}
