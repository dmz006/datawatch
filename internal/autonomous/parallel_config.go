// Parallel execution config: the one place the default for
// autonomous.parallel.read_only_tasks lives. Tasks flagged read_only skip
// the tree lock when the key is on.
package autonomous

import "github.com/dmz006/datawatch/internal/config"

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
