// autonomous merge, autonomous safe-to-delete and the branch reaper (branch-reap,
// branch-reap-restore) over the chat channel: the merge reads and the branch
// surface, so the operator can ask from Signal / Telegram which finished
// Automata are waiting to be merged, which Automaton branches can be
// deleted, which task sub-branches a reap pass would back up and which are
// held, and can bring one reaped branch back from its newest backup ref,
// without curl. The replies render the same shared text the CLI does
// (internal/autonomous/reap_text.go). branch-reap is a read; only
// branch-reap-restore writes (it recreates one branch). Nothing here runs a
// reap pass, merges or pushes.

package router

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/dmz006/datawatch/internal/autonomous"
)

const (
	// mergeUsage, safeToDeleteUsage, reapReportUsage and reapRestoreUsage
	// are the usage lines for the merge-area comm verbs. Only
	// branch-reap-restore takes an argument (the branch name).
	mergeUsage        = "usage: autonomous merge"
	safeToDeleteUsage = "usage: autonomous safe-to-delete"
	reapReportUsage   = "usage: autonomous branch-reap"
	reapRestoreUsage  = "usage: autonomous branch-reap-restore <branch>"
)

// MergeCommVerbs lists the merge-area comm verbs for the parity test.
func MergeCommVerbs() []string {
	return []string{"merge", "safe-to-delete", "branch-reap", "branch-reap-restore"}
}

// handleMergeVerb answers the autonomous command's merge-area verbs.
func (r *Router) handleMergeVerb(verb string, args []string) {
	switch verb {
	case "merge":
		r.replyMergeState(args)
	case "safe-to-delete", "safe_to_delete":
		r.replySafeToDelete(args)
	case "branch-reap", "branch_reap":
		r.replyBranchReap(args)
	case "branch-reap-restore", "branch_reap_restore":
		r.replyBranchReapRestore(args)
	default:
		r.reply("autonomous merge failed", mergeUsage+"\n"+safeToDeleteUsage+"\n"+reapReportUsage+"\n"+reapRestoreUsage)
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

// replyBranchReap answers "autonomous branch-reap": the read-only branch
// reap report (GET /api/autonomous/branch-reap), rendered with the shared
// text the CLI and the PWA card draw — the task sub-branches a pass would
// back up and delete (with the backup ref where the tip lives and what
// contained it), the ones held, and every backup ref. A read verb: it
// previews, it never re-runs the reaper or restores a branch.
func (r *Router) replyBranchReap(args []string) {
	if len(args) > 1 {
		r.reply("autonomous branch-reap failed", reapReportUsage+"\n"+reapRestoreUsage)
		return
	}
	out, err := r.commGet("/api/autonomous/branch-reap", nil)
	if err != nil {
		r.reply("autonomous branch-reap failed", err.Error())
		return
	}
	rep, err := autonomous.ParseReapSurfaceReport([]byte(out))
	if err != nil {
		r.reply("autonomous branch-reap failed", err.Error())
		return
	}
	r.reply("autonomous branch-reap", strings.TrimRight(autonomous.RenderReapReport(rep), "\n"))
}

// replyBranchReapRestore answers "autonomous branch-reap-restore <branch>":
// it posts the one branch name to /api/autonomous/branch-reap/restore and
// renders the shared restore result — the branch that came back, the backup
// ref it was restored from and the commit it now points at. The only write
// of this verb group: the branch is recreated from its newest backup ref.
func (r *Router) replyBranchReapRestore(args []string) {
	if len(args) != 2 {
		r.reply("autonomous branch-reap-restore failed", reapRestoreUsage)
		return
	}
	body, err := json.Marshal(map[string]string{"branch": args[1]})
	if err != nil {
		r.reply("autonomous branch-reap-restore failed", err.Error())
		return
	}
	out, err := r.commJSON(http.MethodPost, "/api/autonomous/branch-reap/restore", string(body))
	if err != nil {
		r.reply("autonomous branch-reap-restore failed", err.Error())
		return
	}
	res, err := autonomous.ParseReapRestoreResult([]byte(out))
	if err != nil {
		r.reply("autonomous branch-reap-restore failed", err.Error())
		return
	}
	r.reply("autonomous branch-reap-restore", strings.TrimRight(autonomous.RenderReapRestore(res), "\n"))
}
