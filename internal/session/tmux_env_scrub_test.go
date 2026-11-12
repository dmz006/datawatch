package session

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Real-tmux tests for the pane TMUX/TMUX_TMPDIR scrub. Every tmux command
// runs against a throwaway server (explicit -S socket under t.TempDir()) with
// TMUX and TMUX_TMPDIR stripped from the command environment, so nothing here
// can reach the production socket.

// cleanTmuxEnv returns the process environment without TMUX/TMUX_TMPDIR,
// plus any extra entries.
func cleanTmuxEnv(extra ...string) []string {
	var env []string
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "TMUX=") || strings.HasPrefix(kv, "TMUX_TMPDIR=") {
			continue
		}
		env = append(env, kv)
	}
	return append(env, extra...)
}

// throwawayTmux is a tmux server bound to its own temp socket. It implements
// the slice of TmuxAPI the scrub path uses; the embedded nil interface makes
// any other call fail loudly.
type throwawayTmux struct {
	TmuxAPI
	t    *testing.T
	sock string
}

func newThrowawayTmux(t *testing.T, startEnv ...string) *throwawayTmux {
	t.Helper()
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not found")
	}
	dir := t.TempDir()
	sock := filepath.Join(dir, "sock")
	if len(sock) > 100 {
		t.Skipf("socket path too long for a unix socket: %s", sock)
	}
	tt := &throwawayTmux{t: t, sock: sock}
	// The first command starts the server; startEnv is that server's
	// environment on top of the cleaned one.
	cmd := exec.Command("tmux", "-S", sock, "new-session", "-d", "-s", "w", "-x", "120", "-y", "30", "/bin/sh")
	cmd.Env = cleanTmuxEnv(startEnv...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("start throwaway tmux: %v: %s", err, out)
	}
	t.Cleanup(func() {
		kill := exec.Command("tmux", "-S", sock, "kill-server")
		kill.Env = cleanTmuxEnv()
		_ = kill.Run()
	})
	return tt
}

func (tt *throwawayTmux) run(args ...string) error {
	cmd := exec.Command("tmux", append([]string{"-S", tt.sock}, args...)...)
	cmd.Env = cleanTmuxEnv()
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("tmux %v: %w: %s", args, err, out)
	}
	return nil
}

func (tt *throwawayTmux) alive() bool { return tt.run("has-session", "-t", "w") == nil }

func (tt *throwawayTmux) SendKeys(session, keys string) error {
	return tt.run("send-keys", "-t", session, keys, "Enter")
}

func waitForFile(t *testing.T, path string) string {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if b, err := os.ReadFile(path); err == nil && len(b) > 0 {
			return string(b)
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", path)
	return ""
}

func TestTmuxEnvScrub_WorkerLaunchPathHasNoTmuxEnv(t *testing.T) {
	beforeTmux, beforeTmpdir := os.Getenv("TMUX"), os.Getenv("TMUX_TMPDIR")

	dummy := t.TempDir()
	tt := newThrowawayTmux(t, "TMUX="+dummy+"/dummy,1,0", "TMUX_TMPDIR="+dummy)
	m := &Manager{tmux: tt}
	out := t.TempDir()

	// Sanity: before the scrub the pane really has TMUX set, so the
	// post-scrub assertion cannot pass vacuously.
	pre := filepath.Join(out, "pre.txt")
	if err := tt.SendKeys("w", "env > "+pre+".tmp && mv "+pre+".tmp "+pre); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(waitForFile(t, pre), "TMUX=") {
		t.Fatal("pane has no TMUX before the scrub; test would be vacuous")
	}

	m.scrubPaneTmuxEnv(&Session{ID: "scrub1", TmuxSession: "w", ScrubTmuxEnv: true})

	post := filepath.Join(out, "post.txt")
	if err := tt.SendKeys("w", "env > "+post+".tmp && mv "+post+".tmp "+post); err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(waitForFile(t, post), "\n") {
		if strings.HasPrefix(line, "TMUX=") || strings.HasPrefix(line, "TMUX_TMPDIR=") {
			t.Fatalf("pane env still has %q after scrub", line)
		}
	}

	// The daemon side is untouched.
	if os.Getenv("TMUX") != beforeTmux || os.Getenv("TMUX_TMPDIR") != beforeTmpdir {
		t.Fatal("scrub modified the test process environment")
	}
}

func TestTmuxEnvScrub_KillServerSparesSecondServer(t *testing.T) {
	a := newThrowawayTmux(t)
	b := newThrowawayTmux(t)
	c := newThrowawayTmux(t) // control: unscrubbed pane
	third := t.TempDir()     // empty TMUX_TMPDIR: no server lives here

	killFromPane := func(tt *throwawayTmux) error {
		return tt.SendKeys("w", "TMUX_TMPDIR="+third+" tmux kill-server")
	}

	// Control: without the scrub, TMUX wins over TMUX_TMPDIR and the
	// pane's own server dies. This is the incident shape.
	if err := killFromPane(c); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for c.alive() && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	if c.alive() {
		t.Fatal("control: kill-server from an unscrubbed pane did not reach its server")
	}

	// Scrubbed pane on A: the same command must not hit A or B.
	m := &Manager{tmux: a}
	m.scrubPaneTmuxEnv(&Session{ID: "scrub2", TmuxSession: "w", ScrubTmuxEnv: true})
	marker := filepath.Join(t.TempDir(), "done")
	if err := a.SendKeys("w", "TMUX_TMPDIR="+third+" tmux kill-server; echo $? > "+marker); err != nil {
		t.Fatal(err)
	}
	// The marker is written after kill-server returns, so by now any kill
	// that was going to reach A has already been delivered. Absence of an
	// event cannot be polled for, so additionally observe A staying alive
	// over a short window (a late kill would flip alive() to false).
	waitForFile(t, marker)
	for end := time.Now().Add(200 * time.Millisecond); time.Now().Before(end); time.Sleep(20 * time.Millisecond) {
		if !a.alive() {
			t.Fatal("server A was killed by a scrubbed pane")
		}
	}

	if !b.alive() {
		t.Fatal("server B was killed by a scrubbed pane on server A")
	}
}
