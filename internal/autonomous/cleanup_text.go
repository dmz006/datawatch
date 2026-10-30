// Cleanup read text: the CLI, the MCP result and the comm channel render
// the body of GET /api/autonomous/cleanup through this file, so no surface
// shows a different wording. The disk alert body is exposed here too, so
// the surfaces and the tests can show the alert text exactly as raised.

package autonomous

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// ParseCleanupStatus reads the body of GET /api/autonomous/cleanup.
func ParseCleanupStatus(body []byte) (CleanupStatus, error) {
	var st CleanupStatus
	if err := json.Unmarshal(body, &st); err != nil {
		return CleanupStatus{}, fmt.Errorf("reading the Automaton cleanup status: %w", err)
	}
	return st, nil
}

func cleanupModeText(mode string) string {
	if mode == "" {
		return "report"
	}
	return mode
}

func cleanupTimeText(t *time.Time) string {
	if t == nil {
		return "-"
	}
	return t.Format(time.RFC3339)
}

func cleanupDiskText(c *CleanupDiskCheck) string {
	if c == nil {
		return ""
	}
	if p := c.Percent; p != nil {
		line := fmt.Sprintf("%s at %d%%", c.Path, *p)
		if c.Alert {
			line += " (alert)"
		}
		return line
	}
	return c.Path + " (no reading)"
}

func cleanupPassText(label string, ps CleanupPassStatus) string {
	if ps.LastRun == nil {
		return fmt.Sprintf("  %s pass: not run yet", label)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "  %s pass (mode %s): last run %s, removed %d, would-remove %d, skipped %d, failed %d, freed %d bytes",
		label, cleanupModeText(ps.Mode), cleanupTimeText(ps.LastRun),
		ps.Removed, ps.WouldRemove, ps.Skipped, ps.Failed, ps.BytesFreed)
	fmt.Fprintf(&b, "\n    owner vs net: %d/%d removed, %d/%d bytes freed",
		ps.OwnerRemoved, ps.NetRemoved, ps.OwnerBytesFreed, ps.NetBytesFreed)
	if ps.LastError != "" {
		b.WriteString("\n    last error: " + ps.LastError)
	}
	return b.String()
}

// RenderCleanupStatus formats the cleanup status read: both passes
// (the worktrees pass and the daemon temp-file sweep) with their last run,
// counts, bytes freed, last error and the owner-vs-net split; the two disk
// readings; and the rate-limited disk alert. It only describes — the
// cleanup passes do the removing, not this read.
func RenderCleanupStatus(st CleanupStatus) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Automaton cleanup (disk alert threshold: %d%%):\n", st.DiskAlertPercent)
	b.WriteString(cleanupPassText("worktrees", st.Worktrees) + "\n")
	b.WriteString(cleanupPassText("temp", st.Temp) + "\n")
	if st.WorktreeDisk == nil && st.TempDisk == nil {
		b.WriteString("  disks: no readings yet\n")
	} else {
		if st.WorktreeDisk != nil {
			b.WriteString("  worktree disk: " + cleanupDiskText(st.WorktreeDisk) + "\n")
		}
		if st.TempDisk != nil {
			b.WriteString("  temp dir disk: " + cleanupDiskText(st.TempDisk) + "\n")
		}
	}
	if st.DiskAlert {
		fmt.Fprintf(&b, "  disk alert: active (last raised %s)\n", cleanupTimeText(st.LastDiskAlertAt))
	} else {
		b.WriteString("  disk alert: not active\n")
	}
	b.WriteString("This is a status read only: the cleanup passes do the removing, not this read.\n")
	return b.String()
}

// CleanupDiskAlertBody is the user-facing disk alert body, for the
// surfaces and the tests that must show the alert text exactly as raised.
func CleanupDiskAlertBody(st CleanupStatus) string {
	return cleanupDiskAlertBody(st)
}
