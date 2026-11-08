package main

import (
	"fmt"
	"os"
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

// sessionEndReapEvidence builds the proof that a process belongs to sess: its
// worktree and scratch folders and the session id in the process environment.
func sessionEndReapEvidence(sess *session.Session) autonomouspkg.OrphanEvidence {
	ev := autonomouspkg.OrphanEvidence{SessionID: sess.FullID, AdoptedBy: []int{os.Getppid()}}
	if sess.ProjectDir != "" {
		ev.Dirs = append(ev.Dirs, sess.ProjectDir)
	}
	return ev
}

// reapSessionEnd ends every process proven to belong to sess, records one
// decision on its task (count and short commands) and writes one audit entry.
// It does nothing when the session carries no evidence. Nothing is looked up
// by name.
func reapSessionEnd(sess *session.Session, reaper *autonomouspkg.OrphanReaper,
	record func(sessionID string, count int, commands []string) bool,
	audit func(auditpkg.Entry)) autonomouspkg.OrphanReapResult {
	if sess == nil || sess.FullID == "" {
		return autonomouspkg.OrphanReapResult{}
	}
	ev := sessionEndReapEvidence(sess)
	if len(ev.Dirs) == 0 {
		return autonomouspkg.OrphanReapResult{}
	}
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
		reapSessionEnd(sess, &autonomouspkg.OrphanReaper{}, record, audit)
	})
}
