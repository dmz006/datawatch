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
//	progress [automation=<id>] [tag=<id> ...] [tag_match=<any|all>] [since=<YYYY-MM-DD>] [until=<YYYY-MM-DD>]
//	progress trend [days] [tag=<id> ...] [tag_match=<any|all>]
//
// Both read the REST routes over the loopback and print the text the CLI
// prints, so the two cannot diverge.

const progressUsage = "usage: progress [automation=<id>] [tag=<id> ...] [tag_match=<any|all>] [since=<YYYY-MM-DD>] [until=<YYYY-MM-DD>]\nusage: progress trend [days] [tag=<id> ...] [tag_match=<any|all>]"

// progressQueryKeys maps a comm argument to its REST query parameter. tag is
// repeatable; the rest are single values.
var progressQueryKeys = map[string]string{
	"automation": "automation_id",
	"tag":        "tag",
	"tag_match":  "tag_match",
	"since":      "since",
	"until":      "until",
}

// ProgressCommVerbs lists the verbs handled by handleProgress. Keep it in step
// with that function.
func ProgressCommVerbs() []string {
	return []string{"progress", "progress trend"}
}

// progressQuery turns "automation=ID tag=ID since=DATE" into REST query
// values. tag is repeatable; the other arguments keep their last value.
func progressQuery(fields []string) (url.Values, error) {
	q := url.Values{}
	for _, f := range fields {
		k, v, ok := strings.Cut(f, "=")
		key, known := progressQueryKeys[strings.ToLower(k)]
		if !ok || !known || v == "" {
			return nil, fmt.Errorf("unknown argument %q", f)
		}
		if key == "tag" {
			q.Add(key, v)
			continue
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
	for _, f := range fields {
		k, v, isKV := strings.Cut(f, "=")
		if !isKV {
			if q.Get("days") != "" {
				r.send(fmt.Sprintf("[%s] %s", r.hostname, progressUsage))
				return
			}
			if _, err := strconv.Atoi(f); err != nil {
				r.send(fmt.Sprintf("[%s] days must be a whole number\n%s", r.hostname, progressUsage))
				return
			}
			q.Set("days", f)
			continue
		}
		key := strings.ToLower(k)
		switch key {
		case "tag":
			if v == "" {
				r.send(fmt.Sprintf("[%s] tag needs an id\n%s", r.hostname, progressUsage))
				return
			}
			q.Add(key, v)
		case "tag_match":
			if v != "any" && v != "all" {
				r.send(fmt.Sprintf("[%s] tag_match must be any or all\n%s", r.hostname, progressUsage))
				return
			}
			q.Set(key, v)
		default:
			r.send(fmt.Sprintf("[%s] %s", r.hostname, progressUsage))
			return
		}
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
	r.reply("Progress trend:", strings.TrimRight(autonomous.RenderTrendReply(t.Days, t.Scope, t.Direction, t.Points), "\n"))
}
