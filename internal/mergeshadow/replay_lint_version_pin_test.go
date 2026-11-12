package mergeshadow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// lintVersionPinFixture is the CI workflow that pins one golangci-lint version
// in its golangci-lint action step.
const lintVersionPinFixture = "../../testdata/mergeshadow/lint-version-pin/ci.yaml"

// pinnedTrialDir copies the fixture CI workflow into a fresh trial directory at
// the path the lint gate reads, and returns the directory.
func pinnedTrialDir(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(lintVersionPinFixture) // #nosec G304 -- fixed fixture path
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	dir := t.TempDir()
	wf := filepath.Join(dir, ".github", "workflows")
	if err := os.MkdirAll(wf, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wf, "ci.yaml"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

// TestReplayLintVersionPinMismatchFailsLintGate injects a local golangci-lint
// version that differs from the fixture's pin. The lint gate must fail, carry
// one lint_version_mismatch finding, and record both versions in its Detail.
func TestReplayLintVersionPinMismatchFailsLintGate(t *testing.T) {
	dir := pinnedTrialDir(t)
	const localLine = "golangci-lint: golangci-lint has version 2.9.0 built with go1.25"

	res := applyLintVersionPin(GateResult{Name: "lint", Status: GatePass}, dir, localLine, "2.9.0")

	if res.Status != GateFail {
		t.Fatalf("status = %q, want %q", res.Status, GateFail)
	}
	if len(res.Findings) != 1 {
		t.Fatalf("findings %+v, want exactly one", res.Findings)
	}
	f := res.Findings[0]
	if f.Code != "lint_version_mismatch" {
		t.Errorf("code = %q, want lint_version_mismatch", f.Code)
	}
	if !f.Blocking {
		t.Error("finding is not blocking")
	}
	for _, want := range []string{"2.9.0", "2.12.2"} {
		if !strings.Contains(res.Detail, want) {
			t.Errorf("detail %q does not record version %s", res.Detail, want)
		}
	}
}

// TestReplayLintVersionPinMatchingVersionPasses injects the pinned version,
// with and without the leading "v", and expects the gate to stay passed with
// no finding.
func TestReplayLintVersionPinMatchingVersionPasses(t *testing.T) {
	dir := pinnedTrialDir(t)
	for _, local := range []string{"2.12.2", "v2.12.2"} {
		res := applyLintVersionPin(GateResult{Name: "lint", Status: GatePass}, dir,
			"golangci-lint: golangci-lint has version "+local, local)
		if res.Status != GatePass {
			t.Errorf("local %q: status = %q, want pass", local, res.Status)
		}
		if len(res.Findings) != 0 {
			t.Errorf("local %q: findings %+v, want none", local, res.Findings)
		}
	}
}

// TestReplayLintVersionPinUnknownLocalVersionFails checks that a local binary
// whose version cannot be read is a mismatch, since the pin cannot be confirmed.
func TestReplayLintVersionPinUnknownLocalVersionFails(t *testing.T) {
	dir := pinnedTrialDir(t)
	res := applyLintVersionPin(GateResult{Name: "lint", Status: GatePass}, dir,
		"golangci-lint not found on PATH", "")
	if res.Status != GateFail || len(res.Findings) != 1 {
		t.Fatalf("status %q findings %+v, want fail with one finding", res.Status, res.Findings)
	}
	if !strings.Contains(res.Findings[0].Message, "unknown") {
		t.Errorf("message %q does not say the local version is unknown", res.Findings[0].Message)
	}
}

// TestReplayLintVersionPinNoPinLeavesGate checks that a trial worktree without a
// CI workflow, or one with no golangci-lint action, is not compared.
func TestReplayLintVersionPinNoPinLeavesGate(t *testing.T) {
	res := applyLintVersionPin(GateResult{Name: "lint", Status: GatePass}, t.TempDir(),
		"golangci-lint: golangci-lint has version 2.9.0", "2.9.0")
	if res.Status != GatePass || len(res.Findings) != 0 {
		t.Errorf("status %q findings %+v, want pass with none", res.Status, res.Findings)
	}
}

// TestPinnedGolangciLintVersionFromFixture reads the pin from the fixture
// bytes directly and checks that the leading "v" is dropped.
func TestPinnedGolangciLintVersionFromFixture(t *testing.T) {
	data, err := os.ReadFile(lintVersionPinFixture) // #nosec G304 -- fixed fixture path
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	got, err := PinnedGolangciLintVersion(data)
	if err != nil {
		t.Fatalf("PinnedGolangciLintVersion: %v", err)
	}
	if got != "2.12.2" {
		t.Errorf("pinned = %q, want 2.12.2", got)
	}
}

// TestPinnedGolangciLintVersionRejectsBadYAML checks that a malformed workflow
// is an error, not a silent pass.
func TestPinnedGolangciLintVersionRejectsBadYAML(t *testing.T) {
	if _, err := PinnedGolangciLintVersion([]byte("jobs: [unclosed")); err == nil {
		t.Error("malformed workflow accepted")
	}
}

// TestNormalizeLintVersion covers the version forms the gate sees.
func TestNormalizeLintVersion(t *testing.T) {
	cases := map[string]string{
		"v2.12.2": "2.12.2",
		"2.12.2":  "2.12.2",
		"golangci-lint has version 2.12.2 built with go1.25": "2.12.2",
		"no version here": "",
	}
	for in, want := range cases {
		if got := NormalizeLintVersion(in); got != want {
			t.Errorf("NormalizeLintVersion(%q) = %q, want %q", in, got, want)
		}
	}
}
