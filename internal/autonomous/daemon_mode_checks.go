// daemon_mode_checks.go — the plan checks' view of daemon-mode template tasks.
//
// A template task whose effective mode is "daemon" is performed by the
// daemon itself (see daemon_template_task.go): no worker session is started
// and no verification script, changed file or report is expected from it.
// The decompose lint (decompose_lint.go) and the plan structure check
// (plan_coverage_findings.go) therefore skip such a task in every per-task
// rule. A task is daemon-mode when either:
//
//   - its DaemonRun flag is set (ApplyTaskTemplates sets it when the template
//     is daemon-mode at apply time), or
//   - it carries a template marker (TaskTemplateIDMarker) whose effective
//     mode is daemon. A planner may write its own version of a template task
//     that ApplyTaskTemplates never adds, so the marker is checked too.
//
// The checks themselves take no config, so the manager stamps DaemonRun on a
// copy of the stories (stampDaemonModeTasks) before it runs them. Tasks in
// llm mode are untouched and are checked exactly as before.
package autonomous

import (
	"github.com/dmz006/datawatch/internal/config"
)

// daemonRunsTask reports whether a task is performed by the daemon without a
// session. The per-task plan checks skip such a task entirely.
func daemonRunsTask(t Task) bool { return t.DaemonRun }

// templateModeIDOf returns the template id a task was built from, read from
// its title first and its spec second. Empty for a task with no marker.
func templateModeIDOf(t Task) string {
	if id := taskTemplateIDOfSpec(t.Title); id != "" {
		return id
	}
	return taskTemplateIDOfSpec(t.Spec)
}

// stampDaemonModeTasks returns a copy of stories in which every task that is
// daemon-mode by its template marker (see the file header) has DaemonRun set.
// The input stories are never modified: each story's task slice is copied
// before any flag is set. A nil auto config stamps nothing.
func stampDaemonModeTasks(stories []Story, auto *config.AutonomousConfig) []Story {
	out := make([]Story, len(stories))
	for si, s := range stories {
		out[si] = s
		if len(s.Tasks) == 0 {
			continue
		}
		tasks := make([]Task, len(s.Tasks))
		copy(tasks, s.Tasks)
		for ti := range tasks {
			if tasks[ti].DaemonRun || auto == nil {
				continue
			}
			id := templateModeIDOf(tasks[ti])
			if id != "" && auto.EffectiveTaskTemplateMode(id) == config.TaskTemplateModeDaemon {
				tasks[ti].DaemonRun = true
			}
		}
		out[si].Tasks = tasks
	}
	return out
}

// daemonModeStories is stampDaemonModeTasks against the manager's current
// task-template config. The copy is taken under the manager lock.
func (m *Manager) daemonModeStories(stories []Story) []Story {
	m.mu.Lock()
	tt := m.cfg.TaskTemplates
	m.mu.Unlock()
	auto := config.AutonomousConfig{TaskTemplates: tt}
	return stampDaemonModeTasks(stories, &auto)
}
