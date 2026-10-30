// repo_ci_watch_runs.go — git/gh helpers for the repo CI watch poll pass
// (repo_ci_watch_poll.go), plus their injectable dependency seam
// (repoCIWatchDeps) in the same pattern as ciWatchDeps in ci_watch.go:
// the Manager carries one function seam per external call, all guarded
// by mu, each with a nil-safe default, so the pass (and its tests)
// never depend on a working git/gh checkout.

package autonomous

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// repoCIWatchVersionTagPattern matches a real release tag of the form
// v<major>.<minor>.<patch> (an optional -pre/+build suffix, like v1.2.3-rc1
// or v1.2.3+build, is allowed — it still sorts between v1.2.3 and the
// next release under -v:refname). Anything else — a plain "v2", a
// "release-2026" marker, a branch-like name — is not a release for this
// watch and is skipped even if it would sort into the list.
var repoCIWatchVersionTagPattern = regexp.MustCompile(`^v\d+\.\d+\.\d+(?:[-+][0-9A-Za-z.-]+)?$`)

// RepoCIWatchRun is one GitHub Actions run listed at a commit, in the
// shape `gh run list --json ...` emits. It is deliberately separate from
// CIWatchRun (the PRD ci-watch's smaller shape): the poll pass needs
// WorkflowName (security classification), Event (record context), and
// the timestamps (hung classification).
type RepoCIWatchRun struct {
	ID           int64     `json:"databaseId"`
	Name         string    `json:"name"`
	WorkflowName string    `json:"workflowName"`
	Status       string    `json:"status"`
	Conclusion   string    `json:"conclusion"`
	URL          string    `json:"url"`
	Event        string    `json:"event"`
	HeadBranch   string    `json:"headBranch"`
	CreatedAt    time.Time `json:"createdAt"`
	StartedAt    time.Time `json:"startedAt"`
}

// repoCIWatchDeps bundles the git/gh seams behind the repo CI watch
// poll pass. Each field is an injectable function; a nil field falls
// back to the corresponding real default below.
type repoCIWatchDeps struct {
	// fetch runs `git fetch --tags origin` in the repo directory so
	// the local view of origin/main and the release tags is current.
	fetch func(ctx context.Context, repoDir string) error
	// origin returns the origin remote URL (for "owner/repo").
	origin func(ctx context.Context, repoDir string) (string, error)
	// head resolves refs/remotes/origin/main to its commit SHA.
	head func(ctx context.Context, repoDir string) (string, error)
	// tags lists the release tags, newest-first version order
	// (git tag --sort=-v:refname, filtered to v<major>.<minor>.<patch>).
	tags func(ctx context.Context, repoDir string) ([]string, error)
	// peel peels a tag ref (annotated or lightweight) to its commit SHA.
	peel func(ctx context.Context, repoDir, tag string) (string, error)
	// runs lists the GitHub Actions runs at one commit of an
	// owner/repo, in the order gh emits them.
	runs func(ctx context.Context, ownerRepo, sha string) ([]RepoCIWatchRun, error)
}

// repoCIWatchDeps returns the active git/gh dependencies of the repo CI
// watch: the Manager's seam (guarded by mu, nil-safe) with every nil
// field replaced by its real default. Mirrors ciWatchDeps exactly.
func (m *Manager) repoCIWatchDeps() repoCIWatchDeps {
	m.mu.Lock()
	d := repoCIWatchDeps{
		fetch:  m.repoCIFetchFn,
		origin: m.repoCIOriginFn,
		head:   m.repoCIHeadFn,
		tags:   m.repoCITagsFn,
		peel:   m.repoCIPeelFn,
		runs:   m.repoCIRunsFn,
	}
	m.mu.Unlock()
	if d.fetch == nil {
		d.fetch = defaultRepoCIFetch
	}
	if d.origin == nil {
		d.origin = defaultRepoCIOrigin
	}
	if d.head == nil {
		d.head = defaultRepoCIHead
	}
	if d.tags == nil {
		d.tags = defaultRepoCITags
	}
	if d.peel == nil {
		d.peel = defaultRepoCIPeel
	}
	if d.runs == nil {
		d.runs = defaultRepoCIRuns
	}
	return d
}

// defaultRepoCIFetch runs `git fetch --tags origin` in repoDir; the
// caller bounds ctx (the poll pass uses the ciWatchTimeout pattern).
func defaultRepoCIFetch(ctx context.Context, repoDir string) error {
	var out bytes.Buffer
	cmd := exec.CommandContext(ctx, "git", "-C", repoDir, "fetch", "--tags", "origin")
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("repo ci watch: git fetch --tags origin in %s: %w: %s",
			repoDir, err, strings.TrimSpace(out.String()))
	}
	return nil
}

// defaultRepoCIOrigin returns the origin remote URL from repoDir.
func defaultRepoCIOrigin(ctx context.Context, repoDir string) (string, error) {
	out, err := exec.CommandContext(ctx, "git", "-C", repoDir, "remote", "get-url", "origin").Output()
	if err != nil {
		return "", fmt.Errorf("repo ci watch: git remote get-url origin in %s: %w", repoDir, err)
	}
	s := strings.TrimSpace(string(out))
	if s == "" {
		return "", fmt.Errorf("repo ci watch: git remote get-url origin in %s: empty URL", repoDir)
	}
	return s, nil
}

// defaultRepoCIHead returns refs/remotes/origin/main's commit SHA —
// the repo's head — an error if the ref is missing (not fetched).
func defaultRepoCIHead(ctx context.Context, repoDir string) (string, error) {
	out, err := exec.CommandContext(ctx, "git", "-C", repoDir, "rev-parse", "refs/remotes/origin/main").Output()
	if err != nil {
		return "", fmt.Errorf("repo ci watch: resolve refs/remotes/origin/main in %s: %w", repoDir, err)
	}
	sha := strings.TrimSpace(string(out))
	if !isFullSHA(sha) {
		return "", fmt.Errorf("repo ci watch: refs/remotes/origin/main in %s resolved to %q, not a full SHA", repoDir, sha)
	}
	return sha, nil
}

// defaultRepoCITags lists the repo's release tags, newest first:
// `git tag --sort=-v:refname` filtered to
// v<major>.<minor>.<patch>-shaped names only.
func defaultRepoCITags(ctx context.Context, repoDir string) ([]string, error) {
	out, err := exec.CommandContext(ctx, "git", "-C", repoDir, "tag", "--sort=-v:refname").Output()
	if err != nil {
		return nil, fmt.Errorf("repo ci watch: git tag --sort=-v:refname in %s: %w", repoDir, err)
	}
	tags := make([]string, 0, 8)
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		tag := strings.TrimSpace(line)
		if tag != "" && repoCIWatchVersionTagPattern.MatchString(tag) {
			tags = append(tags, tag)
		}
	}
	return tags, nil
}

// defaultRepoCIPeel peels a tag (annotated or lightweight) in repoDir
// to the commit SHA it points at, via `rev-parse --verify
// refs/tags/<tag>^{commit}` — the same peel ci_watch.go relies on for
// PRD ci-watch. An unknown tag errors (the tag was force-deleted since
// the tag list) and the poll pass skips that ref, not the repo.
func defaultRepoCIPeel(ctx context.Context, repoDir, tag string) (string, error) {
	ref := "refs/tags/" + tag + "^{commit}"
	out, err := exec.CommandContext(ctx, "git", "-C", repoDir, "rev-parse", "--verify", ref).Output()
	if err != nil {
		return "", fmt.Errorf("repo ci watch: peel %s in %s: %w", ref, repoDir, err)
	}
	sha := strings.TrimSpace(string(out))
	if !isFullSHA(sha) {
		return "", fmt.Errorf("repo ci watch: %s in %s resolved to %q, not a full SHA", ref, repoDir, sha)
	}
	return sha, nil
}

// isFullSHA reports whether s is a full 40-hex git SHA (rev-parse output
// validation for the default seam implementations; fakes inject exact
// SHAs anyway).
func isFullSHA(s string) bool {
	if len(s) != 40 {
		return false
	}
	for _, r := range s {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')) && !(r >= 'A' && r <= 'F') {
			return false
		}
	}
	return true
}

// repoCIWatchRunListJSON is the exact gh run list --json field set the
// pass asks for — every field RepoCIWatchRun carries.
const repoCIWatchRunListJSON = "databaseId,name,workflowName,status,conclusion,url,event,headBranch,createdAt,startedAt"

// defaultRepoCIRuns lists the GitHub Actions runs at one commit of
// owner/repo: one `gh run list --commit <sha>` per SHA, which is what
// bounds the watch to "a single run list per sha", with `--limit 100`
// (the watch cares about recent runs, not archaeology). ctx comes from
// the poll pass, already bounded by the ciWatchTimeout pattern.
func defaultRepoCIRuns(ctx context.Context, ownerRepo, sha string) ([]RepoCIWatchRun, error) {
	out, err := exec.CommandContext(ctx, "gh", "run", "list",
		"--commit", sha,
		"--repo", ownerRepo,
		"--limit", "100",
		"--json", repoCIWatchRunListJSON,
	).Output()
	if err != nil {
		return nil, fmt.Errorf("repo ci watch: gh run list --commit %s --repo %s: %w", sha, ownerRepo, err)
	}
	return parseRepoCIWatchRuns(out)
}

// parseRepoCIWatchRuns decodes gh's run list JSON into RepoCIWatchRun
// values (an empty/null body is an empty list, not an error — a commit
// with no runs is normal).
func parseRepoCIWatchRuns(out []byte) ([]RepoCIWatchRun, error) {
	var runs []RepoCIWatchRun
	trimmed := bytes.TrimSpace(out)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return []RepoCIWatchRun{}, nil
	}
	if err := json.Unmarshal(trimmed, &runs); err != nil {
		return nil, fmt.Errorf("repo ci watch: decode gh run list output: %w", err)
	}
	if runs == nil {
		runs = []RepoCIWatchRun{}
	}
	return runs, nil
}
