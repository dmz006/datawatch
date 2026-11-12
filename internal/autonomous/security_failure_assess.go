// Pure logic that classifies a failed security-relevant CI run from the
// text of its failed log. The caller already fetched that text with
// `gh run view <run-id> --log-failed`; this file never shells out, never
// touches the network or the filesystem. It extracts what the log states
// and nothing it does not: no dependency name and no version is ever
// guessed.

package autonomous

import (
	"regexp"
	"sort"
	"strings"
)

// securityAssessLogCap caps the log text at its last 64 KiB before
// parsing: the identifiers and fix statements that matter sit at the
// end of a long build log, and the head is noise.
const securityAssessLogCap = 64 * 1024

// SecurityFailureKind classifies a failed security-relevant run.
type SecurityFailureKind string

const (
	// SecurityFailureDependency: the log names an advisory and the
	// dependency the advisory applies to.
	SecurityFailureDependency SecurityFailureKind = "dependency"
	// SecurityFailureCVE: the log names an advisory identifier but no
	// dependency.
	SecurityFailureCVE SecurityFailureKind = "cve"
	// SecurityFailureNotDependency: a secret-scan or code-scanning
	// workflow that failed without naming any advisory, e.g. a
	// detected secret.
	SecurityFailureNotDependency SecurityFailureKind = "not_dependency"
	// SecurityFailureUnknown: the log states nothing this assessor can
	// classify.
	SecurityFailureUnknown SecurityFailureKind = "unknown"
)

// SecurityFailureAssessInput is the whole input to the assessor. The
// workflow name, run identity, head SHA and conclusion are carried for
// context; only Workflow and FailedRunLogText drive the classification.
type SecurityFailureAssessInput struct {
	Workflow         string
	RunID            int64
	RunURL           string
	HeadSHA          string
	Conclusion       string
	FailedRunLogText string
}

// SecurityFailureAssessment is the output of AssessSecurityFailure.
// AdvisoryIDs is sorted and de-duplicated; Dependency and FixedVersion
// are non-empty only when the log states them.
type SecurityFailureAssessment struct {
	Kind         SecurityFailureKind
	Dependency   string
	AdvisoryIDs  []string
	FixedVersion string
}

// Compiled once at package level.
var (
	advisoryIDPat = regexp.MustCompile(`\b(CVE-\d{4}-\d{4,7}|GHSA-[0-9a-z]{4}-[0-9a-z]{4}-[0-9a-z]{4}|GO-\d{4}-\d{4,})\b`)
	// A dependency name stated after an explicit label.
	labelledDependencyPat = regexp.MustCompile(`(?im)^\s*(?:package|module|dependency|image)\s*:\s*([^\s:,]+)`)
	// A full module path (host/owner/repo[/...]) stated on the same
	// line as an advisory identifier.
	fullModulePathPat = regexp.MustCompile(`((?:[a-z0-9][a-z0-9-]*\.)+[a-z0-9-]+/[A-Za-z0-9._-]+/[A-Za-z0-9._-]+(?:/[A-Za-z0-9._-]+)*)`)
	versionTokenPat   = `v?[0-9]+\.[0-9]+[A-Za-z0-9.+_-]*`
	fixedVersionPats  = []*regexp.Regexp{
		regexp.MustCompile(`(?i)\bfix(?:ed)?\s+version\s*[:=]\s*(` + versionTokenPat + `)`),
		regexp.MustCompile(`(?i)\bpatch(?:ed)?\s+version\s*[:=]\s*(` + versionTokenPat + `)`),
		regexp.MustCompile(`(?i)\bupgrad(?:e|ing|ed)\s+(?:to\s+)?(` + versionTokenPat + `)`),
		regexp.MustCompile(`(?i)\bversion\s+(` + versionTokenPat + `)\s+contains\s+the\s+(?:fix|patch)`),
	}
)

// AssessSecurityFailure classifies the input. Empty log is unknown; a
// log with no advisory identifier is not_dependency when the workflow
// is a secret-scan or code-scanning workflow, unknown otherwise; an
// advisory identifier with a named dependency is dependency; one
// without a dependency name is cve with an empty Dependency.
func AssessSecurityFailure(in SecurityFailureAssessInput) SecurityFailureAssessment {
	log := in.FailedRunLogText
	if len(log) > securityAssessLogCap {
		log = log[len(log)-securityAssessLogCap:]
	}

	a := SecurityFailureAssessment{Kind: SecurityFailureUnknown}
	if strings.TrimSpace(log) == "" {
		return a
	}

	ids := advisoryIDPat.FindAllString(log, -1)
	if len(ids) == 0 {
		if securitySweepWorkflow(in.Workflow) {
			a.Kind = SecurityFailureNotDependency
		}
		return a
	}
	a.AdvisoryIDs = sortedUnique(ids)
	a.Dependency = dependencyNameInLog(log)
	a.FixedVersion = fixedVersionInLog(log)
	if a.Dependency != "" {
		a.Kind = SecurityFailureDependency
	} else {
		a.Kind = SecurityFailureCVE
	}
	return a
}

// securitySweepWorkflow reports whether the workflow is one of the
// scanning workflows whose findings are not dependency advisories
// (secret-scan, code-scanning, CodeQL).
func securitySweepWorkflow(workflow string) bool {
	w := strings.ToLower(workflow)
	return strings.Contains(w, "secret-scan") ||
		strings.Contains(w, "code-scanning") ||
		strings.Contains(w, "codeql")
}

func sortedUnique(ids []string) []string {
	seen := make(map[string]bool, len(ids))
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	sort.Strings(out)
	return out
}

// dependencyNameInLog returns the dependency the log states, or ""
// when it names none. It only trusts an explicit label or a full
// module path on the same line as an advisory identifier; it never
// guesses.
func dependencyNameInLog(log string) string {
	if m := labelledDependencyPat.FindStringSubmatch(log); m != nil {
		return m[1]
	}
	for _, line := range strings.Split(log, "\n") {
		if !advisoryIDPat.MatchString(line) {
			continue
		}
		if m := fullModulePathPat.FindStringSubmatch(line); m != nil {
			return m[1]
		}
	}
	return ""
}

// fixedVersionInLog returns the patched version the log states, or ""
// when it states none. A version mentioned without a fix statement is
// not a fixed version.
func fixedVersionInLog(log string) string {
	for _, p := range fixedVersionPats {
		if m := p.FindStringSubmatch(log); m != nil {
			return m[1]
		}
	}
	return ""
}
