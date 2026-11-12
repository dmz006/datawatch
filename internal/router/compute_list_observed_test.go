package router

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

func newComputeListRouter(t *testing.T, body string) *Router {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path != "/api/compute/nodes" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	port, err := strconv.Atoi(srv.URL[strings.LastIndex(srv.URL, ":")+1:])
	if err != nil {
		t.Fatal(err)
	}
	r := &Router{hostname: "h", backend: &captureBackend{name: "test", capture: func(string) {}}}
	r.SetWebPort(port)
	return r
}

func TestComputeNodeListObservedSections(t *testing.T) {
	cases := []struct {
		name, body string
		want       []string
	}{
		{"peers and unanswered",
			`{"nodes":[{"name":"n1","kind":"ollama","address":"a:1"}],"observed_peers":[{"name":"peer-a"}],"unanswered_auto_created":[{"name":"ghost","address":"b:2","kind":"ollama"}]}`,
			[]string{"n1 (ollama, a:1)", "Observed peers with no node:", "peer-a - Add as a compute node: compute node add peer-a kind=<kind> address=<address you enter>", "Auto-created nodes that never answered:", "ghost (ollama, b:2)"}},
		{"empty reports",
			`{"nodes":[],"observed_peers":[],"unanswered_auto_created":[]}`,
			[]string{"Observed peers with no node:\n  (none)", "Auto-created nodes that never answered:\n  (none)"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := newComputeListRouter(t, c.body)
			for _, verb := range []string{"compute", "compute node", "compute node list"} {
				got := lastReply(t, r.HandleTestMessage(verb))
				for _, w := range c.want {
					if !strings.Contains(got, w) {
						t.Errorf("%q reply missing %q in:\n%s", verb, w, got)
					}
				}
			}
		})
	}
}

func TestComputeUsageMentionsObservedReports(t *testing.T) {
	if !strings.Contains(computeUsage, "observed peers with no node") {
		t.Error("usage text should mention the observed-peers report")
	}
}
