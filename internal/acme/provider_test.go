package acme

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHTTPProvider_PresentCleanUpHandler(t *testing.T) {
	p := newHTTPProvider()
	h := p.Handler()

	// No token presented yet -> 404.
	req := httptest.NewRequest(http.MethodGet, "/.well-known/acme-challenge/abc123", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 before Present, got %d", rec.Code)
	}

	if err := p.Present("example.test", "abc123", "abc123.keyauth"); err != nil {
		t.Fatalf("Present: %v", err)
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 after Present, got %d", rec.Code)
	}
	if body := rec.Body.String(); body != "abc123.keyauth" {
		t.Fatalf("expected keyAuth body, got %q", body)
	}

	if err := p.CleanUp("example.test", "abc123", "abc123.keyauth"); err != nil {
		t.Fatalf("CleanUp: %v", err)
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 after CleanUp (stale token must not stay servable), got %d", rec.Code)
	}
}

func TestHTTPProvider_UnknownTokenAlways404(t *testing.T) {
	p := newHTTPProvider()
	if err := p.Present("example.test", "real-token", "real-keyauth"); err != nil {
		t.Fatalf("Present: %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, "/.well-known/acme-challenge/some-other-token", nil)
	rec := httptest.NewRecorder()
	p.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for a token that was never Present()ed, got %d", rec.Code)
	}
}
