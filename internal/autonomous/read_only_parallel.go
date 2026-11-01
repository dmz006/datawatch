// Read-only task admission: a task flagged read_only skips the per-tree
// write lock (when autonomous.parallel.read_only_tasks is on), runs with no
// commit step, and fails if it leaves the tree changed.
package autonomous

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// readOnlyTaskSkipsLock reports whether t runs without the tree write lock:
// the task is flagged read_only and the effective setting is on.
func (m *Manager) readOnlyTaskSkipsLock(t *Task) bool {
	if t == nil || !t.ReadOnly {
		return false
	}
	return EffectiveReadOnlyTasks(m.Config().Parallel)
}

// readOnlyWriteScope returns no write scope for a read-only task, so nothing
// can be staged for it.
func readOnlyWriteScope(readOnly bool, scope []string) []string {
	if readOnly {
		return nil
	}
	return scope
}

// readOnlyTreeStatus returns the porcelain status of the task's tree. ok is
// false when the tree cannot be inspected (no tree, not a repo, git missing),
// in which case no violation can be judged.
func (m *Manager) readOnlyTreeStatus(ctx context.Context, prd *PRD, t *Task) (string, bool) {
	_, root, err := ResolveTreeKey(prd, t)
	if err != nil {
		return "", false
	}
	cmd := exec.CommandContext(ctx, "git", "-C", root, "status", "--porcelain", "--untracked-files=all", "--", ".", ":(exclude).datawatch") // #nosec G204 -- fixed subcommand; root comes from the PRD's resolved tree
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
