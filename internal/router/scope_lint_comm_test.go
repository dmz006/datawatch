package router

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

// scopeLintDaemon answers the loopback GET that config-set makes first and
// records any PUT, so a test can see whether a value was sent at all.
type scopeLintDaemon struct {
	puts []string
}

func (d *scopeLintDaemon) serve(t *testing.T) (int, func()) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPut {
			b, _ := io.ReadAll(r.Body)
			d.puts = append(d.puts, string(b))
		}
		_, _ = w.Write([]byte(`{}`))
	}))
	port, err := strconv.Atoi(srv.URL[strings.LastIndex(srv.URL, ":")+1:])
	if err != nil {
		t.Fatalf("parse loopback port from %q: %v", srv.URL, err)
	}
	return port, srv.Close
}

func TestScopeLintComm_ConfigSetAcceptsEachMode(t *testing.T) {
	for _, mode := range []string{"off", "warn", "enforce"} {
		t.Run(mode, func(t *testing.T) {
			d := &scopeLintDaemon{}
			port, closeFn := d.serve(t)
			defer closeFn()

			r := &Router{hostname: "h", backend: &captureBackend{name: "test", capture: func(string) {}}}
			r.SetWebPort(port)
			replies := r.HandleTestMessage("autonomous config-set scope_lint=" + mode)

			if len(d.puts) != 1 {
				t.Fatalf("want one PUT, got %d (replies=%q)", len(d.puts), replies)
			}
			var body map[string]any
			if err := json.Unmarshal([]byte(d.puts[0]), &body); err != nil {
				t.Fatalf("PUT body is not JSON: %v", err)
			}
			if body["scope_lint"] != mode {
				t.Fatalf("scope_lint = %v, want %q (body=%s)", body["scope_lint"], mode, d.puts[0])
			}
		})
	}
}

func TestScopeLintComm_ConfigSetAcceptsConfigureSpelling(t *testing.T) {
	d := &scopeLintDaemon{}
	port, closeFn := d.serve(t)
	defer closeFn()

	r := &Router{
		hostname: "h",
		backend:  &captureBackend{name: "test", capture: func(string) {}},
	}
	r.SetWebPort(port)
	r.HandleTestMessage("autonomous config-set autonomous.scope_lint=warn")

	if len(d.puts) != 1 {
		t.Fatalf("want one PUT, got %d", len(d.puts))
	}
	if !strings.Contains(d.puts[0], `"scope_lint":"warn"`) || strings.Contains(d.puts[0], "autonomous.scope_lint") {
		t.Fatalf("autonomous.scope_lint not normalised to scope_lint: %s", d.puts[0])
	}
}

func TestScopeLintComm_ConfigSetRejectsBadValue(t *testing.T) {
	d := &scopeLintDaemon{}
	port, closeFn := d.serve(t)
	defer closeFn()

	r := &Router{hostname: "h", backend: &captureBackend{name: "test", capture: func(string) {}}}
	r.SetWebPort(port)
	replies := r.HandleTestMessage("autonomous config-set scope_lint=strict")

	if len(d.puts) != 0 {
		t.Fatalf("bad scope_lint was sent to the daemon: %v", d.puts)
	}
	found := false
	for _, reply := range replies {
		if strings.Contains(reply, "config-set failed") && strings.Contains(reply, "scope_lint") {
			found = true
		}
	}
	if !found {
		t.Fatalf("want a failed reply naming scope_lint, got %q", replies)
	}
}
