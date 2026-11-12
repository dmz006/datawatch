// Tests for the security-failure advisory lookup (security_failure_advisory.go).
//
// Two fakes stand in for the outside world: a fake `gh` executable planted
// in a t.TempDir() directory and put on PATH (so the production default is
// exercised end to end, including the command line it builds), and the
// nil-seam (setSecurityFailureAdvisoryFn) for the lookup-error contract.
// Identifiers below are invented.

package autonomous

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

const ghsaAdvisoryBody = `{
  "gh_advisory_id": "GHSA-7p2w-4v3x-9q8m",
  "cve_id": "CVE-2031-1111",
  "published_at": "2026-01-05T12:00:00Z",
  "vulnerabilities": [
    {
      "package": { "ecosystem": "Go", "name": "example.com/example-mod" },
      "first_patched_version": "v0.9.5"
    }
  ]
}`

const cveAdvisoryListBody = `[
  {
    "gh_advisory_id": "GHSA-7p2w-4v3x-9q8m",
    "cve_id": "CVE-2031-0042",
    "published_at": "2026-01-05T12:00:00Z",
    "vulnerabilities": [
      { "package": { "ecosystem": "Go", "name": "examplelib" }, "first_patched_version": "" },
      { "package": { "ecosystem": "Go", "name": "examplelib-extra" }, "first_patched_version": "1.4.3" }
    ]
  }
]`

// newFakeGH creates an executable `gh` in a fresh t.TempDir() directory that
// records "$*" to argsFile, prints body on stdout (when non-empty), fails
// with a stderr line when exitCode is non-zero, and exits with exitCode.
// It returns the directory to put on PATH.
func newFakeGH(t *testing.T, argsFile, body string, exitCode int) string {
	t.Helper()
	dir := t.TempDir()
	var script strings.Builder
	script.WriteString("#!/bin/sh\n")
	script.WriteString("printf '%s\\n' \"$*\" > " + argsFile + "\n")
	if exitCode != 0 {
		script.WriteString("printf 'fake gh failure\\n' 1>&2\n")
	}
	if body != "" {
		bodyFile := filepath.Join(dir, "body.json")
		if err := os.WriteFile(bodyFile, []byte(body), 0o644); err != nil {
			t.Fatalf("write body: %v", err)
		}
		// Absolute path: the test replaces PATH with a directory that holds
		// nothing but `gh`, so a PATH lookup of cat inside the script would
		// fail.
		catBin, err := exec.LookPath("cat")
		if err != nil {
			t.Fatalf("resolve cat: %v", err)
		}
		script.WriteString(catBin + " " + bodyFile + "\n")
	}
	script.WriteString("exit " + strconv.Itoa(exitCode) + "\n")
	ghPath := filepath.Join(dir, "gh")
	if err := os.WriteFile(ghPath, []byte(script.String()), 0o755); err != nil {
		t.Fatalf("write fake gh: %v", err)
	}
	return dir
}

func readArgs(t *testing.T, argsFile string) []string {
	t.Helper()
	raw, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("read recorded args: %v", err)
	}
	return strings.Fields(string(raw))
}

func TestSecurityFailureAdvisoryGHSARoute(t *testing.T) {
	argsFile := filepath.Join(t.TempDir(), "args.txt")
	t.Setenv("PATH", newFakeGH(t, argsFile, ghsaAdvisoryBody, 0))

	res := LookupSecurityAdvisory(context.Background(), "GHSA-7p2w-4v3x-9q8m")

	if res.ErrText != "" {
		t.Fatalf("unexpected error text: %q", res.ErrText)
	}
	if res.PublishedDate != "2026-01-05T12:00:00Z" {
		t.Fatalf("published date = %q, want 2026-01-05T12:00:00Z", res.PublishedDate)
	}
	if res.FixedVersion != "v0.9.5" {
		t.Fatalf("fixed version = %q, want v0.9.5", res.FixedVersion)
	}
	args := readArgs(t, argsFile)
	if len(args) != 2 || args[0] != "api" || args[1] != "advisories/GHSA-7p2w-4v3x-9q8m" {
		t.Fatalf("unexpected gh args: %q", args)
	}
}

func TestSecurityFailureAdvisoryCVERoute(t *testing.T) {
	argsFile := filepath.Join(t.TempDir(), "args.txt")
	t.Setenv("PATH", newFakeGH(t, argsFile, cveAdvisoryListBody, 0))

	res := LookupSecurityAdvisory(context.Background(), "CVE-2031-0042")

	if res.ErrText != "" {
		t.Fatalf("unexpected error text: %q", res.ErrText)
	}
	if res.PublishedDate != "2026-01-05T12:00:00Z" {
		t.Fatalf("published date = %q, want 2026-01-05T12:00:00Z", res.PublishedDate)
	}
	// First non-empty patched version of the first advisory in the list.
	if res.FixedVersion != "1.4.3" {
		t.Fatalf("fixed version = %q, want 1.4.3", res.FixedVersion)
	}
	args := readArgs(t, argsFile)
	if len(args) != 2 || args[0] != "api" || args[1] != "advisories?cve_id=CVE-2031-0042" {
		t.Fatalf("unexpected gh args: %q", args)
	}
}

func TestSecurityFailureAdvisoryLookupError(t *testing.T) {
	// Lookup error: the process fails. The caller must never see a failing
	// return — only an unknown result with the error text kept.
	argsFile := filepath.Join(t.TempDir(), "args.txt")
	t.Setenv("PATH", newFakeGH(t, argsFile, "", 3))

	res := LookupSecurityAdvisory(context.Background(), "CVE-2031-0042")

	if res.PublishedDate != "" || res.FixedVersion != "" {
		t.Fatalf("error result must not carry dates/versions: %+v", res)
	}
	if !strings.Contains(res.ErrText, "fake gh failure") {
		t.Fatalf("want error text kept, got %q", res.ErrText)
	}
}

func TestSecurityFailureAdvisoryGHMissing(t *testing.T) {
	t.Setenv("PATH", t.TempDir()) // a PATH with no gh at all

	res := LookupSecurityAdvisory(context.Background(), "CVE-2031-0042")

	if res.PublishedDate != "" || res.FixedVersion != "" {
		t.Fatalf("missing gh must not invent dates/versions: %+v", res)
	}
	if !strings.Contains(res.ErrText, "gh not found") {
		t.Fatalf("want missing-gh error text, got %q", res.ErrText)
	}
}

func TestSecurityFailureAdvisoryEmptyResponse(t *testing.T) {
	argsFile := filepath.Join(t.TempDir(), "args.txt")
	t.Setenv("PATH", newFakeGH(t, argsFile, "", 0))

	res := LookupSecurityAdvisory(context.Background(), "CVE-2031-0042")

	if res.PublishedDate != "" || res.FixedVersion != "" {
		t.Fatalf("empty response must not invent dates/versions: %+v", res)
	}
	if !strings.Contains(res.ErrText, "empty") {
		t.Fatalf("want empty-response error text, got %q", res.ErrText)
	}
}

func TestSecurityFailureAdvisoryNonJSONResponse(t *testing.T) {
	argsFile := filepath.Join(t.TempDir(), "args.txt")
	t.Setenv("PATH", newFakeGH(t, argsFile, "totally not json", 0))

	res := LookupSecurityAdvisory(context.Background(), "GHSA-7p2w-4v3x-9q8m")

	if res.PublishedDate != "" || res.FixedVersion != "" {
		t.Fatalf("non-JSON response must not invent dates/versions: %+v", res)
	}
	if !strings.Contains(res.ErrText, "non-JSON") {
		t.Fatalf("want non-JSON error text, got %q", res.ErrText)
	}
}

func TestSecurityFailureAdvisoryUnroutableID(t *testing.T) {
	t.Setenv("PATH", t.TempDir()) // no gh; the valid id patterns must reject first

	res := LookupSecurityAdvisory(context.Background(), "OSV-2031-0042")

	if res.PublishedDate != "" || res.FixedVersion != "" {
		t.Fatalf("unroutable id must not carry dates/versions: %+v", res)
	}
	if res.ErrText == "" {
		t.Fatal("want an error text for an unroutable id")
	}
}

func TestSecurityFailureAdvisorySeamLookupError(t *testing.T) {
	// The nil-seam: a test-injected function replaces the production
	// implementation; its unknown result (error text kept, nothing
	// invented) must reach the caller unchanged, and the caller's
	// context and identifier must reach the seam unchanged.
	var gotCtx context.Context
	var gotID string
	restore := setSecurityFailureAdvisoryFn(func(ctx context.Context, advisoryID string) SecurityFailureAdvisoryResult {
		gotCtx = ctx
		gotID = advisoryID
		return SecurityFailureAdvisoryResult{ErrText: "simulated network failure"}
	})
	t.Cleanup(restore)

	ctx := context.Background()
	res := LookupSecurityAdvisory(ctx, "CVE-2031-0042")

	if gotCtx != ctx {
		t.Fatal("seam did not receive the caller's context")
	}
	if gotID != "CVE-2031-0042" {
		t.Fatalf("seam received id %q", gotID)
	}
	if res.ErrText != "simulated network failure" {
		t.Fatalf("seam result not propagated: %+v", res)
	}
	if res.PublishedDate != "" || res.FixedVersion != "" {
		t.Fatalf("unknown seam result must not carry dates/versions: %+v", res)
	}
}
