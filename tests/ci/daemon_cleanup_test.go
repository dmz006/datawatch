package ci

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// daemons tests drive the daemon lifecycle helpers through a bash driver and
// the two guard scripts. Every fake daemon started here is a bare `sleep` or
// `python3` process whose argv[0] is renamed to read like a foreground
// daemon, so nothing that is not ours can be matched on the way out: only
// the PIDs these tests started are ever signalled, and only while their
// live /proc/<pid>/cmdline still carries the config marker. Nothing here
// names a tracker, a release version, or a multiplexer server; daemons are
// ended only by their recorded PID.
//
// All waits are bounded: a poll loop with a deadline instead of an open-end
// sleep, and every child command runs under a hard context timeout.

// dcRepoRoot resolves the repository root from the test working directory
// (tests/ci for this package).
func dcRepoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}
	return root
}

// dcBash locates bash or skips: every subtest drives the daemon library and
// the guard scripts through bash.
func dcBash(t *testing.T) string {
	t.Helper()
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash is not on PATH; daemon cleanup shell helpers cannot run")
	}
	return bash
}

// dcEnv returns the process environment with every multiplexer variable
// dropped (the helpers must not touch a server), plus the given pairs.
func dcEnv(extra ...string) []string {
	var env []string
	for _, kv := range os.Environ() {
		i := strings.IndexByte(kv, '=')
		if i <= 0 {
			continue
		}
		if kv[:i] == "TMUX" || kv[:i] == "TMUX_TMPDIR" {
			continue
		}
		env = append(env, kv)
	}
	return append(env, extra...)
}

func dcPIDAlive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

// dcWaitGone polls until the PID is gone or the deadline passes. A PID can
// be handed to a new process before the first poll (the host churns many
// short-lived processes), so the marker distinguishes "our daemon is gone"
// from "the PID was reused": if the command line no longer carries the
// marker the original daemon has ended either way.
func dcWaitGone(t *testing.T, pid int, marker string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		if !dcPIDAlive(pid) || !strings.Contains(dcCmdline(pid), marker) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("pid %d (command line still carries %q) is still alive after %s", pid, marker, timeout)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// dcRequireNotFlagged expresses the "check must pass" contract on a shared
// host: the check may be red because an unrelated session started its own
// test daemon between our snapshot and the check (the guard then rightly
// flags it); the contract under test is that the daemon we manage is not
// among the offenders. On a clean host the check exits 0, and the OK line
// is required in either case.
func dcRequireNotFlagged(t *testing.T, out string, code int, markers ...string) {
	t.Helper()
	if code == 0 {
		if !strings.Contains(out, "OK: no leftover test daemons") {
			t.Fatalf("expected an OK line:\n%s", out)
		}
		return
	}
	if code != 1 {
		t.Fatalf("unexpected check status %d:\n%s", code, out)
	}
	for _, m := range markers {
		if strings.Contains(out, m) {
			t.Fatalf("check flagged the daemon this test manages (%q):\n%s", m, out)
		}
	}
	t.Logf("check flags only unrelated host daemons; tolerating:\n%s", out)
}

// dcWaitAlive polls until the PID is up or the deadline passes.
func dcWaitAlive(t *testing.T, pid int, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for !dcPIDAlive(pid) {
		if time.Now().After(deadline) {
			t.Fatalf("pid %d never came up within %s", pid, timeout)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func dcCmdline(pid int) string {
	b, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid))
	if err != nil {
		return ""
	}
	return strings.ReplaceAll(string(b), "\x00", " ")
}

// dcRun runs one short command with a hard timeout (bounded wait, no open
// wait) and returns its output and exit code.
func dcRun(t *testing.T, dir string, env []string, args ...string) (string, int) {
	t.Helper()
	if len(args) == 0 {
		t.Fatal("dcRun: no command")
	}
	// The repository-wide audit takes about half a minute on a developer machine and more than a
	// minute on a loaded CI runner, so the bound is generous; it still stops a hung command.
	ctx, cancel := context.WithTimeout(context.Background(), 240*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Dir = dir
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	code := 0
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			code = ee.ExitCode()
		} else {
			t.Fatalf("run %v: %v\n%s", args, err, out)
		}
	}
	return string(out), code
}

// dcFakeDaemon starts a process whose command line reads like a foreground
// daemon with the given --config path and returns its PID. The PID is
// recorded for the test; only it may be signalled, and only while its
// command line still carries the marker.
func dcFakeDaemon(t *testing.T, bash, cfgPath string) int {
	t.Helper()
	cmd := exec.Command(bash, "-c",
		fmt.Sprintf("exec -a 'datawatch start --foreground --config %s' sleep 300", cfgPath))
	cmd.Env = dcEnv()
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		t.Fatalf("start fake daemon with config %s: %v", cfgPath, err)
	}
	pid := cmd.Process.Pid
	dcWaitAlive(t, pid, 5*time.Second)
	marker := cfgPath
	t.Cleanup(func() {
		if dcPIDAlive(pid) {
			if strings.Contains(dcCmdline(pid), marker) {
				_ = syscall.Kill(pid, syscall.SIGKILL)
			} else {
				t.Logf("pid %d no longer carries %q; not signalling it", pid, marker)
			}
		}
		_ = cmd.Wait() // reap if it is still our child
	})
	return pid
}

// TestDaemonCleanupHelper starts a fake daemon through a bash driver that
// sources the shared daemon library, records one daemon PID and leaves one
// identical twin unrecorded, then ends the driver on every exit path:
// normal exit, a failure exit, and the three interrupts. In every case the
// recorded daemon must be gone while the twin lives on (no name search),
// and the driver must keep its own status.
func TestDaemonCleanupHelper(t *testing.T) {
	bash := dcBash(t)
	root := dcRepoRoot(t)
	lib := filepath.Join(root, "scripts", "test-daemon-lib.sh")
	if _, err := os.Stat(lib); err != nil {
		t.Fatalf("daemon library missing at %s: %v", lib, err)
	}

	const driver = `#!/usr/bin/env bash
# fixture driver: one fake daemon is tracked through the shared daemon
# library; one identical twin is deliberately never recorded; the shared
# traps give EVERY exit path (normal, failure, INT, TERM, HUP) a chance to
# end only the recorded PID. Nothing here looks anything up by name.
set -u
FIX="${DW_FIX:?DW_FIX required}"
MODE="${DW_MODE:?DW_MODE required}"
export TD_STATE_DIR="$FIX/state"
mkdir -p "$TD_STATE_DIR"
CFG="$FIX/config.yaml"
source "$DW_LIB"
td_install_traps
# Both fake daemons detach their stdio: the twin is meant to OUTLIVE the
# driver, and if it inherited the driver's stdout/stderr pipes Go's Wait
# would block on a pipe the twin keeps open for hundreds of seconds.
bash -c "exec -a 'datawatch start --foreground --config $CFG' sleep 300" </dev/null >/dev/null 2>&1 &
DPID=$!
td_track_pid "$DPID" "$CFG"
bash -c "exec -a 'datawatch start --foreground --config $CFG' sleep 300" </dev/null >/dev/null 2>&1 &
TWIN=$!
echo "DAEMON_PID=$DPID"
echo "TWIN_PID=$TWIN"
echo "READY"
case "$MODE" in
  normal) exit 0 ;;
  fail)   exit 3 ;;
  wait)   wait "$DPID" ;;
esac
`

	type modeCase struct {
		name   string
		mode   string
		signal syscall.Signal
		want   int
	}
	cases := []modeCase{
		{name: "NormalExit", mode: "normal", want: 0},
		{name: "FailureExitKeepsStatus", mode: "fail", want: 3},
		{name: "InterruptSigint", mode: "wait", signal: syscall.SIGINT, want: 130},
		{name: "InterruptSigterm", mode: "wait", signal: syscall.SIGTERM, want: 143},
		{name: "InterruptSighup", mode: "wait", signal: syscall.SIGHUP, want: 129},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			fix := t.TempDir()
			if err := os.WriteFile(filepath.Join(fix, "config.yaml"), []byte("fixture: true\n"), 0o644); err != nil {
				t.Fatalf("write fixture config: %v", err)
			}
			driverPath := filepath.Join(fix, "driver.sh")
			if err := os.WriteFile(driverPath, []byte(driver), 0o755); err != nil {
				t.Fatalf("write driver: %v", err)
			}

			cmd := exec.Command(bash, driverPath)
			cmd.Dir = root
			cmd.Env = dcEnv(
				"DW_FIX="+fix,
				"DW_MODE="+tc.mode,
				"DW_LIB="+lib,
			)
			stdout, err := cmd.StdoutPipe()
			if err != nil {
				t.Fatalf("stdout pipe: %v", err)
			}
			cmd.Stderr = io.Discard
			if err := cmd.Start(); err != nil {
				t.Fatalf("start driver: %v", err)
			}

			// Bounded read of the three lines the driver reports before it
			// takes its exit path.
			type threeLines struct {
				lines []string
				err   error
			}
			lineCh := make(chan threeLines, 1)
			go func() {
				sc := bufio.NewScanner(stdout)
				var lines []string
				for len(lines) < 3 {
					if !sc.Scan() {
						lineCh <- threeLines{lines, fmt.Errorf("driver ended early after %q (mode %s)", lines, tc.mode)}
						return
					}
					lines = append(lines, sc.Text())
				}
				lineCh <- threeLines{lines, nil}
			}()
			var got []string
			select {
			case tr := <-lineCh:
				if tr.err != nil {
					_ = cmd.Process.Kill()
					t.Fatalf("%v", tr.err)
				}
				got = tr.lines
			case <-time.After(15 * time.Second):
				_ = cmd.Process.Kill()
				t.Fatalf("driver did not report its two fake daemon PIDs within 15s (mode %s)", tc.mode)
			}
			daemonPid, err1 := strconv.Atoi(strings.TrimPrefix(got[0], "DAEMON_PID="))
			twinPid, err2 := strconv.Atoi(strings.TrimPrefix(got[1], "TWIN_PID="))
			if err1 != nil || err2 != nil || got[2] != "READY" {
				_ = cmd.Process.Kill()
				t.Fatalf("unexpected driver preamble %q (mode %s)", got, tc.mode)
			}
			if daemonPid <= 0 || twinPid <= 0 {
				_ = cmd.Process.Kill()
				t.Fatalf("driver reported a bad PID pair %d/%d (mode %s)", daemonPid, twinPid, tc.mode)
			}

			// Only the PIDs this test recorded are ever signalled, and only
			// while their command line still carries the config marker.
			marker := filepath.Join(fix, "config.yaml")
			recorded := []int{daemonPid, twinPid}
			t.Cleanup(func() {
				for _, p := range recorded {
					if dcPIDAlive(p) && strings.Contains(dcCmdline(p), marker) {
						_ = syscall.Kill(p, syscall.SIGKILL)
					}
				}
			})

			if tc.signal != 0 {
				// Let the daemon settle and the driver reach its wait loop
				// before the interrupt lands.
				time.Sleep(300 * time.Millisecond)
				if err := cmd.Process.Signal(tc.signal); err != nil {
					t.Fatalf("send %s: %v", tc.signal, err)
				}
			}

			// Bounded wait for the driver to finish.
			doneCh := make(chan error, 1)
			go func() { doneCh <- cmd.Wait() }()
			var waitErr error
			select {
			case waitErr = <-doneCh:
			case <-time.After(30 * time.Second):
				_ = cmd.Process.Kill()
				select {
				case <-doneCh:
				case <-time.After(10 * time.Second):
				}
				t.Fatalf("driver did not end within 30s on mode %s", tc.mode)
			}
			code := 0
			if waitErr != nil {
				var ee *exec.ExitError
				if errors.As(waitErr, &ee) {
					code = ee.ExitCode()
				} else {
					t.Fatalf("wait on driver: %v (mode %s)", waitErr, tc.mode)
				}
			}
			if code != tc.want {
				t.Fatalf("driver exit status: want %d, got %d (mode %s)", tc.want, code, tc.mode)
			}

			dcWaitGone(t, daemonPid, marker, 10*time.Second)
			if !dcPIDAlive(twinPid) {
				t.Fatalf("unrecorded twin %d was ended too: cleanup must not end processes by name", twinPid)
			}
		})
	}
}

// TestLeftoverDaemonGuard exercises the read-only leftover daemon guard:
// a fake test daemon (a temp-folder config) must fail `check` and name its
// PID, the same daemon must pass once it is in the snapshot, and it must
// pass again after it is ended. A daemon whose config lives outside every
// temporary location must never be flagged.
func TestLeftoverDaemonGuard(t *testing.T) {
	bash := dcBash(t)
	root := dcRepoRoot(t)
	guard := filepath.Join(root, "scripts", "check-no-leftover-test-daemons.sh")
	if _, err := os.Stat(guard); err != nil {
		t.Fatalf("guard missing at %s: %v", guard, err)
	}
	fix := t.TempDir()
	cfg := filepath.Join(fix, "config.yaml")
	if err := os.WriteFile(cfg, []byte("fixture: true\n"), 0o644); err != nil {
		t.Fatalf("write fixture config: %v", err)
	}
	env := dcEnv("TMPDIR=" + fix)
	snap := filepath.Join(fix, "baseline.txt")

	// A snapshot at the start of the run is clean.
	out, code := dcRun(t, root, env, bash, guard, "snapshot", snap)
	if code != 0 {
		t.Fatalf("snapshot: want exit 0, got %d\n%s", code, out)
	}

	// A fake test daemon now lives.
	pid := dcFakeDaemon(t, bash, cfg)

	// `check` must fail and name the PID.
	out, code = dcRun(t, root, env, bash, guard, "check", snap)
	if code != 1 {
		t.Fatalf("check with a live fake daemon: want exit 1, got %d\n%s", code, out)
	}
	if !strings.Contains(out, "leftover test daemon") || !strings.Contains(out, "pid="+strconv.Itoa(pid)+" ") {
		t.Fatalf("check output must name pid %d:\n%s", pid, out)
	}
	if !strings.Contains(out, cfg) {
		t.Fatalf("check output must name the config %s:\n%s", cfg, out)
	}

	// The same PID in the snapshot passes.
	f, err := os.OpenFile(snap, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("open baseline: %v", err)
	}
	if _, err := fmt.Fprintln(f, pid); err != nil {
		_ = f.Close()
		t.Fatalf("append baseline: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close baseline: %v", err)
	}
	out, code = dcRun(t, root, env, bash, guard, "check", snap)
	dcRequireNotFlagged(t, out, code, "pid="+strconv.Itoa(pid)+" ", cfg)

	// End the daemon, and the guard passes again.
	if dcPIDAlive(pid) && strings.Contains(dcCmdline(pid), cfg) {
		if err := syscall.Kill(pid, syscall.SIGKILL); err != nil {
			t.Fatalf("end fake daemon: %v", err)
		}
	}
	dcWaitGone(t, pid, cfg, 10*time.Second)
	out, code = dcRun(t, root, env, bash, guard, "check", snap)
	dcRequireNotFlagged(t, out, code, "pid="+strconv.Itoa(pid)+" ", cfg)

	// A daemon whose config lives outside every temporary location is not a
	// test daemon and must never be flagged. A shared host may legitimately
	// run other test daemons (they land in the baseline), so this section
	// only asserts about the daemon it starts itself.
	otherCfg := "/home/operator/datawatch/config.yaml"
	otherPid := dcFakeDaemon(t, bash, otherCfg)
	otherSnap := filepath.Join(fix, "baseline-other.txt")
	out, code = dcRun(t, root, env, bash, guard, "snapshot", otherSnap)
	if code != 0 {
		t.Fatalf("snapshot with a non-temp daemon live: want exit 0, got %d\n%s", code, out)
	}
	baseline, err := os.ReadFile(otherSnap)
	if err != nil {
		t.Fatalf("read baseline: %v", err)
	}
	for _, line := range strings.Split(string(baseline), "\n") {
		if line == strconv.Itoa(otherPid) {
			t.Fatalf("a non-temp daemon (pid %d) must not be recorded as a test daemon\n%s", otherPid, baseline)
		}
	}
	out, code = dcRun(t, root, env, bash, guard, "check", otherSnap)
	dcRequireNotFlagged(t, out, code, "pid="+strconv.Itoa(otherPid)+" ", otherCfg)
}

// TestLeftoverDaemonGuardListening is the listening-socket variant of the
// guard: a fake daemon that actually binds a 127.0.0.1 port must surface
// that port in the check report. Skipped when python3 is not present.
func TestLeftoverDaemonGuardListening(t *testing.T) {
	bash := dcBash(t)
	root := dcRepoRoot(t)
	guard := filepath.Join(root, "scripts", "check-no-leftover-test-daemons.sh")
	if _, err := os.Stat(guard); err != nil {
		t.Fatalf("guard missing at %s: %v", guard, err)
	}
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 is not on PATH; cannot build a listening fake daemon")
	}
	fix := t.TempDir()
	cfg := filepath.Join(fix, "config.yaml")
	if err := os.WriteFile(cfg, []byte("fixture: true\n"), 0o644); err != nil {
		t.Fatalf("write fixture config: %v", err)
	}
	env := dcEnv("TMPDIR=" + fix)
	snap := filepath.Join(fix, "baseline.txt")

	out, code := dcRun(t, root, env, bash, guard, "snapshot", snap)
	if code != 0 {
		t.Fatalf("snapshot: want exit 0, got %d\n%s", code, out)
	}

	// Pick a free 127.0.0.1 port and hand it to a python daemon.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen for a free port: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	if err := ln.Close(); err != nil {
		t.Fatalf("close probe listener: %v", err)
	}
	py := filepath.Join(fix, "daemon.py")
	const pySrc = `import socket, sys, time
port = int(sys.argv[1])
s = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
s.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
s.bind(("127.0.0.1", port))
s.listen()
time.sleep(300)
`
	if err := os.WriteFile(py, []byte(pySrc), 0o644); err != nil {
		t.Fatalf("write daemon.py: %v", err)
	}

	cmd := exec.Command(bash, "-c",
		fmt.Sprintf("exec -a 'datawatch start --foreground --config %s' %s %s %d", cfg, python, py, port))
	cmd.Env = dcEnv()
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		t.Fatalf("start listening fake daemon: %v", err)
	}
	pid := cmd.Process.Pid
	dcWaitAlive(t, pid, 5*time.Second)
	t.Cleanup(func() {
		if dcPIDAlive(pid) && strings.Contains(dcCmdline(pid), cfg) {
			_ = syscall.Kill(pid, syscall.SIGKILL)
		}
		_ = cmd.Wait()
	})

	// Bounded wait until the fake daemon is actually accepting.
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	deadline := time.Now().Add(10 * time.Second)
	for {
		conn, derr := net.DialTimeout("tcp", addr, 300*time.Millisecond)
		if derr == nil {
			_ = conn.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("fake daemon never started listening on %s", addr)
		}
		if !dcPIDAlive(pid) {
			t.Fatalf("fake daemon pid %d died before it began listening", pid)
		}
		time.Sleep(100 * time.Millisecond)
	}

	out, code = dcRun(t, root, env, bash, guard, "check", snap)
	if code != 1 {
		t.Fatalf("check with a listening fake daemon: want exit 1, got %d\n%s", code, out)
	}
	if !strings.Contains(out, "leftover test daemon") || !strings.Contains(out, "pid="+strconv.Itoa(pid)+" ") {
		t.Fatalf("check output must name pid %d:\n%s", pid, out)
	}
	if !strings.Contains(out, strconv.Itoa(port)) {
		t.Fatalf("check output must surface the listening port %d:\n%s", port, out)
	}
}

// TestDaemonCleanupAudit runs the static cleanup audit against fixture
// files: the compliant one passes; a shell script with no trap, one with an
// EXIT-only trap, one that ends by name, and a Go test without t.Cleanup
// each fail with their own message.
func TestDaemonCleanupAudit(t *testing.T) {
	bash := dcBash(t)
	root := dcRepoRoot(t)
	audit := filepath.Join(root, "scripts", "check-test-daemon-cleanup.sh")
	if _, err := os.Stat(audit); err != nil {
		t.Fatalf("audit missing at %s: %v", audit, err)
	}

	type fixture struct {
		name     string
		file     string
		body     string
		wantFail bool
		needle   string
	}
	fixtures := []fixture{
		{
			name: "Compliant",
			file: "ok.sh",
			body: `#!/usr/bin/env bash
# fixture: cleanup is owned by the shared daemon library and its traps, so
# every exit path (normal, failure, INT, TERM, HUP) ends the recorded daemon
# and only that PID.
set -euo pipefail
source scripts/test-daemon-lib.sh
td_install_traps
td_start datawatch "./cfg.yaml" "./daemon.log"
if [[ -z "${TD_LAST_PID:-}" ]]; then
    exit 1
fi
`,
			wantFail: false,
			needle:   "OK: no daemon cleanup violations",
		},
		{
			name: "NoTrap",
			file: "notrap.sh",
			body: `#!/usr/bin/env bash
# fixture: starts a daemon and never ends it
datawatch start --foreground --config ./cfg.yaml
`,
			wantFail: true,
			needle:   "no cleanup on every exit path",
		},
		{
			name: "ExitTrapOnly",
			file: "exitonly.sh",
			body: `#!/usr/bin/env bash
# fixture: an EXIT-only trap leaves failure and interrupt paths open
datawatch start --foreground --config ./cfg.yaml
trap 'sleep 0.2' EXIT
`,
			wantFail: true,
			needle:   "traps do not cover EXIT, INT and TERM",
		},
		{
			name: "EndByName",
			file: "endbyname.sh",
			body: `#!/usr/bin/env bash
# fixture: ends every process whose name matches — not just the one it
# recorded.
cleanup() {
    pkill -f "datawatch"
}
trap cleanup EXIT
`,
			wantFail: true,
			needle:   "ends processes by name",
		},
		{
			name: "GoNoCleanup",
			file: "noclean_test.go",
			body: `package fixture

import (
	"os/exec"
	"testing"
)

func TestDaemon(t *testing.T) {
	_ = exec.Command("datawatch", "start", "--foreground", "--config", "cfg.yaml").Run()
}
`,
			wantFail: true,
			needle:   "without registering a t.Cleanup",
		},
		{
			name: "GoCleanupOK",
			file: "ok_test.go",
			body: `package fixture

import (
	"os"
	"os/exec"
	"testing"
)

func TestDaemon(t *testing.T) {
	cmd := exec.Command("datawatch", "start", "--foreground", "--config", "cfg.yaml")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start fixture daemon: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Signal(os.Interrupt)
		_ = cmd.Wait()
	})
}
`,
			wantFail: false,
			needle:   "OK: no daemon cleanup violations",
		},
		{
			name: "MarkerDoesNotExemptEndByName",
			file: "markerpk.sh",
			body: `#!/usr/bin/env bash
# fixture: the marker on the pkill line does not authorize an end by name
set -euo pipefail
pkill -f "datawatch"   # daemon-cleanup-ok
`,
			wantFail: true,
			needle:   "the marker does not authorize ending a process by name",
		},
		{
			name: "KillFromPgrep",
			file: "killpgrep.sh",
			body: `#!/usr/bin/env bash
# fixture: signals a PID found by a name search \u2014 not the PID it recorded
set -euo pipefail
kill -TERM "$(pgrep -x datawatch)" 2>/dev/null || true
`,
			wantFail: true,
			needle:   "signals a PID found by a name search (pgrep)",
		},
		{
			name: "KillFromPgrepMarked",
			file: "killpgrepmarked.sh",
			body: `#!/usr/bin/env bash
# fixture: the marker does not authorize a pgrep-found kill either
set -euo pipefail
kill -TERM "$(pgrep -x datawatch)" 2>/dev/null || true   # daemon-cleanup-ok
`,
			wantFail: true,
			needle:   "signals a PID found by a name search (pgrep), on a line carrying the marker",
		},
		{
			name: "KillFromPsGrepPipe",
			file: "psgrepkill.sh",
			body: `#!/usr/bin/env bash
# fixture: a ps-and-grep pipeline feeding a kill is an end found by name
set -euo pipefail
kill -TERM "$(ps -eo args | grep -F "datawatch start" | grep -v grep | awk '{print $1}')" 2>/dev/null || true
`,
			wantFail: true,
			needle:   "kills PIDs found by a ps/grep pipeline instead of signalling only the PID it recorded",
		},
		{
			name: "BareMarker",
			file: "baremarker.sh",
			body: `#!/usr/bin/env bash
# fixture: a marker on a line that needs no exemption is itself a violation
set -euo pipefail
echo "nothing dangerous here"   # daemon-cleanup-ok
`,
			wantFail: true,
			needle:   "carries the marker on a line that needs no exemption",
		},
		{
			name: "PatternDefMarkedOK",
			file: "defok.sh",
			body: `#!/usr/bin/env bash
# fixture: the marker on a banned-pattern definition exempts the words
set -euo pipefail
BANNED='pkill -f|killall'   # daemon-cleanup-ok
`,
			wantFail: false,
			needle:   "OK: no daemon cleanup violations",
		},
		{
			name: "RecordedPidKillMarkedOK",
			file: "recok.sh",
			body: `#!/usr/bin/env bash
# fixture: the marker on the recorded start and end lines \u2014 the daemon is
# recorded, ended by its recorded PID, and its own traps cover every path
set -euo pipefail
datawatch start --foreground --config ./cfg.yaml ./log &   # daemon-cleanup-ok
pid=$!
cleanup() {
    kill -INT "$pid" 2>/dev/null || true    # daemon-cleanup-ok
    kill -KILL "$pid" 2>/dev/null || true   # daemon-cleanup-ok
}
trap cleanup EXIT INT TERM
`,
			wantFail: false,
			needle:   "OK: no daemon cleanup violations",
		},
		{
			name: "GoBareMarker",
			file: "gobare_test.go",
			body: `package fixture

import "testing"

func TestMarker(t *testing.T) {
	_ = 1 // daemon-cleanup-ok
}
`,
			wantFail: true,
			needle:   "carries the marker on a line that needs no exemption",
		},
	}

	for _, fx := range fixtures {
		fx := fx
		t.Run(fx.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, fx.file)
			if err := os.WriteFile(path, []byte(fx.body), 0o644); err != nil {
				t.Fatalf("write fixture %s: %v", fx.file, err)
			}
			out, code := dcRun(t, dir, dcEnv(), bash, audit, path)
			if fx.wantFail && code == 0 {
				t.Fatalf("audit must reject %s:\n%s", fx.name, out)
			}
			if !fx.wantFail && code != 0 {
				t.Fatalf("audit must accept %s, got exit %d:\n%s", fx.name, code, out)
			}
			if !strings.Contains(out, fx.needle) {
				t.Fatalf("audit output for %s must contain %q:\n%s", fx.name, fx.needle, out)
			}
		})
	}
}

// TestRepoDaemonScriptsPassAudit runs the cleanup audit over the whole
// repository and fails (with the audit output) if any script or test starts
// a daemon it does not own on every exit path. Skipped when bash is not on
// PATH.
func TestRepoDaemonScriptsPassAudit(t *testing.T) {
	bash := dcBash(t)
	root := dcRepoRoot(t)
	out, code := dcRun(t, root, dcEnv(), bash, filepath.Join(root, "scripts", "check-test-daemon-cleanup.sh"))
	if code != 0 {
		t.Fatalf("daemon cleanup audit over the repo: exit %d\n%s", code, out)
	}
	if !strings.Contains(out, "OK: no daemon cleanup violations") {
		t.Fatalf("audit output lacks the OK line:\n%s", out)
	}
}
