// Plain-text rendering of an Automaton's completion review outcome for the
// CLI. The REST body stays the source of truth; this only formats it.
package main

import (
	"encoding/json"
	"fmt"
	"strings"
)

// rulesReviewOutcomeView is the part of rules_review_outcome the CLI prints.
type rulesReviewOutcomeView struct {
	Action       string   `json:"action"`
	Findings     []string `json:"findings"`
	RetryBudget  int      `json:"retry_budget"`
	RetriesSpent int      `json:"retries_spent"`
}

// rulesReviewActionWords turns a recorded action into plain words.
func rulesReviewActionWords(action string) string {
	switch action {
	case "fix_task_created":
		return "fix task created"
	case "fix_task_failed":
		return "fix task failed"
	case "retries_spent":
		return "retries spent"
	case "auto_fix_disabled":
		return "auto-fix off"
	case "":
		return "unknown"
	}
	return strings.ReplaceAll(action, "_", " ")
}

// formatRulesReviewOutcome renders the action, the retries spent of the
// budget and each finding as a bullet.
func formatRulesReviewOutcome(o rulesReviewOutcomeView) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Completion review: %s\n", rulesReviewActionWords(o.Action))
	fmt.Fprintf(&b, "Retries spent: %d of %d\n", o.RetriesSpent, o.RetryBudget)
	for _, f := range o.Findings {
		fmt.Fprintf(&b, "  - %s\n", f)
	}
	return b.String()
}

// rulesReviewOutcomeBlocks returns one text block per item of a REST body
// that carries rules_review_outcome. The body is either an object with an
// "items" array (attention) or a single Automaton object. Each block starts
// with the item's id and title when present.
func rulesReviewOutcomeBlocks(body []byte) []string {
	type item struct {
		ID      string                  `json:"id"`
		Title   string                  `json:"title"`
		Outcome *rulesReviewOutcomeView `json:"rules_review_outcome"`
	}
	var wrap struct {
		item
		Items []item `json:"items"`
	}
	if json.Unmarshal(body, &wrap) != nil {
		return nil
	}
	items := wrap.Items
	if len(items) == 0 {
		items = []item{wrap.item}
	}
	var out []string
	for _, it := range items {
		if it.Outcome == nil {
			continue
		}
		head := ""
		if it.ID != "" {
			head = it.ID
			if it.Title != "" {
				head += " " + it.Title
			}
			head += "\n"
		}
		out = append(out, head+formatRulesReviewOutcome(*it.Outcome))
	}
	return out
}
