// `sessions groups [axis] [tag-prefix]` — per-group count summary over the
// comm channel.
//
// Same wire shape as progress_cmd.go (BL430): parse args into the REST
// grouping query, loop into the daemon's own API (r.commGet), render the
// reply. The grouped sessions list is served by
// GET /api/sessions?group_by=...&tag_prefix=..., whose grouping read the
// PWA's session-list page, the MCP list_sessions params and the CLI
// --group-by flag all share — so this verb does not re-implement the read.

package router

import (
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"
)

const sessionGroupsUsage = "usage: sessions groups [tag|state|node|project|automaton] [tag-prefix]"

// SessionGroupAxes in the reply's stable axis order for error text.
var sessionGroupAxes = []string{"tag", "state", "node", "project", "automaton"}

// parseSessionGroupsArgs maps "axis [tag-prefix]" to GET /api/sessions query
// params. Axis defaults to tag (the read's default); the prefix is forwarded
// verbatim — the REST handler decides whether it applies (tag axis only).
func parseSessionGroupsArgs(text string) (url.Values, error) {
	fields := strings.Fields(text)
	axis := "tag"
	if len(fields) > 0 {
		axis = strings.ToLower(fields[0])
		ok := false
		for _, a := range sessionGroupAxes {
			if a == axis {
				ok = true
			}
		}
		if !ok {
			return nil, fmt.Errorf("unknown axis %q (supported: %s)", axis, strings.Join(sessionGroupAxes, ", "))
		}
	}
	if len(fields) > 2 {
		return nil, fmt.Errorf("too many arguments")
	}
	prefix := ""
	if len(fields) == 2 {
		prefix = fields[1]
	}
	q := url.Values{}
	q.Set("group_by", axis)
	if prefix != "" {
		q.Set("tag_prefix", prefix)
	}
	return q, nil
}

// sessionGroupSummary mirrors one group of the REST grouped response.
type sessionGroupSummary struct {
	Key      string         `json:"key"`
	Count    int            `json:"count"`
	StateMix map[string]int `json:"state_mix"`
	Progress *struct {
		Done  int `json:"done"`
		Total int `json:"total"`
	} `json:"progress"`
}

// sessionGroupsResponse mirrors GET /api/sessions?group_by=...
type sessionGroupsResponse struct {
	GroupBy     string                `json:"group_by"`
	TagPrefix   string                `json:"tag_prefix"`
	Groups      []sessionGroupSummary `json:"groups"`
	StateTotals map[string]int        `json:"state_totals"`
	Total       int                   `json:"total"`
}

// stateMixLine renders "state×n, state×m" in stable sorted order.
func stateMixLine(mix map[string]int) string {
	keys := make([]string, 0, len(mix))
	for k := range mix {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s×%d", k, mix[k]))
	}
	return strings.Join(parts, ", ")
}

// sessionGroupsSummary renders the grouped response as the comm reply:
// one line per group (key, count, state mix — plus the block progress on
// the automaton axis) and a per-state totals line at the end.
func sessionGroupsSummary(body string) (string, error) {
	var res sessionGroupsResponse
	if err := json.Unmarshal([]byte(body), &res); err != nil {
		return "", err
	}
	var sb strings.Builder
	if res.TagPrefix != "" {
		fmt.Fprintf(&sb, "by %s, tag prefix %q — %d session(s)\n", res.GroupBy, res.TagPrefix, res.Total)
	} else {
		fmt.Fprintf(&sb, "by %s — %d session(s)\n", res.GroupBy, res.Total)
	}
	for _, g := range res.Groups {
		if g.Progress != nil {
			fmt.Fprintf(&sb, "  %s  %d (%s)  [done %d/%d]\n", g.Key, g.Count, stateMixLine(g.StateMix), g.Progress.Done, g.Progress.Total)
		} else {
			fmt.Fprintf(&sb, "  %s  %d (%s)\n", g.Key, g.Count, stateMixLine(g.StateMix))
		}
	}
	sb.WriteString("  totals: ")
	sb.WriteString(stateMixLine(res.StateTotals) + "\n")
	return strings.TrimRight(sb.String(), "\n"), nil
}

// handleSessionGroups serves the comm verbs `sessions groups [axis]
// [tag-prefix]`.
func (r *Router) handleSessionGroups(args string) {
	q, err := parseSessionGroupsArgs(args)
	if err != nil {
		r.send(fmt.Sprintf("%v\n%s", err, sessionGroupsUsage))
		return
	}
	body, err := r.commGet("/api/sessions", q)
	if err != nil {
		r.reply("sessions groups failed", err.Error())
		return
	}
	text, err := sessionGroupsSummary(body)
	if err != nil {
		r.reply("sessions groups failed", fmt.Sprintf("cannot read the grouped list: %v", err))
		return
	}
	r.reply("Sessions by "+q.Get("group_by"), text)
}
