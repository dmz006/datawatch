// System-alert comm fan-out: alerts that belong to no session (the
// triage alert, the Automation-blocked alert and the other daemon-level
// alerts raised through the Manager's alert sink) reach the operator's
// chat through the same notify pipeline the lifecycle comm sinks use —
// every comm router, ntfy and email, honouring notify_exclude per send.
// The alert body carries to each chat sink verbatim: Router.SendAlert
// does not truncate or reformat it, so the triage alert's body — the
// shared triage record text — arrives in full.

package main

import (
	"github.com/dmz006/datawatch/internal/alerts"
	"github.com/dmz006/datawatch/internal/router"
)

// sendSystemAlertToComm fans one system alert (no session) out over the
// comm channels. hostname stamps the ntfy/email text the same way the
// routers stamp theirs. sendNtfy / sendEmail carry the topic / recipient
// already bound; they may be nil (channel not configured) and are
// skipped when notify_exclude names the channel.
func sendSystemAlertToComm(
	a *alerts.Alert,
	hostname string,
	notifyExclude []string,
	routers []*router.Router,
	sendNtfy func(text string) error,
	sendEmail func(text string) error,
) {
	if a == nil {
		return
	}
	excludeSet := notifyExcludeSet(notifyExclude)
	for _, rt := range routers {
		if !isNotifyExcluded(excludeSet, rt.BackendName()) {
			rt.SendAlert(a)
		}
	}
	text := router.AlertText(hostname, a)
	if sendNtfy != nil && !isNotifyExcluded(excludeSet, "ntfy") {
		_ = sendNtfy(text)
	}
	if sendEmail != nil && !isNotifyExcluded(excludeSet, "email") {
		_ = sendEmail(text)
	}
}
