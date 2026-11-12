package autonomous

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// gateProject returns a temp project dir with an empty .datawatch/verify folder.
func gateProject(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".datawatch", "verify"), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

// writeGateScript writes body to .datawatch/verify/<name> under dir.
func writeGateScript(t *testing.T, dir, name, body string) string {
	t.Helper()
	rel := ".datawatch/verify/" + name
	if err := os.WriteFile(filepath.Join(dir, rel), []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return rel
}

func TestScriptGateRunPassingScript(t *testing.T) {
	dir := gateProject(t)
	rel := writeGateScript(t, dir, "pass.sh", "echo gate-ok\n")
	res := RunScriptGate(context.Background(), GateCheck{Kind: GateCheckScript, Script: rel}, dir, 10*time.Second)
	if !res.Passed || res.ExitCode != 0 || res.TimedOut || res.Reason != "" {
		t.Fatalf("want pass, got %+v", res)
	}
	if !strings.Contains(res.Output, "gate-ok") {
		t.Fatalf("output missing script output: %q", res.Output)
	}
}

func TestScriptGateRunFailingScript(t *testing.T) {
	dir := gateProject(t)
	rel := writeGateScript(t, dir, "fail.sh", "echo broken\nexit 3\n")
	res := RunScriptGate(context.Background(), GateCheck{Kind: GateCheckScript, Script: rel}, dir, 10*time.Second)
	if res.Passed || res.ExitCode != 3 || res.TimedOut {
		t.Fatalf("want fail with exit 3, got %+v", res)
	}
	if !strings.Contains(res.Reason, "code 3") {
		t.Fatalf("reason should name the exit code: %q", res.Reason)
	}
	if !strings.Contains(res.Output, "broken") {
		t.Fatalf("output missing script output: %q", res.Output)
	}
}

func TestScriptGateRunPassingCommand(t *testing.T) {
	dir := gateProject(t)
	res := RunScriptGate(context.Background(), GateCheck{Kind: GateCheckCommand, Command: "pwd"}, dir, 10*time.Second)
	if !res.Passed {
		t.Fatalf("want pass, got %+v", res)
	}
	if !strings.Contains(res.Output, filepath.Base(dir)) {
		t.Fatalf("command should run in the project dir, output %q", res.Output)
	}
}

func TestScriptGateRunFailingCommand(t *testing.T) {
	dir := gateProject(t)
	res := RunScriptGate(context.Background(), GateCheck{Kind: GateCheckCommand, Command: "exit 7"}, dir, 10*time.Second)
	if res.Passed || res.ExitCode != 7 {
		t.Fatalf("want fail with exit 7, got %+v", res)
	}
}

func TestScriptGateRunTimeoutKillsChild(t *testing.T) {
	dir := gateProject(t)
	pidFile := filepath.Join(dir, "child.pid")
	// The script records its own pid, then sleeps. The sleep is a child of
	// the script, so a kill that reaches only the script would leave it.
	rel := writeGateScript(t, dir, "slow.sh", "echo $$ > "+pidFile+"\nsleep 30\n")

	start := time.Now()
	res := RunScriptGate(context.Background(), GateCheck{Kind: GateCheckScript, Script: rel}, dir, time.Second)
	if elapsed := time.Since(start); elapsed > 15*time.Second {
		t.Fatalf("gate did not stop at its limit, took %s", elapsed)
	}
	if !res.TimedOut || res.Passed {
		t.Fatalf("want timed out and not passed, got %+v", res)
	}
	if !strings.Contains(res.Reason, "1-second limit") {
		t.Fatalf("reason should name the limit in seconds: %q", res.Reason)
	}

	raw, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatalf("script never wrote its pid: %v", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil {
		t.Fatalf("bad pid file %q: %v", raw, err)
	}
	waitForProcessGone(t, pid)
}

// waitForProcessGone fails the test if pid is still running (a zombie counts
// as gone; the process has exited).
func waitForProcessGone(t *testing.T, pid int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if processGone(pid) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("child process %d is still running after the gate timed out", pid)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func processGone(pid int) bool {
	if err := syscall.Kill(pid, 0); err != nil {
		return err == syscall.ESRCH
	}
	stat, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "stat"))
	if err != nil {
		return true
	}
	// Field 3 of /proc/<pid>/stat is the state; "Z" is an exited, unreaped child.
	fields := strings.Fields(string(stat))
	return len(fields) > 2 && fields[2] == "Z"
}

func TestScriptGateRunUnsafePath(t *testing.T) {
	dir := gateProject(t)
	outside := filepath.Join(t.TempDir(), "outside.sh")
	if err := os.WriteFile(outside, []byte("exit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	cases := map[string]string{
		"absolute":         outside,
		"parent escape":    "../outside.sh",
		"parent in verify": ".datawatch/verify/../../outside.sh",
		"outside verify":   "scripts/gate.sh",
		"empty":            "",
	}
	for name, script := range cases {
		t.Run(name, func(t *testing.T) {
			res := RunScriptGate(context.Background(), GateCheck{Kind: GateCheckScript, Script: script}, dir, 5*time.Second)
			if res.Passed || res.Reason == "" || res.TimedOut {
				t.Fatalf("unsafe path must not run and must give a reason, got %+v", res)
			}
		})
	}
}

func TestScriptGateRunMissingScript(t *testing.T) {
	dir := gateProject(t)
	start := time.Now()
	res := RunScriptGate(context.Background(), GateCheck{Kind: GateCheckScript, Script: ".datawatch/verify/absent.sh"}, dir, 30*time.Second)
	if time.Since(start) > 5*time.Second {
		t.Fatalf("missing script should fail at once, took %s", time.Since(start))
	}
	if res.Passed || res.TimedOut || res.Reason == "" {
		t.Fatalf("missing script must fail with a reason, got %+v", res)
	}
}

func TestScriptGateRunEmptyCommand(t *testing.T) {
	dir := gateProject(t)
	res := RunScriptGate(context.Background(), GateCheck{Kind: GateCheckCommand, Command: "  "}, dir, time.Second)
	if res.Passed || res.Reason == "" {
		t.Fatalf("empty command must fail with a reason, got %+v", res)
	}
}

func TestScriptGateRunResolveTaskOwnScriptFirst(t *testing.T) {
	task := &Task{ID: "t1", VerificationScript: ".datawatch/verify/t1.sh"}
	story := &Story{Tasks: []Task{
		*task,
		{ID: "t2", VerificationScript: ".datawatch/verify/t2.sh"},
	}}
	checks, reason := ResolveGateCheck(task, story, "make check")
	if reason != "" || len(checks) != 1 {
		t.Fatalf("want the task's own script alone, got %+v reason %q", checks, reason)
	}
	if checks[0].Kind != GateCheckScript || checks[0].Script != ".datawatch/verify/t1.sh" {
		t.Fatalf("wrong check: %+v", checks[0])
	}
}

func TestScriptGateRunResolveProjectCommandSecond(t *testing.T) {
	task := &Task{ID: "t1", VerificationScript: "not/under/verify.sh"}
	story := &Story{Tasks: []Task{{ID: "t2", VerificationScript: ".datawatch/verify/t2.sh"}}}
	checks, reason := ResolveGateCheck(task, story, "  make check  ")
	if reason != "" || len(checks) != 1 {
		t.Fatalf("want the project command, got %+v reason %q", checks, reason)
	}
	if checks[0].Kind != GateCheckCommand || checks[0].Command != "make check" {
		t.Fatalf("wrong check: %+v", checks[0])
	}
}

func TestScriptGateRunResolveStorySiblingsThird(t *testing.T) {
	task := &Task{ID: "t1", VerificationScript: ""}
	story := &Story{Tasks: []Task{
		{ID: "t1", VerificationScript: ".datawatch/verify/t1.sh"},
		{ID: "t2", VerificationScript: ".datawatch/verify/t2.sh"},
		{ID: "t3", VerificationScript: "/abs/t3.sh"},
		{ID: "t4", VerificationScript: ".datawatch/verify/t4.sh"},
	}}
	checks, reason := ResolveGateCheck(task, story, "")
	if reason != "" || len(checks) != 2 {
		t.Fatalf("want the two sibling scripts, got %+v reason %q", checks, reason)
	}
	if checks[0].Script != ".datawatch/verify/t2.sh" || checks[1].Script != ".datawatch/verify/t4.sh" {
		t.Fatalf("wrong sibling scripts: %+v", checks)
	}
}

func TestScriptGateRunResolveNothingRunnable(t *testing.T) {
	task := &Task{ID: "t1", VerificationScript: "bad/path.sh"}
	story := &Story{Tasks: []Task{{ID: "t2", VerificationScript: "../escape.sh"}}}
	checks, reason := ResolveGateCheck(task, story, "   ")
	if len(checks) != 0 {
		t.Fatalf("want no checks, got %+v", checks)
	}
	if !strings.Contains(reason, "nothing runnable was declared") {
		t.Fatalf("reason should say nothing runnable was declared: %q", reason)
	}
	checks, reason = ResolveGateCheck(nil, nil, "")
	if len(checks) != 0 || reason == "" {
		t.Fatalf("nil task and story must give no checks and a reason, got %+v %q", checks, reason)
	}
}
