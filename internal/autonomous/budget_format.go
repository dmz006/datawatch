// Operator-facing text rendering of the /api/budget response, shared by the
// CLI and the comm channel so both print the same fields.

package autonomous

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// FormatBudgetText renders a GET /api/budget body as text. It accepts both
// response shapes: {"budgets": [...]} for every budgeted LLM and a single
// BudgetStatus object for one LLM. The allocation figure is labelled an
// estimate because it never blocks anything.
func FormatBudgetText(body []byte) (string, error) {
	var list struct {
		Budgets *[]BudgetStatus `json:"budgets"`
	}
	if err := json.Unmarshal(body, &list); err != nil {
		return "", fmt.Errorf("budget response: %w", err)
	}
	var statuses []BudgetStatus
	if list.Budgets != nil {
		statuses = *list.Budgets
	} else {
		var one BudgetStatus
		if err := json.Unmarshal(body, &one); err != nil {
			return "", fmt.Errorf("budget response: %w", err)
		}
		statuses = []BudgetStatus{one}
	}
	if len(statuses) == 0 {
		return "No LLM has a budget configured\n", nil
	}
	var b strings.Builder
	for _, st := range statuses {
		writeBudgetStatus(&b, st)
	}
	return b.String(), nil
}

func writeBudgetStatus(b *strings.Builder, st BudgetStatus) {
	window := st.Window
	if st.WindowHours > 0 {
		window = fmt.Sprintf("%s %dh", st.Window, st.WindowHours)
	}
	fmt.Fprintf(b, "%s: spend $%.2f of $%.2f ceiling (%s window)", st.LLM, st.SpendUSD, st.CeilingUSD, window)
	if st.Full {
		b.WriteString(" - FULL")
	}
	b.WriteString("\n")
	if st.PeerSpendUSD > 0 {
		fmt.Fprintf(b, "  peer spend: $%.2f\n", st.PeerSpendUSD)
	}
	fmt.Fprintf(b, "  remaining: $%.2f\n", st.RemainingUSD)
	fmt.Fprintf(b, "  window end: %s\n", st.WindowEnd.Format(time.RFC3339))
	if st.RollsOverAt != nil {
		fmt.Fprintf(b, "  rolls over: %s\n", st.RollsOverAt.Format(time.RFC3339))
	} else {
		b.WriteString("  rolls over: n/a\n")
	}
	if a := st.Allocation; a != nil {
		fmt.Fprintf(b, "  allocation (estimate): ~%.0f%% of $%.2f (%s)\n", a.AllocationPct, a.AllocationUSD, a.PlanTier)
	}
}
