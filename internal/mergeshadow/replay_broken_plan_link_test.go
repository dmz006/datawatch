package mergeshadow

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// brokenPlanLinkFixtureDir holds the README whose registry row links to a
// dated plan that does not exist.
const brokenPlanLinkFixtureDir = "../../testdata/mergeshadow/broken-plan-link"

// brokenPlanLinkTarget is the missing plan the fixture README links to.
const brokenPlanLinkTarget = "2026-01-01-missing-plan.md"

// brokenPlanLinkGateTimeout bounds the link-check gate run.
const brokenPlanLinkGateTimeout = 60 * time.Second

// linkCheckRepoFiles is the minimal set of files the link-check gate needs in
// the temporary repository: the Makefile target, the existing checker script
// (copied unchanged), and the helper module that script imports from its own
// scripts/ directory.
var linkCheckRepoFiles = []string{
	"scripts/check-plans-links.sh",
	"scripts/plans_links.py",
}

// linkCheckMakefile exposes the checker as a make target, the way the project
// Makefile's check-plans-links target does, so it runs through the gates task.
const linkCheckMakefile = "check-plans-links:\n" +
	"\tbash scripts/check-plans-links.sh --quiet\n"

// hasCountLine reports whether one line of output is exactly want, which is
// how the checker reports its broken-target count.
func hasCountLine(output, want string) bool {
	for _, line := range strings.Split(output, "\n") {
		if strings.TrimSpace(line) == want {
			return true
		}
	}
	return false
}

// copyRepoFile copies the file at rel under the source checkout into dst.
func copyRepoFile(t *testing.T, rel, dst string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", rel)) // #nosec G304 -- fixed repository paths
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o750); err != nil {
		t.Fatalf("mkdir for %s: %v", dst, err)
	}
	if err := os.WriteFile(dst, data, 0o600); err != nil {
		t.Fatalf("write %s: %v", dst, err)
	}
}

// buildLinkCheckRepo creates a temporary repository holding the broken-link
// README, the link-check Makefile and the copied checker scripts. When
// withTarget is true the missing plan is created too, so the link resolves.
func buildLinkCheckRepo(t *testing.T, withTarget bool) string {
	t.Helper()
	root := t.TempDir()

	readme, err := os.ReadFile(filepath.Join(brokenPlanLinkFixtureDir, "docs", "plans", "README.md")) // #nosec G304 -- fixed fixture path
	if err != nil {
		t.Fatalf("read fixture README: %v", err)
	}
	plansDir := filepath.Join(root, "docs", "plans")
	if err := os.MkdirAll(plansDir, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(plansDir, "README.md"), readme, 0o600); err != nil {
		t.Fatal(err)
	}
	if withTarget {
		if err := os.WriteFile(filepath.Join(plansDir, brokenPlanLinkTarget), []byte("# Missing plan\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "Makefile"), []byte(linkCheckMakefile), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, rel := range linkCheckRepoFiles {
		copyRepoFile(t, rel, filepath.Join(root, rel))
	}
	return root
}

// TestReplayBrokenPlanLinkFailsGate runs the plans link check through the gate
// runner against a repository whose plan README links to a missing file. The
// gate must fail and its output must report the broken target.
//
// The checker prints only a count of broken targets, never their names, so the
// output is asserted to carry that count. A control run with the target
// present must pass, which shows the failure comes from the broken link.
func TestReplayBrokenPlanLinkFailsGate(t *testing.T) {
	if _, err := os.Stat(filepath.Join(brokenPlanLinkFixtureDir, "docs", "plans", "README.md")); err != nil {
		t.Fatalf("fixture missing: %v", err)
	}

	root := buildLinkCheckRepo(t, false)
	res := RunGate(context.Background(), root, "check-plans-links", brokenPlanLinkGateTimeout)
	if res.Status != GateFail {
		t.Fatalf("status = %q, want %q (detail %q, output %q)", res.Status, GateFail, res.Detail, res.Output)
	}
	if !hasCountLine(res.Output, "1") {
		t.Errorf("output %q does not report one broken target", res.Output)
	}

	control := buildLinkCheckRepo(t, true)
	ok := RunGate(context.Background(), control, "check-plans-links", brokenPlanLinkGateTimeout)
	if ok.Status != GatePass {
		t.Fatalf("control status = %q, want %q (output %q)", ok.Status, GatePass, ok.Output)
	}
}
