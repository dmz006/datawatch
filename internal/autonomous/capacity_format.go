// Operator-facing text rendering of the /api/capacity response, shared by the
// CLI and the comm channel so both print the same reasons the API carries.

package autonomous

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/capacity"
)

// CapacityTextClock supplies "now" for the waited and held durations in
// FormatCapacityText. Tests override it so two renders of one body match.
var CapacityTextClock = time.Now

// capacityBackoffView is one active back-off flag as /api/capacity carries it.
type capacityBackoffView struct {
	Backend    string    `json:"backend"`
	Until      time.Time `json:"until"`
	Reason     string    `json:"reason"`
	ResetKnown bool      `json:"reset_known"`
}

// capacityBody is the part of the /api/capacity response the text needs.
type capacityBody struct {
	Pools   []capacity.PoolStatus `json:"pools"`
	Waiting []capacity.Waiter     `json:"waiting"`
	Backoff []capacityBackoffView `json:"backoff"`
	Budgets []BudgetStatus        `json:"budgets"`
	Tasks   struct {
		Waiting []TaskWaitView `json:"waiting"`
		Running []TaskHoldView `json:"running"`
	} `json:"tasks"`
}

// FormatCapacityText renders a GET /api/capacity body as text: each pool as
// used/limit with holders, each queued task with its reason and how long it
// has waited, each running task with what it holds, each active back-off flag
// and each budget. The budget lines are the ones FormatBudgetText prints.
// Ledger waiters are listed only when the response carries no queued tasks.
func FormatCapacityText(body []byte) (string, error) {
	var c capacityBody
	if err := json.Unmarshal(body, &c); err != nil {
		return "", fmt.Errorf("capacity response: %w", err)
	}
	now := CapacityTextClock()
	var b strings.Builder
	b.WriteString(capacity.FormatPools(c.Pools))

	switch {
	case len(c.Tasks.Waiting) > 0:
		fmt.Fprintf(&b, "Queued tasks (%d):\n", len(c.Tasks.Waiting))
		for _, w := range c.Tasks.Waiting {
			fmt.Fprintf(&b, "  %s (PRD %s): %s - waiting %s\n",
				firstNonEmpty(w.TaskTitle, w.TaskID), firstNonEmpty(w.PRDTitle, w.PRDID), w.Reason, waitedFor(now, w.Since))
		}
	case len(c.Waiting) > 0:
		fmt.Fprintf(&b, "Waiting (%d):\n", len(c.Waiting))
		for _, w := range c.Waiting {
			fmt.Fprintf(&b, "  %s (PRD %s) %s: %s\n", w.Holder, w.PRDID, waitedFor(now, w.Since), w.Reason)
		}
	default:
		b.WriteString("Queued tasks: none\n")
	}

	if len(c.Tasks.Running) == 0 {
		b.WriteString("Running tasks: none\n")
	} else {
		fmt.Fprintf(&b, "Running tasks (%d):\n", len(c.Tasks.Running))
		for _, h := range c.Tasks.Running {
			fmt.Fprintf(&b, "  %s (PRD %s): holds %s", firstNonEmpty(h.TaskTitle, h.TaskID), h.PRDID, holdList(h.Pools))
			if h.Node != "" {
				fmt.Fprintf(&b, ", node %s", h.Node)
			}
			if h.SessionID != "" {
				fmt.Fprintf(&b, ", session %s", h.SessionID)
			}
			fmt.Fprintf(&b, " - running %s", waitedFor(now, h.Since))
			if h.Note != "" {
				fmt.Fprintf(&b, " (%s)", h.Note)
			}
			b.WriteString("\n")
		}
	}

	if len(c.Backoff) == 0 {
		b.WriteString("Back-off: none\n")
	} else {
		fmt.Fprintf(&b, "Back-off (%d):\n", len(c.Backoff))
		for _, f := range c.Backoff {
			when := "reset time unknown"
			if f.ResetKnown {
				when = "resets " + f.Until.Format(time.RFC3339)
			}
			fmt.Fprintf(&b, "  %s: %s", f.Backend, when)
			if f.Reason != "" {
				fmt.Fprintf(&b, " (%s)", f.Reason)
			}
			b.WriteString("\n")
		}
	}

	if len(c.Budgets) == 0 {
		b.WriteString("Budgets: none configured\n")
	} else {
		b.WriteString("Budgets:\n")
		for _, st := range c.Budgets {
			writeBudgetStatus(&b, st)
		}
	}
	return b.String(), nil
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func holdList(pools []string) string {
	if len(pools) == 0 {
		return "nothing"
	}
	return strings.Join(pools, ", ")
}

// waitedFor is the time since t, rounded to the second ("unknown" when t is
// the zero time).
func waitedFor(now, t time.Time) string {
	if t.IsZero() {
		return "unknown time"
	}
	d := now.Sub(t)
	if d < 0 {
		d = 0
	}
	return d.Round(time.Second).String()
}
