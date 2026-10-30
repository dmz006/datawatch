package autonomous

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// memberProofMergeGit is a fake MergeGitRunner for merged-member tests: it
// answers each git invocation from a script keyed on its full argument
// list, records every call so tests can assert what was asked, and fails
// loudly on an unscripted command.
type memberProofMergeGit struct {
	t      *testing.T
	script map[string]scriptStep
	seen   [][]string
}

type scriptStep struct {
	out string
	err error
}

func newMemberProofMergeGit(t *testing.T, m map[string]scriptStep) *memberProofMergeGit {
	t.Helper()
	return &memberProofMergeGit{t: t, script: m}
}

func (g *memberProofMergeGit) Run(_ context.Context, _ string, args ...string) (string, error) {
	key := strings.Join(args, "\x1f")
	g.seen = append(g.seen, append([]string(nil), args...))
	step, ok := g.script[key]
	if !ok {
		g.t.Errorf("scripted git: no answer scripted for %q", args)
		return "", nil
	}
	return step.out, step.err
}

// saw reports whether the scripted runner was asked for exactly args.
func (g *memberProofMergeGit) saw(args ...string) bool {
	for _, c := range g.seen {
		if len(c) != len(args) {
			continue
		}
		match := true
		for i := range args {
			if c[i] != args[i] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

const (
	proofTipSHA   = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"  // the Automaton branch tip
	proofBaseSHA  = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"  // a default-branch commit
	proofOtherSHA = "cccccccccccccccccccccccccccccccccccccccc"  // another default-branch commit
)

// mergedMemberScript answers a merged flow against default branch base and
// branch "automaton/x": two commits, both patches on base, matching
// subjects, plus the proof-commit resolution (merge-base = tip in the
// fast-forward variant).
func mergedMemberScript(base, branch string, tipIsAncestor bool) map[string]scriptStep {
	m := map[string]scriptStep{
		joinArgs("symbolic-ref", "refs/remotes/origin/HEAD"):        {out: "refs/remotes/origin/" + base + "\n"},
		joinArgs("rev-parse", "--verify", "refs/heads/"+branch):     {out: proofTipSHA + "\n"},
		joinArgs("rev-list", "--count", base+".."+branch):           {out: "2\n"},
		joinArgs("cherry", base, branch):                             {out: "- " + proofOtherSHA + "\n- deadbeefdeadbeefdeadbeefdeadbeefdeadbeef\n"},
		joinArgs("log", base+".."+branch, "--format=%s"):            {out: "fix the widget\nadd tests\n"},
		joinArgs("log", base, "--format=%s"):                        {out: "fix the widget\nadd tests\nolder base work\n"},
	}
	if tipIsAncestor {
		m[joinArgs("merge-base", base, branch)] = scriptStep{out: proofTipSHA + "\n"}
		return m
	}
	// rebased/squashed flow: the tip object is not on base, so the proof is
	// the base commit that carries the matching subject
	m[joinArgs("merge-base", base, branch)] = scriptStep{out: proofOtherSHA + "\n"}
	m[joinArgs("log", "-1", "--format=%s", branch)] = scriptStep{out: "fix the widget\n"}
	m[joinArgs("log", "--format=%H%x00%s", base)] = scriptStep{
		out: proofBaseSHA + "\x00fix the widget\n" + proofOtherSHA + "\x00older base work\n",
	}
	return m
}

func TestMergedMemberProof(t *testing.T) {
	const base = "develop"
	const branch = "automaton/x"

	t.Run("merged branch gives a proof with a commit", func(t *testing.T) {
		git := newMemberProofMergeGit(t, mergedMemberScript(base, branch, true))
		proof := MergedMemberProof(context.Background(), git, "/base/repo", branch)
		if !proof.Merged {
			t.Fatalf("Merged = false, want true (reason: %s)", proof.Reason)
		}
		if proof.Branch != branch {
			t.Errorf("Branch = %q, want %q", proof.Branch, branch)
		}
		if proof.Base != base {
			t.Errorf("Base = %q, want %q", proof.Base, base)
		}
		if proof.Commit != proofTipSHA {
			t.Errorf("Commit = %q, want the branch tip %q", proof.Commit, proofTipSHA)
		}
		if proof.Reason != "" {
			t.Errorf("Reason = %q, want empty", proof.Reason)
		}
		if !git.saw("rev-list", "--count", base+".."+branch) {
			t.Errorf("expected the comparison to run against %q", base)
		}
	})

	t.Run("merged with rewritten commits proves with the base commit", func(t *testing.T) {
		git := newMemberProofMergeGit(t, mergedMemberScript(base, branch, false))
		proof := MergedMemberProof(context.Background(), git, "/base/repo", branch)
		if !proof.Merged {
			t.Fatalf("Merged = false, want true (reason: %s)", proof.Reason)
		}
		if proof.Commit != proofBaseSHA {
			t.Errorf("Commit = %q, want the default-branch commit %q that carries the matching patch", proof.Commit, proofBaseSHA)
		}
	})

	t.Run("unmerged branch gives not merged", func(t *testing.T) {
		git := newMemberProofMergeGit(t, map[string]scriptStep{
			joinArgs("symbolic-ref", "refs/remotes/origin/HEAD"):            {out: "refs/remotes/origin/" + base + "\n"},
			joinArgs("rev-parse", "--verify", "refs/heads/"+branch):         {out: proofTipSHA + "\n"},
			joinArgs("rev-list", "--count", base+".."+branch):               {out: "2\n"},
			joinArgs("cherry", base, branch):                                {out: "+ deadbeefdeadbeefdeadbeefdeadbeefdeadbeef\n- " + proofOtherSHA + "\n"},
			joinArgs("log", base+".."+branch, "--format=%s"):                {out: "not landed yet\nalready here\n"},
			joinArgs("log", base, "--format=%s"):                            {out: "already here\nolder base work\n"},
		})
		proof := MergedMemberProof(context.Background(), git, "/base/repo", branch)
		if proof.Merged {
			t.Fatal("Merged = true, want false for a branch with patches not on the default branch")
		}
		if proof.Commit != "" {
			t.Errorf("Commit = %q, want empty", proof.Commit)
		}
		if proof.Reason == "" {
			t.Error("Reason is empty, want an explanation")
		}
	})

	t.Run("git error gives not merged with a reason", func(t *testing.T) {
		git := newMemberProofMergeGit(t, map[string]scriptStep{
			joinArgs("symbolic-ref", "refs/remotes/origin/HEAD"):    {out: "refs/remotes/origin/" + base + "\n"},
			joinArgs("rev-parse", "--verify", "refs/heads/"+branch): {out: proofTipSHA + "\n"},
			joinArgs("rev-list", "--count", base+".."+branch):       {err: errors.New("fatal: ambiguous argument")},
		})
		proof := MergedMemberProof(context.Background(), git, "/base/repo", branch)
		if proof.Merged {
			t.Fatal("Merged = true, want false when git errors")
		}
		if proof.Commit != "" {
			t.Errorf("Commit = %q, want empty", proof.Commit)
		}
		if !strings.Contains(proof.Reason, "rev-list") || !strings.Contains(proof.Reason, "ambiguous") {
			t.Errorf("Reason = %q, want it to carry the git error", proof.Reason)
		}
	})

	t.Run("no branch gives not merged with a reason", func(t *testing.T) {
		git := newMemberProofMergeGit(t, map[string]scriptStep{
			joinArgs("symbolic-ref", "refs/remotes/origin/HEAD"):    {out: "refs/remotes/origin/" + base + "\n"},
			joinArgs("rev-parse", "--verify", "refs/heads/"+branch): {err: errors.New("unknown revision")},
		})
		proof := MergedMemberProof(context.Background(), git, "/base/repo", branch)
		if proof.Merged {
			t.Fatal("Merged = true, want false when the branch is not in the repo")
		}
		if !strings.Contains(proof.Reason, branch) {
			t.Errorf("Reason = %q, want it to name the branch", proof.Reason)
		}
	})
}

func TestMergedMemberDefaultBranch(t *testing.T) {
	const branch = "automaton/x"

	t.Run("compares against trunk when the project default is trunk", func(t *testing.T) {
		git := newMemberProofMergeGit(t, mergedMemberScript("trunk", branch, true))
		proof := MergedMemberProof(context.Background(), git, "/base/repo", branch)
		if !proof.Merged {
			t.Fatalf("Merged = false, want true (reason: %s)", proof.Reason)
		}
		if proof.Base != "trunk" {
			t.Errorf("Base = %q, want trunk", proof.Base)
		}
		if proof.Commit != proofTipSHA {
			t.Errorf("Commit = %q, want the branch tip %q", proof.Commit, proofTipSHA)
		}
		if !git.saw("cherry", "trunk", branch) {
			t.Error("the patch comparison must run against trunk, not any other branch")
		}
		if !git.saw("merge-base", "trunk", branch) {
			t.Error("the proof commit must be resolved against trunk")
		}
	})

	t.Run("falls back to the base repo HEAD branch when the remote HEAD ref is missing", func(t *testing.T) {
		git := newMemberProofMergeGit(t, mergedMemberScript("trunk", branch, true))
		// replace the remote-HEAD answer with a failure, add the fallback
		git.script[joinArgs("symbolic-ref", "refs/remotes/origin/HEAD")] = scriptStep{err: errors.New("not a valid ref")}
		git.script[joinArgs("rev-parse", "--abbrev-ref", "HEAD")] = scriptStep{out: "trunk\n"}
		proof := MergedMemberProof(context.Background(), git, "/base/repo", branch)
		if !proof.Merged {
			t.Fatalf("Merged = false, want true (reason: %s)", proof.Reason)
		}
		if proof.Base != "trunk" {
			t.Errorf("Base = %q, want trunk", proof.Base)
		}
		if !git.saw("cherry", "trunk", branch) {
			t.Error("the patch comparison must run against trunk")
		}
	})

	t.Run("git cannot resolve the default branch gives not merged with a reason", func(t *testing.T) {
		git := newMemberProofMergeGit(t, map[string]scriptStep{
			joinArgs("symbolic-ref", "refs/remotes/origin/HEAD"): {err: errors.New("not a valid ref")},
			joinArgs("rev-parse", "--abbrev-ref", "HEAD"):        {err: errors.New("ambiguous")},
		})
		proof := MergedMemberProof(context.Background(), git, "/base/repo", branch)
		if proof.Merged {
			t.Fatal("Merged = true, want false when the default branch cannot be resolved")
		}
		if !strings.Contains(proof.Reason, "default branch") {
			t.Errorf("Reason = %q, want it to say the default branch is unknown", proof.Reason)
		}
	})
}

// joinArgs keys a scripted git command from its argument list.
func joinArgs(args ...string) string {
	return strings.Join(args, "\x1f")
}
