// autonomous supervisor loop over the chat channel: the supervisor loop
// reads and one manual job run, so the operator can ask from Signal /
// Telegram what the loop has run without curl. The reply renders the same
// bodies the GET routes serve (internal/server/supervisor_loop.go) through
// the same renderer the CLI uses (supervisorloop.RenderJobs /
// supervisorloop.RenderRuns), so both surfaces return the same lines. The
// jobs and runs verbs are reads — nothing is run or written. The run verb
// triggers one job run now (404 for an unknown job name). Supervisor loop
// settings are changed with the existing configure verb, for example
// "configure supervisor_loop.enabled=false".

package router

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/dmz006/datawatch/internal/supervisorloop"
)

const (
	// supervisorLoopUsage is the usage for the supervisor loop comm verbs.
	// jobs and runs take no positional arguments; the run verb takes its
	// options as key=value pairs.
	supervisorLoopUsage = "usage: supervisor jobs | supervisor stats | supervisor runs [job=NAME] [outcome=RESULT] [limit=N] | supervisor wake [status|set key=value ...] | supervisor run job=NAME [input=TEXT] [candidates=A,B]"
)

// SupervisorLoopCommVerbs lists the supervisor loop comm verbs for the
// parity test.
func SupervisorLoopCommVerbs() []string {
	return []string{"supervisor", "supervisor-loop", "supervisor_loop"}
}

// handleSupervisorLoopVerb answers the autonomous command's supervisor
// loop verbs: the jobs and runs reads, and one manual job run.
func (r *Router) handleSupervisorLoopVerb(verb string, args []string) {
	rest := args[1:]
	if len(rest) == 0 {
		r.reply("autonomous supervisor-loop failed", supervisorLoopUsage)
		return
	}
	switch rest[0] {
	case "jobs":
		r.replySupervisorLoopJobs(rest[1:])
	case "stats":
		r.replySupervisorLoopStats(rest[1:])
	case "runs":
		r.replySupervisorLoopRuns(rest[1:])
	case "run":
		r.replySupervisorLoopRun(rest[1:])
	case "wake":
		r.replySupervisorLoopWake(rest[1:])
	default:
		r.reply("autonomous supervisor-loop failed",
			"unknown sub-verb "+rest[0]+"\n"+supervisorLoopUsage)
	}
}

func (r *Router) replySupervisorLoopJobs(args []string) {
	if len(args) > 0 {
		r.reply("autonomous supervisor-loop failed",
			"unknown argument "+strings.Join(args, " ")+"\n"+supervisorLoopUsage)
		return
	}
	out, err := r.commGet("/api/supervisor-loop/jobs", nil)
	if err != nil {
		r.reply("autonomous supervisor-loop failed", err.Error())
		return
	}
	var body supervisorloop.JobsBody
	if err := json.Unmarshal([]byte(out), &body); err != nil {
		r.reply("autonomous supervisor-loop failed", "unparseable jobs body: "+err.Error())
		return
	}
	var b strings.Builder
	supervisorloop.RenderJobs(&b, &body)
	r.reply("autonomous supervisor-loop jobs", strings.TrimRight(b.String(), "\n"))
}

func (r *Router) replySupervisorLoopStats(args []string) {
	if len(args) > 0 {
		r.reply("autonomous supervisor-loop failed",
			"unknown argument "+strings.Join(args, " ")+"\n"+supervisorLoopUsage)
		return
	}
	out, err := r.commGet("/api/supervisor-loop/stats", nil)
	if err != nil {
		r.reply("autonomous supervisor-loop failed", err.Error())
		return
	}
	var st supervisorloop.Stats
	if err := json.Unmarshal([]byte(out), &st); err != nil {
		r.reply("autonomous supervisor-loop failed", "unparseable stats body: "+err.Error())
		return
	}
	var b strings.Builder
	supervisorloop.RenderStats(&b, &st)
	r.reply("autonomous supervisor-loop stats", strings.TrimRight(b.String(), "\n"))
}

func (r *Router) replySupervisorLoopRuns(args []string) {
	q := url.Values{}
	for _, a := range args {
		key, value, ok := strings.Cut(a, "=")
		if !ok {
			r.reply("autonomous supervisor-loop failed",
				"unknown argument "+a+"\n"+supervisorLoopUsage)
			return
		}
		switch key {
		case "job", "outcome", "limit":
			q.Set(key, strings.TrimSpace(value))
		default:
			r.reply("autonomous supervisor-loop failed",
				"unknown argument "+a+"\n"+supervisorLoopUsage)
			return
		}
	}
	out, err := r.commGet("/api/supervisor-loop/runs", q)
	if err != nil {
		r.reply("autonomous supervisor-loop failed", err.Error())
		return
	}
	var body supervisorloop.RunsBody
	if err := json.Unmarshal([]byte(out), &body); err != nil {
		r.reply("autonomous supervisor-loop failed", "unparseable runs body: "+err.Error())
		return
	}
	var b strings.Builder
	supervisorloop.RenderRuns(&b, &body)
	r.reply("autonomous supervisor-loop runs", strings.TrimRight(b.String(), "\n"))
}

func (r *Router) replySupervisorLoopRun(args []string) {
	name, input := "", ""
	var candidates []string
	for _, a := range args {
		key, value, ok := strings.Cut(a, "=")
		if !ok {
			r.reply("autonomous supervisor-loop failed",
				"unknown argument "+a+"\n"+supervisorLoopUsage)
			return
		}
		value = strings.TrimSpace(value)
		switch key {
		case "job":
			if name != "" {
				r.reply("autonomous supervisor-loop failed",
					"job= is given twice\n"+supervisorLoopUsage)
				return
			}
			name = value
		case "input":
			input = value
		case "candidates":
			for _, c := range strings.Split(value, ",") {
				if c = strings.TrimSpace(c); c != "" {
					candidates = append(candidates, c)
				}
			}
		default:
			r.reply("autonomous supervisor-loop failed",
				"unknown argument "+a+"\n"+supervisorLoopUsage)
			return
		}
	}
	if name == "" {
		r.reply("autonomous supervisor-loop failed",
			"run needs job=NAME\n"+supervisorLoopUsage)
		return
	}
	body := map[string]any{}
	if input != "" {
		body["input"] = input
	}
	if len(candidates) > 0 {
		body["candidates"] = candidates
	}
	jsonBody := ""
	if len(body) > 0 {
		b, _ := json.Marshal(body)
		jsonBody = string(b)
	}
	out, err := r.commJSON("POST", "/api/supervisor-loop/jobs/"+url.PathEscape(name)+"/run", jsonBody)
	if err != nil {
		r.reply("autonomous supervisor-loop run failed", err.Error())
		return
	}
	var parsed struct {
		Job    string `json:"job"`
		OK     bool   `json:"ok"`
		Result string `json:"result"`
	}
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		r.reply("autonomous supervisor-loop run", strings.TrimSpace(out))
		return
	}
	state := "ok"
	if !parsed.OK {
		state = "failed"
	}
	text := fmt.Sprintf("job %s: %s", parsed.Job, state)
	if parsed.Result != "" {
		text += "\n" + parsed.Result
	}
	r.reply("autonomous supervisor-loop run", text)
}

// supervisorWakeKeys maps the short setting names the wake verb accepts to
// the supervisor_loop.wake.* config keys.
var supervisorWakeKeys = map[string]string{
	"enabled":                   supervisorloop.KeyWakeEnabled,
	"kinds":                     supervisorloop.KeyWakeKinds,
	"coalesce_seconds":          supervisorloop.KeyWakeCoalesceSeconds,
	"fallback_interval_minutes": supervisorloop.KeyWakeFallbackIntervalMinutes,
}

// replySupervisorLoopWake answers "supervisor wake [status]" (a read) and
// "supervisor wake set key=value ..." (one config write, then the status).
func (r *Router) replySupervisorLoopWake(args []string) {
	const label = "autonomous supervisor-loop wake"
	fail := func(msg string) { r.reply("autonomous supervisor-loop failed", msg+"\n"+supervisorLoopUsage) }
	if len(args) > 0 && args[0] == "status" {
		args = args[1:]
		if len(args) > 0 {
			fail("unknown argument " + strings.Join(args, " "))
			return
		}
	} else if len(args) > 0 {
		if args[0] != "set" || len(args) == 1 {
			fail("wake takes status or set key=value")
			return
		}
		patch := map[string]any{}
		for _, a := range args[1:] {
			key, value, ok := strings.Cut(a, "=")
			if !ok {
				fail("unknown argument " + a)
				return
			}
			full, known := supervisorWakeKeys[strings.TrimPrefix(key, "supervisor_loop.wake.")]
			if !known {
				fail("unknown wake setting " + key)
				return
			}
			value = strings.TrimSpace(value)
			switch full {
			case supervisorloop.KeyWakeEnabled:
				b, err := strconv.ParseBool(value)
				if err != nil {
					fail("enabled must be true or false")
					return
				}
				patch[full] = b
			case supervisorloop.KeyWakeKinds:
				list := []string{}
				for _, k := range strings.Split(value, ",") {
					if k = strings.TrimSpace(k); k != "" {
						list = append(list, k)
					}
				}
				patch[full] = list
			default:
				n, err := strconv.Atoi(value)
				if err != nil {
					fail(key + " must be a whole number")
					return
				}
				patch[full] = n
			}
		}
		raw, _ := json.Marshal(patch)
		if _, err := r.commJSON("PUT", "/api/config", string(raw)); err != nil {
			r.reply("autonomous supervisor-loop wake set failed", err.Error())
			return
		}
	}
	out, err := r.commGet("/api/supervisor-loop/wake", nil)
	if err != nil {
		r.reply("autonomous supervisor-loop failed", err.Error())
		return
	}
	var w struct {
		Enabled                 bool     `json:"enabled"`
		Kinds                   []string `json:"kinds"`
		CoalesceSeconds         int      `json:"coalesce_seconds"`
		FallbackIntervalMinutes int      `json:"fallback_interval_minutes"`
		LastEvent               string   `json:"last_event"`
		LastScheduledCheck      string   `json:"last_scheduled_check"`
		EventCount              int      `json:"event_count"`
		ScheduledCount          int      `json:"scheduled_count"`
	}
	if err := json.Unmarshal([]byte(out), &w); err != nil {
		r.reply("autonomous supervisor-loop failed", "unparseable wake body: "+err.Error())
		return
	}
	never := func(s string) string {
		if s == "" || strings.HasPrefix(s, "0001-01-01") {
			return "never"
		}
		return s
	}
	state := "off"
	if w.Enabled {
		state = "on"
	}
	r.reply(label, fmt.Sprintf("wake: %s\nkinds: %s\ncoalesce: %ds\nfallback interval: %d min\nlast event: %s (%d events)\nlast scheduled check: %s (%d checks)",
		state, strings.Join(w.Kinds, ", "), w.CoalesceSeconds, w.FallbackIntervalMinutes,
		never(w.LastEvent), w.EventCount, never(w.LastScheduledCheck), w.ScheduledCount))
}
