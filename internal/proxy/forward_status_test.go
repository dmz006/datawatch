// A forwarded command the peer did not accept is a failure, not an empty
// success.

package proxy

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

func TestForwardCommandReportsFailures(t *testing.T) {
	cases := map[string]struct {
		status int
		body   string
		want   string
	}{
		"401":              {http.StatusUnauthorized, "unauthorized: SECRET-ECHO\n", "401"},
		"403":              {http.StatusForbidden, `{"error":"forbidden"}`, "403"},
		"500":              {http.StatusInternalServerError, "boom", "500"},
		"503 with a reply": {http.StatusServiceUnavailable, `{"responses":["ok"]}`, "503"},
		"unreadable reply": {http.StatusOK, "<html>sign in</html>", "may have run"},
		"not a reply":      {http.StatusOK, `{"error":"nope"}`, "may have run"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(c.status)
				_, _ = w.Write([]byte(c.body))
			}))
			defer ts.Close()
			d := NewRemoteDispatcher([]config.RemoteServerConfig{{Name: "peer", URL: ts.URL, Enabled: true}})
			got, err := d.ForwardCommand("peer", "send ab12: run the SECRET-COMMAND")
			if err == nil {
				t.Fatalf("no error, replies %q", got)
			}
			if !strings.Contains(err.Error(), c.want) || !strings.Contains(err.Error(), "peer") {
				t.Errorf("error %q, want it to name the server and say %q", err, c.want)
			}
			if strings.Contains(err.Error(), "SECRET") {
				t.Errorf("error %q repeats the command or the peer's body", err)
			}
		})
	}
}

// A reply with no lines is still a success: the peer answers an empty list.
func TestForwardCommandAcceptsAnEmptyReplyList(t *testing.T) {
	for _, body := range []string{`{"responses":[],"count":0}`, `{"responses":null,"count":0}`} {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(body)) }))
		d := NewRemoteDispatcher([]config.RemoteServerConfig{{Name: "peer", URL: ts.URL, Enabled: true}})
		if got, err := d.ForwardCommand("peer", "help"); err != nil || len(got) != 0 {
			t.Errorf("%s: got %q, %v", body, got, err)
		}
		ts.Close()
	}
}
