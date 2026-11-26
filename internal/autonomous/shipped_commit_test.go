package autonomous

import (
	"context"
	"strings"
	"testing"
	"time"
)

// meGit records every git command the manager runs. all, when set, answers
// the "list every commit" command instead of git.
type meGit struct {
	calls [][]string
	all   string
}

func (g *meGit) Run(ctx context.Context, dir string, args ...string) (string, error) {
	g.calls = append(g.calls, append([]string{}, args...))
	if g.all != "" && len(args) == 2 && args[0] == "rev-list" && args[1] == "--all" {
		return g.all, nil
	}
	return NewMergeGitRunner().Run(ctx, dir, args...)
}

// received reports whether any git command was given value, whole or as part
// of an argument.
func (g *meGit) received(value string) []string {
	for _, c := range g.calls {
		for _, a := range c {
			if a == value || strings.Contains(a, value) {
				return c
			}
		}
	}
	return nil
}

func (r *meRig) watchGit() *meGit {
	g := &meGit{}
	r.m.mu.Lock()
	r.m.archivedUnmergedGit = g
	r.m.mu.Unlock()
	return g
}

func (r *meRig) notRecorded(id, why string) {
	r.t.Helper()
	got, _ := r.m.store.GetPRD(id)
	if got.IsShipped() || got.ShippedSource != "" || got.ClosedOutcome != "" || got.Landing.State != LandingPending {
		r.t.Fatalf("%s: something was recorded: commit=%q source=%q outcome=%q landing=%s",
			why, got.MergeCommit, got.ShippedSource, got.ClosedOutcome, got.Landing.State)
	}
}

// pushed merges the Automaton's branch into main and pushes it.
func (r *meRig) pushed(branch string) string {
	r.t.Helper()
	commit := r.mergeLocally(branch)
	gitRun(r.t, r.repo, "push", "-q", "origin", "main")
	return commit
}

func TestShippedMergeStepRefusesTextThatIsNoCommitIDBeforeAnyGit(t *testing.T) {
	r := newMERig(t)
	p, _ := r.automaton(PRDCompleted)
	r.pushed(p.Git.Branch)
	g := r.watchGit()
	for _, v := range []string{
		"--upload-pack=touch", "-n1234567", "HEAD", "origin/main", "refs/heads/main", "HEAD~1",
		"abcdef1^{tree}", "abcdef1..abcdef2", "abcdef1 abcdef2", "zzzzzzzz", "abc123", strings.Repeat("a", 65),
	} {
		g.calls = nil
		_, err := r.m.RecordPRDShipped(p.ID, "v1.0.1", time.Now(), v, ShippedSourceMergeStep)
		if err == nil || !strings.Contains(err.Error(), "not a commit id") {
			t.Errorf("%q: err = %v, want it to say it is not a commit id", v, err)
		}
		if len(g.calls) != 0 {
			t.Errorf("%q: git ran before the value was refused: %q", v, g.calls)
		}
		r.notRecorded(p.ID, v)
	}
}

// A full id has the repository's own hash length.
func TestShippedMergeStepRefusesAnIDLongerThanTheRepositoryHash(t *testing.T) {
	r := newMERig(t)
	p, _ := r.automaton(PRDCompleted)
	commit := r.pushed(p.Git.Branch)
	g := r.watchGit()
	for _, v := range []string{commit + "0", strings.Repeat("a", 64)} {
		_, err := r.m.RecordPRDShipped(p.ID, "v1.0.1", time.Now(), v, ShippedSourceMergeStep)
		if err == nil || !strings.Contains(err.Error(), "40") {
			t.Errorf("%q: err = %v, want it to name the repository's 40 characters", v, err)
		}
		if c := g.received(v); c != nil {
			t.Errorf("%q reached git: %q", v, c)
		}
		r.notRecorded(p.ID, v)
	}
}

// The merge scripts send `git rev-parse --short HEAD`. It is matched against
// git's own list of commits, so the short text never reaches git, and a ref
// with the same name does not win.
func TestShippedMergeStepResolvesAnAbbreviatedID(t *testing.T) {
	r := newMERig(t)
	p, _ := r.automaton(PRDCompleted)
	before := r.rev("main")
	commit := r.pushed(p.Git.Branch)
	short := commit[:7]
	gitRun(t, r.repo, "branch", short, before)
	g := r.watchGit()
	got, err := r.m.RecordPRDShipped(p.ID, "v1.0.1", time.Now(), strings.ToUpper(short), ShippedSourceMergeStep)
	if err != nil {
		t.Fatal(err)
	}
	if got.MergeCommit != commit || got.Landing.State != LandingLanded || got.Landing.Commit != commit {
		t.Fatalf("stored commit=%q landing=%+v, want the full id %s", got.MergeCommit, got.Landing, commit)
	}
	for _, c := range g.calls {
		for _, a := range c {
			if strings.EqualFold(a, short) {
				t.Fatalf("the abbreviated id reached git: %q", c)
			}
		}
	}
	// Sent again, it is the same record: nothing is asked of git or written.
	at := got.UpdatedAt
	g.calls = nil
	again, err := r.m.RecordPRDShipped(p.ID, "v9.9.9", time.Now(), short, ShippedSourceMergeStep)
	if err != nil || again.MergeCommit != commit || again.MergedInRelease != "v1.0.1" || !again.UpdatedAt.Equal(at) || len(g.calls) != 0 {
		t.Fatalf("the same short id again: err=%v commit=%q release=%q git=%q", err, again.MergeCommit, again.MergedInRelease, g.calls)
	}
}

func TestShippedMergeStepRefusesAnAbbreviatedIDItCannotResolve(t *testing.T) {
	r := newMERig(t)
	p, _ := r.automaton(PRDCompleted)
	commit := r.pushed(p.Git.Branch)
	g := r.watchGit()
	twin := func(last string) string { return commit[:39] + last }
	other := "0"
	if strings.HasSuffix(commit, "0") {
		other = "1"
	}
	for _, c := range []struct{ name, all, value, why string }{
		{"two commits start with it", commit + "\n" + twin(other) + "\n", commit[:12], "more than one"},
		{"no commit at all", "warning: nothing\n", commit[:12], "no commit"},
	} {
		g.all = c.all
		_, err := r.m.RecordPRDShipped(p.ID, "v1.0.1", time.Now(), c.value, ShippedSourceMergeStep)
		if err == nil || !strings.Contains(err.Error(), c.why) || !strings.Contains(err.Error(), "full") {
			t.Errorf("%s: err = %v, want it to say %q and ask for the full id", c.name, err, c.why)
		}
		r.notRecorded(p.ID, c.name)
	}
}

// Being on the remote is not enough: the commit has to hold the work.
func TestShippedMergeStepNeedsTheCommitToHoldTheBranch(t *testing.T) {
	for _, remote := range []bool{true, false} {
		r := newMERig(t)
		p, _ := r.automaton(PRDCompleted)
		old := r.rev("main")
		other, _ := r.automaton(PRDCompleted)
		unrelated := r.pushed(other.Git.Branch)
		if !remote {
			gitRun(t, r.repo, "remote", "remove", "origin")
		}
		for _, v := range []string{old, unrelated, old[:10]} {
			_, err := r.m.RecordPRDShipped(p.ID, "v1.0.1", time.Now(), v, ShippedSourceMergeStep)
			if err == nil || !strings.Contains(err.Error(), "does not contain its branch "+p.Git.Branch) {
				t.Fatalf("remote=%v %s: err = %v, want it to say the commit does not contain the branch", remote, v, err)
			}
			r.notRecorded(p.ID, v)
		}
		commit := r.mergeLocally(p.Git.Branch)
		if remote {
			gitRun(t, r.repo, "push", "-q", "origin", "main")
		}
		got, err := r.m.RecordPRDShipped(p.ID, "v1.0.2", time.Now(), commit, ShippedSourceMergeStep)
		if err != nil || got.MergeCommit != commit {
			t.Fatalf("remote=%v: the commit that holds the work: %v", remote, err)
		}
		if landed := got.Landing.State == LandingLanded; landed != remote {
			t.Fatalf("remote=%v: landing=%s", remote, got.Landing.State)
		}
	}
}

// The id of an annotated tag is a full id, but not a commit.
func TestShippedMergeStepRefusesATagObjectID(t *testing.T) {
	r := newMERig(t)
	p, _ := r.automaton(PRDCompleted)
	r.pushed(p.Git.Branch)
	gitRun(t, r.repo, "tag", "-a", "-m", "release", "v1.0.1")
	tag := r.rev("refs/tags/v1.0.1")
	_, err := r.m.RecordPRDShipped(p.ID, "v1.0.1", time.Now(), tag, ShippedSourceMergeStep)
	if err == nil || !strings.Contains(err.Error(), "not a commit") {
		t.Fatalf("err = %v, want it to say the id is not a commit", err)
	}
	r.notRecorded(p.ID, "tag object")
}

func TestRecordMergeLandingRefusesANonFullIDBeforeGitSeesIt(t *testing.T) {
	r := newMERig(t)
	p, _ := r.automaton(PRDCompleted)
	commit := r.pushed(p.Git.Branch)
	g := r.watchGit()
	for _, v := range []string{"HEAD", "--upload-pack=touch", commit[:12]} {
		g.calls = nil
		_, err := r.m.RecordMergeLanding(context.Background(), p.ID, "v1.0.1", v, time.Now())
		if err == nil || !strings.Contains(err.Error(), "full commit id") {
			t.Errorf("%q: err = %v, want it to ask for the full commit id", v, err)
		}
		for _, c := range g.calls {
			for _, a := range c {
				if a == v {
					t.Errorf("%q reached git: %q", v, c)
				}
			}
		}
		r.notRecorded(p.ID, v)
	}
}

// A backfill record is not checked against git, so it is kept as given.
func TestShippedBackfillRecordRunsNoGit(t *testing.T) {
	r := newMERig(t)
	p, _ := r.automaton(PRDCompleted)
	g := r.watchGit()
	got, err := r.m.RecordPRDShipped(p.ID, "v1.0.1", time.Now(), "c0ffee", ShippedSourceBackfill)
	if err != nil || got.MergeCommit != "c0ffee" || len(g.calls) != 0 {
		t.Fatalf("err=%v commit=%q git=%q", err, got.MergeCommit, g.calls)
	}
}
