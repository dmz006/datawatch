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
	for _, p := range st.Peers {
		if p.Counted {
			fmt.Fprintf(b, "  peer %s: $%.2f (counted)\n", p.Peer, p.SpendUSD)
		} else {
			fmt.Fprintf(b, "  peer %s: not counted (%s)\n", p.Peer, p.Reason)
		}
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

// peerNoConfirmWording ends every peer line of an Automation budget block: a
// peer is counted only because the operator named it, and nothing confirms
// that it shares the account.
const peerNoConfirmWording = "opted in by the operator; no automatic confirmation that the peer shares the account"

// FormatAutomationBudgetText renders the computed budget view of one
// Automation as a text block. The CLI and the comm channel both print it, so
// they show the same lines: scope, spend against the ceiling, window, reset
// time (or that it is unknown), what a breach will do, a pacing hold if there
// is one, the allocation figure on its own line (always an estimate, never a
// stop) and one line per peer.
func FormatAutomationBudgetText(view BudgetView) string {
	var b strings.Builder
	fmt.Fprintf(&b, "budget (%s)\n", view.Scope)

	if view.CeilingType == "" {
		// No policy in play any more: only the record of the last breach is left.
		writeBudgetEpisode(&b, view.State)
		return b.String()
	}

	window := view.Window
	if window == "automation" {
		window = "lifetime"
	}
	if view.CeilingType == "dollar" {
		fmt.Fprintf(&b, "  spent: $%.2f of $%.2f ceiling (%s window)\n", view.SpentUSD, view.CeilingUSD, window)
	} else {
		fmt.Fprintf(&b, "  spent: $%.2f (%s window); no dollar ceiling\n", view.SpentUSD, window)
	}
	if view.WindowStart != nil && view.WindowEnd != nil {
		fmt.Fprintf(&b, "  window: %s to %s\n", view.WindowStart.Format(time.RFC3339), view.WindowEnd.Format(time.RFC3339))
	}
	if view.ResetKnown && view.ResetAt != nil {
		fmt.Fprintf(&b, "  reset: %s", view.ResetAt.Format(time.RFC3339))
		if view.ResetSource != "" {
			fmt.Fprintf(&b, " (%s)", view.ResetSource)
		}
		b.WriteString("\n")
	} else {
		b.WriteString("  reset: reset time unknown\n")
	}

	switch {
	case view.CeilingType != "dollar":
		b.WriteString("  on breach: nothing; this policy is an estimate only and never pauses or blocks\n")
	case view.OnBreach != "block" && view.ResetKnown:
		b.WriteString("  on breach: pauses and resumes by itself at the reset\n")
	default:
		b.WriteString("  on breach: blocked and waits for the operator\n")
	}
	if view.Pacing == "even" {
		fmt.Fprintf(&b, "  pacing: even ($%.2f allowed so far)\n", view.PacingAllowedUSD)
	}
	if view.Hold != "" {
		fmt.Fprintf(&b, "  hold (%s): %s\n", view.Hold, view.HoldReason)
	}
	writeBudgetEpisode(&b, view.State)

	if a := view.Allocation; a != nil {
		fmt.Fprintf(&b, "  allocation (estimated): ~%.0f%% of $%.2f (%s); an estimate only, it never pauses or blocks\n",
			a.AllocationPct, a.AllocationUSD, a.PlanTier)
	}
	for _, p := range view.Peers {
		if p.Counted {
			fmt.Fprintf(&b, "  peer %s: counted, $%.2f (%s)\n", p.Peer, p.SpendUSD, peerNoConfirmWording)
		} else {
			fmt.Fprintf(&b, "  peer %s: not counted (%s; %s)\n", p.Peer, p.Reason, peerNoConfirmWording)
		}
	}
	if view.PeerNote != "" {
		fmt.Fprintf(&b, "  peers: %s\n", view.PeerNote)
	}
	return b.String()
}

// writeBudgetEpisode prints the stored breach episode: what the Automation is
// doing about it and, for a pause, when it resumes by itself.
func writeBudgetEpisode(b *strings.Builder, st *BudgetState) {
	if st == nil {
		return
	}
	fmt.Fprintf(b, "  state: %s since %s", st.Action, st.BreachedAt.Format(time.RFC3339))
	if st.Reason != "" {
		fmt.Fprintf(b, " - %s", st.Reason)
	}
	b.WriteString("\n")
	if st.Action == BudgetActionPaused && st.ResumeAt != nil {
		fmt.Fprintf(b, "  resumes by itself at: %s\n", st.ResumeAt.Format(time.RFC3339))
	}
}
