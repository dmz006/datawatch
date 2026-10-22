package autonomous

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/inference"
)

// budgetFakeClock is a manually advanced clock.
type budgetFakeClock struct{ t time.Time }

func (c *budgetFakeClock) Now() time.Time          { return c.t }
func (c *budgetFakeClock) Advance(d time.Duration) { c.t = c.t.Add(d) }

func newTestBudgetLedger(t *testing.T, clk *budgetFakeClock, bw *inference.BudgetWindow) *BudgetLedger {
	t.Helper()
	l := NewBudgetLedger(filepath.Join(t.TempDir(), "budget-ledger.jsonl"))
	l.Clock = clk.Now
	l.Budgets = func(llm string) *inference.BudgetWindow {
		if llm == "claude" {
			return bw
		}
		return nil
	}
	l.LLMs = func() []string { return []string{"claude", "other"} }
	return l
}

func TestBudgetRollingWindowInAndOut(t *testing.T) {
	clk := &budgetFakeClock{t: time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC)}
	l := newTestBudgetLedger(t, clk, &inference.BudgetWindow{CeilingUSD: 10, Hours: 5})

	l.Record("claude", clk.Now(), 4, 100, 50)
	clk.Advance(time.Hour)
	l.Record("claude", clk.Now(), 3, 100, 50)
	l.Record("other", clk.Now(), 99, 1, 1)
	if got := l.SpendInWindow("claude"); got != 7 {
		t.Fatalf("spend = %v, want 7", got)
	}
	if blocked, _ := l.Blocked("claude"); blocked {
		t.Fatal("blocked below ceiling")
	}
	if blocked, _ := l.Blocked("other"); blocked {
		t.Fatal("LLM without a budget must never be blocked")
	}

	clk.Advance(time.Hour)
	l.Record("claude", clk.Now(), 3, 100, 50) // 10 total: at the ceiling
	blocked, reason := l.Blocked("claude")
	if !blocked || !strings.HasPrefix(reason, "budget window full:") {
		t.Fatalf("blocked=%v reason=%q", blocked, reason)
	}
	st, ok := l.Status("claude")
	if !ok || !st.Full || st.RemainingUSD != 0 || st.Window != "rolling" || st.WindowHours != 5 {
		t.Fatalf("status = %+v", st)
	}
	// First entry (4) ages out at 09:00+5h; that drops spend to 6 < 10.
	want := time.Date(2026, 10, 20, 14, 0, 0, 0, time.UTC)
	if st.RollsOverAt == nil || !st.RollsOverAt.Equal(want) {
		t.Fatalf("rolls_over_at = %v, want %v", st.RollsOverAt, want)
	}
	if !strings.Contains(reason, want.Format(time.RFC3339)) {
		t.Errorf("reason %q lacks rollover time", reason)
	}

	// Past the first entry's age-out: spend 6, not blocked.
	clk.Advance(3*time.Hour + time.Minute)
	if got := l.SpendInWindow("claude"); got != 6 {
		t.Fatalf("spend after first ages out = %v, want 6", got)
	}
	if blocked, _ := l.Blocked("claude"); blocked {
		t.Fatal("still blocked after window rolled")
	}
	st, _ = l.Status("claude")
	if st.RollsOverAt == nil || !st.RollsOverAt.Equal(time.Date(2026, 10, 20, 15, 0, 0, 0, time.UTC)) {
		t.Fatalf("not-full rolls_over_at = %v, want oldest counted entry age-out 15:00", st.RollsOverAt)
	}

	// Everything ages out: empty, and no rollover reported.
	clk.Advance(10 * time.Hour)
	if got := l.SpendInWindow("claude"); got != 0 {
		t.Fatalf("spend = %v, want 0", got)
	}
	st, _ = l.Status("claude")
	if st.RollsOverAt != nil {
		t.Fatalf("rolls_over_at = %v with no spend, want nil", st.RollsOverAt)
	}
	if !st.WindowEnd.Equal(clk.Now()) {
		t.Errorf("rolling window_end = %v, want now", st.WindowEnd)
	}
}

func TestBudgetWeeklyAnchorRollover(t *testing.T) {
	ny, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skip("tzdata unavailable")
	}
	bw := &inference.BudgetWindow{CeilingUSD: 20, Weekday: "friday", Time: "17:00", Zone: "America/New_York"}
	// Wednesday 2026-10-21 noon New York.
	clk := &budgetFakeClock{t: time.Date(2026, 10, 21, 12, 0, 0, 0, ny)}
	l := newTestBudgetLedger(t, clk, bw)

	l.Record("claude", clk.Now(), 25, 1000, 1000)
	blocked, reason := l.Blocked("claude")
	if !blocked || !strings.HasPrefix(reason, "budget window full:") {
		t.Fatalf("blocked=%v reason=%q", blocked, reason)
	}
	anchor := time.Date(2026, 10, 23, 17, 0, 0, 0, ny)
	st, _ := l.Status("claude")
	if st.Window != "weekly" || st.WindowHours != 0 {
		t.Fatalf("status = %+v", st)
	}
	if !st.WindowEnd.Equal(anchor) || st.RollsOverAt == nil || !st.RollsOverAt.Equal(anchor) {
		t.Fatalf("window_end=%v rolls_over_at=%v, want next anchor %v", st.WindowEnd, st.RollsOverAt, anchor)
	}
	if !st.WindowStart.Equal(time.Date(2026, 10, 16, 17, 0, 0, 0, ny)) {
		t.Errorf("window_start = %v", st.WindowStart)
	}

	// Just before the anchor: still blocked.
	clk.t = anchor.Add(-time.Minute)
	if blocked, _ := l.Blocked("claude"); !blocked {
		t.Fatal("not blocked just before anchor")
	}
	// At the anchor a new window starts: empty and not blocked.
	clk.t = anchor
	if got := l.SpendInWindow("claude"); got != 0 {
		t.Fatalf("spend at anchor = %v, want 0", got)
	}
	if blocked, _ := l.Blocked("claude"); blocked {
		t.Fatal("blocked after anchor passed")
	}
	st, _ = l.Status("claude")
	if !st.WindowEnd.Equal(anchor.AddDate(0, 0, 7)) {
		t.Errorf("next window_end = %v", st.WindowEnd)
	}
}

func TestBudgetLedgerPersistsAcrossRestart(t *testing.T) {
	clk := &budgetFakeClock{t: time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC)}
	path := filepath.Join(t.TempDir(), "state", "budget-ledger.jsonl")
	bw := &inference.BudgetWindow{CeilingUSD: 10, Hours: 24}
	configure := func(l *BudgetLedger) *BudgetLedger {
		l.Clock = clk.Now
		l.Budgets = func(string) *inference.BudgetWindow { return bw }
		return l
	}

	// Seed the file with an entry older than the retention period plus a
	// garbage line, as an earlier daemon might have left behind.
	old := budgetEntry{LLM: "claude", At: clk.Now().Add(-40 * 24 * time.Hour), USD: 50}
	oldLine, _ := json.Marshal(old)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(append(oldLine, '\n'), []byte("not json\n")...), 0o600); err != nil {
		t.Fatal(err)
	}

	first := configure(NewBudgetLedger(path))
	first.Record("claude", clk.Now().Add(-time.Hour), 6, 10, 20)
	first.Record("claude", clk.Now().Add(-30*time.Minute), 4, 10, 20)
	if blocked, _ := first.Blocked("claude"); !blocked {
		t.Fatal("first ledger not blocked at ceiling")
	}

	second := configure(NewBudgetLedger(path))
	if got := second.SpendInWindow("claude"); got != 10 {
		t.Fatalf("restarted ledger spend = %v, want 10", got)
	}
	if blocked, _ := second.Blocked("claude"); !blocked {
		t.Fatal("restarted ledger forgot spend")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 {
		t.Fatalf("file has %d lines after pruning, want 2:\n%s", len(lines), data)
	}
	for _, ln := range lines {
		var e budgetEntry
		if err := json.Unmarshal([]byte(ln), &e); err != nil || e.USD == 50 {
			t.Fatalf("bad or unpruned line %q: %v", ln, err)
		}
	}

	// Entries age out of retention on write as well.
	clk.Advance(32 * 24 * time.Hour)
	second.Record("claude", clk.Now(), 1, 1, 1)
	third := configure(NewBudgetLedger(path))
	if got := third.SpendInWindow("claude"); got != 1 {
		t.Fatalf("spend after retention = %v, want 1", got)
	}
	data, _ = os.ReadFile(path)
	if n := len(strings.Split(strings.TrimSpace(string(data)), "\n")); n != 1 {
		t.Fatalf("file has %d lines after write-time prune, want 1", n)
	}
}

func TestBudgetAllocationPctEstimatedNeverBlocks(t *testing.T) {
	clk := &budgetFakeClock{t: time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC)}
	bw := &inference.BudgetWindow{CeilingUSD: 1000, Hours: 24, AllocationPct: true}
	l := newTestBudgetLedger(t, clk, bw)
	l.PlanTier = func(string) inference.PlanTier { return inference.PlanTierMax5x }
	tiers := map[string]float64{"max_5x": 100}
	l.PlanTierAllocations = func() map[string]float64 { return tiers }

	l.Record("claude", clk.Now(), 500, 1, 1) // 500% of the 100 allocation
	st, _ := l.Status("claude")
	if st.Allocation == nil {
		t.Fatal("allocation missing though opted in with a declared tier allocation")
	}
	a := st.Allocation
	if !a.Estimated || a.AllocationPct != 500 || a.AllocationUSD != 100 || a.PlanTier != "max_5x" || a.Basis == "" {
		t.Fatalf("allocation = %+v", a)
	}
	if blocked, reason := l.Blocked("claude"); blocked {
		t.Fatalf("estimate far above 100%% blocked: %s", reason)
	}
	raw, _ := json.Marshal(st)
	if !strings.Contains(string(raw), `"estimated":true`) {
		t.Errorf("json lacks estimated label: %s", raw)
	}

	// Not opted in.
	bw.AllocationPct = false
	if st, _ := l.Status("claude"); st.Allocation != nil {
		t.Errorf("allocation present without opt-in: %+v", st.Allocation)
	}
	bw.AllocationPct = true

	// No allocation declared for the tier.
	tiers = map[string]float64{}
	if st, _ := l.Status("claude"); st.Allocation != nil {
		t.Errorf("allocation present with no declared tier allocation")
	}
	tiers = map[string]float64{"max_5x": 0}
	if st, _ := l.Status("claude"); st.Allocation != nil {
		t.Errorf("allocation present for a zero declaration")
	}
	tiers = map[string]float64{"max_5x": 100}

	// No plan tier.
	l.PlanTier = func(string) inference.PlanTier { return inference.PlanTierUnset }
	st, _ = l.Status("claude")
	if st.Allocation != nil {
		t.Errorf("allocation present without a plan tier")
	}
	raw, _ = json.Marshal(st)
	if strings.Contains(string(raw), "allocation") {
		t.Errorf("json carries allocation when absent: %s", raw)
	}
}

func TestBudgetPeerSpendCountsTowardBlocked(t *testing.T) {
	clk := &budgetFakeClock{t: time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC)}
	l := newTestBudgetLedger(t, clk, &inference.BudgetWindow{CeilingUSD: 10, Hours: 5})
	l.Record("claude", clk.Now(), 4, 1, 1)
	l.PeerSpend = func(string) float64 { return 6 }
	blocked, reason := l.Blocked("claude")
	if !blocked || !strings.HasPrefix(reason, "budget window full:") {
		t.Fatalf("blocked=%v reason=%q", blocked, reason)
	}
	st, _ := l.Status("claude")
	if st.SpendUSD != 4 || st.PeerSpendUSD != 6 {
		t.Fatalf("status = %+v", st)
	}
	if all := l.StatusAll(); len(all) != 1 || all[0].LLM != "claude" {
		t.Fatalf("StatusAll = %+v", all)
	}
}
