package websearch

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "websearch.db")
	s, err := NewStore(dbPath)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestStoreRecordAndSummary(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC()

	events := []Event{
		{Time: now, ProviderName: "brave-primary", ProviderType: "brave", Query: "q1", Success: true, ResultCount: 5},
		{Time: now, ProviderName: "brave-primary", ProviderType: "brave", Query: "q2", Success: true, ResultCount: 3, CacheHit: true},
		{Time: now, ProviderName: "brave-primary", ProviderType: "brave", Query: "q3", Success: false, Error: "timeout"},
		{Time: now, ProviderName: "searxng-fallback", ProviderType: "searxng", Query: "q4", Success: true, ResultCount: 2},
	}
	for _, e := range events {
		if err := s.Record(ctx, e); err != nil {
			t.Fatalf("Record: %v", err)
		}
	}

	sum, err := s.Summary(ctx)
	if err != nil {
		t.Fatalf("Summary: %v", err)
	}
	if sum.Total != 4 {
		t.Errorf("Total = %d, want 4", sum.Total)
	}
	if sum.Today != 4 {
		t.Errorf("Today = %d, want 4 (all events are from now)", sum.Today)
	}
	if sum.CacheHits != 1 {
		t.Errorf("CacheHits = %d, want 1", sum.CacheHits)
	}
	if len(sum.Providers) != 2 {
		t.Fatalf("want 2 providers in summary, got %d", len(sum.Providers))
	}
	var brave *ProviderStats
	for i := range sum.Providers {
		if sum.Providers[i].Name == "brave-primary" {
			brave = &sum.Providers[i]
		}
	}
	if brave == nil {
		t.Fatal("brave-primary not found in summary")
	}
	if brave.Total != 3 {
		t.Errorf("brave-primary Total = %d, want 3", brave.Total)
	}
	if brave.Errors != 1 {
		t.Errorf("brave-primary Errors = %d, want 1", brave.Errors)
	}
	if brave.LastQueryAt == nil {
		t.Error("brave-primary LastQueryAt should be set")
	}
}

func TestStoreSummaryCalendarBoundaries(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// An event from 40 days ago should count toward Total but not
	// Today/ThisWeek/ThisMonth.
	old := time.Now().UTC().AddDate(0, 0, -40)
	if err := s.Record(ctx, Event{Time: old, ProviderName: "p", ProviderType: "brave", Success: true, ResultCount: 1}); err != nil {
		t.Fatalf("Record: %v", err)
	}
	sum, err := s.Summary(ctx)
	if err != nil {
		t.Fatalf("Summary: %v", err)
	}
	if sum.Total != 1 {
		t.Errorf("Total = %d, want 1", sum.Total)
	}
	if sum.Today != 0 || sum.ThisWeek != 0 || sum.ThisMonth != 0 {
		t.Errorf("a 40-day-old event should not count in any calendar window: today=%d week=%d month=%d", sum.Today, sum.ThisWeek, sum.ThisMonth)
	}
}

func TestStoreDailySeriesZeroFilled(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	if err := s.Record(ctx, Event{Time: now, ProviderName: "p", ProviderType: "brave", Success: true, ResultCount: 1}); err != nil {
		t.Fatalf("Record: %v", err)
	}

	series, err := s.DailySeries(ctx, 7)
	if err != nil {
		t.Fatalf("DailySeries: %v", err)
	}
	if len(series) != 7 {
		t.Fatalf("want 7 days, got %d", len(series))
	}
	// Last entry (today) should have the one recorded event; every other
	// day must be present with count 0, not skipped.
	last := series[len(series)-1]
	if last.Date != now.Format("2006-01-02") {
		t.Errorf("last day = %q, want today %q", last.Date, now.Format("2006-01-02"))
	}
	if last.Count != 1 {
		t.Errorf("today's count = %d, want 1", last.Count)
	}
	zeroDays := 0
	for _, d := range series[:len(series)-1] {
		if d.Count == 0 {
			zeroDays++
		}
	}
	if zeroDays != 6 {
		t.Errorf("want 6 zero-filled days, got %d", zeroDays)
	}
}

func TestStoreHistoryPaginationAndOrder(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	base := time.Now().UTC()
	for i := 0; i < 5; i++ {
		e := Event{
			Time: base.Add(time.Duration(i) * time.Second), ProviderName: "p", ProviderType: "brave",
			Query: "q" + string(rune('0'+i)), Success: true, ResultCount: 1,
		}
		if err := s.Record(ctx, e); err != nil {
			t.Fatalf("Record: %v", err)
		}
	}
	page1, err := s.History(ctx, 2, 0)
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	if len(page1) != 2 {
		t.Fatalf("want 2 entries, got %d", len(page1))
	}
	// Newest first: the last-inserted event (i=4) should come first.
	if page1[0].Query != "q4" {
		t.Errorf("page1[0].Query = %q, want q4 (newest first)", page1[0].Query)
	}
	page2, err := s.History(ctx, 2, 2)
	if err != nil {
		t.Fatalf("History page2: %v", err)
	}
	if len(page2) != 2 || page2[0].Query != "q2" {
		t.Errorf("page2 = %+v, want starting at q2", page2)
	}
}

func TestStorePrune(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	old := time.Now().UTC().AddDate(0, 0, -100)
	recent := time.Now().UTC()
	_ = s.Record(ctx, Event{Time: old, ProviderName: "p", ProviderType: "brave", Success: true})
	_ = s.Record(ctx, Event{Time: recent, ProviderName: "p", ProviderType: "brave", Success: true})

	if err := s.Prune(ctx, 90*24*time.Hour); err != nil {
		t.Fatalf("Prune: %v", err)
	}
	sum, err := s.Summary(ctx)
	if err != nil {
		t.Fatalf("Summary: %v", err)
	}
	if sum.Total != 1 {
		t.Errorf("Total after prune = %d, want 1 (old event should be deleted)", sum.Total)
	}
}

func TestNilStoreIsSafe(t *testing.T) {
	var s *Store
	ctx := context.Background()
	if err := s.Record(ctx, Event{}); err != nil {
		t.Errorf("nil store Record should no-op, got error: %v", err)
	}
	if sum, err := s.Summary(ctx); err != nil || sum.Total != 0 {
		t.Errorf("nil store Summary should return zero value, got %+v, err=%v", sum, err)
	}
	if h, err := s.History(ctx, 10, 0); err != nil || h != nil {
		t.Errorf("nil store History should return nil, got %+v, err=%v", h, err)
	}
	if err := s.Close(); err != nil {
		t.Errorf("nil store Close should no-op, got error: %v", err)
	}
}
