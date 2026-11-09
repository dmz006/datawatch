// standard_tasks.go — the comm channel reply for the standard task list:
// one line per task template the decomposer can add to a story, with its
// id, title, on/off state and mode.
//
// The rows come from autonomous.ListStandardTasks, the same listing the
// REST read (GET /api/autonomous/standard-tasks) and the MCP tool
// autonomous_standard_tasks serve, so chat, REST and MCP agree. The chat
// word is "standard tasks" (or "autonomous standard-tasks"), routed from
// commands.go and sx2_parity.go's autonomous switch.
package router

import (
	"fmt"
	"strings"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
)

// standardTasksEmpty is the body when the listing has no rows.
const standardTasksEmpty = "no standard tasks are defined."

// buildStandardTasksReply renders the standard task list for projectDir
// (empty reports the global switch). It is one line per row:
// "<id> | <title> | on|off | <mode>". A nil cfg reads as the shipped defaults.
func buildStandardTasksReply(cfg *config.Config, projectDir string) string {
	rows := autonomous.ListStandardTasks(cfg, projectDir)
	if len(rows) == 0 {
		return standardTasksEmpty
	}
	lines := make([]string, 0, len(rows))
	for _, row := range rows {
		state := "off"
		if row.Enabled {
			state = "on"
		}
		lines = append(lines, fmt.Sprintf("%s | %s | %s | %s", row.ID, row.Title, state, row.Mode))
	}
	return strings.Join(lines, "\n")
}

// replyStandardTasks — "standard tasks": reply with the standard task list
// for the global settings.
func (r *Router) replyStandardTasks() {
	r.reply("standard tasks", buildStandardTasksReply(r.cfg, ""))
}
