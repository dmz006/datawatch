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

// pollStartupDialog answers a startup dialog of a new session that the log
// based responders miss: it tries every interval, for at most limit, until one
// answer is sent or over says the session is past start-up. It takes the gate
// those responders take, so a dialog is never answered by two of them at once.
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
		answered := respond(sessID)
		claudeStartupAccepted.Delete(sessID)
		if answered {
			return
		}
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
