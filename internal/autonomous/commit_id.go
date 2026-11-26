// Commit ids that arrive in a request, and the ones already stored from one.
package autonomous

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// minShortCommitID is the shortest abbreviation accepted: git's default for
// `rev-parse --short`, which merge scripts send.
const minShortCommitID = 7

// commitIDText is what a full or abbreviated commit id looks like.
var commitIDText = regexp.MustCompile(`^[0-9a-f]{7,64}$`)

// commitIDLength is the length of a full commit id in the repository.
func commitIDLength(ctx context.Context, git MergeGitRunner, repo string) (int, error) {
	out, err := git.Run(ctx, repo, "rev-parse", "--show-object-format")
	if err == nil {
		for _, line := range strings.Split(out, "\n") {
			switch strings.TrimSpace(line) {
			case "sha1":
				return 40, nil
			case "sha256":
				return 64, nil
			}
		}
	}
	return 0, fmt.Errorf("the merge repository's hash length cannot be read: %v", err)
}

// resolveCommitID turns id, which must match commitIDText, into a full commit
// id of the repository's hash length. An abbreviation is compared here with
// git's own list of the commits reachable from any ref, so it is never given
// to git, where a ref of the same name would win over the commit.
func resolveCommitID(ctx context.Context, git MergeGitRunner, repo, id string) (string, error) {
	n, err := commitIDLength(ctx, git, repo)
	switch {
	case err != nil:
		return "", err
	case len(id) > n:
		return "", fmt.Errorf("%s has %d characters; a commit id in this repository has %d", id, len(id), n)
	case len(id) == n:
		return id, nil
	}
	out, err := git.Run(ctx, repo, "rev-list", "--all")
	if err != nil {
		return "", fmt.Errorf("the commits cannot be listed to find %s (give the full commit id): %v", id, err)
	}
	found := ""
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if len(line) != n || !strings.HasPrefix(line, id) || line == found {
			continue
		}
		if found != "" {
			return "", fmt.Errorf("more than one commit starts with %s: give the full commit id", id)
		}
		found = line
	}
	if found == "" {
		return "", fmt.Errorf("%w: no commit in the merge repository starts with %s (give the full commit id)", errUnknownCommit, id)
	}
	return found, nil
}

// commitObject returns the commit that the full id names.
func commitObject(ctx context.Context, git MergeGitRunner, repo, id string) (string, bool) {
	out, err := git.Run(ctx, repo, "rev-parse", "--verify", "--quiet", id+"^{commit}")
	sha := strings.TrimSpace(out)
	return sha, err == nil && sha != ""
}

// errUnknownCommit: the repository does not have the commit, which a fetch may
// change.
var errUnknownCommit = errors.New("unknown commit")

// lookupCommit is the full id of the commit id names in the repository.
func lookupCommit(ctx context.Context, git MergeGitRunner, repo, id string) (string, error) {
	full, err := resolveCommitID(ctx, git, repo, id)
	if err != nil {
		return "", err
	}
	got, ok := commitObject(ctx, git, repo, full)
	switch {
	case !ok:
		return "", fmt.Errorf("%w: %s is not a commit in the merge repository", errUnknownCommit, full)
	case got != full:
		return "", fmt.Errorf("%s is not a commit: it names another git object", full)
	}
	return full, nil
}

// fetchedCommit is lookupCommit that, when the commit is not here and there is
// a remote, fetches the remote's default branch once and looks again: the
// commit may have been pushed from another clone. The fetch is the one the
// remote check makes; it takes nothing from the request. Its output is left
// out of the error, because it can hold the remote's address and credential.
func fetchedCommit(ctx context.Context, git MergeGitRunner, repo, remote, id string) (string, error) {
	full, err := lookupCommit(ctx, git, repo, id)
	if !errors.Is(err, errUnknownCommit) || remote == "" {
		return full, err
	}
	base := resolveMergeBase(ctx, git, repo)
	if _, ferr := git.Run(ctx, repo, "fetch", "--quiet", remote, base); ferr != nil {
		return "", fmt.Errorf("%w; fetching %s from remote %s failed (%v), so the remote may have it: record it again once the fetch works", err, base, remote, ferr)
	}
	full, err = lookupCommit(ctx, git, repo, id)
	if errors.Is(err, errUnknownCommit) {
		return "", fmt.Errorf("%w, also after fetching %s from remote %s", err, base, remote)
	}
	return full, err
}

// storedCommitText is what a stored commit may look like when it is not a full
// id: hex, no shorter than git's own minimum abbreviation.
var storedCommitText = regexp.MustCompile(`^[0-9a-f]{4,64}$`)

// storedCommitArg is the only way a stored shipped-record commit becomes a git
// argument. A stored value is text someone sent, and records written without a
// check keep whatever that was. A 40 character id is used as it is. Shorter
// or longer hex is used only when git reads it as an object and not as the name
// of a ref, and then as the full id git gives for it. Anything else, such as
// an option, HEAD or a branch name, is refused. It only reads.
func storedCommitArg(ctx context.Context, git MergeGitRunner, repo, stored string) (string, error) {
	refuse := func(why string) (string, error) {
		return "", fmt.Errorf("stored shipped-record commit %q %s", stored, why)
	}
	id := strings.ToLower(strings.TrimSpace(stored))
	if len(id) == 40 && fullCommitID.MatchString(id) {
		return id, nil
	}
	if !storedCommitText.MatchString(id) {
		return refuse("is not a commit id")
	}
	// Prints the ref's full name for a ref, and nothing for an object id.
	out, err := git.Run(ctx, repo, "rev-parse", "--symbolic-full-name", id)
	switch {
	case err != nil:
		return refuse(fmt.Sprintf("names no commit git can read: %v", err))
	case strings.TrimSpace(out) != "":
		return refuse("is read by git as the name of a ref")
	}
	full, ok := commitObject(ctx, git, repo, id)
	if !ok || !fullCommitID.MatchString(full) {
		// No full id came back. The text is hex and names no ref, so git can
		// read it only as an object id.
		return id, nil
	}
	if !strings.HasPrefix(full, id) {
		return refuse("names another commit")
	}
	return full, nil
}
