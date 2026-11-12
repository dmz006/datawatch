// autonomous security failure reports over the chat channel: the
// security-failure verb. With no argument it lists the open reports in
// the same plain text `datawatch autonomous security-failure` prints;
// with a run identifier it replies with the full report of that run
// (dependency, advisory identifier, publication date or unknown, registry
// entry and dates, recommendation, and the action taken or the
// would-have-taken line from shadow mode). An unknown run gets a
// not-found reply. A read — the verb takes no action of its own: it never
// stops, resumes or reruns anything and writes nothing.

package router

import (
	"encoding/json"
	"strings"

	"github.com/dmz006/datawatch/internal/autonomous"
)

const (
	// securityFailureUsage is the usage line for the security failure
	// comm verb.
	securityFailureUsage = "usage: autonomous security-failure [run-id]"

	securityFailureHeader = "autonomous security-failure"
)

// SecurityFailureCommVerbs lists the security failure comm verbs for the
// parity test.
func SecurityFailureCommVerbs() []string {
	return []string{"security-failure", "security_failure"}
}

// handleSecurityFailureVerb answers the autonomous command's security
// failure verbs.
func (r *Router) handleSecurityFailureVerb(verb string, args []string) {
	switch verb {
	case "security-failure", "security_failure":
		r.replySecurityFailure(args)
	default:
		r.reply(securityFailureHeader+" failed", securityFailureUsage)
	}
}

func (r *Router) replySecurityFailure(args []string) {
	if len(args) > 2 {
		r.reply(securityFailureHeader+" failed", securityFailureUsage)
		return
	}
	out, err := r.commGet("/api/autonomous/security-failures", nil)
	if err != nil {
		r.reply(securityFailureHeader+" failed", err.Error())
		return
	}
	var entries []autonomous.SecurityFailureListEntry
	if err := json.Unmarshal([]byte(out), &entries); err != nil {
		r.reply(securityFailureHeader+" failed", "unparseable read view: "+err.Error())
		return
	}
	if len(args) == 2 {
		runID := strings.TrimSpace(args[1])
		entry, ok := autonomous.FindSecurityFailure(entries, runID)
		if !ok {
			r.reply(securityFailureHeader, "No security failure report found for run "+runID+".")
			return
		}
		r.reply(securityFailureHeader, strings.TrimRight(autonomous.RenderSecurityFailureReport(entry), "\n"))
		return
	}
	r.reply(securityFailureHeader, strings.TrimRight(
		autonomous.RenderSecurityFailureList(autonomous.FilterOpenSecurityFailures(entries)), "\n"))
}
