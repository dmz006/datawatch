// planning-routing over the chat channel: show the planning routing config,
// route a sample input with no LLM call, and read the counts-only scorecard.
// Each verb is a read over the REST loopback, so the answer is the same one
// the CLI, MCP and REST surfaces give. Dispatched from handleAutonomous.

package router

import (
	"encoding/json"
	"net/url"
	"strconv"
	"strings"

	"github.com/dmz006/datawatch/internal/autonomous"
)

const planningRoutingCommUsage = "usage: autonomous planning-routing show | " +
	"autonomous planning-routing test [kind=K] [tags=a,b] [spec_bytes=N] [files=a,b] [project=P] | " +
	"autonomous planning-routing scorecard [window]"

// PlanningRoutingCommVerbs lists the planning-routing comm verbs for the parity test.
func PlanningRoutingCommVerbs() []string { return []string{"planning-routing"} }

// handlePlanningRoutingVerb answers `autonomous planning-routing <sub> ...`.
// args[0] is the verb itself.
func (r *Router) handlePlanningRoutingVerb(args []string) {
	if len(args) < 2 {
		r.reply("autonomous planning-routing failed", planningRoutingCommUsage)
		return
	}
	switch strings.ToLower(args[1]) {
	case "show":
		r.replyPlanningRoutingShow()
	case "test":
		r.replyPlanningRoutingTest(args[2:])
	case "scorecard":
		r.replyPlanningRoutingScorecard(args[2:])
	default:
		r.reply("autonomous planning-routing failed", planningRoutingCommUsage)
	}
}

func (r *Router) replyPlanningRoutingShow() {
	out, err := r.commGet("/api/autonomous/planning-routing", nil)
	if err != nil {
		r.reply("autonomous planning-routing failed", err.Error())
		return
	}
	v, err := autonomous.ParsePlanningRoutingView([]byte(out))
	if err != nil {
		r.reply("autonomous planning-routing failed", err.Error())
		return
	}
	r.reply("autonomous planning-routing", autonomous.RenderPlanningRoutingView(v))
}

func (r *Router) replyPlanningRoutingTest(args []string) {
	body := map[string]any{"kind": "", "tags": []string{}, "spec_bytes": 0, "files": []string{}, "project": ""}
	for _, a := range args {
		k, v, ok := strings.Cut(a, "=")
		if !ok {
			r.reply("autonomous planning-routing failed", "expected key=value, got "+strconv.Quote(a)+"; "+planningRoutingCommUsage)
			return
		}
		switch k {
		case "kind", "project":
			body[k] = v
		case "tags", "files":
			list := []string{}
			for _, p := range strings.Split(v, ",") {
				if p = strings.TrimSpace(p); p != "" {
					list = append(list, p)
				}
			}
			body[k] = list
		case "spec_bytes":
			n, err := strconv.Atoi(v)
			if err != nil || n < 0 {
				r.reply("autonomous planning-routing failed", "spec_bytes must be a whole number of 0 or more")
				return
			}
			body[k] = n
		default:
			r.reply("autonomous planning-routing failed", "unknown key "+strconv.Quote(k)+"; "+planningRoutingCommUsage)
			return
		}
	}
	raw, _ := json.Marshal(body)
	out, err := r.commJSON("POST", "/api/autonomous/planning-routing/test", string(raw))
	if err != nil {
		r.reply("autonomous planning-routing failed", err.Error())
		return
	}
	d, err := autonomous.ParsePlanningRouteDecisionView([]byte(out))
	if err != nil {
		r.reply("autonomous planning-routing failed", err.Error())
		return
	}
	r.reply("autonomous planning-routing test", autonomous.RenderPlanningRouteDecisionView(d))
}

func (r *Router) replyPlanningRoutingScorecard(args []string) {
	if len(args) > 1 {
		r.reply("autonomous planning-routing failed", planningRoutingCommUsage)
		return
	}
	q := url.Values{}
	if len(args) == 1 && strings.TrimSpace(args[0]) != "" {
		q.Set("window", strings.TrimSpace(args[0]))
	}
	out, err := r.commGet("/api/autonomous/planning-routing/scorecard", q)
	if err != nil {
		r.reply("autonomous planning-routing failed", err.Error())
		return
	}
	report, err := autonomous.ParsePlanningScorecardReport([]byte(out))
	if err != nil {
		r.reply("autonomous planning-routing failed", err.Error())
		return
	}
	r.reply("autonomous planning-routing scorecard", report)
}
