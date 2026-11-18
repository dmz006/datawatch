// task-routing over the chat channel: show the routing table, list and
// switch profiles, see which row decided each task's LLM in an Automaton and
// run the no-write dry run. Each verb goes over the REST loopback, so the
// answer is the same one the CLI, MCP and REST surfaces give. Replies are
// kept short for a chat channel. Dispatched from handleAutonomous.
//
// Only `profile use` writes. Like every other autonomous verb it reaches the
// daemon through the authenticated loopback, so it is allowed exactly when
// the operator may send chat commands to this daemon; no new gate is added.

package router

import (
	"encoding/json"
	"net/url"
	"strings"

	"github.com/dmz006/datawatch/internal/autonomous"
)

const taskRoutingCommUsage = "usage: autonomous task-routing show | " +
	"autonomous task-routing profile list | " +
	"autonomous task-routing profile use <name> | " +
	"autonomous task-routing automaton <id> | " +
	"autonomous task-routing dry-run <id> [profile]"

const taskRoutingCommAPI = "/api/autonomous/task-routing"

// TaskRoutingCommVerbs lists the task-routing comm verbs for the parity test.
func TaskRoutingCommVerbs() []string { return []string{"task-routing"} }

// handleTaskRoutingVerb answers `autonomous task-routing <sub> ...`.
// args[0] is the verb itself.
func (r *Router) handleTaskRoutingVerb(args []string) {
	fail := func(msg string) { r.reply("autonomous task-routing failed", msg) }
	if len(args) < 2 {
		fail(taskRoutingCommUsage)
		return
	}
	rest := args[2:]
	switch strings.ToLower(args[1]) {
	case "show":
		if len(rest) != 0 {
			fail(taskRoutingCommUsage)
			return
		}
		r.replyTaskRouting("autonomous task-routing", "GET", taskRoutingCommAPI, "",
			func(b []byte) (string, error) { return autonomous.RenderTaskRoutingTable(b, false) })
	case "profile":
		r.handleTaskRoutingProfile(rest)
	case "automaton":
		if len(rest) != 1 {
			fail(taskRoutingCommUsage)
			return
		}
		r.replyTaskRouting("autonomous task-routing automaton", "GET",
			"/api/autonomous/prds/"+url.PathEscape(rest[0])+"/task-routing", "",
			func(b []byte) (string, error) { return autonomous.RenderTaskRoutingAutomaton(b, false) })
	case "dry-run", "dry_run":
		if len(rest) < 1 || len(rest) > 2 {
			fail(taskRoutingCommUsage)
			return
		}
		body := map[string]string{"profile": ""}
		if len(rest) == 2 {
			body["profile"] = rest[1]
		}
		raw, _ := json.Marshal(body)
		r.replyTaskRouting("autonomous task-routing dry-run", "POST",
			"/api/autonomous/prds/"+url.PathEscape(rest[0])+"/task-routing/dry-run", string(raw),
			func(b []byte) (string, error) { return autonomous.RenderTaskRoutingDryRun(b, false) })
	default:
		fail(taskRoutingCommUsage)
	}
}

func (r *Router) handleTaskRoutingProfile(args []string) {
	if len(args) == 0 {
		r.reply("autonomous task-routing failed", taskRoutingCommUsage)
		return
	}
	switch strings.ToLower(args[0]) {
	case "list":
		if len(args) != 1 {
			r.reply("autonomous task-routing failed", taskRoutingCommUsage)
			return
		}
		r.replyTaskRouting("autonomous task-routing profiles", "GET", taskRoutingCommAPI+"/profiles", "",
			autonomous.RenderTaskRoutingProfiles)
	case "use":
		if len(args) != 2 {
			r.reply("autonomous task-routing failed", taskRoutingCommUsage)
			return
		}
		name := args[1]
		raw, _ := json.Marshal(map[string]string{"name": name})
		r.replyTaskRouting("autonomous task-routing profile use", "PUT", taskRoutingCommAPI+"/active-profile", string(raw),
			func([]byte) (string, error) {
				return "Active profile is now " + name + ". Tasks created from now on use it; running work is unchanged.", nil
			})
	default:
		r.reply("autonomous task-routing failed", taskRoutingCommUsage)
	}
}

// replyTaskRouting sends one loopback request and replies with render(body).
func (r *Router) replyTaskRouting(title, method, path, body string, render func([]byte) (string, error)) {
	var (
		out string
		err error
	)
	if method == "GET" {
		out, err = r.commGet(path, nil)
	} else {
		out, err = r.commJSON(method, path, body)
	}
	if err != nil {
		r.reply("autonomous task-routing failed", err.Error())
		return
	}
	text, err := render([]byte(out))
	if err != nil {
		r.reply("autonomous task-routing failed", err.Error())
		return
	}
	r.reply(title, text)
}
