package autonomous

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/mergeshadow"
	"github.com/dmz006/datawatch/internal/pause"
)

// meRig is a sandbox: a repository with a local bare "remote" and a manager
// whose merge base repository it is.
type meRig struct {
	t      *testing.T
	m      *Manager
	repo   string
	origin string
}

func newMERig(t *testing.T) *meRig {
	t.Helper()
	repo := newTestRepo(t)
	origin := filepath.Join(t.TempDir(), "origin.git")
	gitRun(t, repo, "init", "-q", "--bare", origin)
	gitRun(t, repo, "remote", "add", "origin", origin)
	gitRun(t, repo, "push", "-q", "origin", "main")
	cfg := DefaultConfig()
	cfg.WorktreeBaseRepo = repo
	m, err := NewManager(t.TempDir(), cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	return &meRig{t: t, m: m, repo: repo, origin: origin}
}

func (r *meRig) rev(ref string) string {
	r.t.Helper()
	out, err := NewMergeGitRunner().Run(context.Background(), r.repo, "rev-parse", ref)
	if err != nil {
		r.t.Fatalf("rev-parse %s: %v %s", ref, err, out)
	}
	return strings.TrimSpace(out)
}

// commitOn adds one commit to branch (created from main when new) and
// returns its id. main stays checked out.
func (r *meRig) commitOn(branch, file string) string {
	r.t.Helper()
	if _, err := NewMergeGitRunner().Run(context.Background(), r.repo, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch); err != nil {
		gitRun(r.t, r.repo, "branch", branch, "main")
	}
	gitRun(r.t, r.repo, "checkout", "-q", branch)
	if err := os.WriteFile(filepath.Join(r.repo, file), []byte(file+"\n"), 0o644); err != nil {
		r.t.Fatal(err)
	}
	gitRun(r.t, r.repo, "add", file)
	gitRun(r.t, r.repo, "commit", "-q", "-m", "work "+file)
	sha := r.rev("HEAD")
	gitRun(r.t, r.repo, "checkout", "-q", "main")
	return sha
}

// automaton makes a finished-looking Automaton with one completed code task
// and a branch holding one commit.
func (r *meRig) automaton(status PRDStatus) (*PRD, string) {
	r.t.Helper()
	p, err := r.m.store.CreatePRD("spec", "", "", "", "")
	if err != nil {
		r.t.Fatal(err)
	}
	branch := "automaton/" + p.ID
	tip := r.commitOn(branch, "f-"+p.ID)
	p.Status = status
	p.Git.Branch = branch
	p.Landing = &Landing{State: LandingPending, Actor: "autonomous", At: time.Now(), Reason: "branch created"}
	p.Story = []Story{{ID: "s1", Tasks: []Task{{ID: "t1", Title: "code", Status: TaskCompleted}}}}
	r.save(p)
	return p, tip
}

func (r *meRig) save(p *PRD) {
	r.t.Helper()
	if err := r.m.store.SavePRD(p); err != nil {
		r.t.Fatal(err)
	}
}

func meReview(outcome, commit string) GuardrailVerdict {
	return GuardrailVerdict{Guardrail: "rules-review", Outcome: outcome, Summary: outcome, VerdictAt: time.Now(), JudgedCommitSHA: commit}
}

func meAccept(commit string) *MergeAcceptance {
	return &MergeAcceptance{Commit: commit, Actor: "operator", Reason: "reviewed by hand", At: time.Now()}
}

type mePause struct{}

func (mePause) Blocked(string) (pause.Block, bool) {
	return pause.Block{Scope: pause.ScopeGlobal, Reason: "test pause"}, true
}

// TestMergeEligibilityTable is the sandbox proof: one record per state, and
// the eligible list matches the expected table. Every exclusion says why.
func TestMergeEligibilityTable(t *testing.T) {
	r := newMERig(t)
	type row struct {
		name   string
		status PRDStatus
		set    func(p *PRD, tip string)
		basis  string // "" = excluded
		why    string // substring of the exclusion reasons
	}
	passed := func(p *PRD, tip string) { p.Verdicts = []GuardrailVerdict{meReview("pass", tip)} }
	acceptedPass := func(mut func(p *PRD)) func(p *PRD, tip string) {
		return func(p *PRD, tip string) { passed(p, tip); p.MergeAcceptance = meAccept(tip); mut(p) }
	}
	timeout := GuardrailVerdict{Guardrail: "rules-review", Outcome: "block", Category: JudgeTimeoutCategory, Summary: "judge timed out", VerdictAt: time.Now(),
		JudgeAttempts: []JudgeAttemptRecord{{Backend: "b", Outcome: JudgeOutcomeTimedOut}}}
	if !IsJudgeTimeoutHold(timeout) {
		t.Fatal("fixture is not a judge-timeout hold")
	}
	other := strings.Repeat("a", 40)
	rows := []row{
		{name: "completed and passed", status: PRDCompleted, set: passed, basis: MergeBasisReviewPassed},
		{name: "passed on another commit", status: PRDCompleted, set: func(p *PRD, _ string) { p.Verdicts = []GuardrailVerdict{meReview("pass", other)} }, why: "passed on commit"},
		{name: "passed with no commit", status: PRDCompleted, set: func(p *PRD, _ string) { p.Verdicts = []GuardrailVerdict{meReview("pass", "")} }, why: "records no commit"},
		{name: "task pass only", status: PRDCompleted, set: func(p *PRD, tip string) { p.Story[0].Tasks[0].Verdicts = []GuardrailVerdict{meReview("pass", tip)} }, why: "completion review"},
		{name: "review pending", status: PRDCompleted, set: func(*PRD, string) {}, why: "review is pending"},
		{name: "review pending accepted", status: PRDCompleted, set: func(p *PRD, tip string) { p.MergeAcceptance = meAccept(tip) }, basis: MergeBasisAccepted},
		{name: "review timed out", status: PRDCompleted, set: func(p *PRD, _ string) { p.Verdicts = []GuardrailVerdict{timeout} }, why: "timed_out"},
		{name: "review timed out accepted", status: PRDCompleted, set: func(p *PRD, tip string) { p.Verdicts = []GuardrailVerdict{timeout}; p.MergeAcceptance = meAccept(tip) }, basis: MergeBasisAccepted},
		{name: "needs_review passed, not accepted", status: PRDNeedsReview, set: passed, why: "needs_review"},
		{name: "needs_review accepted", status: PRDNeedsReview, set: func(p *PRD, tip string) { p.MergeAcceptance = meAccept(tip) }, basis: MergeBasisAccepted},
		{name: "acceptance of another commit", status: PRDNeedsReview, set: func(p *PRD, _ string) { p.MergeAcceptance = meAccept(other) }, why: "acceptance is for commit"},
		{name: "review blocked accepted", status: PRDCompleted, set: func(p *PRD, tip string) {
			p.Verdicts = []GuardrailVerdict{meReview("block", tip)}
			p.MergeAcceptance = meAccept(tip)
		}, why: "review is blocked"},
		{name: "block hidden by a later task pass", status: PRDCompleted, set: func(p *PRD, tip string) {
			p.Verdicts = []GuardrailVerdict{meReview("block", tip)}
			later := meReview("pass", tip)
			later.VerdictAt = time.Now().Add(time.Hour)
			p.Story[0].Tasks[0].Verdicts = []GuardrailVerdict{later}
			p.MergeAcceptance = meAccept(tip)
		}, why: "review is blocked"},
		{name: "failed", status: PRDFailed, set: acceptedPass(func(*PRD) {}), why: "failed work is never merged"},
		{name: "blocked", status: PRDBlocked, set: acceptedPass(func(*PRD) {}), why: "blocked work is never merged"},
		{name: "cancelled", status: PRDCancelled, set: acceptedPass(func(*PRD) {}), why: "cancelled work is never merged"},
		{name: "archived", status: PRDArchived, set: acceptedPass(func(*PRD) {}), why: "archived work is never merged"},
		{name: "rejected", status: PRDRejected, set: acceptedPass(func(*PRD) {}), why: "rejected work is never merged"},
		{name: "still running", status: PRDRunning, set: acceptedPass(func(*PRD) {}), why: "not finished"},
		{name: "held status", status: PRDHeld, set: acceptedPass(func(*PRD) {}), why: "hold"},
		{name: "hold record", status: PRDCompleted, set: acceptedPass(func(p *PRD) { p.Hold = &HoldState{} }), why: "hold"},
		{name: "paused status", status: PRDPaused, set: acceptedPass(func(*PRD) {}), why: "paused"},
		{name: "uncommitted work", status: PRDCompleted, set: acceptedPass(func(p *PRD) { p.UncommittedWork = &UncommittedWorkHold{} }), why: "uncommitted"},
		{name: "code task open", status: PRDNeedsReview, set: acceptedPass(func(p *PRD) { p.Story[0].Tasks[0].Status = TaskPending }), why: "code task t1"},
		{name: "code task failed", status: PRDCompleted, set: acceptedPass(func(p *PRD) { p.Story[0].Tasks[0].Status = TaskFailed }), why: "code task t1"},
		{name: "no tasks done", status: PRDNeedsReview, set: acceptedPass(func(p *PRD) { p.Story = nil }), why: "no completed code task"},
		{name: "already shipped", status: PRDCompleted, set: acceptedPass(func(p *PRD) { p.MergeCommit = other }), why: "shipped record"},
		{name: "already landed", status: PRDCompleted, set: acceptedPass(func(p *PRD) { p.Landing.State = LandingLanded }), why: "landed"},
		{name: "kept", status: PRDCompleted, set: acceptedPass(func(p *PRD) { p.Landing.State = LandingKept }), why: "kept"},
		{name: "dropped", status: PRDCompleted, set: acceptedPass(func(p *PRD) { p.Landing.State = LandingDropped }), why: "dropped"},
		{name: "landing by hand", status: PRDCompleted, set: func(p *PRD, tip string) {
			passed(p, tip)
			p.Landing.Reason = landByHandReason
		}, why: "by hand"},
		{name: "superseded", status: PRDCompleted, set: acceptedPass(func(p *PRD) { p.ClosedOutcome = OutcomeSuperseded }), why: "superseded"},
		{name: "other repository", status: PRDCompleted, set: acceptedPass(func(p *PRD) { p.Git.Branch = "automaton/not-here" }), why: "not in the merge repository"},
		{name: "unmerged task branch", status: PRDCompleted, set: acceptedPass(func(p *PRD) {
			tb := p.Git.Branch + "--task"
			r.commitOn(tb, "task-"+p.ID)
			p.Story[0].Tasks[0].WorktreeBranch = tb
			p.Story[0].Tasks[0].MergeState = MergeStatePending
		}), why: "task branch"},
	}
	var want, got []string
	for _, c := range rows {
		p, tip := r.automaton(c.status)
		c.set(p, tip)
		r.save(p)
		e := r.m.MergeEligibility(context.Background(), p.ID)
		if c.basis != "" {
			want = append(want, c.name)
		}
		if e.Eligible {
			got = append(got, c.name)
		}
		switch {
		case c.basis != "":
			if !e.Eligible || e.Basis != c.basis || e.Tip != tip || len(e.Reasons) != 0 {
				t.Errorf("%s: %+v, want eligible by %s at %s", c.name, e, c.basis, tip)
			}
		case e.Eligible:
			t.Errorf("%s: selected, want excluded (%s)", c.name, c.why)
		case !strings.Contains(strings.Join(e.Reasons, "; "), c.why):
			t.Errorf("%s: reasons %q do not say %q", c.name, e.Reasons, c.why)
		}
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("eligible list\n got: %v\nwant: %v", got, want)
	}
}

func TestMergeEligibilityOperatorPause(t *testing.T) {
	r := newMERig(t)
	p, tip := r.automaton(PRDCompleted)
	p.Verdicts = []GuardrailVerdict{meReview("pass", tip)}
	r.save(p)
	r.m.SetPauseGate(mePause{})
	e := r.m.MergeEligibility(context.Background(), p.ID)
	if e.Eligible || !strings.Contains(strings.Join(e.Reasons, ";"), "test pause") {
		t.Fatalf("paused project: %+v", e)
	}
}

func TestMergeAcceptanceCoversOnlyItsCommit(t *testing.T) {
	r := newMERig(t)
	ctx := context.Background()
	p, x := r.automaton(PRDNeedsReview)
	if _, err := r.m.AcceptForMerge(ctx, p.ID, x, "operator", "checked the diff"); err != nil {
		t.Fatal(err)
	}
	if e := r.m.MergeEligibility(ctx, p.ID); !e.Eligible || e.Basis != MergeBasisAccepted {
		t.Fatalf("accepted commit X: %+v", e)
	}
	got, _ := r.m.store.GetPRD(p.ID)
	last := got.Decisions[len(got.Decisions)-1]
	if last.Kind != DecisionMergeAccept || last.Actor != "operator" || !strings.Contains(last.Note, x) || !strings.Contains(last.Note, "checked the diff") {
		t.Fatalf("decision record = %+v", last)
	}
	next := r.commitOn(p.Git.Branch, "later-"+p.ID)
	e := r.m.MergeEligibility(ctx, p.ID)
	if e.Eligible || !strings.Contains(strings.Join(e.Reasons, ";"), "acceptance is for commit") {
		t.Fatalf("acceptance of X must not cover X+1 (%s): %+v", next, e)
	}
}

func TestMergeAcceptanceRefusals(t *testing.T) {
	r := newMERig(t)
	ctx := context.Background()
	p, tip := r.automaton(PRDNeedsReview)
	old := r.rev("main")
	for name, c := range map[string][3]string{
		"short commit": {tip[:12], "operator", "r"},
		"not the tip":  {old, "operator", "r"},
		"branch name":  {p.Git.Branch, "operator", "r"},
		"no actor":     {tip, " ", "r"},
		"no reason":    {tip, "operator", ""},
	} {
		if _, err := r.m.AcceptForMerge(ctx, p.ID, c[0], c[1], c[2]); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	for _, st := range []PRDStatus{PRDFailed, PRDBlocked, PRDCancelled, PRDArchived, PRDRunning} {
		q, qtip := r.automaton(st)
		if _, err := r.m.AcceptForMerge(ctx, q.ID, qtip, "operator", "r"); err == nil {
			t.Errorf("%s: accepted", st)
		}
	}
	b, btip := r.automaton(PRDCompleted)
	b.Verdicts = []GuardrailVerdict{meReview("block", btip)}
	r.save(b)
	if _, err := r.m.AcceptForMerge(ctx, b.ID, btip, "operator", "r"); err == nil {
		t.Error("review blocked: accepted")
	}
	// A session doing Automaton work is held like other work: it cannot
	// accept work for merge.
	w, _ := r.automaton(PRDRunning)
	w.Story[0].Tasks[0].SessionID = "host-ab12"
	r.save(w)
	if _, err := r.m.AcceptForMerge(ctx, p.ID, tip, "session:host-ab12", "my own work is fine"); err == nil {
		t.Error("an Automaton worker session accepted work for merge")
	}
	if _, err := r.m.AcceptForMerge(ctx, p.ID, tip, "session:host-sup1", "supervisor checked"); err != nil {
		t.Errorf("the supervisor session: %v", err)
	}
}

func TestSelectMergeRun(t *testing.T) {
	r := newMERig(t)
	ctx := context.Background()
	good, tip := r.automaton(PRDCompleted)
	good.Verdicts = []GuardrailVerdict{meReview("pass", tip)}
	r.save(good)
	bad, _ := r.automaton(PRDFailed)
	e, err := r.m.SelectMergeRun(ctx, []string{good.Git.Branch}, "")
	if err != nil || e.AutomatonID != good.ID || e.Tip != tip {
		t.Fatalf("eligible branch: %+v %v", e, err)
	}
	gitRun(t, r.repo, "branch", "hand/feature", "main")
	for name, c := range map[string]struct {
		branches []string
		id, why  string
	}{
		"failed":             {[]string{bad.Git.Branch}, "", "failed work is never merged"},
		"two branches":       {[]string{good.Git.Branch, bad.Git.Branch}, "", "one Automaton per run"},
		"no Automaton":       {[]string{"hand/feature"}, "", "no Automaton"},
		"wrong Automaton id": {[]string{good.Git.Branch}, bad.ID, bad.ID},
	} {
		if _, err := r.m.SelectMergeRun(ctx, c.branches, c.id); err == nil || !strings.Contains(err.Error(), c.why) {
			t.Errorf("%s: err = %v, want it to say %q", name, err, c.why)
		}
	}
}

// mergeLocally merges branch into the sandbox's local main without pushing.
func (r *meRig) mergeLocally(branch string) string {
	r.t.Helper()
	gitRun(r.t, r.repo, "merge", "-q", "--no-ff", "-m", "Merge "+branch, branch)
	return r.rev("HEAD")
}

func TestRecordMergeLandingNeedsTheCommitOnTheRemote(t *testing.T) {
	r := newMERig(t)
	ctx := context.Background()
	p, tip := r.automaton(PRDNeedsReview)
	commit := r.mergeLocally(p.Git.Branch)
	pending := func(why string) {
		t.Helper()
		got, _ := r.m.store.GetPRD(p.ID)
		if got.Landing.State != LandingPending || got.IsShipped() || got.Status != PRDNeedsReview {
			t.Fatalf("%s: landing=%s shipped=%v status=%s, want pending and unchanged", why, got.Landing.State, got.IsShipped(), got.Status)
		}
	}
	if _, err := r.m.RecordMergeLanding(ctx, p.ID, "v1.0.1", commit, time.Now()); err == nil {
		t.Fatal("recorded a landing the remote does not have")
	}
	pending("held or failed push")
	// The remote has a commit that does not contain the work.
	if _, err := r.m.RecordMergeLanding(ctx, p.ID, "v1.0.1", r.rev("origin/main"), time.Now()); err == nil {
		t.Fatal("recorded a commit that does not contain the work")
	}
	pending("wrong commit")
	gitRun(t, r.repo, "push", "-q", "origin", "main")
	got, err := r.m.RecordMergeLanding(ctx, p.ID, "v1.0.1", commit, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != PRDCompleted || got.Landing.State != LandingLanded || got.Landing.Commit != commit ||
		got.MergeCommit != commit || got.MergedInRelease != "v1.0.1" || got.ShippedSource != ShippedSourceMergeStep || got.MergedAt == nil {
		t.Fatalf("after a confirmed push (tip %s): status=%s landing=%+v shipped=%q %q %q", tip, got.Status, got.Landing, got.MergeCommit, got.MergedInRelease, got.ShippedSource)
	}
	n := len(got.Decisions)
	again, err := r.m.RecordMergeLanding(ctx, p.ID, "v1.0.1", commit, time.Now())
	if err != nil || len(again.Decisions) != n {
		t.Fatalf("recording twice: %v, decisions %d -> %d", err, n, len(again.Decisions))
	}
}

func TestRecordMergeLandingWithoutRemoteStaysPending(t *testing.T) {
	r := newMERig(t)
	p, _ := r.automaton(PRDCompleted)
	commit := r.mergeLocally(p.Git.Branch)
	gitRun(t, r.repo, "remote", "remove", "origin")
	if _, err := r.m.RecordMergeLanding(context.Background(), p.ID, "v1.0.1", commit, time.Now()); err == nil {
		t.Fatal("recorded a landing with no remote to confirm it")
	}
}

func TestLandingAncestorNeedsTheRemote(t *testing.T) {
	r := newMERig(t)
	ctx := context.Background()
	p, _ := r.automaton(PRDCompleted)
	r.mergeLocally(p.Git.Branch)
	if r.m.CheckLandingAncestor(ctx, p.ID) {
		t.Fatal("merged on local main only, but reported landed")
	}
	gitRun(t, r.repo, "push", "-q", "origin", "main")
	if !r.m.CheckLandingAncestor(ctx, p.ID) {
		t.Fatal("pushed, but not reported landed")
	}
}

func TestShippedMergeStepNeedsTheCommitOnTheRemote(t *testing.T) {
	r := newMERig(t)
	p, _ := r.automaton(PRDCompleted)
	commit := r.mergeLocally(p.Git.Branch)
	if _, err := r.m.RecordPRDShipped(p.ID, "v1.0.1", time.Now(), commit, ShippedSourceMergeStep); err == nil {
		t.Fatal("shipped record accepted for a commit that is not on the remote")
	}
	if got, _ := r.m.store.GetPRD(p.ID); got.IsShipped() {
		t.Fatal("refused record was stored")
	}
	gitRun(t, r.repo, "push", "-q", "origin", "main")
	got, err := r.m.RecordPRDShipped(p.ID, "v1.0.1", time.Now(), commit, ShippedSourceMergeStep)
	if err != nil {
		t.Fatal(err)
	}
	if got.Landing.State != LandingLanded || got.Landing.Commit != commit {
		t.Fatalf("a confirmed shipped record lands too: %+v", got.Landing)
	}
}

// The shadow merge plan carries the same rule's answer, with its reasons.
func TestShadowPlanCarriesTheSelection(t *testing.T) {
	r := newMERig(t)
	r.m.dataDir = t.TempDir()
	p, _ := r.automaton(PRDCompleted)
	p.ProjectDir = r.repo
	r.save(p)
	got := make(chan *mergeshadow.PlanSelection, 1)
	prev := mergeShadowRun
	mergeShadowRun = func(_ context.Context, in mergeshadow.Input) (*mergeshadow.MergePlan, error) {
		got <- in.Selection
		return nil, nil
	}
	t.Cleanup(func() { mergeShadowRun = prev })
	done := r.m.startMergeShadow(p)
	if done == nil {
		t.Fatal("no shadow run started")
	}
	<-done
	sel := <-got
	if sel == nil || sel.Eligible || !strings.Contains(strings.Join(sel.Reasons, ";"), "review is pending") {
		t.Fatalf("selection on the plan = %+v", sel)
	}
	plan, err := mergeshadow.Runner{Gates: func(context.Context, string, time.Duration) []mergeshadow.GateResult { return nil }}.Run(context.Background(),
		mergeshadow.Input{AutomatonID: p.ID, Project: "demo", RepoPath: r.repo, Branch: p.Git.Branch, BaseRef: "main", DataDir: r.m.dataDir, Selection: sel})
	if err != nil || plan.Selection != sel {
		t.Fatalf("stored plan: %+v %v", plan, err)
	}
}

// A finding that says "land it" also says what the rule allows, and why not.
func TestFinishedWorkFindingSaysWhetherDatawatchMayMergeIt(t *testing.T) {
	r := newMERig(t)
	p, _ := r.automaton(PRDCompleted)
	sw := r.m.FinishedWorkSweep()
	if sw == nil {
		t.Fatal("no finished-work store")
	}
	f := r.m.finishedWorkFinding(p, fwCandidate{cause: FinishedWorkNotMerged, since: time.Now()}, 1, time.Now())
	r.m.actOnFinishedWork(sw, FinishedWorkShadow, p, &f, fwCandidate{cause: FinishedWorkNotMerged}, time.Now(), &FinishedWorkPass{})
	stored, ok := sw.Get(p.ID)
	if !ok || !strings.Contains(stored.MergeSelection, "datawatch will not merge it: its review is pending") {
		t.Fatalf("finding = %+v", stored)
	}
	var b strings.Builder
	if err := RenderFinishedWorkDigest(&b, &FinishedWorkDigest{Waiting: []FinishedWorkFinding{stored}}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(b.String(), stored.MergeSelection) {
		t.Errorf("digest text lacks the answer:\n%s", b.String())
	}
}

// Work that can never be merged must not read as if an acceptance would help,
// and a task with no recorded status still says what is wrong.
func TestMergeExclusionWording(t *testing.T) {
	r := newMERig(t)
	p, _ := r.automaton(PRDFailed)
	p.Story[0].Tasks = append(p.Story[0].Tasks, Task{ID: "t2", Title: "more code"})
	r.save(p)
	got := strings.Join(r.m.MergeEligibility(context.Background(), p.ID).Reasons, "; ")
	if strings.Contains(got, "no acceptance") {
		t.Errorf("never-merged work suggests an acceptance: %s", got)
	}
	if !strings.Contains(got, "code task t2 is not completed") {
		t.Errorf("a task with no status: %s", got)
	}
}
