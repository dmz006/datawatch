// llm_classifier.go — the classification judge by chat:
//
//	llm classifier status
//	llm classifier publishers
//	llm classifier add-publisher <name> confirm=true
//	llm classifier verdicts <automaton> [task=<id>] [low]
//	llm classifier review <automaton> <task> retry|cancel [note=<text>]
//
// Each verb proxies the REST routes under /api/llm/classifier through the
// loopback. review decides a low-confidence block: it first checks that the
// task really has one, then sends the decision to resolve_task_review, the same
// route the review-task verb uses.

package router

import (
	"encoding/json"
	"net/url"
	"strings"
)

const llmClassifierUsage = `usage:
  llm classifier status
  llm classifier publishers
  llm classifier add-publisher <name> confirm=true
  llm classifier verdicts <automaton> [task=<id>] [low]
  llm classifier review <automaton> <task> retry|cancel [note=<text>]`

func (r *Router) handleLLMClassifier(tail string) {
	const label = "llm classifier"
	words := strings.Fields(tail)
	if len(words) == 0 {
		r.reply(label, llmClassifierUsage)
		return
	}
	verb, args := strings.ToLower(words[0]), words[1:]
	switch verb {
	case "status":
		r.classifierGet(label+" status", "/api/llm/classifier", nil)
	case "publishers":
		r.classifierGet(label+" publishers", "/api/llm/classifier/publishers", nil)
	case "add-publisher", "add_publisher":
		r.classifierAddPublisher(args)
	case "verdicts":
		r.classifierVerdicts(args)
	case "review":
		r.classifierReview(args)
	default:
		r.reply(label, llmClassifierUsage)
	}
}

func (r *Router) classifierGet(label, path string, q url.Values) {
	out, err := r.commGet(path, q)
	if err != nil {
		r.reply(label+" failed", err.Error())
		return
	}
	r.reply(label, prettyJSON(out))
}

func (r *Router) classifierAddPublisher(args []string) {
	const label = "llm classifier add-publisher"
	if len(args) == 0 || strings.Contains(args[0], "=") {
		r.reply(label+" failed", "usage: llm classifier add-publisher <name> confirm=true")
		return
	}
	confirm := false
	for _, w := range args[1:] {
		if strings.EqualFold(w, "confirm=true") {
			confirm = true
		}
	}
	// Without confirm the REST route refuses and says why; the daemon owns the rule.
	body, _ := json.Marshal(map[string]any{"publisher": args[0], "confirm": confirm, "actor": "operator"})
	out, err := r.commJSON("POST", "/api/llm/classifier/publishers", string(body))
	if err != nil {
		r.reply(label+" failed", err.Error())
		return
	}
	r.reply(label, prettyJSON(out))
}

// classifierVerdictQuery builds the verdicts query from the Automaton and the
// optional task=<id> and low words.
func classifierVerdictQuery(automaton string, rest []string) url.Values {
	q := url.Values{}
	q.Set("automaton", automaton)
	for _, w := range rest {
		switch {
		case strings.HasPrefix(w, "task="):
			if t := strings.TrimPrefix(w, "task="); t != "" {
				q.Set("task", t)
			}
		case strings.EqualFold(w, "low"), strings.EqualFold(w, "low=true"), strings.EqualFold(w, "low-confidence"):
			q.Set("low_confidence", "true")
		}
	}
	return q
}

func (r *Router) classifierVerdicts(args []string) {
	const label = "llm classifier verdicts"
	if len(args) == 0 {
		r.reply(label+" failed", "usage: llm classifier verdicts <automaton> [task=<id>] [low]")
		return
	}
	r.classifierGet(label, "/api/llm/classifier/verdicts", classifierVerdictQuery(args[0], args[1:]))
}

func (r *Router) classifierReview(args []string) {
	const label = "llm classifier review"
	const usage = "usage: llm classifier review <automaton> <task> retry|cancel [note=<text>]"
	if len(args) < 3 {
		r.reply(label+" failed", usage)
		return
	}
	automaton, task, action := args[0], args[1], strings.ToLower(args[2])
	if action != "retry" && action != "cancel" {
		r.reply(label+" failed", "action must be retry or cancel\n"+usage)
		return
	}
	raw, err := r.commGet("/api/llm/classifier/verdicts", classifierVerdictQuery(automaton, []string{"task=" + task, "low"}))
	if err != nil {
		r.reply(label+" failed", err.Error())
		return
	}
	var list struct {
		Verdicts []struct {
			Scope   string `json:"scope"`
			Summary string `json:"summary"`
		} `json:"verdicts"`
	}
	if err := json.Unmarshal([]byte(raw), &list); err != nil {
		r.reply(label+" failed", "verdict list could not be read: "+err.Error())
		return
	}
	if len(list.Verdicts) == 0 {
		r.reply(label+" failed", "task "+task+" has no low-confidence block to review; use review-task for other held tasks")
		return
	}
	body := map[string]any{"task_id": task, "action": action, "actor": "operator"}
	if note := noteFromWords(args[3:]); note != "" {
		body["note"] = note
	}
	payload, _ := json.Marshal(body)
	out, err := r.commJSON("POST", "/api/autonomous/prds/"+automaton+"/resolve_task_review", string(payload))
	if err != nil {
		r.reply(label+" failed", err.Error())
		return
	}
	r.reply(label, prettyJSON(out))
}

// noteFromWords returns the text after note=, running over the words that
// follow it. Words before note= are ignored.
func noteFromWords(words []string) string {
	for i, w := range words {
		if strings.HasPrefix(w, "note=") {
			return strings.TrimSpace(strings.Join(append([]string{strings.TrimPrefix(w, "note=")}, words[i+1:]...), " "))
		}
	}
	return ""
}
