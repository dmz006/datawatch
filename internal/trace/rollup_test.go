package trace

import (
	"testing"
	"time"
)

var rollNow = time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)

func fixtureEvents() []Event {
	mk := func(sid, task, backend string, k Kind, name, args, outcome string, ms, in, out int64, off int) Event {
		return Event{SessionID: sid, TaskID: task, StoryID: "s1", AutomatonID: "a1", Agent: "ag-" + backend, Backend: backend,
			Kind: k, Name: name, ArgSummary: args, Outcome: outcome, DurationMS: ms, TokensIn: in, TokensOut: out,
			Timestamp: rollNow.Add(time.Duration(off) * time.Second)}
	}
	evs := []Event{
		mk("s-1", "t1", "pi", KindToolCall, "bash", "ls", "", 10, 0, 0, 0),
		mk("s-1", "t1", "pi", KindToolResult, "bash", "ls", "ok", 900, 0, 0, 1),
		mk("s-1", "t1", "pi", KindToolCall, "read", "a.go", "", 5, 0, 0, 2),
		mk("s-1", "t1", "pi", KindToolResult, "read", "a.go", "error: missing", 5, 0, 0, 3),
		mk("s-1", "t1", "pi", KindModelRequest, "m", "", "", 0, 100, 40, 4),
		mk("s-1", "t1", "pi", KindModelRequest, "m", "", "", 0, 50, 10, 5),
		mk("s-1", "t1", "pi", KindCapabilityCheck, "net", "", "denied", 0, 0, 0, 6),
		mk("s-1", "t1", "pi", KindPermissionDecision, "write", "", "allowed", 0, 0, 0, 7),
		mk("s-1", "t1", "pi", KindError, "bash", "", "", 0, 0, 0, 8),
		mk("s-2", "t2", "claude", KindModelRequest, "m", "", "", 0, 7, 3, 9),
	}
	for i := 0; i < 5; i++ {
		evs = append(evs, mk("s-2", "t2", "claude", KindToolCall, "grep", "TODO", "", 1, 0, 0, 10+i))
	}
	evs = append(evs, mk("s-2", "t2", "claude", KindToolCall, "grep", "FIXME", "", 1, 0, 0, 20))
	return evs
}

func byKey(rs []Rollup, k string) Rollup {
	for _, r := range rs {
		if r.Key == k {
			return r
		}
	}
	return Rollup{}
}

func TestRollupSession(t *testing.T) {
	rs := BuildRollups(fixtureEvents(), DimSession, RollupOptions{})
	if len(rs) != 2 || rs[0].Key != "s-1" {
		t.Fatalf("keys: %+v", rs)
	}
	r := byKey(rs, "s-1")
	if r.ToolCounts["bash"] != 1 || r.ToolCounts["read"] != 1 {
		t.Errorf("counts: %v", r.ToolCounts)
	}
	if r.ModelRequests != 2 || r.TokensIn != 150 || r.TokensOut != 50 {
		t.Errorf("usage: %+v", r.Usage())
	}
	if r.CapabilityDenials != 1 {
		t.Errorf("denials: %d", r.CapabilityDenials)
	}
	// failed read result, error event and denied check over 2 tool calls, capped at 1
	if r.Errors != 3 || r.ErrorRate != 1 {
		t.Errorf("errors: %d rate %v", r.Errors, r.ErrorRate)
	}
	if r.SlowestTools[0].Name != "bash" || r.SlowestTools[0].MaxMS != 900 {
		t.Errorf("slowest: %+v", r.SlowestTools)
	}
}

func TestRollupLoopWarning(t *testing.T) {
	r := byKey(BuildRollups(fixtureEvents(), DimSession, RollupOptions{}), "s-2")
	if len(r.LoopWarnings) != 1 || r.LoopWarnings[0].Tool != "grep" || r.LoopWarnings[0].ArgSummary != "TODO" || r.LoopWarnings[0].Count != 5 {
		t.Fatalf("loops: %+v", r.LoopWarnings)
	}
	r = byKey(BuildRollups(fixtureEvents(), DimSession, RollupOptions{LoopThreshold: 6}), "s-2")
	if len(r.LoopWarnings) != 0 {
		t.Fatalf("threshold 6: %+v", r.LoopWarnings)
	}
}

func TestRollupDimensions(t *testing.T) {
	all := RollupAll(fixtureEvents(), RollupOptions{})
	if len(all[DimTask]) != 2 || len(all[DimStory]) != 1 || len(all[DimAutomaton]) != 1 ||
		len(all[DimAgent]) != 2 || len(all[DimBackend]) != 2 {
		t.Fatalf("dims: %v", all)
	}
	if got := byKey(all[DimStory], "s1"); got.ModelRequests != 3 || got.TokensIn != 157 {
		t.Errorf("story usage: %+v", got.Usage())
	}
	if got := byKey(all[DimBackend], "claude"); got.ToolCounts["grep"] != 6 {
		t.Errorf("backend counts: %v", got.ToolCounts)
	}
}

func TestRollupDeterministicAndSkipsUnkeyed(t *testing.T) {
	evs := append(fixtureEvents(), Event{Kind: KindToolCall, Name: "x", Timestamp: rollNow})
	a := BuildRollups(evs, DimSession, RollupOptions{})
	b := BuildRollups(evs, DimSession, RollupOptions{})
	if len(a) != 2 || len(a) != len(b) || a[0].Key != b[0].Key {
		t.Fatalf("unstable: %v %v", a, b)
	}
}

func TestRollupEmpty(t *testing.T) {
	if rs := BuildRollups(nil, DimTask, RollupOptions{}); len(rs) != 0 {
		t.Fatal(rs)
	}
	s := SummarizeUsage(nil)
	if s.Total.ModelRequests != 0 || len(s.ByBackend) != 0 {
		t.Fatal(s)
	}
}

func TestSummarizeUsageAndStoreRollups(t *testing.T) {
	st, _ := newTestStore(t, Settings{})
	if _, err := st.Append(fixtureEvents()...); err != nil {
		t.Fatal(err)
	}
	evs, err := st.ReadAll()
	if err != nil || len(evs) != len(fixtureEvents()) {
		t.Fatalf("readall: %d %v", len(evs), err)
	}
	s := SummarizeUsage(evs)
	if s.Total.ModelRequests != 3 || s.Total.TokensIn != 157 || s.Total.TokensOut != 53 {
		t.Errorf("total: %+v", s.Total)
	}
	if s.ByBackend["pi"].ModelRequests != 2 || s.ByBackend["claude"].TokensIn != 7 {
		t.Errorf("by backend: %+v", s.ByBackend)
	}
	rs, err := st.Rollups(DimBackend, RollupOptions{})
	if err != nil || len(rs) != 2 {
		t.Fatalf("rollups: %v %v", rs, err)
	}
}
