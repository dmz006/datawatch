// The security-failure acceptance registry: read access to a repository's
// security/accepted-risks.yml, keeping only the judgment-relevant fields of
// each entry (id, kind, package, version, first_added, added, expires) and
// discarding the rest (severity, images, impact, rationale prose) that the
// shared registry carries.
//
// A failed run's advisory identifiers (the CVE-…/GHSA-… forms produced by the
// security_failure assessment) are matched against the registry strictly:
// an entry's id is written with a kind prefix ("cve/CVE-2023-45853",
// "osv/GHSA-aaaa-bbbb-cccc") and the identifier part after that prefix must
// equal the advisory identifier exactly — no substring, no fuzzy, no
// cross-family matching.
//
// The registry path is always joined to the repository root the caller passes
// in; an empty repository path is an error rather than a read of the process's
// working directory, so this file never reaches outside the caller's tree.
// A missing, unreadable, or malformed registry is reported to the caller as
// an error; it is never reported as a found entry and never panics.

package autonomous

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// securityFailureRegistryName is the accepted-risks registry file name,
// under security/ at the repository root.
const securityFailureRegistryName = "accepted-risks.yml"

// SecurityFailureRiskEntry is one accepted-risk registry entry, reduced to
// the fields a caller needs for an acceptance judgment. Unknown fields in the
// file are ignored on decode, per the shared registry schema's own rule that
// a consumer keeps only what it needs.
type SecurityFailureRiskEntry struct {
	ID         string `yaml:"id"`
	Kind       string `yaml:"kind"`
	Package    string `yaml:"package"`
	Version    string `yaml:"version"`
	FirstAdded string `yaml:"first_added"`
	Added      string `yaml:"added"`
	Expires    string `yaml:"expires"`
}

// securityFailureRegistryFile is the registry's on-disk shape: a top-level
// map whose "entries" key holds the list. A file that is itself a bare
// list also decodes (handled by loadSecurityFailureRegistry).
type securityFailureRegistryFile struct {
	Entries []SecurityFailureRiskEntry `yaml:"entries"`
}

// LookupSecurityFailureRisk reports whether the advisory identifier
// (e.g. "CVE-2031-0042" or "GHSA-aaaa-bbbb-cccc") is recorded in the
// accepted-risks registry under repoPath/security/accepted-risks.yml.
// found=false with a non-nil error means the registry could not be
// consulted (missing, unreadable, or malformed); found=false with a nil
// error means the registry was consulted and the identifier is not in it.
func LookupSecurityFailureRisk(repoPath, advisoryID string) (bool, SecurityFailureRiskEntry, error) {
	entries, err := loadSecurityFailureRegistry(repoPath)
	if err != nil {
		return false, SecurityFailureRiskEntry{}, err
	}
	want := strings.TrimSpace(advisoryID)
	for _, entry := range entries {
		if securityFailureRiskIdentifier(entry.ID) == want {
			return true, entry, nil
		}
	}
	return false, SecurityFailureRiskEntry{}, nil
}

// loadSecurityFailureRegistry reads and decodes the registry under repoPath.
// An empty repoPath is refused up front so that a join can never fall back to
// a working-directory-relative read.
func loadSecurityFailureRegistry(repoPath string) ([]SecurityFailureRiskEntry, error) {
	if strings.TrimSpace(repoPath) == "" {
		return nil, errors.New("registry: repository path is required (the working directory is never read)")
	}
	raw, err := os.ReadFile(filepath.Join(repoPath, "security", securityFailureRegistryName))
	if err != nil {
		return nil, err
	}
	var file securityFailureRegistryFile
	if err := yaml.Unmarshal(raw, &file); err != nil {
		// Either the yaml is malformed or the top level is a bare list.
		var listing []SecurityFailureRiskEntry
		if lerr := yaml.Unmarshal(raw, &listing); lerr != nil {
			return nil, lerr
		}
		return listing, nil
	}
	return file.Entries, nil
}

// securityFailureRiskIdentifier is the part of a registry entry id that
// identifies the vulnerability: entries are written with a kind prefix —
// "cve/CVE-…", "osv/GHSA-…" — and the identifier is whatever follows that
// prefix. An entry id with no prefix is its own identifier.
func securityFailureRiskIdentifier(entryID string) string {
	id := strings.TrimSpace(entryID)
	if _, after, found := strings.Cut(id, "/"); found {
		return after
	}
	return id
}
