package mergepipeline

import (
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/autonomous"
)

// Whatever happens while the checks run, the push refuses and the run changes
// nothing of its own.
func TestIsolatedPushRefusals(t *testing.T) {
	continuing := check("unit tests", "exit 1")
	continuing.OnFailure = OnFailureContinue
	for name, c := range map[string]struct {
		step Step
		why  string
	}{
		"a failed check set to continue": {continuing, "a step of this run failed"},
		"a commit lands on local main during the run": {
			check("unit tests", `cd "$MERGE_PROJECT_DIR" && git commit -q --allow-empty -m "made during the run"`),
			"local main changed during the run"},
		"the branch moves during the checks": {
			check("unit tests", `cd "$MERGE_PROJECT_DIR" && git update-ref refs/heads/feature "$(git commit-tree -p feature -m later 'feature^{tree}')"`),
			"moved since it was selected"},
	} {
		t.Run(name, func(t *testing.T) {
			s := newSandbox(t)
			remote := runGit(t, s.bare, "for-each-ref")
			r := s.runner(t, noTagSteps(c.step))
			r.Selection = &fixedSelection{tip: runGit(t, s.dir, "rev-parse", "feature")}
			res, err := mergeFeature(t, r)
			if res.Pushed || stepResults(res.Run)["push-release"] != autonomous.MergeStepFailed || err == nil {
				t.Fatalf("pushed=%v err=%v %+v", res.Pushed, err, res.Run.Steps)
			}
			if d := stepDetail(res.Run, "push-release"); !strings.Contains(d, c.why) {
				t.Errorf("push refusal %q does not say %q", d, c.why)
			}
			if got := runGit(t, s.bare, "for-each-ref"); got != remote {
				t.Errorf("the remote changed: %s", got)
			}
			if strings.Contains(runGit(t, s.dir, "log", "--format=%s", "main"), "Merge branch") {
				t.Error("the run's merge reached local main")
			}
		})
	}
}

// A push step that reports success while the remote does not show the commit
// moves nothing, and the run does not count as pushed.
func TestIsolatedUnconfirmedPushMovesNothing(t *testing.T) {
	s := newSandbox(t)
	putFile(t, s.dir, "later.txt", "later\n") // makes the run rebase
	runGit(t, s.dir, "add", "-A")
	runGit(t, s.dir, "commit", "-q", "-m", "later on main")
	runGit(t, s.dir, "push", "-q", "origin", "main")
	before := s.state(t)
	r := s.runner(t, noTagSteps())
	r.Ship = &fakeShip{} // says it pushed and sends nothing
	res, err := mergeFeature(t, r)
	if err != nil || res.Pushed {
		t.Fatalf("err=%v pushed=%v %+v", err, res.Pushed, res.Run.Steps)
	}
	if got := stepResults(res.Run)[localMainStepName]; got != autonomous.MergeStepSkipped {
		t.Errorf("%s = %q, want skipped", localMainStepName, got)
	}
	s.assertUnchanged(t, before)
}
