package websearch

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

// fakeProvider is a test-only Provider with scriptable behavior.
type fakeProvider struct {
	results []Result
	err     error
	calls   int
}

func (f *fakeProvider) Search(ctx context.Context, query string, limit int) ([]Result, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	return f.results, nil
}

// newTestRegistry builds a Registry directly (bypassing NewProvider/specs)
// so tests can inject fakeProvider instances and inspect call counts.
func newTestRegistry(t *testing.T, providers []registeredProvider, cacheTTL time.Duration) *Registry {
	t.Helper()
	store, err := NewStore(filepath.Join(t.TempDir(), "websearch.db"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return &Registry{providers: providers, cache: NewCache(cacheTTL), store: store, defaultTTL: cacheTTL}
}

func TestRegistryPriorityFallback(t *testing.T) {
	failing := &fakeProvider{err: fmt.Errorf("boom")}
	working := &fakeProvider{results: []Result{{Title: "ok"}}}
	r := newTestRegistry(t, []registeredProvider{
		{Name: "primary", Type: "brave", Priority: 0, Impl: failing},
		{Name: "fallback", Type: "searxng", Priority: 1, Impl: working},
	}, time.Minute)

	results, provider, err := r.Search(context.Background(), "q", 5, "sess1")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if provider != "fallback" {
		t.Errorf("provider = %q, want fallback", provider)
	}
	if len(results) != 1 || results[0].Title != "ok" {
		t.Errorf("unexpected results: %+v", results)
	}
	if failing.calls != 1 || working.calls != 1 {
		t.Errorf("call counts: failing=%d working=%d, want 1,1", failing.calls, working.calls)
	}

	sum, err := r.Summary(context.Background())
	if err != nil {
		t.Fatalf("Summary: %v", err)
	}
	if sum.Total != 2 { // one failure recorded + one success recorded
		t.Errorf("Summary.Total = %d, want 2 (failure + success both recorded)", sum.Total)
	}
}

func TestRegistrySkipsEmptyResultsNotJustErrors(t *testing.T) {
	// GH#165 — a provider that "succeeds" with zero results (e.g. SearXNG
	// silently falling through to a junk engine) must not block a better
	// provider lower in priority from being tried.
	empty := &fakeProvider{results: nil}
	good := &fakeProvider{results: []Result{{Title: "real result"}}}
	r := newTestRegistry(t, []registeredProvider{
		{Name: "flaky", Type: "searxng", Priority: 0, Impl: empty},
		{Name: "reliable", Type: "brave", Priority: 1, Impl: good},
	}, time.Minute)

	results, provider, err := r.Search(context.Background(), "q", 5, "")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if provider != "reliable" {
		t.Errorf("provider = %q, want reliable", provider)
	}
	if len(results) != 1 {
		t.Errorf("want 1 result from the reliable provider, got %d", len(results))
	}
}

func TestRegistryAllProvidersFail(t *testing.T) {
	a := &fakeProvider{err: fmt.Errorf("err-a")}
	b := &fakeProvider{err: fmt.Errorf("err-b")}
	r := newTestRegistry(t, []registeredProvider{
		{Name: "a", Priority: 0, Impl: a},
		{Name: "b", Priority: 1, Impl: b},
	}, time.Minute)

	_, _, err := r.Search(context.Background(), "q", 5, "")
	if err == nil {
		t.Fatal("expected error when all providers fail")
	}
}

func TestRegistryCacheHitSkipsLiveCall(t *testing.T) {
	provider := &fakeProvider{results: []Result{{Title: "first"}}}
	r := newTestRegistry(t, []registeredProvider{
		{Name: "p", Priority: 0, Impl: provider},
	}, time.Minute)

	ctx := context.Background()
	if _, _, err := r.Search(ctx, "same query", 5, ""); err != nil {
		t.Fatalf("first Search: %v", err)
	}
	if _, _, err := r.Search(ctx, "same query", 5, ""); err != nil {
		t.Fatalf("second Search: %v", err)
	}
	if provider.calls != 1 {
		t.Errorf("provider.calls = %d, want 1 (second query should be served from cache)", provider.calls)
	}

	sum, err := r.Summary(ctx)
	if err != nil {
		t.Fatalf("Summary: %v", err)
	}
	if sum.CacheHits != 1 {
		t.Errorf("CacheHits = %d, want 1", sum.CacheHits)
	}
}

func TestRegistryPriorityOrdering(t *testing.T) {
	// Providers passed out of order; NewRegistry must sort by Priority.
	specs := []ProviderSpec{
		{Name: "second", Type: "searxng", Enabled: true, Priority: 5, URL: "http://example.invalid"},
		{Name: "first", Type: "searxng", Enabled: true, Priority: 1, URL: "http://example.invalid"},
		{Name: "disabled", Type: "searxng", Enabled: false, Priority: 0, URL: "http://example.invalid"},
	}
	r, err := NewRegistry(specs, NewCache(0), nil, 0)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	names := r.ProviderNames()
	if len(names) != 2 {
		t.Fatalf("want 2 enabled providers, got %d: %v", len(names), names)
	}
	if names[0] != "first" || names[1] != "second" {
		t.Errorf("ProviderNames() = %v, want [first second]", names)
	}
}

func TestRegistryUnknownProviderType(t *testing.T) {
	_, err := NewRegistry([]ProviderSpec{
		{Name: "bad", Type: "yahoo", Enabled: true},
	}, nil, nil, 0)
	if err == nil {
		t.Fatal("expected error for unknown provider type")
	}
}

func TestRegistryNotEnabledWithNoProviders(t *testing.T) {
	var r *Registry
	if r.Enabled() {
		t.Error("nil registry should report Enabled() == false")
	}
	r2, err := NewRegistry(nil, nil, nil, 0)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	if r2.Enabled() {
		t.Error("registry with no providers should report Enabled() == false")
	}
	if _, _, err := r2.Search(context.Background(), "q", 5, ""); err == nil {
		t.Error("Search on an empty registry should error")
	}
}
