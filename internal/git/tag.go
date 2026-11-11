// Release tag helpers for the ship step. Every function runs git by command
// name with an argument slice — no shell string — through Run. Nothing here
// ever passes a force flag or a '+' refspec.
package git

import (
	"context"
	"fmt"
	"strings"
)

// TagState is the result of comparing a tag against the commit to be tagged.
type TagState string

const (
	// TagAbsent: no such tag exists.
	TagAbsent TagState = "absent"
	// TagSameCommit: the tag exists and points at the wanted commit.
	TagSameCommit TagState = "same_commit"
	// TagOtherCommit: the tag exists and points at a different commit.
	TagOtherCommit TagState = "other_commit"
)

// FormatTag fills the {version} placeholder of format.
func FormatTag(format, version string) (string, error) {
	if !strings.Contains(format, "{version}") {
		return "", fmt.Errorf("tag format %q has no {version} placeholder", format)
	}
	if strings.TrimSpace(version) == "" {
		return "", fmt.Errorf("empty release version")
	}
	return strings.ReplaceAll(format, "{version}", version), nil
}

// IsClean reports whether the working tree has no staged, unstaged or
// untracked changes.
func IsClean(ctx context.Context, dir string) (bool, error) {
	out, err := Run(ctx, dir, "status", "--porcelain")
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(out) == "", nil
}

// HeadCommit returns the full commit id HEAD points at.
func HeadCommit(ctx context.Context, dir string) (string, error) {
	out, err := Run(ctx, dir, "rev-parse", "--verify", "HEAD^{commit}")
	return strings.TrimSpace(out), err
}

// CurrentBranch returns the checked-out branch name ("" when detached).
func CurrentBranch(ctx context.Context, dir string) (string, error) {
	out, err := Run(ctx, dir, "symbolic-ref", "--short", "-q", "HEAD")
	if err != nil {
		return "", nil // detached HEAD exits non-zero
	}
	return strings.TrimSpace(out), nil
}

// TagStatus compares tag against commit. It returns the commit the tag
// resolves to ("" when absent).
func TagStatus(ctx context.Context, dir, tag, commit string) (TagState, string, error) {
	if err := checkRefName(tag); err != nil {
		return "", "", err
	}
	if _, err := Run(ctx, dir, "rev-parse", "-q", "--verify", "refs/tags/"+tag); err != nil {
		return TagAbsent, "", nil
	}
	out, err := Run(ctx, dir, "rev-parse", "--verify", "refs/tags/"+tag+"^{commit}")
	if err != nil {
		return "", "", err
	}
	at := strings.TrimSpace(out)
	if at == commit {
		return TagSameCommit, at, nil
	}
	return TagOtherCommit, at, nil
}

// CreateAnnotatedTag creates an annotated tag at HEAD.
func CreateAnnotatedTag(ctx context.Context, dir, tag, message string) error {
	if err := checkRefName(tag); err != nil {
		return err
	}
	_, err := Run(ctx, dir, TagCreateArgs(tag, message)...)
	return err
}

// CreateTagForHost creates a tag at HEAD using the host's signing policy.
// Without SignTags it is an annotated tag (as CreateAnnotatedTag). With
// SignTags it is a signed tag (git tag -s), using SigningKey when set.
func CreateTagForHost(ctx context.Context, dir, tag, message string, host GitHostConfig) error {
	if err := checkRefName(tag); err != nil {
		return err
	}
	_, err := Run(ctx, dir, TagCreateArgsForHost(tag, message, host)...)
	RecordHostOp(HostOpTag, err == nil)
	if err == nil {
		recordHostOperation(ctx, host, HostAuditTag, "", tag)
	}
	return err
}

// TagCreateArgsForHost returns the argument slice (after "git") that
// CreateTagForHost runs. Kept separate so the signing arguments can be
// checked without a GPG key.
func TagCreateArgsForHost(tag, message string, host GitHostConfig) []string {
	if !host.SignTags {
		return TagCreateArgs(tag, message)
	}
	args := []string{"tag", "-s"}
	if host.SigningKey != "" {
		args = append(args, "-u", host.SigningKey)
	}
	return append(args, tag, "-m", message)
}

// PushBranchRef pushes branch to remote (fast-forward only; never forced).
func PushBranchRef(ctx context.Context, dir, remote, branch string) error {
	if err := checkRefName(branch); err != nil {
		return err
	}
	_, err := Run(ctx, dir, ShipPushBranchArgs(remote, branch)...)
	return err
}

// PushTag pushes one tag to remote (never forced).
func PushTag(ctx context.Context, dir, remote, tag string) error {
	if err := checkRefName(tag); err != nil {
		return err
	}
	_, err := Run(ctx, dir, PushTagArgs(remote, tag)...)
	return err
}

// ShipPushBranchArgs, TagCreateArgs and PushTagArgs return the exact argument
// slices (after "git") the functions above run; shadow mode records them.
func ShipPushBranchArgs(remote, branch string) []string {
	return []string{"push", remote, "refs/heads/" + branch + ":refs/heads/" + branch}
}

func TagCreateArgs(tag, message string) []string {
	return []string{"tag", "-a", tag, "-m", message}
}

func PushTagArgs(remote, tag string) []string {
	return []string{"push", remote, "refs/tags/" + tag + ":refs/tags/" + tag}
}

// checkRefName rejects empty names, a leading '-' (option injection), a
// leading '+' (force refspec) and characters git refs cannot contain.
func checkRefName(name string) error {
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("git: empty ref name")
	}
	if strings.HasPrefix(name, "-") || strings.HasPrefix(name, "+") {
		return fmt.Errorf("git: unsafe ref name %q", name)
	}
	if strings.ContainsAny(name, " \t\r\n:~^?*[\\") || strings.Contains(name, "..") {
		return fmt.Errorf("git: invalid ref name %q", name)
	}
	return nil
}
