package router

import (
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/session"
)

// chatAgainst delivers one chat message to a router with one session waiting
// for input, and reports what was replied, typed and asked of the daemon.
func chatAgainst(t *testing.T, mode, text string) (reply string, typed int, asked []string) {
	t.Helper()
	var mu sync.Mutex
	daemon := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		asked = append(asked, r.URL.Path)
		mu.Unlock()
		_, _ = w.Write([]byte(`{"mode":"report","overview":{"lists":[]}}`))
	}))
	t.Cleanup(daemon.Close)
	_, p, err := net.SplitHostPort(daemon.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	port, _ := strconv.Atoi(p)
	sm, err := session.NewManager("h", t.TempDir(), "echo", 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	allowTestTemp(sm)
	trackSessionManager(t, sm)
	t.Cleanup(sm.WaitBackground)
	f := sm.WithFakeTmux()
	f.Pane["cs-a"] = "What should I do next?\n> "
	now := time.Now()
	if err := sm.SaveSession(&session.Session{ID: "aa11", FullID: "h-aa11", Hostname: "h", TmuxSession: "cs-a",
		State: session.StateWaitingInput, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	r := &Router{hostname: "h", groupID: "g", manager: sm, backend: &captureBackend{name: "test", capture: func(string) {}}}
	r.SetWebPort(port)
	if mode != "" {
		cfg := &config.Config{}
		cfg.Acceptance.Mode = mode
		r.SetConfig(cfg)
	}
	reply = joinReplies(r.HandleTestMessage(text))
	mu.Lock()
	defer mu.Unlock()
	return reply, f.Count("send-keys") + f.Count("send-keys-settle"), append([]string(nil), asked...)
}

// While the setting is off, a message that only starts with the word
// "acceptance" goes where any other plain message goes.
func TestAcceptanceChat_OffLeavesPlainMessagesAlone(t *testing.T) {
	for _, mode := range []string{"", "off", "something-else"} {
		for _, text := range []string{"acceptance tests look good, carry on", "Acceptance criteria are met", "acceptance"} {
			word, rest, _ := strings.Cut(text, " ")
			plain := strings.TrimSpace("banana " + rest)
			reply, typed, asked := chatAgainst(t, mode, text)
			wantReply, wantTyped, wantAsked := chatAgainst(t, mode, plain)
			if Parse(text).Type != Parse(plain).Type || typed != wantTyped || len(asked) != len(wantAsked) ||
				strings.Contains(reply, "acceptance failed") || (reply == "") != (wantReply == "") {
				t.Errorf("mode %q, %q (%s): reply %q typed %d asked %v; a plain message: reply %q typed %d asked %v",
					mode, text, word, reply, typed, asked, wantReply, wantTyped, wantAsked)
			}
		}
	}
}

func TestAcceptanceChat_ReportModeAnswersTheCommand(t *testing.T) {
	reply, typed, asked := chatAgainst(t, "report", "acceptance list")
	if typed != 0 || !strings.Contains(reply, "no done-when lists") || len(asked) == 0 || asked[len(asked)-1] != "/api/acceptance" {
		t.Fatalf("reply %q typed %d asked %v", reply, typed, asked)
	}
}
