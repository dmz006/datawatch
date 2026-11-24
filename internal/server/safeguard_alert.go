// Interim safeguard alert.
//
// Three actions lower a queue safeguard: resuming an operator pause, force
// resetting a task's attempt count, and force running Automaton work that
// already shipped. Today the local API needs no token, so a script, a CLI call
// or a model that reaches the API is indistinguishable from the operator. Until
// real identities exist, every such action made by a caller that is neither the
// web app nor a session credential raises one alert and writes one audit
// record, so the operator learns of it the moment it happens.
//
// The web app is told apart by the X-Datawatch-Client: pwa header its fetch
// wrapper sends. The header is not a secret and anyone can send it, so it can
// only suppress the alert, never grant anything; every marked call is still
// audited with the marker, the address and the user agent, so a forged marker
// leaves a record. A session credential never reaches the alert: the daemon
// already refuses or demotes its action, and its identity is in the audit log.
//
// Every covered action calls noteSafeguardAction, the one entry point: the
// pause resume and the attempt-count force reset call it today, and the
// shipped-work force run calls it the same way.

package server

import (
	"fmt"
	"net/http"
	"strings"
	"unicode"

	"github.com/dmz006/datawatch/internal/alerts"
	"github.com/dmz006/datawatch/internal/audit"
)

const (
	// safeguardClientHeader names the request header the web app sends.
	safeguardClientHeader = "X-Datawatch-Client"
	// safeguardClientWebApp is the value the web app sends.
	safeguardClientWebApp = "pwa"
)

// The safeguard actions that raise the alert (audit "action" detail).
const (
	safeguardPauseResume     = "pause_resume"
	safeguardAttemptReset    = "attempt_count_force_reset"
	safeguardShippedForceRun = "shipped_work_force_run"
)

// The caller kinds (audit "caller_kind" detail) and the longest caller-supplied
// text an alert or audit record carries.
const (
	safeguardCallerKindScript = "script"
	safeguardCallerKindWeb    = "web_app"
	safeguardCallerKindPeer   = "federation_peer"
	safeguardCallerKindSess   = "session"
	safeguardFieldMax         = 200
)

// safeguardLabels is the plain wording of each action in the alert text.
var safeguardLabels = map[string]string{
	safeguardPauseResume:     "pause resume (new work admitted again)",
	safeguardAttemptReset:    "attempt count force reset (the task's attempt count was cleared)",
	safeguardShippedForceRun: "force run of work that already shipped",
}

// safeguardEvent describes one covered action.
type safeguardEvent struct {
	// Action is one of the safeguard* action names.
	Action string
	// Target names what the action touched: the pause scope, or the Automaton
	// and task.
	Target string
	// Actor is the actor the daemon resolved for the call.
	Actor string
	// Claimed is the actor label the caller put in its request, "" when none.
	Claimed string
	// Reason is the reason the caller gave, "" when none.
	Reason string
}

// isWebAppCall reports whether the request carries the web app's client marker.
func isWebAppCall(r *http.Request) bool {
	return strings.EqualFold(strings.TrimSpace(r.Header.Get(safeguardClientHeader)), safeguardClientWebApp)
}

// safeguardCleanText makes a caller-supplied string safe to print in an alert
// line: control characters (so a user agent cannot add lines of its own) become
// spaces and the length is bounded.
func safeguardCleanText(v string) string {
	v = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, strings.TrimSpace(v))
	if r := []rune(v); len(r) > safeguardFieldMax {
		v = string(r[:safeguardFieldMax]) + "..."
	}
	return v
}

// safeguardCaller classifies the request: its kind and a plain description of
// the credential (never the credential itself).
func (s *Server) safeguardCaller(r *http.Request) (kind, credential string) {
	ctx := r.Context()
	switch {
	case sessionCapsFromContext(ctx) != nil:
		return safeguardCallerKindSess, "session credential"
	case peerFromContext(ctx) != nil:
		return safeguardCallerKindPeer, "federation peer " + peerFromContext(ctx).Name
	case isWebAppCall(r):
		return safeguardCallerKindWeb, "local API credential"
	}
	return safeguardCallerKindScript, "local API credential or none"
}

// noteSafeguardAction records a covered action that has just taken effect. It
// writes one audit record for every web app and script call, and raises one
// alert unless the caller is the web app (or a session credential, which is
// neither alerted nor recorded here) or audit.safeguard_alert is off. A missing
// alert store or audit log is not an error.
func (s *Server) noteSafeguardAction(r *http.Request, ev safeguardEvent) {
	kind, credential := s.safeguardCaller(r)
	if kind == safeguardCallerKindSess {
		return
	}
	addr := safeguardCleanText(r.RemoteAddr)
	agent := safeguardCleanText(r.UserAgent())
	claimed := safeguardCleanText(ev.Claimed)
	reason := safeguardCleanText(ev.Reason)
	target := safeguardCleanText(ev.Target)
	marked := kind == safeguardCallerKindWeb

	alertOn := s.cfg == nil || s.cfg.Audit.SafeguardAlertOrDefault()
	raise := !marked && alertOn && s.alertStore != nil
	var alertID string
	if raise {
		a := s.alertStore.AddSystem(alerts.LevelWarn, safeguardAlertTitle(ev.Action), safeguardAlertBody(ev, credential, addr, agent, claimed, reason, target))
		if a != nil {
			alertID = a.ID
		}
	}

	details := map[string]any{
		"action":        ev.Action,
		"target":        target,
		"reason":        reason,
		"claimed_actor": claimed,
		"caller_kind":   kind,
		"credential":    credential,
		"remote_addr":   addr,
		"user_agent":    agent,
		"fetch_site":    safeguardCleanText(r.Header.Get("Sec-Fetch-Site")),
		"alert_raised":  raise,
	}
	if v := safeguardCleanText(r.Header.Get(safeguardClientHeader)); v != "" {
		details["client_marker"] = v
	}
	if alertID != "" {
		details["alert_id"] = alertID
	}
	if !raise && !marked {
		if !alertOn {
			details["alert_skipped"] = "audit.safeguard_alert is off"
		} else {
			details["alert_skipped"] = "no alert store"
		}
	}
	actor := ev.Actor
	if actor == "" {
		actor = "operator"
	}
	s.auditAs(actor, audit.ActionSafeguardAction, "safeguard", ev.Action, details)
}

func safeguardAlertTitle(action string) string {
	switch action {
	case safeguardPauseResume:
		return "Pause resumed by a script or CLI caller, not the web app"
	case safeguardAttemptReset:
		return "Attempt count force reset by a script or CLI caller, not the web app"
	case safeguardShippedForceRun:
		return "Shipped work force run by a script or CLI caller, not the web app"
	}
	return "Safeguard lowered by a script or CLI caller, not the web app"
}

func safeguardAlertBody(ev safeguardEvent, credential, addr, agent, claimed, reason, target string) string {
	label := safeguardLabels[ev.Action]
	if label == "" {
		label = ev.Action
	}
	if reason == "" {
		reason = "no reason given"
	}
	if claimed == "" {
		claimed = "none claimed, treated as " + safeguardCleanText(ev.Actor)
	}
	if agent == "" {
		agent = "none"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Action: %s\n", label)
	fmt.Fprintf(&b, "Target: %s\n", target)
	fmt.Fprintf(&b, "Reason: %s\n", reason)
	fmt.Fprintf(&b, "Caller: actor %s, address %s, user agent %s, %s\n", claimed, addr, agent, credential)
	b.WriteString("It did not come from the web app and not from a session credential. If this was not you, look at the audit log entry for this action.")
	return b.String()
}
