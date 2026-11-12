// Comm verb: "autonomous event-kinds" — the lifecycle event kinds over chat.
//
//	autonomous event-kinds [group=<g>]
//	    GET /api/autonomous/event-kinds
//
// The reply lists each group followed by its kinds, one per line: the kind, a
// marker when it is in the default set that wakes the supervisor, and its
// description. group= keeps one group, matched case-insensitively. The REST
// route returns every group; this verb parses the argument, keeps the wanted
// group and renders the response.

package router

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

const (
	commEventKindsPath  = "/api/autonomous/event-kinds"
	commEventKindsUsage = "usage: autonomous event-kinds [group=<g>]"
)

// commEventKindsBody is the /api/autonomous/event-kinds body.
type commEventKindsBody struct {
	Groups []commEventKindsGroup `json:"groups"`
}

// commEventKindsGroup is one group of kinds in the listing.
type commEventKindsGroup struct {
	Group string               `json:"group"`
	Kinds []commEventKindsKind `json:"kinds"`
}

// commEventKindsKind is one kind in a group.
type commEventKindsKind struct {
	Kind        string `json:"kind"`
	Description string `json:"description"`
	WakeDefault bool   `json:"wake_default"`
}

// parseCommEventKinds parses the arguments after "event-kinds". The only
// accepted argument is group=<g>; a blank value means no group filter.
func parseCommEventKinds(args []string) (string, error) {
	group := ""
	for _, a := range args {
		i := strings.IndexByte(a, '=')
		if i <= 0 {
			return "", fmt.Errorf("%q is not key=value\n%s", a, commEventKindsUsage)
		}
		key := strings.ToLower(strings.TrimSpace(a[:i]))
		if key != "group" {
			return "", fmt.Errorf("unknown key %q\n%s", key, commEventKindsUsage)
		}
		group = strings.TrimSpace(a[i+1:])
	}
	return group, nil
}

// filterCommEventKindsGroup keeps only the group named group, matched
// case-insensitively. A blank group keeps everything. An unknown group is an
// error that lists the groups the daemon does have.
func filterCommEventKindsGroup(resp commEventKindsBody, group string) (commEventKindsBody, error) {
	if group == "" {
		return resp, nil
	}
	var names []string
	for _, g := range resp.Groups {
		if strings.EqualFold(g.Group, group) {
			return commEventKindsBody{Groups: []commEventKindsGroup{g}}, nil
		}
		names = append(names, g.Group)
	}
	return resp, fmt.Errorf("unknown group %q (groups: %s)", group, strings.Join(names, ", "))
}

// handleAutonomousEventKinds implements "autonomous event-kinds".
func (r *Router) handleAutonomousEventKinds(args []string) {
	group, err := parseCommEventKinds(args)
	if err != nil {
		r.reply("autonomous event-kinds", err.Error())
		return
	}
	body, err := r.commGet(commEventKindsPath, url.Values{})
	if err != nil {
		r.reply("autonomous event-kinds failed", err.Error())
		return
	}
	text, err := renderCommEventKinds(body, group)
	if err != nil {
		r.reply("autonomous event-kinds failed", err.Error())
		return
	}
	r.reply("autonomous event-kinds", text)
}

// renderCommEventKinds renders the event-kinds REST body as text: a heading
// per group, then one line per kind. A kind that wakes the supervisor by
// default is marked with "*", explained in a footer line.
func renderCommEventKinds(body, group string) (string, error) {
	var resp commEventKindsBody
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		return "", fmt.Errorf("reading event kinds: %w", err)
	}
	resp, err := filterCommEventKindsGroup(resp, group)
	if err != nil {
		return "", err
	}
	if len(resp.Groups) == 0 {
		return "no event kinds", nil
	}
	var lines []string
	anyWake := false
	for i, g := range resp.Groups {
		if i > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, g.Group+":")
		for _, k := range g.Kinds {
			mark := " "
			if k.WakeDefault {
				mark = "*"
				anyWake = true
			}
			lines = append(lines, strings.TrimRight(fmt.Sprintf("  %s %s  %s", mark, k.Kind, k.Description), " "))
		}
	}
	if anyWake {
		lines = append(lines, "", "* wakes the supervisor by default")
	}
	return strings.Join(lines, "\n"), nil
}
