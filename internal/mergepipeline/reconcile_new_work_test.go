//go:build unix

package mergepipeline

// A push whose confirmation was lost, followed by new work on the branch: the
// repair still finishes, records what was pushed, and leaves the new work
// where it is. Sandboxes with local bare remotes only.

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
)

// tipLanding is sandboxLanding that can also record a landing for the exact
// commit that was pushed, as the daemon's record does.
type tipLanding struct {
	sandboxLanding
	tips *[]string
}

func (l tipLanding) RecordMergeLandingOfPushedTip(_ context.Context, id, _, commit, tip string, _ time.Time) (*autonomous.PRD, error) {
	runGitOK := func(dir string, args ...string) bool { return gitSucceeds(dir, args...) }
	if !runGitOK(l.s.bare, "merge-base", "--is-ancestor", commit, "refs/heads/main") ||
		!runGitOK(l.s.dir, "merge-base", "--is-ancestor", tip, commit) {
		return nil, os.ErrInvalid
	}
	*l.recorded = append(*l.recorded, id+" "+commit)
	*l.tips = append(*l.tips, tip)
	return &autonomous.PRD{}, nil
}

// newBranchWork puts one more commit on branch without checking it out.
func newBranchWork(t *testing.T, dir, branch string) string {
	t.Helper()
	c := runGit(t, dir, "commit-tree", "-p", "refs/heads/"+branch, "-m", "new work after the lost push", "refs/heads/"+branch+"^{tree}")
	runGit(t, dir, "update-ref", "refs/heads/"+branch, c)
	return c
}

func TestNewBranchWorkAfterALostPushDoesNotStickTheRepair(t *testing.T) {
	for _, rebased := range []bool{false, true} {
		t.Run(map[bool]string{false: "as selected", true: "rebased by the run"}[rebased], func(t *testing.T) {
			s := newSandbox(t)
			committerEnv(t)
			if rebased {
				diverge(t, s)
			}
			selected := runGit(t, s.dir, "rev-parse", "feature")
			var landed, tips []string
			landing := tipLanding{sandboxLanding{s: s, recorded: &landed}, &tips}
			// The push arrives and the remote cannot be read afterwards.
			first := releaseRunner(t, s, noTagSteps(), realPusher{after: func() error { return os.Rename(s.bare, s.bare+".gone") }}, landing, &fakeNotify{})
			_, _ = releaseFeature(first)
			if err := os.Rename(s.bare+".gone", s.bare); err != nil {
				t.Fatal(err)
			}
			if len(pushJournal(t, s)) != 1 {
				t.Fatalf("fixture: journal %v", pushJournal(t, s))
			}
			newer := newBranchWork(t, s.dir, "feature")
			remote := runGit(t, s.bare, "rev-parse", "refs/heads/main")

			r := releaseRunner(t, s, noTagSteps(), realPusher{}, landing, &fakeNotify{})
			results, err := r.ReconcilePushes(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if len(results) != 1 || !results[0].Done || !results[0].Pushed {
				t.Fatalf("the repair did not finish: %+v", results)
			}
			if left := pushJournal(t, s); len(left) != 0 {
				t.Errorf("journal left: %v", left)
			}
			if got := runGit(t, s.dir, "rev-parse", "feature"); got != newer {
				t.Errorf("the branch with new work moved: %s, want %s", got, newer)
			}
			if got := runGit(t, s.dir, "rev-parse", "main"); got != remote {
				t.Errorf("local main %s is not the pushed commit %s", got, remote)
			}
			if len(landed) != 1 || landed[0] != "a1 "+remote {
				t.Errorf("landing = %v", landed)
			}
			if len(tips) != 1 || (!rebased && tips[0] != selected) || tips[0] == newer {
				t.Errorf("landing tip = %v, want the tip that was pushed (selected %s)", tips, selected)
			}
			if !strings.Contains(results[0].Note, "newer") {
				t.Errorf("the note does not say the branch holds newer work: %q", results[0].Note)
			}
			// Nothing is stuck: a later run is not refused for the old push.
			res, err := releaseFeature(releaseRunner(t, s, noTagSteps(), realPusher{}, landing, &fakeNotify{}))
			if err != nil && strings.Contains(err.Error(), "already pushed") {
				t.Errorf("still refused for the repaired push: %v %+v", err, res.Run.Steps)
			}
		})
	}
}

// A branch that changed and a landing record that cannot take the pushed tip
// stay journalled: nothing is recorded that cannot be proven.
func TestNewBranchWorkWithoutAPushedTipRecordStaysJournalled(t *testing.T) {
	s := newSandbox(t)
	committerEnv(t)
	var landed []string
	landing := sandboxLanding{s: s, recorded: &landed}
	first := releaseRunner(t, s, noTagSteps(), realPusher{after: func() error { return os.Rename(s.bare, s.bare+".gone") }}, landing, &fakeNotify{})
	_, _ = releaseFeature(first)
	if err := os.Rename(s.bare+".gone", s.bare); err != nil {
		t.Fatal(err)
	}
	newBranchWork(t, s.dir, "feature")
	results, err := releaseRunner(t, s, noTagSteps(), realPusher{}, landing, &fakeNotify{}).ReconcilePushes(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Done || len(landed) != 0 || len(pushJournal(t, s)) != 1 {
		t.Errorf("results %+v landed %v journal %v", results, landed, pushJournal(t, s))
	}
}
