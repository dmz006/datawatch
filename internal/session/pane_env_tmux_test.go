package session

import (
	"context"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/llm"
	"github.com/dmz006/datawatch/internal/resources"
)

// Real-tmux tests for the environment of the worker process in a session's
// first pane. The production Manager and TmuxManager run unchanged; a "tmux"
// wrapper first on PATH pins every tmux call (the daemon's and the pane's) to
// a throwaway server: its own -S socket under t.TempDir(), no user config,
// TMUX and TMUX_TMPDIR unset. Nothing here can reach another tmux server.

const paneEnvTestSecret = "dw-test-secret-4c1f9a7e5b"

type paneEnvFixture struct {
	t       *testing.T
	m       *Manager
	sock    string
	repo    string
	out     string // where the worker script reports; outside the project
	script  string
	realBin string
	proj    string // the session's project folder
}

func newPaneEnvFixture(t *testing.T) *paneEnvFixture {
	t.Helper()
	realBin, err := exec.LookPath("tmux")
	if err != nil {
		t.Skip("tmux not found")
	}
	root, err := os.MkdirTemp("", "dwpe")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	f := &paneEnvFixture{t: t, sock: filepath.Join(root, "s"), out: filepath.Join(root, "out"), realBin: realBin}
	if len(f.sock) > 100 {
		t.Skipf("socket path too long for a unix socket: %s", f.sock)
	}
	binDir, home, guardTmp := filepath.Join(root, "bin"), filepath.Join(root, "home"), filepath.Join(root, "tmp")
	f.proj = filepath.Join(home, "workspace", "proj")
	for _, d := range []string{binDir, home, guardTmp, f.out, f.proj} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	f.repo = guardTestRepo(t)
	wrapper := "#!/bin/sh\nunset TMUX TMUX_TMPDIR\nexec " + shellSingleQuote(realBin) + " -S " + shellSingleQuote(f.sock) + " -f /dev/null \"$@\"\n"
	if err := os.WriteFile(filepath.Join(binDir, "tmux"), []byte(wrapper), 0o700); err != nil { // #nosec G306 -- test wrapper must be executable
		t.Fatal(err)
	}
	// The worker: reports its own environment, then tries to commit.
	f.script = filepath.Join(root, "worker.sh")
	script := `#!/bin/sh
out="$1"
env > "$out/env"
cd "$2" || exit 1
git commit -q --allow-empty -m worker > "$out/commit" 2>&1
echo $? > "$out/rc"
echo done > "$out/done"
`
	if err := os.WriteFile(f.script, []byte(script), 0o700); err != nil { // #nosec G306 -- test script
		t.Fatal(err)
	}
	// What the daemon and, through the tmux server, the pane inherit.
	for _, k := range []string{"TMUX", "TMUX_TMPDIR", "DATAWATCH_SESSION_ID", "DATAWATCH_BASE_URL",
		"DATAWATCH_MCP_CONFIG", "DATAWATCH_COMMIT_TOKEN", "GIT_CONFIG_COUNT", "GIT_CONFIG_KEY_0", "GIT_CONFIG_VALUE_0"} {
		t.Setenv(k, "")
		_ = os.Unsetenv(k)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("HOME", home)
	t.Setenv("SHELL", "/bin/sh")
	t.Setenv("TMPDIR", guardTmp)
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")

	m, err := NewManager("testhost", filepath.Join(root, "data"), "/bin/true", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{}
	cfg.Server.Port = 18443
	m.SetConfig(cfg)
	f.m = m
	// Runs after the private tmux server is gone (cleanups run last first).
	t.Cleanup(func() {
		if err := m.Close(); err != nil {
			t.Logf("close manager: %v", err)
		}
	})

	// A holder session keeps the private server up for the whole test.
	if out, err := f.tmux("new-session", "-d", "-s", "holder", "/bin/sh"); err != nil {
		t.Fatalf("start throwaway tmux: %v: %s", err, out)
	}
	t.Cleanup(func() {
		_, _ = f.tmux("kill-server") // the private -S server only
	})
	return f
}

func (f *paneEnvFixture) tmux(args ...string) (string, error) {
	cmd := exec.Command(f.realBin, append([]string{"-S", f.sock, "-f", os.DevNull}, args...)...)
	cmd.Env = cleanTmuxEnv()
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// launch types the worker command into the pane, the way every pane backend
// launches its program.
func (f *paneEnvFixture) launch(ctx context.Context, task, tmuxSession, projectDir, logFile string) error {
	return llm.SendKeys(ctx, tmuxSession, "sh "+shellSingleQuote(f.script)+" "+shellSingleQuote(f.out)+" "+shellSingleQuote(f.repo))
}

// workerReport waits for the worker and returns its environment, its git
// commit exit code and the commit output.
func (f *paneEnvFixture) workerReport(tmuxSession string) (map[string]string, string, string) {
	f.t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for {
		if _, err := os.Stat(filepath.Join(f.out, "done")); err == nil {
			break
		}
		if time.Now().After(deadline) {
			pane, _ := f.tmux("capture-pane", "-p", "-S", "-", "-t", tmuxSession)
			f.t.Fatalf("worker did not finish; pane:\n%s", pane)
		}
		time.Sleep(50 * time.Millisecond)
	}
	read := func(name string) string {
		b, err := os.ReadFile(filepath.Join(f.out, name))
		if err != nil {
			f.t.Fatal(err)
		}
		return string(b)
	}
	env := map[string]string{}
	for _, line := range strings.Split(read("env"), "\n") {
		if k, v, ok := strings.Cut(line, "="); ok {
			env[k] = v
		}
	}
	rc, commit := strings.TrimSpace(read("rc")), read("commit")
	if err := os.Remove(filepath.Join(f.out, "done")); err != nil {
		f.t.Fatal(err)
	}
	return env, rc, commit
}

// assertNoSecretVisible checks the pane scrollback, the session's logs and
// the project folder for the value of a profile secret.
func (f *paneEnvFixture) assertNoSecretVisible(sess *Session) {
	f.t.Helper()
	pane, err := f.tmux("capture-pane", "-p", "-J", "-S", "-", "-t", sess.TmuxSession)
	if err != nil {
		f.t.Fatalf("capture pane: %v: %s", err, pane)
	}
	if strings.Contains(pane, paneEnvTestSecret) {
		f.t.Errorf("secret value is visible in the pane scrollback:\n%s", pane)
	}
	for _, dir := range []string{sess.ProjectDir, f.repo, f.m.dataDir} {
		_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, werr error) error {
			if werr != nil || !d.Type().IsRegular() {
				return nil
			}
			if b, rerr := os.ReadFile(p); rerr == nil && strings.Contains(string(b), paneEnvTestSecret) {
				f.t.Errorf("secret value was written to %s", p)
			}
			return nil
		})
	}
}

func assertWorkerEnv(t *testing.T, env map[string]string, rc, commit string, sess *Session) {
	t.Helper()
	if got := env["DATAWATCH_SESSION_ID"]; got != sess.FullID {
		t.Errorf("worker DATAWATCH_SESSION_ID = %q, want %q", got, sess.FullID)
	}
	if got := env[PaneSessionMarkerEnv]; !sess.Subprocess && got != PaneSessionMarker(sess.FullID) {
		t.Errorf("worker %s = %q; without it the session-end reap would end every worker process", PaneSessionMarkerEnv, got)
	}
	if got := env["DATAWATCH_BASE_URL"]; got != "http://127.0.0.1:18443" {
		t.Errorf("worker DATAWATCH_BASE_URL = %q", got)
	}
	if got := env["GIT_CONFIG_KEY_0"]; got != "core.hooksPath" {
		t.Errorf("worker GIT_CONFIG_KEY_0 = %q, want core.hooksPath", got)
	}
	if rc == "0" {
		t.Errorf("worker git commit succeeded; the commit guard did not reach the worker")
	} else if !strings.Contains(commit, "the harness commits after validation") {
		t.Errorf("worker git commit failed, but not because of the guard: %s", commit)
	}
}

func TestPaneEnvReachesWorkerAndGuardRefusesCommit(t *testing.T) {
	for _, tc := range []struct {
		name   string
		scrub  bool // worker pane: TMUX scrub and tmux kill guard typed too
		scoped bool // the pane runs a wrapper command, not the default shell
	}{{"plain pane", false, false}, {"worker pane", true, false}, {"wrapped pane command", true, true}} {
		t.Run(tc.name, func(t *testing.T) {
			stubPaneCeiling(t, tc.scoped)
			f := newPaneEnvFixture(t)
			// Stands in for the daemon's per-session MCP file hook, which
			// also runs after the pane exists.
			f.m.SetOnPreLaunch(func(s *Session) {
				if err := f.m.SetPaneEnv(s.TmuxSession, map[string]string{"DATAWATCH_MCP_CONFIG": "/private/mcp.json"}); err != nil {
					t.Errorf("set MCP config path: %v", err)
				}
			})
			// A value with quotes, $, backticks and spaces must arrive intact
			// and must not be run.
			odd := paneEnvTestSecret + ` "q" 's' $HOME ` + "`false`" + ` \end; $(false)`
			sess, err := f.m.Start(context.Background(), "do the work", "", f.proj, &StartOptions{
				Backend: "pane-env-test", LaunchFn: f.launch, PRDID: "prd1", ScrubTmuxEnv: tc.scrub,
				Env: map[string]string{"DW_PROFILE_TOKEN": odd},
			})
			if err != nil {
				t.Fatalf("start: %v", err)
			}
			env, rc, commit := f.workerReport(sess.TmuxSession)
			assertWorkerEnv(t, env, rc, commit, sess)
			if got := env["DATAWATCH_MCP_CONFIG"]; got != "/private/mcp.json" {
				t.Errorf("worker DATAWATCH_MCP_CONFIG = %q", got)
			}
			if tc.scoped && env["DW_PANE_WRAPPED"] != "1" {
				t.Errorf("pane did not run the wrapper command")
			}
			if tc.scrub && (env["TMUX"] != "" || env["TMUX_TMPDIR"] != "") {
				t.Errorf("worker still has TMUX=%q TMUX_TMPDIR=%q", env["TMUX"], env["TMUX_TMPDIR"])
			}
			if env["DW_PROFILE_TOKEN"] != odd {
				t.Errorf("worker profile variable did not arrive intact (got %d bytes, want %d)", len(env["DW_PROFILE_TOKEN"]), len(odd))
			}
			f.assertNoSecretVisible(sess)

			// A restart builds a new first pane; it needs the same values.
			stored, ok := f.m.GetSession(sess.FullID)
			if !ok {
				t.Fatal("session not stored")
			}
			done := *stored
			done.State = StateComplete
			// A session on a named profile (the rate-limit fallback records
			// one) gets that profile's environment again.
			done.Profile = "fallback"
			f.m.cfg.Profiles = map[string]config.ProfileConfig{"fallback": {Env: map[string]string{
				"DW_PROFILE_TOKEN": odd, "DATAWATCH_SESSION_ID": "not-this-session",
			}}}
			if err := f.m.SaveSession(&done); err != nil {
				t.Fatal(err)
			}
			f.m.SetLLMBackend("pane-env-test", f.launch)
			again, err := f.m.Restart(context.Background(), sess.FullID)
			if err != nil {
				t.Fatalf("restart: %v", err)
			}
			env, rc, commit = f.workerReport(again.TmuxSession)
			assertWorkerEnv(t, env, rc, commit, again)
			if env["DW_PROFILE_TOKEN"] != odd {
				t.Errorf("restarted worker lacks the profile variable (got %d bytes)", len(env["DW_PROFILE_TOKEN"]))
			}
			f.assertNoSecretVisible(again)
		})
	}
}

// stubPaneCeiling keeps the test from creating a real systemd scope. With
// wrapped, the pane runs a wrapper command the way the scope launcher does.
func stubPaneCeiling(t *testing.T, wrapped bool) {
	t.Helper()
	oldAvail, oldCmd := scopeAvailableFn, scopeCommandFn
	t.Cleanup(func() { scopeAvailableFn, scopeCommandFn = oldAvail, oldCmd })
	scopeAvailableFn = func() bool { return wrapped }
	scopeCommandFn = func(spec resources.ScopeSpec) (string, []string, error) {
		return "env", append([]string{"DW_PANE_WRAPPED=1"}, spec.Command...), nil
	}
}

func TestPaneEnvUnguardedSessionCanStillCommit(t *testing.T) {
	f := newPaneEnvFixture(t)
	sess, err := f.m.Start(context.Background(), "operator work", "", f.proj, &StartOptions{
		Backend: "pane-env-test", LaunchFn: f.launch,
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	env, rc, commit := f.workerReport(sess.TmuxSession)
	if env["DATAWATCH_SESSION_ID"] != sess.FullID {
		t.Errorf("DATAWATCH_SESSION_ID = %q, want %q", env["DATAWATCH_SESSION_ID"], sess.FullID)
	}
	if env["GIT_CONFIG_COUNT"] != "" || rc != "0" {
		t.Errorf("unguarded session is guarded: GIT_CONFIG_COUNT=%q rc=%s %s", env["GIT_CONFIG_COUNT"], rc, commit)
	}
}

func TestSubprocessSessionGetsProfileEnvAndGuard(t *testing.T) {
	f := newPaneEnvFixture(t)
	f.m.SetSubprocessBaseURL("http://127.0.0.1:18443")
	task := "sh " + shellSingleQuote(f.script) + " " + shellSingleQuote(f.out) + " " + shellSingleQuote(f.repo)
	sess, err := f.m.Start(context.Background(), task, "", f.proj, &StartOptions{
		Backend: "pane-env-test", Subprocess: true, PRDID: "prd1",
		Env: map[string]string{"DW_PROFILE_TOKEN": paneEnvTestSecret, "DATAWATCH_SESSION_ID": "not-this-session"},
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	env, rc, commit := f.workerReport("")
	assertWorkerEnv(t, env, rc, commit, sess)
	if env["DW_PROFILE_TOKEN"] != paneEnvTestSecret {
		t.Errorf("subprocess worker lacks the profile variable")
	}
	// Let the runner finish before the fixture is removed.
	for i := 0; i < 200; i++ {
		if s, ok := f.m.GetSession(sess.FullID); ok && s.State != StateRunning {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
}
