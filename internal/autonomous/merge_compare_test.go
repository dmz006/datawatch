package autonomous

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// fakeMergeGit answers git calls from canned output keyed by the joined
// args and records every call so tests can assert read-only behaviour.
type fakeMergeGit struct {
	out   map[string]string
	errOn string
	calls []string
}

func (f *fakeMergeGit) Run(_ context.Context, _ string, args ...string) (string, error) {
	key := strings.Join(args, " ")
	f.calls = append(f.calls, key)
	if f.errOn != "" && strings.HasPrefix(key, f.errOn) {
		return "fatal: boom", errors.New("exit status 128")
	}
	return f.out[key], nil
}

func newFakeMergeGit(ahead, cherry, branchSubjects, baseSubjects string) *fakeMergeGit {
	return &fakeMergeGit{out: map[string]string{
		"rev-list --count main..feat": ahead,
		"cherry main feat":            cherry,
		"log main..feat --format=%s":  branchSubjects,
		"log main --format=%s":        baseSubjects,
	}}
}

func TestMergeCompare_DecisionTable(t *testing.T) {
	cases := []struct {
		name                 string
		ahead, cherry        string
		branchSubj, baseSubj string
		wantState            string
		wantNotOn, wantOn    int
		wantSubjectsOnBase   bool
	}{
		{"no commits is empty", "0\n", "", "", "a\nb\n", MergeStateEmpty, 0, 0, true},
		{"all patches on base is merged", "2\n", "- aaa\n- bbb\n", "x\ny\n", "x\ny\nz\n", MergeStateMerged, 0, 2, true},
		{"patch missing and subject missing is pending", "1\n", "+ aaa\n", "new work\n", "other\n", MergeStatePending, 1, 0, false},
		{"patch missing but subject on base needs look", "1\n", "+ aaa\n", "same subject\n", "same subject\n", MergeStateNeedsLook, 1, 0, true},
		{"patches on base but subject missing needs look", "1\n", "- aaa\n", "reworded\n", "original\n", MergeStateNeedsLook, 0, 1, false},
		{"mixed cherry with subjects absent is pending", "2\n", "- aaa\n+ bbb\n", "one\ntwo\n", "one\n", MergeStatePending, 1, 1, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeMergeGit(tc.ahead, tc.cherry, tc.branchSubj, tc.baseSubj)
			got := CompareBranchToMain(context.Background(), f, "repo", "feat", "main")
			if got.State != tc.wantState {
				t.Fatalf("state = %q, want %q (%+v)", got.State, tc.wantState, got)
			}
			if got.CherryNotOnBase != tc.wantNotOn || got.CherryOnBase != tc.wantOn {
				t.Fatalf("cherry = +%d/-%d, want +%d/-%d", got.CherryNotOnBase, got.CherryOnBase, tc.wantNotOn, tc.wantOn)
			}
			if got.SubjectsOnBase != tc.wantSubjectsOnBase {
				t.Fatalf("SubjectsOnBase = %v, want %v", got.SubjectsOnBase, tc.wantSubjectsOnBase)
			}
			for _, c := range f.calls {
				sub := strings.Fields(c)[0]
				if sub != "rev-list" && sub != "cherry" && sub != "log" {
					t.Fatalf("non read-only git call: %q", c)
				}
			}
		})
	}

	t.Run("git error is unknown and keeps text", func(t *testing.T) {
		for _, prefix := range []string{"rev-list", "cherry", "log main..feat", "log main --"} {
			f := newFakeMergeGit("1\n", "+ aaa\n", "s\n", "t\n")
			f.errOn = prefix
			got := CompareBranchToMain(context.Background(), f, "repo", "feat", "main")
			if got.State != MergeStateUnknown {
				t.Fatalf("%s: state = %q, want unknown", prefix, got.State)
			}
			if !strings.Contains(got.Error, "fatal: boom") {
				t.Fatalf("%s: error text lost: %q", prefix, got.Error)
			}
		}
	})

	t.Run("unparseable count is unknown", func(t *testing.T) {
		f := newFakeMergeGit("garbage\n", "", "", "")
		got := CompareBranchToMain(context.Background(), f, "repo", "feat", "main")
		if got.State != MergeStateUnknown || got.Error == "" {
			t.Fatalf("got %+v, want unknown with error", got)
		}
	})
}

// gitFixture creates a repository with one commit on main.
func gitFixture(t *testing.T) string {
	t.Helper()
	t.Setenv("GIT_AUTHOR_NAME", "Automata Test")
	t.Setenv("GIT_AUTHOR_EMAIL", "automata@example.invalid")
	t.Setenv("GIT_COMMITTER_NAME", "Automata Test")
	t.Setenv("GIT_COMMITTER_EMAIL", "automata@example.invalid")
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_SYSTEM", os.DevNull)
	dir := t.TempDir()
	gitT(t, dir, "init", "-b", "main")
	writeFileT(t, dir, "base.txt", "base\n")
	gitT(t, dir, "add", "base.txt")
	gitT(t, dir, "commit", "-m", "Automaton base commit")
	return dir
}

func gitT(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

func writeFileT(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestMergeCompare_BranchWithCommitsNotOnMain_IsPending(t *testing.T) {
	dir := gitFixture(t)
	gitT(t, dir, "checkout", "-b", "automaton/work")
	writeFileT(t, dir, "work.txt", "work\n")
	gitT(t, dir, "add", "work.txt")
	gitT(t, dir, "commit", "-m", "Automaton branch only commit")

	got := CompareBranchToMain(context.Background(), NewMergeGitRunner(), dir, "automaton/work", "main")
	if got.State != MergeStatePending {
		t.Fatalf("state = %q, want pending (%+v)", got.State, got)
	}
	if got.Ahead != 1 || got.CherryNotOnBase != 1 || got.SubjectsOnBase {
		t.Fatalf("unexpected counts: %+v", got)
	}
}

func TestMergeCompare_EmptyBranch_IsEmpty(t *testing.T) {
	dir := gitFixture(t)
	gitT(t, dir, "branch", "automaton/work")

	got := CompareBranchToMain(context.Background(), NewMergeGitRunner(), dir, "automaton/work", "main")
	if got.State != MergeStateEmpty || got.Ahead != 0 {
		t.Fatalf("got %+v, want empty", got)
	}
}

func TestMergeCompare_PatchChangedDuringMerge_NeedsLook(t *testing.T) {
	dir := gitFixture(t)
	gitT(t, dir, "checkout", "-b", "automaton/work")
	writeFileT(t, dir, "work.txt", "branch content\n")
	gitT(t, dir, "add", "work.txt")
	gitT(t, dir, "commit", "-m", "Automaton shared subject")
	gitT(t, dir, "checkout", "main")
	writeFileT(t, dir, "work.txt", "different content on main\n")
	gitT(t, dir, "add", "work.txt")
	gitT(t, dir, "commit", "-m", "Automaton shared subject")

	got := CompareBranchToMain(context.Background(), NewMergeGitRunner(), dir, "automaton/work", "main")
	if got.State != MergeStateNeedsLook {
		t.Fatalf("state = %q, want needs_look (%+v)", got.State, got)
	}
	if got.CherryNotOnBase != 1 || !got.SubjectsOnBase {
		t.Fatalf("unexpected counts: %+v", got)
	}
}

func TestMergeCompare_AllCommitsOnMain_IsMerged(t *testing.T) {
	dir := gitFixture(t)
	gitT(t, dir, "checkout", "-b", "automaton/work")
	writeFileT(t, dir, "work.txt", "work\n")
	gitT(t, dir, "add", "work.txt")
	gitT(t, dir, "commit", "-m", "Automaton cherry picked commit")
	gitT(t, dir, "checkout", "main")
	// A second main-only commit makes the cherry-pick a new commit rather
	// than a fast-forward, so the branch commit is not an ancestor of main.
	writeFileT(t, dir, "other.txt", "other\n")
	gitT(t, dir, "add", "other.txt")
	gitT(t, dir, "commit", "-m", "Automaton main only commit")
	gitT(t, dir, "cherry-pick", "automaton/work")

	got := CompareBranchToMain(context.Background(), NewMergeGitRunner(), dir, "automaton/work", "main")
	if got.State != MergeStateMerged {
		t.Fatalf("state = %q, want merged (%+v)", got.State, got)
	}
	if got.Ahead != 1 || got.CherryNotOnBase != 0 || got.CherryOnBase != 1 || !got.SubjectsOnBase {
		t.Fatalf("unexpected counts: %+v", got)
	}
}
