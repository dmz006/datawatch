// A typed request leaves only "request <id> typed" in what datawatch keeps.

package session

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const reqMarker = "SECRET-RQT-41"

func reqTypedSession(t *testing.T, m *Manager, id, family string, state State) *Session {
	t.Helper()
	s := &Session{ID: id, FullID: "testhost-" + id, TmuxSession: "cs-testhost-" + id, State: state,
		BackendFamily: family, ProjectDir: t.TempDir(), UpdatedAt: time.Now()}
	if err := m.SaveSession(s); err != nil {
		t.Fatal(err)
	}
	tr, err := NewTracker(m.dataDir, s)
	if err != nil {
		t.Fatal(err)
	}
	m.mu.Lock()
	m.trackers[s.FullID] = tr
	m.mu.Unlock()
	return s
}

// reqTypedManager reports an agent in the pane's foreground.
func reqTypedManager(t *testing.T) (*Manager, *FakeTmux) {
	t.Helper()
	m, f := newTestManagerWithFake(t)
	m.verbose = true
	m.paneProcsFn = func(string) (int, string, []paneProc, bool) {
		return 10, "pts/1", []paneProc{
			{pid: 10, pgid: 10, tpgid: 20, tty: "pts/1", comm: "bash"},
			{pid: 20, ppid: 10, pgid: 20, tpgid: 20, tty: "pts/1", comm: "agent"},
		}, true
	}
	return m, f
}

func reqSentKeys(f *FakeTmux) (n int) {
	for _, op := range []string{"send-keys", "send-keys-settle", "send-keys-guarded", "send-keys-literal"} {
		n += f.Count(op)
	}
	return n
}

func reqFilesHolding(t *testing.T, dir, needle string) (out []string) {
	t.Helper()
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, rerr := os.ReadFile(path)
		if rerr == nil && strings.Contains(string(data), needle) {
			out = append(out, path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestReqTypedOnlyTheLineSaysItWasTyped(t *testing.T) {
	m, f := reqTypedManager(t)
	s := reqTypedSession(t, m, "aa01", "opencode", StateWaitingInput)
	var err error
	logged := daemonOutputDuring(t, func() {
		err = m.SendRequestText(s.FullID, "r-77", "datawatch request r-77: "+reqMarker)
	})
	if err != nil {
		t.Fatal(err)
	}
	if reqSentKeys(f) != 1 {
		t.Fatalf("calls: %+v", f.Calls)
	}
	if strings.Contains(logged, reqMarker) {
		t.Errorf("the daemon log holds the text:\n%s", logged)
	}
	if got := reqFilesHolding(t, m.dataDir, reqMarker); len(got) != 0 {
		t.Errorf("files holding the text: %v", got)
	}
	got, _ := m.GetSession(s.FullID)
	if got.LastInput != "request r-77 typed" {
		t.Errorf("the session record keeps %q", got.LastInput)
	}
	var lines int
	for _, path := range reqFilesHolding(t, m.dataDir, "request r-77 typed") {
		if base := filepath.Base(path); base == "conversation.md" || base == "timeline.md" {
			lines++
		}
	}
	if lines != 2 {
		t.Errorf("the tracker's conversation and timeline must say it was typed (%d of 2)", lines)
	}
}

func TestReqTypedNothingIsTypedWhereItCouldDoHarm(t *testing.T) {
	for name, edit := range map[string]func(m *Manager, f *FakeTmux, s *Session){
		"a permission prompt": func(_ *Manager, f *FakeTmux, s *Session) { f.Pane[s.TmuxSession] = approvalPane },
		"the shell reads the keyboard": func(m *Manager, _ *FakeTmux, _ *Session) {
			m.paneProcsFn = func(string) (int, string, []paneProc, bool) {
				return 10, "pts/1", []paneProc{{pid: 10, pgid: 10, tpgid: 10, tty: "pts/1", comm: "bash"}}, true
			}
		},
		"the processes cannot be read": func(m *Manager, _ *FakeTmux, _ *Session) {
			m.paneProcsFn = func(string) (int, string, []paneProc, bool) { return 0, "", nil, false }
		},
		"rate limited": func(m *Manager, _ *FakeTmux, s *Session) { s.State = StateRateLimited; _ = m.SaveSession(s) },
		"ended":        func(m *Manager, _ *FakeTmux, s *Session) { s.State = StateComplete; _ = m.SaveSession(s) },
		"chat mode":    func(m *Manager, _ *FakeTmux, s *Session) { s.OutputMode = "chat"; _ = m.SaveSession(s) },
		"an HTTP conversation backend": func(m *Manager, _ *FakeTmux, s *Session) {
			s.BackendFamily = "opencode-acp"
			_ = m.SaveSession(s)
		},
		"a shell session": func(m *Manager, _ *FakeTmux, s *Session) { s.BackendFamily = "shell"; _ = m.SaveSession(s) },
	} {
		m, f := reqTypedManager(t)
		s := reqTypedSession(t, m, "aa01", "opencode", StateWaitingInput)
		edit(m, f, s)
		if err := m.CanTypeRequest(s.FullID); !errors.Is(err, ErrNothingTyped) {
			t.Errorf("%s: readiness %v", name, err)
		}
		if err := m.SendRequestText(s.FullID, "r-1", reqMarker); !errors.Is(err, ErrNothingTyped) {
			t.Errorf("%s: %v", name, err)
		}
		if reqSentKeys(f) != 0 {
			t.Errorf("%s: keys went: %+v", name, f.Calls)
		}
	}
	m, _ := reqTypedManager(t)
	if err := m.SendRequestText("testhost-none", "r-1", "x"); !errors.Is(err, ErrNothingTyped) {
		t.Errorf("no session: %v", err)
	}
}

func TestReqTypedAFailureAfterTheKeysIsNotCalledNothing(t *testing.T) {
	m, f := reqTypedManager(t)
	s := reqTypedSession(t, m, "aa01", "opencode", StateWaitingInput)
	f.FailNextOp = map[string]int{"send-keys": 1, "send-keys-settle": 1, "send-keys-guarded": 1}
	err := m.SendRequestText(s.FullID, "r-1", reqMarker)
	if err == nil || errors.Is(err, ErrNothingTyped) {
		t.Fatalf("%v", err)
	}
}

func TestReqTypedNoticeIsAGuardedLine(t *testing.T) {
	m, f := reqTypedManager(t)
	s := reqTypedSession(t, m, "aa01", "claude-code", StateRunning)
	if err := m.SendRequestNotice(s.FullID, "datawatch: 3 request(s) wait for you."); err != nil {
		t.Fatal(err)
	}
	if reqSentKeys(f) != 1 {
		t.Fatalf("%+v", f.Calls)
	}
	if !IsAutomatedInputSource(RequestNoticeSource) || !IsAutomatedInputSource(RequestTypedSource) ||
		!heldToTypingGuard(RequestNoticeSource) || !heldToTypingGuard(RequestTypedSource) {
		t.Fatal("datawatch's own typing must be held to every guard")
	}
	f.Pane[s.TmuxSession] = approvalPane
	if err := m.SendRequestNotice(s.FullID, "x"); !errors.Is(err, ErrNothingTyped) || reqSentKeys(f) != 1 {
		t.Fatalf("a prompt: %v", err)
	}
}
