// Test file for the `compute node drain <name> [until=<RFC3339>]
// [reason=<text>]` and `compute node undrain <name>` comm-channel verbs.
// The fake daemon plays the role of /api/compute/nodes/<name>/drain: it
// records the method, path and body and replies with the node health JSON
// the way the server handler does.
package router

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
)

const drainTestUntil = "2026-11-02T09:30:00Z"

type computeDrainFakeDaemon struct {
	mu       sync.Mutex
	requests int
	method   string
	path     string
	body     map[string]any
}

func newComputeDrainRouter(t *testing.T, d *computeDrainFakeDaemon) *Router {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		if !strings.HasPrefix(r.URL.Path, "/api/compute/nodes/") || !strings.HasSuffix(r.URL.Path, "/drain") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		name := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/compute/nodes/"), "/drain")
		body := map[string]any{}
		_ = json.Unmarshal(raw, &body)
		d.mu.Lock()
		d.requests++
		d.method, d.path = r.Method, r.URL.Path
		d.body = body
		d.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodDelete {
			_, _ = w.Write([]byte(`{"name":"` + name + `","drained":false,"drain_active":false}`))
			return
		}
		out := map[string]any{"name": name, "drained": true, "drain_active": true}
		if until, ok := body["until"].(string); ok && until != "" {
			out["drain_until"] = until
		}
		if reason, ok := body["reason"].(string); ok && reason != "" {
			out["drain_reason"] = reason
		}
		b, _ := json.Marshal(out)
		_, _ = w.Write(b)
	}))
	t.Cleanup(srv.Close)
	port, err := strconv.Atoi(srv.URL[strings.LastIndex(srv.URL, ":")+1:])
	if err != nil {
		t.Fatalf("port from %q: %v", srv.URL, err)
	}
	r := &Router{
		hostname: "h",
		backend:  &captureBackend{name: "test", capture: func(string) {}},
	}
	r.SetWebPort(port)
	return r
}

// TestComputeNodeDrainWithUntil runs the full verb and checks the request
// method, path and body the daemon sees and the one-line reply.
func TestComputeNodeDrainWithUntil(t *testing.T) {
	d := &computeDrainFakeDaemon{}
	r := newComputeDrainRouter(t, d)
	replies := r.HandleTestMessage("compute node drain gpu-box-1 until=" + drainTestUntil + " reason=maintenance")

	if d.method != http.MethodPost || d.path != "/api/compute/nodes/gpu-box-1/drain" {
		t.Fatalf("request = %s %s, want POST /api/compute/nodes/gpu-box-1/drain", d.method, d.path)
	}
	if d.body["until"] != drainTestUntil {
		t.Errorf("body until = %v, want %v", d.body["until"], drainTestUntil)
	}
	if d.body["reason"] != "maintenance" {
		t.Errorf("body reason = %v, want maintenance", d.body["reason"])
	}
	if got := lastReply(t, replies); !strings.Contains(got, "drained gpu-box-1 until "+drainTestUntil) {
		t.Errorf("reply = %q, want it to contain %q", got, "drained gpu-box-1 until "+drainTestUntil)
	}
}

// TestComputeNodeDrainWithoutUntil is the open-ended form: no until or reason
// key is sent, and the reply says so.
func TestComputeNodeDrainWithoutUntil(t *testing.T) {
	d := &computeDrainFakeDaemon{}
	r := newComputeDrainRouter(t, d)
	replies := r.HandleTestMessage("compute node drain gpu-box-1")

	if d.method != http.MethodPost || d.path != "/api/compute/nodes/gpu-box-1/drain" {
		t.Fatalf("request = %s %s, want POST /api/compute/nodes/gpu-box-1/drain", d.method, d.path)
	}
	if until, present := d.body["until"]; present {
		t.Errorf("body carries until=%v, want absent for an open-ended drain", until)
	}
	if got := lastReply(t, replies); !strings.Contains(got, "drained gpu-box-1 with no end time") {
		t.Errorf("reply = %q, want it to contain %q", got, "drained gpu-box-1 with no end time")
	}
}

// TestComputeNodeDrainRejectsBadUntil makes sure a non-RFC3339 until is
// rejected before anything reaches the daemon.
func TestComputeNodeDrainRejectsBadUntil(t *testing.T) {
	d := &computeDrainFakeDaemon{}
	r := newComputeDrainRouter(t, d)
	replies := r.HandleTestMessage("compute node drain gpu-box-1 until=next-week")

	if d.requests != 0 {
		t.Fatalf("daemon saw %d request(s); a bad until must not reach it", d.requests)
	}
	if got := lastReply(t, replies); !strings.Contains(got, "until must be an RFC3339") {
		t.Errorf("reply = %q, want it to naming the RFC3339 requirement", got)
	}
}

// TestComputeNodeUndrain sends DELETE on the same path and gets the
// one-line "undrained" reply. Extra keys are a usage error.
func TestComputeNodeUndrain(t *testing.T) {
	d := &computeDrainFakeDaemon{}
	r := newComputeDrainRouter(t, d)
	replies := r.HandleTestMessage("compute node undrain gpu-box-1")

	if d.method != http.MethodDelete || d.path != "/api/compute/nodes/gpu-box-1/drain" {
		t.Fatalf("request = %s %s, want DELETE /api/compute/nodes/gpu-box-1/drain", d.method, d.path)
	}
	if got := lastReply(t, replies); !strings.Contains(got, "undrained gpu-box-1") {
		t.Errorf("reply = %q, want it to contain %q", got, "undrained gpu-box-1")
	}

	replies = r.HandleTestMessage("compute node undrain gpu-box-1 until=" + drainTestUntil)
	if got := lastReply(t, replies); !strings.Contains(got, "Usage: compute node undrain <name>") {
		t.Errorf("reply = %q, want the undrain usage line", got)
	}
}

// TestComputeNodeDrainUsageListsBothVerbs checks the help text and the
// missing-name usage replies.
func TestComputeNodeDrainUsageListsBothVerbs(t *testing.T) {
	for _, want := range []string{
		"compute node drain <name> [until=<RFC3339>] [reason=<text>]",
		"compute node undrain <name>",
	} {
		if !strings.Contains(computeUsage, want) {
			t.Errorf("computeUsage lacks %q", want)
		}
	}
	d := &computeDrainFakeDaemon{}
	r := newComputeDrainRouter(t, d)

	if got := lastReply(t, r.HandleTestMessage("compute help")); !strings.Contains(got, "drain <name>") || !strings.Contains(got, "undrain <name>") {
		t.Errorf("`compute help` reply lacks the drain verbs:\n%s", got)
	}
	if got := lastReply(t, r.HandleTestMessage("compute node drain")); !strings.Contains(got, "Usage: compute node drain <name>") {
		t.Errorf("missing-name reply = %q, want the drain usage line", got)
	}
	if got := lastReply(t, r.HandleTestMessage("compute node undrain")); !strings.Contains(got, "Usage: compute node undrain <name>") {
		t.Errorf("missing-name reply = %q, want the undrain usage line", got)
	}
	if d.requests != 0 {
		t.Errorf("usage replies must not reach the daemon (saw %d request(s))", d.requests)
	}
}
