// Read-only task admission: a task flagged read_only skips the per-tree
// write lock (when autonomous.parallel.read_only_tasks is on), runs with no
// commit step, and fails if it leaves the tree changed.
package autonomous

import (
	"context"
	"fmt"
	"os/exec"
	"path"
	"strings"
)

// readOnlyTaskSkipsLock reports whether t runs without the tree write lock:
// the task is flagged read_only and the effective setting is on (daemon
// default, or a project/ Automaton tier that sets it).
func (m *Manager) readOnlyTaskSkipsLock(prd *PRD, t *Task) bool {
	if t == nil || !t.ReadOnly {
		return false
	}
	return m.effectiveReadOnlyTasks(prd)
}

// readOnlyWriteScope returns no write scope for a read-only task, so nothing
// can be staged for it.
func readOnlyWriteScope(readOnly bool, scope []string) []string {
	if readOnly {
		return nil
	}
	return scope
}

// reportPathOf returns the task's report path, only when the task is
// read-only; a non-read-only task's ReportPath is ignored.
func reportPathOf(t *Task) string {
	if t == nil || !t.ReadOnly {
		return ""
	}
	return t.ReportPath
}

// validateReportPath checks that p is a legal report path for a
// read-only task: repo-relative (not absolute), no .. segment, already
// clean, and under .datawatch/reports/. Empty is valid (the task
// writes nothing.
func validateReportPath(p string) error {
	if p == "" {
		return nil
	}
	if strings.HasPrefix(p, "/") {
		return fmt.Errorf("report path %q must be repo-relative, not absolute", p)
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == ".." {
			return fmt.Errorf("report path %q must not contain a .. segment", p)
		}
	}
	if path.Clean(p) != p {
		return fmt.Errorf("report path %q must be clean (path.Clean(p) == p)", p)
	}
	if !strings.HasPrefix(p, ".datawatch/reports/") {
		return fmt.Errorf("report path %q must lie under .datawatch/reports/", p)
	}
	return nil
}

// taskHasUsableReportPath reports whether t declares a report path
// that is a usable report location: non-empty and passing
// validateReportPath. It is the proof a read-only task can show on
// failure — a task with neither a verification script nor a usable
// report path is unverifiable (see the read_only_unverifiable rule and
// the readonly_unverifiable structural finding).
func taskHasUsableReportPath(t Task) bool {
	rp := strings.TrimSpace(t.ReportPath)
	if rp == "" {
		return false
	}
	return validateReportPath(rp) == nil
}

// readOnlyTreeStatus returns the porcelain status of the task's tree. ok is
// false when the tree cannot be inspected (no tree, not a repo, git missing),
// in which case no violation can be judged. When the task has a report path,
// that one path is excluded too, so the single allowed write does not
// register as dirty.
func (m *Manager) readOnlyTreeStatus(ctx context.Context, prd *PRD, t *Task) (string, bool) {
	_, root, err := ResolveTreeKey(prd, t)
	if err != nil {
		return "", false
	}
	args := []string{"-C", root, "status", "--porcelain", "--untracked-files=all", "--", ".", ":(exclude).datawatch"}
	// An unvalidated path is never passed to git: if the path fails
	// validateReportPath the exclusion is simply not applied, so a write
	// there still registers as dirty.
	if rp := reportPathOf(t); rp != "" && validateReportPath(rp) == nil {
		args = append(args, ":(exclude)"+rp)
	}
	// #nosec G204 -- fixed "git" binary and fixed subcommand; the root comes
	// from the PRD's resolved tree and any report path was validated by
	// validateReportPath above, so every argument stays inside the root.
	cmd := exec.CommandContext(ctx, "git", args...) // #nosec G204 -- see note above
	out, err := cmd.Output()
	if err != nil {
		return "", false
	}
	return strings.TrimSpace(string(out)), true
}

// readOnlyViolation returns an error when the tree differs from baseline
// after a read-only task finished. The baseline keeps dirt that was already
// there from counting against the task.
func (m *Manager) readOnlyViolation(ctx context.Context, prd *PRD, t *Task, baseline string) error {
	now, ok := m.readOnlyTreeStatus(ctx, prd, t)
	if !ok || now == baseline {
		return nil
	}
	return fmt.Errorf("read-only task changed the tree: git status is dirty after it finished (%s)", firstStatusLine(now, baseline))
}

// firstStatusLine names one entry that is in now but not in baseline.
func firstStatusLine(now, baseline string) string {
	seen := map[string]bool{}
	for _, l := range strings.Split(baseline, "\n") {
		seen[l] = true
	}
	for _, l := range strings.Split(now, "\n") {
		if !seen[l] {
			return strings.TrimSpace(l)
		}
	}
	return "tree changed"
}
