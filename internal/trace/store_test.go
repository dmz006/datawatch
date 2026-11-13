package trace

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var storeNow = time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)

func newTestStore(t *testing.T, s Settings) (*Store, string) {
	t.Helper()
	dir := t.TempDir()
	st, err := NewStore(dir, s, func() time.Time { return storeNow }, "/work/proj")
	if err != nil {
		t.Fatal(err)
	}
	return st, dir
}

func sev(sid string, kind Kind, name string, at time.Time) Event {
	return Event{SessionID: sid, TaskID: "t1", StoryID: "s1", AutomatonID: "a1", Agent: "ag", Backend: "pi",
		Kind: kind, Name: name, Timestamp: at}
}

func TestAppendReadIndex(t *testing.T) {
	st, dir := newTestStore(t, Settings{})
	evs := []Event{
		sev("sess1", KindToolCall, "bash", storeNow),
		sev("sess1", KindToolResult, "bash", storeNow.Add(time.Second)),
		sev("sess1", KindError, "bash", storeNow.Add(2*time.Second)),
	}
	n, err := st.Append(evs...)
	if err != nil || n != 3 {
		t.Fatalf("append: %d %v", n, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "trace", "sess1.jsonl")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "trace", "index.json")); err != nil {
		t.Fatal(err)
	}
	got, _ := st.Read("sess1", Filter{})
	if len(got) != 3 || got[0].Kind != KindToolCall {
		t.Fatalf("read: %+v", got)
	}
	idx := st.Index()
	if len(idx) != 1 {
		t.Fatalf("index: %+v", idx)
	}
	e := idx[0]
	if e.Total != 3 || e.Errors != 1 || e.Counts[KindToolCall] != 1 || e.Task != "t1" || e.Automaton != "a1" || e.Backend != "pi" {
		t.Fatalf("entry: %+v", e)
	}
	if err := st.SetCoverage("sess1", CoveragePartial); err != nil {
		t.Fatal(err)
	}
	// reopen: index and coverage persist
	st2, err := NewStore(dir, Settings{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if i := st2.Index(); len(i) != 1 || i[0].Coverage != CoveragePartial || i[0].Total != 3 {
		t.Fatalf("reopened index: %+v", i)
	}
}

func TestAppendIdempotent(t *testing.T) {
	st, dir := newTestStore(t, Settings{})
	evs := []Event{sev("s", KindToolCall, "bash", storeNow), sev("s", KindToolCall, "read", storeNow)}
	if _, err := st.Append(evs...); err != nil {
		t.Fatal(err)
	}
	n, _ := st.Append(evs...)
	if n != 0 {
		t.Fatalf("duplicate append wrote %d", n)
	}
	// incremental re-read: old plus one new
	n, _ = st.Append(append(evs, sev("s", KindToolCall, "edit", storeNow))...)
	if n != 1 {
		t.Fatalf("incremental wrote %d", n)
	}
	// a fresh store (restart) still dedups from disk
	st2, _ := NewStore(dir, Settings{}, nil, "/work/proj")
	if n, _ := st2.Append(evs...); n != 0 {
		t.Fatalf("after reopen wrote %d", n)
	}
	if got, _ := st2.Read("s", Filter{}); len(got) != 3 {
		t.Fatalf("events: %d", len(got))
	}
	if st2.Index()[0].Total != 3 {
		t.Fatal("index total drifted")
	}
}

func TestAppendRedacts(t *testing.T) {
	st, dir := newTestStore(t, Settings{Mode: ModeFull})
	ev := sev("s", KindToolCall, "bash", storeNow)
	ev.ArgSummary = "cat /work/proj/a.go " + homeDir + "/bob/.ssh/x"
	if _, err := st.Append(ev); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(dir, "trace", "s.jsonl"))
	if strings.Contains(string(b), "/work/proj") || strings.Contains(string(b), "bob") {
		t.Fatalf("not redacted: %s", b)
	}
	if _, err := st.Append(Event{Kind: KindToolCall}); err == nil {
		t.Fatal("expected error for missing session")
	}
}

func TestReadFilters(t *testing.T) {
	st, _ := newTestStore(t, Settings{})
	a := sev("s", KindToolCall, "bash", storeNow)
	b := sev("s", KindToolResult, "read", storeNow.Add(time.Second))
	b.Backend = "claude-code"
	c := sev("s", KindToolResult, "bash", storeNow.Add(2*time.Second))
	c.Outcome = "failed: exit 1"
	d := sev("s", KindError, "", storeNow.Add(3*time.Second))
	if _, err := st.Append(a, b, c, d); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		f    Filter
		want int
	}{
		{Filter{}, 4},
		{Filter{Tool: "bash"}, 2},
		{Filter{Backend: "claude-code"}, 1},
		{Filter{Kind: KindToolResult}, 2},
		{Filter{ErrorsOnly: true}, 2},
		{Filter{Tool: "bash", ErrorsOnly: true}, 1},
	}
	for _, tc := range cases {
		got, _ := st.Read("s", tc.f)
		if len(got) != tc.want {
			t.Errorf("%+v: got %d want %d", tc.f, len(got), tc.want)
		}
	}
	if got, err := st.Read("missing", Filter{}); err != nil || len(got) != 0 {
		t.Fatalf("missing session: %v %v", got, err)
	}
}

func TestPruneRetention(t *testing.T) {
	st, dir := newTestStore(t, Settings{RetentionDays: 7})
	old := storeNow.Add(-8 * 24 * time.Hour)
	edge := storeNow.Add(-7 * 24 * time.Hour) // exactly at cutoff: kept
	if _, err := st.Append(
		sev("mixed", KindToolCall, "bash", old),
		sev("mixed", KindToolCall, "read", edge),
		sev("mixed", KindError, "x", storeNow),
		sev("stale", KindToolCall, "bash", old),
	); err != nil {
		t.Fatal(err)
	}
	if err := st.SetCoverage("mixed", CoverageFull); err != nil {
		t.Fatal(err)
	}
	n, err := st.Prune()
	if err != nil || n != 2 {
		t.Fatalf("prune: %d %v", n, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "trace", "stale.jsonl")); !os.IsNotExist(err) {
		t.Fatal("stale file should be removed")
	}
	got, _ := st.Read("mixed", Filter{})
	if len(got) != 2 {
		t.Fatalf("kept %d", len(got))
	}
	idx := st.Index()
	if len(idx) != 1 || idx[0].Total != 2 || idx[0].Errors != 1 || idx[0].Coverage != CoverageFull {
		t.Fatalf("index after prune: %+v", idx)
	}
	// pruned events can be re-appended (seen set was reset)
	if n, _ := st.Append(sev("mixed", KindToolCall, "bash", old)); n != 1 {
		t.Fatalf("re-append: %d", n)
	}
	if n, _ := st.Prune(); n != 1 {
		t.Fatalf("second prune: %d", n)
	}
}

func TestPruneDefaultRetention(t *testing.T) {
	// The new default is 0 = keep forever: prune removes nothing, even for
	// 30-day-old events, and touches neither file nor index.
	st, dir := newTestStore(t, Settings{})
	if DefaultRetentionDays != 0 {
		t.Fatalf("DefaultRetentionDays = %d, want 0", DefaultRetentionDays)
	}
	if n, err := st.Append(
		sev("a", KindToolCall, "x", storeNow.Add(-30*24*time.Hour)),
		sev("a", KindToolCall, "y", storeNow.Add(-13*24*time.Hour)),
	); err != nil || n != 2 {
		t.Fatalf("append: %d %v", n, err)
	}
	if n, err := st.Prune(); err != nil || n != 0 {
		t.Fatalf("prune with default retention: %d %v, want 0 nil", n, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "trace", "a.jsonl")); err != nil {
		t.Fatalf("session file must stay: %v", err)
	}
	if got := st.Index(); len(got) != 1 || got[0].Total != 2 {
		t.Fatalf("index after no-op prune: %+v", got)
	}
	if got, _ := st.Read("a", Filter{}); len(got) != 2 {
		t.Fatalf("events after no-op prune: %d", len(got))
	}
}

func TestDeleteSession(t *testing.T) {
	st, dir := newTestStore(t, Settings{})
	if _, err := st.Append(
		sev("a", KindToolCall, "bash", storeNow),
		sev("a", KindToolResult, "bash", storeNow.Add(time.Second)),
		sev("b", KindToolCall, "read", storeNow.Add(2*time.Second)),
	); err != nil {
		t.Fatal(err)
	}
	if err := st.SetCoverage("a", CoveragePartial); err != nil {
		t.Fatal(err)
	}
	if got := len(st.Index()); got != 2 {
		t.Fatalf("index: %d entries, want 2", got)
	}

	if err := st.DeleteSession("a"); err != nil {
		t.Fatal(err)
	}
	// File gone, index entry gone, b untouched.
	if _, err := os.Stat(filepath.Join(dir, "trace", "a.jsonl")); !os.IsNotExist(err) {
		t.Errorf("a.jsonl should be gone, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "trace", "b.jsonl")); err != nil {
		t.Errorf("b.jsonl must stay: %v", err)
	}
	idx := st.Index()
	if len(idx) != 1 || idx[0].Session != "b" || idx[0].Coverage != "" {
		t.Fatalf("index after delete: %+v", idx)
	}
	if evs, err := st.Read("b", Filter{}); err != nil || len(evs) != 1 {
		t.Fatalf("read b: %d %v, want 1", len(evs), err)
	}
	if evs, err := st.Read("a", Filter{}); err != nil || len(evs) != 0 {
		t.Fatalf("read a: %v %v, want none", evs, err)
	}
	// Re-appending to the deleted id starts a fresh trace.
	if n, err := st.Append(sev("a", KindToolCall, "bash", storeNow.Add(time.Hour))); err != nil || n != 1 {
		t.Fatalf("re-append a: %d %v, want 1", n, err)
	}
	// A duplicate of that freshly stored event is still deduped (the
	// replaced seen-set is keyed to the new file).
	if n, _ := st.Append(sev("a", KindToolCall, "bash", storeNow.Add(time.Hour))); n != 0 {
		t.Fatalf("dup re-append wrote %d", n)
	}
	// The persisted index on disk agrees with the in-memory one.
	st2, err := NewStore(dir, Settings{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := st2.Index(); len(got) != 2 || got[0].Session != "a" || got[1].Session != "b" {
		t.Fatalf("reopened index: %+v", got)
	}
}

func TestDeleteSessionUnknownAndInvalid(t *testing.T) {
	st, dir := newTestStore(t, Settings{})
	if _, err := st.Append(sev("known", KindToolCall, "bash", storeNow)); err != nil {
		t.Fatal(err)
	}
	// Unknown id: nil error, no file, index or seen-set change.
	if err := st.DeleteSession("no-such-session"); err != nil {
		t.Fatalf("unknown id: %v", err)
	}
	if got := len(st.Index()); got != 1 {
		t.Errorf("index: %d entries, want 1", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "trace", "known.jsonl")); err != nil {
		t.Errorf("known.jsonl must stay: %v", err)
	}
	// Invalid ids are refused without touching the store.
	for _, bad := range []string{"", "  ", ".", "..", "../evil", "a/b", "a\\b"} {
		if err := st.DeleteSession(bad); err == nil {
			t.Errorf("DeleteSession(%q) should be refused", bad)
		}
	}
	if got := len(st.Index()); got != 1 {
		t.Errorf("index: %d entries after refused deletes, want 1", got)
	}
	if evs, _ := st.Read("known", Filter{}); len(evs) != 1 {
		t.Fatalf("known session lost: %d events", len(evs))
	}
}

func TestSessionIDPathSafe(t *testing.T) {
	st, dir := newTestStore(t, Settings{})
	if _, err := st.Append(sev("../evil/x", KindToolCall, "bash", storeNow)); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(filepath.Join(dir, "trace"))
	for _, e := range entries {
		if e.IsDir() {
			t.Fatalf("unexpected dir %s", e.Name())
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "evil")); err == nil {
		t.Fatal("escaped trace dir")
	}
	if got, _ := st.Read("../evil/x", Filter{}); len(got) != 1 {
		t.Fatal("read back failed")
	}
}

func TestStoreStatsCounters(t *testing.T) {
	st, _ := newTestStore(t, Settings{Mode: ModeFull})
	if _, err := st.Append(sev("a", KindToolCall, "bash", storeNow), sev("a", KindToolCall, "bash", storeNow.Add(time.Second))); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Append(sev("a", KindToolCall, "bash", storeNow)); err != nil { // duplicate: not counted
		t.Fatal(err)
	}
	if _, err := st.Append(Event{Name: "no session"}); err == nil {
		t.Fatal("event without a session id must be rejected")
	}
	got := st.Stats()
	if got.Sessions != 1 || got.Events != 2 || got.Appended != 2 || got.AppendErrors != 1 {
		t.Fatalf("stats = %+v", got)
	}
	if got.LastEventAt.IsZero() {
		t.Error("LastEventAt not set")
	}
}
