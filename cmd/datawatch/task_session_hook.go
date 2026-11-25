package main

import (
	autonomouspkg "github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/session"
)

// taskSessionCreatedHook is the session manager's session-created callback: it
// makes the task->worker-session link durable before the task text is
// delivered. Verifier and judge sessions are not linked.
func taskSessionCreatedHook(amgr *autonomouspkg.Manager) func(*session.Session) {
	return func(s *session.Session) {
		amgr.LinkCreatedSession(autonomouspkg.SessionInfo{ID: s.FullID, Name: s.Name, PRDID: s.PRDID, TaskID: s.TaskID})
	}
}
