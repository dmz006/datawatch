// handleChannelReady must type the task into a claude-code session that is not
// one-shot too. Every interactive `session new` build session of 2026-11-23
// came up at an empty prompt: the task went out as a channel notification that
// never reached the model, and nothing checked.

package server

import (
	"os"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/session"
)

func TestHandleChannelReady_InteractiveClaudeTaskIsTypedOnce(t *testing.T) {
	s, fake := channelReadyServer(t, t.TempDir())
	raw, err := os.ReadFile("../session/testdata/delivery/claude-2.1.331-startup-idle.txt")
	if err != nil {
		t.Fatal(err)
	}
	sess := &session.Session{
		FullID: "testhost-1a2b", ID: "1a2b", Hostname: "testhost",
		TmuxSession: "cs-1a2b", BackendFamily: "claude-code",
		State: session.StateRunning, OneShot: false,
		Task: redeliveryTask, CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	if err := s.manager.SaveSession(sess); err != nil {
		t.Fatal(err)
	}
	fake.Pane[sess.TmuxSession] = string(raw)

	postChannelReady(t, s, sess.FullID)
	if n := waitTaskSends(fake, 1, 6*time.Second); n != 1 {
		t.Fatalf("interactive claude session: task typed %d times, want exactly 1", n)
	}
	// The bridge's heartbeat must not type it again.
	postChannelReady(t, s, sess.FullID)
	if n := waitTaskSends(fake, 2, 2500*time.Millisecond); n != 1 {
		t.Fatalf("after a heartbeat: task typed %d times, want 1", n)
	}
}
