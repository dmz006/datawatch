package mcp

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The SSE stream carries JSON-RPC results that can echo request values.
// These tests pin that the stream is served as a non-HTML type with
// nosniff, whatever the wrapped handler did or did not set.

const sseReflected = `<script>alert(1)</script>`

func serveObserved(t *testing.T, h http.HandlerFunc) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/sse?q="+sseReflected, nil)
	req.Header.Set("Accept", "text/event-stream")
	rr := httptest.NewRecorder()
	ObserveSSEHandler(h).ServeHTTP(rr, req)
	return rr
}

func assertSSEHeaders(t *testing.T, rr *httptest.ResponseRecorder) {
	t.Helper()
	ct := strings.ToLower(rr.Header().Get("Content-Type"))
	if ct == "" || strings.Contains(ct, "html") {
		t.Fatalf("Content-Type = %q, want non-HTML", ct)
	}
	if got := rr.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("X-Content-Type-Options = %q, want nosniff", got)
	}
	if !strings.Contains(rr.Body.String(), sseReflected) {
		t.Fatalf("reflected value should still be delivered unchanged, got %q", rr.Body.String())
	}
}

func TestSSEObserver_ReflectedValueEventStreamNosniff(t *testing.T) {
	rr := serveObserved(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "data: "+r.URL.Query().Get("q")+"\n\n")
	})
	assertSSEHeaders(t, rr)
	if got := rr.Header().Get("Content-Type"); got != "text/event-stream" {
		t.Fatalf("handler's Content-Type changed to %q", got)
	}
}

func TestSSEObserver_MissingTypeOnImplicitWriteGetsEventStream(t *testing.T) {
	rr := serveObserved(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "data: "+r.URL.Query().Get("q")+"\n\n")
	})
	assertSSEHeaders(t, rr)
	if got := rr.Header().Get("Content-Type"); got != "text/event-stream" {
		t.Fatalf("Content-Type = %q, want text/event-stream", got)
	}
}

func TestSSEObserver_HTMLTypeIsReplaced(t *testing.T) {
	rr := serveObserved(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, r.URL.Query().Get("q"))
	})
	assertSSEHeaders(t, rr)
	if got := rr.Header().Get("Content-Type"); got != "text/event-stream" {
		t.Fatalf("Content-Type = %q, want text/event-stream", got)
	}
}

// Every write goes through the type pin, not only the first one: chunks
// after the first keep the stream type and arrive unchanged.
func TestSSEObserver_ChunkedWritesKeepTypeAndBytes(t *testing.T) {
	rr := serveObserved(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		q := r.URL.Query().Get("q")
		_, _ = io.WriteString(w, "data: "+q[:5])
		_, _ = io.WriteString(w, q[5:]+"\n\n")
		_, _ = io.WriteString(w, "data: second\n\n")
	})
	assertSSEHeaders(t, rr)
	if got := rr.Header().Get("Content-Type"); got != "text/event-stream" {
		t.Fatalf("Content-Type = %q, want text/event-stream", got)
	}
	want := "data: " + sseReflected + "\n\ndata: second\n\n"
	if got := rr.Body.String(); got != want {
		t.Fatalf("body = %q, want %q", got, want)
	}
}
