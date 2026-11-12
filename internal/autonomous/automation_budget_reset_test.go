package autonomous

import (
	"strings"
	"testing"
	"time"
)

// lifetimePolicy is a $10 lifetime ceiling on the claude backend, already
// breached by a spend entry of usd.
func breachedLifetime(f *budgetEvalFixture, usd float64) (*Automation, *BudgetPolicy) {
	p := &BudgetPolicy{CeilingType: "dollar", CeilingValue: 10, Window: "automation", Backend: "claude"}
	a := evalAutomation(p, AutomationMember{ID: "m1", PRDID: "p1"})
	f.entries = append(f.entries, fakeSpendEntry{at: f.clk.Now().Add(-time.Hour), member: "m1", llm: "claude", usd: usd})
	return a, p
}

func TestBudgetResetKnownForDayWindow(t *testing.T) {
	f := newBudgetEvalFixture(t)
	f.b.Resets = f.b.ResetFor
	a := evalAutomation(dayPolicy(10), AutomationMember{ID: "m1", PRDID: "p1"})
	f.spend("m1", f.clk.Now().Add(-time.Hour), 12)

	v := f.b.Evaluate(a, nil, false)
	want := time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC)
	if !v.Gate || !v.ResetKnown || v.ResetAt == nil || !v.ResetAt.Equal(want) || v.ResetSource != BudgetResetWindow {
		t.Fatalf("verdict = %+v, reset = %v", v, v.ResetAt)
	}
}

func TestBudgetResetKnownForAnchoredWeek(t *testing.T) {
	f := newBudgetEvalFixture(t)
	f.b.Resets = f.b.ResetFor
	p := &BudgetPolicy{CeilingType: "dollar", CeilingValue: 10, Window: "week",
		ResetWeekday: "wednesday", ResetTime: "09:30", ResetZone: "America/New_York"}
	a := evalAutomation(p, AutomationMember{ID: "m1", PRDID: "p1"})
	// Thursday 2026-10-22 12:00 UTC is 08:00 EDT, so the window began Wednesday
	// 10-21 09:30 EDT and ends the next Wednesday 09:30 EDT (13:30 UTC).
	f.spend("m1", time.Date(2026, 10, 21, 15, 0, 0, 0, time.UTC), 11)

	v := f.b.Evaluate(a, nil, false)
	want := time.Date(2026, 10, 28, 13, 30, 0, 0, time.UTC)
	if !v.Gate || !v.ResetKnown || v.ResetAt == nil || !v.ResetAt.Equal(want) || v.ResetSource != BudgetResetWindow {
		t.Fatalf("verdict = %+v, reset = %v, want %v", v, v.ResetAt, want)
	}
}

func TestBudgetResetUnknownForLifetimeWindow(t *testing.T) {
	f := newBudgetEvalFixture(t)
	f.b.Resets = f.b.ResetFor
	a, _ := breachedLifetime(f, 12)

	// No back-off entry at all, and an entry for another backend.
	f.b.Backoffs = func() []Entry {
		return []Entry{{Backend: "other", Until: f.clk.Now().Add(time.Hour), Reason: "rate-limit — reset 13:00", Source: BackoffSourceRateLimit}}
	}
	v := f.b.Evaluate(a, nil, false)
	if !v.Gate || v.ResetKnown || v.ResetAt != nil || v.ResetSource != "" {
		t.Fatalf("verdict = %+v, want an unknown reset", v)
	}
	f.b.Backoffs = nil
	if v := f.b.Evaluate(a, nil, false); !v.Gate || v.ResetKnown {
		t.Fatalf("verdict without back-off source = %+v", v)
	}
}

func TestBudgetResetFromParsedRateLimit(t *testing.T) {
	f := newBudgetEvalFixture(t)
	f.b.Resets = f.b.ResetFor
	a, _ := breachedLifetime(f, 12)
	bo := NewBackoff()
	bo.Clock = f.clk.Now
	until := f.clk.Now().Add(90 * time.Minute)
	bo.Set("claude", until, "rate-limit — reset 13:30", BackoffSourceRateLimit)
	f.b.Backoffs = bo.Live

	v := f.b.Evaluate(a, nil, false)
	if !v.Gate || !v.ResetKnown || v.ResetAt == nil || !v.ResetAt.Equal(until) || v.ResetSource != BudgetResetRateLimit {
		t.Fatalf("verdict = %+v, reset = %v", v, v.ResetAt)
	}

	// Once the entry has expired the reset is unknown again.
	f.clk.Advance(2 * time.Hour)
	if v := f.b.Evaluate(a, nil, false); v.ResetKnown {
		t.Fatalf("expired back-off still gives a known reset: %+v", v)
	}
}

func TestBudgetResetIgnoresFallbackBackoff(t *testing.T) {
	f := newBudgetEvalFixture(t)
	f.b.Resets = f.b.ResetFor
	a, _ := breachedLifetime(f, 12)
	bo := NewBackoff()
	bo.Clock = f.clk.Now
	f.b.Backoffs = bo.Live

	// The configured maximum back-off, used when the message had no reset time.
	bo.Set("claude", f.clk.Now().Add(time.Hour), BackoffReasonNoResetTime, BackoffSourceRateLimit)
	if v := f.b.Evaluate(a, nil, false); !v.Gate || v.ResetKnown || v.ResetAt != nil {
		t.Fatalf("fallback back-off gave a known reset: %+v", v)
	}
	// An operator-set back-off is not a parsed reset either.
	bo.Set("claude", f.clk.Now().Add(time.Hour), "paused by operator", BackoffSourceOperator)
	if v := f.b.Evaluate(a, nil, false); v.ResetKnown {
		t.Fatalf("operator back-off gave a known reset: %+v", v)
	}
}

// peerEvalFixture is a day policy on claude opting in "edge", with edge and a
// registered "other" peer answering the usage endpoint.
func peerEvalFixture(t *testing.T, edgeSpend, otherSpend float64) (*budgetEvalFixture, *Automation, *fakePeer, *fakePeer) {
	t.Helper()
	f := newBudgetEvalFixture(t)
	edge, other := newFakePeer(t, edgeSpend), newFakePeer(t, otherSpend)
	peers := map[string]*fakePeer{"edge": edge, "other": other}
	f.b.Peers = &PeerBudget{
		Clock: f.clk.Now,
		Lookup: func(name string) (string, string, bool) {
			p, ok := peers[name]
			if !ok {
				return "", "", false
			}
			return p.srv.URL, "tok", true
		},
		KnownPeers: func() []string { return []string{"edge", "other"} },
	}
	p := dayPolicy(10)
	p.Backend = "claude"
	p.PeerWindow = []string{"edge"}
	a := evalAutomation(p, AutomationMember{ID: "m1", PRDID: "p1"})
	f.entries = append(f.entries, fakeSpendEntry{at: f.clk.Now().Add(-time.Hour), member: "m1", llm: "claude", usd: 6})
	return f, a, edge, other
}

func TestBudgetEvalPeerNotOptedInIgnored(t *testing.T) {
	f, a, edge, other := peerEvalFixture(t, 1, 500)

	v := f.b.Evaluate(a, nil, false)
	if v.Gate || v.SpentUSD != 7 || v.PeerSpendUSD != 1 {
		t.Fatalf("verdict = %+v, want only the opted-in peer counted", v)
	}
	if other.calls.Load() != 0 {
		t.Fatalf("peer that is not opted in was contacted %d times", other.calls.Load())
	}
	var found bool
	for _, l := range v.Peers {
		if l.Peer == "other" {
			found = true
			if l.Counted || l.SpendUSD != 0 || l.Reason != peerReasonNotOptedIn {
				t.Errorf("not-opted-in line = %+v", l)
			}
		}
	}
	if !found {
		t.Errorf("registered peer missing from lines: %+v", v.Peers)
	}

	// With no peer named at all nobody is queried.
	calls := edge.calls.Load()
	a.BudgetPolicy.PeerWindow = nil
	if v := f.b.Evaluate(a, nil, false); v.SpentUSD != 6 || len(v.Peers) != 0 {
		t.Fatalf("verdict without opt-in = %+v", v)
	}
	if edge.calls.Load() != calls || other.calls.Load() != 0 {
		t.Fatalf("peers contacted without an opt-in")
	}
}

func TestBudgetEvalPeerOptedInCounts(t *testing.T) {
	f, a, edge, _ := peerEvalFixture(t, 5, 0)

	v := f.b.Evaluate(a, nil, false)
	if !v.Gate || v.Kind != BudgetKindCeiling || v.SpentUSD != 11 || v.PeerSpendUSD != 5 {
		t.Fatalf("verdict = %+v, want local $6 + peer $5 over the $10 ceiling", v)
	}
	edge.mu.Lock()
	q := edge.query
	edge.mu.Unlock()
	if q["llm"] != "claude" || q["from"] != "2026-10-22T00:00:00Z" || q["to"] != "2026-10-22T12:00:00Z" {
		t.Errorf("peer asked for %v", q)
	}

	// A repeat within the cache interval does not ask the peer again.
	f.b.Evaluate(a, nil, false)
	if edge.calls.Load() != 1 {
		t.Errorf("peer called %d times, want the cached answer reused", edge.calls.Load())
	}
}

func TestBudgetPeerStatusStatesNoAutoConfirmation(t *testing.T) {
	f, a, _, _ := peerEvalFixture(t, 2, 0)
	const wording = "counted because the operator opted this peer in; there is no automatic confirmation that the peer shares the account, because the resource fingerprint exchange does not exist yet"

	v := f.b.Evaluate(a, nil, false)
	var edge *PeerBudgetStatus
	for i := range v.Peers {
		if v.Peers[i].Peer == "edge" {
			edge = &v.Peers[i]
		}
	}
	if edge == nil || !edge.Counted || !strings.Contains(edge.Note, wording) {
		t.Fatalf("edge line = %+v, want the fixed wording", edge)
	}
}

func TestBudgetEvalPeerUnreachableFailsOpen(t *testing.T) {
	f, a, edge, _ := peerEvalFixture(t, 50, 0)
	edge.status = 500

	v := f.b.Evaluate(a, nil, false)
	if v.Gate || v.SpentUSD != 6 || v.PeerSpendUSD != 0 {
		t.Fatalf("verdict = %+v, want the unreachable peer to count as zero", v)
	}
	var line *PeerBudgetStatus
	for i := range v.Peers {
		if v.Peers[i].Peer == "edge" {
			line = &v.Peers[i]
		}
	}
	if line == nil || line.Counted || !strings.Contains(line.Reason, peerReasonUnreach) {
		t.Fatalf("edge line = %+v", line)
	}

	// Once the peer answers again, after the cache interval, it counts.
	edge.status = 200
	f.clk.Advance(peerSpendTTL + time.Second)
	if v := f.b.Evaluate(a, nil, false); !v.Gate || v.PeerSpendUSD != 50 {
		t.Fatalf("verdict after recovery = %+v", v)
	}
}

func TestBudgetEvalPeerNeedsBackend(t *testing.T) {
	f, a, edge, _ := peerEvalFixture(t, 50, 0)
	a.BudgetPolicy.Backend = ""

	v := f.b.Evaluate(a, nil, false)
	if v.PeerSpendUSD != 0 || len(v.Peers) != 0 || !strings.Contains(v.PeerNote, "no backend") {
		t.Fatalf("verdict = %+v, want a note and no peer spend", v)
	}
	if edge.calls.Load() != 0 {
		t.Fatal("peer contacted without a backend")
	}
}
