package websearch

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSearxngProviderSearch(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":[
			{"title":"Result One","url":"https://example.com/1","content":"snippet one"},
			{"title":"Result Two","url":"https://example.com/2","content":"snippet two"}
		]}`))
	}))
	defer srv.Close()

	p := &SearxngProvider{URL: srv.URL, Engine: "bing", NumResults: 10}
	results, err := p.Search(context.Background(), "rust async", 10)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("want 2 results, got %d", len(results))
	}
	if results[0].Title != "Result One" || results[0].URL != "https://example.com/1" {
		t.Errorf("unexpected first result: %+v", results[0])
	}
	// GH#165 — the engine must always be explicitly forced; this is the
	// whole point of this provider over bare SearXNG defaults.
	if !strings.Contains(gotQuery, "engines=bing") {
		t.Errorf("request did not force engines=bing: %q", gotQuery)
	}
	if !strings.Contains(gotQuery, "categories=general") {
		t.Errorf("request did not set categories=general: %q", gotQuery)
	}
}

func TestSearxngProviderLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":[
			{"title":"1","url":"u1","content":"c1"},
			{"title":"2","url":"u2","content":"c2"},
			{"title":"3","url":"u3","content":"c3"}
		]}`))
	}))
	defer srv.Close()

	p := &SearxngProvider{URL: srv.URL, NumResults: 10}
	results, err := p.Search(context.Background(), "q", 2)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("limit not respected: got %d results", len(results))
	}
}

func TestSearxngProviderEmptyURL(t *testing.T) {
	p := &SearxngProvider{}
	if _, err := p.Search(context.Background(), "q", 5); err == nil {
		t.Fatal("expected error for empty URL, got nil")
	}
}

func TestSearxngProviderHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()
	p := &SearxngProvider{URL: srv.URL}
	if _, err := p.Search(context.Background(), "q", 5); err == nil {
		t.Fatal("expected error for HTTP 502, got nil")
	}
}

func TestSearxngProviderSnippetTruncation(t *testing.T) {
	longContent := strings.Repeat("x", 500)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":[{"title":"t","url":"u","content":"` + longContent + `"}]}`))
	}))
	defer srv.Close()
	p := &SearxngProvider{URL: srv.URL}
	results, err := p.Search(context.Background(), "q", 5)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results[0].Content) != 400 {
		t.Errorf("want snippet truncated to 400 chars, got %d", len(results[0].Content))
	}
}
