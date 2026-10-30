// autonomous cleanup over the chat channel: the cleanup status read, so
// the operator can ask from Signal / Telegram what the worktrees pass and
// the temp-file sweep have each done, which disks they guard, and whether
// the disk alert is active — without curl. The reply renders the same
// shared text the CLI does (internal/autonomous/cleanup_text.go), and
// posts the disk alert text after the status when the alert flag is set.
// A read — nothing is removed.

package router

import (
	"strings"

	"github.com/dmz006/datawatch/internal/autonomous"
)

// cleanupUsage is the usage line for the cleanup comm verb. The verb
// takes no argument.
const cleanupUsage = "usage: autonomous cleanup"

// CleanupCommVerbs lists the cleanup comm verbs for the parity test.
func CleanupCommVerbs() []string { return []string{"cleanup"} }

// handleCleanupVerb answers the autonomous command's cleanup verb.
func (r *Router) handleCleanupVerb(verb string, args []string) {
	switch verb {
	case "cleanup":
		r.replyCleanupStatus(args)
	default:
		r.reply("autonomous cleanup failed", cleanupUsage)
	}
}

func (r *Router) replyCleanupStatus(args []string) {
	if len(args) > 1 {
		r.reply("autonomous cleanup failed", cleanupUsage)
		return
	}
	out, err := r.commGet("/api/autonomous/cleanup", nil)
	if err != nil {
		r.reply("autonomous cleanup failed", err.Error())
		return
	}
	st, err := autonomous.ParseCleanupStatus([]byte(out))
	if err != nil {
		r.reply("autonomous cleanup failed", err.Error())
		return
	}
	text := strings.TrimRight(autonomous.RenderCleanupStatus(st), "\n")
	if st.DiskAlert {
		text += "\n" + autonomous.CleanupDiskAlertBody(st)
	}
	r.reply("autonomous cleanup", text)
}
