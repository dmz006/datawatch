package mergepipeline

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
)

// The release-bump validator checks, before a merge ships, that the release
// version is consistent everywhere it is written and is the next legal step
// after the newest release tag. It only reads: it never writes a file, a tag
// or a remote. Every check runs, so the result names each one that failed.

// Names of the five checks, in the order the validator runs them.
const (
	checkVersionVariables = "version_variables_equal"
	checkChangelogHeading = "changelog_heading_matches"
	checkOneStepAboveTag  = "one_step_above_last_tag"
	checkTagNotTaken      = "tag_not_taken"
	checkReadmeLine       = "readme_line_matches"
)

// defaultReadmeFile and defaultRemote apply when ValidateInput leaves them
// empty.
const (
	defaultReadmeFile = "README.md"
	defaultRemote     = "origin"
)

var (
	// releaseHeading matches a release heading such as `## v1.2.3 — title`.
	releaseHeading = regexp.MustCompile(`^## v([0-9]+\.[0-9]+\.[0-9]+)(\s|$)`)
	// releaseTag matches a release tag such as v1.2.3.
	releaseTag = regexp.MustCompile(`^v([0-9]+)\.([0-9]+)\.([0-9]+)$`)
	// versionInText finds the first X.Y.Z in a line, with an optional v.
	versionInText = regexp.MustCompile(`v?([0-9]+\.[0-9]+\.[0-9]+)`)
)

const currentReleasePrefix = "Current release:"

// ValidateInput says what the release-bump validator reads. Paths are
// relative to Dir, the repository root; absolute paths and paths containing
// ".." are refused by the checks that read them. Empty ChangelogFile,
// ReadmeFile and Remote take the defaults CHANGELOG.md, README.md and origin.
// VersionFiles has no default: a release with no version files fails the
// version check rather than guessing.
//
// ExcludeTag names a tag the validator must not count as the previous
// release: the tag the run has just made for the version being released.
// Empty means no tag is excluded.
type ValidateInput struct {
	Dir           string
	VersionFiles  []string
	ChangelogFile string
	ReadmeFile    string
	Remote        string
	ExcludeTag    string
}

// ValidateReleaseBump runs all five release-bump checks against the
// repository in in.Dir and reports each one by name. Pass is true only when
// every check passes. It never changes a file, a tag or a remote.
func ValidateReleaseBump(ctx context.Context, in ValidateInput) autonomous.MergeValidatorResult {
	in = in.withDefaults()
	res := autonomous.MergeValidatorResult{CheckedAt: time.Now().UTC()}
	if in.Dir == "" {
		res.Checks = failAllChecks("no project directory is set")
	} else {
		version, vc := checkVersionVariablesEqual(in)
		res.Version = version
		res.Checks = []autonomous.MergeValidatorCheck{
			vc,
			checkChangelogMatches(in, version),
			checkOneStepAbove(ctx, in, version),
			checkTagAvailable(ctx, in, version),
			checkReadmeMatches(in, version),
		}
	}
	res.Pass = true
	for _, c := range res.Checks {
		if !c.Pass {
			res.Pass = false
		}
	}
	return res
}

func (in ValidateInput) withDefaults() ValidateInput {
	if in.ChangelogFile == "" {
		in.ChangelogFile = DefaultChangelogFile
	}
	if in.ReadmeFile == "" {
		in.ReadmeFile = defaultReadmeFile
	}
	if in.Remote == "" {
		in.Remote = defaultRemote
	}
	return in
}

func passCheck(name, detail string) autonomous.MergeValidatorCheck {
	return autonomous.MergeValidatorCheck{Name: name, Pass: true, Detail: detail}
}

func failCheck(name, detail string) autonomous.MergeValidatorCheck {
	return autonomous.MergeValidatorCheck{Name: name, Pass: false, Detail: detail}
}

// failAllChecks returns the five checks, each failed with detail.
func failAllChecks(detail string) []autonomous.MergeValidatorCheck {
	names := []string{checkVersionVariables, checkChangelogHeading, checkOneStepAboveTag, checkTagNotTaken, checkReadmeLine}
	out := make([]autonomous.MergeValidatorCheck, 0, len(names))
	for _, n := range names {
		out = append(out, failCheck(n, detail))
	}
	return out
}

// readRepoFile reads rel, a path relative to dir, refusing paths that leave
// the repository.
func readRepoFile(dir, rel string) ([]byte, error) {
	if rel == "" {
		return nil, errors.New("no file is configured")
	}
	if err := checkDir(rel); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel))) // #nosec G304 -- relative path validated, joined under the repository
	if err != nil {
		return nil, err
	}
	return data, nil
}

// readVersionFile returns the X.Y.Z of the `var Version = "..."` line in rel.
func readVersionFile(dir, rel string) (string, error) {
	data, err := readRepoFile(dir, rel)
	if err != nil {
		return "", err
	}
	m := versionLine.FindSubmatch(data)
	if m == nil {
		return "", fmt.Errorf("no `var Version = \"...\"` line in %s", rel)
	}
	if !semver3.Match(m[1]) {
		return "", fmt.Errorf("%s carries %q, which is not X.Y.Z", rel, string(m[1]))
	}
	return string(m[1]), nil
}

// checkVersionVariablesEqual passes when every version file carries the same
// X.Y.Z. It returns that version, or the first readable one when the files
// disagree, so the other checks have a version to compare.
func checkVersionVariablesEqual(in ValidateInput) (string, autonomous.MergeValidatorCheck) {
	if len(in.VersionFiles) == 0 {
		return "", failCheck(checkVersionVariables, "no version files are configured")
	}
	var first string
	var problems []string
	pairs := make([]string, 0, len(in.VersionFiles))
	differ := false
	for _, f := range in.VersionFiles {
		v, err := readVersionFile(in.Dir, f)
		if err != nil {
			problems = append(problems, err.Error())
			continue
		}
		if first == "" {
			first = v
		}
		if v != first {
			differ = true
		}
		pairs = append(pairs, f+"="+v)
	}
	if differ {
		problems = append(problems, "version files differ: "+strings.Join(pairs, ", "))
	}
	if len(problems) > 0 {
		return first, failCheck(checkVersionVariables, strings.Join(problems, "; "))
	}
	return first, passCheck(checkVersionVariables, fmt.Sprintf("all %d version files carry %s", len(in.VersionFiles), first))
}

// checkChangelogMatches passes when the first release heading in the
// changelog is for version.
func checkChangelogMatches(in ValidateInput, version string) autonomous.MergeValidatorCheck {
	if version == "" {
		return failCheck(checkChangelogHeading, "no version was read to compare with")
	}
	data, err := readRepoFile(in.Dir, in.ChangelogFile)
	if err != nil {
		return failCheck(checkChangelogHeading, "could not read "+in.ChangelogFile+": "+err.Error())
	}
	for _, line := range strings.Split(string(data), "\n") {
		m := releaseHeading.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		if m[1] != version {
			return failCheck(checkChangelogHeading, fmt.Sprintf("the first release heading in %s is v%s, not v%s", in.ChangelogFile, m[1], version))
		}
		return passCheck(checkChangelogHeading, fmt.Sprintf("the first release heading in %s is v%s", in.ChangelogFile, version))
	}
	return failCheck(checkChangelogHeading, "no release heading (## vX.Y.Z) in "+in.ChangelogFile)
}

// checkOneStepAbove passes when version is exactly one patch, minor or major
// step above the newest v-tag in the repository.
func checkOneStepAbove(ctx context.Context, in ValidateInput, version string) autonomous.MergeValidatorCheck {
	if version == "" {
		return failCheck(checkOneStepAboveTag, "no version was read to compare with")
	}
	last, ok, err := newestReleaseTag(ctx, in.Dir, in.ExcludeTag)
	if err != nil {
		return failCheck(checkOneStepAboveTag, "could not list tags: "+err.Error())
	}
	if !ok {
		return failCheck(checkOneStepAboveTag, "no v-tag exists to compare with")
	}
	if err := requireOneStep(last, version); err != nil {
		return failCheck(checkOneStepAboveTag, err.Error())
	}
	return passCheck(checkOneStepAboveTag, fmt.Sprintf("v%s is one step above the newest tag v%s", version, last))
}

// newestReleaseTag returns the highest v-tag of the form vX.Y.Z among the
// repository's local tags, skipping exclude. ok is false when there is none.
func newestReleaseTag(ctx context.Context, dir, exclude string) (string, bool, error) {
	out, err := gitOutput(ctx, dir, "tag", "--list", "v*")
	if err != nil {
		return "", false, err
	}
	best, found := "", false
	var bestNums [3]int
	for _, line := range strings.Split(out, "\n") {
		tag := strings.TrimSpace(line)
		m := releaseTag.FindStringSubmatch(tag)
		if m == nil || tag == exclude {
			continue
		}
		nums, err := tagNumbers(m)
		if err != nil {
			continue
		}
		if !found || lessVersion(bestNums, nums) {
			best, bestNums, found = strings.TrimPrefix(tag, "v"), nums, true
		}
	}
	return best, found, nil
}

// tagNumbers converts the three captured numbers of a release tag.
func tagNumbers(m []string) ([3]int, error) {
	var nums [3]int
	for i := 0; i < 3; i++ {
		v, err := strconv.Atoi(m[i+1])
		if err != nil {
			return nums, err
		}
		nums[i] = v
	}
	return nums, nil
}

// lessVersion reports whether a sorts before b, comparing major, minor, patch.
func lessVersion(a, b [3]int) bool {
	for i := range a {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return false
}

// requireOneStep returns an error unless next is exactly one patch, minor or
// major step above last. Both are X.Y.Z strings.
func requireOneStep(last, next string) error {
	lMaj, lMin, lPat, err := parseReleaseVersion(last)
	if err != nil {
		return err
	}
	nMaj, nMin, nPat, err := parseReleaseVersion(next)
	if err != nil {
		return err
	}
	switch {
	case nMaj == lMaj && nMin == lMin && nPat == lPat+1: // patch step
	case nMaj == lMaj && nMin == lMin+1 && nPat == 0: // minor step
	case nMaj == lMaj+1 && nMin == 0 && nPat == 0: // major step
	default:
		return fmt.Errorf("v%s is not one patch, minor or major step above v%s", next, last)
	}
	return nil
}

// checkTagAvailable passes when the version's tag is not on the remote and
// any local tag of that name points at HEAD.
func checkTagAvailable(ctx context.Context, in ValidateInput, version string) autonomous.MergeValidatorCheck {
	if version == "" {
		return failCheck(checkTagNotTaken, "no version was read to compare with")
	}
	tag := "v" + version
	remoteOut, err := gitOutput(ctx, in.Dir, "ls-remote", "--tags", in.Remote, "refs/tags/"+tag)
	if err != nil {
		return failCheck(checkTagNotTaken, "could not query remote "+in.Remote+": "+err.Error())
	}
	for _, line := range strings.Split(remoteOut, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[1] == "refs/tags/"+tag {
			return failCheck(checkTagNotTaken, fmt.Sprintf("tag %s is already on remote %s", tag, in.Remote))
		}
	}
	localTags, err := gitOutput(ctx, in.Dir, "tag", "--list", tag)
	if err != nil {
		return failCheck(checkTagNotTaken, "could not list local tags: "+err.Error())
	}
	if strings.TrimSpace(localTags) == "" {
		return passCheck(checkTagNotTaken, fmt.Sprintf("tag %s is free on remote %s and not set locally", tag, in.Remote))
	}
	tagCommit, err := gitOutput(ctx, in.Dir, "rev-parse", tag+"^{commit}")
	if err != nil {
		return failCheck(checkTagNotTaken, "could not resolve local tag "+tag+": "+err.Error())
	}
	head, err := gitOutput(ctx, in.Dir, "rev-parse", "HEAD")
	if err != nil {
		return failCheck(checkTagNotTaken, "could not resolve HEAD: "+err.Error())
	}
	if strings.TrimSpace(tagCommit) != strings.TrimSpace(head) {
		return failCheck(checkTagNotTaken, fmt.Sprintf("local tag %s exists and does not point at HEAD", tag))
	}
	return passCheck(checkTagNotTaken, fmt.Sprintf("tag %s is free on remote %s; the local tag points at HEAD", tag, in.Remote))
}

// checkReadmeMatches passes when the README line starting "Current release:"
// names version.
func checkReadmeMatches(in ValidateInput, version string) autonomous.MergeValidatorCheck {
	if version == "" {
		return failCheck(checkReadmeLine, "no version was read to compare with")
	}
	data, err := readRepoFile(in.Dir, in.ReadmeFile)
	if err != nil {
		return failCheck(checkReadmeLine, "could not read "+in.ReadmeFile+": "+err.Error())
	}
	for _, line := range strings.Split(string(data), "\n") {
		rest, ok := releaseLineRest(line)
		if !ok {
			continue
		}
		m := versionInText.FindStringSubmatch(rest)
		if m == nil {
			return failCheck(checkReadmeLine, "the Current release: line in "+in.ReadmeFile+" names no version")
		}
		if m[1] != version {
			return failCheck(checkReadmeLine, fmt.Sprintf("the Current release: line in %s names v%s, not v%s", in.ReadmeFile, m[1], version))
		}
		return passCheck(checkReadmeLine, fmt.Sprintf("the Current release: line in %s names v%s", in.ReadmeFile, version))
	}
	return failCheck(checkReadmeLine, "no Current release: line in "+in.ReadmeFile)
}

// releaseLineRest reports whether line is the README's Current release: line
// and returns the text after the prefix. A leading bold marker or spaces are
// allowed, so the line may read "**Current release: vX.Y.Z (DATE).**".
func releaseLineRest(line string) (string, bool) {
	t := strings.TrimLeft(strings.TrimSpace(line), "*_ \t")
	if !strings.HasPrefix(t, currentReleasePrefix) {
		return "", false
	}
	return strings.TrimPrefix(t, currentReleasePrefix), true
}

// gitOutput runs git in dir with the operator's tmux variables removed and
// returns its output. A non-zero exit is an error that carries the redacted
// tail of the output.
func gitOutput(ctx context.Context, dir string, args ...string) (string, error) {
	m := &Merger{Dir: dir}
	res, err := m.run(ctx, dir, "git", args...)
	if err != nil {
		return "", errors.New(redact(err.Error()))
	}
	if res.Code != 0 {
		return "", fmt.Errorf("git %s exited %d: %s", args[0], res.Code, redact(strings.TrimSpace(tail(res.Out, 300))))
	}
	return res.Out, nil
}
