// Shared wording for the operator surface of the task sub-branch reaper:
// the CLI, the MCP tools and the comm channel all render through these, so
// the report reads the same on every surface.
package autonomous

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ParseReapReport parses the shared reaped/waiting split out of a report
// body (the GET surface body or the POST run response).
func ParseReapReport(body []byte) (ReapReport, error) {
	var rep ReapReport
	if err := json.Unmarshal(body, &rep); err != nil {
		return ReapReport{}, fmt.Errorf("parse the branch reap report: %v", err)
	}
	return rep, nil
}

// ParseReapSurfaceReport parses the GET /api/autonomous/branch-reap body.
func ParseReapSurfaceReport(body []byte) (ReapSurfaceReport, error) {
	var rep ReapSurfaceReport
	if err := json.Unmarshal(body, &rep); err != nil {
		return ReapSurfaceReport{}, fmt.Errorf("parse the branch reap report: %v", err)
	}
	return rep, nil
}

// ParseReapRestoreResult parses the restore response body.
func ParseReapRestoreResult(body []byte) (ReapRestoreResult, error) {
	var res ReapRestoreResult
	if err := json.Unmarshal(body, &res); err != nil {
		return ReapRestoreResult{}, fmt.Errorf("parse the branch reap restore: %v", err)
	}
	return res, nil
}

// RenderReapReport renders the GET surface body: the would-be-reaped and
// would-be-held branches, the backup refs that exist and the retention.
func RenderReapReport(r ReapSurfaceReport) string {
	var b strings.Builder
	b.WriteString("Branch reaper preview (the report deletes nothing yet)\n\n")
	b.WriteString(renderReapEntries(r.ReapReport, true))
	b.WriteString("\n")
	switch d := r.BackupDays; {
	case d > 0:
		fmt.Fprintf(&b, "Backup refs (retention %d whole days):\n", d)
	default:
		b.WriteString("Backup refs (retention 30 whole days when unset):\n")
	}
	if len(r.Backups) == 0 {
		b.WriteString("  none yet\n")
	} else {
		for _, bk := range r.Backups {
			line := "  - " + bk.Ref + " -> " + bk.SHA
			if bk.Branch != "" {
				line += "  (branch " + bk.Branch + ")"
			}
			b.WriteString(line + "\n")
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

// RenderReapRun renders the POST run response: the pass that just happened.
func RenderReapRun(rep ReapReport) string {
	var b strings.Builder
	b.WriteString("Branch reap pass done\n\n")
	b.WriteString(renderReapEntries(rep, false))
	return strings.TrimRight(b.String(), "\n")
}

// RenderReapRestore renders the restore response: the branch that came
// back and where its tip was kept.
func RenderReapRestore(r ReapRestoreResult) string {
	var b strings.Builder
	b.WriteString("Branch restored from its newest backup ref\n")
	b.WriteString("  branch: " + r.Branch + "\n")
	if r.BackupRef != "" {
		b.WriteString("  backup ref: " + r.BackupRef + "\n")
	}
	if r.SHA != "" {
		b.WriteString("  commit: " + r.SHA + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// renderReapEntries renders the two lists of one report; would selects the
// preview wording ("would be reaped") from the run wording ("reaped").
func renderReapEntries(rep ReapReport, would bool) string {
	var b strings.Builder
	if len(rep.Reaped) == 0 && len(rep.Waiting) == 0 {
		b.WriteString("No branch qualified and nothing was held.\n")
		return b.String()
	}
	if len(rep.Reaped) > 0 {
		if would {
			b.WriteString("Would be reaped:\n")
		} else {
			b.WriteString("Reaped (each backup ref was written before the delete):\n")
		}
		b.WriteString(reapEntryLines(rep.Reaped))
		b.WriteString("\n")
	}
	if len(rep.Waiting) > 0 {
		b.WriteString("Held (branch kept):\n")
		b.WriteString(reapEntryLines(rep.Waiting))
	}
	return b.String()
}

// reapEntryLines renders one line per branch: the branch, the Automaton it
// belongs to, the plain reason, and the backup ref when one was written.
func reapEntryLines(es []ReapEntry) string {
	var b strings.Builder
	for _, e := range es {
		line := "  - "
		if e.Branch != "" {
			line += e.Branch + "  "
		}
		if e.AutomatonID != "" {
			line += "(Automaton " + e.AutomatonID + ")  "
		}
		if e.Reason != "" {
			line += e.Reason
		}
		if e.BackupRef != "" {
			line += "  [backup " + e.BackupRef + "]"
		}
		b.WriteString(line + "\n")
	}
	return b.String()
}
