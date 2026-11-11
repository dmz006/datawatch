package router

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/dmz006/datawatch/internal/autonomous"
)

// Comm verb for the Automata timeline:
//
//	timeline [project=<dir>] [automaton=<id>] [stage=<id>] [kind=<kind>] [tag=<id>]
//
// It reads the REST route over the loopback and prints the short summary the
// CLI's text renderer produces, so the two cannot diverge.

const timelineUsage = "usage: timeline [project=<dir>] [automaton=<id>] [stage=<id>] [kind=<kind>] [tag=<id>]"

// timelineQueryKeys maps a comm argument to its REST query parameter.
var timelineQueryKeys = map[string]string{
	"project":   "project",
	"automaton": "automaton",
	"stage":     "stage",
	"kind":      "kind",
	"tag":       "tag",
}

// TimelineCommVerbs lists the verbs handled by handleTimeline. Keep it in step
// with that function.
func TimelineCommVerbs() []string {
	return []string{"timeline"}
}

func (r *Router) handleTimeline(args string) {
	fields := strings.Fields(args)
	q := url.Values{}
	for _, f := range fields {
		k, v, ok := strings.Cut(f, "=")
		key, known := timelineQueryKeys[strings.ToLower(k)]
		if !ok || !known || v == "" {
			r.send(fmt.Sprintf("[%s] unknown argument %q\n%s", r.hostname, f, timelineUsage))
			return
		}
		q.Set(key, v)
	}
	body, err := r.commGet("/api/autonomous/timeline", q)
	if err != nil {
		r.reply("timeline failed", err.Error())
		return
	}
	res, err := autonomous.ParseTimeline([]byte(body))
	if err != nil {
		r.reply("timeline failed", err.Error())
		return
	}
	r.reply("Timeline:", strings.TrimRight(autonomous.RenderTimelineSummary(res), "\n"))
}
