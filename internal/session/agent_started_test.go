package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// newAgentStartedSession saves a running one-shot claude-code session whose
// pane is "cs-a6e1" and whose recorded output lives under the test's temp dir.
func newAgentStartedSession(t *testing.T, mgr *Manager) *Session {
	t.Helper()
	sess := &Session{
		ID: "a6e1", FullID: "testhost-a6e1", Hostname: "testhost",
		TmuxSession: "cs-a6e1", BackendFamily: "claude-code",
		OneShot: true, State: StateRunning, Task: "count the files",
		LogFile:   filepath.Join(t.TempDir(), "a6e1.log"),
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	if err := mgr.SaveSession(sess); err != nil {
		t.Fatal(err)
	}
	return sess
}

// writeAgentLog replaces the session's recorded output with content.
func writeAgentLog(t *testing.T, sess *Session, content string) {
	t.Helper()
	if err := os.WriteFile(sess.LogFile, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestAgentStarted_BareShellNoOutput(t *testing.T) {
	mgr, fake := newTestManagerWithFake(t)
	sess := newAgentStartedSession(t, mgr)
	fake.PaneCmd[sess.TmuxSession] = "bash"

	if mgr.AgentStarted(sess.FullID) {
		t.Error("bare shell with no recorded output reported as started")
	}
}

func TestAgentStarted_AgentProcessRunning(t *testing.T) {
	mgr, fake := newTestManagerWithFake(t)
	sess := newAgentStartedSession(t, mgr)
	fake.PaneCmd[sess.TmuxSession] = "claude"

	if !mgr.AgentStarted(sess.FullID) {
		t.Error("agent process running in the pane reported as not started")
	}
}

func TestAgentStarted_FirstAssistantTurn(t *testing.T) {
	mgr, fake := newTestManagerWithFake(t)
	sess := newAgentStartedSession(t, mgr)
	fake.PaneCmd[sess.TmuxSession] = "bash"
	writeAgentLog(t, sess, "Welcome banner\n❯ count the files\n\x1b[1m• Listing the directory\x1b[0m\n")

	if !mgr.AgentStarted(sess.FullID) {
		t.Error("recorded first assistant turn reported as not started")
	}
}

func TestAgentStarted_PromptWithoutAssistantTurn(t *testing.T) {
	mgr, fake := newTestManagerWithFake(t)
	sess := newAgentStartedSession(t, mgr)
	fake.PaneCmd[sess.TmuxSession] = "bash"
	// A bullet before the prompt is startup chrome, not an assistant turn.
	writeAgentLog(t, sess, "• Tip: run /init\n❯ \n")

	if mgr.AgentStarted(sess.FullID) {
		t.Error("prompt with no assistant turn after it reported as started")
	}
}

func TestAgentStarted_ChannelReady(t *testing.T) {
	mgr, fake := newTestManagerWithFake(t)
	sess := newAgentStartedSession(t, mgr)
	fake.PaneCmd[sess.TmuxSession] = "bash"
	sess.ChannelReady = true
	if err := mgr.SaveSession(sess); err != nil {
		t.Fatal(err)
	}

	if !mgr.AgentStarted(sess.FullID) {
		t.Error("channel-ready session reported as not started")
	}
}

func TestAgentStarted_TaskDelivered(t *testing.T) {
	mgr, fake := newTestManagerWithFake(t)
	sess := newAgentStartedSession(t, mgr)
	fake.PaneCmd[sess.TmuxSession] = "bash"
	if !mgr.ClaimTaskDelivery(sess.FullID) {
		t.Fatal("claim should succeed on a fresh session")
	}

	if !mgr.AgentStarted(sess.FullID) {
		t.Error("session with a delivered task reported as not started")
	}
}

func TestAgentStarted_MissingSession(t *testing.T) {
	mgr, _ := newTestManagerWithFake(t)

	if mgr.AgentStarted("testhost-ffff") {
		t.Error("unknown session reported as started")
	}
}

func TestAgentStarted_VirtualSessionWithoutRecord(t *testing.T) {
	mgr, _ := newTestManagerWithFake(t)
	sess := newAgentStartedSession(t, mgr)
	sess.TmuxSession = "" // virtual session: no pane to probe
	if err := mgr.SaveSession(sess); err != nil {
		t.Fatal(err)
	}

	if mgr.AgentStarted(sess.FullID) {
		t.Error("virtual session with no record reported as started")
	}
}

func TestAgentStarted_CommsRunAgent(t *testing.T) {
	cases := []struct {
		name  string
		comms string
		want  bool
	}{
		{"shell only", "bash\n", false},
		{"login shell only", "-bash\n", false},
		{"empty", "", false},
		{"shell wrapper with agent child", "bash\nnode\n", true},
		{"agent alone", "claude\n", true},
	}
	for _, tc := range cases {
		if got := commsRunAgent(tc.comms); got != tc.want {
			t.Errorf("%s: commsRunAgent(%q) = %v, want %v", tc.name, tc.comms, got, tc.want)
		}
	}
}

func TestAgentStarted_OutputShowsAssistantTurn(t *testing.T) {
	cases := []struct {
		name string
		out  string
		want bool
	}{
		{"no prompt", "• bullet with no prompt\n", false},
		{"prompt then bullet", "❯ task\n• reply\n", true},
		{"prompt then tool marker", "❯ task\n⏺ Bash(ls)\n", true},
		{"bullet on prompt line", "❯ • not after\n", false},
		{"prompt then bullet without newline", "❯ task\n• reply", true},
	}
	for _, tc := range cases {
		if got := outputShowsAssistantTurn(strings.NewReader(tc.out)); got != tc.want {
			t.Errorf("%s: outputShowsAssistantTurn = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// A task records the short session id. AgentStarted must resolve it the way
// GetSession does, or a live worker reads as a bare shell at boot.
func TestAgentStarted_ShortIDResolves(t *testing.T) {
	mgr, fake := newTestManagerWithFake(t)
	sess := newAgentStartedSession(t, mgr)
	fake.PaneCmd[sess.TmuxSession] = "claude"

	if sess.ID == "" || sess.ID == sess.FullID {
		t.Fatalf("test needs a distinct short id; got id=%q full=%q", sess.ID, sess.FullID)
	}
	if !mgr.AgentStarted(sess.ID) {
		t.Error("agent running under the short session id reported as not started")
	}
}
