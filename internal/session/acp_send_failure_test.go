// An opencode server that does not take a message is a failed send, not a
// success, and the text is not typed into the terminal the server runs in.

package session

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	opencode "github.com/dmz006/datawatch/internal/llm/backends/opencode"
)

func TestACPRefusalIsAFailedSend(t *testing.T) {
	cases := map[string]int{"error reply": http.StatusInternalServerError, "no such session": http.StatusNotFound}
	for label, code := range cases {
		t.Run(label, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodGet { // the event stream
					<-r.Context().Done()
					return
				}
				w.WriteHeader(code)
			}))
			defer srv.Close()
			defer srv.CloseClientConnections()
			mgr, f := newTestManagerWithFake(t)
			sess := addNamedHolder(t, mgr, f, "aa01", "worker", StateWaitingInput, idlePane)
			mgr.store.Mutate(func() { sess.BackendFamily = "opencode-acp" })
			tmux := "cs-acp-" + code2name(code)
			mgr.store.Mutate(func() { sess.TmuxSession = tmux })
			f.Pane[tmux] = idlePane
			opencode.ReconnectACP(tmux, sess.FullID, srv.URL, "s1", filepath.Join(t.TempDir(), "log"))
			time.Sleep(20 * time.Millisecond)

			if err := mgr.SendInput(sess.FullID, "do it", "web"); err == nil {
				t.Error("a refused message was reported as sent")
			}
			if got := typedInto(f, tmux); len(got) != 0 {
				t.Errorf("typed into the server's terminal: %q", got)
			}
		})
	}
}

func code2name(code int) string {
	if code == http.StatusNotFound {
		return "404"
	}
	return "500"
}
