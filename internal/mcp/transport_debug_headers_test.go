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
// nosniff, and that a handler which did not vouch for a non-HTML type has
// its payload HTML-escaped, whatever it did or did not set.

const (
	sseReflected = `<script>alert(1)</script>`
	sseEscaped   = `&lt;script&gt;alert(1)&lt;/script&gt;`
)

func serveObserved(t *testing.T, h http.HandlerFunc) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/sse?q="+sseReflected, nil)
	req.Header.Set("Accept", "text/event-stream")
	rr := httptest.NewRecorder()
	ObserveSSEHandler(h).ServeHTTP(rr, req)
	return rr
}

// getOnTheWire serves h through ObserveSSEHandler on a real connection and
// returns what the client received. A recorder's header map is live and
// shows headers set after WriteHeader that never reach a client; a real
// connection shows only the snapshot net/http sent.
func getOnTheWire(t *testing.T, h http.HandlerFunc) (*http.Response, string) {
	t.Helper()
	srv := httptest.NewServer(ObserveSSEHandler(h))
	defer srv.Close()
	req, err := http.NewRequest(http.MethodGet, srv.URL+"/sse?q="+sseReflected, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Accept", "text/event-stream")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp, string(body)
}

func assertSSEWireHeaders(t *testing.T, resp *http.Response) {
	t.Helper()
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("client received Content-Type %q, want text/event-stream", ct)
	}
	if got := resp.Header.Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("client received X-Content-Type-Options %q, want nosniff", got)
	}
}

// A handler that declares text/event-stream is the normal case (mcp-go does):
// the payload is JSON-RPC and must arrive byte for byte.
func TestSSEObserver_DeclaredEventStreamIsUnchanged(t *testing.T) {
	resp, body := getOnTheWire(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `data: {"q":"`+r.URL.Query().Get("q")+`"}`+"\n\n")
	})
	assertSSEWireHeaders(t, resp)
	if want := `data: {"q":"` + sseReflected + `"}` + "\n\n"; body != want {
		t.Fatalf("body = %q, want %q", body, want)
	}
}

// With no type set and an implicit 200, net/http would sniff the reflected
// markup as text/html. The type is pinned before the first write and the
// payload is escaped.
func TestSSEObserver_ImplicitWriteWithoutTypeIsPinnedAndEscaped(t *testing.T) {
	resp, body := getOnTheWire(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, r.URL.Query().Get("q"))
	})
	assertSSEWireHeaders(t, resp)
	if body != sseEscaped {
		t.Fatalf("body = %q, want %q", body, sseEscaped)
	}
}

func TestSSEObserver_ExplicitWriteHeaderWithoutTypeIsPinnedAndEscaped(t *testing.T) {
	resp, body := getOnTheWire(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "data: "+r.URL.Query().Get("q")+"\n\n")
	})
	assertSSEWireHeaders(t, resp)
	if want := "data: " + sseEscaped + "\n\n"; body != want {
		t.Fatalf("body = %q, want %q", body, want)
	}
}

func TestSSEObserver_HTMLTypeIsReplacedAndEscaped(t *testing.T) {
	for name, explicitStatus := range map[string]bool{"implicit": false, "explicit": true} {
		t.Run(name, func(t *testing.T) {
			resp, body := getOnTheWire(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				if explicitStatus {
					w.WriteHeader(http.StatusOK)
				}
				_, _ = io.WriteString(w, r.URL.Query().Get("q"))
			})
			assertSSEWireHeaders(t, resp)
			if body != sseEscaped {
				t.Fatalf("body = %q, want %q", body, sseEscaped)
			}
		})
	}
}

// Chunks after the first are escaped too, including one that splits the
// markup, and a declared stream keeps every chunk as written.
func TestSSEObserver_ChunkedWrites(t *testing.T) {
	chunked := func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query().Get("q")
		_, _ = io.WriteString(w, "data: "+q[:5])
		_, _ = io.WriteString(w, q[5:]+"\n\n")
		_, _ = io.WriteString(w, "data: second\n\n")
	}
	t.Run("untyped", func(t *testing.T) {
		resp, body := getOnTheWire(t, chunked)
		assertSSEWireHeaders(t, resp)
		if want := "data: " + sseEscaped + "\n\ndata: second\n\n"; body != want {
			t.Fatalf("body = %q, want %q", body, want)
		}
	})
	t.Run("declared", func(t *testing.T) {
		resp, body := getOnTheWire(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			chunked(w, r)
		})
		assertSSEWireHeaders(t, resp)
		if want := "data: " + sseReflected + "\n\ndata: second\n\n"; body != want {
			t.Fatalf("body = %q, want %q", body, want)
		}
	})
}

// The recorder form of the check: the body the handler produced is escaped
// and the headers are in place after the handler returns.
func TestSSEObserver_RecorderBodyEscaped(t *testing.T) {
	rr := serveObserved(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, r.URL.Query().Get("q"))
	})
	if ct := rr.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("Content-Type = %q, want text/event-stream", ct)
	}
	if got := rr.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("X-Content-Type-Options = %q, want nosniff", got)
	}
	if strings.Contains(rr.Body.String(), "<script>") || rr.Body.String() != sseEscaped {
		t.Fatalf("body = %q, want %q", rr.Body.String(), sseEscaped)
	}
}

// Escaping lengthens the payload on the wire, so Write has to hand the
// caller its own length back or io.WriteString callers see a short write.
func TestSSEObserver_WriteReturnsCallerLengthWhenEscaping(t *testing.T) {
	rr := httptest.NewRecorder()
	obs := NewSSECloseObserver(rr, "test")
	n, err := obs.Write([]byte(sseReflected))
	if err != nil || n != len(sseReflected) {
		t.Fatalf("Write = (%d, %v), want (%d, nil)", n, err, len(sseReflected))
	}
	if rr.Body.String() != sseEscaped {
		t.Fatalf("body = %q, want %q", rr.Body.String(), sseEscaped)
	}
}

// headerSnapshotWriter records the Content-Type at each WriteHeader call, the
// moment net/http would snapshot it.
type headerSnapshotWriter struct {
	h     http.Header
	codes []int
	types []string
	body  strings.Builder
}

func (s *headerSnapshotWriter) Header() http.Header { return s.h }
func (s *headerSnapshotWriter) WriteHeader(code int) {
	s.codes = append(s.codes, code)
	s.types = append(s.types, s.h.Get("Content-Type"))
}
func (s *headerSnapshotWriter) Write(p []byte) (int, error) { return s.body.Write(p) }

// An informational status is followed by the final one; the type is pinned
// before the final status, not consumed by the 1xx.
func TestSSEObserver_InformationalStatusDoesNotSkipPinning(t *testing.T) {
	sw := &headerSnapshotWriter{h: http.Header{}}
	obs := NewSSECloseObserver(sw, "test")
	obs.WriteHeader(http.StatusEarlyHints)
	obs.Header().Set("Content-Type", "text/html")
	obs.WriteHeader(http.StatusOK)
	_, _ = obs.Write([]byte(sseReflected))
	if len(sw.codes) != 2 || sw.types[1] != "text/event-stream" {
		t.Fatalf("codes %v, Content-Type at each WriteHeader %v; want text/event-stream at the final one", sw.codes, sw.types)
	}
	if sw.body.String() != sseEscaped {
		t.Fatalf("body = %q, want %q", sw.body.String(), sseEscaped)
	}
}
