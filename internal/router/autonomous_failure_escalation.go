// Comm-channel verbs for failure escalation:
//
//	autonomous set-failure-escalation <automaton> [task] clear
//	autonomous set-failure-escalation <automaton> [task] [enabled=true|false] [max_failures=N] [steps=backend:model[@attempts],...]
//	autonomous review-task <automaton> <task> retry|cancel [backend=X] [model=Y] [effort=Z] [files=a,b] [spec=...] [note=...]
//
// Both verbs proxy to the REST routes under /api/autonomous/prds/{id}/ through
// the loopback, the same way the other autonomous verbs do. The daily global
// default is set with `configure autonomous.failure_escalation.<key>=<value>`,
// which goes through PUT /api/config.
package router

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	autonomouspkg "github.com/dmz006/datawatch/internal/autonomous"
)

const (
	setFailureEscalationUsage = "usage: autonomous set-failure-escalation <automaton> [task] clear | [enabled=true|false] [max_failures=N] [steps=backend:model[@attempts],...]"
	reviewTaskUsage           = "usage: autonomous review-task <automaton> <task> retry|cancel [backend=X] [model=Y] [effort=Z] [files=a,b] [spec=...] [note=...]"
)

// handleAutonomousSetFailureEscalation serves set-failure-escalation. args
// holds the words after the verb: the Automaton, an optional task, then either
// clear or key=value settings.
func (r *Router) handleAutonomousSetFailureEscalation(args []string) {
	const label = "autonomous set-failure-escalation"
	if len(args) == 0 {
		r.reply(label+" failed", setFailureEscalationUsage)
		return
	}
	prdID, rest := args[0], args[1:]
	taskID := ""
	if len(rest) > 0 && rest[0] != "clear" && !strings.Contains(rest[0], "=") {
		taskID, rest = rest[0], rest[1:]
	}
	body := map[string]any{"task_id": taskID, "actor": "operator"}
	switch {
	case len(rest) == 1 && rest[0] == "clear":
		body["policy"] = nil
	case len(rest) == 0:
		r.reply(label+" failed", setFailureEscalationUsage)
		return
	default:
		policy, err := failureEscalationPolicyFromWords(rest)
		if err != nil {
			r.reply(label+" failed", err.Error()+"\n"+setFailureEscalationUsage)
			return
		}
		body["policy"] = policy
	}
	raw, _ := json.Marshal(body)
	out, err := r.commJSON("POST", "/api/autonomous/prds/"+prdID+"/set_failure_escalation", string(raw))
	if err != nil {
		r.reply(label+" failed", err.Error())
		return
	}
	r.reply(label, prettyJSON(out))
}

// failureEscalationPolicyFromWords reads key=value settings into a policy. A
// setting that is not given inherits from the next level up.
func failureEscalationPolicyFromWords(words []string) (*autonomouspkg.FailureEscalationPolicy, error) {
	p := &autonomouspkg.FailureEscalationPolicy{}
	for _, w := range words {
		key, value, ok := strings.Cut(w, "=")
		if !ok {
			return nil, fmt.Errorf("%q needs the form key=value", w)
		}
		switch key {
		case "enabled":
			b, err := strconv.ParseBool(value)
			if err != nil {
				return nil, fmt.Errorf("enabled must be true or false (got %q)", value)
			}
			p.Enabled = &b
		case "max_failures":
			n, err := strconv.Atoi(value)
			if err != nil || n < 1 {
				return nil, fmt.Errorf("max_failures must be a whole number of 1 or more (got %q)", value)
			}
			p.MaxFailures = n
		case "steps":
			steps, err := autonomouspkg.ParseFailureEscalationSteps(value)
			if err != nil {
				return nil, err
			}
			p.Steps = steps
		default:
			return nil, fmt.Errorf("unknown key %q; valid keys: enabled, max_failures, steps", key)
		}
	}
	return p, nil
}

// handleAutonomousReviewTask serves review-task. args holds the words after the
// verb: the Automaton, the task, the action, then optional settings. A spec or
// note value may run over several words: the words that follow it, up to the
// next key=value word, belong to it.
func (r *Router) handleAutonomousReviewTask(args []string) {
	const label = "autonomous review-task"
	if len(args) < 3 {
		r.reply(label+" failed", reviewTaskUsage)
		return
	}
	action := strings.ToLower(args[2])
	if action != "retry" && action != "cancel" {
		r.reply(label+" failed", "action must be retry or cancel (got "+strconv.Quote(args[2])+")\n"+reviewTaskUsage)
		return
	}
	body := map[string]any{"task_id": args[1], "action": action, "actor": "operator"}
	textKey := ""
	for _, w := range args[3:] {
		key, value, ok := strings.Cut(w, "=")
		if !ok {
			if textKey == "" {
				r.reply(label+" failed", fmt.Sprintf("%q needs the form key=value\n%s", w, reviewTaskUsage))
				return
			}
			body[textKey] = body[textKey].(string) + " " + w
			continue
		}
		textKey = ""
		switch key {
		case "backend", "model", "effort":
			body[key] = value
		case "spec", "note":
			body[key] = value
			textKey = key
		case "files":
			body["files"] = splitReviewWords(value)
		default:
			r.reply(label+" failed", fmt.Sprintf("unknown key %q; valid keys: backend, model, effort, files, spec, note\n%s", key, reviewTaskUsage))
			return
		}
	}
	raw, _ := json.Marshal(body)
	out, err := r.commJSON("POST", "/api/autonomous/prds/"+args[0]+"/resolve_task_review", string(raw))
	if err != nil {
		r.reply(label+" failed", err.Error())
		return
	}
	r.reply(label, prettyJSON(out))
}

// replyHeldForReview follows the status reply with one line per task held for
// operator review, so the operator sees what needs a decision without reading
// the JSON. It sends nothing when no task is held.
func (r *Router) replyHeldForReview(status []byte) {
	var st struct {
		ReviewNeeded []struct {
			AutomatonID    string `json:"automaton_id"`
			AutomatonTitle string `json:"automaton_title"`
			TaskID         string `json:"task_id"`
			TaskTitle      string `json:"task_title"`
			FailureCount   int    `json:"failure_count"`
			LastBackend    string `json:"last_backend"`
			LastModel      string `json:"last_model"`
			HeldAt         string `json:"held_at"`
			HeldCause      string `json:"held_cause"`
		} `json:"review_needed"`
	}
	if json.Unmarshal(status, &st) != nil || len(st.ReviewNeeded) == 0 {
		return
	}
	lines := make([]string, 0, len(st.ReviewNeeded))
	for _, h := range st.ReviewNeeded {
		worker := h.LastBackend
		if h.LastModel != "" {
			worker += ":" + h.LastModel
		}
		line := fmt.Sprintf("%s %q task %s %q: %d failed attempts, last worker %s, held since %s",
			h.AutomatonID, h.AutomatonTitle, h.TaskID, h.TaskTitle, h.FailureCount, worker, h.HeldAt)
		if h.HeldCause != "" {
			line += "; held after infrastructure retries: " + h.HeldCause
		}
		lines = append(lines, line)
	}
	r.reply("autonomous held for review", strings.Join(lines, "\n")+"\nDecide with: autonomous review-task <automaton> <task> retry|cancel")
}

// splitReviewWords splits a comma-separated file list, dropping blanks.
func splitReviewWords(s string) []string {
	var out []string
	for _, f := range strings.Split(s, ",") {
		if f = strings.TrimSpace(f); f != "" {
			out = append(out, f)
		}
	}
	return out
}
