// question_raise_notifier.go — the messaging leg of question-raise
// notification, on the daemon side. main.go wires this into the
// server's QuestionRaiseNotifier seam; the Server dispatches it
// non-blockingly after each successful POST /api/questions, and it is
// the only leg that needs routers (the WS broadcast and the
// high-priority alert live in internal/server).
//
// Per send, the daemon's notify_exclude list is honoured exactly like
// the lifecycle CommSink: fresh exclude set per raise, so a live config
// change takes effect on the next raise.

package main

import (
	autonomouspkg "github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/router"
)

// questionRaiseNotifier fans the question out to every active messaging
// router it is not excluded on.
type questionRaiseNotifier struct {
	routers []*router.Router
	cfg     *config.Config
}

// NotifyQuestionRaised implements server.QuestionRaiseNotifier.
func (n questionRaiseNotifier) NotifyQuestionRaised(q autonomouspkg.Question) {
	excludeSet := notifyExcludeSet(n.cfg.NotifyExclude)
	for _, r := range n.routers {
		if !isNotifyExcluded(excludeSet, r.BackendName()) {
			r.NotifyQuestionRaised(q)
		}
	}
}
