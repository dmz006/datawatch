// Deprecation notice for the graph create, plan and run entry points, which
// keep working but are replaced by the Automation tab.

package orchestrator

import "sync"

// DeprecationNotice returns the operator-facing deprecation text for one of
// the graph create, plan or run tools.
func DeprecationNotice(tool string) string {
	return tool + " is deprecated and will be removed in a future release. " +
		"Migrate with 'automation import-graph' (the automation_import_graph tool, " +
		"or POST /api/automations/import-graph/{graph_id}); " +
		"the Automation tab replaces the graph workflow."
}

var deprecationWarned sync.Map

// WarnDeprecatedOnce calls warn the first time it sees the given tool name in
// this process and returns true; later calls do nothing and return false.
func WarnDeprecatedOnce(tool string, warn func(format string, args ...any)) bool {
	if _, loaded := deprecationWarned.LoadOrStore(tool, struct{}{}); loaded {
		return false
	}
	if warn != nil {
		warn("[orchestrator] %s", DeprecationNotice(tool))
	}
	return true
}

// resetDeprecationWarned clears the once-per-process state for tests.
func resetDeprecationWarned() { deprecationWarned = sync.Map{} }

// ResetDeprecationWarnedForTest lets tests in other packages start from a
// clean once-per-process state.
func ResetDeprecationWarnedForTest() { resetDeprecationWarned() }
