// Automaton member-merge proof. MergedMemberProof decides whether one
// member Automaton's branch is merged on the project's default branch and
// returns the proof with it: the branch name, the default branch it was
// compared with, and the commit that proves the merge. The merged state
// reuses the merge-pending comparison (CompareBranchToMain in
// merge_compare.go: patch ids via git cherry plus a commit-subject check),
// and the default branch is resolved per project from git - the symbolic
// ref of the remote HEAD, falling back to the branch currently checked out
// in the base repository - never assumed from a name. An unknown state (no
// repo, no branch, a git error) reports not-merged with a reason, so a
// caller keeps its blocked behaviour. A branch with no commits ahead of the
// default branch is reported not merged too (the comparison calls it empty):
// that state cannot tell a branch that was fast-forwarded in from one that
// never had work, so it stays conservative and keeps today's blocked
// behaviour. Nothing here writes, merges, deletes or pushes.
package autonomous

import (
	"context"
	"fmt"
	"strings"
)

// MemberMergeProof is the answer to the question "is this member
// Automaton's branch merged, and what proves it?".
type MemberMergeProof struct {
	// Merged is true only when read-only git says every patch of the branch
	// is already on the project's default branch.
	Merged bool `json:"merged"`
	// Branch is the Automaton branch that was asked about.
	Branch string `json:"branch"`
	// Base is the default branch the branch was compared with.
	Base string `json:"base,omitempty"`
	// Commit is the commit that proves the merge: the branch tip when the tip
	// is on the default branch, else the default-branch commit that carries
	// the matching commit. Empty when Merged is false.
	Commit string `json:"commit,omitempty"`
	// Reason explains a not-merged or unknown answer; the caller can surface
	// it and keep its blocked behaviour.
	Reason string `json:"reason,omitempty"`
}

// MergedMemberProof decides whether branch is merged on the default branch
// of repo and returns the proof. The default branch is resolved per project
// (remote HEAD symbolic ref, falling back to the HEAD branch of the base
// repository), never assumed. It issues only read-only git commands
// (symbolic-ref, rev-parse, rev-list, cherry, log, diff-tree, merge-base) through
// git, so tests substitute a fake MergeGitRunner. Any unknown state returns
// Merged false with the reason in Reason.
func MergedMemberProof(ctx context.Context, git MergeGitRunner, repo, branch string) MemberMergeProof {
	repo = strings.TrimSpace(repo)
	branch = strings.TrimSpace(branch)
	proof := MemberMergeProof{Branch: branch}

	if repo == "" {
		proof.Reason = "Automaton merge proof: no base repository is configured"
		return proof
	}
	if branch == "" || strings.ContainsAny(branch, "\t\n ") {
		proof.Reason = "Automaton merge proof: no Automaton branch to check"
		return proof
	}

	base, err := defaultBranchForRepo(ctx, git, repo)
	if err != nil {
		proof.Reason = "Automaton merge proof: " + err.Error()
		return proof
	}
	proof.Base = base

	out, err := git.Run(ctx, repo, "rev-parse", "--verify", "refs/heads/"+branch)
	if err != nil {
		proof.Reason = fmt.Sprintf("Automaton merge proof: branch %s is not in the base repository: %v: %s",
			branch, err, strings.TrimSpace(out))
		return proof
	}
	tip := strings.TrimSpace(out)
	if tip == "" {
		proof.Reason = "Automaton merge proof: branch " + branch + " resolved to no commit"
		return proof
	}

	cmp := CompareBranchToMain(ctx, git, repo, branch, base)
	if cmp.State == MergeStateUnknown {
		proof.Reason = cmp.Error
		return proof
	}
	if cmp.State != MergeStateMerged {
		proof.Reason = fmt.Sprintf("Automaton merge proof: branch %s is %s of %s (%d commit(s) ahead, %d patch(es) not on %s)",
			branch, cmp.State, base, cmp.Ahead, cmp.CherryNotOnBase, base)
		return proof
	}

	commit, why := mergedProofCommit(ctx, git, repo, branch, base, tip)
	if commit == "" {
		proof.Reason = why
		return proof
	}
	proof.Merged = true
	proof.Commit = commit
	return proof
}

// mergedProofCommit picks the commit that proves a merge already decided by
// the patch comparison, in order: the branch tip when it is on the default
// branch (merge-base equals the tip), else the default-branch commit that
// carries the patch of the branch tip. The carrying commit is found by
// patch, not by subject: git log --cherry-mark lists the default-branch
// commits whose patch id equals a branch commit, and the first of them whose
// diff is the tip's diff (index and hunk-position lines ignored) wins. It
// returns an empty commit and a reason when no commit can be found.
func mergedProofCommit(ctx context.Context, git MergeGitRunner, repo, branch, base, tip string) (string, string) {
	fail := func(what string, err error, out string) (string, string) {
		return "", fmt.Sprintf("Automaton merge proof: %s: %v: %s", what, err, strings.TrimSpace(out))
	}

	out, err := git.Run(ctx, repo, "merge-base", base, branch)
	if err != nil {
		return fail("merge-base", err, out)
	}
	if strings.TrimSpace(out) == tip {
		return tip, "" // the branch tip is on the default branch itself
	}

	tipPatch, err := git.Run(ctx, repo, "diff-tree", "-p", "--no-commit-id", "-r", tip)
	if err != nil {
		return fail("read branch tip patch", err, tipPatch)
	}
	tipPatch = normalizeProofPatch(tipPatch)
	if tipPatch == "" {
		return "", fmt.Sprintf("Automaton merge proof: the branch tip of %s has no patch to match on %s", branch, base)
	}

	logOut, err := git.Run(ctx, repo, "log", "--left-only", "--cherry-mark", "--format=%m%H", base+"..."+branch)
	if err != nil {
		return fail("list default-branch commits with a matching patch", err, logOut)
	}
	checked := 0
	for _, line := range strings.Split(logOut, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "=") || len(line) == 1 {
			continue // not patch-equivalent to a branch commit
		}
		if checked == maxMergeProofCandidates {
			break
		}
		checked++
		hash := line[1:]
		patch, err := git.Run(ctx, repo, "diff-tree", "-p", "--no-commit-id", "-r", hash)
		if err != nil {
			return fail("read default-branch commit patch", err, patch)
		}
		if normalizeProofPatch(patch) == tipPatch {
			return hash, ""
		}
	}
	return "", fmt.Sprintf("Automaton merge proof: %s is merged by patch comparison, but no commit on %s carries the patch of the branch tip", branch, base)
}

// maxMergeProofCandidates bounds how many patch-equivalent default-branch
// commits the proof search reads a diff for.
const maxMergeProofCandidates = 200

// normalizeProofPatch reduces git diff-tree -p output to what identifies a
// patch across a rebase: the index and hunk-header lines, which carry blob
// ids and line numbers, are dropped.
func normalizeProofPatch(patch string) string {
	var keep []string
	for _, line := range strings.Split(patch, "\n") {
		if strings.HasPrefix(line, "index ") || strings.HasPrefix(line, "@@") {
			continue
		}
		keep = append(keep, line)
	}
	return strings.TrimSpace(strings.Join(keep, "\n"))
}

// defaultBranchForRepo resolves the default branch of repo per project:
// the symbolic ref of the remote HEAD (refs/remotes/origin/HEAD), falling
// back to the branch currently checked out in the base repository. It never
// assumes a name and reports an error when git cannot answer either way.
func defaultBranchForRepo(ctx context.Context, git MergeGitRunner, repo string) (string, error) {
	out, err := git.Run(ctx, repo, "symbolic-ref", "refs/remotes/origin/HEAD")
	if err == nil {
		if name, ok := defaultBranchName(out); ok {
			return name, nil
		}
	}
	out, err2 := git.Run(ctx, repo, "rev-parse", "--abbrev-ref", "HEAD")
	if err2 != nil {
		return "", fmt.Errorf("no default branch for %s: remote HEAD: %v; HEAD branch: %v", repo, err, err2)
	}
	name, ok := defaultBranchName(out)
	if !ok || name == "HEAD" {
		return "", fmt.Errorf("no default branch for %s: the base repository is not on a branch (got %q)", repo, strings.TrimSpace(out))
	}
	return name, nil
}

// defaultBranchName extracts the branch name from a git ref or short-branch
// output line. A remote-tracking ref (refs/remotes/<remote>/<branch>) loses
// the prefix and the remote name, and refs/heads/ is dropped; the rest is
// kept whole, so a slash-named branch such as release/1.0 survives intact.
func defaultBranchName(out string) (string, bool) {
	ref := strings.TrimSpace(out)
	if ref == "" || strings.ContainsAny(ref, "\t\n ") {
		return "", false
	}
	switch {
	case strings.HasPrefix(ref, "refs/remotes/"):
		_, ref, _ = strings.Cut(strings.TrimPrefix(ref, "refs/remotes/"), "/") // drop the remote name
	case strings.HasPrefix(ref, "refs/heads/"):
		ref = strings.TrimPrefix(ref, "refs/heads/")
	}
	if ref == "" {
		return "", false
	}
	return ref, true
}
