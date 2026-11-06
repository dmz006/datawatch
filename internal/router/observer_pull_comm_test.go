// Observer pull protocol — comm channel surface.
//
//	observer pull status → GET /api/observer/pull → reply to the group
//
// Loopback tests use the same test-server + drain-backend pattern as the
// other BL220 handler tests (no real messaging backend, no external
// network except the 127.0.0.1 test server).

package router

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const observerPullTestBody = `{
  "peers": [
    {
      "name": "edge-a",
      "source": "pull",
      "shadow": false,
      "stale": true,
      "stale_reason": "timeout",
      "last_success_at": "2026-05-01T00:00:00Z",
      "interval_s": 60,
      "timeout_s": 10
    },
    {
      "name": "edge-b",
      "source": "pull",
      "shadow": true,
      "stale": false,
      "stale_reason": "none",
      "last_success_at": "2026-05-01T01:00:00Z",
      "interval_s": 0,
      "timeout_s": 0
    }
  ]
}`

func observerPullTestMux(t *testing.T, status int, body string) *http.ServeMux {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/observer/pull", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	})
	return mux
}

func TestObserverPullComm_Parse(t *testing.T) {
	c := Parse("observer pull status")
	if c.Type != CmdObserver {
		t.Fatalf("type = %q, want %q", c.Type, CmdObserver)
	}
	if c.Text != "pull status" {
		t.Fatalf("text = %q, want %q", c.Text, "pull status")
	}
}

func TestObserverPullComm_StatusReply(t *testing.T) {
	srv := httptest.NewServer(observerPullTestMux(t, http.StatusOK, observerPullTestBody))
	defer srv.Close()

	r := loopbackRouter(t, srv)
	got := captureSends(r, func() {
		r.handleObserver(Command{Type: CmdObserver, Text: "pull status"})
	})
	for _, want := range []string{
		"[h] observer pull status",
		"edge-a",
		"timeout",
		"edge-b",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("reply missing %q in:\n%s", want, got)
		}
	}
}

func TestObserverPullComm_EndToEnd(t *testing.T) {
	srv := httptest.NewServer(observerPullTestMux(t, http.StatusOK, observerPullTestBody))
	defer srv.Close()

	r := loopbackRouter(t, srv)
	out := strings.Join(r.HandleTestMessage("observer pull status"), "\n")
	for _, want := range []string{"observer pull status", "edge-a"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

func TestObserverPullComm_BarePullShowsUsage(t *testing.T) {
	srv := httptest.NewServer(observerPullTestMux(t, http.StatusOK, observerPullTestBody))
	defer srv.Close()

	r := loopbackRouter(t, srv)
	got := captureSends(r, func() {
		r.handleObserver(Command{Type: CmdObserver, Text: "pull"})
	})
	if !strings.Contains(got, "usage: observer pull status") {
		t.Errorf("expected usage hint, got:\n%s", got)
	}
}

func TestObserverPullComm_UpstreamError(t *testing.T) {
	srv := httptest.NewServer(observerPullTestMux(t, http.StatusInternalServerError, "boom"))
	defer srv.Close()

	r := loopbackRouter(t, srv)
	got := captureSends(r, func() {
		r.handleObserver(Command{Type: CmdObserver, Text: "pull status"})
	})
	if !strings.Contains(got, "observer pull status failed") {
		t.Errorf("expected failure reply, got:\n%s", got)
	}
}

func TestObserverPullComm_UnknownVerbStillHelps(t *testing.T) {
	srv := httptest.NewServer(observerPullTestMux(t, http.StatusOK, observerPullTestBody))
	defer srv.Close()

	r := loopbackRouter(t, srv)
	got := captureSends(r, func() {
		r.handleObserver(Command{Type: CmdObserver, Text: "bogus"})
	})
	for _, want := range []string{"unknown verb bogus", "pull status"} {
		if !strings.Contains(got, want) {
			t.Errorf("expected %q in help, got:\n%s", want, got)
		}
	}
}
