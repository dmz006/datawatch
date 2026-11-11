// autonomous branch-refresh over the chat channel: set the per-Automaton
// branch refresh override.
//
//	autonomous branch-refresh <id> enabled|disabled|inherit
//
// enabled refreshes the Automaton even when the daemon setting is off,
// disabled never refreshes it, inherit clears the override so the daemon
// setting applies. The daemon settings themselves are config keys, set with
// the configure verb: autonomous.branch_refresh_enabled and
// autonomous.branch_refresh_behind_limit.

package router

import (
	"fmt"
	"net/url"
	"strings"
)

const branchRefreshUsage = "usage: autonomous branch-refresh <id> enabled|disabled|inherit"

// BranchRefreshCommVerbs lists the branch refresh comm verbs for the parity test.
func BranchRefreshCommVerbs() []string { return []string{"branch-refresh"} }

// parseBranchRefreshMode maps a mode word to the REST "enabled" JSON value.
func parseBranchRefreshMode(word string) (string, bool) {
	switch strings.ToLower(word) {
	case "enabled", "enable", "on", "true":
		return "true", true
	case "disabled", "disable", "off", "false":
		return "false", true
	case "inherit", "clear", "default":
		return "null", true
	}
	return "", false
}

// handleBranchRefreshVerb answers the autonomous command's branch-refresh verb.
func (r *Router) handleBranchRefreshVerb(verb string, args []string) {
	if len(args) != 3 || strings.TrimSpace(args[1]) == "" {
		r.reply("autonomous branch-refresh failed", branchRefreshUsage)
		return
	}
	id := args[1]
	val, ok := parseBranchRefreshMode(args[2])
	if !ok {
		r.reply("autonomous branch-refresh failed", fmt.Sprintf("unknown mode %q\n%s", args[2], branchRefreshUsage))
		return
	}
	_, err := r.commJSON("POST", "/api/autonomous/prds/"+url.PathEscape(id)+"/branch-refresh", `{"enabled":`+val+`}`)
	if err != nil {
		r.reply("autonomous branch-refresh failed", err.Error())
		return
	}
	word := strings.ToLower(args[2])
	if val == "null" {
		word = "inherit"
	} else if val == "true" {
		word = "enabled"
	} else {
		word = "disabled"
	}
	r.reply("autonomous branch-refresh", fmt.Sprintf("branch refresh for %s: %s", id, word))
}
