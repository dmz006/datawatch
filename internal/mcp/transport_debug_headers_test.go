package mcp

import (
	"encoding/json"
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
	// sseJSONEscaped is sseReflected inside a JSON string once < and > are
	// written as \u escapes; it decodes back to sseReflected.
	sseJSONEscaped = `\u003cscript\u003ealert(1)\u003c/script\u003e`
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
	defer func() { _ = resp.Body.Close() }()
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
// the payload is JSON-RPC, so the markup characters leave as JSON \u escapes
// and the client decodes exactly the value the handler wrote.
func TestSSEObserver_DeclaredEventStreamKeepsJSONMeaning(t *testing.T) {
	resp, body := getOnTheWire(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		raw, _ := json.Marshal(map[string]string{"q": r.URL.Query().Get("q")})
		_, _ = io.WriteString(w, "data: "+string(raw)+"\n\n")
	})
	assertSSEWireHeaders(t, resp)
	if strings.Contains(body, "<script>") {
		t.Fatalf("raw markup on the wire: %q", body)
	}
	data, ok := strings.CutPrefix(strings.TrimSpace(body), "data: ")
	if !ok {
		t.Fatalf("body = %q, want a data: line", body)
	}
	var got map[string]string
	if err := json.Unmarshal([]byte(data), &got); err != nil {
		t.Fatalf("data line is not JSON: %v (%q)", err, body)
	}
	if got["q"] != sseReflected {
		t.Fatalf("decoded q = %q, want %q", got["q"], sseReflected)
	}
}

// The request echoes <script>alert(1)</script> into the stream the way a
// JSON-RPC result can echo a tool argument. Whatever the handler declared (or
// forgot to), the client gets a non-HTML type, nosniff and no raw markup.
func TestSSEObserver_EchoedScriptNeverReachesClientRaw(t *testing.T) {
	cases := map[string]http.HandlerFunc{
		"declared event-stream": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, `data: {"echo":"`+r.URL.Query().Get("q")+`"}`+"\n\n")
		},
		"no type, implicit 200": func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.WriteString(w, r.URL.Query().Get("q"))
		},
		"no type, explicit 200": func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, r.URL.Query().Get("q"))
		},
		"html type": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html")
			_, _ = io.WriteString(w, r.URL.Query().Get("q"))
		},
	}
	for name, h := range cases {
		t.Run(name, func(t *testing.T) {
			resp, body := getOnTheWire(t, h)
			if strings.Contains(strings.ToLower(resp.Header.Get("Content-Type")), "html") {
				t.Fatalf("Content-Type %q is HTML", resp.Header.Get("Content-Type"))
			}
			assertSSEWireHeaders(t, resp)
			if strings.Contains(body, "<script>") || strings.Contains(body, "</script>") {
				t.Fatalf("raw markup in body: %q", body)
			}
			if !strings.Contains(body, "alert(1)") {
				t.Fatalf("echoed value lost from body: %q", body)
			}
		})
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
// markup, and a declared stream escapes every chunk as JSON.
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
		if want := "data: " + sseJSONEscaped + "\n\ndata: second\n\n"; body != want {
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

// Only < and > leave a declared stream as \u escapes: quotes, ampersands and
// literal entity text in JSON strings and in plain data lines (the endpoint
// event) arrive as written, so the framing and the decoded values hold.
func TestSSEObserver_DeclaredStreamKeepsNonMarkupBytes(t *testing.T) {
	const payload = "event: endpoint\ndata: /message?a=1&b=2\n\n" +
		`data: {"s":"say \"hi\" it's &lt; & &amp; done"}` + "\n\n"
	resp, body := getOnTheWire(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, payload)
	})
	assertSSEWireHeaders(t, resp)
	if body != payload {
		t.Fatalf("body = %q, want %q", body, payload)
	}
}

// A request the observer does not wrap (a POST, or a GET that did not ask for
// text/event-stream) is answered by the same listener and still carries
// nosniff on the wire.
func TestSSEObserver_UnwrappedRequestsCarryNoSniff(t *testing.T) {
	srv := httptest.NewServer(ObserveSSEHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusAccepted)
	})))
	defer srv.Close()
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		req, err := http.NewRequest(method, srv.URL+"/message?q="+sseReflected, nil)
		if err != nil {
			t.Fatal(err)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		if got := resp.Header.Get("X-Content-Type-Options"); got != "nosniff" {
			t.Errorf("%s without an event-stream Accept: X-Content-Type-Options %q, want nosniff", method, got)
		}
	}
}
