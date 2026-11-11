// autonomous unlanded-work over the chat channel: the list of archived
// Automaton branches whose work is not on the default branch, and the actions
// on one listed branch (keep, archive-branch, delete-worktree). The reply
// renders the same shared text the CLI does
// (internal/autonomous/unlanded_work_text.go). The list is a read; deleting a
// worktree needs the word confirm.

package router

import (
	"encoding/json"
	"strings"

	"github.com/dmz006/datawatch/internal/autonomous"
)

// unlandedWorkUsage is the usage line for the unlanded-work comm verb.
const unlandedWorkUsage = "usage: autonomous unlanded-work [keep|archive-branch|delete-worktree <branch> [confirm]]"

// UnlandedWorkCommVerbs lists the unlanded-work comm verbs for the parity test.
func UnlandedWorkCommVerbs() []string { return []string{"unlanded-work"} }

// handleUnlandedWorkVerb answers the autonomous command's unlanded-work verb.
func (r *Router) handleUnlandedWorkVerb(verb string, args []string) {
	switch verb {
	case "unlanded-work", "unlanded_work":
		r.replyUnlandedWork(args)
	default:
		r.reply("autonomous unlanded-work failed", unlandedWorkUsage)
	}
}

func (r *Router) replyUnlandedWork(args []string) {
	// args[0] is the verb itself; the rest is the optional action.
	if len(args) <= 1 {
		r.replyUnlandedList()
		return
	}
	action := strings.ToLower(strings.ReplaceAll(args[1], "_", "-"))
	switch action {
	case "keep", "archive-branch", "delete-worktree":
	default:
		r.reply("autonomous unlanded-work failed", unlandedWorkUsage)
		return
	}
	if len(args) < 3 || strings.TrimSpace(args[2]) == "" {
		r.reply("autonomous unlanded-work failed", unlandedWorkUsage)
		return
	}
	req := map[string]any{"branch": strings.TrimSpace(args[2])}
	switch action {
	case "keep":
		if len(args) > 3 {
			req["note"] = strings.Join(args[3:], " ")
		}
	case "delete-worktree":
		if len(args) != 4 || !strings.EqualFold(args[3], "confirm") {
			r.reply("autonomous unlanded-work failed", "deleting a worktree needs the word confirm: autonomous unlanded-work delete-worktree <branch> confirm")
			return
		}
		req["confirm"] = true
	}
	payload, _ := json.Marshal(req)
	out, err := r.commJSON("POST", "/api/autonomous/unlanded-work/"+action, string(payload))
	if err != nil {
		r.reply("autonomous unlanded-work failed", err.Error())
		return
	}
	r.reply("autonomous unlanded-work "+action, autonomous.RenderUnlandedActionResult(action, []byte(out)))
}

func (r *Router) replyUnlandedList() {
	out, err := r.commGet("/api/autonomous/unlanded-work", nil)
	if err != nil {
		r.reply("autonomous unlanded-work failed", err.Error())
		return
	}
	l, err := autonomous.ParseUnlandedWorkList([]byte(out))
	if err != nil {
		r.reply("autonomous unlanded-work failed", err.Error())
		return
	}
	r.reply("autonomous unlanded-work", strings.TrimRight(autonomous.RenderUnlandedWorkList(l), "\n"))
}
