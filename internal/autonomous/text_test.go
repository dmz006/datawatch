package autonomous

import (
	"strings"
	"testing"
	"time"
)

const capacityTextFixture = `{
"pools":[{"name":"host","limit":9,"external":1,"held":2,"holders":["t1","t2"]},{"name":"node:a","limit":1,"held":1,"holders":["t1"]}],
"leases":[],"waiting":[],
"backoff":[{"backend":"claude-code","until":"2026-10-22T20:00:00Z","reason":"usage limit","reset_known":true},{"backend":"opencode","until":"2026-10-23T01:00:00Z","reason":"rate limited","reset_known":false}],
"budgets":[{"llm":"claude","ceiling_usd":50,"spend_usd":12.5,"peer_spend_usd":0,"remaining_usd":37.5,"window":"weekly","window_start":"2026-10-19T00:00:00Z","window_end":"2026-10-26T00:00:00Z","full":false}],
"tasks":{
"waiting":[{"task_id":"t3","prd_id":"p9","prd_title":"Plan nine","task_title":"Write parser","reason":"waiting for node:a, held by t1","since":"2026-10-22T16:00:00Z"}],
"running":[{"task_id":"t1","prd_id":"p1","task_title":"Build index","pools":["host","llm:claude","node:a"],"node":"a","session_id":"s-1","since":"2026-10-22T16:55:00Z","note":"parked on an external condition"}]}}`

func TestCapacityText_QueuedReasonAndHoldingsRendered(t *testing.T) {
	CapacityTextClock = func() time.Time { return time.Date(2026, 10, 22, 17, 0, 0, 0, time.UTC) }
	t.Cleanup(func() { CapacityTextClock = time.Now })

	out, err := FormatCapacityText([]byte(capacityTextFixture))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"host: 3/9 (1 external) held by t1,t2",
		"node:a: 1/1 held by t1",
		"Write parser (PRD Plan nine): waiting for node:a, held by t1 - waiting 1h0m0s",
		"Build index (PRD p1): holds host, llm:claude, node:a, node a, session s-1 - running 5m0s (parked on an external condition)",
		"claude-code: resets 2026-10-22T20:00:00Z",
		"opencode: reset time unknown",
		"claude: spend $12.50 of $50.00 ceiling (weekly window)",
		"  window end: 2026-10-26T00:00:00Z",
		"  rolls over: n/a",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}

	// The budget lines are exactly what FormatBudgetText prints.
	budgetOnly, err := FormatBudgetText([]byte(`{"budgets":[{"llm":"claude","ceiling_usd":50,"spend_usd":12.5,"remaining_usd":37.5,"window":"weekly","window_start":"2026-10-19T00:00:00Z","window_end":"2026-10-26T00:00:00Z"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(out, "Budgets:\n"+budgetOnly) {
		t.Fatalf("budget text differs from FormatBudgetText:\n%s\nvs\n%s", out, budgetOnly)
	}
}

func TestCapacityText_EmptyAndLedgerWaiters(t *testing.T) {
	out, err := FormatCapacityText([]byte(`{"pools":[],"waiting":[{"holder":"t3","prd_id":"p9","pools":["node:a"],"since":"2020-01-01T00:00:00Z","reason":"pool node:a full"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Pools: none configured", "Waiting (1):", "t3 (PRD p9)", "pool node:a full", "Running tasks: none", "Back-off: none", "Budgets: none configured"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
	if _, err := FormatCapacityText([]byte("not json")); err == nil {
		t.Fatal("expected error for bad body")
	}
}
