package main

import (
	"errors"
	"os/exec"
	"testing"

	autonomouspkg "github.com/dmz006/datawatch/internal/autonomous"
)

func TestFormatRunnerProcessLine_NodeHostedPi(t *testing.T) {
	got := formatRunnerProcessLine(4242, "MainThread",
		[]string{"node", "/usr/lib/node_modules/@pi/cli/bin/pi", "--print", "task"})
	want := "4242 MainThread node /usr/lib/node_modules/@pi/cli/bin/pi"
	if got != want {
		t.Fatalf("formatRunnerProcessLine = %q, want %q", got, want)
	}
}

func TestFormatRunnerProcessLine_EmptyArgv(t *testing.T) {
	if got, want := formatRunnerProcessLine(77, "sleep", nil), "77 sleep"; got != want {
		t.Fatalf("formatRunnerProcessLine = %q, want %q", got, want)
	}
	if got, want := formatRunnerProcessLine(78, "", []string{}), "78"; got != want {
		t.Fatalf("formatRunnerProcessLine = %q, want %q", got, want)
	}
}

func TestRunnerProcessLines_MissingSessionIsNil(t *testing.T) {
	for _, bin := range []string{"tmux", "pgrep"} {
		if _, err := exec.LookPath(bin); err != nil {
			t.Skipf("%s not available", bin)
		}
	}
	t.Setenv("TMUX_TMPDIR", t.TempDir())
	t.Setenv("TMUX", "")
	if got := runnerProcessLines("no-such-session-for-runner-evidence"); got != nil {
		t.Fatalf("runnerProcessLines for a missing session = %v, want nil", got)
	}
	if got := runnerProcessLines(""); got != nil {
		t.Fatalf("runnerProcessLines for an empty name = %v, want nil", got)
	}
}

func TestWithRunnerExitStatus_KeepsStalledMatch(t *testing.T) {
	base := autonomouspkg.ExitSilenceError("runner gone")
	status := 3
	err := withRunnerExitStatus(base, &autonomouspkg.RunnerEvidence{ExitStatus: &status})
	if !errors.Is(err, autonomouspkg.ErrWorkerStalled) {
		t.Fatalf("errors.Is(err, ErrWorkerStalled) = false; err = %v", err)
	}
	if want := base.Error() + " (pi exited with status 3)"; err.Error() != want {
		t.Fatalf("err = %q, want %q", err.Error(), want)
	}
}

func TestWithRunnerExitStatus_NoStatusLeavesErrorAlone(t *testing.T) {
	base := autonomouspkg.ExitSilenceError("runner gone")
	if got := withRunnerExitStatus(base, nil); got != base {
		t.Fatalf("nil evidence changed the error: %v", got)
	}
	if got := withRunnerExitStatus(base, &autonomouspkg.RunnerEvidence{}); got != base {
		t.Fatalf("evidence without a status changed the error: %v", got)
	}
}
