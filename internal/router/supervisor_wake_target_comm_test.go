package router

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// The wake verb sets and clears the designated session and replies with the
// target and why.
func TestSupervisorWakeCommDesignatesAndShowsTarget(t *testing.T) {
	var bodies []string
	_, port := newFakePeerParent(t, func(w http.ResponseWriter, rq *http.Request) {
		if rq.Method == http.MethodPut {
			b := make([]byte, 512)
			n, _ := rq.Body.Read(b)
			bodies = append(bodies, string(b[:n]))
		}
		_, _ = w.Write([]byte(`{"enabled":true,"session":"host-sup1","target":"","target_reason":"designated session host-sup1 was not found; nothing is typed"}`))
	})
	r := setupPeerRouter(t, port)
	got := strings.Join(r.HandleTestMessage("autonomous supervisor wake set session=host-sup1"), "\n")
	r.HandleTestMessage("autonomous supervisor wake set session=none")
	for _, want := range []string{"designated session: host-sup1", "target: none", "why: designated session host-sup1 was not found"} {
		if !strings.Contains(got, want) {
			t.Errorf("reply lacks %q: %q", want, got)
		}
	}
	if len(bodies) != 2 {
		t.Fatalf("config writes = %q", bodies)
	}
	for i, want := range []string{"host-sup1", ""} {
		var m map[string]any
		if err := json.Unmarshal([]byte(bodies[i]), &m); err != nil || m["supervisor_loop.wake.session"] != want {
			t.Errorf("write %d = %q, want session %q (%v)", i, bodies[i], want, err)
		}
	}
}
