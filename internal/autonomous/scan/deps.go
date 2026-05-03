// Dependency scanner — reports manifest files and checks lock files for
// known-vulnerable package versions. Full CVE scanning is future work;
// this version provides baseline visibility into dependency posture.

package scan

import (
	"os"
	"path/filepath"
	"strings"
)

// DepsScanner reports dependency manifests and known-vulnerable pinned versions.
type DepsScanner struct{}

// NewDepsScanner returns a new dependency scanner.
func NewDepsScanner() Scanner { return DepsScanner{} }

func (DepsScanner) Name() string { return "deps" }

// knownVulnPkgs are package@version substrings known to carry critical CVEs.
// Operators may extend this list; a future integration with osv.dev or Trivy
// will supersede these static entries.
var knownVulnPkgs = []struct {
	pkg  string
	msg  string
	rule string
}{
	{"lodash@4.17.20", "lodash <4.17.21 — prototype pollution (CVE-2021-23337)", "DEP001"},
	{"lodash@4.17.19", "lodash <4.17.21 — prototype pollution (CVE-2021-23337)", "DEP001"},
	{"lodash@4.17.15", "lodash <4.17.21 — prototype pollution (CVE-2021-23337)", "DEP001"},
	{"node-fetch@2.6.0", "node-fetch <2.6.7 — improper redirect (CVE-2022-0235)", "DEP002"},
	{"minimist@1.2.5", "minimist ≤1.2.5 — prototype pollution (CVE-2021-44906)", "DEP003"},
	{"minimist@1.2.4", "minimist ≤1.2.5 — prototype pollution (CVE-2021-44906)", "DEP003"},
	{"ansi-regex@3.0.0", "ansi-regex <5.0.1 — ReDoS (CVE-2021-3807)", "DEP004"},
	{"glob-parent@3.1.0", "glob-parent <5.1.2 — ReDoS (CVE-2020-28469)", "DEP005"},
}

var manifestFiles = map[string]bool{
	"package.json":      true,
	"package-lock.json": true,
	"yarn.lock":         true,
	"pnpm-lock.yaml":    true,
	"requirements.txt":  true,
	"Pipfile":           true,
	"Pipfile.lock":      true,
	"go.mod":            true,
	"go.sum":            true,
	"Gemfile":           true,
	"Gemfile.lock":      true,
	"Cargo.toml":        true,
	"Cargo.lock":        true,
	"pom.xml":           true,
	"build.gradle":      true,
	"build.gradle.kts":  true,
	"pyproject.toml":    true,
	"poetry.lock":       true,
}

func (DepsScanner) Scan(dir string) ([]Finding, error) {
	var findings []Finding
	seen := map[string]bool{}

	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if skipDirs[filepath.Base(path)] {
				return filepath.SkipDir
			}
			return nil
		}
		base := filepath.Base(path)
		if !manifestFiles[base] {
			return nil
		}
		rel, _ := filepath.Rel(dir, path)
		if !seen[rel] {
			seen[rel] = true
			findings = append(findings, Finding{
				Scanner:  "deps",
				File:     rel,
				Severity: SeverityInfo,
				RuleID:   "DEP000",
				Message:  "dependency manifest: " + base,
			})
		}

		// Scan lock / pinned files for known-vulnerable versions
		isLock := strings.HasSuffix(base, ".lock") ||
			base == "package-lock.json" ||
			base == "pnpm-lock.yaml" ||
			base == "go.sum"
		if !isLock {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		content := string(data)
		for _, v := range knownVulnPkgs {
			if strings.Contains(content, v.pkg) {
				findings = append(findings, Finding{
					Scanner:  "deps",
					File:     rel,
					Severity: SeverityError,
					RuleID:   v.rule,
					Message:  v.msg,
				})
			}
		}
		return nil
	})
	return findings, err
}
