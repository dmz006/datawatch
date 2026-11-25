package server

// The merge selection rule on the daemon's own paths: the run route and the
// supervisor loop merge job. Every repository here is a sandbox with a local
// bare "remote".

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/mergepipeline"
)

// selAutomaton records an Automaton that owns the sandbox's "feature" branch.
func selAutomaton(t *testing.T, mgr *autonomous.Manager, status autonomous.PRDStatus, branch string) *autonomous.PRD {
	t.Helper()
	p, err := mgr.CreatePRD("spec", "", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	p.Status = status
	p.Git.Branch = branch
	p.Landing = &autonomous.Landing{State: autonomous.LandingPending, Actor: "autonomous", At: time.Now(), Reason: "branch created"}
	p.Story = []autonomous.Story{{ID: "s1", Tasks: []autonomous.Task{{ID: "t1", Title: "code", Status: autonomous.TaskCompleted}}}}
	if err := mgr.Store().SavePRD(p); err != nil {
		t.Fatal(err)
	}
	return p
}

func selAccept(t *testing.T, mgr *autonomous.Manager, p *autonomous.PRD, commit string) {
	t.Helper()
	p.MergeAcceptance = &autonomous.MergeAcceptance{Commit: commit, Actor: "operator", Reason: "checked", At: time.Now()}
	if err := mgr.Store().SavePRD(p); err != nil {
		t.Fatal(err)
	}
}

func selRun(t *testing.T, s *Server, body string) (autonomous.MergePipelineRun, string) {
	t.Helper()
	rr := liveDo(s.handleAutonomousMergeRun, http.MethodPost, "/api/autonomous/merge/run", body)
	if rr.Code != http.StatusOK {
		t.Fatalf("merge run: %d %s", rr.Code, rr.Body)
	}
	var out struct {
		Run   autonomous.MergePipelineRun `json:"run"`
		Error string                      `json:"error"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out.Run, out.Error
}

func selStep(run autonomous.MergePipelineRun, name string) string {
	for _, st := range run.Steps {
		if st.Step == name {
			return st.Result
		}
	}
	return ""
}

func TestMergeRunRouteRefusesIneligibleWork(t *testing.T) {
	for name, c := range map[string]struct {
		status autonomous.PRDStatus
		why    string
	}{
		"failed, even accepted": {autonomous.PRDFailed, "failed work is never merged"},
		"completed, no review":  {autonomous.PRDCompleted, "review is pending"},
		"no Automaton":          {"", "no Automaton has branch feature"},
	} {
		t.Run(name, func(t *testing.T) {
			s, mgr, repo, _, ship, _ := switchServer(t)
			s.cfg.Autonomous.MergeDeployRunEnabled = true
			if c.status != "" {
				p := selAutomaton(t, mgr, c.status, "feature")
				if c.status == autonomous.PRDFailed {
					selAccept(t, mgr, p, postShipGit(t, repo, "rev-parse", "feature"))
				}
			}
			before := switchHead(t, repo)
			run, msg := selRun(t, s, switchMergeBody)
			if run.Result != autonomous.MergeRunFailed || !strings.Contains(msg, c.why) || !strings.Contains(run.Reason, c.why) {
				t.Errorf("result=%s reason=%q error=%q, want a refusal saying %q", run.Result, run.Reason, msg, c.why)
			}
			if after := switchHead(t, repo); after != before {
				t.Errorf("refs changed on a refused run:\n%s%s", before, after)
			}
			if len(ship.calls) != 0 {
				t.Errorf("ship calls on a refused run: %v", ship.calls)
			}
		})
	}
}

// A push that did not reach the remote leaves the landing pending and the
// Automaton unchanged, whatever the push step said.
func TestMergeRunPushNotOnRemoteStaysPending(t *testing.T) {
	s, mgr, repo, _, _, _ := switchServer(t) // the spy's Push sends nothing
	s.cfg.Autonomous.MergeDeployRunEnabled = true
	s.cfg.Autonomous.MergePipeline.PostShip = config.PostShipConfig{Enabled: true,
		Hooks: []config.PostShipHook{{Name: "mark", Builtin: "mark_completed", Enabled: true}}}
	p := selAutomaton(t, mgr, autonomous.PRDNeedsReview, "feature")
	selAccept(t, mgr, p, postShipGit(t, repo, "rev-parse", "feature"))
	run, _ := selRun(t, s, switchMergeBody)
	if selStep(run, "push-release") != autonomous.MergeStepOK {
		t.Fatalf("the run did not reach the push: %+v", run.Steps)
	}
	if got := selStep(run, "record-landing"); got != autonomous.MergeStepFailed {
		t.Errorf("record-landing = %q, want failed", got)
	}
	got, _ := mgr.Store().GetPRD(p.ID)
	if got.Landing.State != autonomous.LandingPending || got.IsShipped() || got.Status != autonomous.PRDNeedsReview {
		t.Errorf("landing=%s shipped=%v status=%s, want pending and unchanged", got.Landing.State, got.IsShipped(), got.Status)
	}
}

// A confirmed push gives landed, completed and shipped together, for accepted
// needs_review work, with no post-ship hook configured and no Automaton named
// in the request.
func TestMergeRunConfirmedPushRecordsLanding(t *testing.T) {
	s, mgr, repo, _, _, _ := switchServer(t)
	s.cfg.Autonomous.MergeDeployRunEnabled = true
	s.cfg.Autonomous.Deploy = config.DeployConfig{}
	s.mergeShip = mergepipeline.ExecShipRunner{} // really pushes to the sandbox remote
	p := selAutomaton(t, mgr, autonomous.PRDNeedsReview, "feature")
	selAccept(t, mgr, p, postShipGit(t, repo, "rev-parse", "feature"))
	run, msg := selRun(t, s, switchMergeBody)
	if run.Result != autonomous.MergeRunOK {
		t.Fatalf("run: %s %s %+v", run.Result, msg, run.Steps)
	}
	remote := strings.Fields(postShipGit(t, repo, "ls-remote", "origin", "refs/heads/main"))[0]
	got, _ := mgr.Store().GetPRD(p.ID)
	if got.Status != autonomous.PRDCompleted || got.Landing.State != autonomous.LandingLanded || got.Landing.Commit != remote ||
		got.MergeCommit != remote || got.MergedInRelease == "" || got.ShippedSource != autonomous.ShippedSourceMergeStep {
		t.Fatalf("status=%s landing=%+v shipped=%q release=%q source=%q, remote main %s",
			got.Status, got.Landing, got.MergeCommit, got.MergedInRelease, got.ShippedSource, remote)
	}
}

// The branch moving between the rule's answer and the merge stops the run: an
// acceptance of commit X never covers a later commit.
func TestMergeRunRefusesBranchMovedAfterSelection(t *testing.T) {
	repo := newPostShipRepo(t)
	before := postShipGit(t, repo, "rev-parse", "main")
	ship := &postShipSpyShip{}
	r := &mergepipeline.Runner{Dir: repo, Project: "demo", Ship: ship,
		VersionFiles: []string{"cmd/datawatch/main.go", "internal/server/api.go"},
		Selection:    staleSelection{tip: before}}
	res, err := r.Run(context.Background(), mergepipeline.RunRequest{Title: "t", Branches: []string{"feature"}})
	if err == nil || !strings.Contains(err.Error(), "moved since it was selected") {
		t.Fatalf("err = %v (%+v)", err, res.Run.Steps)
	}
	if postShipGit(t, repo, "rev-parse", "main") != before || len(ship.calls) != 0 {
		t.Errorf("the run merged or shipped a commit that was never selected: %v", ship.calls)
	}
}

type staleSelection struct{ tip string }

func (s staleSelection) SelectMergeRun(context.Context, []string, string) (autonomous.MergeSelectionEntry, error) {
	return autonomous.MergeSelectionEntry{AutomatonID: "a1", Eligible: true, Tip: s.tip}, nil
}

// The supervisor job merges only what the rule selects, one Automaton per run,
// and says why it left the rest.
func TestSupervisorLoopMergeUsesTheRule(t *testing.T) {
	repo := newPostShipRepo(t)
	_, mgr := liveAPIServer(t, repo)
	postShipGit(t, repo, "branch", "bad", "feature")
	good := selAutomaton(t, mgr, autonomous.PRDNeedsReview, "feature")
	selAccept(t, mgr, good, postShipGit(t, repo, "rev-parse", "feature"))
	bad := selAutomaton(t, mgr, autonomous.PRDFailed, "bad")
	sel := autonomous.MergeSelection{}
	for _, id := range []string{good.ID, bad.ID} {
		if e := mgr.MergeEligibility(context.Background(), id); e.Eligible {
			sel.Eligible = append(sel.Eligible, e)
		} else {
			sel.Excluded = append(sel.Excluded, e)
		}
	}
	out, err := supervisorLoopMergeSelected(context.Background(), mgr, t.TempDir(),
		config.MergePipelineConfig{VersionFiles: []string{"cmd/datawatch/main.go", "internal/server/api.go"}},
		sel, func(r *mergepipeline.Runner) { r.Ship = mergepipeline.ExecShipRunner{} })
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(out.Merged, ",") != "feature" {
		t.Errorf("merged = %v, want only feature", out.Merged)
	}
	if len(out.Excluded) != 1 || !strings.Contains(out.Excluded[0], bad.ID) || !strings.Contains(out.Excluded[0], "failed work is never merged") {
		t.Errorf("excluded = %v", out.Excluded)
	}
	runs := mgr.MergeRuns(0)
	if len(runs) != 1 || strings.Join(runs[0].Branches, ",") != "feature" {
		t.Errorf("runs = %+v, want one run for one Automaton", runs)
	}
	got, _ := mgr.Store().GetPRD(good.ID)
	if got.Landing.State != autonomous.LandingLanded || !got.IsShipped() || got.Status != autonomous.PRDCompleted {
		t.Errorf("landing=%s shipped=%v status=%s", got.Landing.State, got.IsShipped(), got.Status)
	}
}
