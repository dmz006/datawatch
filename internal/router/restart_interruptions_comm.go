package router

import (
	"net/url"
	"strings"

	"github.com/dmz006/datawatch/internal/autonomous"
)

// handleRestartInterruptionsVerb answers `autonomous restart-interruptions`:
// what a daemon restart now would interrupt. A read.
func (r *Router) handleRestartInterruptionsVerb(args []string) {
	const title = "autonomous restart-interruptions"
	if len(args) > 1 {
		r.reply(title+" failed", "usage: autonomous restart-interruptions")
		return
	}
	out, err := r.commGet("/api/autonomous/restart-interruptions", url.Values{})
	if err != nil {
		r.reply(title+" failed", err.Error())
		return
	}
	rep, perr := autonomous.ParseRestartInterruption([]byte(out))
	if perr != nil {
		r.reply(title+" failed", perr.Error())
		return
	}
	r.reply(title, strings.TrimRight(autonomous.RestartInterruptionText(rep), "\n"))
}
