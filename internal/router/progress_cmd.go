package router

import (
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/dmz006/datawatch/internal/autonomous"
)

// Comm verbs for the progress metrics:
//
//	progress [automation=<id>] [project=<id>] [automaton=<id>] [group=<project|automation|automaton>] [tag=<id> ...] [tag_match=<any|all>] [since=<YYYY-MM-DD>] [until=<YYYY-MM-DD>]
//	progress trend [days] [tag=<id> ...] [tag_match=<any|all>]
//
// Both read the REST routes over the loopback and print the text the CLI
// prints, so the two cannot diverge.

const progressUsage = "usage: progress [automation=<id>] [project=<id>] [automaton=<id>] [group=<project|automation|automaton>] [tag=<id> ...] [tag_match=<any|all>] [since=<YYYY-MM-DD>] [until=<YYYY-MM-DD>]\nusage: progress trend [days] [tag=<id> ...] [tag_match=<any|all>]"

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

// progressGroupArgs holds the grouping arguments, which the comm command
// handles itself: group picks the axis, project and automaton keep one row.
type progressGroupArgs struct {
	group, project, automaton string
}

// splitProgressGroupArgs removes group=, project= and automaton= from fields
// and returns the rest. project or automaton alone imply their axis; asking for
// a different axis as well is an error. (none) is a valid project or Automation
// key, shown for records that have none.
func splitProgressGroupArgs(fields []string) ([]string, progressGroupArgs, error) {
	var ga progressGroupArgs
	var rest []string
	for _, f := range fields {
		k, v, ok := strings.Cut(f, "=")
		switch strings.ToLower(k) {
		case "group", "project", "automaton":
			if !ok || v == "" {
				return nil, ga, fmt.Errorf("unknown argument %q", f)
			}
		default:
			rest = append(rest, f)
			continue
		}
		switch strings.ToLower(k) {
		case "group":
			g := strings.ToLower(v)
			if g != "project" && g != "automation" && g != "automaton" {
				return nil, ga, fmt.Errorf("group must be project, automation or automaton")
			}
			ga.group = g
		case "project":
			ga.project = v
		case "automaton":
			ga.automaton = v
		}
	}
	implied := ""
	if ga.project != "" {
		implied = "project"
	}
	if ga.automaton != "" {
		if implied != "" {
			return nil, ga, fmt.Errorf("give project or automaton, not both")
		}
		implied = "automaton"
	}
	if implied != "" {
		if ga.group != "" && ga.group != implied {
			return nil, ga, fmt.Errorf("group=%s does not fit %s=", ga.group, implied)
		}
		ga.group = implied
	}
	return rest, ga, nil
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
	fields, ga, err := splitProgressGroupArgs(fields)
	var q url.Values
	if err == nil {
		q, err = progressQuery(fields)
	}
	if err != nil {
		r.send(fmt.Sprintf("[%s] %s\n%s", r.hostname, err, progressUsage))
		return
	}
	if ga.group != "" {
		q.Set("group", ga.group)
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
	text := strings.TrimRight(autonomous.RenderProgressText(m), "\n")
	if ga.group != "" {
		var g struct {
			GroupTotals map[string]autonomous.ProgressGroup `json:"group_totals"`
		}
		if err := json.Unmarshal([]byte(body), &g); err != nil {
			r.reply("progress failed", err.Error())
			return
		}
		key := ga.project
		if ga.automaton != "" {
			key = ga.automaton
		}
		text += "\n" + strings.TrimRight(renderProgressGroupText(ga.group, g.GroupTotals, key, m.Completion), "\n")
	}
	r.reply("Progress:", text)
}

// renderProgressGroupText prints one row per group key, sorted by name with the
// (none) row last, then the sample size the completion estimate rests on. key,
// when set, keeps only that row.
func renderProgressGroupText(group string, totals map[string]autonomous.ProgressGroup, key string, c autonomous.CompletionRange) string {
	var sb strings.Builder
	label := map[string]string{"project": "Project", "automation": "Automation", "automaton": "Automaton"}[group]
	fmt.Fprintf(&sb, "\nTotals by %s:\n", group)
	keys := make([]string, 0, len(totals))
	for k := range totals {
		if key != "" && k != key {
			continue
		}
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		ni, nj := keys[i] == autonomous.ProgressKeyNone, keys[j] == autonomous.ProgressKeyNone
		if ni != nj {
			return nj
		}
		return keys[i] < keys[j]
	})
	if len(keys) == 0 {
		if key != "" {
			fmt.Fprintf(&sb, "no Automata for %s %q\n", group, key)
		} else {
			sb.WriteString("no Automata counted\n")
		}
	} else {
		fmt.Fprintf(&sb, "%-30s %9s %5s %5s %6s %7s | %6s %6s %5s\n", label, "Automata", "done", "run", "wait", "blocked", "tasks", "done", "left")
		for _, k := range keys {
			g := totals[k]
			fmt.Fprintf(&sb, "%-30s %9d %5d %5d %6d %7d | %6d %6d %5d\n", k, g.PRDs, g.Done, g.Running, g.Waiting, g.Blocked, g.Tasks, g.TasksDone, g.TasksLeft)
		}
	}
	an, tn := "Automata", "tasks"
	if c.SampleRecords == 1 {
		an = "Automaton"
	}
	if c.SampleTasks == 1 {
		tn = "task"
	}
	fmt.Fprintf(&sb, "estimate sample: %d finished %s, %d completed %s\n", c.SampleRecords, an, c.SampleTasks, tn)
	return sb.String()
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
