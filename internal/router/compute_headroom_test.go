// Router tests for `compute headroom` commands (measure / show) and
// verifier_slots k=v parsing in node add/update.

package router

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// headroomFakeDaemon captures the last request and returns a configurable
// status/body, unlike the computeTLSFakeDaemon which is node-specific.
type headroomFakeDaemon struct {
	mu         sync.Mutex
	gotMethod  string
	gotPath    string
	gotBody    string
	replyCode  int
	replyBody  string
}

func newHeadroomRouter(t *testing.T, d *headroomFakeDaemon) *Router {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		d.mu.Lock()
		d.gotMethod = r.Method
		d.gotPath = r.URL.Path
		d.gotBody = string(raw)
		code := d.replyCode
		body := d.replyBody
		d.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if code == 0 {
			code = http.StatusOK
		}
		w.WriteHeader(code)
		if body != "" {
			_, _ = w.Write([]byte(body))
		}
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

// TestComputeHeadroomMeasureDispatch verifies `compute headroom measure <node>`
// sends a POST to the correct path and relays the response.
func TestComputeHeadroomMeasureDispatch(t *testing.T) {
	d := &headroomFakeDaemon{
		replyCode: http.StatusOK,
		replyBody: `{"node":"gpu-a","free_mb":8192,"ok":true}`,
	}
	r := newHeadroomRouter(t, d)
	replies := r.HandleTestMessage("compute headroom measure gpu-a")
	got := lastReply(t, replies)

	d.mu.Lock()
	if d.gotMethod != http.MethodPost {
		t.Errorf("method = %s, want POST", d.gotMethod)
	}
	if d.gotPath != "/api/compute/nodes/gpu-a/headroom" {
		t.Errorf("path = %s, want /api/compute/nodes/gpu-a/headroom", d.gotPath)
	}
	d.mu.Unlock()

	if !strings.Contains(got, "gpu-a") || !strings.Contains(got, "8192") {
		t.Errorf("reply %q missing node or free_mb", got)
	}
}

// TestComputeHeadroomShowDispatch verifies `compute headroom <node>` (no
// "measure" keyword) sends a GET to the same path.
func TestComputeHeadroomShowDispatch(t *testing.T) {
	d := &headroomFakeDaemon{
		replyCode: http.StatusOK,
		replyBody: `{"node":"gpu-a","free_mb":4096,"verifier_slots":3}`,
	}
	r := newHeadroomRouter(t, d)
	replies := r.HandleTestMessage("compute headroom gpu-a")
	got := lastReply(t, replies)

	d.mu.Lock()
	if d.gotMethod != http.MethodGet {
		t.Errorf("method = %s, want GET", d.gotMethod)
	}
	if d.gotPath != "/api/compute/nodes/gpu-a/headroom" {
		t.Errorf("path = %s, want /api/compute/nodes/gpu-a/headroom", d.gotPath)
	}
	d.mu.Unlock()

	if !strings.Contains(got, "4096") {
		t.Errorf("reply %q missing free_mb", got)
	}
}

// TestComputeHeadroomMeasureRefusal verifies that a 409 refusal from the
// daemon is relayed with the node name, free memory, and shortfall.
func TestComputeHeadroomMeasureRefusal(t *testing.T) {
	refusal := `{"error":"verifier cap 4 on node gpu-a refused: measured free 2048 MB, need 2560 MB, shortfall 512 MB","node":"gpu-a","free_mb":2048,"shortfall_mb":512}`
	d := &headroomFakeDaemon{
		replyCode: http.StatusConflict,
		replyBody: refusal,
	}
	r := newHeadroomRouter(t, d)
	replies := r.HandleTestMessage("compute headroom measure gpu-a")
	got := lastReply(t, replies)

	// The reply should contain the error text with node, free MB, and shortfall.
	for _, want := range []string{"409", "gpu-a", "2048", "512", "shortfall"} {
		if !strings.Contains(got, want) {
			t.Errorf("reply %q does not contain %q", got, want)
		}
	}
}

// TestComputeHeadroomUsage verifies that `compute headroom` with no args
// prints the usage text.
func TestComputeHeadroomUsage(t *testing.T) {
	d := &headroomFakeDaemon{}
	r := newHeadroomRouter(t, d)
	replies := r.HandleTestMessage("compute headroom")
	got := lastReply(t, replies)

	if !strings.Contains(got, "measure") || !strings.Contains(got, "Usage") {
		t.Errorf("reply %q does not look like usage text", got)
	}
}
