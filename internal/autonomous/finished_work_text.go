// Plain-text rendering of the finished-work digest and run results, shared by
// the CLI (`datawatch autonomous finished-work`) and the chat verb. Read-only.

package autonomous

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"text/tabwriter"
	"time"
)

// ParseFinishedWorkDigest decodes the body of GET /api/autonomous/finished-work.
func ParseFinishedWorkDigest(body []byte) (*FinishedWorkDigest, error) {
	var d FinishedWorkDigest
	if err := json.Unmarshal(body, &d); err != nil {
		return nil, err
	}
	if d.Waiting == nil {
		d.Waiting = []FinishedWorkFinding{}
	}
	return &d, nil
}

// finishedWorkWaited renders how long a finding has waited as of now.
func finishedWorkWaited(since string, now time.Time) string {
	t, err := time.Parse(time.RFC3339, since)
	if err != nil {
		return "?"
	}
	h := int(now.Sub(t).Hours())
	if h >= 48 {
		return fmt.Sprintf("%dd", h/24)
	}
	return fmt.Sprintf("%dh", h)
}

// RenderFinishedWorkDigest writes the digest: the mode, when the sweep, the
// daily digest and the audit last ran, then one row per Automaton waiting.
func RenderFinishedWorkDigest(w io.Writer, d *FinishedWorkDigest) error {
	now := time.Now()
	if t, err := time.Parse(time.RFC3339, d.GeneratedAt); err == nil {
		now = t
	}
	dash := func(s string) string {
		if s == "" {
			return "never"
		}
		return s
	}
	if _, err := fmt.Fprintf(w, "Finished work (autonomous.finished_work: %s). Last sweep: %s. Last digest: %s. Last audit: %s.\n",
		d.Mode, dash(d.LastSweep), dash(d.LastDigest), dash(d.LastAudit)); err != nil {
		return err
	}
	if len(d.Waiting) == 0 {
		_, err := io.WriteString(w, "Nothing is waiting to land.\n")
		return err
	}
	rows := append([]FinishedWorkFinding(nil), d.Waiting...)
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].Since < rows[j].Since })
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	_, _ = io.WriteString(tw, "AUTOMATON\tTITLE\tCAUSE\tCOMMITS\tWAITED\tOWNER\tQUESTION\n")
	for _, f := range rows {
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%d\t%s\t%s\t%s\n", f.Automaton, f.Title, f.Cause, f.Ahead,
			finishedWorkWaited(f.Since, now), f.Owner, f.QuestionID)
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	for _, f := range rows {
		if f.MergeSelection == "" {
			continue
		}
		if _, err := fmt.Fprintf(w, "%s: %s\n", f.Automaton, f.MergeSelection); err != nil {
			return err
		}
	}
	return nil
}

// RenderFinishedWorkRun writes the outcome of a sweep pass or an audit from
// the raw run response.
func RenderFinishedWorkRun(w io.Writer, run string, body []byte) error {
	switch run {
	case "audit":
		var r FinishedWorkAuditResult
		if err := json.Unmarshal(body, &r); err != nil {
			return err
		}
		_, err := fmt.Fprintf(w, "Audit (%s): read %d Automata, %d finding(s)%s.\n", r.Mode, r.Scanned, len(r.Findings), auditRecordText(r))
		if err != nil {
			return err
		}
		causes := make([]string, 0, len(r.ByCause))
		for c := range r.ByCause {
			causes = append(causes, c)
		}
		sort.Strings(causes)
		for _, c := range causes {
			if _, err := fmt.Fprintf(w, "  %s: %d\n", c, r.ByCause[c]); err != nil {
				return err
			}
		}
		return nil
	default:
		var p FinishedWorkPass
		if err := json.Unmarshal(body, &p); err != nil {
			return err
		}
		capped := ""
		if p.Capped {
			capped = " (stopped at the limit; the next pass resumes after it)"
		}
		_, err := fmt.Fprintf(w, "Sweep (%s): compared %d branch(es)%s, %d waiting, %d younger than a day, %d question(s) raised (%d re-asked)%s.\n",
			p.Mode, p.GitChecks, capped, p.Found, p.Younger, p.Asked, p.Reasked, digestText(p))
		return err
	}
}

func auditRecordText(r FinishedWorkAuditResult) string {
	if r.DecisionRecordID == "" {
		return ""
	}
	return ", written as decision record " + r.DecisionRecordID
}

func digestText(p FinishedWorkPass) string {
	if p.Digest {
		return "; the daily digest was sent"
	}
	return ""
}
