// autonomous request-watch over the chat channel: the model-request watch
// read, so the operator can ask from Signal / Telegram whether a worker or
// verifier session that is waiting on a model request is generating, idle or
// on an unreachable node, and for how long it has shown no progress — without
// curl. It reads the same status GET /api/autonomous/request-watch serves.
// A read: it makes no model call and ends, retries or unloads nothing.

package router

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/dmz006/datawatch/internal/requestwatch"
)

// requestWatchUsage is the usage line for the request-watch comm verb. The
// optional pair narrows the read to one session or one Automaton.
const requestWatchUsage = "usage: autonomous request-watch [session <id> | automaton <id>]"

// requestWatchChatLines is the most observation lines one chat reply carries.
const requestWatchChatLines = 20

// RequestWatchCommVerbs lists the request-watch comm verbs for the parity
// test.
func RequestWatchCommVerbs() []string { return []string{"request-watch"} }

// handleRequestWatchVerb answers the autonomous command's request-watch verb.
func (r *Router) handleRequestWatchVerb(verb string, args []string) {
	switch verb {
	case "request-watch", "request_watch":
		r.replyRequestWatch(args)
	default:
		r.reply("autonomous request-watch failed", requestWatchUsage)
	}
}

// replyRequestWatch reads the request-watch status and replies with one line
// per observation. args[0] is the verb; then nothing, or "session <id>" or
// "automaton <id>".
func (r *Router) replyRequestWatch(args []string) {
	const label = "autonomous request-watch"
	q := url.Values{}
	switch {
	case len(args) == 1:
	case len(args) == 3 && strings.TrimSpace(args[2]) != "":
		switch strings.ToLower(args[1]) {
		case "session":
			q.Set("session", strings.TrimSpace(args[2]))
		case "automaton":
			q.Set("automaton", strings.TrimSpace(args[2]))
		default:
			r.reply(label+" failed", requestWatchUsage)
			return
		}
	default:
		r.reply(label+" failed", requestWatchUsage)
		return
	}
	out, err := r.commGet("/api/autonomous/request-watch", q)
	if err != nil {
		r.reply(label+" failed", err.Error())
		return
	}
	var st requestwatch.Status
	if err := json.Unmarshal([]byte(out), &st); err != nil {
		r.reply(label+" failed", "unreadable reply: "+err.Error())
		return
	}
	r.reply(label, renderRequestWatchForChat(st))
}

// renderRequestWatchForChat renders the status as plain text: one line per
// observation naming the session, its role and task, the node and the
// summary, or one sentence when nothing is watched or the feature is off.
func renderRequestWatchForChat(st requestwatch.Status) string {
	if !st.Enabled {
		return "Model request watching is off for Automaton workers and verifiers (autonomous.request_watch.enabled is false)."
	}
	if len(st.Observations) == 0 {
		return "No model requests are being watched for Automaton workers or verifiers."
	}
	var b strings.Builder
	for i, o := range st.Observations {
		if i == requestWatchChatLines {
			fmt.Fprintf(&b, "... and %d more", len(st.Observations)-i)
			break
		}
		task, node, summary := o.TaskID, o.Node, o.Summary
		if task == "" {
			task = "-"
		}
		if node == "" {
			node = "-"
		}
		if summary == "" {
			summary = requestwatch.Summary(o)
		}
		fmt.Fprintf(&b, "%s %s task %s on %s: %s\n", o.SessionID, o.Role, task, node, summary)
	}
	return strings.TrimRight(b.String(), "\n")
}
