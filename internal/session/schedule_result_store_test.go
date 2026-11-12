package session

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

func mustPayload(t *testing.T, title string) *ResultPayload {
	t.Helper()
	np := ResultPayload{Title: title, Status: "ok", Summary: "done"}
	if err := ValidateResultPayload(&np); err != nil {
		t.Fatalf("valid payload rejected: %v", err)
	}
	return &np
}

func TestScheduleResultStoreEvictsOldestFirst(t *testing.T) {
	path := t.TempDir() + "/schedule_results.json"
	store, err := NewScheduleResultStore(path)
	if err != nil {
		t.Fatalf("NewScheduleResultStore: %v", err)
	}
	const limit = 3
	for i := 1; i <= 5; i++ {
		e := &ScheduleResultEntry{
			Payload:      *mustPayload(t, "run"),
			SessionID:    "sess",
			ScheduleName: "daily-report",
			PostedAt:     time.Date(2026, 10, 20, 9, i, 0, 0, time.UTC),
		}
		if err := store.Add(e, limit); err != nil {
			t.Fatalf("Add %d: %v", i, err)
		}
	}
	h := store.History("daily-report")
	if len(h) != limit {
		t.Fatalf("expected %d retained entries, got %d", limit, len(h))
	}
	// Oldest first: first retained entry is run 3, last is run 5.
	if h[0].PostedAt.Day() != 20 || h[0].PostedAt.Hour() != 9 || h[0].PostedAt.Minute() != 3 {
		t.Errorf("oldest retained entry wrong: %v (want 09:03)", h[0].PostedAt)
	}
	if h[len(h)-1].PostedAt.Minute() != 5 {
		t.Errorf("newest retained entry wrong: %v (want 09:05)", h[len(h)-1].PostedAt)
	}
	list := store.List()
	if len(list) != 1 {
		t.Fatalf("List: expected 1 schedule, got %d", len(list))
	}
	if list[0].Count != limit {
		t.Errorf("List count = %d, want %d", list[0].Count, limit)
	}
	if list[0].Latest.PostedAt.Minute() != 5 {
		t.Errorf("List latest = %v, want 09:05", list[0].Latest.PostedAt)
	}
	// Eviction removed run 1 and run 2.
	for _, e := range h {
		if e.PostedAt.Minute() <= 2 {
			t.Errorf("evicted entry still present: %v", e.PostedAt)
		}
	}
}

func TestScheduleResultStoreLimitChange(t *testing.T) {
	path := t.TempDir() + "/schedule_results.json"
	store, err := NewScheduleResultStore(path)
	if err != nil {
		t.Fatalf("NewScheduleResultStore: %v", err)
	}
	for i := 1; i <= 3; i++ {
		e := &ScheduleResultEntry{
			Payload:      *mustPayload(t, "run"),
			ScheduleName: "jobs",
			PostedAt:     time.Now().Add(time.Duration(i) * time.Second),
		}
		if err := store.Add(e, 3); err != nil {
			t.Fatalf("Add: %v", err)
		}
	}
	if got := len(store.History("jobs")); got != 3 {
		t.Fatalf("expected 3 entries before limit change, got %d", got)
	}
	// Lower the limit: the next Add immediately evicts down to 1.
	if err := store.Add(&ScheduleResultEntry{
		Payload:      *mustPayload(t, "run"),
		ScheduleName: "jobs",
		PostedAt:     time.Now(),
	}, 1); err != nil {
		t.Fatalf("Add with limit 1: %v", err)
	}
	h := store.History("jobs")
	if len(h) != 1 {
		t.Fatalf("expected 1 entry after lowering limit, got %d", len(h))
	}
	// Raising the limit does not resurrect evicted entries.
	if err := store.Add(&ScheduleResultEntry{
		Payload:      *mustPayload(t, "run"),
		ScheduleName: "jobs",
		PostedAt:     time.Now(),
	}, 100); err != nil {
		t.Fatalf("Add with limit 100: %v", err)
	}
	if got := len(store.History("jobs")); got != 2 {
		t.Fatalf("expected 2 entries after raising limit, got %d", got)
	}
}

func TestScheduleResultStorePersistence(t *testing.T) {
	path := t.TempDir() + "/schedule_results.json"
	store, err := NewScheduleResultStore(path)
	if err != nil {
		t.Fatalf("NewScheduleResultStore: %v", err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	for _, name := range []string{"alpha", "beta"} {
		for i := 1; i <= 2; i++ {
			e := &ScheduleResultEntry{
				Payload:      *mustPayload(t, "run"),
				SessionID:    "sess-" + name,
				ScheduleName: name,
				PostedAt:     now.Add(time.Duration(i) * time.Second),
			}
			if err := store.Add(e, 5); err != nil {
				t.Fatalf("Add %s/%d: %v", name, i, err)
			}
		}
	}
	// Reopen from disk — the store must hold identical contents.
	reopened, err := NewScheduleResultStore(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if reopened.Count() != 2 {
		t.Fatalf("Count = %d, want 2", reopened.Count())
	}
	for _, name := range []string{"alpha", "beta"} {
		h := reopened.History(name)
		if len(h) != 2 {
			t.Fatalf("History(%s) = %d entries, want 2", name, len(h))
		}
		if h[1].SessionID != "sess-"+name {
			t.Errorf("History(%s) latest session = %q", name, h[1].SessionID)
		}
	}
	list := reopened.List()
	if len(list) != 2 || list[0].ScheduleName != "alpha" || list[1].ScheduleName != "beta" {
		t.Fatalf("List wrong after reload: %+v", list)
	}
	if list[0].Latest.PostedAt != now.Add(2*time.Second) {
		t.Errorf("loaded latest = %v", list[0].Latest.PostedAt)
	}
	// A third Add post-reload must evict correctly against reloaded data.
	if err := reopened.Add(&ScheduleResultEntry{
		Payload:      *mustPayload(t, "run"),
		ScheduleName: "alpha",
		PostedAt:     now.Add(10 * time.Second),
	}, 1); err != nil {
		t.Fatalf("Add post-reload: %v", err)
	}
	if got := len(reopened.History("alpha")); got != 1 {
		t.Fatalf("History(alpha) after limit-1 add = %d, want 1", got)
	}
}

func TestScheduleResultStoreTruncation(t *testing.T) {
	long := strings.Repeat("a", ScheduleResultMaxStrChars+500)
	p := ResultPayload{
		Title:    "big",
		Status:   "warn",
		Summary:  long,
		Markdown: long,
		Tables: []ResultTable{{
			Title:   long,
			Columns: []string{long},
			Rows:    [][]any{{long, 42, "keep"}},
		}},
	}
	if err := ValidateResultPayload(&p); err != nil {
		t.Fatalf("payload with long strings must be truncated, not rejected: %v", err)
	}
	want := strings.Repeat("a", ScheduleResultMaxStrChars) + ScheduleResultTruncationMarker
	if p.Summary != want {
		t.Errorf("summary not truncated to limit + marker (len=%d)", len(p.Summary))
	}
	if p.Markdown != want {
		t.Errorf("markdown not truncated: len=%d", len(p.Markdown))
	}
	tb := p.Tables[0]
	if tb.Title != want || tb.Columns[0] != want {
		t.Errorf("table title/columns not truncated")
	}
	if cell, ok := tb.Rows[0][0].(string); !ok || cell != want {
		t.Errorf("string cell not truncated: %q", cell)
	}
	if n, ok := tb.Rows[0][1].(int); !ok || n != 42 {
		t.Errorf("non-string cell altered: %v", tb.Rows[0][1])
	}
	if tb.Rows[0][2] != "keep" {
		t.Errorf("short cell altered: %v", tb.Rows[0][2])
	}
	// Store Add must also truncate + accept.
	path := t.TempDir() + "/schedule_results.json"
	store, err := NewScheduleResultStore(path)
	if err != nil {
		t.Fatalf("NewScheduleResultStore: %v", err)
	}
	np := *mustPayload(t, "r")
	np.Summary = long
	e := &ScheduleResultEntry{Payload: np, ScheduleName: "x"}
	if err := store.Add(e, 5); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if got := store.History("x")[0].Payload.Summary; got != want {
		t.Errorf("stored summary not truncated (len=%d)", len(got))
	}
}

func TestScheduleResultStoreRejections(t *testing.T) {
	path := t.TempDir() + "/schedule_results.json"
	store, err := NewScheduleResultStore(path)
	if err != nil {
		t.Fatalf("NewScheduleResultStore: %v", err)
	}
	add := func(p ResultPayload) error {
		return store.Add(&ScheduleResultEntry{Payload: p, ScheduleName: "x"}, 5)
	}

	// Missing title.
	if err := add(ResultPayload{Status: "ok", Summary: "no title"}); !errors.Is(err, ErrResultMissingTitle) {
		t.Errorf("missing title: got %v, want ErrResultMissingTitle", err)
	}
	if err := add(ResultPayload{Title: "   ", Status: "ok"}); !errors.Is(err, ErrResultMissingTitle) {
		t.Errorf("blank title: got %v, want ErrResultMissingTitle", err)
	}
	// Unrecognized status.
	if err := add(ResultPayload{Title: "t", Status: "weird"}); !errors.Is(err, ErrResultInvalidStatus) {
		t.Errorf("bad status: got %v, want ErrResultInvalidStatus", err)
	}
	// Valid statuses accepted, including empty.
	for _, st := range []string{"", "ok", "warn", "error"} {
		if err := add(ResultPayload{Title: "t", Status: st}); err != nil {
			t.Errorf("status %q rejected: %v", st, err)
		}
	}
	// More than 20 tables.
	many := ResultPayload{Title: "t"}
	for i := 0; i < ScheduleResultMaxTables+1; i++ {
		many.Tables = append(many.Tables, ResultTable{Columns: []string{"c"}})
	}
	if err := add(many); !errors.Is(err, ErrResultTooManyTables) {
		t.Errorf("too many tables: got %v, want ErrResultTooManyTables", err)
	}
	// A table over 100 rows.
	rows := make([][]any, ScheduleResultMaxRowsPerTable+1)
	for i := range rows {
		rows[i] = []any{"v"}
	}
	if err := add(ResultPayload{Title: "t", Tables: []ResultTable{{Columns: []string{"c"}, Rows: rows}}}); !errors.Is(err, ErrResultTooManyRows) {
		t.Errorf("too many rows: got %v, want ErrResultTooManyRows", err)
	}
	// Exactly-at-limit values are accepted.
	exact := ResultPayload{Title: "t"}
	for i := 0; i < ScheduleResultMaxTables; i++ {
		r := make([][]any, ScheduleResultMaxRowsPerTable)
		for j := range r {
			r[j] = []any{"v"}
		}
		exact.Tables = append(exact.Tables, ResultTable{Columns: []string{"c"}, Rows: r})
	}
	if err := add(exact); err != nil {
		t.Errorf("at-limit tables/rows rejected: %v", err)
	}
	// Serialized payload over 64 KB (post-truncation). 20 tables x 100
	// rows x one 1000-char cell serializes to roughly 2 MB.
	big := ResultPayload{Title: "t"}
	cell := strings.Repeat("x", 1000)
	for i := 0; i < ScheduleResultMaxTables; i++ {
		r := make([][]any, ScheduleResultMaxRowsPerTable)
		for j := range r {
			r[j] = []any{cell}
		}
		big.Tables = append(big.Tables, ResultTable{Columns: []string{"c"}, Rows: r})
	}
	if err := add(big); !errors.Is(err, ErrResultTooLarge) {
		t.Errorf("oversized payload: got %v, want ErrResultTooLarge", err)
	}
}

func TestScheduleResultStoreConcurrentAdd(t *testing.T) {
	path := t.TempDir() + "/schedule_results.json"
	store, err := NewScheduleResultStore(path)
	if err != nil {
		t.Fatalf("NewScheduleResultStore: %v", err)
	}
	const (
		goroutines = 8
		perG       = 5
		limit      = 10
	)
	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < perG; i++ {
				e := &ScheduleResultEntry{
					Payload:      *mustPayload(t, "run"),
					SessionID:    "sess",
					ScheduleName: "load",
					PostedAt:     time.Now(),
				}
				if err := store.Add(e, limit); err != nil {
					t.Errorf("concurrent Add: %v", err)
					return
				}
			}
		}(g)
	}
	wg.Wait()
	h := store.History("load")
	if len(h) != limit {
		t.Fatalf("expected exactly %d retained entries after concurrent adds, got %d", limit, len(h))
	}
}

func TestScheduleResultStoreValidationTypedErrors(t *testing.T) {
	// Directly exercise ValidateResultPayload's typed errors, including
	// its nil and size paths.
	if err := ValidateResultPayload(nil); !errors.Is(err, ErrResultMissingTitle) {
		t.Errorf("nil payload: got %v, want ErrResultMissingTitle", err)
	}
	metrics := map[string]any{}
	v := strings.Repeat("m", 3000)
	for i := 0; i < 50; i++ {
		metrics[key(i)] = v
	}
	p := &ResultPayload{Title: "t", Metrics: metrics}
	if err := ValidateResultPayload(p); !errors.Is(err, ErrResultTooLarge) {
		t.Errorf("oversized metrics: got %v, want ErrResultTooLarge", err)
	}
}

func key(i int) string {
	const digits = "0123456789abcdefghijklmnopqrstuvwxyz"
	first := digits[i/len(digits)]
	second := digits[i%len(digits)]
	return string(first) + string(second)
}
