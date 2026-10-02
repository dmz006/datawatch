package websearch

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBraveProviderSearch(t *testing.T) {
	var gotAuth, gotAccept string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("X-Subscription-Token")
		gotAccept = r.Header.Get("Accept")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"web":{"results":[
			{"title":"Brave One","url":"https://example.com/b1","description":"desc one"},
			{"title":"Brave Two","url":"https://example.com/b2","description":"desc two"}
		]}}`))
	}))
	defer srv.Close()

	p := &BraveProvider{APIKey: "test-key-123", NumResults: 10, endpoint: srv.URL}
	results, err := p.Search(context.Background(), "golang testing", 10)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("want 2 results, got %d", len(results))
	}
	if results[0].Title != "Brave One" || results[0].Content != "desc one" {
		t.Errorf("unexpected first result: %+v", results[0])
	}
	if gotAuth != "test-key-123" {
		t.Errorf("X-Subscription-Token = %q, want test-key-123", gotAuth)
	}
	if gotAccept != "application/json" {
		t.Errorf("Accept header = %q, want application/json", gotAccept)
	}
}

func TestBraveProviderEmptyAPIKey(t *testing.T) {
	p := &BraveProvider{}
	if _, err := p.Search(context.Background(), "q", 5); err == nil {
		t.Fatal("expected error for empty API key, got nil")
	}
}

func TestBraveProviderAuthRejected(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()
	p := &BraveProvider{APIKey: "bad-key", endpoint: srv.URL}
	if _, err := p.Search(context.Background(), "q", 5); err == nil {
		t.Fatal("expected error for HTTP 401, got nil")
	}
}

func TestBraveProviderRateLimited(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()
	p := &BraveProvider{APIKey: "key", endpoint: srv.URL}
	if _, err := p.Search(context.Background(), "q", 5); err == nil {
		t.Fatal("expected error for HTTP 429, got nil")
	}
}

func TestBraveProviderLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"web":{"results":[
			{"title":"1","url":"u1","description":"d1"},
			{"title":"2","url":"u2","description":"d2"},
			{"title":"3","url":"u3","description":"d3"}
		]}}`))
	}))
	defer srv.Close()
	p := &BraveProvider{APIKey: "key", NumResults: 10, endpoint: srv.URL}
	results, err := p.Search(context.Background(), "q", 2)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("limit not respected: got %d results", len(results))
	}
}
