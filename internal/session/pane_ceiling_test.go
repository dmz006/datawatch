package session

import (
	"errors"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/resources"
)

// paneFake records the pane command handed to tmux new-session.
type paneFake struct {
	*FakeTmux
	paneCmd []string
	pid     int
}

func (p *paneFake) NewSessionWithPaneCommand(name string, cols, rows int, cmd []string) error {
	p.paneCmd = cmd
	return p.NewSessionWithSize(name, cols, rows)
}
func (p *paneFake) PanePID(string) int { return p.pid }

func stubHost(t *testing.T, scope, proc bool) {
	t.Helper()
	os, ps, ht, sc := scopeAvailableFn, procExistsFn, hostTotalMBFn, scopeCommandFn
	t.Cleanup(func() { scopeAvailableFn, procExistsFn, hostTotalMBFn, scopeCommandFn = os, ps, ht, sc })
	scopeAvailableFn = func() bool { return scope }
	procExistsFn = func() bool { return proc }
	hostTotalMBFn = func() (int, error) { return 8192, nil }
	scopeCommandFn = resources.ScopeCommand
}

func TestShellJoinQuotes(t *testing.T) {
	if got := shellJoin([]string{"a b", "it's"}); got != `'a b' 'it'\''s'` {
		t.Fatalf("got %s", got)
	}
	args := newSessionArgs("n", "80", "24", []string{"x"})
	if args[len(args)-1] != `'x'` || args[0] != "new-session" {
		t.Fatalf("args %v", args)
	}
	if a := newSessionArgs("n", "80", "24", nil); a[len(a)-1] != "24" {
		t.Fatalf("no pane command must add nothing: %v", a)
	}
}

func TestPlanPaneLaunch_NotWorkerIsUntouched(t *testing.T) {
	stubHost(t, true, true)
	m := &Manager{}
	if p := m.planPaneLaunch(&Session{}, resources.Override{}); p.Path != "" || p.Cmd != nil {
		t.Fatalf("operator session must not be wrapped: %+v", p)
	}
}

func TestPlanPaneLaunch_Scope(t *testing.T) {
	stubHost(t, true, true)
	scopeCommandFn = func(s resources.ScopeSpec) (string, []string, error) {
		return "/usr/bin/systemd-run", append([]string{"--user", "--scope", "-p", "MemoryMax=4096M", "--"}, s.Command...), nil
	}
	m := &Manager{}
	p := m.planPaneLaunch(&Session{FullID: "h-1", ScrubTmuxEnv: true}, resources.Override{})
	if p.Path != CeilingPathScope || p.CeilingMB != 4096 {
		t.Fatalf("plan %+v", p)
	}
	if p.Cmd[0] != "/usr/bin/systemd-run" || p.Cmd[1] != "--user" || p.Cmd[2] != "--scope" {
		t.Fatalf("cmd %v", p.Cmd)
	}
	for _, w := range p.Cmd {
		if w == "tmux" || w == "new-session" {
			t.Fatalf("tmux must stay outside the scope: %v", p.Cmd)
		}
	}
}

func TestPlanPaneLaunch_OverrideAndNone(t *testing.T) {
	stubHost(t, false, true)
	m := &Manager{}
	s := &Session{FullID: "h-1", ScrubTmuxEnv: true}
	if p := m.planPaneLaunch(s, resources.Override{Set: true, MB: 1000}); p.Path != CeilingPathWatchdog || p.CeilingMB != 1000 {
		t.Fatalf("plan %+v", p)
	}
	if p := m.planPaneLaunch(s, resources.Override{Set: true, None: true}); p.Path != CeilingPathNone {
		t.Fatalf("plan %+v", p)
	}
	stubHost(t, false, false)
	if p := m.planPaneLaunch(s, resources.Override{}); p.Path != CeilingPathNone {
		t.Fatalf("plan %+v", p)
	}
}

func TestPlanPaneLaunch_ScopeErrorFallsBackToWatchdog(t *testing.T) {
	stubHost(t, true, true)
	scopeCommandFn = func(resources.ScopeSpec) (string, []string, error) { return "", nil, errors.New("boom") }
	p := (&Manager{}).planPaneLaunch(&Session{FullID: "h-1", ScrubTmuxEnv: true}, resources.Override{})
	if p.Path != CeilingPathWatchdog || p.Cmd != nil {
		t.Fatalf("plan %+v", p)
	}
}

func TestStartWorkerPane_ScopeRecordsPath(t *testing.T) {
	stubHost(t, true, true)
	scopeCommandFn = func(s resources.ScopeSpec) (string, []string, error) {
		return "systemd-run", append([]string{"--user", "--scope", "--"}, s.Command...), nil
	}
	pf := &paneFake{FakeTmux: NewFakeTmux()}
	m := &Manager{tmux: pf}
	s := &Session{FullID: "h-1", TmuxSession: "cs-h-1", ScrubTmuxEnv: true}
	if err := m.startWorkerPane(s, 80, 24, resources.Override{}); err != nil {
		t.Fatal(err)
	}
	if s.CeilingPath != CeilingPathScope || s.CeilingMB != 4096 {
		t.Fatalf("recorded %q %d", s.CeilingPath, s.CeilingMB)
	}
	if !strings.Contains(strings.Join(pf.paneCmd, " "), "--scope --") {
		t.Fatalf("pane cmd %v", pf.paneCmd)
	}
}

func TestStartWorkerPane_WatchdogRecordsPath(t *testing.T) {
	stubHost(t, false, true)
	pf := &paneFake{FakeTmux: NewFakeTmux(), pid: 0}
	m := &Manager{tmux: pf}
	s := &Session{FullID: "h-1", TmuxSession: "cs-h-1", ScrubTmuxEnv: true}
	if err := m.startWorkerPane(s, 80, 24, resources.Override{}); err != nil {
		t.Fatal(err)
	}
	if s.CeilingPath != CeilingPathWatchdog || pf.paneCmd != nil {
		t.Fatalf("path %q cmd %v", s.CeilingPath, pf.paneCmd)
	}
	if !pf.SessionExists("cs-h-1") {
		t.Fatal("plain pane was not created")
	}
}

func TestStartWorkerPane_PlainSessionUnchanged(t *testing.T) {
	stubHost(t, true, true)
	ft := NewFakeTmux()
	m := &Manager{tmux: ft}
	s := &Session{FullID: "h-1", TmuxSession: "cs-h-1"}
	if err := m.startWorkerPane(s, 80, 24, resources.Override{}); err != nil {
		t.Fatal(err)
	}
	if s.CeilingPath != "" {
		t.Fatalf("operator session got path %q", s.CeilingPath)
	}
}
