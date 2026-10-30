// autonomous merge and autonomous safe-to-delete over the chat channel: the
// two merge reads, so the operator can ask from Signal / Telegram which
// finished Automata are waiting to be merged and which Automaton branches
// can be deleted, without curl. The replies render the same shared text the
// CLI does (internal/autonomous/merge_text.go). Both are reads — nothing is
// merged, deleted or pushed.

package router

import (
	"strings"

	"github.com/dmz006/datawatch/internal/autonomous"
)

const (
	// mergeUsage and safeToDeleteUsage are the usage lines for the merge
	// comm verbs. Neither verb takes an argument.
	mergeUsage        = "usage: autonomous merge"
	safeToDeleteUsage = "usage: autonomous safe-to-delete"
)

// MergeCommVerbs lists the merge comm verbs for the parity test.
func MergeCommVerbs() []string { return []string{"merge", "safe-to-delete"} }

// handleMergeVerb answers the autonomous command's merge verbs.
func (r *Router) handleMergeVerb(verb string, args []string) {
	switch verb {
	case "merge":
		r.replyMergeState(args)
	case "safe-to-delete", "safe_to_delete":
		r.replySafeToDelete(args)
	default:
		r.reply("autonomous merge failed", mergeUsage+"\n"+safeToDeleteUsage)
	}
}

func (r *Router) replyMergeState(args []string) {
	if len(args) > 1 {
		r.reply("autonomous merge failed", mergeUsage)
		return
	}
	out, err := r.commGet("/api/autonomous/merge", nil)
	if err != nil {
		r.reply("autonomous merge failed", err.Error())
		return
	}
	ms, err := autonomous.ParseMergeStatus([]byte(out))
	if err != nil {
		r.reply("autonomous merge failed", err.Error())
		return
	}
	r.reply("autonomous merge", strings.TrimRight(autonomous.RenderMergeState(ms), "\n"))
}

func (r *Router) replySafeToDelete(args []string) {
	if len(args) > 1 {
		r.reply("autonomous safe-to-delete failed", safeToDeleteUsage)
		return
	}
	out, err := r.commGet("/api/autonomous/safe-to-delete", nil)
	if err != nil {
		r.reply("autonomous safe-to-delete failed", err.Error())
		return
	}
	rep, err := autonomous.ParseSafeToDelete([]byte(out))
	if err != nil {
		r.reply("autonomous safe-to-delete failed", err.Error())
		return
	}
	r.reply("autonomous safe-to-delete", strings.TrimRight(autonomous.RenderSafeToDelete(rep), "\n"))
}
