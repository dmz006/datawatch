package capacity

import (
	"context"
	"strings"
	"testing"
)

// acquireOnNode takes one slot on pool for holder, bound to node ("" = none).
func acquireOnNode(t *testing.T, l *Ledger, holder, node, pool string) {
	t.Helper()
	req := Request{Holder: holder, PRDID: "p1", Pools: []string{pool}, Node: node}
	if err := l.Acquire(context.Background(), req, 0, nil, nil); err != nil {
		t.Fatalf("acquire %s: %v", holder, err)
	}
}

func TestCapacityTextShowsHeldNode(t *testing.T) {
	l := New(Options{})
	l.SetLimit("llm:pi", 4)
	acquireOnNode(t, l, "t-a", "gpu-a", "llm:pi")
	acquireOnNode(t, l, "t-b", "gpu-b", "llm:pi")
	acquireOnNode(t, l, "t-none", "", "llm:pi")

	st := l.Snapshot()
	if len(st.Pools) != 1 || st.Pools[0].Name != "llm:pi" {
		t.Fatalf("pools = %+v", st.Pools)
	}
	if got, want := st.Pools[0].HoldNodes, []string{"gpu-a", "gpu-b", ""}; strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("HoldNodes = %v, want %v (parallel to Holders %v)", got, want, st.Pools[0].Holders)
	}
	out := FormatText(st)
	for _, want := range []string{
		"llm:pi: 3/4 held by t-a on gpu-a,t-b on gpu-b,t-none",
		"Waiting: none",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
	// The same holders through FormatPools with readable names: node follows the name.
	named := []PoolStatus{{Name: "llm:pi", Limit: 4, Held: 2, Holders: []string{"t-a", "t-b"}, HoldNames: []string{"Ship / Export (t-a1234)", "Ship / Import (t-b5678)"}, HoldNodes: []string{"gpu-a", "gpu-b"}}}
	if out := FormatPools(named); !strings.Contains(out, "held by Ship / Export (t-a1234) on gpu-a,Ship / Import (t-b5678) on gpu-b") {
		t.Fatalf("readable form with nodes:\n%s", out)
	}
}

func TestCapacityViewSingleNodeUnchanged(t *testing.T) {
	// A one-node entry: both holders sit on gpu-a, so no node is printed.
	l := New(Options{})
	l.SetLimit("node:gpu-a", 2)
	acquireOnNode(t, l, "t-1", "gpu-a", "node:gpu-a")
	acquireOnNode(t, l, "t-2", "gpu-a", "node:gpu-a")
	st := l.Snapshot()
	if got := FormatText(st); !strings.HasPrefix(got, "node:gpu-a: 2/2 held by t-1,t-2\n") {
		t.Fatalf("one-node entry changed:\n%s", got)
	}
	// Carried in the snapshot all the same, for the JSON consumers.
	if got := st.Pools[0].HoldNodes; len(got) != 2 || got[0] != "gpu-a" || got[1] != "gpu-a" {
		t.Fatalf("HoldNodes = %v", got)
	}

	// A pool whose only leases name no node: no HoldNodes at all, rendered as before.
	l2 := New(Options{})
	l2.SetLimit("host", 3)
	acquireOnNode(t, l2, "t-x", "", "host")
	acquireOnNode(t, l2, "t-y", "", "host")
	st2 := l2.Snapshot()
	if st2.Pools[0].HoldNodes != nil {
		t.Fatalf("HoldNodes = %v, want nil for leases with no node", st2.Pools[0].HoldNodes)
	}
	want := "host: 2/3 held by t-x,t-y\nWaiting: none\n"
	if got := FormatText(st2); got != want {
		t.Fatalf("no-node lease changed:\ngot  %q\nwant %q", got, want)
	}

	// A pool with holders on two nodes shows the node; one holder with no node stays bare.
	pool := []PoolStatus{{Name: "llm:pi", Limit: 4, Held: 3, Holders: []string{"a", "b", "c"}, HoldNodes: []string{"gpu-a", "", "gpu-b"}}}
	if got := FormatPools(pool); got != "llm:pi: 3/4 held by a on gpu-a,b,c on gpu-b\n" {
		t.Fatalf("mixed pool = %q", got)
	}
}
