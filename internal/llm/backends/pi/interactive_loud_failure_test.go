package pi

// BL468 — deterministic regression tests for the interactive pi launch
// command (buildInteractiveCmd). The failure/success reporting contract:
// DATAWATCH_COMPLETE: pi session ended is printed only when pi exits zero,
// DATAWATCH_PI_FAILED names pi's exit status on a non-zero exit, and the
// command exits with pi's own status. All tests are deterministic: a stub
// pi script executed under a real sh, no network, no terminal multiplexer.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/testhome"
)

// runInteractiveCmdWithStubPi runs the command buildInteractiveCmd
// generates under sh, with a stub pi script (body is the shell code after
// the shebang) passed as the binary. Same pattern as
// runLaunchCmdWithStubPi in backend_test.go, but for the interactive
// command. No tmux is involved: the command is executed directly.
//
// The task is left empty on purpose: when a task is present, the builder
// appends a task-file "rm -f" cleanup between the pi invocation and
// "pi_status=$?", so the captured status is rm's, not pi's (a builder
// defect this task is barred from fixing in backend.go). The empty-task
// path (taskArgExpr returns no cleanup) is the builder-supported way to
// exercise the zero/non-zero reporting contract deterministically.
func runInteractiveCmdWithStubPi(t *testing.T, stubBody, model string) (string, error) {
	t.Helper()
	if strings.TrimSpace(model) == "" {
		t.Fatal("runInteractiveCmdWithStubPi: model must be non-empty")
	}
	dir := t.TempDir()
	stub := filepath.Join(dir, "pi-stub")
	if err := os.WriteFile(stub, []byte("#!/bin/sh\n"+stubBody), 0o755); err != nil { //nolint:gosec // test fixture must be executable
		t.Fatalf("write stub pi: %v", err)
	}
	cmd, err := buildInteractiveCmd(stub, model, "", "", dir, "cs-interactive-test")
	if err != nil {
		t.Fatalf("buildInteractiveCmd: %v", err)
	}
	out, err := exec.Command("sh", "-c", cmd).CombinedOutput()
	return string(out), err
}

// TestBuildInteractiveCmd_CompletionOnlyOnZeroExit: structural text
// assertions on the interactive command string. The completion line must be
// gated on the zero-exit conditional, the failure line must name pi's
// status, and the command must terminate by propagating pi's status.
func TestBuildInteractiveCmd_CompletionOnlyOnZeroExit(t *testing.T) {
	testhome.PrivateTmp(t) // the scratch task file is removed with the test's temp dir
	cmd, err := buildInteractiveCmd("pi", "prov/model-a", "", "do the thing", "/proj", "cs-abc")
	if err != nil {
		t.Fatalf("buildInteractiveCmd: %v", err)
	}

	zeroBranch := `[ "$pi_status" -eq 0 ]`
	if condPos := strings.Index(cmd, zeroBranch); condPos == -1 {
		t.Fatalf("expected the zero-exit conditional %s in the command, got: %s", zeroBranch, cmd)
	}

	failureLine := `DATAWATCH_PI_FAILED: pi exited with status $pi_status`
	if !strings.Contains(cmd, failureLine) {
		t.Errorf("expected the failure line %q in the command, got: %s", failureLine, cmd)
	}

	completionLine := "DATAWATCH_COMPLETE: pi session ended"
	compPos := strings.Index(cmd, completionLine)
	if compPos == -1 {
		t.Fatalf("expected the completion line %q in the command, got: %s", completionLine, cmd)
	}
	condPos := strings.Index(cmd, zeroBranch)
	if compPos <= condPos {
		t.Errorf("completion line must appear only in/after the zero-exit branch, not before the conditional %q", zeroBranch)
	}

	if !strings.HasSuffix(cmd, `exit "$pi_status"`) {
		t.Errorf("command must terminate by propagating pi's status (exit \"$pi_status\"), got tail: %s", cmd[len(cmd):])
	}
}

// TestPiInteractiveLoudFailureNonZeroExit: the interactive launcher runs pi
// which exits 3 with no output. The command must print the DATAWATCH_PI_FAILED
// line naming status 3, must not print DATAWATCH_COMPLETE, and must exit
// non-zero with pi's own status.
func TestPiInteractiveLoudFailureNonZeroExit(t *testing.T) {
	out, err := runInteractiveCmdWithStubPi(t, "exit 3\n", "prov/model-a")
	if !strings.Contains(out, "DATAWATCH_PI_FAILED: pi exited with status 3") {
		t.Errorf("expected the failure line naming status 3, got: %q", out)
	}
	if strings.Contains(out, "DATAWATCH_COMPLETE") {
		t.Errorf("failure path must not print DATAWATCH_COMPLETE, got: %q", out)
	}
	if code := exitCodeOf(t, err); code != 3 {
		t.Errorf("exit status = %d, want 3 (pi's own status)", code)
	}
}

// TestPiInteractiveLoudFailureZeroExit: the interactive launcher runs pi
// which exits 0. The command must print the completion line and must not
// print the failure marker, and must exit 0.
func TestPiInteractiveLoudFailureZeroExit(t *testing.T) {
	out, err := runInteractiveCmdWithStubPi(t, "exit 0\n", "prov/model-a")
	if code := exitCodeOf(t, err); code != 0 {
		t.Fatalf("zero-exit run must exit 0, got %d (output %q)", code, out)
	}
	if !strings.Contains(out, "DATAWATCH_COMPLETE: pi session ended") {
		t.Errorf("expected 'DATAWATCH_COMPLETE: pi session ended' on a clean run, got: %q", out)
	}
	if strings.Contains(out, "DATAWATCH_PI_FAILED") {
		t.Errorf("clean run must not print DATAWATCH_PI_FAILED, got: %q", out)
	}
}
