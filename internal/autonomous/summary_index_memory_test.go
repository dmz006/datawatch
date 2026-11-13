// Memory guard for the summary index at 700 Automata.
//
// The fixture is built in memory (temp-dir store, no network, no git): 700
// Automata with 10 to 40 tasks each, long decision logs, session history and
// task outputs, so a full record is large and an index entry must not be.
// The tests assert a fixed per-entry and total byte bound, that an entry does
// not grow with the text of its record, and that heap growth after 5,000
// refreshes of one entry is bounded.

package autonomous

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"runtime"
	"strings"
	"testing"
	"time"
)

const (
	memIndexAutomata = 700
	// memEntryMaxBytes bounds one entry: its JSON form plus its list row.
	memEntryMaxBytes = 16 << 10
	// memIndexMaxHeapBytes bounds the heap the whole 700-entry index holds.
	memIndexMaxHeapBytes = 16 << 20
	// memRefreshes is how many times one entry is refreshed in the leak check.
	memRefreshes = 5000
	// memRefreshGrowthMax bounds heap growth across those refreshes.
	memRefreshGrowthMax = 2 << 20
)

func memText(rng *rand.Rand, n int) string {
	const words = "the task session output checkpoint verifier result error warning retry build compile test passed failed running waiting capacity backend model prompt transcript diff commit branch worktree story"
	w := strings.Fields(words)
	var b strings.Builder
	for b.Len() < n {
		b.WriteString(w[rng.Intn(len(w))])
		b.WriteByte(' ')
	}
	return b.String()[:n]
}

// memFixtureRecord builds one large record, deterministic in i.
func memFixtureRecord(rng *rand.Rand, i int) *PRD {
	base := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	created := base.Add(time.Duration(i) * 40 * time.Minute)
	id := fmt.Sprintf("%08x", 0x30000000+i)
	status := PRDCompleted
	switch i % 20 {
	case 0, 1, 2:
		status = PRDRunning
	case 3, 4:
		status = PRDCancelled
	case 5:
		status = PRDFailed
	}
	p := &PRD{
		ID: id, Title: fmt.Sprintf("memory fixture %03d", i), Spec: memText(rng, 3000),
		ProjectDir: fmt.Sprintf("/srv/mem/project-%d", i%3), Status: status,
		CreatedAt: created, UpdatedAt: created.Add(time.Hour), PlannerSessionID: fmt.Sprintf("plan-%04d", i),
	}
	nTasks := 10 + rng.Intn(31)
	nStories := 2 + rng.Intn(4)
	for s := 0; s < nStories; s++ {
		p.Story = append(p.Story, Story{ID: fmt.Sprintf("%s-s%d", id, s), PRDID: id, Description: memText(rng, 600)})
	}
	for k := 0; k < nTasks; k++ {
		st := &p.Story[k%nStories]
		started := created.Add(time.Duration(k) * 10 * time.Minute)
		done := started.Add(8 * time.Minute)
		t := Task{
			ID: fmt.Sprintf("%s-t%03x", id, k), StoryID: st.ID, PRDID: id, Status: TaskCompleted,
			Spec: memText(rng, 1200), Error: memText(rng, 800), StartFailureTail: []string{memText(rng, 1500)},
			SessionID: fmt.Sprintf("work-%04d-%03d", i, k), VerifierSessionID: fmt.Sprintf("ver-%04d-%03d", i, k),
			StartedAt: &started, CompletedAt: &done,
			Verification: &VerificationResult{OK: true, RawJudge: memText(rng, 1500), VerifiedAt: done},
		}
		st.Tasks = append(st.Tasks, t)
	}
	for d := 0; d < 120; d++ {
		p.Decisions = append(p.Decisions, Decision{
			At: created.Add(time.Duration(d) * time.Minute), Kind: "run", Actor: "autonomous",
			Note: memText(rng, 400), JudgeSessionID: fmt.Sprintf("judge-%04d-%03d", i, d%12),
		})
	}
	return p
}

// memFixtureStore returns a temp-dir store whose records are the fixture.
// The records are set in memory and nothing is written to disk.
func memFixtureStore(t testing.TB) *Store {
	t.Helper()
	s, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	rng := rand.New(rand.NewSource(20261113))
	s.mu.Lock()
	for i := 0; i < memIndexAutomata; i++ {
		p := memFixtureRecord(rng, i)
		s.prds[p.ID] = p
	}
	s.mu.Unlock()
	return s
}

func memHeap() uint64 {
	var ms runtime.MemStats
	runtime.GC()
	runtime.GC()
	runtime.ReadMemStats(&ms)
	return ms.HeapAlloc
}

// entryBytes is the JSON size of an entry plus its row (the row is not
// serialised with the entry).
func entryBytes(t testing.TB, e SummaryEntry) int {
	t.Helper()
	a, err := json.Marshal(e)
	if err != nil {
		t.Fatalf("marshal entry: %v", err)
	}
	b, err := json.Marshal(e.Row)
	if err != nil {
		t.Fatalf("marshal row: %v", err)
	}
	return len(a) + len(b)
}

func TestSummaryIndexMemoryEntryAndTotalBound(t *testing.T) {
	s := memFixtureStore(t)

	recordBytes := 0
	s.mu.Lock()
	for _, p := range s.prds {
		b, _ := json.Marshal(p)
		recordBytes += len(b)
	}
	s.mu.Unlock()
	if avg := recordBytes / memIndexAutomata; avg < 50<<10 {
		t.Fatalf("fixture records average only %d bytes; they must be large", avg)
	}

	before := memHeap()
	s.mu.Lock()
	s.rebuildSummaryLocked()
	s.mu.Unlock()
	after := memHeap()

	entries := s.SummaryEntries()
	if len(entries) != memIndexAutomata {
		t.Fatalf("index holds %d entries, want %d", len(entries), memIndexAutomata)
	}
	total, largest := 0, 0
	for _, e := range entries {
		n := entryBytes(t, e)
		total += n
		if n > largest {
			largest = n
		}
		if n > memEntryMaxBytes {
			t.Fatalf("entry %s is %d bytes, per-entry bound %d", e.ID, n, memEntryMaxBytes)
		}
	}
	if total > memIndexAutomata*memEntryMaxBytes {
		t.Fatalf("index encodes to %d bytes, total bound %d", total, memIndexAutomata*memEntryMaxBytes)
	}
	var grown uint64
	if after > before {
		grown = after - before
	}
	if grown > memIndexMaxHeapBytes {
		t.Fatalf("the index holds %d heap bytes, bound %d", grown, memIndexMaxHeapBytes)
	}
	if uint64(recordBytes) > 0 && grown*4 > uint64(recordBytes) {
		t.Errorf("the index holds %d heap bytes against %d bytes of records: it must stay a small fraction", grown, recordBytes)
	}
	t.Logf("records %d MB; index %d entries, encoded %d KB (largest %d B), heap %d KB",
		recordBytes>>20, len(entries), total>>10, largest, grown>>10)
}

// An entry never grows with the amount of spec, decision or output text.
func TestSummaryIndexMemoryEntryIgnoresRecordText(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	p := memFixtureRecord(rng, 1)
	small := entryBytes(t, buildSummaryEntry(p))
	p.Spec += memText(rng, 2<<20)
	for i := range p.Story {
		for j := range p.Story[i].Tasks {
			p.Story[i].Tasks[j].Spec += memText(rng, 20000)
			p.Story[i].Tasks[j].StartFailureTail = append(p.Story[i].Tasks[j].StartFailureTail, memText(rng, 20000))
		}
	}
	for i := 0; i < 200; i++ {
		p.Decisions = append(p.Decisions, Decision{Kind: "run", Note: memText(rng, 2000)})
	}
	big := entryBytes(t, buildSummaryEntry(p))
	if big > small+2048 {
		t.Fatalf("entry grew from %d to %d bytes when only record text grew", small, big)
	}
}

// 5,000 refreshes of one entry must not leak: the entry count stays, the
// revision counts every refresh, and the heap stays bounded.
func TestSummaryIndexMemoryRefreshNoLeak(t *testing.T) {
	s := memFixtureStore(t)
	s.mu.Lock()
	s.rebuildSummaryLocked()
	var id string
	for k := range s.prds {
		if id == "" || k < id {
			id = k
		}
	}
	s.mu.Unlock()

	rev0 := s.Revision()
	// Warm up so one-time allocations are not counted as growth.
	for i := 0; i < 200; i++ {
		s.mu.Lock()
		s.refreshSummaryLocked(id)
		s.mu.Unlock()
	}
	before := memHeap()
	for i := 0; i < memRefreshes; i++ {
		s.mu.Lock()
		s.refreshSummaryLocked(id)
		s.mu.Unlock()
	}
	after := memHeap()

	if got := len(s.SummaryEntries()); got != memIndexAutomata {
		t.Fatalf("index holds %d entries after the refreshes, want %d", got, memIndexAutomata)
	}
	if got := s.Revision() - rev0; got != uint64(200+memRefreshes) {
		t.Fatalf("revision moved %d, want %d", got, 200+memRefreshes)
	}
	var grown uint64
	if after > before {
		grown = after - before
	}
	if grown > memRefreshGrowthMax {
		t.Fatalf("heap grew %d bytes over %d refreshes of one entry, bound %d", grown, memRefreshes, memRefreshGrowthMax)
	}
	t.Logf("heap growth over %d refreshes: %d KB", memRefreshes, grown>>10)
}
