package mergepipeline

import (
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/autonomous"
)

// What a check tested is what is pushed: every command leaves the scratch tree
// as it found it, and the push needs a clean one.
func TestIsolatedCheckMustLeaveTheTreeAsItFoundIt(t *testing.T) {
	cases := map[string]struct {
		def     func() Definition
		failing string
	}{
		"a check edits a tracked file the next one passes on": {func() Definition {
			return noTagSteps(check("autofix", "echo FIXED > feature.txt"), check("unit tests", "grep -q FIXED feature.txt"))
		}, "autofix"},
		"a check leaves an untracked file the next one needs": {func() Definition {
			return noTagSteps(check("generate", "echo gen > generated.txt"), check("unit tests", "test -f generated.txt"))
		}, "generate"},
		"a command stages a file before the release bump": {func() Definition {
			var out []Step
			for _, st := range noTagSteps().Steps {
				if st.Action == ActionCommitReleaseBump {
					out = append(out, check("stage", "echo unchecked > evil.txt && git add evil.txt"))
				}
				out = append(out, st)
			}
			return Definition{Steps: out}
		}, "stage"},
		"a check changes a file the release steps already edited": {func() Definition {
			var out []Step
			for _, st := range noTagSteps().Steps {
				if st.Action == ActionCommitReleaseBump {
					out = append(out, check("rewrite", "echo extra >> CHANGELOG.md"))
				}
				out = append(out, st)
			}
			return Definition{Steps: out}
		}, "rewrite"},
		"a version command leaves a file the bump does not commit": {func() Definition {
			return noTagSteps(Step{Name: "own-version", Kind: KindVersionBump, Command: "echo x > stray.txt"})
		}, "push-release"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			s := newSandbox(t)
			before := s.state(t)
			res, err := mergeFeature(t, s.runner(t, c.def()))
			if err == nil || res.Pushed || stepResults(res.Run)[c.failing] != autonomous.MergeStepFailed {
				t.Fatalf("err=%v pushed=%v, want step %s failed: %+v", err, res.Pushed, c.failing, res.Run.Steps)
			}
			if d := stepDetail(res.Run, c.failing); !strings.Contains(d, "tree") {
				t.Errorf("the failure does not name the tree: %s", d)
			}
			s.assertUnchanged(t, before)
		})
	}
}

// Ignored build output is not a change to what is pushed.
func TestIsolatedCheckMayWriteIgnoredFiles(t *testing.T) {
	s := newSandbox(t)
	putFile(t, s.dir, ".gitignore", "bin/\n")
	runGit(t, s.dir, "add", ".gitignore")
	runGit(t, s.dir, "commit", "-q", "-m", "ignore bin")
	runGit(t, s.dir, "push", "-q", "origin", "main")
	res, err := mergeFeature(t, s.runner(t, noTagSteps(check("build", "mkdir -p bin && echo b > bin/app"))))
	if err != nil || !res.Pushed {
		t.Fatalf("%v %+v", err, res.Run.Steps)
	}
}

// A check judging a conflict proposal cannot add to the proposal's commit.
func TestIsolatedProposalCheckCannotChangeTheProposal(t *testing.T) {
	s := clashSandbox(t)
	putFile(t, s.dir, "Makefile", "build:\n\t@true\n")
	runGit(t, s.dir, "add", "Makefile")
	runGit(t, s.dir, "commit", "-q", "-m", "makefile")
	runGit(t, s.dir, "push", "-q", "origin", "main")
	before := s.state(t)
	def := noTagSteps(check("unit tests", `if [ -n "$MERGE_PROPOSAL" ]; then echo sneaked > sneaked.txt && git add sneaked.txt; fi`))
	reply := `{"resolutions":[{"file":"shared.txt","hunk":1,"text":"both sides\n"}]}`
	res, err := clashRun(t, s, def, PolicyMechanicalAndModel, newFakeReleaseQuestions(), &replyModel{reply: reply})
	if err == nil || res.Pushed {
		t.Fatalf("err=%v pushed=%v %+v", err, res.Pushed, res.Run.Steps)
	}
	s.assertUnchanged(t, before)
}
