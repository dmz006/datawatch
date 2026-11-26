// The chat verb "supervisor wake" shows the pass count beside the count of
// typed event wakes.

package router

import (
	"net/http"
	"strings"
	"testing"
)

func TestSupervisorWakeStatusShowsPassCount(t *testing.T) {
	body := strings.Replace(supervisorWakeCommBody, `"event_count":0`, `"event_count":2,"pass_count":9`, 1)
	_, port := laneFillCommDaemon(t, http.StatusOK, body)
	r := setupPeerRouter(t, port)
	got := strings.Join(r.HandleTestMessage("autonomous supervisor wake"), "\n")
	for _, want := range []string{"(2 events)", "event wake passes run: 9"} {
		if !strings.Contains(got, want) {
			t.Errorf("reply lacks %q: %q", want, got)
		}
	}
}
