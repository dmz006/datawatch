package main

import (
	"time"

	"github.com/dmz006/datawatch/internal/session"
)

// finishCouncilSession writes a council persona's answer, or its failure, on
// the virtual session that stands for it. The caller saves the record.
func finishCouncilSession(sess *session.Session, response, errMsg string) {
	from := sess.State
	sess.UpdatedAt = time.Now().UTC()
	reason := "persona answered"
	if errMsg != "" {
		sess.State = session.StateFailed
		sess.LastResponse = errMsg
		reason = "persona failed: " + truncate(errMsg, 200)
	} else {
		sess.State = session.StateComplete
		sess.LastResponse = response
	}
	session.StampEnd(sess, from, session.EndReason{Caller: "council.persona", Reason: reason})
}
