// Parallel execution config: the one place the default for
// autonomous.parallel.read_only_tasks lives. Tasks flagged read_only skip
// the tree lock when the key is on.
package autonomous

import (
	"strings"

	"github.com/dmz006/datawatch/internal/config"
)

// defaultReadOnlyTasks is the value read when the key is absent.
const defaultReadOnlyTasks = true

// EffectiveReadOnlyTasks reports whether tasks flagged read_only skip the
// tree lock. An absent key (nil) reads true; an explicit false stays false.
func EffectiveReadOnlyTasks(c config.AutonomousParallelConfig) bool {
	if c.ReadOnlyTasks == nil {
		return defaultReadOnlyTasks
	}
	return *c.ReadOnlyTasks
}

// defaultWorktreePerTask is the value read when worktree_per_task is absent.
const defaultWorktreePerTask = true

// EffectiveWorktreePerTask reports whether each parallel task gets its own
// worktree. An absent key (nil) reads true; an explicit false stays false.
func EffectiveWorktreePerTask(c config.AutonomousParallelConfig) bool {
	if c.WorktreePerTask == nil {
		return defaultWorktreePerTask
	}
	return *c.WorktreePerTask
}

// EffectiveMergeConflictPolicy returns the merge conflict policy, the
// default additive_then_model when unset or unrecognized.
func EffectiveMergeConflictPolicy(c config.AutonomousParallelConfig) string {
	if config.ValidateMergeConflictPolicy(c.MergeConflictPolicy) != nil || c.MergeConflictPolicy == "" {
		return config.MergeConflictPolicyAdditiveThenModel
	}
	return c.MergeConflictPolicy
}

// EffectiveMergeRetrySeconds returns the seconds between timed merge retry
// passes: the default 60 when unset or negative, 0 when explicitly 0 (timer
// off).
func EffectiveMergeRetrySeconds(c config.AutonomousParallelConfig) int {
	if c.MergeRetrySeconds == nil || config.ValidateMergeRetrySeconds(c.MergeRetrySeconds) != nil {
		return config.DefaultMergeRetrySeconds
	}
	return *c.MergeRetrySeconds
}

// EffectiveMergeConflictModel returns the model that resolves merge
// conflicts. Empty means the strongest configured planning model; the
// caller resolves that.
func EffectiveMergeConflictModel(c config.AutonomousParallelConfig) string {
	return strings.TrimSpace(c.MergeConflictModel)
}
