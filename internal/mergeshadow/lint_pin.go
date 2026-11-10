package mergeshadow

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// LintVersionMismatchCode is the finding code for a golangci-lint binary on
// PATH whose version differs from the version the CI workflow pins in its
// golangci-lint action step. A lint run with a different linter can pass
// locally and fail in CI, or the reverse, so the lint gate fails on it.
const LintVersionMismatchCode = "lint_version_mismatch"

// ciWorkflowPath is the CI workflow read from the trial worktree, relative to
// the worktree root.
const ciWorkflowPath = ".github/workflows/ci.yaml"

// golangciActionPrefix identifies the golangci-lint action step by its uses
// reference, with or without the version suffix.
const golangciActionPrefix = "golangci/golangci-lint-action"

// versionPattern finds the first semantic version in a version string, with or
// without a leading "v".
var versionPattern = regexp.MustCompile(`v?(\d+\.\d+\.\d+)`)

type ciWorkflow struct {
	Jobs map[string]struct {
		Steps []ciStep `yaml:"steps"`
	} `yaml:"jobs"`
}

type ciStep struct {
	Uses string         `yaml:"uses"`
	With map[string]any `yaml:"with"`
}

// PinnedGolangciLintVersion returns the golangci-lint version that a CI
// workflow pins in its golangci-lint action step, normalized without a leading
// "v". It returns "" and no error when the workflow has no such step, so a
// project that does not pin golangci-lint in CI has nothing to compare. A
// workflow that is not valid YAML is an error.
func PinnedGolangciLintVersion(data []byte) (string, error) {
	var wf ciWorkflow
	if err := yaml.Unmarshal(data, &wf); err != nil {
		return "", fmt.Errorf("mergeshadow: parse CI workflow: %w", err)
	}
	jobs := make([]string, 0, len(wf.Jobs))
	for name := range wf.Jobs {
		jobs = append(jobs, name)
	}
	sort.Strings(jobs)
	for _, job := range jobs {
		for _, step := range wf.Jobs[job].Steps {
			if !strings.HasPrefix(step.Uses, golangciActionPrefix) {
				continue
			}
			raw, ok := step.With["version"]
			if !ok {
				return "", nil
			}
			return NormalizeLintVersion(fmt.Sprint(raw)), nil
		}
	}
	return "", nil
}

// ReadPinnedGolangciLintVersion reads the CI workflow under trialDir and
// returns the golangci-lint version it pins. A missing workflow file means no
// pin, so it returns "" and no error.
func ReadPinnedGolangciLintVersion(trialDir string) (string, error) {
	data, err := os.ReadFile(filepath.Join(trialDir, ciWorkflowPath)) // #nosec G304 -- fixed relative path under trialDir
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("mergeshadow: read CI workflow: %w", err)
	}
	return PinnedGolangciLintVersion(data)
}

// NormalizeLintVersion extracts the first semantic version from s and returns
// it without a leading "v". It returns "" when s holds no version.
func NormalizeLintVersion(s string) string {
	m := versionPattern.FindStringSubmatch(s)
	if m == nil {
		return ""
	}
	return m[1]
}

// CheckLintVersionPin compares the pinned CI version with the local
// golangci-lint version. Both are normalized first, so "v2.12.2" and
// "2.12.2" agree. It returns one blocking lint_version_mismatch finding when
// they differ, and none when they match. A local version that cannot be
// determined is a mismatch, because the pin cannot be confirmed.
func CheckLintVersionPin(pinned, local string) []Finding {
	p := NormalizeLintVersion(pinned)
	l := NormalizeLintVersion(local)
	if p == "" || p == l {
		return nil
	}
	found := l
	if found == "" {
		found = "unknown"
	}
	return []Finding{{
		Code:     LintVersionMismatchCode,
		Message:  fmt.Sprintf("golangci-lint %s is on PATH but CI pins golangci-lint %s", found, p),
		Blocking: true,
	}}
}

// applyLintVersionPin checks the lint gate result against the CI pin in
// trialDir. localLine is the one-line local version description recorded in
// the gate Detail, and localVersion is the version it reports. The Detail
// records both versions. A mismatch adds a lint_version_mismatch finding and
// sets the gate status to fail. A pin that cannot be read is recorded in the
// Detail and leaves the status unchanged.
func applyLintVersionPin(res GateResult, trialDir, localLine, localVersion string) GateResult {
	pinned, err := ReadPinnedGolangciLintVersion(trialDir)
	if err != nil {
		res.Detail = joinDetail(res.Detail, joinDetail(localLine, err.Error()))
		return res
	}
	if pinned == "" {
		res.Detail = joinDetail(res.Detail, localLine)
		return res
	}
	res.Detail = joinDetail(res.Detail, joinDetail(localLine,
		"CI pins golangci-lint "+pinned))
	findings := CheckLintVersionPin(pinned, localVersion)
	if len(findings) > 0 {
		res.Status = GateFail
		res.Findings = append(res.Findings, findings...)
	}
	return res
}
