package session

import "testing"

func TestUsageEventHookFires(t *testing.T) {
	mgr := newTestManager(t)
	mgr.SetCostRates(map[string]CostRate{"pi-prompt": {InPerK: 1, OutPerK: 2}})
	s := newTestSession(t, mgr, "hk01")
	s.BackendFamily = "pi-prompt"
	if err := mgr.store.Save(s); err != nil {
		t.Fatalf("save: %v", err)
	}

	var got []UsageEvent
	mgr.SetUsageEventHook(func(ev UsageEvent) { got = append(got, ev) })

	if err := mgr.AddUsage(s.FullID, 1000, 500, CostRate{}); err != nil {
		t.Fatalf("AddUsage: %v", err)
	}
	if err := mgr.AddUsage(s.FullID, 2000, 0, CostRate{}); err != nil {
		t.Fatalf("AddUsage: %v", err)
	}
	// Both token deltas zero: no event.
	if err := mgr.AddUsage(s.FullID, 0, 0, CostRate{}); err != nil {
		t.Fatalf("AddUsage: %v", err)
	}

	if len(got) != 2 {
		t.Fatalf("events = %d, want 2", len(got))
	}
	if got[0].SessionID != s.FullID || got[0].Backend != "pi-prompt" {
		t.Errorf("event 0 identity = %+v", got[0])
	}
	if got[0].TokensIn != 1000 || got[0].TokensOut != 500 || got[0].USD != 2.0 {
		t.Errorf("event 0 = %+v, want in=1000 out=500 usd=2.0", got[0])
	}
	// Delta, not cumulative: the second call alone is 2000 in at 1/K.
	if got[1].TokensIn != 2000 || got[1].TokensOut != 0 || got[1].USD != 2.0 {
		t.Errorf("event 1 = %+v, want in=2000 out=0 usd=2.0", got[1])
	}
	if got[0].At.IsZero() {
		t.Error("event At not set")
	}
}

func TestUsageEventHookNilSafe(t *testing.T) {
	mgr := newTestManager(t)
	s := newTestSession(t, mgr, "hk02")
	if err := mgr.AddUsage(s.FullID, 10, 10, CostRate{}); err != nil {
		t.Fatalf("AddUsage with no hook: %v", err)
	}
	mgr.SetUsageEventHook(func(UsageEvent) {})
	mgr.SetUsageEventHook(nil)
	if err := mgr.AddUsage(s.FullID, 10, 10, CostRate{}); err != nil {
		t.Fatalf("AddUsage after clearing hook: %v", err)
	}
}
