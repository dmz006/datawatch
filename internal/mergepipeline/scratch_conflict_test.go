package mergepipeline

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
)

// clashSandbox has a branch "clash" and a pushed main commit that edit the
// same line differently, so the rebase and the merge both conflict.
func clashSandbox(t *testing.T) sandbox {
	t.Helper()
	s := newSandbox(t)
	addConflictingBranch(t, s.dir)
	runGit(t, s.dir, "push", "-q", "origin", "main")
	return s
}

func clashRun(t *testing.T, s sandbox, def Definition, policy ConflictPolicy, qs *fakeReleaseQuestions, model ConflictModel) (RunResult, error) {
	t.Helper()
	r := s.runner(t, def)
	r.Questions, r.Model = qs, model
	r.ConflictSettings = func() ConflictSettings {
		return ConflictSettings{Policy: policy, Mode: ConflictModeOn, ModelTimeout: time.Minute}
	}
	return r.Run(context.Background(), RunRequest{Title: "Clash", Branches: []string{"clash"}})
}

func withRules(def Definition, rules ...ConflictRule) Definition {
	def.ConflictRules = rules
	return def
}

func TestProjectConflictRules(t *testing.T) {
	for name, c := range map[string]struct {
		rules  []ConflictRule
		policy ConflictPolicy
		want   string // shared.txt on the remote; empty means the run asked
	}{
		"keep main":               {[]ConflictRule{{Path: "shared.txt", Rule: ConflictRuleKeepMain}}, PolicyMechanical, "main side"},
		"keep both, by pattern":   {[]ConflictRule{{Path: "*.txt", Rule: ConflictRuleKeepBoth}}, PolicyMechanical, "main side\nbranch side"},
		"first match wins: ask":   {[]ConflictRule{{Path: "shared.txt", Rule: ConflictRuleAsk}, {Path: "*.txt", Rule: ConflictRuleKeepBoth}}, PolicyMechanical, ""},
		"an unmatched file asks":  {[]ConflictRule{{Path: "other.txt", Rule: ConflictRuleKeepMain}}, PolicyMechanical, ""},
		"stop and ask ignores it": {[]ConflictRule{{Path: "shared.txt", Rule: ConflictRuleKeepMain}}, PolicyStopAndAsk, ""},
	} {
		t.Run(name, func(t *testing.T) {
			s := clashSandbox(t)
			before := s.state(t)
			qs := newFakeReleaseQuestions()
			res, err := clashRun(t, s, withRules(noTagSteps(), c.rules...), c.policy, qs, nil)
			if c.want == "" {
				if err == nil || res.Pushed || qs.raised != 1 {
					t.Fatalf("err=%v pushed=%v questions=%d %+v", err, res.Pushed, qs.raised, res.Run.Steps)
				}
				s.assertUnchanged(t, before)
				return
			}
			if err != nil || !res.Pushed || qs.raised != 0 {
				t.Fatalf("err=%v pushed=%v questions=%d %+v", err, res.Pushed, qs.raised, res.Run.Steps)
			}
			if got := runGit(t, s.bare, "show", "refs/heads/main:shared.txt"); got != c.want {
				t.Errorf("shared.txt = %q, want %q", got, c.want)
			}
		})
	}
}

// The conflict question names the real branch, and a later run of the same
// work against the same main does not raise it again.
func TestIsolatedConflictAsksOnceNamingTheBranch(t *testing.T) {
	s := clashSandbox(t)
	qs := newFakeReleaseQuestions()
	for i := 0; i < 2; i++ {
		if _, err := clashRun(t, s, noTagSteps(), PolicyStopAndAsk, qs, nil); err == nil {
			t.Fatal("the conflict was not reported")
		}
	}
	if qs.raised != 1 {
		t.Fatalf("questions raised = %d, want 1", qs.raised)
	}
	for _, q := range qs.questions {
		if !strings.Contains(q.Text, "branch clash") || strings.Contains(q.Text, scratchBranchPrefix) {
			t.Errorf("question text: %s", q.Text)
		}
	}
}

type replyModel struct {
	reply string
	calls int
}

func (m *replyModel) Propose(context.Context, string) (string, error) {
	m.calls++
	return m.reply, nil
}

func TestIsolatedModelProposalNeedsATestStep(t *testing.T) {
	reply := `{"resolutions":[{"file":"shared.txt","hunk":1,"text":"both sides\n"}]}`
	t.Run("no test step: nothing is applied", func(t *testing.T) {
		s := clashSandbox(t)
		before := s.state(t)
		model := &replyModel{reply: reply}
		res, err := clashRun(t, s, noTagSteps(), PolicyMechanicalAndModel, newFakeReleaseQuestions(), model)
		if err == nil || res.Pushed || model.calls != 1 {
			t.Fatalf("err=%v pushed=%v calls=%d", err, res.Pushed, model.calls)
		}
		s.assertUnchanged(t, before)
	})
	t.Run("a passing build and test step: applied and pushed", func(t *testing.T) {
		s := clashSandbox(t)
		putFile(t, s.dir, "Makefile", "build:\n\t@true\n")
		runGit(t, s.dir, "add", "Makefile")
		runGit(t, s.dir, "commit", "-q", "-m", "makefile")
		runGit(t, s.dir, "push", "-q", "origin", "main")
		def := noTagSteps(check("unit tests", "! grep -q '<<<<<<<' shared.txt"))
		res, err := clashRun(t, s, def, PolicyMechanicalAndModel, newFakeReleaseQuestions(), &replyModel{reply: reply})
		if err != nil || !res.Pushed {
			t.Fatalf("err=%v %+v", err, res.Run.Steps)
		}
		if got := runGit(t, s.bare, "show", "refs/heads/main:shared.txt"); got != "both sides" {
			t.Errorf("shared.txt = %q", got)
		}
		if res.Run.Result != autonomous.MergeRunOK || runGit(t, s.dir, "rev-parse", "main") != runGit(t, s.bare, "rev-parse", "refs/heads/main") {
			t.Errorf("result %s", res.Run.Result)
		}
	})
}

// A check can tell it is judging a model's proposal, before any release bump,
// from judging the candidate that will be pushed.
func TestIsolatedCheckKnowsItIsJudgingAProposal(t *testing.T) {
	s := clashSandbox(t)
	putFile(t, s.dir, "Makefile", "build:\n\t@true\n")
	runGit(t, s.dir, "add", "Makefile")
	runGit(t, s.dir, "commit", "-q", "-m", "makefile")
	runGit(t, s.dir, "push", "-q", "origin", "main")
	t.Setenv("MERGE_PROPOSAL", "inherited")
	def := noTagSteps(check("unit tests", "echo \"proposal=$MERGE_PROPOSAL\" >> '"+s.note("seen")+"'"))
	reply := `{"resolutions":[{"file":"shared.txt","hunk":1,"text":"both sides\n"}]}`
	if res, err := clashRun(t, s, def, PolicyMechanicalAndModel, newFakeReleaseQuestions(), &replyModel{reply: reply}); err != nil {
		t.Fatalf("%v %+v", err, res.Run.Steps)
	}
	if got := s.read("seen"); got != "proposal=1\nproposal=" {
		t.Errorf("checks saw %q, want the proposal run marked and the candidate run not", got)
	}
}
