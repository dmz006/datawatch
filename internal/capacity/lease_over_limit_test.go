package capacity

import (
	"strings"
	"testing"
)

// The capacity read names each adopted worker lease that sits in a pool
// holding more than its limit, with a machine-readable code and a reason.
func TestSnapshotFlagsAdoptedWorkersOverTheLimit(t *testing.T) {
	l := New(fast())
	l.SetLimit("llm:pi-prompt", 3)
	for i := 0; i < 5; i++ {
		id := string(rune('a' + i))
		l.Adopt(Request{Holder: "t-" + id, Pools: []string{"llm:pi-prompt"}}, "sess-"+id)
	}
	st := l.Snapshot()
	if len(st.Leases) != 5 {
		t.Fatalf("leases = %d, want 5", len(st.Leases))
	}
	for _, ls := range st.Leases {
		if !ls.Adopted || !ls.OverLimit || ls.OverLimitCode != OverLimitAdopted {
			t.Fatalf("lease %s not flagged over the limit: %+v", ls.Holder, ls)
		}
		if !strings.Contains(ls.OverLimitReason, "llm:pi-prompt") || !strings.Contains(ls.OverLimitReason, "5") || !strings.Contains(ls.OverLimitReason, "3") {
			t.Fatalf("reason must name the pool, what it holds and the limit: %q", ls.OverLimitReason)
		}
	}
	// Back at the limit: no flag.
	l.Release("t-a")
	l.Release("t-b")
	for _, ls := range l.Snapshot().Leases {
		if ls.OverLimit || ls.OverLimitCode != "" || ls.OverLimitReason != "" {
			t.Fatalf("lease %s still flagged at the limit: %+v", ls.Holder, ls)
		}
	}
}

// A lease granted by normal admission is never flagged, and the stored lease
// is not changed by a read.
func TestSnapshotDoesNotFlagAdmittedWorkers(t *testing.T) {
	l := New(fast())
	l.SetLimit("llm:x", 1)
	if err := acq(t, l, Request{Holder: "a", Pools: []string{"llm:x"}}, 0); err != nil {
		t.Fatal(err)
	}
	for _, ls := range l.Snapshot().Leases {
		if ls.OverLimit {
			t.Fatalf("admitted lease flagged: %+v", ls)
		}
	}
}

// An operator start over the cap is not a worker over the limit: the overage
// is already on its own lease, and an adopted worker in the same pool is not
// flagged because of it.
func TestSnapshotOperatorOverageDoesNotFlagWorkers(t *testing.T) {
	l := New(fast())
	l.SetLimit("llm:x", 1)
	l.Adopt(Request{Holder: "w", Pools: []string{"llm:x"}}, "sess-w")
	if err := acq(t, l, Request{Holder: "op", Class: ClassOperator, Pools: []string{"llm:x"}}, 0); err != nil {
		t.Fatal(err)
	}
	for _, ls := range l.Snapshot().Leases {
		if ls.OverLimit {
			t.Fatalf("lease %s flagged only because of an operator overage: %+v", ls.Holder, ls)
		}
	}
}

// An adopted lease held outside a full node pool reads over the limit on the node.
func TestSnapshotFlagsNodeOverflowLease(t *testing.T) {
	l := New(fast())
	l.SetLimit("node:gpu1", 1)
	l.Adopt(Request{Holder: "a", Pools: []string{"llm:m"}, Node: "gpu1"}, "sess-a")
	l.Adopt(Request{Holder: "b", Pools: []string{"llm:m"}, Node: "gpu1"}, "sess-b")
	flagged := 0
	for _, ls := range l.Snapshot().Leases {
		if ls.OverLimit {
			flagged++
			if !strings.Contains(ls.OverLimitReason, "node gpu1") {
				t.Fatalf("reason must name the node: %q", ls.OverLimitReason)
			}
		}
	}
	if flagged == 0 {
		t.Fatal("no lease flagged on the over-limit node")
	}
}
