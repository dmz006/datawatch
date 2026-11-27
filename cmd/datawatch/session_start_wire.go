package main

import (
	"fmt"
	"time"

	alertspkg "github.com/dmz006/datawatch/internal/alerts"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/session"
)

// hookDaemonURL is where a session's hook script posts its events. With TLS on
// its own port the main port only redirects, which the script does not follow.
func hookDaemonURL(cfg *config.Config) string {
	scheme, port := "http", cfg.Server.Port
	if cfg.Server.TLSEnabled {
		scheme = "https"
		if cfg.Server.TLSPort > 0 {
			port = cfg.Server.TLSPort
		}
	}
	return fmt.Sprintf("%s://localhost:%d", scheme, port)
}

// pollStartupDialog answers the startup dialogs of a new session that the log
// based responders miss: it calls respond every interval, for at most limit,
// until respond reports that it is done or over says the session is past
// start-up. It takes the gate those responders take, so a dialog is never
// answered by two of them at once.
func pollStartupDialog(sessID string, respond func(string) bool, interval, limit time.Duration, over ...func(string) bool) {
	deadline := time.Now().Add(limit)
	for time.Now().Before(deadline) {
		time.Sleep(interval)
		for _, done := range over {
			if done(sessID) {
				return
			}
		}
		if _, busy := claudeStartupAccepted.LoadOrStore(sessID, struct{}{}); busy {
			continue
		}
		done := respond(sessID)
		claudeStartupAccepted.Delete(sessID)
		if done {
			return
		}
	}
}

const (
	// startPollAnswers is how many answers the start poller gives one screen:
	// the first and one retry. A terminal that acts on keys late would pass
	// every further one on to whatever screen comes next.
	startPollAnswers = 2
	// startPollEndGrace is how long a dialog still up at the end of the window
	// is given before the warning: the 5 s before keys count as lost and the
	// 2 s a dialog must stand. An answer sent just before the end has that long
	// to take effect.
	startPollEndGrace = 7 * time.Second
	// startPollEndTime is how long after its window the poll loop may run for
	// the final check: the grace, and room for a look begun just before the
	// window ended.
	startPollEndTime = startPollEndGrace + 10*time.Second

	startupStuckNotice = "start-up dialog still up after 2 answers; answer it yourself"
	startupLateNotice  = "start-up dialog still up when the start-up check ended; answer it yourself"
)

// startPoller is the start poller's state for one session start.
type startPoller struct {
	m        *session.Manager
	answered bool
	// tries counts the poller's attempts on each screen.
	tries  map[string]int
	warned bool
	// checkAt is when the final check ends; zero until it has begun.
	checkAt time.Time
}

func newStartPoller(m *session.Manager) *startPoller {
	return &startPoller{m: m, tries: map[string]int{}}
}

// look is the poller's look at the session's screen. It never reports the
// poller done: an accept dialog still up or back after its answer, or the next
// one, is answered under the usual rules (not again within 5 s, and only once
// it has settled), until over or the time limit. One screen gets
// startPollAnswers attempts; still up after them, it is sent nothing more and
// the operator is warned.
//
// After its first answer it only accepts. A decline is left to the other
// responders, and the screen is first looked at without the settle step, which
// forgets declines that a reading does not show: watching on must not let an
// Esc be sent twice.
func (p *startPoller) look(sessID string) bool {
	if p.answered {
		if a, up := p.m.LiveStartupDialog(sessID); !up || a.Retry {
			return false
		}
	}
	a, ok := p.m.SettledStartupDialog(sessID)
	if !ok || (p.answered && a.Retry) {
		return false
	}
	if p.tries[a.Screen] >= startPollAnswers {
		// The settle step takes time: the session may have moved on.
		if !p.m.StartupOver(sessID) {
			p.warn(sessID, startupStuckNotice)
		}
		return false
	}
	sent, tried := tryStartupAnswer(p.m, sessID, a)
	if tried {
		p.tries[a.Screen]++
	}
	if sent {
		p.answered = true
	}
	return false
}

// warn raises the one warning a session start gets.
func (p *startPoller) warn(sessID, title string) {
	if p.warned {
		return
	}
	p.warned = true
	p.m.ReportStartupDialogLeft(sessID, title)
}

// dialogLeft returns the start-up dialog on the live screen, if it is one
// datawatch knows. An unrecognised question is not.
func (p *startPoller) dialogLeft(sessID string) (session.StartupDialogAction, bool) {
	a, up := p.m.LiveStartupDialog(sessID)
	return a, up && a.Kind != session.StartupDialogUnknownConfirm
}

// finalCheck is made at each tick once the window has ended. A start-up dialog
// up then, and still one after the grace time, is left for a person: the
// warning says so. It types nothing, so it needs no turn at the session, which
// another responder may keep for 20 s. It reports when it is done. The poll loop
// ends it with no warning once the session is past start-up.
func (p *startPoller) finalCheck(sessID string) (done bool) {
	if p.warned {
		return true
	}
	a, up := p.dialogLeft(sessID)
	if p.checkAt.IsZero() {
		p.checkAt = time.Now().Add(startPollEndGrace)
		return !up
	}
	if time.Now().Before(p.checkAt) {
		return false
	}
	if up {
		title := startupLateNotice
		if p.tries[a.Screen] >= startPollAnswers {
			title = startupStuckNotice
		}
		p.warn(sessID, title)
	}
	return true
}

// startPollResponder is the look of a new start poller.
func startPollResponder(m *session.Manager) func(string) bool {
	return newStartPoller(m).look
}

// startWindowPoller is the start poller of one session start. respond watches
// and answers for window and types nothing after it. atEnd is the final check:
// give it to the poll loop as its last stop, which is asked at every tick even
// while another responder has the session, and give the loop window plus
// startPollEndTime.
func startWindowPoller(m *session.Manager, window time.Duration) (respond, atEnd func(string) bool) {
	p, end := newStartPoller(m), time.Now().Add(window)
	respond = func(sessID string) bool {
		return time.Now().Before(end) && p.look(sessID)
	}
	atEnd = func(sessID string) bool {
		return !time.Now().Before(end) && p.finalCheck(sessID)
	}
	return respond, atEnd
}

// reportSessionNotices sends the manager's session notices as system alerts,
// which reach the alert list, the web app and the chat channels. It also sends
// the one notice for the sessions found dead at start, so call it only once
// alerts can be delivered.
func reportSessionNotices(mgr *session.Manager) {
	mgr.SetSessionNoticeHook(func(title, body string) {
		alertspkg.EmitSystem(alertspkg.LevelWarn, title, body)
	})
	mgr.ReportBootEnded()
}
