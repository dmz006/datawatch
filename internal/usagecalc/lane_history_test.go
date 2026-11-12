package usagecalc

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/capacity"
)

var laneTestNow = time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)

// fakeLaneCounter is an injected counter the tests can change.
type fakeLaneCounter struct {
	mu    sync.Mutex
	count int
	ok    bool
}

func (c *fakeLaneCounter) Lanes() (int, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.count, c.ok
}

func (c *fakeLaneCounter) set(n int, ok bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.count, c.ok = n, ok
}

func claudePoolSet(names ...string) map[string]struct{} {
	m := make(map[string]struct{}, len(names))
	for _, n := range names {
		m[n] = struct{}{}
	}
	return m
}

func openLaneTest(t *testing.T, dir string, clock *fakeClock, counter LaneCounter) *LaneHistory {
	t.Helper()
	h, err := OpenLaneHistory(dir, clock.Now, counter, WithLaneLogger(t.Logf))
	if err != nil {
		t.Fatalf("OpenLaneHistory: %v", err)
	}
	return h
}

func TestClaudeLanesCountsHoldersAcrossClaudePools(t *testing.T) {
	pools := claudePoolSet("llm:claude-a", "llm:claude-b")
	leases := []capacity.Lease{
		{Holder: "h1", Pools: []string{"host:one", "llm:claude-a"}},
		{Holder: "h2", Pools: []string{"llm:claude-b"}},
		{Holder: "h3", Pools: []string{"llm:claude-a", "llm:claude-b"}},
	}
	if got := ClaudeLanes(leases, pools); got != 3 {
		t.Fatalf("ClaudeLanes = %d, want 3", got)
	}
}

func TestClaudeLanesSkipsLeaseWithoutClaudePool(t *testing.T) {
	pools := claudePoolSet("llm:claude-a")
	leases := []capacity.Lease{
		{Holder: "h1", Pools: []string{"llm:claude-a"}},
		{Holder: "h2", Pools: []string{"llm:qwen", "node:gpu"}},
		{Holder: "h3"},
	}
	if got := ClaudeLanes(leases, pools); got != 1 {
		t.Fatalf("ClaudeLanes = %d, want 1", got)
	}
}

func TestClaudeLanesCountsDuplicateHolderOnce(t *testing.T) {
	pools := claudePoolSet("llm:claude-a", "llm:claude-b")
	leases := []capacity.Lease{
		{Holder: "h1", Pools: []string{"llm:claude-a"}},
		{Holder: "h1", Pools: []string{"llm:claude-b"}},
		{Holder: "h2", Pools: []string{"llm:claude-a"}},
	}
	if got := ClaudeLanes(leases, pools); got != 2 {
		t.Fatalf("ClaudeLanes = %d, want 2", got)
	}
	if got := ClaudeLanes(nil, pools); got != 0 {
		t.Fatalf("ClaudeLanes(nil) = %d, want 0", got)
	}
}

func TestLaneHistoryRecordsOnChange(t *testing.T) {
	clock := newFakeClock(laneTestNow)
	counter := &fakeLaneCounter{count: 2, ok: true}
	h := openLaneTest(t, t.TempDir(), clock, counter)

	if !h.Sample() {
		t.Fatal("first sample was not recorded")
	}
	clock.Advance(5 * time.Second)
	counter.set(3, true)
	if !h.Sample() {
		t.Fatal("a changed count was not recorded")
	}
	got := h.Samples()
	if len(got) != 2 || got[0].Count != 2 || got[1].Count != 3 {
		t.Fatalf("samples = %+v, want counts 2 then 3", got)
	}
}

func TestLaneHistoryCadence(t *testing.T) {
	clock := newFakeClock(laneTestNow)
	counter := &fakeLaneCounter{count: 2, ok: true}
	h := openLaneTest(t, t.TempDir(), clock, counter)

	h.Sample()
	clock.Advance(LaneSampleInterval - time.Second)
	if h.Sample() {
		t.Fatal("sample recorded before the cadence elapsed with an unchanged count")
	}
	clock.Advance(time.Second)
	if !h.Sample() {
		t.Fatal("sample not recorded once the cadence elapsed")
	}
	if n := len(h.Samples()); n != 2 {
		t.Fatalf("got %d samples, want 2", n)
	}
}

func TestLaneHistoryNoCounterRecordsNothing(t *testing.T) {
	clock := newFakeClock(laneTestNow)
	counter := &fakeLaneCounter{count: 4, ok: false}
	h := openLaneTest(t, t.TempDir(), clock, counter)
	if h.Sample() {
		t.Fatal("sample recorded while no ledger is wired")
	}
	if n := len(h.Samples()); n != 0 {
		t.Fatalf("got %d samples, want 0", n)
	}
}

func TestLaneHistoryLookupWindow(t *testing.T) {
	clock := newFakeClock(laneTestNow)
	counter := &fakeLaneCounter{count: 2, ok: true}
	h := openLaneTest(t, t.TempDir(), clock, counter)
	h.Sample()
	clock.Advance(30 * time.Second)
	counter.set(3, true)
	h.Sample()
	second := laneTestNow.Add(30 * time.Second)

	cases := []struct {
		name  string
		at    time.Time
		count int
		ok    bool
	}{
		{"at the first sample", laneTestNow, 2, true},
		{"between samples uses the older one", laneTestNow.Add(10 * time.Second), 2, true},
		{"at the second sample", second, 3, true},
		{"exactly two minutes after", second.Add(LaneLookupWindow), 3, true},
		{"just over two minutes after", second.Add(LaneLookupWindow + time.Second), 0, false},
	}
	for _, c := range cases {
		count, ok := h.Lookup(c.at)
		if count != c.count || ok != c.ok {
			t.Errorf("%s: Lookup = (%d, %v), want (%d, %v)", c.name, count, ok, c.count, c.ok)
		}
	}
}

func TestLaneHistoryLookupBeforeFirstSample(t *testing.T) {
	clock := newFakeClock(laneTestNow)
	counter := &fakeLaneCounter{count: 2, ok: true}
	h := openLaneTest(t, t.TempDir(), clock, counter)
	if _, ok := h.Lookup(laneTestNow); ok {
		t.Fatal("lookup on an empty history reported a count")
	}
	h.Sample()
	if _, ok := h.Lookup(laneTestNow.Add(-time.Second)); ok {
		t.Fatal("lookup before the first sample reported a count")
	}
}

func TestLaneHistoryPrunesPastRetention(t *testing.T) {
	clock := newFakeClock(laneTestNow)
	counter := &fakeLaneCounter{count: 2, ok: true}
	h := openLaneTest(t, t.TempDir(), clock, counter)
	h.Sample()

	clock.Advance(ReadingRetention + time.Minute)
	counter.set(1, true)
	h.Sample()

	got := h.Samples()
	if len(got) != 1 || got[0].Count != 1 {
		t.Fatalf("samples = %+v, want only the new one", got)
	}
	if _, ok := h.Lookup(laneTestNow); ok {
		t.Fatal("a pruned sample still answers a lookup")
	}
}

func TestLaneHistoryPersistsAcrossReopen(t *testing.T) {
	dir := t.TempDir()
	clock := newFakeClock(laneTestNow)
	counter := &fakeLaneCounter{count: 2, ok: true}
	h := openLaneTest(t, dir, clock, counter)
	h.Sample()
	clock.Advance(20 * time.Second)
	counter.set(3, true)
	h.Sample()

	if _, err := os.Stat(filepath.Join(dir, storeDirName, laneFileName)); err != nil {
		t.Fatalf("history file not written next to the usage store: %v", err)
	}

	again := openLaneTest(t, dir, clock, counter)
	if got := again.Samples(); len(got) != 2 || got[0].Count != 2 || got[1].Count != 3 {
		t.Fatalf("reopened samples = %+v, want counts 2 then 3", got)
	}
	if count, ok := again.Lookup(laneTestNow.Add(30 * time.Second)); !ok || count != 3 {
		t.Fatalf("Lookup after reopen = (%d, %v), want (3, true)", count, ok)
	}
}

func TestLaneHistoryMovesUnreadableFileAside(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, storeDirName, laneFileName)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	h := openLaneTest(t, dir, newFakeClock(laneTestNow), &fakeLaneCounter{ok: true})
	if n := len(h.Samples()); n != 0 {
		t.Fatalf("got %d samples from a bad file, want 0", n)
	}
	if _, err := os.Stat(path + ".bad"); err != nil {
		t.Fatalf("bad file was not kept: %v", err)
	}
}

func TestLaneHistoryRunStopsOnCancel(t *testing.T) {
	clock := newFakeClock(laneTestNow)
	counter := &fakeLaneCounter{count: 2, ok: true}
	h := openLaneTest(t, t.TempDir(), clock, counter)
	h.tick = time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		h.Run(ctx)
		close(done)
	}()

	deadline := time.After(5 * time.Second)
	for len(h.Samples()) == 0 {
		select {
		case <-deadline:
			cancel()
			t.Fatal("Run recorded no sample")
		case <-time.After(time.Millisecond):
		}
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after the context was cancelled")
	}
}
