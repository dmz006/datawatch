package router

import (
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/session"
)

// A chat message that names no session is typed into the one waiting session.
// While that session shows an approval prompt, only a plain answer is typed:
// any other message plus Enter would approve it by accident.
func TestImplicitSendOnApprovalPromptNeedsAPlainAnswer(t *testing.T) {
	const pane = " Do you want to proceed?\n ❯ 1. Yes\n   2. No\n"
	for text, typed := range map[string]bool{
		"thanks, looking now": false, "ok": false, "what is it doing?": false,
		"yes": true, "No": true, "y": true, "2": true,
	} {
		t.Run(text, func(t *testing.T) {
			sm, err := session.NewManager("h", t.TempDir(), "echo", 30*time.Second)
			if err != nil {
				t.Fatal(err)
			}
			allowTestTemp(sm)
			trackSessionManager(t, sm)
			t.Cleanup(sm.WaitBackground)
			f := sm.WithFakeTmux()
			f.Pane["cs-a"] = pane
			now := time.Now()
			if err := sm.SaveSession(&session.Session{ID: "aa11", FullID: "h-aa11", Hostname: "h", TmuxSession: "cs-a",
				State: session.StateWaitingInput, CreatedAt: now, UpdatedAt: now}); err != nil {
				t.Fatal(err)
			}
			r := &Router{hostname: "h", manager: sm}
			out := captureSends(r, func() { r.handleImplicitSend(text) })
			got := f.Count("send-keys")+f.Count("send-keys-settle") > 0
			if got != typed {
				t.Fatalf("typed=%v want %v (reply %q)", got, typed, out)
			}
			if !typed && !strings.Contains(out, "send aa11:") {
				t.Errorf("refusal must say how to answer: %q", out)
			}
		})
	}
}
