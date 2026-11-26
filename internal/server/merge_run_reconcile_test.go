package server

// The daemon's own landing record accepts what a reconciled push moved, and a
// held run is reported as not merged. Sandboxes with local bare remotes only.

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/mergepipeline"
)

// lostReply pushes as the daemon does and then loses the outcome.
type lostReply struct {
	mergepipeline.ExecShipRunner
	after func() error
}

func (l lostReply) PushCommit(ctx context.Context, dir, commit, branch, tag string) error {
	if err := l.ExecShipRunner.PushCommit(ctx, dir, commit, branch, tag); err != nil {
		return err
	}
	return l.after()
}

// divergedSelection is eligibleSelection with main moved after the branch
// forked, so the run rebases the reviewed branch.
func divergedSelection(t *testing.T, mgr *autonomous.Manager, repo string) (*autonomous.PRD, autonomous.MergeSelection) {
	t.Helper()
	if err := os.WriteFile(repo+"/later.txt", []byte("later\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	postShipGit(t, repo, "add", "later.txt")
	postShipGit(t, repo, "commit", "-q", "-m", "later on main")
	postShipGit(t, repo, "push", "-q", "origin", "main")
	return eligibleSelection(t, mgr, repo)
}

func assertLanded(t *testing.T, mgr *autonomous.Manager, repo, id string) {
	t.Helper()
	remote := strings.Fields(postShipGit(t, repo, "ls-remote", "origin", "refs/heads/main"))[0]
	got, _ := mgr.Store().GetPRD(id)
	if got.Landing.State != autonomous.LandingLanded || got.Landing.Commit != remote || got.MergeCommit != remote || got.Status != autonomous.PRDCompleted {
		t.Errorf("landing=%+v shipped=%q status=%s, want landed at %s", got.Landing, got.MergeCommit, got.Status, remote)
	}
	if local := postShipGit(t, repo, "rev-parse", "main"); local != remote {
		t.Errorf("local main %s, remote %s", local, remote)
	}
	postShipGit(t, repo, "merge-base", "--is-ancestor", got.Git.Branch, remote)
}

func TestLostPushReplyRecordsTheRebasedBranchWithTheRealRecord(t *testing.T) {
	for name, after := range map[string]func(origin string) func() error{
		"the push reports an error": func(string) func() error {
			return func() error { return errors.New("connection reset") }
		},
		"the remote cannot be read until the next start": func(origin string) func() error {
			return func() error { return os.Rename(origin, origin+".gone") }
		},
	} {
		t.Run(name, func(t *testing.T) {
			repo := newPostShipRepo(t)
			_, mgr := liveAPIServer(t, repo)
			p, sel := divergedSelection(t, mgr, repo)
			origin := postShipGit(t, repo, "remote", "get-url", "origin")
			mp := config.MergePipelineConfig{VersionFiles: isolateVersionFiles}
			data := t.TempDir()
			ship := lostReply{after: after(origin)}
			_, _ = supervisorLoopMergeSelected(context.Background(), mgr, data, mp, sel,
				func(r *mergepipeline.Runner) { r.Ship = ship })
			_ = os.Rename(origin+".gone", origin)
			cfg := &config.Config{DataDir: data}
			cfg.Autonomous.MergePipeline = mp
			ReconcileMergePushesAtStart(context.Background(), mgr, cfg)
			assertLanded(t, mgr, repo, p.ID)
		})
	}
}

func TestSupervisorLoopMarksAHeldRunNotMerged(t *testing.T) {
	repo := newPostShipRepo(t)
	_, mgr := liveAPIServer(t, repo)
	_, sel := eligibleSelection(t, mgr, repo)
	out, err := supervisorLoopMergeSelected(context.Background(), mgr, t.TempDir(),
		config.MergePipelineConfig{VersionFiles: isolateVersionFiles,
			ReleaseApproval: config.ReleaseApprovalModes{Patch: config.ReleaseApprovalManual}}, sel)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(out.NotMerged, ",") != "feature" {
		t.Errorf("not merged = %v, want the held branch", out.NotMerged)
	}
}

// The daemon reconciles at start and before it lists what is waiting to merge.
func TestDaemonReconcilesAtStart(t *testing.T) {
	src, err := os.ReadFile("../../cmd/datawatch/main.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(src), "go server.ReconcileMergePushesAtStart(context.Background(), amgr, cfg)") {
		t.Error("main.go does not reconcile journalled pushes at start")
	}
}
