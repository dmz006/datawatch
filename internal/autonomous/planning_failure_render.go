// planning_failure_render.go — plain-text renderings of the planning failure
// view for the CLI and the chat verbs, read from the REST body so every
// surface shows the same words. Pure text work; nothing here touches a session.
package autonomous

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// planningFailureOfBody decodes the planning_failure object of an Automaton body,
// or nil when the body carries none.
func planningFailureOfBody(raw json.RawMessage) *PlanningFailure {
	var v struct {
		PF *PlanningFailure `json:"planning_failure"`
	}
	if json.Unmarshal(raw, &v) != nil {
		return nil
	}
	return v.PF
}

func retryCountsText(counts map[string]int) string {
	keys := make([]string, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%d", k, counts[k]))
	}
	return strings.Join(parts, ", ")
}

// PlanningFailureOneLine is the one-line form: the reason and the phrases.
func PlanningFailureOneLine(pf *PlanningFailure) string {
	if pf == nil {
		return ""
	}
	line := "Planning failed: " + pf.Reason
	if len(pf.Phrases) > 0 {
		line += " (phrases: " + strings.Join(pf.Phrases, "; ") + ")"
	}
	return line
}

// PlanningFailureLines renders the "Planning failed" section of an Automaton
// body (get): reason, category, rule, phrases and retry counts. It returns nil
// when the body carries no planning failure.
func PlanningFailureLines(body []byte) []string {
	pf := planningFailureOfBody(body)
	if pf == nil {
		return nil
	}
	lines := []string{
		"Planning failed",
		"  reason:   " + pf.Reason,
		"  category: " + pf.Category,
		"  rule:     " + pf.Source,
	}
	if len(pf.Phrases) > 0 {
		lines = append(lines, "  phrases:  "+strings.Join(pf.Phrases, "; "))
	}
	retry := fmt.Sprintf("  retries:  %d of %d", pf.RetryCounts[pf.Category], pf.RetryBudget)
	if len(pf.RetryCounts) > 0 {
		retry += " (" + retryCountsText(pf.RetryCounts) + ")"
	}
	if pf.Retryable {
		retry += ", retryable"
	} else {
		retry += ", not retryable"
	}
	return append(lines, retry)
}

// PlanningFailureListLines renders one line per Automaton of a list body that
// has a planning failure: "<id> <title>: Planning failed: ...".
func PlanningFailureListLines(body []byte) []string {
	var v struct {
		PRDs []json.RawMessage `json:"prds"`
	}
	if json.Unmarshal(body, &v) != nil {
		return nil
	}
	var out []string
	for _, raw := range v.PRDs {
		pf := planningFailureOfBody(raw)
		if pf == nil {
			continue
		}
		var h struct {
			ID    string `json:"id"`
			Title string `json:"title"`
		}
		_ = json.Unmarshal(raw, &h)
		name := h.ID
		if h.Title != "" {
			name = h.Title + " (" + h.ID + ")"
		}
		out = append(out, name+": "+PlanningFailureOneLine(pf))
	}
	return out
}

// PlanningFailureFromBody decodes the planning_failure object of an Automaton
// body, or nil when it carries none.
func PlanningFailureFromBody(body []byte) *PlanningFailure { return planningFailureOfBody(body) }
