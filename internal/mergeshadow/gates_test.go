package mergeshadow

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// writeGateMakefile writes a temporary Makefile with a passing target, a
// failing target and a sleeping target, and returns its directory.
func writeGateMakefile(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("make"); err != nil {
		t.Skip("make not on PATH")
	}
	dir := t.TempDir()
	body := "" +
		"pass:\n" +
		"\t@echo gate-pass-output\n" +
		"\n" +
		"fail:\n" +
		"\t@echo gate-fail-output\n" +
		"\t@exit 3\n" +
		"\n" +
		"sleep:\n" +
		"\t@sleep 30\n"
	if err := os.WriteFile(filepath.Join(dir, "Makefile"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestRunGatePassingTarget(t *testing.T) {
	dir := writeGateMakefile(t)
	res := RunGate(context.Background(), dir, "pass", 30*time.Second)
	if res.Status != GatePass {
		t.Fatalf("status = %q, want pass (detail %q, output %q)", res.Status, res.Detail, res.Output)
	}
	if !strings.Contains(res.Output, "gate-pass-output") {
		t.Errorf("output %q missing make output", res.Output)
	}
	if res.Duration <= 0 {
		t.Errorf("duration = %v, want > 0", res.Duration)
	}
}

func TestRunGateFailingTarget(t *testing.T) {
	dir := writeGateMakefile(t)
	res := RunGate(context.Background(), dir, "fail", 30*time.Second)
	if res.Status != GateFail {
		t.Fatalf("status = %q, want fail", res.Status)
	}
	if !strings.Contains(res.Output, "gate-fail-output") {
		t.Errorf("output %q missing make output", res.Output)
	}
	if res.Detail == "" {
		t.Error("failing gate should record why it failed in Detail")
	}
}

func TestRunGateSleepingTargetTimesOut(t *testing.T) {
	dir := writeGateMakefile(t)
	start := time.Now()
	res := RunGate(context.Background(), dir, "sleep", 500*time.Millisecond)
	elapsed := time.Since(start)
	if res.Status != GateTimeout {
		t.Fatalf("status = %q, want timeout (detail %q)", res.Status, res.Detail)
	}
	if elapsed > 15*time.Second {
		t.Errorf("timed-out gate took %v; the timeout did not stop it", elapsed)
	}
}

func TestRunGateMissingTargetSkipped(t *testing.T) {
	dir := writeGateMakefile(t)
	res := RunGate(context.Background(), dir, "lint", time.Second)
	if res.Status != GateSkipped {
		t.Fatalf("status = %q, want skipped", res.Status)
	}
	if !strings.Contains(res.Detail, "lint") {
		t.Errorf("detail %q should name the missing target", res.Detail)
	}
}

func TestRunGateNoMakefileSkipped(t *testing.T) {
	res := RunGate(context.Background(), t.TempDir(), "build", time.Second)
	if res.Status != GateSkipped {
		t.Fatalf("status = %q, want skipped", res.Status)
	}
}

func TestMakefileHasGateTargetMatchesPipelineRule(t *testing.T) {
	dir := t.TempDir()
	// "passx:" must not match "pass:"; the first Makefile name found is used.
	if err := os.WriteFile(filepath.Join(dir, "Makefile"), []byte("passx:\n\techo\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if makefileHasGateTarget(dir, "pass") {
		t.Error("prefix match on a longer target name must not count")
	}
	if !makefileHasGateTarget(dir, "passx") {
		t.Error("exact target line should be found")
	}
}

func TestRunGatesReportsEveryGate(t *testing.T) {
	dir := writeGateMakefile(t)
	// The temp Makefile defines none of build/lint/test, so each is skipped.
	results := RunGates(context.Background(), dir, time.Second)
	if len(results) != len(gateNames) {
		t.Fatalf("got %d results, want %d", len(results), len(gateNames))
	}
	for i, r := range results {
		if r.Name != gateNames[i] {
			t.Errorf("result %d name = %q, want %q", i, r.Name, gateNames[i])
		}
		if r.Status != GateSkipped {
			t.Errorf("%s status = %q, want skipped", r.Name, r.Status)
		}
	}
}

func TestLintGateRecordsGolangciLintVersion(t *testing.T) {
	dir := writeGateMakefile(t)
	if err := os.WriteFile(filepath.Join(dir, "Makefile"),
		[]byte("lint:\n\t@echo linted\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	res := RunGate(context.Background(), dir, "lint", 30*time.Second)
	if res.Status != GatePass {
		t.Fatalf("status = %q, want pass", res.Status)
	}
	if _, err := exec.LookPath("golangci-lint"); err != nil {
		if !strings.Contains(res.Detail, "golangci-lint not found") {
			t.Errorf("detail %q should say golangci-lint is missing", res.Detail)
		}
		return
	}
	if !strings.Contains(res.Detail, "golangci-lint: ") {
		t.Errorf("detail %q should record the golangci-lint version", res.Detail)
	}
}
