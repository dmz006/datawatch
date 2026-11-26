package opencode

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// The server answers a message when the model's turn is over. A message it has
// received but not yet answered is delivered; one that never reached it is not.
func TestSendACPTellsALateReplyFromAFailedSend(t *testing.T) {
	old := replyWait
	replyWait = 200 * time.Millisecond
	t.Cleanup(func() { replyWait = old })

	release := make(chan struct{})
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-release }))
	defer slow.Close()
	defer close(release)
	gone := httptest.NewServer(http.NotFoundHandler())
	goneURL := gone.URL
	gone.Close()

	for name, url := range map[string]string{"late": slow.URL, "refused": goneURL} {
		acpStateMap.Store("cs-"+name, &acpSessionState{baseURL: url, sessionID: "s1"})
		t.Cleanup(func() { acpStateMap.Delete("cs-" + name) })
	}
	if active, err := SendACP("cs-late", "x"); !active || err != nil {
		t.Errorf("late reply: active=%v err=%v, want delivered", active, err)
	}
	if active, err := SendACP("cs-refused", "x"); !active || err == nil {
		t.Errorf("refused connection: active=%v err=%v, want an error", active, err)
	}
	if active, err := SendACP("cs-none", "x"); active || err != nil {
		t.Errorf("no ACP session: active=%v err=%v", active, err)
	}
}
