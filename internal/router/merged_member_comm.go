// autonomous merged-members over the chat channel: the merged-member
// decisions, so the operator can read from Signal / Telegram which members the
// coordinator moved past or held because their branch was already merged. The
// reply renders the same shared text the CLI does
// (internal/autonomous/merged_member_text.go), at most ten lines, newest first.
// A read — nothing is written.

package router

import (
	"net/url"
	"strings"

	"github.com/dmz006/datawatch/internal/autonomous"
)

const (
	// mergedMemberUsage is the usage line for the merged-members comm verb.
	// The optional automaton id narrows the read to one Automaton.
	mergedMemberUsage = "usage: autonomous merged-members [automation-id]"

	// mergedMemberCommLines is the most lines one reply carries.
	mergedMemberCommLines = 10
)

// MergedMemberCommVerbs lists the merged-member comm verbs for the parity test.
func MergedMemberCommVerbs() []string { return []string{"merged-members"} }

// handleMergedMemberVerb answers the autonomous command's merged-members verb.
func (r *Router) handleMergedMemberVerb(verb string, args []string) {
	if len(args) > 2 {
		r.reply("autonomous merged-members failed", mergedMemberUsage)
		return
	}
	path := "/api/automations/merged-members"
	if len(args) == 2 && strings.TrimSpace(args[1]) != "" {
		path = "/api/automations/" + url.PathEscape(strings.TrimSpace(args[1])) + "/merged-members"
	}
	out, err := r.commGet(path, nil)
	if err != nil {
		r.reply("autonomous merged-members failed", err.Error())
		return
	}
	rows, err := autonomous.ParseMergedMembers([]byte(out))
	if err != nil {
		r.reply("autonomous merged-members failed", err.Error())
		return
	}
	r.reply("autonomous merged-members", strings.TrimRight(autonomous.RenderMergedMembers(rows, mergedMemberCommLines), "\n"))
}
