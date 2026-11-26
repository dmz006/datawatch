package server

// The daemon's own merge runs are isolated: local main moves only after a
// confirmed push, release facts come from the Automaton, and the conflict
// model is checked against the real registry. Sandboxes with local bare
// remotes only.

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/mergepipeline"
)

var isolateVersionFiles = []string{"cmd/datawatch/main.go", "internal/server/api.go"}

type isolateLookup map[string]string

func (l isolateLookup) ResolveKind(n string) (string, bool) { k, ok := l[n]; return k, ok }

func TestDaemonMergeRunnerIsIsolated(t *testing.T) {
	_, mgr := liveAPIServer(t, newPostShipRepo(t))
	data := t.TempDir()
	r := newMergeRunner(autonomous.NewAPI(mgr), config.MergePipelineConfig{VersionFiles: isolateVersionFiles}, mgr.MergeBaseRepo(), "demo", data, nil)
	if !r.Isolate || r.ScratchRoot != filepath.Join(data, "merge-pipeline", "scratch") {
		t.Errorf("isolate=%v scratch=%q", r.Isolate, r.ScratchRoot)
	}
}

func TestDaemonMergeRunnerChecksTheConflictModelAgainstTheRegistry(t *testing.T) {
	_, mgr := liveAPIServer(t, newPostShipRepo(t))
	mgr.SetMergeConflictModel(isolateLookup{"sonnet-entry": "claude-code"}, nil)
	for name, want := range map[string]mergepipeline.ConflictPolicy{
		"sonnet-entry":      mergepipeline.PolicyMechanicalAndModel,
		"claude-sonnet-5-5": mergepipeline.PolicyStopAndAsk, // a model name, not an entry
	} {
		n := name
		r := newMergeRunner(autonomous.NewAPI(mgr), config.MergePipelineConfig{ConflictModel: &n}, mgr.MergeBaseRepo(), "demo", t.TempDir(), nil)
		if got := r.ConflictSettings().Policy; got != want {
			t.Errorf("%s: policy %q, want %q", name, got, want)
		}
	}
}

func TestDaemonMergeRunnerSizesTheReleaseFromTheAutomaton(t *testing.T) {
	_, mgr := liveAPIServer(t, newPostShipRepo(t))
	r := newMergeRunner(autonomous.NewAPI(mgr), config.MergePipelineConfig{VersionFiles: isolateVersionFiles}, mgr.MergeBaseRepo(), "demo", t.TempDir(), nil)
	if r.ReleaseFor == nil {
		t.Fatal("no release facts are wired")
	}
	p := selAutomaton(t, mgr, autonomous.PRDNeedsReview, "feature")
	p.Kind = "feature"
	if err := mgr.Store().SavePRD(p); err != nil {
		t.Fatal(err)
	}
	if f, ok := r.ReleaseFor(p.ID); !ok || f.Size != "minor" || f.Approval != config.ReleaseApprovalAuto {
		t.Errorf("feature: %+v %v", f, ok)
	}
	p.BreakingChange = true
	if err := mgr.Store().SavePRD(p); err != nil {
		t.Fatal(err)
	}
	if f, _ := r.ReleaseFor(p.ID); f.Approval != config.ReleaseApprovalAsk {
		t.Errorf("a breaking change must ask: %+v", f)
	}
}

// eligibleSelection is one accepted Automaton on branch feature.
func eligibleSelection(t *testing.T, mgr *autonomous.Manager, repo string) (*autonomous.PRD, autonomous.MergeSelection) {
	t.Helper()
	p := selAutomaton(t, mgr, autonomous.PRDNeedsReview, "feature")
	p.Title, p.Kind = "Add the export button", "bug"
	if err := mgr.Store().SavePRD(p); err != nil {
		t.Fatal(err)
	}
	selAccept(t, mgr, p, postShipGit(t, repo, "rev-parse", "feature"))
	e := mgr.MergeEligibility(context.Background(), p.ID)
	if !e.Eligible {
		t.Fatalf("fixture: %v", e.Reasons)
	}
	return p, autonomous.MergeSelection{Eligible: []autonomous.MergeSelectionEntry{e}}
}

func isolateRefs(t *testing.T, repo string) string {
	t.Helper()
	return switchHead(t, repo) + postShipGit(t, repo, "ls-remote", "origin")
}

func TestSupervisorLoopHeldMergeLeavesLocalMain(t *testing.T) {
	repo := newPostShipRepo(t)
	_, mgr := liveAPIServer(t, repo)
	p, sel := eligibleSelection(t, mgr, repo)
	before := isolateRefs(t, repo)
	_, err := supervisorLoopMergeSelected(context.Background(), mgr, t.TempDir(),
		config.MergePipelineConfig{VersionFiles: isolateVersionFiles,
			ReleaseApproval: config.ReleaseApprovalModes{Patch: config.ReleaseApprovalManual}}, sel)
	if err != nil {
		t.Fatal(err)
	}
	if after := isolateRefs(t, repo); after != before {
		t.Errorf("a held run changed refs:\n%s\n%s", before, after)
	}
	if got, _ := mgr.Store().GetPRD(p.ID); got.Landing.State != autonomous.LandingPending {
		t.Errorf("landing = %s", got.Landing.State)
	}
}

func TestSupervisorLoopMergeUsesTheAutomatonTitleAndRecordsIt(t *testing.T) {
	repo := newPostShipRepo(t)
	_, mgr := liveAPIServer(t, repo)
	p, sel := eligibleSelection(t, mgr, repo)
	if _, err := supervisorLoopMergeSelected(context.Background(), mgr, t.TempDir(),
		config.MergePipelineConfig{VersionFiles: isolateVersionFiles}, sel); err != nil {
		t.Fatalf("%v %+v", err, mgr.MergeRuns(0))
	}
	if got := postShipGit(t, repo, "show", "origin/main:CHANGELOG.md"); !strings.Contains(got, "## v1.2.4 — Add the export button") {
		t.Errorf("changelog: %s", got)
	}
	if runs := mgr.MergeRuns(0); len(runs) != 1 || runs[0].AutomatonID != p.ID {
		t.Errorf("runs = %+v, want one naming %s", runs, p.ID)
	}
	if postShipGit(t, repo, "rev-parse", "main") != postShipGit(t, repo, "rev-parse", "origin/main") {
		t.Error("local main is not at the pushed commit")
	}
}

// The loop's runs ask again, just before the push, whether merging is allowed.
func TestSupervisorLoopMergeRefusesToPushWithTheSwitchOff(t *testing.T) {
	repo := newPostShipRepo(t)
	_, mgr := liveAPIServer(t, repo)
	_, sel := eligibleSelection(t, mgr, repo)
	before := isolateRefs(t, repo)
	cfg := &config.Config{DataDir: t.TempDir()}
	// The switch went off after the loop started this run.
	cfg.Autonomous.MergeDeployRunEnabled = false
	_, err := supervisorLoopMergeSelected(context.Background(), mgr, t.TempDir(),
		config.MergePipelineConfig{VersionFiles: isolateVersionFiles}, sel, supervisorMergeOption(cfg))
	if err == nil || !strings.Contains(err.Error(), config.MergeDeployRunKey) {
		t.Fatalf("err = %v, want a refusal naming the switch", err)
	}
	if after := isolateRefs(t, repo); after != before {
		t.Errorf("refs changed:\n%s\n%s", before, after)
	}
}
