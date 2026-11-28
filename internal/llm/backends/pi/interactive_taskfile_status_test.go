package pi

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/testhome"
)

// runInteractiveCmdWithTaskFile is runInteractiveCmdWithStubPi (in
// interactive_loud_failure_test.go) with the one change the tests here need:
// a NON-EMPTY task, so the command line carries the scratch task file and
// its "rm -f" cleanup. A failing pi on this path used to be reported as
// success, because the status capture ran after that cleanup — these tests
// pin the fix.
func runInteractiveCmdWithTaskFile(t *testing.T, stubBody, model, task string) (string, error) {
	t.Helper()
	dir := t.TempDir()
	stub := filepath.Join(dir, "pi-stub")
	if err := os.WriteFile(stub, []byte("#!/bin/sh\n"+stubBody), 0o755); err != nil { //nolint:gosec // test fixture must be executable
		t.Fatalf("write stub pi: %v", err)
	}
	cmd, err := buildInteractiveCmd(stub, model, "taskfile-sess", task, dir, "cs-taskfile-test")
	if err != nil {
		t.Fatalf("buildInteractiveCmd: %v", err)
	}
	out, err := exec.Command("sh", "-c", cmd).CombinedOutput()
	return string(out), err
}

// TestInteractiveTaskFileFailureKeepsPiStatus: pi exits 3 and the task came
// through a scratch task file. The run must report DATAWATCH_PI_FAILED with
// pi's status 3, must NOT print DATAWATCH_COMPLETE, and must exit with
// pi's own status — not 0, and not the cleanup's status.
func TestInteractiveTaskFileFailureKeepsPiStatus(t *testing.T) {
	testhome.PrivateTmp(t) // the scratch task file stays in the test's temp dir
	out, err := runInteractiveCmdWithTaskFile(t, "exit 3\n", "prov/model-a", "do the thing")
	if code := exitCodeOf(t, err); code != 3 {
		t.Errorf("exit status = %d, want 3 (pi's own status; output %q)", code, out)
	}
	if !strings.Contains(out, "DATAWATCH_PI_FAILED: pi exited with status 3") {
		t.Errorf("expected the failure line naming pi's status, got: %q", out)
	}
	if strings.Contains(out, "DATAWATCH_COMPLETE") {
		t.Errorf("a failed pi must not be reported complete, got: %q", out)
	}
}

// TestInteractiveTaskFileZeroExitStillCompletes: the fix must not bite the
// other way — a clean run on the task-file path still prints exactly the
// completion marker and exits 0.
func TestInteractiveTaskFileZeroExitStillCompletes(t *testing.T) {
	testhome.PrivateTmp(t)
	out, err := runInteractiveCmdWithTaskFile(t, "exit 0\n", "prov/model-a", "do the thing")
	if code := exitCodeOf(t, err); code != 0 {
		t.Fatalf("a clean run must exit 0, got %d (output %q)", code, out)
	}
	if !strings.Contains(out, "DATAWATCH_COMPLETE: pi session ended") {
		t.Errorf("clean run must print the completion marker, got: %q", out)
	}
	if strings.Contains(out, "DATAWATCH_PI_FAILED") {
		t.Errorf("clean run must not print the failure line, got: %q", out)
	}
}

// TestInteractiveStatusCapturedBeforeTaskFileCleanup pins the statement
// order in the built command: pi's exit status must be captured in the
// statement immediately after pi, with no "rm" before it. (This is the
// structural half of the behavioral tests above; before the fix the first
// "pi_status=$?" in the command sat after the task-file "rm -f".)
func TestInteractiveStatusCapturedBeforeTaskFileCleanup(t *testing.T) {
	testhome.PrivateTmp(t)
	cmd, err := buildInteractiveCmd("pi", "prov/model-a", "taskfile-sess", "do the thing", "/proj", "cs-abc")
	if err != nil {
		t.Fatalf("buildInteractiveCmd: %v", err)
	}
	i := strings.Index(cmd, "pi_status=$?")
	if i == -1 {
		t.Fatalf("no status capture in command: %s", cmd)
	}
	if got := cmd[:i]; strings.Contains(got, "rm -f ") || strings.Contains(got, "rm -rf ") {
		t.Errorf("a cleanup runs before the status capture, which would let a failed pi be reported complete:\n%s\n---\n(through: %s)", cmd, got)
	}
}
