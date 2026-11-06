// Tests for the `compute headroom <node>` and `compute headroom measure <node>`
// comm-channel verbs. The fake daemon plays the node's /headroom REST route:
// it records the method and path and answers the success, busy-node and
// never-measured cases the way the server does.
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

type headroomVerbDaemon struct {
	mu     sync.Mutex
	method string
	path   string
	calls  int
}

func newHeadroomVerbRouter(t *testing.T, d *headroomVerbDaemon) *Router {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)
		d.mu.Lock()
		d.calls++
		d.method, d.path = r.Method, r.URL.Path
		d.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/busy-box/headroom"):
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte(`{"error":"node busy","node":"busy-box","held":1}`))
		case strings.HasSuffix(r.URL.Path, "/never/headroom"):
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte("no headroom measurement for node never"))
		case r.Method == http.MethodPost:
			_, _ = w.Write([]byte(`{"node":"gpu-1","supported_cap":15,"fresh":true}`))
		default:
			_, _ = w.Write([]byte(`{"node":"gpu-1","age_seconds":30,"fresh":true}`))
		}
	}))
	t.Cleanup(srv.Close)
	port, err := strconv.Atoi(srv.URL[strings.LastIndex(srv.URL, ":")+1:])
	if err != nil {
		t.Fatalf("port from %q: %v", srv.URL, err)
	}
	r := &Router{hostname: "h", backend: &captureBackend{name: "test", capture: func(string) {}}}
	r.SetWebPort(port)
	return r
}

func TestComputeHeadroomGet_ReadsStoredView(t *testing.T) {
	d := &headroomVerbDaemon{}
	r := newHeadroomVerbRouter(t, d)
	out := strings.Join(r.HandleTestMessage("compute headroom gpu-1"), "\n")
	if d.method != http.MethodGet || d.path != "/api/compute/nodes/gpu-1/headroom" {
		t.Fatalf("request = %s %s, want GET /api/compute/nodes/gpu-1/headroom", d.method, d.path)
	}
	if !strings.Contains(out, "age_seconds") {
		t.Errorf("reply %q, want the stored view", out)
	}
}

func TestComputeHeadroomMeasure_PostsAndReplies(t *testing.T) {
	d := &headroomVerbDaemon{}
	r := newHeadroomVerbRouter(t, d)
	out := strings.Join(r.HandleTestMessage("compute headroom measure gpu-1"), "\n")
	if d.method != http.MethodPost || d.path != "/api/compute/nodes/gpu-1/headroom" {
		t.Fatalf("request = %s %s, want POST /api/compute/nodes/gpu-1/headroom", d.method, d.path)
	}
	if !strings.Contains(out, "supported_cap") {
		t.Errorf("reply %q, want the measured view", out)
	}
}

func TestComputeHeadroomMeasure_BusyNodeReplyNamesRefusal(t *testing.T) {
	d := &headroomVerbDaemon{}
	r := newHeadroomVerbRouter(t, d)
	out := strings.Join(r.HandleTestMessage("compute headroom measure busy-box"), "\n")
	if !strings.Contains(out, "node busy") {
		t.Errorf("busy reply %q, want the refusal", out)
	}
}

func TestComputeHeadroomGet_NoMeasurementYetReplies404Text(t *testing.T) {
	d := &headroomVerbDaemon{}
	r := newHeadroomVerbRouter(t, d)
	out := strings.Join(r.HandleTestMessage("compute headroom never"), "\n")
	if !strings.Contains(out, "no headroom measurement") {
		t.Errorf("reply %q, want the never-measured text", out)
	}
}

func TestComputeHeadroom_UsageAndBadArgsDoNotCallDaemon(t *testing.T) {
	d := &headroomVerbDaemon{}
	r := newHeadroomVerbRouter(t, d)
	for _, msg := range []string{"compute headroom", "compute headroom measure", "compute headroom a b c"} {
		out := strings.Join(r.HandleTestMessage(msg), "\n")
		if !strings.Contains(out, "Usage") {
			t.Errorf("%q reply %q, want usage text", msg, out)
		}
	}
	if d.calls != 0 {
		t.Errorf("usage or bad arguments reached the daemon %d times", d.calls)
	}
}
