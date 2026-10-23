package capacity

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func rateLedger(clk *fakeClock) *Ledger {
	return New(Options{PollInterval: time.Hour, Clock: clk})
}

func tryAcquire(l *Ledger, holder string, pools ...string) error {
	return l.Acquire(context.Background(), Request{Holder: holder, Pools: pools}, 0, func() bool { return true }, nil)
}

// A held request that cannot be granted immediately is queued; this reports
// whether holder is in the wait queue.
func queued(l *Ledger, holder string) bool {
	for _, w := range l.Snapshot().Waiting {
		if w.Holder == holder {
			return true
		}
	}
	return false
}

func TestRateRequestsCeilingAdmitsThenQueuesThenWindowFrees(t *testing.T) {
	clk := newFakeClock()
	l := rateLedger(clk)
	l.SetRate("llm", RateCeiling{Requests: 2, Window: time.Minute})
	start := clk.Now()
	for _, h := range []string{"a", "b"} {
		if err := tryAcquire(l, h, "llm"); err != nil {
			t.Fatalf("%s within ceiling: %v", h, err)
		}
		clk.Advance(10 * time.Second)
	}
	l.Release("a")
	l.Release("b")

	done := make(chan error, 1)
	go func() {
		done <- l.Acquire(context.Background(), Request{Holder: "c", Pools: []string{"llm"}}, 0, nil, nil)
	}()
	waitPending(t, clk, 1)
	deadline := time.Now().Add(2 * time.Second)
	for !queued(l, "c") && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if !queued(l, "c") {
		t.Fatal("third request must queue at the ceiling")
	}
	w := l.Snapshot().Waiting[0]
	if !strings.HasPrefix(w.Reason, "pool llm rate ceiling:") || !strings.Contains(w.Reason, "requests 2/2") {
		t.Fatalf("reason = %q", w.Reason)
	}
	// Oldest entry (at start) leaves the window at start+1m; clock is at +20s.
	clk.Advance(39 * time.Second)
	if queued(l, "c") == false {
		t.Fatal("must still be queued before the window boundary")
	}
	clk.Advance(time.Second) // now == start+60s: boundary passed
	if err := <-done; err != nil {
		t.Fatalf("waiter should be admitted after the window: %v", err)
	}
	if !l.Held("c") {
		t.Fatal("c must hold a lease")
	}
	_ = start
}

func TestRateTokensCeilingWindowFrees(t *testing.T) {
	clk := newFakeClock()
	l := rateLedger(clk)
	l.SetRate("llm", RateCeiling{Tokens: 1000, Window: time.Minute})
	if err := tryAcquire(l, "a", "llm"); err != nil {
		t.Fatal(err)
	}
	l.Release("a")
	l.RecordTokens("llm", 600)
	clk.Advance(20 * time.Second)
	l.RecordTokens("llm", 500)

	done := make(chan error, 1)
	go func() {
		done <- l.Acquire(context.Background(), Request{Holder: "b", Pools: []string{"llm"}}, 0, nil, nil)
	}()
	deadline := time.Now().Add(2 * time.Second)
	for !queued(l, "b") && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	w := l.Snapshot().Waiting
	if len(w) != 1 || !strings.Contains(w[0].Reason, "tokens 1100/1000") {
		t.Fatalf("waiting = %+v", w)
	}
	// 600 leaves at +60s, leaving 500 < 1000.
	clk.Advance(40 * time.Second)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestRateBothDimensionsNameBlockingOne(t *testing.T) {
	clk := newFakeClock()
	l := rateLedger(clk)
	l.SetRate("llm", RateCeiling{Requests: 5, Tokens: 100, Window: time.Minute})
	l.RecordTokens("llm", 100)
	r := l.blockReasonLocked2(Request{Pools: []string{"llm"}})
	if !strings.Contains(r, "tokens 100/100") || strings.Contains(r, "requests") {
		t.Fatalf("tokens must block: %q", r)
	}

	l2 := rateLedger(newFakeClock())
	l2.SetRate("llm", RateCeiling{Requests: 1, Tokens: 100, Window: time.Minute})
	if err := tryAcquire(l2, "a", "llm"); err != nil {
		t.Fatal(err)
	}
	r = l2.blockReasonLocked2(Request{Pools: []string{"llm"}})
	if !strings.Contains(r, "requests 1/1") || strings.Contains(r, "tokens") {
		t.Fatalf("requests must block: %q", r)
	}
}

// blockReasonLocked2 is a locking wrapper for tests.
func (l *Ledger) blockReasonLocked2(req Request) string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.blockReasonLocked(req)
}

func TestRateResetAtReportsOldestEntryLeaving(t *testing.T) {
	clk := newFakeClock()
	l := rateLedger(clk)
	if _, ok := l.RateResetAt("llm"); ok {
		t.Fatal("no ceiling: no reset")
	}
	l.SetRate("llm", RateCeiling{Requests: 2, Tokens: 50, Window: time.Minute})
	start := clk.Now()
	_ = tryAcquire(l, "a", "llm")
	l.Release("a")
	clk.Advance(15 * time.Second)
	_ = tryAcquire(l, "b", "llm")
	l.Release("b")

	at, ok := l.RateResetAt("llm")
	if !ok || !at.Equal(start.Add(time.Minute)) {
		t.Fatalf("reset = %v %v, want %v", at, ok, start.Add(time.Minute))
	}
	// Tokens full too, with its own later reset: the pool frees only when both do.
	l.RecordTokens("llm", 50)
	clk.Advance(5 * time.Second)
	at, ok = l.RateResetAt("llm")
	if want := start.Add(15*time.Second + time.Minute); !ok || !at.Equal(want) {
		t.Fatalf("reset = %v, want %v", at, want)
	}
	var ps PoolStatus
	for _, p := range l.Snapshot().Pools {
		if p.Name == "llm" {
			ps = p
		}
	}
	if !ps.RateResetsAt.Equal(at) || ps.RequestsInWindow != 2 || ps.TokensInWindow != 50 || ps.RateWindowSeconds != 60 {
		t.Fatalf("pool status = %+v", ps)
	}
	if !strings.Contains(FormatPools([]PoolStatus{ps}), "rate req 2/2 tok 50/50 per 1m0s") {
		t.Fatalf("format = %q", FormatPools([]PoolStatus{ps}))
	}
}

func TestRateSetZeroClearsAndRedispatches(t *testing.T) {
	clk := newFakeClock()
	l := rateLedger(clk)
	l.SetRate("llm", RateCeiling{Requests: 1, Window: time.Hour})
	_ = tryAcquire(l, "a", "llm")
	l.Release("a")
	done := make(chan error, 1)
	go func() {
		done <- l.Acquire(context.Background(), Request{Holder: "b", Pools: []string{"llm"}}, 0, nil, nil)
	}()
	deadline := time.Now().Add(2 * time.Second)
	for !queued(l, "b") && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if !queued(l, "b") {
		t.Fatal("b must queue")
	}
	l.SetRate("llm", RateCeiling{})
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if got := l.RateOf("llm"); got != (RateCeiling{}) {
		t.Fatalf("RateOf = %+v", got)
	}
	b, _ := json.Marshal(l.Snapshot())
	if strings.Contains(string(b), "rate_") {
		t.Fatalf("cleared pool must not serialise rate fields: %s", b)
	}
}

func TestRateAdoptRecordsRequest(t *testing.T) {
	clk := newFakeClock()
	l := rateLedger(clk)
	l.SetRate("llm", RateCeiling{Requests: 1, Window: time.Minute})
	l.Adopt(Request{Holder: "a", Pools: []string{"llm"}}, "s1")
	l.Release("a")
	if r := l.blockReasonLocked2(Request{Pools: []string{"llm"}}); !strings.HasPrefix(r, "pool llm rate ceiling:") {
		t.Fatalf("adopted lease must count: %q", r)
	}
}

func TestRateNoCeilingSnapshotUnchanged(t *testing.T) {
	clk := newFakeClock()
	l := rateLedger(clk)
	l.SetLimit("node:a", 2)
	_ = tryAcquire(l, "t1", "node:a", "host")
	l.RecordTokens("node:a", 999) // no ceiling: ignored
	b, err := json.Marshal(l.Snapshot())
	if err != nil {
		t.Fatal(err)
	}
	want := `{"pools":[{"name":"host","limit":0,"external":0,"held":1,"holders":["t1"]},` +
		`{"name":"node:a","limit":2,"external":0,"held":1,"holders":["t1"]}],` +
		`"leases":[{"holder":"t1","prd_id":"","pools":["node:a","host"],"acquired":"2026-10-19T12:00:00Z"}],"waiting":null}`
	if string(b) != want {
		t.Fatalf("snapshot changed:\n got %s\nwant %s", b, want)
	}
	if strings.Contains(FormatPools(l.Snapshot().Pools), "rate") {
		t.Fatal("no rate segment without a ceiling")
	}
}
