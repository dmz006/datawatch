package router

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/dmz006/datawatch/internal/autonomous"
)

// Comm verbs for the progress metrics:
//
//	progress [automation=<id>] [tag=<id>] [since=<YYYY-MM-DD>] [until=<YYYY-MM-DD>]
//	progress trend [days]
//
// Both read the REST routes over the loopback and print the text the CLI
// prints, so the two cannot diverge.

const progressUsage = "usage: progress [automation=<id>] [tag=<id>] [since=<YYYY-MM-DD>] [until=<YYYY-MM-DD>] | progress trend [days]"

// progressQueryKeys maps a comm argument to its REST query parameter.
var progressQueryKeys = map[string]string{
	"automation": "automation_id",
	"tag":        "tag_id",
	"since":      "since",
	"until":      "until",
}

// ProgressCommVerbs lists the verbs handled by handleProgress. Keep it in step
// with that function.
func ProgressCommVerbs() []string {
	return []string{"progress", "progress trend"}
}

// progressQuery turns "automation=ID tag=ID since=DATE" into REST query values.
func progressQuery(fields []string) (url.Values, error) {
	q := url.Values{}
	for _, f := range fields {
		k, v, ok := strings.Cut(f, "=")
		key, known := progressQueryKeys[strings.ToLower(k)]
		if !ok || !known || v == "" {
			return nil, fmt.Errorf("unknown argument %q", f)
		}
		q.Set(key, v)
	}
	return q, nil
}

func (r *Router) handleProgress(args string) {
	fields := strings.Fields(args)
	if len(fields) > 0 && strings.EqualFold(fields[0], "trend") {
		r.handleProgressTrend(fields[1:])
		return
	}
	q, err := progressQuery(fields)
	if err != nil {
		r.send(fmt.Sprintf("[%s] %s\n%s", r.hostname, err, progressUsage))
		return
	}
	body, err := r.commGet("/api/autonomous/progress", q)
	if err != nil {
		r.reply("progress failed", err.Error())
		return
	}
	m, err := autonomous.ParseProgressMetrics([]byte(body))
	if err != nil {
		r.reply("progress failed", err.Error())
		return
	}
	r.reply("Progress:", strings.TrimRight(autonomous.RenderProgressText(m), "\n"))
}

func (r *Router) handleProgressTrend(fields []string) {
	q := url.Values{}
	if len(fields) > 1 {
		r.send(fmt.Sprintf("[%s] %s", r.hostname, progressUsage))
		return
	}
	if len(fields) == 1 {
		if _, err := strconv.Atoi(fields[0]); err != nil {
			r.send(fmt.Sprintf("[%s] days must be a whole number\n%s", r.hostname, progressUsage))
			return
		}
		q.Set("days", fields[0])
	}
	body, err := r.commGet("/api/autonomous/progress/trend", q)
	if err != nil {
		r.reply("progress trend failed", err.Error())
		return
	}
	t, err := autonomous.ParseProgressTrend([]byte(body))
	if err != nil {
		r.reply("progress trend failed", err.Error())
		return
	}
	r.reply("Progress trend:", strings.TrimRight(autonomous.RenderTrendReply(t.Days, t.Direction, t.Points), "\n"))
}
