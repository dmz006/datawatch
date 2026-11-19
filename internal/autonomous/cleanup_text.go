// Cleanup read text: the CLI, the MCP result and the comm channel render
// the body of GET /api/autonomous/cleanup through this file, so no surface
// shows a different wording. The disk alert body is exposed here too, so
// the surfaces and the tests can show the alert text exactly as raised.

package autonomous

import (
	"encoding/json"
	"fmt"
	"sort"
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
		if c.Threshold > 0 {
			line = fmt.Sprintf("%s (threshold %d%%)", line, c.Threshold)
		}
		return line
	}
	if c.Threshold > 0 {
		return fmt.Sprintf("%s (no reading, threshold %d%%)", c.Path, c.Threshold)
	}
	return c.Path + " (no reading)"
}

// humanSize renders a byte count the way a disk alert should say it:
// whole bytes under a KiB, whole units at ten or more, one decimal
// below. The alert prefixes it with "about", so the rounding is part
// of the wording, not an error.
func humanSize(n int64) string {
	if n < 0 {
		n = 0
	}
	const unit = 1024
	f := float64(n)
	name := "B"
	for _, u := range []string{"KiB", "MiB", "GiB", "TiB", "PiB"} {
		if f < unit {
			break
		}
		f /= 1024
		name = u
	}
	switch name {
	case "B":
		return fmt.Sprintf("%d B", n)
	default:
		if f >= 10 {
			return fmt.Sprintf("%d %s", int64(f), name)
		}
		return fmt.Sprintf("%.1f %s", f, name)
	}
}

var cleanupReasonText = map[string]string{
	cleanupSkipCommits:          "unlanded commits",
	cleanupSkipDirty:            "uncommitted work",
	cleanupSkipNotFinished:      "live Automaton",
	cleanupSkipLiveSession:      "live session",
	cleanupSkipLiveLease:        "live lease",
	cleanupSkipRetained:         "inside retention window",
	cleanupSkipPrimary:          "main checkout",
	cleanupSkipNoBase:           "no base branch",
	cleanupSkipHeld:             "held by safety report",
	cleanupSkipOverLimit:        "over removal limit",
	cleanupSkipPermissionDenied: "permission denied",
}

// cleanupSkippedText renders the reason groups in a stable order.
func cleanupSkippedText(m map[string]int) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		label := cleanupReasonText[k]
		if label == "" {
			label = k
		}
		parts = append(parts, fmt.Sprintf("%s %d", label, m[k]))
	}
	return strings.Join(parts, ", ")
}

// worktreeHoldsText renders the holds the worktree pass placed on
// uncommitted work: the reason for each one, the exact files the hold
// is keyed on, and the single action that releases it (matching what
// the pass keyed it on). "" when the pass left no such hold behind.
func worktreeHoldsText(skips []WorktreeCleanupSkipped) string {
	if len(skips) == 0 {
		return ""
	}
	var b strings.Builder
	any := false
	for _, s := range skips {
		if s.ReleaseAction == "" {
			continue
		}
		any = true
		subject := s.Branch
		if s.Worktree != "" {
			subject = s.Worktree
		}
		fmt.Fprintf(&b, "  worktree hold at %s: %s\n", subject, s.Detail)
		if len(s.Files) > 0 {
			names := strings.Join(s.Files, ", ")
			if len(s.Files) > 8 {
				names = strings.Join(s.Files[:8], ", ") + fmt.Sprintf(", … (%d total)", len(s.Files))
			}
			b.WriteString("    held files: " + names + "\n")
		}
		b.WriteString("    released by: " + s.ReleaseAction + "\n")
	}
	if !any {
		return ""
	}
	return b.String()
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
	if len(ps.SkippedByReason) > 0 {
		b.WriteString("\n    skipped by reason: " + cleanupSkippedText(ps.SkippedByReason))
	}
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
	b.WriteString(worktreeHoldsText(st.LastWorktreeSkips))
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
	if len(st.BiggestTempFolders) > 0 {
		b.WriteString("  biggest folders in the temp folder (measured when that disk was over its threshold):\n")
		for _, f := range st.BiggestTempFolders {
			fmt.Fprintf(&b, "    %s — about %s — %s\n", f.Path, humanSize(f.Size), folderLivenessText(f))
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
