// Tests for the security-failure acceptance registry reader
// (security_failure_registry.go). The fixtures are the repository's own
// accepted-risks shape, with invented identifiers: a "cve/CVE-…" entry, an
// "osv/GHSA-…" entry, and the extra fields (severity, reason, …) that a
// real registry carries and this reader must keep only seven of.

package autonomous

import (
	"os"
	"path/filepath"
	"testing"
)

const riskRegistryFixture = `entries:
  - id: cve/CVE-2031-0042
    kind: container
    package: "examplelib"
    version: "1.4.2"
    severity: critical
    images:
      - agent-base
    first_added: "2026-01-01"
    added: "2026-01-02"
    expires: "2026-12-31"
    validated_by: "some-operator"
    reason: "no fix on the stable release yet"
  - id: osv/GHSA-7p2w-4v3x-9q8m
    kind: dependency
    package: "example.com/example-mod"
    version: "v0.9.1"
    first_added: "2026-02-01"
    added: "2026-02-03"
    expires: "2026-11-30"
`

// writeRiskRepo plants body at repo/security/accepted-risks.yml in a fresh t.TempDir
// tree and returns the repo root.
func writeRiskRepo(t *testing.T, body string) string {
	t.Helper()
	repo := t.TempDir()
	secDir := filepath.Join(repo, "security")
	if err := os.MkdirAll(secDir, 0o755); err != nil {
		t.Fatalf("mkdir security dir: %v", err)
	}
	registry := filepath.Join(secDir, "accepted-risks.yml")
	if err := os.WriteFile(registry, []byte(body), 0o644); err != nil {
		t.Fatalf("write registry: %v", err)
	}
	return repo
}

func TestSecurityFailureRegistryFoundCVE(t *testing.T) {
	repo := writeRiskRepo(t, riskRegistryFixture)
	found, entry, err := LookupSecurityFailureRisk(repo, "CVE-2031-0042")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !found {
		t.Fatal("want found=true for cve/ entry, got false")
	}
	want := SecurityFailureRiskEntry{
		ID:         "cve/CVE-2031-0042",
		Kind:       "container",
		Package:    "examplelib",
		Version:    "1.4.2",
		FirstAdded: "2026-01-01",
		Added:      "2026-01-02",
		Expires:    "2026-12-31",
	}
	if entry != want {
		t.Fatalf("entry mismatch:\n got %+v\nwant %+v", entry, want)
	}
}

func TestSecurityFailureRegistryFoundOSV(t *testing.T) {
	repo := writeRiskRepo(t, riskRegistryFixture)
	found, entry, err := LookupSecurityFailureRisk(repo, "GHSA-7p2w-4v3x-9q8m")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !found {
		t.Fatal("want found=true for osv/ entry, got false")
	}
	if entry.ID != "osv/GHSA-7p2w-4v3x-9q8m" || entry.Kind != "dependency" ||
		entry.Package != "example.com/example-mod" || entry.Version != "v0.9.1" {
		t.Fatalf("unexpected entry: %+v", entry)
	}
}

func TestSecurityFailureRegistryNotListed(t *testing.T) {
	repo := writeRiskRepo(t, riskRegistryFixture)
	found, entry, err := LookupSecurityFailureRisk(repo, "CVE-2031-9999")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if found {
		t.Fatalf("want found=false for unlisted id, got %+v", entry)
	}
}

func TestSecurityFailureRegistryExactMatchOnly(t *testing.T) {
	// "cve/CVE-2031-0042" must not match "CVE-2031-004" or "CVE-2031-00421".
	repo := writeRiskRepo(t, riskRegistryFixture)
	for _, id := range []string{"CVE-2031-004", "CVE-2031-00421", "cve/CVE-2031-0042", "cve-2031-0042"} {
		found, _, err := LookupSecurityFailureRisk(repo, id)
		if err != nil {
			t.Fatalf("id %q: unexpected error: %v", id, err)
		}
		if found {
			t.Fatalf("id %q: want no fuzzy match, got found=true", id)
		}
	}
}

func TestSecurityFailureRegistryFileMissing(t *testing.T) {
	repo := t.TempDir() // no security/ directory at all
	found, entry, err := LookupSecurityFailureRisk(repo, "CVE-2031-0042")
	if err == nil {
		t.Fatal("want error for missing registry file, got nil")
	}
	if found {
		t.Fatalf("missing file must not report a found entry: %+v", entry)
	}
}

func TestSecurityFailureRegistryMalformedYAML(t *testing.T) {
	repo := writeRiskRepo(t, "entries: [unclosed\n  - id: cve/CVE-2031-0042\n")
	found, entry, err := LookupSecurityFailureRisk(repo, "CVE-2031-0042")
	if err == nil {
		t.Fatal("want error for malformed yaml, got nil")
	}
	if found {
		t.Fatalf("malformed registry must not report a found entry: %+v", entry)
	}
}

func TestSecurityFailureRegistryWrongEntryType(t *testing.T) {
	repo := writeRiskRepo(t, "entries: just a string, not a list\n")
	found, entry, err := LookupSecurityFailureRisk(repo, "CVE-2031-0042")
	if err == nil {
		t.Fatal("want error for non-list entries, got nil")
	}
	if found {
		t.Fatalf("bad registry must not report a found entry: %+v", entry)
	}
}

func TestSecurityFailureRegistryRequiresRepoPath(t *testing.T) {
	found, entry, err := LookupSecurityFailureRisk("", "CVE-2031-0042")
	if err == nil {
		t.Fatal("want error for empty repo path, got nil")
	}
	if found {
		t.Fatalf("empty repo path must not report a found entry: %+v", entry)
	}
}
