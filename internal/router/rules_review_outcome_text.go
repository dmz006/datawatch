// Short chat rendering of an Automaton's completion review outcome.
package router

import (
	"encoding/json"
	"fmt"
	"strings"
)

// rulesReviewMaxFindings caps the findings shown in one chat reply.
const rulesReviewMaxFindings = 5

type rulesReviewOutcomeView struct {
	Action       string   `json:"action"`
	Findings     []string `json:"findings"`
	RetryBudget  int      `json:"retry_budget"`
	RetriesSpent int      `json:"retries_spent"`
}

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

// formatRulesReviewOutcome is one action line plus up to five findings and
// "+N more" when there are others.
func formatRulesReviewOutcome(o rulesReviewOutcomeView) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Completion review: %s (retries %d of %d)", rulesReviewActionWords(o.Action), o.RetriesSpent, o.RetryBudget)
	for i, f := range o.Findings {
		if i == rulesReviewMaxFindings {
			fmt.Fprintf(&b, "\n+%d more", len(o.Findings)-rulesReviewMaxFindings)
			break
		}
		b.WriteString("\n- " + f)
	}
	return b.String()
}

// rulesReviewOutcomeLines returns one block per item of a REST body (an
// "items" list or a single Automaton) that carries rules_review_outcome.
func rulesReviewOutcomeLines(body []byte) []string {
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
			head = it.ID + ": "
		}
		out = append(out, head+formatRulesReviewOutcome(*it.Outcome))
	}
	return out
}
