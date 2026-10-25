package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"testing"
	"time"
)

// These tests characterize what the idle probes in session_idle_probe.go report
// for real processes. Each one runs a stub named pi inside an interactive bash
// pane of a private tmux server, the same shape as a pi-prompt launch, and
// asserts the values the current code produces. They are measurements, not a
// statement of the desired behavior.

const piPromptLivenessPaneSettleTimeout = 3 * time.Second

// piPromptLivenessPaneIsolate points tmux at a private socket directory so the
// probes can reach only the server this test starts.
func piPromptLivenessPaneIsolate(t *testing.T, needNode bool) {
	t.Helper()
	t.Setenv("TMUX_TMPDIR", t.TempDir())
	t.Setenv("TMUX", "")
	tools := []string{"tmux", "pgrep"}
	if needNode {
		tools = append(tools, "node")
	}
	for _, tool := range tools {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s not on PATH", tool)
		}
	}
}

func piPromptLivenessPaneTmux(t *testing.T, args ...string) string {
	t.Helper()
	out, err := exec.Command("tmux", args...).CombinedOutput()
	if err != nil {
		t.Fatalf("tmux %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out))
}

// piPromptLivenessPaneReap sends SIGKILL to every process under the panes of
// the session, then to the pane shells. The private server exits by itself once
// its last pane shell is gone. Pids come from the process probes.
func piPromptLivenessPaneReap(name string) {
	roots, tree, ok := sessionProcessTree(name)
	if !ok {
		return
	}
	for _, kids := range tree {
		for _, pid := range kids {
			_ = syscall.Kill(pid, syscall.SIGKILL)
		}
	}
	for _, pid := range roots {
		_ = syscall.Kill(pid, syscall.SIGKILL)
	}
}

// piPromptLivenessPaneStart starts a detached session whose pane runs an
// interactive bash, and registers its cleanup.
func piPromptLivenessPaneStart(t *testing.T, name string) {
	t.Helper()
	t.Cleanup(func() { piPromptLivenessPaneReap(name) })
	piPromptLivenessPaneTmux(t, "new-session", "-d", "-s", name, "-x", "200", "-y", "50", "bash --norc --noprofile")
	piPromptLivenessPaneTmux(t, "list-panes", "-t", name, "-F", "#{pane_pid}")
}

// piPromptLivenessPaneRun writes body as an executable named pi and types its
// full path into the pane, so the stub is a child of the pane shell.
func piPromptLivenessPaneRun(t *testing.T, name, body string) {
	t.Helper()
	stub := filepath.Join(t.TempDir(), "pi")
	if err := os.WriteFile(stub, []byte(body), 0o755); err != nil { // #nosec G306 -- the stub must be executable
		t.Fatalf("write stub: %v", err)
	}
	if err := os.Chmod(stub, 0o755); err != nil { // #nosec G302 -- the stub must be executable
		t.Fatalf("chmod stub: %v", err)
	}
	piPromptLivenessPaneTmux(t, "send-keys", "-t", name, "-l", stub)
	piPromptLivenessPaneTmux(t, "send-keys", "-t", name, "Enter")
}

// piPromptLivenessPaneComms returns pid -> kernel command name for every
// process below the pane shells.
func piPromptLivenessPaneComms(tree map[int][]int) map[int]string {
	comms := make(map[int]string)
	for _, kids := range tree {
		for _, pid := range kids {
			comms[pid] = readProcessCommName(pid)
		}
	}
	return comms
}

func piPromptLivenessPaneSnapshot(comms map[int]string) string {
	lines := make([]string, 0, len(comms))
	for pid, comm := range comms {
		lines = append(lines, fmt.Sprintf("%d:%s", pid, comm))
	}
	sort.Strings(lines)
	return strings.Join(lines, " ")
}

// piPromptLivenessPaneSettle polls until ready holds for the process tree under
// the session and the tree is unchanged between two consecutive polls.
func piPromptLivenessPaneSettle(t *testing.T, name string, ready func(comms map[int]string) bool) map[int]string {
	t.Helper()
	deadline := time.Now().Add(piPromptLivenessPaneSettleTimeout)
	prev := ""
	for time.Now().Before(deadline) {
		if _, tree, ok := sessionProcessTree(name); ok {
			comms := piPromptLivenessPaneComms(tree)
			if ready(comms) {
				snap := piPromptLivenessPaneSnapshot(comms)
				if snap == prev {
					return comms
				}
				prev = snap
			} else {
				prev = ""
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("process tree under the pane did not settle within %s", piPromptLivenessPaneSettleTimeout)
	return nil
}

func piPromptLivenessPaneHasComm(comms map[int]string, want string) bool {
	for _, comm := range comms {
		if comm == want {
			return true
		}
	}
	return false
}

// piPromptLivenessPaneHasNodeArgv reports whether a process below the pane runs
// node, going by argv rather than the kernel command name.
func piPromptLivenessPaneHasNodeArgv(comms map[int]string) bool {
	for pid := range comms {
		if argv := readProcessCmdline(pid); len(argv) > 0 && filepath.Base(argv[0]) == "node" {
			return true
		}
	}
	return false
}

// piPromptLivenessPaneMeasure runs both probes and logs what they report.
func piPromptLivenessPaneMeasure(t *testing.T, name string, comms map[int]string) (runner, child bool) {
	t.Helper()
	projectDir := t.TempDir()
	child = hasLiveChildProcess(name, projectDir)
	runner = hasLiveRunnerProcess(name)
	names := make([]string, 0, len(comms))
	for _, comm := range comms {
		names = append(names, comm)
	}
	sort.Strings(names)
	t.Logf("process names under the pane: %v", names)
	t.Logf("hasLiveChildProcess=%v hasLiveRunnerProcess=%v", child, runner)
	return runner, child
}

// TestPiPromptLivenessPanePlainRunner: a sh stub blocked reading the pane tty,
// with no child process.
func TestPiPromptLivenessPanePlainRunner(t *testing.T) {
	piPromptLivenessPaneIsolate(t, false)
	const name = "liveness-plain"
	piPromptLivenessPaneStart(t, name)
	piPromptLivenessPaneRun(t, name, "#!/bin/sh\nread line\n")
	comms := piPromptLivenessPaneSettle(t, name, func(c map[int]string) bool {
		return piPromptLivenessPaneHasComm(c, "pi")
	})
	runner, child := piPromptLivenessPaneMeasure(t, name, comms)
	if !runner {
		t.Errorf("hasLiveRunnerProcess = false, want true for a plain pi runner")
	}
	if child {
		t.Errorf("hasLiveChildProcess = true, want false for a runner with no child")
	}
}

// TestPiPromptLivenessPaneNodeRunner: a node-hosted stub holding a timer. The
// kernel names the process MainThread, which is not in sessionIdleRunnerNames.
func TestPiPromptLivenessPaneNodeRunner(t *testing.T) {
	piPromptLivenessPaneIsolate(t, true)
	const name = "liveness-node"
	piPromptLivenessPaneStart(t, name)
	piPromptLivenessPaneRun(t, name, "#!/usr/bin/env node\nsetInterval(() => {}, 1000);\n")
	comms := piPromptLivenessPaneSettle(t, name, piPromptLivenessPaneHasNodeArgv)
	runner, child := piPromptLivenessPaneMeasure(t, name, comms)
	// The runner-resolution task flips this assertion to true.
	if runner {
		t.Errorf("hasLiveRunnerProcess = true, want false: a node-run pi reports the name MainThread")
	}
	if child {
		t.Errorf("hasLiveChildProcess = true, want false for a node runner with no child")
	}
}

// TestPiPromptLivenessPaneRunnerWithChild: a sh stub with a background sleep.
func TestPiPromptLivenessPaneRunnerWithChild(t *testing.T) {
	piPromptLivenessPaneIsolate(t, false)
	const name = "liveness-child"
	piPromptLivenessPaneStart(t, name)
	piPromptLivenessPaneRun(t, name, "#!/bin/sh\nsleep 300 &\nwait\n")
	comms := piPromptLivenessPaneSettle(t, name, func(c map[int]string) bool {
		return piPromptLivenessPaneHasComm(c, "pi") && piPromptLivenessPaneHasComm(c, "sleep")
	})
	runner, child := piPromptLivenessPaneMeasure(t, name, comms)
	if !runner {
		t.Errorf("hasLiveRunnerProcess = false, want true for a pi runner with a child")
	}
	if !child {
		t.Errorf("hasLiveChildProcess = false, want true for a runner with a live child")
	}
}

// TestPiPromptLivenessPaneFinishedRun: no stub, the bash pane left at its
// prompt, the shape of a finished run.
func TestPiPromptLivenessPaneFinishedRun(t *testing.T) {
	piPromptLivenessPaneIsolate(t, false)
	const name = "liveness-finished"
	piPromptLivenessPaneStart(t, name)
	comms := piPromptLivenessPaneSettle(t, name, func(c map[int]string) bool {
		return len(c) == 0
	})
	runner, child := piPromptLivenessPaneMeasure(t, name, comms)
	if runner {
		t.Errorf("hasLiveRunnerProcess = true, want false for a pane at its prompt")
	}
	if child {
		t.Errorf("hasLiveChildProcess = true, want false for a pane at its prompt")
	}
}
