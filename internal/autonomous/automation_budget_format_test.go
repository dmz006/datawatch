package autonomous

import (
	"strings"
	"testing"
	"time"
)

func TestFormatAutomationBudgetText(t *testing.T) {
	start := time.Date(2026, 10, 19, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 0, 7)

	base := BudgetView{
		Scope: "automation", CeilingType: "dollar", CeilingUSD: 40, SpentUSD: 12.4, RemainingUSD: 27.6,
		Window: "week", WindowStart: &start, WindowEnd: &end,
		Pacing: "none", OnBreach: "pause", ResetAt: &end, ResetKnown: true, ResetSource: BudgetResetWindow,
	}

	t.Run("known reset pauses and resumes by itself", func(t *testing.T) {
		out := FormatAutomationBudgetText(base)
		for _, want := range []string{
			"budget (automation)",
			"spent: $12.40 of $40.00 ceiling (week window)",
			"window: 2026-10-19T00:00:00Z to 2026-10-26T00:00:00Z",
			"reset: 2026-10-26T00:00:00Z (window)",
			"on breach: pauses and resumes by itself at the reset",
		} {
			if !strings.Contains(out, want) {
				t.Errorf("missing %q in:\n%s", want, out)
			}
		}
		if strings.Contains(out, "allocation") || strings.Contains(out, "peer ") {
			t.Errorf("no allocation or peer line expected:\n%s", out)
		}
	})

	t.Run("unknown reset goes to blocked", func(t *testing.T) {
		v := base
		v.ResetAt, v.ResetKnown, v.ResetSource = nil, false, ""
		out := FormatAutomationBudgetText(v)
		if !strings.Contains(out, "reset: reset time unknown") {
			t.Errorf("want the words reset time unknown:\n%s", out)
		}
		if !strings.Contains(out, "on breach: blocked and waits for the operator") || strings.Contains(out, "resumes by itself") {
			t.Errorf("unknown reset must be blocked:\n%s", out)
		}
	})

	t.Run("block rule waits for the operator even with a known reset", func(t *testing.T) {
		v := base
		v.OnBreach = "block"
		out := FormatAutomationBudgetText(v)
		if !strings.Contains(out, "on breach: blocked and waits for the operator") {
			t.Errorf("block rule:\n%s", out)
		}
	})

	t.Run("member scope and pacing hold", func(t *testing.T) {
		v := base
		v.Scope, v.Pacing, v.PacingAllowedUSD = "member:m2", "even", 8
		v.Hold, v.HoldReason = BudgetKindPacing, "budget is ahead of an even pace"
		out := FormatAutomationBudgetText(v)
		for _, want := range []string{"budget (member:m2)", "pacing: even ($8.00 allowed so far)", "hold (pacing): budget is ahead of an even pace"} {
			if !strings.Contains(out, want) {
				t.Errorf("missing %q in:\n%s", want, out)
			}
		}
	})

	t.Run("peers: counted and not counted both carry the fixed wording", func(t *testing.T) {
		v := base
		v.Peers = []PeerBudgetStatus{
			{Peer: "ci-box", SpendUSD: 3.2, Counted: true},
			{Peer: "laptop", Reason: "peer unreachable"},
		}
		out := FormatAutomationBudgetText(v)
		var peerLines []string
		for _, line := range strings.Split(out, "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "peer ") {
				peerLines = append(peerLines, line)
			}
		}
		if len(peerLines) != 2 {
			t.Fatalf("want one line per peer, got %d:\n%s", len(peerLines), out)
		}
		if !strings.Contains(peerLines[0], "ci-box: counted, $3.20") || !strings.Contains(peerLines[1], "laptop: not counted (peer unreachable") {
			t.Errorf("peer lines:\n%s", strings.Join(peerLines, "\n"))
		}
		for _, l := range peerLines {
			if !strings.Contains(l, peerNoConfirmWording) {
				t.Errorf("peer line lacks the no-automatic-confirmation wording: %q", l)
			}
		}
	})

	t.Run("paused episode shows when it resumes", func(t *testing.T) {
		v := base
		v.State = &BudgetState{Action: BudgetActionPaused, BreachedAt: start.Add(time.Hour), Reason: "budget reached", ResumeAt: &end}
		out := FormatAutomationBudgetText(v)
		if !strings.Contains(out, "state: paused since 2026-10-19T01:00:00Z - budget reached") ||
			!strings.Contains(out, "resumes by itself at: 2026-10-26T00:00:00Z") {
			t.Errorf("episode:\n%s", out)
		}
	})

	t.Run("estimate-only policy never claims a breach action", func(t *testing.T) {
		v := BudgetView{Scope: "automation", CeilingType: "allocation_pct", SpentUSD: 5, Window: "week", Note: allocationOnlyNote}
		out := FormatAutomationBudgetText(v)
		if !strings.Contains(out, "no dollar ceiling") || !strings.Contains(out, "on breach: nothing") {
			t.Errorf("estimate-only:\n%s", out)
		}
	})

	t.Run("record of a breach without a policy", func(t *testing.T) {
		v := BudgetView{Scope: "automation", State: &BudgetState{Action: BudgetActionBlocked, BreachedAt: start, Reason: "ceiling reached"}}
		out := FormatAutomationBudgetText(v)
		if !strings.Contains(out, "state: blocked since 2026-10-19T00:00:00Z - ceiling reached") || strings.Contains(out, "spent:") {
			t.Errorf("state only:\n%s", out)
		}
	})
}

func TestFormatAutomationBudgetTextLabelsEstimate(t *testing.T) {
	end := time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC)
	v := BudgetView{
		Scope: "automation", CeilingType: "dollar", CeilingUSD: 40, SpentUSD: 10, Window: "week",
		OnBreach: "pause", ResetAt: &end, ResetKnown: true, ResetSource: BudgetResetWindow,
		Allocation: &BudgetAllocation{AllocationPct: 25, Estimated: true, AllocationUSD: 40, PlanTier: "max_5x"},
	}
	out := FormatAutomationBudgetText(v)
	var lines []string
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "allocation") {
			lines = append(lines, line)
		}
	}
	if len(lines) != 1 {
		t.Fatalf("allocation must be on exactly one line, got %d:\n%s", len(lines), out)
	}
	line := lines[0]
	if !strings.Contains(line, "allocation (estimated): ~25% of $40.00 (max_5x)") {
		t.Errorf("allocation line: %q", line)
	}
	if !strings.Contains(line, "never pauses or blocks") {
		t.Errorf("allocation line must say it never pauses or blocks: %q", line)
	}
	if strings.Contains(strings.ToLower(line), "limit") || strings.Contains(strings.ToLower(line), "ceiling") {
		t.Errorf("allocation must not be described as a limit: %q", line)
	}

	v.Allocation = nil
	if strings.Contains(FormatAutomationBudgetText(v), "allocation") {
		t.Errorf("no allocation line when the estimate is off")
	}
}
