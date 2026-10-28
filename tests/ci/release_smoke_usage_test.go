package ci

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// usageSmokeScript reads scripts/release-smoke.sh from the repository root.
func usageSmokeScript(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(pwaRepoRoot(t), "scripts", "release-smoke.sh"))
	if err != nil {
		t.Fatalf("read release-smoke.sh: %v", err)
	}
	return string(b)
}

// usageSection returns the text of the usage calculator section, from its
// header up to the Summary header.
func usageSection(t *testing.T, script string) string {
	t.Helper()
	start := strings.Index(script, "H \"85. Usage calculator")
	end := strings.Index(script, "H \"Summary\"")
	if start < 0 || end < start {
		t.Fatalf("usage calculator section not found (start=%d end=%d)", start, end)
	}
	return script[start:end]
}

// TestReleaseSmokeParses fails when the script no longer parses, which is what
// an unclosed if chain in an earlier section does to every later section.
func TestReleaseSmokeParses(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash not on PATH")
	}
	cmd := exec.Command(bash, "-n", filepath.Join(pwaRepoRoot(t), "scripts", "release-smoke.sh"))
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("bash -n release-smoke.sh: %v\n%s", err, out)
	}
}

// TestUsageSectionCoversEachSurface checks that the section exercises every
// surface the spec lists, so a dropped check shows up here and not in a release.
func TestUsageSectionCoversEachSurface(t *testing.T) {
	sec := usageSection(t, usageSmokeScript(t))
	for _, want := range []string{
		"/api/usage/advice",
		"/api/usage/readings",
		"/api/usage/extra-credit",
		"/api/config",
		"/api/test/message",
		"variant=bogus",
		"policy=bogus",
		`\"source\":\"pasted\"`,
		"usage.calculator.variant",
		"by_lanes",
		"$DW_CLI usage advice",
		"not just grepped",
	} {
		if !strings.Contains(sec, want) {
			t.Errorf("usage section does not mention %q", want)
		}
	}
	if !regexp.MustCompile(`\[\[ "\$UX_PS" == "501"`).MatchString(sec) {
		t.Error("usage section does not assert the pasted source answers 501")
	}
	if !strings.Contains(sec, `"text":"usage advice"`) {
		t.Error("usage section does not send 'usage advice' through the test message route")
	}
}

// TestUsageSectionRegistersExactCleanup checks that readings are cleaned up by
// the exact ids the POST returned, the extra credit and variant are restored to
// exact values, and the orphan sweep did not grow a usage pattern.
func TestUsageSectionRegistersExactCleanup(t *testing.T) {
	script := usageSmokeScript(t)
	sec := usageSection(t, script)

	for _, want := range []string{
		`add_cleanup usage "$UX_ID1"`,
		`add_cleanup usage "$UX_ID2"`,
		`add_cleanup usage-credit 0`,
		`add_cleanup usage-variant "$UX_VRESTORE"`,
	} {
		if !strings.Contains(sec, want) {
			t.Errorf("usage section is missing %q", want)
		}
	}

	if !strings.Contains(script, `usage)           UXQ=`) || !strings.Contains(script, `/api/usage/readings?id=$UXQ`) {
		t.Error("cleanup_all has no usage kind that deletes a reading by exact id")
	}
	for _, kind := range []string{"usage-credit)", "usage-variant)"} {
		if !strings.Contains(script, kind) {
			t.Errorf("cleanup_all has no %s case", kind)
		}
	}

	sweepStart := strings.Index(script, "race-condition orphan sweep")
	sweepEnd := strings.Index(script, "if [[ -n \"$NEW_ORPHANS\" ]]")
	if sweepStart >= 0 && sweepEnd > sweepStart && strings.Contains(strings.ToLower(script[sweepStart:sweepEnd]), "usage") {
		t.Error("orphan sweep mentions usage; it must stay limited to autonomous:* and smoke-*")
	}
}

// TestRestartMatrixHonoursSmokeOnly checks that the restart matrix section is
// gated on SECTION_SKIP before it can start daemons, so SMOKE_ONLY=82 does not
// run the long matrix.
func TestRestartMatrixHonoursSmokeOnly(t *testing.T) {
	script := usageSmokeScript(t)
	start := strings.Index(script, "RM_MISSING=\"\"")
	if start < 0 {
		t.Fatal("restart matrix block not found")
	}
	block := script[start:]
	gate := strings.Index(block, `if [[ "$SECTION_SKIP" == "1" ]]; then`)
	matrix := strings.Index(block, `"${SMOKE_RESTART_MATRIX:-1}"`)
	if gate < 0 || matrix < 0 || gate > matrix {
		t.Fatalf("restart matrix is not gated on SECTION_SKIP before the matrix switch (gate=%d matrix=%d)", gate, matrix)
	}
}

// TestSmokeSectionCountDocMatches keeps the section count in docs/testing.md in
// step with the number of top-level numbered section headers in the script.
func TestSmokeSectionCountDocMatches(t *testing.T) {
	script := usageSmokeScript(t)
	n := len(regexp.MustCompile(`(?m)^H "[0-9]+\. `).FindAllString(script, -1))

	doc, err := os.ReadFile(filepath.Join(pwaRepoRoot(t), "docs", "testing.md"))
	if err != nil {
		t.Fatalf("read docs/testing.md: %v", err)
	}
	m := regexp.MustCompile(`carries (\d+) top-level numbered section headers`).FindStringSubmatch(string(doc))
	if m == nil {
		t.Fatal("docs/testing.md does not state the top-level numbered section count")
	}
	got, _ := strconv.Atoi(m[1])
	if got != n {
		t.Errorf("docs/testing.md says %d top-level numbered sections, script has %d", got, n)
	}
}
