//go:build linux

package proclimit

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// needScope skips where this system cannot give the limit. The limits here are
// small, so what runs stays small whatever happens.
func needScope(t *testing.T, bytes int64) (context.Context, *Tally) {
	t.Helper()
	s := Scope{MemoryBytes: bytes}
	if err := s.Available(context.Background()); err != nil {
		t.Skipf("no systemd scope here: %v", err)
	}
	left := &Tally{}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	t.Cleanup(cancel)
	return With(ctx, s, left), left
}

func sh(ctx context.Context, script string, args ...string) *exec.Cmd {
	return exec.CommandContext(ctx, "sh", append([]string{"-c", script, "x"}, args...)...)
}

// hog holds about 200 MB: tail keeps everything of an input with no newline.
const hog = "head -c 200000000 /dev/zero | tail | wc -c >/dev/null"

func TestTheLimitCoversTheWholeTree(t *testing.T) {
	ctx, _ := needScope(t, 64<<20)
	for name, script := range map[string]string{
		// The command itself exits 0 although what it started was stopped.
		"a child":                         hog + "; true",
		"a child in a session of its own": "setsid -w sh -c '" + hog + "'; true",
		// Many processes, each far under the limit, together over it.
		"many small ones": "for i in 1 2 3 4 5 6; do head -c 30000000 /dev/zero | { tail; sleep 2; } >/dev/null & done; wait; true",
	} {
		t.Run(name, func(t *testing.T) {
			err := Run(ctx, sh(ctx, script))
			var limit *LimitError
			if !errors.As(err, &limit) || !strings.Contains(limit.Why, "memory limit") || !strings.Contains(limit.Why, "64 MiB") {
				t.Errorf("err = %v", err)
			}
		})
	}
	if err := Run(ctx, sh(ctx, "head -c 1000000 /dev/zero | tail | wc -c >/dev/null")); err != nil {
		t.Errorf("a small command: %v", err)
	}
}

func TestTheTimeLimitStopsTheWholeTree(t *testing.T) {
	outer, _ := needScope(t, 256<<20)
	mark := filepath.Join(t.TempDir(), "outlived")
	ctx, cancel := context.WithTimeout(outer, 700*time.Millisecond)
	defer cancel()
	start := time.Now()
	err := Run(ctx, sh(ctx, `setsid sh -c 'sleep 3; touch "$1"' x "$1" >/dev/null 2>&1 & sleep 30`, mark))
	if err == nil {
		t.Error("the command was not stopped")
	}
	if took := time.Since(start); took > 10*time.Second {
		t.Errorf("it took %s", took)
	}
	time.Sleep(3500 * time.Millisecond)
	if _, err := os.Stat(mark); !os.IsNotExist(err) {
		t.Errorf("a process in a session of its own ran on: %v", err)
	}
}

func TestWhatACommandLeavesIsStoppedAndCounted(t *testing.T) {
	ctx, _ := needScope(t, 256<<20)
	mark := filepath.Join(t.TempDir(), "outlived")
	cmd := sh(ctx, `setsid sh -c 'sleep 4; touch "$1"' x "$1" >/dev/null 2>&1 & exit 3`, mark)
	finish, err := Prepare(ctx, cmd)
	if err != nil {
		t.Fatal(err)
	}
	runErr := cmd.Run()
	r := finish()
	var exit *exec.ExitError
	if !errors.As(runErr, &exit) || exit.ExitCode() != 3 {
		t.Errorf("the command's own exit status is lost: %v", runErr)
	}
	if r.Left == 0 || r.Failed != "" {
		t.Errorf("result %+v", r)
	}
	time.Sleep(4500 * time.Millisecond)
	if _, err := os.Stat(mark); !os.IsNotExist(err) {
		t.Errorf("the left process ran on: %v", err)
	}
}

func TestTheCommandGetsItsArgumentsInputAndEnvironmentUnchanged(t *testing.T) {
	ctx, _ := needScope(t, 256<<20)
	cmd := sh(ctx, `cat; printf '[%s]' "$@" "${XDG_RUNTIME_DIR-unset}" "${DBUS_SESSION_BUS_ADDRESS-unset}" "$ONLY"; pwd; cat /proc/self/cgroup`,
		"a $HOME b", "$$", "%h", "two words")
	dir := t.TempDir()
	cmd.Dir, cmd.Env, cmd.Stdin = dir, []string{"PATH=" + os.Getenv("PATH"), "ONLY=this"}, strings.NewReader("in:")
	var out strings.Builder
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := Run(ctx, cmd); err != nil {
		t.Fatalf("%v: %s", err, out.String())
	}
	for _, want := range []string{"in:[a $HOME b][$$][%h][two words][unset][unset][this]" + dir, UnitPrefix} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output lacks %q:\n%s", want, out.String())
		}
	}
}

// Without the user manager nothing is started.
func TestWithNoWayToLimitNothingRuns(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", filepath.Join(t.TempDir(), "none"))
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", "")
	if os.Geteuid() == 0 {
		t.Skip("as root the system manager is used")
	}
	s := Scope{MemoryBytes: 64 << 20}
	if err := s.Available(context.Background()); err == nil {
		t.Fatal("available without a user manager")
	}
	mark := filepath.Join(t.TempDir(), "ran")
	ctx := With(context.Background(), s, nil)
	if err := Run(ctx, sh(ctx, `touch "$1"`, mark)); err == nil {
		t.Error("no error")
	}
	if _, err := os.Stat(mark); !os.IsNotExist(err) {
		t.Errorf("the command ran with no limit: %v", err)
	}
}
