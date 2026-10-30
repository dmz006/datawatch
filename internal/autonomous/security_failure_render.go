// Plain-text rendering of the security failure read, shared by the
// `datawatch autonomous security-failure` command and the
// `autonomous security-failure` comm verb so both print the same words.
// The input is the body of GET /api/autonomous/security-failures; nothing
// here reads or writes anything.

package autonomous

import (
	"fmt"
	"strings"
	"text/tabwriter"
)

// SecurityFailureListEntry is one entry of the security failure list the
// read route serves: the run's identity from its repo CI watch record and
// the stored report.
type SecurityFailureListEntry struct {
	Repo     string                `json:"repo"`
	RefKind  string                `json:"ref_kind"`
	RefName  string                `json:"ref_name"`
	Workflow string                `json:"workflow"`
	RunID    string                `json:"run_id"`
	RunURL   string                `json:"run_url"`
	Status   string                `json:"status"`
	Report   SecurityFailureReport `json:"report"`
}

// SecurityFailureEntryOpen reports whether the entry's run still needs the
// operator: its watch status is neither green nor superseded.
func SecurityFailureEntryOpen(e SecurityFailureListEntry) bool {
	return e.Status != "green" && e.Status != "superseded"
}

// FilterOpenSecurityFailures keeps the open entries, in order.
func FilterOpenSecurityFailures(entries []SecurityFailureListEntry) []SecurityFailureListEntry {
	var out []SecurityFailureListEntry
	for _, e := range entries {
		if SecurityFailureEntryOpen(e) {
			out = append(out, e)
		}
	}
	return out
}

// FindSecurityFailure returns the entry for the run identifier, matching
// the list entry's run ID or the report's own run ID.
func FindSecurityFailure(entries []SecurityFailureListEntry, runID string) (SecurityFailureListEntry, bool) {
	runID = strings.TrimSpace(runID)
	for _, e := range entries {
		if e.RunID == runID || (e.Report.RunID != 0 && fmt.Sprint(e.Report.RunID) == runID) {
			return e, true
		}
	}
	return SecurityFailureListEntry{}, false
}

func securityFailureDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "-"
	}
	return strings.TrimSpace(s)
}

func securityFailureRef(e SecurityFailureListEntry) string {
	if e.RefName != "" && e.RefName != e.RefKind {
		return e.RefKind + "/" + e.RefName
	}
	return securityFailureDash(e.RefKind)
}

// RenderSecurityFailureList renders the entries as the operator table:
// one row per report (repository, ref, workflow, run, status, dependency,
// advisory, recommendation, action). An empty list reads as an explicit
// note.
func RenderSecurityFailureList(entries []SecurityFailureListEntry) string {
	if len(entries) == 0 {
		return "No open security failure reports.\n"
	}
	var b strings.Builder
	tw := tabwriter.NewWriter(&b, 0, 4, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "REPOSITORY\tREF\tWORKFLOW\tRUN\tSTATUS\tDEPENDENCY\tADVISORY\tRECOMMENDATION\tACTION")
	for _, e := range entries {
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			securityFailureDash(e.Repo), securityFailureRef(e), securityFailureDash(e.Workflow),
			securityFailureDash(e.RunID), securityFailureDash(e.Status),
			securityFailureDash(e.Report.Dependency),
			securityFailureDash(strings.Join(e.Report.AdvisoryIDs, ", ")),
			securityFailureDash(e.Report.Recommendation), securityFailureDash(e.Report.Action))
	}
	tw.Flush() //nolint:errcheck
	return b.String()
}

// securityFailureActionLine words the action the report carries: what was
// done, or in shadow mode what would have been done.
func securityFailureActionLine(r SecurityFailureReport) string {
	switch r.Action {
	case actionStopped:
		return "Action taken: stopped the Automaton line"
	case actionWouldStop:
		return "Would have taken (shadow mode): stop the Automaton line"
	case actionReportOnly:
		return "Action taken: none, report only"
	default:
		return "Action taken: " + securityFailureDash(r.Action)
	}
}

// RenderSecurityFailureReport renders the full report of one entry:
// dependency, advisory identifier, publication date or unknown, registry
// entry and dates, recommendation, and the action taken or the
// would-have-taken line from shadow mode.
func RenderSecurityFailureReport(e SecurityFailureListEntry) string {
	r := e.Report
	unknown := func(s string) string {
		if strings.TrimSpace(s) == "" {
			return "unknown"
		}
		return strings.TrimSpace(s)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Security failure report, run %s\n", securityFailureDash(e.RunID))
	fmt.Fprintf(&b, "Repository: %s\n", securityFailureDash(e.Repo))
	fmt.Fprintf(&b, "Ref: %s\n", securityFailureRef(e))
	fmt.Fprintf(&b, "Workflow: %s\n", securityFailureDash(e.Workflow))
	fmt.Fprintf(&b, "Run: %s\n", securityFailureDash(e.RunURL))
	fmt.Fprintf(&b, "Watch status: %s\n", securityFailureDash(e.Status))
	fmt.Fprintf(&b, "Dependency: %s\n", unknown(r.Dependency))
	fmt.Fprintf(&b, "Advisory: %s\n", unknown(strings.Join(r.AdvisoryIDs, ", ")))
	fmt.Fprintf(&b, "Published: %s\n", unknown(r.PublishedAt))
	if r.FixedVersion != "" {
		fmt.Fprintf(&b, "Fixed version: %s\n", r.FixedVersion)
	}
	if r.RegistryFound {
		fmt.Fprintf(&b, "Registry entry: %s (first added %s, added %s, expires %s)\n",
			unknown(r.RegistryID), unknown(r.RegistryFirstAdded), unknown(r.RegistryAdded), unknown(r.RegistryExpires))
	} else if r.RegistryError != "" {
		fmt.Fprintf(&b, "Registry entry: none (registry could not be read: %s)\n", r.RegistryError)
	} else {
		b.WriteString("Registry entry: none\n")
	}
	if r.LookupError != "" {
		fmt.Fprintf(&b, "Advisory lookup: failed (%s)\n", r.LookupError)
	}
	fmt.Fprintf(&b, "Recommendation: %s\n", unknown(r.Recommendation))
	fmt.Fprintf(&b, "Mode: %s\n", unknown(r.Mode))
	b.WriteString(securityFailureActionLine(r) + "\n")
	if r.Note != "" {
		fmt.Fprintf(&b, "Note: %s\n", r.Note)
	}
	return b.String()
}
