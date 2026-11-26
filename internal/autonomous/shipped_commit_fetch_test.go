package autonomous

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// pushedElsewhere merges the Automaton's branch in another clone and pushes
// it, so the merge repository does not have the commit. more adds commits on
// top, so that the first one is no longer the head of the remote.
func (r *meRig) pushedElsewhere(branch string, more bool) string {
	r.t.Helper()
	gitRun(r.t, r.repo, "push", "-q", "origin", branch)
	other := filepath.Join(r.t.TempDir(), "other")
	gitRun(r.t, r.repo, "clone", "-q", r.origin, other)
	gitRun(r.t, other, "config", "user.email", "t@datawatch")
	gitRun(r.t, other, "config", "user.name", "test")
	gitRun(r.t, other, "checkout", "-q", "-B", "main", "origin/main")
	gitRun(r.t, other, "merge", "-q", "--no-ff", "-m", "Merge "+branch, "origin/"+branch)
	out, err := NewMergeGitRunner().Run(context.Background(), other, "rev-parse", "HEAD")
	if err != nil {
		r.t.Fatal(err)
	}
	if more {
		gitRun(r.t, other, "commit", "--allow-empty", "-q", "-m", "later work")
	}
	gitRun(r.t, other, "push", "-q", "origin", "main")
	commit := strings.TrimSpace(out)
	if _, err := NewMergeGitRunner().Run(context.Background(), r.repo, "cat-file", "-e", commit); err == nil {
		r.t.Fatal("the merge repository already has the commit")
	}
	return commit
}

// A commit pushed from another clone is fetched before it is judged.
func TestShippedMergeStepFetchesACommitItDoesNotHave(t *testing.T) {
	for _, c := range []struct {
		name        string
		more, short bool
	}{
		{"full id, head of the remote", false, false},
		{"full id, behind the head of the remote", true, false},
		{"abbreviated id", false, true},
		{"abbreviated id, behind the head of the remote", true, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := newMERig(t)
			p, _ := r.automaton(PRDCompleted)
			commit := r.pushedElsewhere(p.Git.Branch, c.more)
			value := commit
			if c.short {
				value = commit[:9]
			}
			got, err := r.m.RecordPRDShipped(p.ID, "v1.0.1", time.Now(), value, ShippedSourceMergeStep)
			if err != nil {
				t.Fatal(err)
			}
			if got.MergeCommit != commit || got.Landing.State != LandingLanded {
				t.Fatalf("commit=%q landing=%s, want %s landed", got.MergeCommit, got.Landing.State, commit)
			}
		})
	}
}

// noFetchGit fails every fetch.
type noFetchGit struct{ *meGit }

func (g noFetchGit) Run(ctx context.Context, dir string, args ...string) (string, error) {
	if len(args) > 0 && args[0] == "fetch" {
		g.calls = append(g.calls, args)
		return "fatal: https://user:hunter2@host.invalid/ unreachable", errors.New("exit status 128")
	}
	return g.meGit.Run(ctx, dir, args...)
}

func TestShippedMergeStepSaysWhenTheFetchFailsOrTheCommitIsUnknown(t *testing.T) {
	r := newMERig(t)
	p, _ := r.automaton(PRDCompleted)
	commit := r.pushedElsewhere(p.Git.Branch, false)
	nowhere := strings.Repeat("1234567890", 4)

	g := &meGit{}
	r.m.mu.Lock()
	r.m.archivedUnmergedGit = noFetchGit{g}
	r.m.mu.Unlock()
	for _, v := range []string{commit, commit[:9]} {
		_, err := r.m.RecordPRDShipped(p.ID, "v1.0.1", time.Now(), v, ShippedSourceMergeStep)
		if err == nil || !strings.Contains(err.Error(), "fetching main from remote origin failed") {
			t.Fatalf("%s: err = %v, want it to say the fetch failed", v, err)
		}
		if strings.Contains(err.Error(), "hunter2") {
			t.Fatalf("git output, which can hold a credential, is in the answer: %v", err)
		}
		r.notRecorded(p.ID, "fetch failed")
	}

	g = r.watchGit()
	for _, v := range []string{nowhere, "123456789"} {
		g.calls = nil
		_, err := r.m.RecordPRDShipped(p.ID, "v1.0.1", time.Now(), v, ShippedSourceMergeStep)
		if err == nil || !strings.Contains(err.Error(), "unknown commit") || !strings.Contains(err.Error(), "also after fetching main from remote origin") {
			t.Fatalf("%s: err = %v, want an unknown commit, also after the fetch", v, err)
		}
		fetches := 0
		for _, c := range g.calls {
			if c[0] == "fetch" {
				fetches++
				for _, a := range c {
					if strings.Contains(a, v) {
						t.Fatalf("the request value is in the fetch: %q", c)
					}
				}
			}
		}
		if fetches != 1 {
			t.Fatalf("%s: %d fetches, want 1", v, fetches)
		}
		r.notRecorded(p.ID, "unknown")
	}
}

// A commit the repository has is judged without a fetch before the lookup.
func TestShippedMergeStepWithNoRemoteSaysTheCommitIsUnknown(t *testing.T) {
	r := newMERig(t)
	p, _ := r.automaton(PRDCompleted)
	r.mergeLocally(p.Git.Branch)
	gitRun(t, r.repo, "remote", "remove", "origin")
	g := r.watchGit()
	_, err := r.m.RecordPRDShipped(p.ID, "v1.0.1", time.Now(), strings.Repeat("a1", 20), ShippedSourceMergeStep)
	if err == nil || !strings.Contains(err.Error(), "unknown commit") {
		t.Fatalf("err = %v, want unknown commit", err)
	}
	for _, c := range g.calls {
		if c[0] == "fetch" {
			t.Fatalf("fetched with no remote: %q", c)
		}
	}
}
