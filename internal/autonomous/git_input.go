// Checks on values that reach a git command line.
package autonomous

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// ErrBadRestoreBranch is returned by a reap restore whose branch value is not a
// branch name. The REST surfaces answer it with a 400.
var ErrBadRestoreBranch = errors.New("not a valid branch name")

// branchCharsRe is the character half of git's branch name rules
// (git check-ref-format): no space, no control character, none of ~ ^ : ? * [ \
// anywhere, and no leading '-' (git would read it as an option), '.' or '/'.
// branchShapeOK is the other half.
var branchCharsRe = regexp.MustCompile(`^[^-./\x00-\x20\x7f~^:?*\[\\][^\x00-\x20\x7f~^:?*\[\\]*$`)

// branchShapeOK applies the rest of git's branch name rules: the forbidden
// sequences and the rules for each '/' separated part.
func branchShapeOK(branch string) bool {
	if branch == "@" || branch == "HEAD" || strings.Contains(branch, "..") || strings.Contains(branch, "@{") ||
		strings.HasSuffix(branch, ".") {
		return false
	}
	for _, part := range strings.Split(branch, "/") {
		if part == "" || strings.HasPrefix(part, ".") || strings.HasSuffix(part, ".lock") {
			return false
		}
	}
	return true
}

// taskGitCommands are the git commands the task-worktree code runs through
// taskGitOut.
var taskGitCommands = map[string]bool{
	"branch": true, "cherry": true, "ls-files": true, "merge-base": true,
	"rev-list": true, "rev-parse": true, "show": true, "status": true,
	"symbolic-ref": true, "update-ref": true, "worktree": true,
}

var errTaskGitArgs = errors.New("git arguments not accepted")

// checkTaskGitArgs accepts one of taskGitCommands, optionally behind the one
// fixed configuration override the removal code uses. No other git command and
// no other option can come before the command.
func checkTaskGitArgs(args []string) error {
	rest := args
	if len(rest) >= 2 && rest[0] == "-c" && rest[1] == showUntrackedOverride {
		rest = rest[2:]
	}
	if len(rest) == 0 || !taskGitCommands[rest[0]] {
		return fmt.Errorf("%w: %q", errTaskGitArgs, args)
	}
	for _, a := range args {
		if strings.ContainsRune(a, 0) {
			return fmt.Errorf("%w: NUL byte", errTaskGitArgs)
		}
	}
	return nil
}
