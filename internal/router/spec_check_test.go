package router

import (
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

const commSpecCheckDirty = `{"id":"p1","spec_check":[{"rule":"names_tracker_id","phrase":"BL123","severity":"warning","correctable":true,"suggestion":"remove 'BL123'"}]}`
const commSpecCheckClean = `{"id":"p1","spec_check":[]}`

func specCheckCommRouter(t *testing.T, reply string) *Router {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(reply))
	}))
	t.Cleanup(srv.Close)
	_, portStr, _ := net.SplitHostPort(srv.Listener.Addr().String())
	port, _ := strconv.Atoi(portStr)
	return &Router{hostname: "h", webPort: port, backend: &captureBackend{name: "test", capture: func(string) {}}}
}

func TestSpecCheckCommCreateAndReviseShowFindings(t *testing.T) {
	r := specCheckCommRouter(t, commSpecCheckDirty)
	for _, msg := range []string{"autonomous create Fix BL123 now", "autonomous request-revision p1 tighten"} {
		got := strings.Join(r.HandleTestMessage(msg), "\n")
		if !strings.Contains(got, "Spec check: 1 finding(s)") || !strings.Contains(got, `"BL123"`) || !strings.Contains(got, "remove 'BL123'") {
			t.Fatalf("%q: reply lacks the findings: %q", msg, got)
		}
	}
}

func TestSpecCheckCommCleanSpecShowsNone(t *testing.T) {
	r := specCheckCommRouter(t, commSpecCheckClean)
	for _, msg := range []string{"autonomous create Add a retry", "autonomous request-revision p1"} {
		got := strings.Join(r.HandleTestMessage(msg), "\n")
		if strings.Contains(got, "Spec check") {
			t.Fatalf("%q: clean spec printed findings: %q", msg, got)
		}
	}
}
