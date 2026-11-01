package router

import (
	"encoding/json"
	"net/url"
	"strings"

	"github.com/dmz006/datawatch/internal/onboardingreport"
)

const onboardingReportUsage = "usage: automation onboarding-report [text|json]"

// OnboardingReportCommVerbs lists the onboarding report comm verbs, for the
// parity test.
func OnboardingReportCommVerbs() []string {
	return []string{"onboarding-report"}
}

// replyOnboardingReport reads the onboarding report over the REST loopback and
// replies with the text the package renders, or the JSON body when the caller
// asks for json. args[0] is the verb; an optional second word is the format.
func (r *Router) replyOnboardingReport(args []string) {
	const label = "onboarding-report"
	format := "text"
	switch len(args) {
	case 1:
	case 2:
		format = strings.ToLower(strings.TrimSpace(args[1]))
		format = strings.TrimPrefix(format, "--format=")
	default:
		r.reply(label+" failed", onboardingReportUsage)
		return
	}
	if format != "text" && format != "json" {
		r.reply(label+" failed", onboardingReportUsage)
		return
	}
	out, err := r.commGet("/api/automation/onboarding-report", url.Values{"format": {"json"}})
	if err != nil {
		r.reply(label+" failed", err.Error())
		return
	}
	if format == "json" {
		r.reply(label, out)
		return
	}
	var rep onboardingreport.Report
	if err := json.Unmarshal([]byte(out), &rep); err != nil {
		r.reply(label+" failed", "decode report: "+err.Error())
		return
	}
	r.reply(label, strings.TrimRight(rep.Text(), "\n"))
}
