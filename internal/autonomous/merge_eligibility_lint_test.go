package autonomous

// A lint-gate block lifts when the same unit passes the lint gate, and a lint
// pass never counts as a review pass.

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// meLintRig is an Automaton whose project directory is a worktree of the merge
// repository, with an untracked Go file to lint and passing verification
// scripts on its task and story.
func meLintRig(t *testing.T) (*meRig, *PRD, string) {
	t.Helper()
	r := newMERig(t)
	cfg := r.m.Config()
	cfg.PerTaskGuardrails = []string{"rules"}
	cfg.PerStoryGuardrails = []string{"rules"}
	cfg.VerificationModel = "ollama/qwen3.8:27b"
	r.m.SetConfig(cfg)
	r.m.SetGuardrail(fatalGuardrail(t))
	p, tip := r.automaton(PRDCompleted)
	wt := filepath.Join(t.TempDir(), "wt")
	gitRun(t, r.repo, "worktree", "add", "-q", wt, p.Git.Branch)
	writeLintFile(t, wt, "pkg/a/a.go", "package a\n\nfunc helper() {}\n")
	script := writeVerifyScript(t, wt, "ok.sh", "exit 0\n")
	p.ProjectDir = wt
	p.Story[0].VerificationScript = script
	p.Story[0].Tasks[0].VerificationScript = script
	r.save(p)
	return r, p, tip
}

func meLintFails(t *testing.T) {
	err := exitError(t)
	withFakeLint(t, func(context.Context, string, []string) (string, error) { return unusedFinding, err })
}

func meLintPasses(t *testing.T) {
	withFakeLint(t, func(context.Context, string, []string) (string, error) { return "", nil })
}

func meCountLint(vs []GuardrailVerdict, outcome string) int {
	n := 0
	for _, v := range vs {
		if v.Guardrail == "lint-gate" && v.Outcome == outcome {
			n++
		}
	}
	return n
}

// Through the real task and story check calls: lint fails, the work is fixed,
// the rerun passes, and the accepted Automaton is no longer barred.
func TestLintGateBlockLiftsWhenTheSameUnitPassesLint(t *testing.T) {
	ctx := context.Background()
	for name, unit := range map[string]struct {
		run      func(r *meRig, p *PRD) (bool, error)
		verdicts func(p *PRD) []GuardrailVerdict
	}{
		"task": {
			func(r *meRig, p *PRD) (bool, error) {
				blocked, _, err := r.m.runPerTaskGuardrails(ctx, p, &p.Story[0].Tasks[0])
				return blocked, err
			},
			func(p *PRD) []GuardrailVerdict { return p.Story[0].Tasks[0].Verdicts },
		},
		"story": {
			func(r *meRig, p *PRD) (bool, error) {
				blocked, _, err := r.m.runPerStoryGuardrails(ctx, p, &p.Story[0])
				return blocked, err
			},
			func(p *PRD) []GuardrailVerdict { return p.Story[0].Verdicts },
		},
	} {
		t.Run(name, func(t *testing.T) {
			r, p, tip := meLintRig(t)
			meLintFails(t)
			if blocked, err := unit.run(r, p); err != nil || !blocked {
				t.Fatalf("failing lint: blocked=%v err=%v", blocked, err)
			}
			p.MergeAcceptance = meAccept(tip)
			r.save(p)
			if why := meReasons(r, p.ID); !strings.Contains(why, meBlockedWhy) {
				t.Fatalf("with the lint block open: %q", why)
			}
			meLintPasses(t)
			if blocked, err := unit.run(r, p); err != nil || blocked {
				t.Fatalf("passing lint: blocked=%v err=%v", blocked, err)
			}
			r.save(p)
			if e := r.m.MergeEligibility(ctx, p.ID); !e.Eligible || e.Basis != MergeBasisAccepted {
				t.Errorf("fixed and accepted, but: %v", e.Reasons)
			}
			if got := meCountLint(unit.verdicts(p), "pass"); got != 1 {
				t.Errorf("lint-gate passes recorded = %d, want 1", got)
			}
			var lifted bool
			for _, d := range p.Decisions {
				lifted = lifted || d.Kind == DecisionLintGateLifted
				if d.Kind == "guardrail_verdict" && d.VerdictOutcome == "pass" && strings.Contains(d.Note, "lint-gate") {
					t.Errorf("the lint pass was logged as a review verdict: %s", d.Note)
				}
			}
			if !lifted {
				t.Error("no decision records that the block was lifted")
			}
			// A second clean run has no open block, so it records nothing more.
			if _, err := unit.run(r, p); err != nil {
				t.Fatal(err)
			}
			if got := meCountLint(unit.verdicts(p), "pass"); got != 1 {
				t.Errorf("after another clean run, lint-gate passes = %d, want 1", got)
			}
		})
	}
}

// A gate that did not run (the directory is not a git work tree) passes
// nothing, and a clean gate with no earlier block records nothing.
func TestLintGateRecordsAPassOnlyToLiftABlockItRan(t *testing.T) {
	ctx := context.Background()
	r, p, _ := meLintRig(t)
	meLintPasses(t)
	tk := &p.Story[0].Tasks[0]
	if _, _, err := r.m.runPerTaskGuardrails(ctx, p, tk); err != nil {
		t.Fatal(err)
	}
	if n := meCountLint(tk.Verdicts, "pass"); n != 0 {
		t.Errorf("no earlier block, but %d lint-gate pass(es) recorded", n)
	}
	meLintFails(t)
	if blocked, _, err := r.m.runPerTaskGuardrails(ctx, p, tk); err != nil || !blocked {
		t.Fatalf("blocked=%v err=%v", blocked, err)
	}
	plain := t.TempDir()
	tk.VerificationScript = writeVerifyScript(t, plain, "ok.sh", "exit 0\n")
	p.ProjectDir = plain
	meLintPasses(t)
	if _, _, err := r.m.runPerTaskGuardrails(ctx, p, tk); err != nil {
		t.Fatal(err)
	}
	if n := meCountLint(tk.Verdicts, "pass"); n != 0 || !unitReviewBlocked(tk.Verdicts) {
		t.Errorf("the gate did not run, but passes=%d blocked=%v", n, unitReviewBlocked(tk.Verdicts))
	}
}

func TestLintGatePassIsNeverAReviewPass(t *testing.T) {
	at := func(guardrail, outcome string, d time.Duration) GuardrailVerdict {
		return GuardrailVerdict{Guardrail: guardrail, Outcome: outcome, Summary: outcome, VerdictAt: time.Now().Add(d)}
	}
	block := at("lint-gate", "block", -3*time.Hour)
	pass := at("lint-gate", "pass", -time.Minute)
	for name, c := range map[string]struct {
		a, b   []GuardrailVerdict
		state  ReviewState
		barred bool
	}{
		"lint block then lint pass, no review":    {[]GuardrailVerdict{block, pass}, nil, ReviewStatePending, false},
		"a lone lint pass":                        {[]GuardrailVerdict{pass}, nil, ReviewStatePending, false},
		"does not outrank another unit's block":   {[]GuardrailVerdict{pass}, []GuardrailVerdict{at("rules", "block", -2*time.Hour)}, ReviewStateBlocked, true},
		"does not lift another check's block":     {[]GuardrailVerdict{at("rules", "block", -2*time.Hour), block, pass}, nil, ReviewStateBlocked, true},
		"does not lift another unit's lint block": {[]GuardrailVerdict{pass}, []GuardrailVerdict{block}, ReviewStateBlocked, true},
		"a later lint block stands":               {[]GuardrailVerdict{block, at("lint-gate", "pass", -2*time.Hour), at("lint-gate", "block", -time.Minute)}, nil, ReviewStateBlocked, true},
		"a real review pass still counts":         {[]GuardrailVerdict{block, pass}, []GuardrailVerdict{at("rules", "pass", -time.Hour)}, ReviewStatePassed, false},
	} {
		p := &PRD{Story: []Story{{ID: "s1", Tasks: []Task{{ID: "t1", Verdicts: c.a}, {ID: "t2", Verdicts: c.b}}}}}
		if got := ReviewStateOf(p).State; got != c.state {
			t.Errorf("%s: review state = %s, want %s", name, got, c.state)
		}
		barred := ReviewStateOf(p).State == ReviewStateBlocked || anyReviewBlocked(p)
		if barred != c.barred {
			t.Errorf("%s: barred = %v, want %v", name, barred, c.barred)
		}
	}
}
