package mergeshadow

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/dmz006/datawatch/internal/config"
)

// Value is one value read from the default branch's ref. Found is false
// when the value is not present on the ref; Value is empty then. A missing
// file or a missing line is a finding, not an error: the reader reports
// what the ref actually holds.
type Value struct {
	Value string `json:"value"`
	Found bool   `json:"found"`
}

// Landed is the read-back of what a merge left on the default branch:
// the top changelog heading, the version in every version file the merge
// pipeline settings name, the README line for the release and the
// registry row. Every value is read from the ref through
// `git show <ref>:<path>`; the working tree is never read, so uncommitted
// edits and checkouts of other branches cannot change the result.
type Landed struct {
	// ChangelogHeading is the first "## " heading in the changelog.
	ChangelogHeading Value `json:"changelog_heading"`
	// Versions maps each version file path to the version read from it.
	// The map holds one entry per configured version file, including
	// entries whose value was not found.
	Versions map[string]Value `json:"versions"`
	// ReadmeLine is the README "Current release" line for the release.
	ReadmeLine Value `json:"readme_line"`
	// RegistryRow is the registry table row naming the Automaton.
	RegistryRow Value `json:"registry_row"`
}

// LandedQuery names what to look for on the ref. Project selects the
// project's merge pipeline values; AutomatonID picks the registry row;
// Version picks the README release line (empty matches the first release
// line); Pipeline is the merge pipeline settings that name the version
// files.
type LandedQuery struct {
	Project     string
	AutomatonID string
	Version     string
	Pipeline    config.MergePipelineConfig
}

// DefaultRegistryFile is the registry file the reader looks in when the
// merge pipeline settings name none.
const DefaultRegistryFile = "docs/plans/README.md"

// ReadLanded reads what landed on the default branch's ref for one
// Automaton. repoPath is the repository, ref is the default branch (a
// commit-ish: a branch name, tag or sha). The returned Landed holds one
// Value per named thing, each with its found flag.
//
// The reader never touches the working tree: every file is read through
// `git show <ref>:<path>`. An unresolvable ref is an error; a file that
// is not on the ref, or a line that is not in the file, is not an error
// but a Value with Found false.
func ReadLanded(ctx context.Context, repoPath, ref string, q LandedQuery) (Landed, error) {
	if repoPath == "" {
		return Landed{}, errors.New("mergeshadow: repo path is required")
	}
	if ref == "" || strings.HasPrefix(ref, "-") {
		return Landed{}, errors.New("mergeshadow: a non-empty ref is required")
	}
	if out, err := runGit(ctx, repoPath, "rev-parse", "--verify", "--quiet", ref+"^{commit}"); err != nil {
		return Landed{}, fmt.Errorf("mergeshadow: ref %q is not a commit on this repository: %s", ref, strings.TrimSpace(out))
	}

	out := Landed{Versions: map[string]Value{}}

	changelog, ok, err := refFile(ctx, repoPath, ref, DefaultChangelogFile)
	if err != nil {
		return Landed{}, err
	}
	if ok {
		out.ChangelogHeading = findHeading(changelog)
	}

	for _, vf := range q.Pipeline.EffectiveVersionFiles(q.Project) {
		content, ok, err := refFile(ctx, repoPath, ref, vf)
		if err != nil {
			return Landed{}, err
		}
		out.Versions[vf] = Value{}
		if ok {
			if v, found := findVersion(content); found {
				out.Versions[vf] = Value{Value: v, Found: true}
			}
		}
	}

	readme, ok, err := refFile(ctx, repoPath, ref, DefaultReadmeFile)
	if err != nil {
		return Landed{}, err
	}
	if ok {
		out.ReadmeLine = findReadmeLine(readme, q.Version)
	}

	registryPath := q.Pipeline.EffectivePostShip().EffectiveRegistryFile()
	if registryPath == "" {
		registryPath = DefaultRegistryFile
	}
	registryPath = strings.ReplaceAll(registryPath, "{project}", q.Project)
	registry, ok, err := refFile(ctx, repoPath, ref, registryPath)
	if err != nil {
		return Landed{}, err
	}
	if ok {
		out.RegistryRow = findRegistryRow(registry, q.AutomatonID)
	}
	return out, nil
}

// refFile reads one file from the ref without ever reading the working
// tree. ok is false when the file is not on the ref (a plain finding).
func refFile(ctx context.Context, repoPath, ref, p string) (string, bool, error) {
	if !validRepoPath(p) {
		return "", false, fmt.Errorf("mergeshadow: %q is not a repository-relative file path", p)
	}
	// The ref was verified to resolve above, so a failed ref:path is a
	// path that is not on the ref: a plain finding, not an error.
	if _, err := runGit(ctx, repoPath, "rev-parse", "--verify", ref+":"+p); err != nil {
		return "", false, nil
	}
	content, err := runGit(ctx, repoPath, "show", ref+":"+p)
	if err != nil {
		return "", false, fmt.Errorf("mergeshadow: read %s on %s: %w", p, ref, err)
	}
	return content, true, nil
}

func validRepoPath(p string) bool {
	if p == "" || strings.HasPrefix(p, "/") || strings.HasPrefix(p, "-") {
		return false
	}
	clean := strings.ReplaceAll(p, "\\", "/")
	if clean != p {
		return false
	}
	for _, seg := range strings.Split(clean, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return false
		}
	}
	return true
}

// findHeading returns the first "## " heading, trimmed.
func findHeading(content string) Value {
	for _, line := range strings.Split(normalizeNewlines(content), "\n") {
		if strings.HasPrefix(line, "## ") {
			return Value{Value: strings.TrimRight(line, "\r"), Found: true}
		}
	}
	return Value{}
}

var versionLineRE = regexp.MustCompile(`var\s+Version\s*=\s*"([^"\r\n]+)"`)

// findVersion returns the first `var Version = "X"` value in the file.
func findVersion(content string) (string, bool) {
	m := versionLineRE.FindStringSubmatch(normalizeNewlines(content))
	if m == nil {
		return "", false
	}
	v := strings.TrimSpace(m[1])
	if v == "" {
		return "", false
	}
	return v, true
}

// findReadmeLine returns the first README "Current release" line, the
// one naming version when version is non-empty.
func findReadmeLine(content, version string) Value {
	for _, line := range strings.Split(normalizeNewlines(content), "\n") {
		if !strings.Contains(line, "Current release") {
			continue
		}
		if version != "" && !strings.Contains(line, version) {
			continue
		}
		return Value{Value: strings.TrimRight(line, "\r"), Found: true}
	}
	return Value{}
}

// findRegistryRow returns the first table row naming the Automaton.
func findRegistryRow(content, automatonID string) Value {
	if automatonID == "" {
		return Value{}
	}
	for _, line := range strings.Split(normalizeNewlines(content), "\n") {
		trimmed := strings.TrimRight(line, "\r")
		if strings.HasPrefix(trimmed, "|") && strings.Contains(trimmed, automatonID) {
			return Value{Value: trimmed, Found: true}
		}
	}
	return Value{}
}

func normalizeNewlines(s string) string {
	return strings.ReplaceAll(s, "\r\n", "\n")
}
