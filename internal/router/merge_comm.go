// autonomous merge, autonomous safe-to-delete, the branch reaper (branch-reap,
// branch-reap-restore) and the remote branch reaper (remote-reap,
// remote-reap-restore) over the chat channel: the merge reads and the reap
// surfaces, so the operator can ask from Signal / Telegram which finished
// Automata are waiting to be merged, which Automaton branches can be
// deleted, which task sub-branches a reap pass would back up and which are
// held, which Automaton work branches on origin a remote reap pass would
// delete and which are held, and can bring one reaped branch back from its
// newest backup ref, without curl. The replies render the same shared text
// the CLI does (internal/autonomous/reap_text.go and
// remote_reap_text.go). remote-reap is a read; only remote-reap-restore
// writes (it recreates one branch). Nothing here runs a reap pass, merges
// or pushes.

package router

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/dmz006/datawatch/internal/autonomous"
)

const (
	// mergeUsage, safeToDeleteUsage, reapReportUsage, reapRestoreUsage and
	// the remoteReap* lines are the usage lines for the merge-area comm
	// verbs. Only branch-reap-restore and remote-reap-restore take an
	// argument (the branch name).
	mergeUsage             = "usage: autonomous merge"
	safeToDeleteUsage      = "usage: autonomous safe-to-delete"
	reapReportUsage        = "usage: autonomous branch-reap"
	reapRestoreUsage       = "usage: autonomous branch-reap-restore <branch>"
	remoteReapUsage        = "usage: autonomous remote-reap"
	remoteReapRestoreUsage = "usage: autonomous remote-reap-restore <branch>"
)

// MergeCommVerbs lists the merge-area comm verbs for the parity test.
func MergeCommVerbs() []string {
	return []string{"merge", "safe-to-delete", "branch-reap", "branch-reap-restore", "remote-reap", "remote-reap-restore"}
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
	case "remote-reap", "remote_reap":
		r.replyRemoteReap(args)
	case "remote-reap-restore", "remote_reap_restore":
		r.replyRemoteReapRestore(args)
	default:
		r.reply("autonomous merge failed", mergeUsage+"\n"+safeToDeleteUsage+"\n"+reapReportUsage+"\n"+reapRestoreUsage+"\n"+remoteReapUsage+"\n"+remoteReapRestoreUsage)
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

// replyRemoteReap answers "autonomous remote-reap": the read-only remote
// reap report on origin (GET /api/autonomous/remote-reap), rendered with
// the shared text the CLI and the PWA card draw — the Automaton work
// branches a pass would delete from origin (fully merged and older than the
// retention window), the ones held with the reason, every backup ref under
// refs/datawatch/remote-reap-backup/ and the retention in whole days. A
// read verb: it previews, it never deletes anything on origin or restores
// a branch.
func (r *Router) replyRemoteReap(args []string) {
	if len(args) > 1 {
		r.reply("autonomous remote-reap failed", remoteReapUsage+"\n"+remoteReapRestoreUsage)
		return
	}
	out, err := r.commGet("/api/autonomous/remote-reap", nil)
	if err != nil {
		r.reply("autonomous remote-reap failed", err.Error())
		return
	}
	rep, err := autonomous.ParseRemoteReapSurfaceReport([]byte(out))
	if err != nil {
		r.reply("autonomous remote-reap failed", err.Error())
		return
	}
	r.reply("autonomous remote-reap", strings.TrimRight(autonomous.RenderRemoteReapReport(rep), "\n"))
}

// replyRemoteReapRestore answers "autonomous remote-reap-restore <branch>":
// it posts the one branch name to /api/autonomous/remote-reap/restore and
// renders the shared restore result — the branch that came back from its
// newest remote reap backup ref and the commit it now points at.
func (r *Router) replyRemoteReapRestore(args []string) {
	if len(args) != 2 {
		r.reply("autonomous remote-reap-restore failed", remoteReapRestoreUsage)
		return
	}
	body, err := json.Marshal(map[string]string{"branch": args[1]})
	if err != nil {
		r.reply("autonomous remote-reap-restore failed", err.Error())
		return
	}
	out, err := r.commJSON(http.MethodPost, "/api/autonomous/remote-reap/restore", string(body))
	if err != nil {
		r.reply("autonomous remote-reap-restore failed", err.Error())
		return
	}
	res, err := autonomous.ParseRemoteReapRestoreResult([]byte(out))
	if err != nil {
		r.reply("autonomous remote-reap-restore failed", err.Error())
		return
	}
	r.reply("autonomous remote-reap-restore", strings.TrimRight(autonomous.RenderRemoteReapRestore(res), "\n"))
}
