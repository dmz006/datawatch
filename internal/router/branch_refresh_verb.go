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
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

const branchRefreshUsage = "usage: autonomous branch-refresh <id> enabled|disabled|inherit"

// BranchRefreshCommVerbs lists the branch refresh comm verbs for the parity test.
func BranchRefreshCommVerbs() []string {
	return []string{"branch-refresh", "branch-refresh-stats"}
}

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
	var word string
	switch val {
	case "null":
		word = "inherit"
	case "true":
		word = "enabled"
	default:
		word = "disabled"
	}
	r.reply("autonomous branch-refresh", fmt.Sprintf("branch refresh for %s: %s", id, word))
}

// handleBranchRefreshStatsVerb answers "autonomous branch-refresh-stats": the
// branch refresh numbers from GET /api/autonomous/branch-refresh/stats.
func (r *Router) handleBranchRefreshStatsVerb() {
	body, err := r.commJSON("GET", "/api/autonomous/branch-refresh/stats", "")
	if err != nil {
		r.reply("autonomous branch-refresh-stats failed", err.Error())
		return
	}
	var st struct {
		Enabled     bool `json:"enabled"`
		BehindLimit int  `json:"behind_limit"`
		Tracked     int  `json:"tracked"`
		Clean       int  `json:"clean"`
		Conflict    int  `json:"conflict"`
		Skipped     int  `json:"skipped"`
		Error       int  `json:"error"`
		Stopped     int  `json:"stopped"`
		OverrideOn  int  `json:"override_on"`
		OverrideOff int  `json:"override_off"`
	}
	if err := json.Unmarshal([]byte(body), &st); err != nil {
		r.reply("autonomous branch-refresh-stats failed", "unexpected response: "+err.Error())
		return
	}
	setting := "off"
	if st.Enabled {
		setting = "on"
	}
	r.reply("autonomous branch-refresh-stats", fmt.Sprintf(
		"branch refresh: %s (limit %d commits behind)\nautomata with a refresh: %d (clean %d, conflict %d, skipped %d, error %d)\nstopped by a conflict: %d\noverrides: %d on, %d off",
		setting, st.BehindLimit, st.Tracked, st.Clean, st.Conflict, st.Skipped, st.Error, st.Stopped, st.OverrideOn, st.OverrideOff))
}
