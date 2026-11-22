package capacity

import "testing"

func TestAdoptOverLimitFlagsAndBlocks(t *testing.T) {
	l := New(Options{})
	l.SetLimit("llm:x", 1)
	for _, h := range []string{"a", "b"} {
		l.Adopt(Request{Holder: h, PRDID: h, Pools: []string{"llm:x"}}, "s-"+h)
	}
	over := l.OverLimitPools()
	if len(over) != 1 || over[0].Held != 2 || over[0].Limit != 1 || over[0].Pool != "llm:x" {
		t.Fatalf("over = %+v", over)
	}
	for _, ls := range l.Leases() {
		if !ls.OverLimit || ls.OverLimitReason == "" {
			t.Fatalf("lease not flagged: %+v", ls)
		}
	}
	l.Release("a")
	l.Release("b")
	if len(l.OverLimitPools()) != 0 {
		t.Fatal("still over")
	}
}
