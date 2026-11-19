package autonomous

// The plain-language reading of the remote branch reaper surface, for the
// MCP tools, the CLI and the comm channel: parse the daemon's JSON and
// render it in words. The local branch reaper's copy is in reap_text.go;
// this one is for the branches that live on origin.

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ParseRemoteReapReport parses the run response (POST
// /api/autonomous/remote-reap/run).
func ParseRemoteReapReport(body []byte) (RemoteReapReport, error) {
	var rep RemoteReapReport
	if err := json.Unmarshal(body, &rep); err != nil {
		return RemoteReapReport{}, fmt.Errorf("could not read the remote reap run answer: %w", err)
	}
	return rep, nil
}

// ParseRemoteReapSurfaceReport parses the GET
// /api/autonomous/remote-reap body.
func ParseRemoteReapSurfaceReport(body []byte) (RemoteReapSurfaceReport, error) {
	var rep RemoteReapSurfaceReport
	if err := json.Unmarshal(body, &rep); err != nil {
		return RemoteReapSurfaceReport{}, fmt.Errorf("could not read the remote reap report: %w", err)
	}
	return rep, nil
}

// ParseRemoteReapRestoreResult parses the POST
// /api/autonomous/remote-reap/restore body.
func ParseRemoteReapRestoreResult(body []byte) (RemoteReapRestoreResult, error) {
	var res RemoteReapRestoreResult
	if err := json.Unmarshal(body, &res); err != nil {
		return RemoteReapRestoreResult{}, fmt.Errorf("could not read the remote reap restore answer: %w", err)
	}
	return res, nil
}

// RenderRemoteReapReport renders the GET surface body: the branches on
// origin that would be reaped or held, the backup refs that exist, and the
// retention window.
func RenderRemoteReapReport(r RemoteReapSurfaceReport) string {
	var b strings.Builder
	b.WriteString("Remote branch reaper preview (the report deletes nothing yet)\n\n")
	if r.ReportOnly {
		b.WriteString("autonomous.remote_reap_enabled is off or unset, so a pass would report only.\n\n")
	}
	b.WriteString(renderRemoteReapEntries(r.RemoteReapReport))
	b.WriteString("\n")
	fmt.Fprintf(&b, "Backup refs (retention %d whole days):\n", r.RetentionDays)
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

// RenderRemoteReapRun renders the POST run response: the pass that just
// happened.
func RenderRemoteReapRun(rep RemoteReapReport) string {
	var b strings.Builder
	if rep.ReportOnly {
		b.WriteString("Remote reap pass done (report only: autonomous.remote_reap_enabled is off or unset, so nothing on origin was touched)\n\n")
	} else {
		b.WriteString("Remote reap pass done\n\n")
	}
	b.WriteString(renderRemoteReapEntries(rep))
	return strings.TrimRight(b.String(), "\n")
}

// RenderRemoteReapRestore renders the restore response: the branch that
// came back and where its tip was kept.
func RenderRemoteReapRestore(r RemoteReapRestoreResult) string {
	var b strings.Builder
	b.WriteString("Branch restored from its newest remote reap backup ref\n")
	b.WriteString("  branch: " + r.Branch + "\n")
	if r.BackupRef != "" {
		b.WriteString("  backup ref: " + r.BackupRef + "\n")
	}
	if r.SHA != "" {
		b.WriteString("  commit: " + r.SHA + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// renderRemoteReapEntries renders the two lists of one remote reap report,
// in the wording the report itself carries ("would be reaped" for a
// report-only pass, "reaped" for an enabled one).
func renderRemoteReapEntries(rep RemoteReapReport) string {
	var b strings.Builder
	if len(rep.Reaped) == 0 && len(rep.Held) == 0 {
		b.WriteString("No stored Automaton work branch on origin, or none needed attention.\n")
	}
	if len(rep.Reaped) > 0 {
		if rep.ReportOnly {
			b.WriteString("Would be reaped (merged into the default branch and older than the retention window):\n")
		} else {
			b.WriteString("Reaped (deleted from origin after their tip was backed up):\n")
		}
		for _, e := range rep.Reaped {
			line := "  - " + e.Branch
			if e.Reason != "" {
				line += " — " + e.Reason
			}
			if e.Tip != "" {
				line += " (tip " + e.Tip
				if e.AgeDays > 0 {
					line += fmt.Sprintf(", %d whole days old", e.AgeDays)
				}
				line += ")"
			}
			if e.BackupRef != "" {
				line += " [backup " + e.BackupRef + "]"
			}
			b.WriteString(line + "\n")
		}
		b.WriteString("\n")
	}
	if len(rep.Held) > 0 {
		b.WriteString("Held (left on origin):\n")
		for _, e := range rep.Held {
			line := "  - " + e.Branch
			if e.Reason != "" {
				line += " — " + e.Reason
			}
			if e.AgeDays > 0 {
				line += fmt.Sprintf(" (tip %d whole days old)", e.AgeDays)
			}
			b.WriteString(line + "\n")
		}
	}
	return b.String()
}
