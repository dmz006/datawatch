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
	"strings"

	"github.com/dmz006/datawatch/internal/supervisorloop"
)

const (
	// supervisorLoopUsage is the usage for the supervisor loop comm verbs.
	// jobs and runs take no positional arguments; the run verb takes its
	// options as key=value pairs.
	supervisorLoopUsage = "usage: supervisor jobs | supervisor stats | supervisor runs [job=NAME] [outcome=RESULT] [limit=N] | supervisor run job=NAME [input=TEXT] [candidates=A,B]"
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
