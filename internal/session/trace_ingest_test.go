package session

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/trace"
	"github.com/dmz006/datawatch/internal/trace/adapters"
)

var traceFixedNow = time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)

type fakeTraceSink struct {
	mu  sync.Mutex
	evs []trace.Event
}

func (f *fakeTraceSink) Append(evs ...trace.Event) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.evs = append(f.evs, evs...)
	return len(evs), nil
}

func (f *fakeTraceSink) all() []trace.Event {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]trace.Event(nil), f.evs...)
}

type fakeTimeline struct {
	mu    sync.Mutex
	lines []string
}

func (f *fakeTimeline) add(id, line string) {
	f.mu.Lock()
	f.lines = append(f.lines, id+" :: "+line)
	f.mu.Unlock()
}

func piFixtureLines(t *testing.T) []string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "trace", "adapters", "testdata", "pi.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	return strings.SplitAfter(strings.TrimRight(string(b), "\n"), "\n")
}

func newTestIngestor(t *testing.T, mode string, locate func(Session) string) (*TraceIngestor, *fakeTraceSink, *fakeTimeline) {
	t.Helper()
	sink, tl := &fakeTraceSink{}, &fakeTimeline{}
	ing := NewTraceIngestor(TraceIngestConfig{
		Sink: sink, Settings: trace.Settings{Mode: mode}, Locate: locate,
		Timeline: tl.add, Now: func() time.Time { return traceFixedNow },
	})
	ing.Start()
	t.Cleanup(ing.Close)
	return ing, sink, tl
}

func piSession() Session {
	return Session{FullID: "h-ab12", BackendFamily: "pi", TaskID: "t1", StoryID: "s1", PRDID: "a1", State: StateRunning}
}

func TestTraceIngestIncrementalThenFinal(t *testing.T) {
	lines := piFixtureLines(t)
	path := filepath.Join(t.TempDir(), "pi.jsonl")
	if err := os.WriteFile(path, []byte(strings.Join(lines[:2], "")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ing, sink, tl := newTestIngestor(t, "summary", func(Session) string { return path })
	s := piSession()
	ing.SessionStarted(s)
	ing.Poll()
	ing.Flush()
	first := len(sink.all())
	if first == 0 {
		t.Fatal("incremental ingest produced no events")
	}
	ing.Poll() // nothing new
	ing.Flush()
	if got := len(sink.all()); got != first {
		t.Fatalf("re-poll duplicated events: %d -> %d", first, got)
	}
	f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	_, _ = f.WriteString(strings.Join(lines[2:], "") + "\n")
	_ = f.Close()
	s.State = StateComplete
	ing.SessionEnded(s)
	ing.Flush()
	all := sink.all()
	if len(all) <= first {
		t.Fatalf("final ingest added nothing: %d", len(all))
	}
	if all[0].SessionID != s.FullID || all[0].TaskID != "t1" || all[0].StoryID != "s1" || all[0].AutomatonID != "a1" {
		t.Errorf("identity not stamped: %+v", all[0])
	}
	if len(tl.lines) != len(all) {
		t.Errorf("timeline has %d lines for %d events", len(tl.lines), len(all))
	}
	ing.Poll()
	ing.Flush()
	if len(sink.all()) != len(all) {
		t.Error("an ended session was polled again")
	}
}

func TestTraceIngestFallbackWithoutTranscript(t *testing.T) {
	ing, sink, tl := newTestIngestor(t, "summary", func(Session) string { return "" })
	s := piSession()
	s.State = StateFailed
	ing.SessionStarted(s)
	ing.SessionEnded(s)
	ing.Flush()
	all := sink.all()
	if len(all) != 1 || all[0].Kind != trace.KindStateChange || all[0].Name != string(StateFailed) ||
		all[0].ArgSummary != adapters.FallbackNote || !all[0].Timestamp.Equal(traceFixedNow) {
		t.Fatalf("fallback: %+v", all)
	}
	if len(tl.lines) != 1 || !strings.Contains(tl.lines[0], "| trace-state_change | failed") {
		t.Errorf("timeline: %v", tl.lines)
	}
}

func TestTraceRecordEventStampsAndAppendsTimeline(t *testing.T) {
	ing, sink, tl := newTestIngestor(t, "summary", func(Session) string { return "" })
	s := piSession()
	ing.SessionStarted(s)
	ing.RecordEvent(s.FullID, trace.Event{Kind: trace.KindCapabilityCheck, Name: "sessions.kill", Outcome: "denied\nsecond line"})
	ing.RecordEvent("h-unknown", trace.Event{Kind: trace.KindPermissionDecision, Name: "permission", Outcome: "relayed",
		Timestamp: traceFixedNow.Add(time.Minute)})
	ing.RecordEvent("", trace.Event{Kind: trace.KindToolCall}) // no identity: ignored
	ing.Flush()
	all := sink.all()
	if len(all) != 2 {
		t.Fatalf("events: %+v", all)
	}
	if all[0].SessionID != s.FullID || all[0].TaskID != "t1" || all[0].Backend != "pi" || !all[0].Timestamp.Equal(traceFixedNow) {
		t.Errorf("stamped event: %+v", all[0])
	}
	if all[1].TaskID != "" || !all[1].Timestamp.Equal(traceFixedNow.Add(time.Minute)) {
		t.Errorf("unknown session event: %+v", all[1])
	}
	want := "h-ab12 :: 2026-03-04T05:06:07Z | trace-capability_check | sessions.kill | outcome=denied second line"
	if tl.lines[0] != want {
		t.Errorf("timeline line\n got %q\nwant %q", tl.lines[0], want)
	}
}

func TestTraceIngestOffAndDisabledBackend(t *testing.T) {
	ing, sink, _ := newTestIngestor(t, "off", func(Session) string { return "" })
	s := piSession()
	ing.SessionStarted(s)
	ing.RecordEvent(s.FullID, trace.Event{Kind: trace.KindError})
	ing.SessionEnded(s)
	ing.Flush()
	if len(sink.all()) != 0 {
		t.Fatalf("mode off recorded %+v", sink.all())
	}

	sink2, tl2 := &fakeTraceSink{}, &fakeTimeline{}
	ing2 := NewTraceIngestor(TraceIngestConfig{Sink: sink2, Timeline: tl2.add,
		Settings: trace.Settings{Backends: map[string]bool{"pi": false}},
		Locate:   func(Session) string { return "" }, Now: func() time.Time { return traceFixedNow }})
	ing2.Start()
	defer ing2.Close()
	ing2.SessionStarted(s)
	ing2.SessionEnded(s)
	ing2.Flush()
	if len(sink2.all()) != 0 {
		t.Fatalf("disabled backend ingested %+v", sink2.all())
	}
}

func TestTraceIngestNeverBlocksCaller(t *testing.T) {
	// No worker started and a queue of one: callers must still return.
	ing := NewTraceIngestor(TraceIngestConfig{Sink: &fakeTraceSink{}, Queue: 1})
	done := make(chan struct{})
	go func() {
		for i := 0; i < 50; i++ {
			ing.RecordEvent("h-ab12", trace.Event{Kind: trace.KindToolCall})
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("RecordEvent blocked on a full queue")
	}
	if ing.Dropped() != 49 {
		t.Errorf("dropped = %d, want 49", ing.Dropped())
	}
}

func TestDefaultTranscriptLocatorPicksNewestSinceCreation(t *testing.T) {
	cfg, work := t.TempDir(), "/work/proj"
	dir := filepath.Join(cfg, "projects", adapters.ClaudeProjectDirName(work))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	old, cur := filepath.Join(dir, "old.jsonl"), filepath.Join(dir, "cur.jsonl")
	for _, p := range []string{old, cur} {
		if err := os.WriteFile(p, []byte("{}\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	created := traceFixedNow
	_ = os.Chtimes(old, created.Add(-time.Hour), created.Add(-time.Hour))
	_ = os.Chtimes(cur, created.Add(time.Minute), created.Add(time.Minute))
	loc := DefaultTranscriptLocator(cfg)
	s := Session{BackendFamily: "claude-code", ProjectDir: work, CreatedAt: created}
	if got := loc(s); got != cur {
		t.Errorf("locator = %q, want %q", got, cur)
	}
	s.BackendFamily = "ollama"
	if got := loc(s); got != "" {
		t.Errorf("non-claude backend locator = %q", got)
	}
}

func TestManagerAppendTimelineUsesExistingFile(t *testing.T) {
	m := newTestManager(t)
	m.dataDir = t.TempDir()
	dir := filepath.Join(m.dataDir, "sessions", "h-ab12")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "timeline.md"), []byte("head\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := m.AppendTimeline("h-ab12", "x | trace-tool_call | bash"); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(dir, "timeline.md"))
	if string(b) != "head\nx | trace-tool_call | bash\n" {
		t.Errorf("timeline.md = %q", b)
	}
}
