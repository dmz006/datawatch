// handleChannelReady must deliver a OneShot session's task at most once.
//
// Found live 2026-10-14 (sessions 348b/258d): the channel bridge re-POSTs
// /api/channel/ready every 60 s (cmd/datawatch-channel readyHeartbeat, so a
// restarted daemon re-learns bridge ports). handleChannelReady re-delivered
// the full task on every one of those heartbeats that found the session in
// waiting_input — 348b accumulated ~80 "[Pasted text #N]" chips. See
// docs/plans/2026-10-14-cli-self-update-dialog-recovery.md.

package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/session"
)

const redeliveryTask = "You are running non-interactively as part of an automated pipeline. TASK-MARKER-7f3a"

// channelReadyServer is bl90Server with a caller-owned data dir, so a test
// can rebuild the Manager on the same dir to simulate a daemon restart.
func channelReadyServer(t *testing.T, dir string) (*Server, *session.FakeTmux) {
	t.Helper()
	mgr, err := session.NewManager("testhost", dir, "/bin/echo", 0)
	if err != nil {
		t.Fatal(err)
	}
	allowTestTemp(mgr)
	t.Cleanup(mgr.WaitBackground)
	fake := mgr.WithFakeTmux()
	s := &Server{
		manager:  mgr,
		hostname: "testhost",
		cfg: &config.Config{
			Session: config.SessionConfig{MaxSessions: 10, ScheduleSettleMs: 200, TailLines: 20},
			Server:  config.ServerConfig{Host: "127.0.0.1", Port: 8080},
		},
		cfgPath: dir + "/cfg.yaml",
	}
	s.hub = NewHub()
	return s, fake
}

// taskSends counts send-keys calls that carried the task text.
func taskSends(f *session.FakeTmux) int {
	n := 0
	for _, c := range f.CallsSnapshot() {
		if !strings.HasPrefix(c.Op, "send-keys") {
			continue
		}
		for _, a := range c.Args {
			if strings.Contains(a, "TASK-MARKER-7f3a") {
				n++
				break
			}
		}
	}
	return n
}

func postChannelReady(t *testing.T, s *Server, sessID string) {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"session_id": sessID, "port": 40000})
	req := httptest.NewRequest(http.MethodPost, "/api/channel/ready", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	s.handleChannelReady(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("POST /api/channel/ready: status %d body=%s", rr.Code, rr.Body.String())
	}
}

// waitTaskSends waits until the task has been sent at least want times or
// the timeout passes, and returns the final count.
func waitTaskSends(f *session.FakeTmux, want int, timeout time.Duration) int {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if taskSends(f) >= want {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	return taskSends(f)
}

// setWaiting puts the session back at the idle ❯ prompt, as the monitor
// does when Claude finishes a turn but keeps background work running.
func setWaiting(t *testing.T, s *Server, sessID string) {
	t.Helper()
	sess, ok := s.manager.GetSession(sessID)
	if !ok {
		t.Fatal("session vanished")
	}
	sess.State = session.StateWaitingInput
	if err := s.manager.SaveSession(sess); err != nil {
		t.Fatal(err)
	}
}

func seedOneShot(t *testing.T, s *Server, fake *session.FakeTmux, id, pane string) string {
	t.Helper()
	sess := &session.Session{
		FullID: "testhost-" + id, ID: id, Hostname: "testhost",
		TmuxSession: "cs-" + id, BackendFamily: "claude-code",
		State: session.StateWaitingInput, OneShot: true,
		Task: redeliveryTask, CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	if err := s.manager.SaveSession(sess); err != nil {
		t.Fatal(err)
	}
	fake.Pane[sess.TmuxSession] = pane
	return sess.FullID
}

func TestHandleChannelReady_HeartbeatNeverRepastesOneShotTask(t *testing.T) {
	dir := t.TempDir()
	s, fake := channelReadyServer(t, dir)
	raw, err := os.ReadFile("../session/testdata/claude_self_update_dialog.txt")
	if err != nil {
		t.Fatal(err)
	}
	// Pane shows the CLI update notice plus the idle prompt, as in 348b.
	sessID := seedOneShot(t, s, fake, "258d", string(raw)+"\n❯ \n")

	// First ready: the real, first-time delivery.
	postChannelReady(t, s, sessID)
	if n := waitTaskSends(fake, 1, 4*time.Second); n != 1 {
		t.Fatalf("first ready: task sent %d times, want exactly 1", n)
	}

	// Heartbeat re-announces while the session idles at the prompt.
	for i := 0; i < 3; i++ {
		setWaiting(t, s, sessID)
		postChannelReady(t, s, sessID)
		if n := waitTaskSends(fake, 2, 2500*time.Millisecond); n != 1 {
			t.Fatalf("heartbeat #%d: task sent %d times total, want 1 — task re-pasted into a session that already had it", i+1, n)
		}
	}

	// Heartbeat healing still works: ChannelReady/ChannelPort stay set.
	got, _ := s.manager.GetSession(sessID)
	if !got.ChannelReady || got.ChannelPort != 40000 {
		t.Errorf("heartbeat re-announce lost channel state: ready=%v port=%d", got.ChannelReady, got.ChannelPort)
	}

	// Daemon restart: rebuild the Manager on the same data dir; the
	// bridge's next heartbeat must not deliver the task again either.
	s2, fake2 := channelReadyServer(t, dir)
	setWaiting(t, s2, sessID)
	fake2.Pane["cs-258d"] = "❯ \n"
	postChannelReady(t, s2, sessID)
	if n := waitTaskSends(fake2, 1, 2500*time.Millisecond); n != 0 {
		t.Fatalf("after daemon restart: task re-sent %d times, want 0", n)
	}
}

func TestHandleChannelReady_FreshOneShotStillGetsTaskOnce(t *testing.T) {
	s, fake := channelReadyServer(t, t.TempDir())
	sessID := seedOneShot(t, s, fake, "f00d", "❯ \n")

	postChannelReady(t, s, sessID)
	if n := waitTaskSends(fake, 1, 4*time.Second); n != 1 {
		t.Fatalf("fresh one-shot: task sent %d times, want exactly 1", n)
	}
}
