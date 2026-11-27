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

// startPollResponder is the start poller's look at a new session's screen. It
// never reports the poller done: an accept dialog still up or back after its
// answer, or the next one, is answered under the usual rules (not again within
// 5 s, and only once it has settled), until over or the time limit.
//
// After its first answer it only accepts. A decline is left to the other
// responders, and the screen is first looked at without the settle step, which
// forgets declines that a reading does not show: watching on must not let an
// Esc be sent twice.
func startPollResponder(m *session.Manager) func(string) bool {
	answered := false
	return func(sessID string) bool {
		if answered {
			if a, up := m.LiveStartupDialog(sessID); !up || a.Retry {
				return false
			}
		}
		a, ok := m.SettledStartupDialog(sessID)
		if ok && (!answered || !a.Retry) && answerStartupDialog(m, sessID, a) {
			answered = true
		}
		return false
	}
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
