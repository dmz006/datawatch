// BL24 wiring — the daemon's pre-commit security scan. The scanner
// (security.go's SecurityScan) already existed as a standalone helper;
// this file is the runtime glue so autonomous.security_scan is no longer
// a dead toggle: the daemon runs it over the task's worktree exactly
// once, immediately before the task's completion commit, and records the
// findings on the task.
//
// The contract is warn-only, end to end:
//   - RunPreCommitSecurityScan never fails — a failed or disabled scan
//     simply yields no findings.
//   - The session-manager pre-commit hook it is installed as
//     (session.Manager.SetPreCommitScan) recovers panics and ignores
//     return values, so the completion commit always proceeds.
//   - Manager.RecordTaskSecurityFindings never changes the task's
//     status — findings must not move a task to any failed or blocked
//     state.
package autonomous

import (
	"fmt"
	"strings"
	"time"
)

// maxSecurityFindingsStored bounds the human-readable finding text kept on
// the task record; the full volume is preserved on SecurityFindingCount.
const maxSecurityFindingsStored = 20

// RunPreCommitSecurityScan is the warn-only entry point the daemon's
// pre-commit hook calls over a task's worktree. It returns nil when the
// scan is disabled or there is no directory to scan; when enabled it
// walks projectDir with SecurityScan (passing no extensions when exts is
// empty so the scanner's built-in defaults, .py/.pyw, apply). A scan
// error is swallowed and reported as "no findings" — a failed scan is
// logged by the caller and never fails the task or its commit.
func RunPreCommitSecurityScan(enabled bool, projectDir string, exts []string) []string {
	if !enabled || projectDir == "" {
		return nil
	}
	findings, err := SecurityScan(projectDir, exts...)
	if err != nil {
		return nil
	}
	return findings
}

// securityFindingsNote builds the decision-log Note for one
// RunPreCommitSecurityScan pass: the task id, the full count (with a cap
// marker when the stored text is truncated), the warn-only fact, and the
// first finding truncated to bound the record.
func securityFindingsNote(taskID string, fullCount int, firstFinding string) string {
	n := fmt.Sprintf("task=%s security_scan findings=%d (warn-only: recorded on the task only; the completion commit proceeded)",
		taskID, fullCount)
	if s := strings.TrimSpace(firstFinding); s != "" {
		if len(s) > 120 {
			s = s[:120] + "…"
		}
		n += "; first: " + s
	}
	return n
}

// RecordTaskSecurityFindings (sibling of RecordTaskFilesTouched /
// RecordTaskSkippedSnapshotPaths) — warn-only recording of a task's
// pre-commit security scan. Sets the task's SecurityFindings to the first
// 20 findings (full human-readable text), increments SecurityFindingCount
// by the FULL len(findings) so multiple passes of the same task
// accumulate, appends exactly one decision-log entry (kind
// security_scan_findings), and saves the PRD. It never touches the
// task's status — findings must not move a task to any failed or
// blocked state — and the daemon's hook treats even an error here as a
// logged, non-fatal no-op.
func (m *Manager) RecordTaskSecurityFindings(prdID, taskID string, findings []string) error {
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return fmt.Errorf("prd %q not found", prdID)
	}
	stored := findings
	if len(stored) > maxSecurityFindingsStored {
		stored = stored[:maxSecurityFindingsStored]
	}
	fullCount := len(findings)
	first := ""
	if len(findings) > 0 {
		first = findings[0]
	}
	found := false
	m.store.WithLock(func() {
		for si := range prd.Story {
			for ti := range prd.Story[si].Tasks {
				if prd.Story[si].Tasks[ti].ID == taskID {
					// Warn-only: overwrite the stored text, accumulate the
					// counter, set the last-scan time — never the status.
					prd.Story[si].Tasks[ti].SecurityFindings = stored
					prd.Story[si].Tasks[ti].SecurityFindingCount += fullCount
					now := time.Now()
					prd.Story[si].Tasks[ti].UpdatedAt = now
					found = true
					break
				}
			}
			if found {
				break
			}
		}
	})
	if !found {
		return fmt.Errorf("task %q not found in prd %q", taskID, prdID)
	}
	m.store.WithLock(func() {
		prd.UpdatedAt = time.Now()
		prd.Decisions = append(prd.Decisions, Decision{
			At:    time.Now(),
			Kind:  "security_scan_findings",
			Actor: "autonomous",
			Note:  securityFindingsNote(taskID, fullCount, first),
		})
	})
	return m.store.SavePRD(prd)
}
