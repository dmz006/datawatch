// Triage read text: the CLI and the comm channel both render the body of
// GET /api/autonomous/triage through this file, and the triage alert
// raises the same text as its body, so no surface shows a different
// wording.

package autonomous

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// TriageList is the body of the triage read route: the stored triage
// records, newest first (already sorted by the daemon), alongside the
// alert threshold in effect as whole minutes (0 = alert off).
type TriageList struct {
	Records      []*TriageRecord `json:"records"`
	AlertMinutes int             `json:"alert_minutes"`
}

// ParseTriageList reads the body of GET /api/autonomous/triage.
func ParseTriageList(body []byte) (TriageList, error) {
	var tl TriageList
	if err := json.Unmarshal(body, &tl); err != nil {
		return TriageList{}, fmt.Errorf("reading the triage records: %w", err)
	}
	if tl.Records == nil {
		tl.Records = []*TriageRecord{}
	}
	return tl, nil
}

func triageThresholdText(minutes int) string {
	if minutes <= 0 {
		return "off"
	}
	return fmt.Sprintf("%d min", minutes)
}

// RenderTriageText formats the triage read: each record in the order the
// daemon sent it (newest first) and the alert threshold in effect. A
// single record renders the same as the alert for it, so a surface that
// filters to one Automaton shows the alert's text and nothing else.
func RenderTriageText(tl TriageList) string {
	var b strings.Builder
	if len(tl.Records) == 0 {
		fmt.Fprintf(&b, "No failed or blocked Automata to triage. (Triage alert threshold: %s)\n", triageThresholdText(tl.AlertMinutes))
		return b.String()
	}
	fmt.Fprintf(&b, "Triage: %d Automat%s waiting for the operator (alert threshold: %s), newest first:\n\n",
		len(tl.Records), pluralN(len(tl.Records)), triageThresholdText(tl.AlertMinutes))
	for i, rec := range tl.Records {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString(RenderTriageRecord(rec))
	}
	b.WriteString("\n")
	return b.String()
}

// pluralN completes "... N Automat%s": "on" for one, "a" for more than
// one (Automaton → Automata).
func pluralN(n int) string {
	if n == 1 {
		return "on"
	}
	return "a"
}

// RenderTriageRecord renders one triage record in full: who it is and in
// what state, the failed or blocked task with its error and the block
// reason and session end reason when present, the retry budget standing,
// how long it has been stuck, the alert stamp when set, and any notes
// added after. This is the text the triage alert raises as its body, so
// every surface that shows it says the same thing.
func RenderTriageRecord(rec *TriageRecord) string {
	var b strings.Builder
	name := rec.PRDTitle
	if name == "" {
		name = rec.PRDID
	}
	fmt.Fprintf(&b, "Automaton %q (id %s) is %s and waiting for operator triage.\n", name, rec.PRDID, rec.Status)
	if rec.AutomationID != "" {
		member := ""
		if rec.MemberID != "" {
			member = ", member " + rec.MemberID
		}
		anon := ""
		if rec.AutomationName == "" {
			anon = " (no name, created before automations had one)"
		}
		fmt.Fprintf(&b, "Member of Automation %q (id %s)%s%s.\n", rec.AutomationName, rec.AutomationID, member, anon)
	} else {
		b.WriteString("Standalone Automaton (not part of an Automation).\n")
	}
	b.WriteString(triageDetailsBlock(rec))
	b.WriteString("\n")
	fmt.Fprintf(&b, "Stuck since %s.", rec.FirstSeenAt.Format(time.RFC3339))
	if rec.UpdatedAt.After(rec.FirstSeenAt) {
		fmt.Fprintf(&b, " Last updated %s.", rec.UpdatedAt.Format(time.RFC3339))
	}
	if rec.AlertedAt != nil {
		fmt.Fprintf(&b, " Alerted at %s.", rec.AlertedAt.Format(time.RFC3339))
	}
	b.WriteString("\n")
	if len(rec.Notes) > 0 {
		b.WriteString("Notes:\n")
		for _, n := range rec.Notes {
			b.WriteString("  ")
			if !n.At.IsZero() {
				b.WriteString(n.At.Format("2006-01-02 15:04") + " ")
			}
			if n.Source != "" {
				b.WriteString(n.Source + ": ")
			}
			b.WriteString(strings.ReplaceAll(n.Text, "\n", " ") + "\n")
		}
	}
	if s := rec.Security; s != nil {
		b.WriteString("Security failure:\n")
		if s.RunURL != "" {
			b.WriteString("  Run: " + s.RunURL + "\n")
		}
		if dep := strings.TrimSpace(s.Dependency); dep != "" {
			b.WriteString("  Dependency: " + dep + "\n")
		}
		if len(s.AdvisoryIDs) > 0 {
			b.WriteString("  Advisories: " + strings.Join(s.AdvisoryIDs, ", ") + "\n")
		}
		if s.FixedVersion != "" {
			b.WriteString("  Fixed version: " + s.FixedVersion + "\n")
		}
		if s.Recommendation != "" {
			b.WriteString("  Recommendation: " + s.Recommendation + "\n")
		}
		if s.RegistryFound {
			b.WriteString("  Accepted risk: " + s.RegistryID + "\n")
		}
		if note := strings.TrimSpace(s.Note); note != "" {
			b.WriteString("  Note: " + note + "\n")
		}
		b.WriteString("  Action: " + s.Action + " (mode " + s.Mode + ")\n")
	}
	return b.String()
}
