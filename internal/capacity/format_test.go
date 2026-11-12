package capacity

import (
	"strings"
	"testing"
	"time"
)

func TestFormatPools_HoldNamesPreferred(t *testing.T) {
	out := FormatPools([]PoolStatus{
		{Name: "node:a", Limit: 1, Held: 1, Holders: []string{"t7abc1234"}, HoldNames: []string{"Ship billing / Add exports (t7abc123)"}},
		{Name: "host", Limit: 9, Held: 2, Holders: []string{"t1", "t2"}},                                            // no HoldNames: raw, as before
		{Name: "llm:claude", Limit: 2, Held: 2, Holders: []string{"t9", "t10"}, HoldNames: []string{"a", "b", "c"}}, // not parallel: raw
	})
	for _, want := range []string{
		"node:a: 1/1 held by Ship billing / Add exports (t7abc123)",
		"host: 2/9 held by t1,t2",
		"llm:claude: 2/2 held by t9,t10",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
	if out := FormatPools(nil); out != "Pools: none configured\n" {
		t.Fatalf("empty = %q", out)
	}
}

func TestFormatText_ReadableWaiter(t *testing.T) {
	st := Status{
		Pools:   []PoolStatus{{Name: "node:a", Limit: 1, Held: 1, Holders: []string{"t7"}, HoldNames: []string{"Ship / Export (t7a1b2)"}}},
		Waiting: []Waiter{{Holder: "t9", PRDID: "p9", Since: time.Now().Add(-time.Hour), Reason: "waiting for node:a, held by t7", ReadReason: "waiting for node:a, held by Ship / Export (t7a1b2)", HoldName: "Queue / Task (9fedcba8)"}},
	}
	out := FormatText(st)
	for _, want := range []string{
		"held by Ship / Export (t7a1b2)",
		"Queue / Task (9fedcba8) (PRD p9)",
		"held by Ship / Export (t7a1b2)",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "waiting for node:a, held by t7 ") || strings.Contains(out, "held by t7\n") {
		t.Fatalf("raw reason should be replaced by the readable one:\n%s", out)
	}

	// Unresolved snapshot: exactly the previous raw rendering.
	st2 := Status{
		Pools:   []PoolStatus{{Name: "node:a", Limit: 1, Held: 1, Holders: []string{"t7"}}},
		Waiting: []Waiter{{Holder: "t9", PRDID: "p9", Since: time.Now().Add(-time.Hour), Reason: "waiting for node:a, held by t7"}},
	}
	out2 := FormatText(st2)
	for _, want := range []string{"held by t7", "t9 (PRD p9)", "waiting for node:a, held by t7"} {
		if !strings.Contains(out2, want) {
			t.Fatalf("missing raw %q in:\n%s", want, out2)
		}
	}
	if out := FormatText(Status{}); out != "Pools: none configured\nWaiting: none\n" {
		t.Fatalf("empty = %q", out)
	}
}
