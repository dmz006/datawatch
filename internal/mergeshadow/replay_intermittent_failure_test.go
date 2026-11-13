//go:build unix

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

// intermittentFixturePath is the fake test that fails on its first run and
// passes afterwards, decided by a flag file. It is run through a temporary
// Makefile target.
const intermittentFixturePath = "../../testdata/mergeshadow/intermittent-failure/fake-test.sh"

// makefileWithTestTarget writes a Makefile whose test target runs recipe.
func makefileWithTestTarget(t *testing.T, recipe string) string {
	t.Helper()
	if _, err := exec.LookPath("make"); err != nil {
		t.Skip("make not on PATH")
	}
	dir := t.TempDir()
	body := "test:\n\t@" + recipe + "\n"
	if err := os.WriteFile(filepath.Join(dir, "Makefile"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

// TestRerunMarksIntermittentFailure runs the fake test through the gate. The
// first run fails and creates the flag file, the rerun passes. The gate must be
// intermittent, with the first failing lines kept as the finding and the exact
// Detail "intermittent: passed on rerun".
func TestRerunMarksIntermittentFailure(t *testing.T) {
	if _, err := os.Stat(intermittentFixturePath); err != nil {
		t.Fatalf("fixture: %v", err)
	}
	abs, err := filepath.Abs(intermittentFixturePath)
	if err != nil {
		t.Fatal(err)
	}
	flag := filepath.Join(t.TempDir(), "first-run-flag")
	t.Setenv("MERGESHADOW_FLAG_FILE", flag)
	dir := makefileWithTestTarget(t, "sh '"+abs+"'")

	res := RunGateWithRerun(context.Background(), dir, rerunGateName, 60*time.Second)

	if res.Status != GateIntermittent {
		t.Fatalf("status = %q, want %q (detail %q, output %q)", res.Status, GateIntermittent, res.Detail, res.Output)
	}
	if res.Detail != "intermittent: passed on rerun" {
		t.Errorf("detail = %q, want %q", res.Detail, "intermittent: passed on rerun")
	}
	if !strings.Contains(res.Finding, "race: worker reached the shared counter") {
		t.Errorf("finding %q does not keep the first failing line", res.Finding)
	}
	if !strings.Contains(res.Finding, "expected 1 increment, got 2") {
		t.Errorf("finding %q does not keep the second failing line", res.Finding)
	}
	if strings.Contains(res.Finding, "ok fake-test") {
		t.Errorf("finding %q must not contain the rerun's output", res.Finding)
	}
	if _, err := os.Stat(flag); err != nil {
		t.Errorf("flag file not created by the first run: %v", err)
	}
}

func TestRerunKeepsFailureWhenRerunAlsoFails(t *testing.T) {
	dir := makefileWithTestTarget(t, "echo hard-broken; exit 2")
	res := RunGateWithRerun(context.Background(), dir, rerunGateName, 30*time.Second)
	if res.Status != GateFail {
		t.Fatalf("status = %q, want fail", res.Status)
	}
	if res.Finding != "" {
		t.Errorf("finding %q set on a plain failure", res.Finding)
	}
	if !strings.Contains(res.Detail, "rerun also fail") {
		t.Errorf("detail %q does not note the failed rerun", res.Detail)
	}
}

func TestRerunSkippedWhenFirstRunPasses(t *testing.T) {
	dir := makefileWithTestTarget(t, "echo fine")
	res := RunGateWithRerun(context.Background(), dir, rerunGateName, 30*time.Second)
	if res.Status != GatePass {
		t.Fatalf("status = %q, want pass", res.Status)
	}
}

// TestGateDetailCarriesIntermittentFinding checks that the plan's gate Detail
// keeps the finding of an intermittent gate, and that other gates keep only
// their own Detail.
func TestGateDetailCarriesIntermittentFinding(t *testing.T) {
	ig := GateResult{Status: GateIntermittent, Detail: "intermittent: passed on rerun", Finding: "line one\nline two"}
	got := gateDetail(ig)
	if !strings.Contains(got, "finding: line one | line two") || !strings.Contains(got, "intermittent: passed on rerun") {
		t.Errorf("gateDetail = %q, want the finding and the detail", got)
	}
	plain := GateResult{Status: GateFail, Detail: "exit status 2"}
	if got := gateDetail(plain); got != "exit status 2" {
		t.Errorf("gateDetail(fail) = %q, want the detail unchanged", got)
	}
}
