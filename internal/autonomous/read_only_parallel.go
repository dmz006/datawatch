// Read-only task admission: a task flagged read_only skips the per-tree
// write lock (when autonomous.parallel.read_only_tasks is on), runs with no
// commit step, and fails if it leaves the tree changed.
package autonomous

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"
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
	filtered := dropDaemonOnlyStatusEntries(ctx, root, string(out), m.baseCommitRunner())
	return strings.TrimSpace(filtered), true
}

// dropDaemonOnlyStatusEntries removes from raw porcelain text every entry
// for a tracked file whose only difference from HEAD is the daemon's own
// marked sections (daemonSectionsOnlyChange). Entries are "XY path"; untracked
// ("??"), rename/copy ("->" in the path or R/C status), quoted-path and
// malformed entries are always kept. The baseline and the final status both
// come from readOnlyTreeStatus, so both sides are filtered alike.
func dropDaemonOnlyStatusEntries(ctx context.Context, root, porcelain string, run driftGitRunner) string {
	lines := strings.Split(porcelain, "\n")
	kept := make([]string, 0, len(lines))
	for _, l := range lines {
		if isDaemonOnlyStatusEntry(ctx, root, l, run) {
			continue
		}
		kept = append(kept, l)
	}
	return strings.Join(kept, "\n")
}

// isDaemonOnlyStatusEntry reports whether one porcelain line is a modified
// tracked file that differs from HEAD only by daemon sections.
func isDaemonOnlyStatusEntry(ctx context.Context, root, line string, run driftGitRunner) bool {
	line = strings.TrimRight(line, "\r")
	if len(line) < 4 || line[2] != ' ' {
		return false
	}
	xy, p := line[:2], line[3:]
	if xy == "??" || xy == "!!" || strings.ContainsAny(xy, "RC") || strings.Contains(p, " -> ") {
		return false
	}
	if strings.HasPrefix(p, "\"") { // git-quoted path: cannot parse safely, keep
		return false
	}
	return daemonSectionsOnlyChange(ctx, root, p, run)
}

// readOnlyViolation returns an error when the tree gained a status entry
// that was not in baseline after a read-only task finished. Only new
// entries fail a read-only task: entries that disappeared since the
// baseline never count, and the baseline keeps dirt that was already there
// from counting against the task. When the tree cannot be inspected
// (readOnlyTreeStatus ok is false), no violation is reported.
func (m *Manager) readOnlyViolation(ctx context.Context, prd *PRD, t *Task, baseline string) error {
	now, ok := m.readOnlyTreeStatus(ctx, prd, t)
	if !ok {
		return nil
	}
	return readOnlyStatusViolation(now, baseline)
}

// newStatusEntries returns every non-empty line of now that does not appear
// as an exact line of baseline, in order, with trailing whitespace trimmed
// from each returned entry. Lines are compared raw (not trimmed), so a
// status-code change on the same path still counts as a new entry.
func newStatusEntries(now, baseline string) []string {
	seen := map[string]bool{}
	for _, l := range strings.Split(baseline, "\n") {
		seen[l] = true
	}
	var out []string
	for _, l := range strings.Split(now, "\n") {
		if l == "" || seen[l] {
			continue
		}
		out = append(out, strings.TrimRight(l, " \t\r"))
	}
	return out
}

// readOnlyStatusViolation reports whether a read-only task's tree gained
// any status entry relative to baseline. Only new entries fail a
// read-only task — entries that disappeared since the baseline never
// count — so it returns nil when newStatusEntries finds none.
func readOnlyStatusViolation(now, baseline string) error {
	entries := newStatusEntries(now, baseline)
	if len(entries) == 0 {
		return nil
	}
	return fmt.Errorf("read-only task changed the tree: git status is dirty after it finished (%s)", entries[0])
}

// ReportPathOf is the exported form of reportPathOf: the task's report
// path when the task is read-only, "" otherwise.
func ReportPathOf(t *Task) string { return reportPathOf(t) }

// ValidateReportPath is the exported form of validateReportPath.
func ValidateReportPath(p string) error { return validateReportPath(p) }

const (
	strayReportsDir = "docs/plans/reports/"
	reportsDir      = ".datawatch/reports"
)

// within reports whether p is root or lies under it.
func within(root, p string) bool {
	rel, err := filepath.Rel(root, p)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// readOnlyBaselineUntracked returns the set of paths the baseline status lists
// as untracked ("?? <path>"). Git double-quotes (C-style escapes) a path that
// holds unusual bytes, such as non-ASCII names, so a quoted entry is unquoted
// before it is compared with the raw path that ls-files reports.
func readOnlyBaselineUntracked(baseline string) map[string]bool {
	set := map[string]bool{}
	for _, l := range strings.Split(baseline, "\n") {
		l = strings.TrimRight(l, " \t\r")
		if !strings.HasPrefix(l, "?? ") {
			continue
		}
		p := l[3:]
		if strings.HasPrefix(p, `"`) {
			u, err := strconv.Unquote(p)
			if err != nil {
				continue
			}
			p = u
		}
		set[p] = true
	}
	return set
}

// settleReadOnlyReportFiles runs after a read-only task finished and before
// the violation check. Each new untracked file under docs/plans/reports/ (not
// in baseline) is moved into the folder of the task's report path under
// .datawatch/reports/ (name kept, folder created), or deleted when a file of
// that name already exists there. A Decision names every file moved or
// deleted. Paths that resolve outside the tree root, symlinks and anything
// else are left alone, so the violation check still fails them.
func (m *Manager) settleReadOnlyReportFiles(ctx context.Context, prd *PRD, t *Task, baseline string) {
	_, root, err := ResolveTreeKey(prd, t)
	if err != nil || root == "" {
		return
	}
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return
	}
	// #nosec G204 -- fixed git binary and subcommand; root is the PRD's resolved tree.
	out, err := exec.CommandContext(ctx, "git", "-C", root, "ls-files", "--others", "--exclude-standard", "-z", "--", strayReportsDir).Output()
	if err != nil {
		return
	}
	destDir := reportsDir
	if rp := reportPathOf(t); rp != "" && validateReportPath(rp) == nil {
		destDir = path.Dir(rp)
	}
	inBaseline := readOnlyBaselineUntracked(baseline)
	var moved, deleted []string
	for _, rel := range strings.Split(string(out), "\x00") {
		if rel == "" || !strings.HasPrefix(rel, strayReportsDir) || path.Clean(rel) != rel || inBaseline[rel] {
			continue
		}
		src := filepath.Join(realRoot, filepath.FromSlash(rel))
		fi, err := os.Lstat(src)
		if err != nil || !fi.Mode().IsRegular() {
			continue
		}
		if real, err := filepath.EvalSymlinks(filepath.Dir(src)); err != nil || !within(realRoot, real) {
			continue
		}
		dst := filepath.Join(realRoot, filepath.FromSlash(destDir))
		if err := os.MkdirAll(dst, 0o750); err != nil {
			continue
		}
		if real, err := filepath.EvalSymlinks(dst); err != nil || !within(filepath.Join(realRoot, ".datawatch", "reports"), real) {
			continue
		}
		target := filepath.Join(dst, filepath.Base(src))
		if _, err := os.Lstat(target); err == nil {
			if os.Remove(src) == nil {
				deleted = append(deleted, rel)
			}
			continue
		}
		if os.Rename(src, target) == nil {
			moved = append(moved, rel)
		}
	}
	if len(moved)+len(deleted) == 0 {
		return
	}
	var parts []string
	if len(moved) > 0 {
		parts = append(parts, fmt.Sprintf("moved to %s/: %s", destDir, strings.Join(moved, ", ")))
	}
	if len(deleted) > 0 {
		parts = append(parts, fmt.Sprintf("deleted (same name already in %s/): %s", destDir, strings.Join(deleted, ", ")))
	}
	note := fmt.Sprintf("read-only task %s left report files under %s; %s", t.ID, strayReportsDir, strings.Join(parts, "; "))
	m.store.WithLock(func() {
		prd.Decisions = append(prd.Decisions, Decision{At: time.Now(), Kind: "read_only_report_moved", Actor: "autonomous", Note: note})
	})
	if err := m.store.SavePRD(prd); err != nil {
		log.Printf("[autonomous] settleReadOnlyReportFiles: save: %v", err)
	}
}
