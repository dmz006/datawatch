package autonomous

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
)

// ParseOutcomeCheckBody decodes the body of GET /api/autonomous/outcome-check.
func ParseOutcomeCheckBody(b []byte) (OutcomeCheckReport, error) {
	var rep OutcomeCheckReport
	if err := json.Unmarshal(b, &rep); err != nil {
		return rep, err
	}
	return rep, nil
}

// RenderOutcomeCheck writes the report as plain text for the CLI and the chat
// verb: the mode, what was scanned, the findings by kind, then the intent
// proof counts.
func RenderOutcomeCheck(w io.Writer, rep OutcomeCheckReport) {
	_, _ = fmt.Fprintf(w, "Closed work check: mode %s, %d Automata scanned", rep.Mode, rep.Scanned)
	if !rep.RunAt.IsZero() {
		_, _ = fmt.Fprintf(w, ", at %s", rep.RunAt.Format("2006-01-02 15:04 MST"))
	}
	_, _ = fmt.Fprintln(w)
	if rep.DeploysAfterShip {
		_, _ = fmt.Fprintf(w, "This host deploys after ship (running %s).\n", orDash(rep.RunningVersion))
	}
	if len(rep.Findings) == 0 {
		_, _ = fmt.Fprintln(w, "No findings.")
	}
	kinds := make([]string, 0, len(rep.Counts))
	for k := range rep.Counts {
		kinds = append(kinds, k)
	}
	sort.Strings(kinds)
	for _, k := range kinds {
		_, _ = fmt.Fprintf(w, "%s: %d\n", strings.ReplaceAll(k, "_", " "), rep.Counts[k])
		for _, f := range rep.Findings {
			if f.Kind != k {
				continue
			}
			who := f.Automaton
			if who == "" {
				who = "daemon"
			}
			_, _ = fmt.Fprintf(w, "  %s  %s\n", who, f.Detail)
		}
	}
	if rep.Proof.Checked > 0 {
		_, _ = fmt.Fprintf(w, "Intent proof: %d checked, %d proven, %d partial, %d unproven.\n",
			rep.Proof.Checked, rep.Proof.Proven, rep.Proof.Partial, rep.Proof.Unproven)
	}
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
