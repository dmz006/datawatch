// Comm verb: "autonomous event-log" — the Automata event log over chat.
//
//	autonomous event-log [kind=<k[,k...]>] [project=<p>] [q=<text>] [limit=<1..10>] [before=<cursor>]
//	    GET /api/autonomous/event-log?kind=&project=&q=&limit=&before=
//
// The reply is a short text list, newest first, of up to ten entries. Each
// line carries the time, the kind, the summary and the next action. When more
// events match, the reply ends with the "before=" cursor for the next page.
// The REST route does the filtering, paging and cursor checks; this verb only
// parses the key=value arguments and renders the response.

package router

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

const (
	commEventLogPath         = "/api/autonomous/event-log"
	commEventLogDefaultLimit = 10
	commEventLogMaxLimit     = 10
	commEventLogUsage        = "usage: autonomous event-log [kind=<k[,k...]>] [project=<p>] [q=<text>] [limit=<1..10>] [before=<cursor>]"
)

// commEventLogArgs is the parsed form of the key=value arguments.
type commEventLogArgs struct {
	kind    string
	project string
	q       string
	limit   int
	before  string
}

// parseCommEventLog parses the arguments after "event-log". Every argument
// must be key=value with a known key. A blank value means the key is unset,
// the same as on the REST route. A limit must be a whole number from 1 to 10.
func parseCommEventLog(args []string) (commEventLogArgs, error) {
	p := commEventLogArgs{limit: commEventLogDefaultLimit}
	for _, a := range args {
		i := strings.IndexByte(a, '=')
		if i <= 0 {
			return p, fmt.Errorf("%q is not key=value\n%s", a, commEventLogUsage)
		}
		key := strings.ToLower(strings.TrimSpace(a[:i]))
		val := strings.TrimSpace(a[i+1:])
		switch key {
		case "kind":
			p.kind = val
		case "project":
			p.project = val
		case "q":
			p.q = val
		case "before":
			p.before = val
		case "limit":
			if val == "" {
				continue
			}
			n, err := strconv.Atoi(val)
			if err != nil || n < 1 || n > commEventLogMaxLimit {
				return p, fmt.Errorf("limit must be a whole number from 1 to %d\n%s", commEventLogMaxLimit, commEventLogUsage)
			}
			p.limit = n
		default:
			return p, fmt.Errorf("unknown key %q\n%s", key, commEventLogUsage)
		}
	}
	return p, nil
}

// query builds the REST query string for the parsed arguments.
func (p commEventLogArgs) query() url.Values {
	v := url.Values{"limit": {strconv.Itoa(p.limit)}}
	if p.kind != "" {
		v.Set("kind", p.kind)
	}
	if p.project != "" {
		v.Set("project", p.project)
	}
	if p.q != "" {
		v.Set("q", p.q)
	}
	if p.before != "" {
		v.Set("before", p.before)
	}
	return v
}

// commandArgs renders the set arguments back as key=value text, in a fixed
// order, for the "more" hint.
func (p commEventLogArgs) commandArgs() string {
	var parts []string
	if p.kind != "" {
		parts = append(parts, "kind="+p.kind)
	}
	if p.project != "" {
		parts = append(parts, "project="+p.project)
	}
	if p.q != "" {
		parts = append(parts, "q="+p.q)
	}
	if p.limit != commEventLogDefaultLimit {
		parts = append(parts, "limit="+strconv.Itoa(p.limit))
	}
	if p.before != "" {
		parts = append(parts, "before="+p.before)
	}
	return strings.Join(parts, " ")
}

// handleAutonomousEventLog implements "autonomous event-log".
func (r *Router) handleAutonomousEventLog(args []string) {
	p, err := parseCommEventLog(args)
	if err != nil {
		r.reply("autonomous event-log", err.Error())
		return
	}
	body, err := r.commGet(commEventLogPath, p.query())
	if err != nil {
		r.reply("autonomous event-log failed", err.Error())
		return
	}
	text, err := renderCommEventLog(body, p)
	if err != nil {
		r.reply("autonomous event-log failed", err.Error())
		return
	}
	r.reply("autonomous event-log", text)
}

// renderCommEventLog renders the event-log REST body as a short text list.
// The "more" line repeats the same filters, so the operator can paste it back.
func renderCommEventLog(body string, p commEventLogArgs) (string, error) {
	var resp struct {
		Events []struct {
			Time       string `json:"time"`
			Kind       string `json:"kind"`
			Summary    string `json:"summary"`
			NextAction string `json:"next_action"`
		} `json:"events"`
		NextCursor string `json:"next_cursor"`
		Total      int    `json:"total"`
	}
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		return "", fmt.Errorf("reading event log: %w", err)
	}
	if len(resp.Events) == 0 {
		return "no matching Automata events", nil
	}
	shown := resp.Events
	if len(shown) > commEventLogMaxLimit {
		shown = shown[:commEventLogMaxLimit]
	}
	lines := []string{fmt.Sprintf("%d of %d matching events (newest first):", len(shown), resp.Total)}
	for _, e := range shown {
		line := fmt.Sprintf("%s %s %s", e.Time, e.Kind, e.Summary)
		if e.NextAction != "" {
			line += " | next: " + e.NextAction
		}
		lines = append(lines, line)
	}
	if resp.NextCursor != "" {
		next := p
		next.before = resp.NextCursor
		lines = append(lines, "more: autonomous event-log "+next.commandArgs())
	}
	return strings.Join(lines, "\n"), nil
}
