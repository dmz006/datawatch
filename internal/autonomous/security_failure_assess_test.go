package autonomous

import (
	"strings"
	"testing"
)

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestSecurityFailureAssess(t *testing.T) {
	t.Run("govulncheck GO identifier names the package", func(t *testing.T) {
		log := strings.Join([]string{
			"# go run github.com/julienschmidt/httprouter v1.3.2",
			"scanned main in /home/runner/work/app/app",
			"",
			"main:",
			"  * github.com/example/vulnlib is vulnerable to GO-2024-2407",
			"    Package: github.com/example/vulnlib",
			"    Fix version: 2.1.0",
		}, "\n")
		a := AssessSecurityFailure(SecurityFailureAssessInput{
			Workflow:         "code-scanning.yml",
			RunID:            42,
			RunURL:           "https://github.com/example/app/runs/42",
			HeadSHA:          "abc123",
			Conclusion:       "failure",
			FailedRunLogText: log,
		})
		if a.Kind != SecurityFailureDependency {
			t.Fatalf("kind = %q, want dependency", a.Kind)
		}
		if a.Dependency != "github.com/example/vulnlib" {
			t.Errorf("Dependency = %q, want github.com/example/vulnlib", a.Dependency)
		}
		if !sameStrings(a.AdvisoryIDs, []string{"GO-2024-2407"}) {
			t.Errorf("AdvisoryIDs = %v, want [GO-2024-2407]", a.AdvisoryIDs)
		}
		if a.FixedVersion != "2.1.0" {
			t.Errorf("FixedVersion = %q, want 2.1.0", a.FixedVersion)
		}
	})

	t.Run("dependency-review GHSA identifier names the module", func(t *testing.T) {
		log := strings.Join([]string{
			"##[error]github.com/example/dep 1.0.2 is vulnerable to GHSA-8q6c-p444-9c7h. Upgrade to 1.0.3 to fix.",
		}, "\n")
		a := AssessSecurityFailure(SecurityFailureAssessInput{Workflow: "dependency-review.yml", FailedRunLogText: log})
		if a.Kind != SecurityFailureDependency {
			t.Fatalf("kind = %q, want dependency", a.Kind)
		}
		if a.Dependency != "github.com/example/dep" {
			t.Errorf("Dependency = %q, want github.com/example/dep", a.Dependency)
		}
		if !sameStrings(a.AdvisoryIDs, []string{"GHSA-8q6c-p444-9c7h"}) {
			t.Errorf("AdvisoryIDs = %v, want [GHSA-8q6c-p444-9c7h]", a.AdvisoryIDs)
		}
		if a.FixedVersion != "1.0.3" {
			t.Errorf("FixedVersion = %q, want 1.0.3", a.FixedVersion)
		}
	})

	t.Run("scanner CVE identifier names the package", func(t *testing.T) {
		log := strings.Join([]string{
			"2026-11-01 TRIVY found 1 vulnerability",
			"Package: libcrypto3",
			"CVE-2023-0286  HIGH  OpenSSL certificate validation issue",
		}, "\n")
		a := AssessSecurityFailure(SecurityFailureAssessInput{Workflow: "security-scan.yml", FailedRunLogText: log})
		if a.Kind != SecurityFailureDependency {
			t.Fatalf("kind = %q, want dependency", a.Kind)
		}
		if a.Dependency != "libcrypto3" {
			t.Errorf("Dependency = %q, want libcrypto3", a.Dependency)
		}
		if !sameStrings(a.AdvisoryIDs, []string{"CVE-2023-0286"}) {
			t.Errorf("AdvisoryIDs = %v, want [CVE-2023-0286]", a.AdvisoryIDs)
		}
		if a.FixedVersion != "" {
			t.Errorf("FixedVersion = %q, want empty (the log states no patched version)", a.FixedVersion)
		}
	})

	t.Run("secret-scan log with no advisory identifier", func(t *testing.T) {
		log := strings.Join([]string{
			"30 secret(s) found",
			"AKIA[REDACTED] detected at src/config.yaml:12",
			"The secret-scan job failed. Review the findings above.",
		}, "\n")
		a := AssessSecurityFailure(SecurityFailureAssessInput{
			Workflow:         "secret-scan.yml",
			FailedRunLogText: log,
		})
		if a.Kind != SecurityFailureNotDependency {
			t.Fatalf("kind = %q, want not_dependency", a.Kind)
		}
		if a.Dependency != "" || a.AdvisoryIDs != nil || a.FixedVersion != "" {
			t.Errorf("assessment = %+v, want no dependency, advisories or version", a)
		}
	})

	t.Run("non-scan workflow with no advisory identifier stays unknown", func(t *testing.T) {
		log := "##[error]Process completed with exit code 1."
		a := AssessSecurityFailure(SecurityFailureAssessInput{
			Workflow:         "ci.yml",
			FailedRunLogText: log,
		})
		if a.Kind != SecurityFailureUnknown {
			t.Fatalf("kind = %q, want unknown", a.Kind)
		}
	})

	t.Run("empty log", func(t *testing.T) {
		a := AssessSecurityFailure(SecurityFailureAssessInput{
			Workflow:         "secret-scan.yml",
			FailedRunLogText: "",
		})
		if a.Kind != SecurityFailureUnknown {
			t.Fatalf("kind = %q, want unknown", a.Kind)
		}
		a = AssessSecurityFailure(SecurityFailureAssessInput{FailedRunLogText: "   \n\t  "})
		if a.Kind != SecurityFailureUnknown {
			t.Fatalf("blank-log kind = %q, want unknown", a.Kind)
		}
	})

	t.Run("duplicates collapsed and sorted", func(t *testing.T) {
		log := strings.Join([]string{
			"main:",
			"  * github.com/example/a is vulnerable to GO-2025-1111 and GHSA-8q6c-p444-9c7h",
			"  * github.com/example/b is vulnerable to GHSA-8q6c-p444-9c7h",
			"  * CVE-2023-0286 reported twice: CVE-2023-0286",
			"  * also GHSA-8q6c-p444-9c7h and GO-2025-1111",
		}, "\n")
		a := AssessSecurityFailure(SecurityFailureAssessInput{Workflow: "code-scanning.yml", FailedRunLogText: log})
		want := []string{"CVE-2023-0286", "GHSA-8q6c-p444-9c7h", "GO-2025-1111"}
		if len(a.AdvisoryIDs) != len(want) {
			t.Fatalf("AdvisoryIDs = %v, want %v", a.AdvisoryIDs, want)
		}
		for i := range want {
			if a.AdvisoryIDs[i] != want[i] {
				t.Fatalf("AdvisoryIDs = %v, want %v", a.AdvisoryIDs, want)
			}
		}
	})

	t.Run("advisory identifier with no dependency name", func(t *testing.T) {
		log := strings.Join([]string{
			"Scan completed with findings.",
			"Vulnerability GHSA-8q6c-p444-9c7h detected in the workspace.",
			"No component inventory was attached to this run.",
		}, "\n")
		a := AssessSecurityFailure(SecurityFailureAssessInput{Workflow: "security-scan.yml", FailedRunLogText: log})
		if a.Kind != SecurityFailureCVE {
			t.Fatalf("kind = %q, want cve", a.Kind)
		}
		if a.Dependency != "" {
			t.Errorf("Dependency = %q, want empty", a.Dependency)
		}
		if !sameStrings(a.AdvisoryIDs, []string{"GHSA-8q6c-p444-9c7h"}) {
			t.Errorf("AdvisoryIDs = %v, want [GHSA-8q6c-p444-9c7h]", a.AdvisoryIDs)
		}
		if a.FixedVersion != "" {
			t.Errorf("FixedVersion = %q, want empty", a.FixedVersion)
		}
	})

	t.Run("log is capped to its last 64 KiB", func(t *testing.T) {
		tail := "main:\n  * github.com/example/vulnlib is vulnerable to GO-2025-2222\n    Fix version: 3.0.1"
		// The identifier at the tail survives the cap.
		longTail := strings.Repeat("noise line, no identifiers here\n", 10000) + tail
		a := AssessSecurityFailure(SecurityFailureAssessInput{Workflow: "code-scanning.yml", FailedRunLogText: longTail})
		if a.Kind != SecurityFailureDependency || a.Dependency != "github.com/example/vulnlib" {
			t.Fatalf("tail log: kind = %q, dependency = %q, want dependency github.com/example/vulnlib", a.Kind, a.Dependency)
		}
		if !sameStrings(a.AdvisoryIDs, []string{"GO-2025-2222"}) {
			t.Errorf("tail log AdvisoryIDs = %v, want [GO-2025-2222]", a.AdvisoryIDs)
		}
		if a.FixedVersion != "3.0.1" {
			t.Errorf("tail log FixedVersion = %q, want 3.0.1", a.FixedVersion)
		}

		// An identifier earlier than the cap is cut away.
		head := "Vulnerability GHSA-1111-2222-3333 detected.\n"
		onlyHead := head + strings.Repeat("padding line, no identifiers\n", 10000)
		b := AssessSecurityFailure(SecurityFailureAssessInput{Workflow: "code-scanning.yml", FailedRunLogText: onlyHead})
		if b.Kind != SecurityFailureNotDependency {
			t.Fatalf("head log kind = %q, want not_dependency (identifier cut away by the cap)", b.Kind)
		}
	})
}
