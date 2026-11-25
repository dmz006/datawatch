package autonomous

// From the independent review of the merge selection rule.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func meReasons(r *meRig, id string) string {
	return strings.Join(r.m.MergeEligibility(context.Background(), id).Reasons, "; ")
}

const meBlockedWhy = "its review is blocked"

// A real block stays a block when a later re-judge only times out. Driven
// through the real re-judge call on the same commit.
func TestRealBlockSurvivesATimedOutRejudge(t *testing.T) {
	r := newMERig(t)
	ctx := context.Background()
	cfg := r.m.Config()
	cfg.VerificationModel = "ollama/qwen3.8:27b"
	r.m.SetConfig(cfg)
	p, tip := r.automaton(PRDBlocked)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "AGENT.md"), []byte("rules"), 0o644); err != nil {
		t.Fatal(err)
	}
	block := meReview("block", tip)
	block.VerdictAt = time.Now().Add(-time.Hour)
	p.ProjectDir = dir
	p.PerPRDGuardrails = []string{"rules-review"}
	p.BlockedReason = "guardrail rules-review returned block"
	p.Verdicts = []GuardrailVerdict{block}
	r.save(p)

	r.m.SetGuardrail(newTimeoutJudge(t).fn)
	got, err := r.m.RejudgeCompletionGuardrail(ctx, p.ID, "operator", "")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != PRDNeedsReview || ReviewStateOf(got).State != ReviewStateTimedOut {
		t.Fatalf("fixture: status=%s review=%s, want a timeout hold", got.Status, ReviewStateOf(got).State)
	}
	if _, err := r.m.AcceptForMerge(ctx, p.ID, tip, "operator", "the judge only timed out"); err == nil || !strings.Contains(err.Error(), meBlockedWhy) {
		t.Errorf("accept after block then timeout: %v, want a refusal saying %q", err, meBlockedWhy)
	}
	got.MergeAcceptance = meAccept(tip)
	r.save(got)
	if e := r.m.MergeEligibility(ctx, p.ID); e.Eligible || !strings.Contains(meReasons(r, p.ID), meBlockedWhy) {
		t.Errorf("block then timeout is selected: %+v", e)
	}

	// A later real pass by the same guardrail does lift the block.
	pass := meReview("pass", tip)
	pass.VerdictAt = time.Now().Add(time.Minute)
	got.Verdicts = append(got.Verdicts, pass)
	got.Status = PRDCompleted
	r.save(got)
	if why := meReasons(r, p.ID); strings.Contains(why, meBlockedWhy) {
		t.Errorf("a later real pass did not lift the block: %s", why)
	}
}

// A block on one task or story is not hidden by a later pass elsewhere.
func TestUnitBlockIsNotHiddenByAnotherUnit(t *testing.T) {
	r := newMERig(t)
	at := func(outcome string, d time.Duration) GuardrailVerdict {
		v := meReview(outcome, "")
		v.VerdictAt = time.Now().Add(d)
		return v
	}
	timeout := at("block", -time.Hour)
	timeout.Category = JudgeTimeoutCategory
	for name, c := range map[string]struct {
		set     func(p *PRD)
		blocked bool
	}{
		"task block, later pass on another task": {func(p *PRD) {
			p.Story[0].Tasks[0].Verdicts = []GuardrailVerdict{at("block", -2*time.Hour)}
			p.Story[0].Tasks[1].Verdicts = []GuardrailVerdict{at("pass", -time.Minute)}
		}, true},
		"story block, later pass on a task": {func(p *PRD) {
			p.Story[0].Verdicts = []GuardrailVerdict{at("block", -2*time.Hour)}
			p.Story[0].Tasks[1].Verdicts = []GuardrailVerdict{at("pass", -time.Minute)}
		}, true},
		"task block, then a timeout on that task, then a pass elsewhere": {func(p *PRD) {
			p.Story[0].Tasks[0].Verdicts = []GuardrailVerdict{at("block", -2*time.Hour), timeout}
			p.Story[0].Tasks[1].Verdicts = []GuardrailVerdict{at("pass", -time.Minute)}
		}, true},
		"task block, then a pass on the same task": {func(p *PRD) {
			p.Story[0].Tasks[0].Verdicts = []GuardrailVerdict{at("block", -2*time.Hour), at("pass", -time.Hour)}
		}, false},
	} {
		p, tip := r.automaton(PRDCompleted)
		p.Story[0].Tasks = append(p.Story[0].Tasks, Task{ID: "t2", Title: "more code", Status: TaskCompleted})
		c.set(p)
		p.MergeAcceptance = meAccept(tip)
		r.save(p)
		why := meReasons(r, p.ID)
		if got := strings.Contains(why, meBlockedWhy); got != c.blocked {
			t.Errorf("%s: blocked=%v (%q), want %v", name, got, why, c.blocked)
		}
	}
}

// A real code fix is not "no code work" because its title reads like docs.
func TestRealFixWithADocsLikeTitleCanBeSelected(t *testing.T) {
	r := newMERig(t)
	for _, title := range []string{"Fix the documentation search crash", "Document upload parser: reject bad input"} {
		p, tip := r.automaton(PRDCompleted)
		p.Story[0].Tasks[0].Title = title
		p.Verdicts = []GuardrailVerdict{meReview("pass", tip)}
		r.save(p)
		if e := r.m.MergeEligibility(context.Background(), p.ID); !e.Eligible {
			t.Errorf("%q: excluded: %v", title, e.Reasons)
		}
	}
	// A finished gate task alone is still not completed work.
	p, tip := r.automaton(PRDNeedsReview)
	p.Story[0].Tasks[0].Kind = TaskKindFinalGate
	p.MergeAcceptance = meAccept(tip)
	r.save(p)
	if why := meReasons(r, p.ID); !strings.Contains(why, "no completed code task") {
		t.Errorf("only a gate task done: %q", why)
	}
}

// The completion migration does not call work landed while the merge exists
// only on the local default branch.
func TestCompletedMigrationNeedsTheRemote(t *testing.T) {
	r := newMERig(t)
	ctx := context.Background()
	p, _ := r.automaton(PRDCancelled)
	// Another commit on main first, so the picked commit gets its own id and
	// the branch reads merged by patch.
	gitRun(t, r.repo, "commit", "--allow-empty", "-q", "-m", "other")
	gitRun(t, r.repo, "cherry-pick", p.Git.Branch)
	proof := MergedMemberProof(ctx, NewMergeGitRunner(), r.repo, p.Git.Branch)
	if !proof.Merged {
		t.Fatalf("fixture: the local merge is not proven: %+v", proof)
	}
	would := func() bool {
		rep, err := r.m.MigrateCompleted(ctx, CompletedMigrationOpts{DryRun: true, ID: p.ID})
		if err != nil {
			t.Fatal(err)
		}
		return len(rep.WouldComplete) == 1
	}
	if would() {
		t.Fatal("merged on local main only, but the migration would complete it")
	}
	gitRun(t, r.repo, "push", "-q", "origin", "main")
	if !would() {
		t.Fatal("pushed, but the migration would not complete it")
	}
}

// A work directory that belongs to another repository bars the branch even
// when the merge repository has a branch of the same name.
func TestWorkDirectoryOfAnotherRepositoryIsBarred(t *testing.T) {
	r := newMERig(t)
	ctx := context.Background()
	p, tip := r.automaton(PRDCompleted)
	p.Verdicts = []GuardrailVerdict{meReview("pass", tip)}
	own := filepath.Join(t.TempDir(), "own")
	gitRun(t, r.repo, "worktree", "add", "-q", own, p.Git.Branch)
	p.ProjectDir = own
	r.save(p)
	if e := r.m.MergeEligibility(ctx, p.ID); !e.Eligible {
		t.Fatalf("a worktree of the merge repository: %v", e.Reasons)
	}
	p.ProjectDir = newTestRepo(t)
	r.save(p)
	if why := meReasons(r, p.ID); !strings.Contains(why, "belongs to another repository") {
		t.Fatalf("a work directory of another repository: %q", why)
	}
}
