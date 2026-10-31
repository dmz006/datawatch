package router

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// Model routing comm verbs:
//
//	compute node models [name] [model=<m>] [backend=<b>] [refresh]
//	autonomous set-task-node <prd-id> <task-id> <node|clear>
//	autonomous set-story-node <prd-id> <story-id> <node|clear>
//
// The routing behaviour itself is set with
// `configure autonomous.model_routing.<key>=<value>`.

const computeNodeModelsUsage = "usage: compute node models [name] [model=<m>] [backend=<b>] [refresh]"

const setTaskNodeUsage = "usage: autonomous set-task-node <automaton-id> <task-id> <node|clear>"

const setStoryNodeUsage = "usage: autonomous set-story-node <automaton-id> <story-id> <node|clear>"

// ModelRoutingCommVerbs lists the model routing comm verbs, for the parity test.
func ModelRoutingCommVerbs() []string {
	return []string{"compute node models", "set-task-node", "set-story-node"}
}

// servedModelsReply is the part of GET /api/compute/served-models the chat
// reply uses.
type servedModelsReply struct {
	CacheSeconds int    `json:"cache_seconds"`
	RefusalMode  string `json:"refusal_mode"`
	Nodes        []struct {
		Node       string   `json:"node"`
		Kind       string   `json:"kind"`
		Models     []string `json:"models"`
		Known      bool     `json:"known"`
		Cached     bool     `json:"cached"`
		AgeSeconds int      `json:"age_seconds"`
		Error      string   `json:"error"`
		Disabled   bool     `json:"disabled"`
	} `json:"nodes"`
	Check *struct {
		Model       string   `json:"model"`
		Applicable  bool     `json:"applicable"`
		Refused     bool     `json:"refused"`
		NodeUnknown bool     `json:"node_unknown"`
		ServedBy    []string `json:"served_by"`
		Message     string   `json:"message"`
	} `json:"check"`
}

// replyComputeNodeModels handles `compute node models`. tail is everything
// after the word models: an optional node name, model=<m>, backend=<b> and the
// word refresh, in any order.
func (r *Router) replyComputeNodeModels(tail string) {
	const label = "compute node models"
	q := url.Values{}
	node := ""
	for _, w := range strings.Fields(tail) {
		lower := strings.ToLower(w)
		switch {
		case lower == "refresh" || lower == "refresh=true":
			q.Set("refresh", "true")
		case strings.HasPrefix(lower, "model="):
			if v := w[len("model="):]; v != "" {
				q.Set("model", v)
			}
		case strings.HasPrefix(lower, "backend="):
			if v := w[len("backend="):]; v != "" {
				q.Set("backend", v)
			}
		case strings.Contains(w, "=") || node != "":
			r.reply(label+" failed", computeNodeModelsUsage)
			return
		default:
			node = w
		}
	}
	if node != "" {
		q.Set("node", node)
	}
	out, err := r.commGet("/api/compute/served-models", q)
	if err != nil {
		r.reply(label+" failed", err.Error())
		return
	}
	var resp servedModelsReply
	if err := json.Unmarshal([]byte(out), &resp); err != nil {
		r.reply(label+" failed", "the daemon sent a reply that is not understood: "+err.Error())
		return
	}
	r.reply(label, renderServedModelsForChat(resp))
}

// renderServedModelsForChat renders one short block per node, then the model
// check when one was asked for.
func renderServedModelsForChat(resp servedModelsReply) string {
	var b strings.Builder
	if len(resp.Nodes) == 0 {
		b.WriteString("No compute nodes are registered.\n")
	}
	for _, n := range resp.Nodes {
		b.WriteString(n.Node)
		if n.Kind != "" {
			b.WriteString(" (" + n.Kind + ")")
		}
		b.WriteString("\n")
		switch {
		case n.Disabled:
			b.WriteString("  disabled, not read\n")
		case !n.Known:
			b.WriteString("  models unknown")
			if n.Error != "" {
				b.WriteString(": " + n.Error)
			}
			b.WriteString("\n")
		default:
			age := "fresh"
			if n.Cached {
				age = "kept copy, " + formatAgeSeconds(n.AgeSeconds) + " old"
			}
			b.WriteString("  " + pluralModels(len(n.Models)) + ", " + age + "\n")
			if len(n.Models) > 0 {
				b.WriteString("  " + strings.Join(n.Models, ", ") + "\n")
			}
		}
	}
	if c := resp.Check; c != nil {
		b.WriteString("Check for model " + c.Model + ": ")
		switch {
		case !c.Applicable:
			b.WriteString("does not apply")
		case c.Refused:
			b.WriteString("would be refused")
		case c.NodeUnknown:
			b.WriteString("accepted, but a node's models could not be read")
		default:
			b.WriteString("accepted")
		}
		if len(c.ServedBy) > 0 {
			b.WriteString("; served by " + strings.Join(c.ServedBy, ", "))
		}
		b.WriteString("\n")
		if c.Message != "" {
			b.WriteString(c.Message + "\n")
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

func pluralModels(n int) string {
	if n == 1 {
		return "1 model"
	}
	return strconv.Itoa(n) + " models"
}

// formatAgeSeconds renders an age in seconds as 45s, 12m or 3h.
func formatAgeSeconds(s int) string {
	switch {
	case s < 60:
		return strconv.Itoa(s) + "s"
	case s < 3600:
		return strconv.Itoa(s/60) + "m"
	default:
		return strconv.Itoa(s/3600) + "h"
	}
}

// replySetNodePin handles `autonomous set-task-node` and `autonomous set-story-node`.
// args[0] is the verb, then the Automaton ID, the task or story ID and the node
// name, or the word clear.
func (r *Router) replySetNodePin(args []string, story bool) {
	unit, idKey, route, usage := "task", "task_id", "set_task_node", setTaskNodeUsage
	if story {
		unit, idKey, route, usage = "story", "story_id", "set_story_node", setStoryNodeUsage
	}
	label := "autonomous set-" + unit + "-node"
	if len(args) != 4 {
		r.reply(label+" failed", usage)
		return
	}
	prdID, unitID, node := args[1], args[2], args[3]
	if strings.EqualFold(node, "clear") {
		node = ""
	}
	raw, _ := json.Marshal(map[string]string{idKey: unitID, "node": node, "actor": "operator"})
	if _, err := r.commJSON(http.MethodPost, "/api/autonomous/prds/"+url.PathEscape(prdID)+"/"+route, string(raw)); err != nil {
		r.reply(label+" failed", err.Error())
		return
	}
	if node == "" {
		r.reply(label, "Cleared the node pin on "+unit+" "+unitID+" of Automaton "+prdID+". It goes back to normal node choice.")
		return
	}
	r.reply(label, "Pinned "+unit+" "+unitID+" of Automaton "+prdID+" to node "+node+".")
}
