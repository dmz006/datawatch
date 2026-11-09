package mergepipeline

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/autonomous"
)

// newValidatedReleaseRepo is newMergeRepo made into a published 1.2.3 release:
// the base is tagged v1.2.3 and pushed to an origin remote, and the feature
// branch carries a README whose Current release line names readmeVersion, the
// value the release-bump validator checks.
func newValidatedReleaseRepo(t *testing.T, readmeVersion string) string {
	t.Helper()
	dir := newMergeRepo(t)
	runGit(t, dir, "tag", "v1.2.3")
	origin := filepath.Join(t.TempDir(), "origin.git")
	runGit(t, dir, "init", "-q", "--bare", origin)
	runGit(t, dir, "remote", "add", "origin", origin)
	runGit(t, dir, "push", "-q", "origin", "main", "v1.2.3")
	runGit(t, dir, "checkout", "-q", "feature")
	putFile(t, dir, "README.md", "# App\n\n**Current release: v"+readmeVersion+" (2026-01-01).**\n")
	runGit(t, dir, "add", "-A")
	runGit(t, dir, "commit", "-q", "-m", "readme")
	runGit(t, dir, "checkout", "-q", "main")
	return dir
}

// definitionWithoutReleaseValidator is the built-in pipeline minus its
// validate-release-bump step. Tests of other run behavior use it, so their
// fixtures need no README or origin remote; the validator has its own tests.
// definitionWithoutReadmeStep is the default pipeline minus the step that
// rewrites the README's Current release: line.
func definitionWithoutReadmeStep() Definition {
	var steps []Step
	for _, s := range DefaultDefinition().Steps {
		if s.Action != ActionSetReadmeReleaseLine {
			steps = append(steps, s)
		}
	}
	return Definition{Steps: steps}
}

func definitionWithoutReleaseValidator() Definition {
	var steps []Step
	for _, s := range DefaultDefinition().Steps {
		if s.Action != ActionValidateReleaseBump {
			steps = append(steps, s)
		}
	}
	return Definition{Steps: steps}
}

// localTagShip is fakeShip, except that Tag also makes the local tag, as the
// production ship runner does, so the validator sees the run's own tag.
type localTagShip struct {
	*fakeShip
	t   *testing.T
	dir string
}

func (s localTagShip) Tag(ctx context.Context, dir, tag, msg string) error {
	if err := s.fakeShip.Tag(ctx, dir, tag, msg); err != nil {
		return err
	}
	runGit(s.t, s.dir, "tag", tag)
	return nil
}

// A good run records a passing validator on the run and then pushes.
func TestValidatedRunRecordsPassingValidatorAndPushes(t *testing.T) {
	dir := newValidatedReleaseRepo(t, "1.2.4")
	rec := &memRecorder{}
	ship := &fakeShip{}
	r := &Runner{VersionFiles: testVersionFiles, Dir: dir, Project: "p", Ship: ship, Recorder: rec}
	res, err := r.Run(context.Background(), RunRequest{Title: "validated release", Branches: []string{"feature"}})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.Run.Validator == nil || !res.Run.Validator.Pass {
		t.Fatalf("run validator = %+v, want a passing result", res.Run.Validator)
	}
	if got := len(res.Run.Validator.Checks); got != 5 {
		t.Fatalf("validator ran %d checks, want 5", got)
	}
	if got := rec.only(t).Validator; got == nil || !got.Pass {
		t.Fatalf("recorded validator = %+v, want a passing result", got)
	}
	if !containsCall(ship.calls, "push main v1.2.4") {
		t.Fatalf("ship calls = %v, want a push of main and v1.2.4", ship.calls)
	}
	if got := stepResult(t, res.Run, "validate-release-bump"); got != autonomous.MergeStepOK {
		t.Fatalf("validate-release-bump step result = %q, want ok", got)
	}
}

// A run whose validator check fails stops before push: the step fails, the
// failing check is named, and the fake ship runner records no push.
func TestValidatorFailureStopsBeforePush(t *testing.T) {
	dir := newValidatedReleaseRepo(t, "1.2.3") // stale README line
	rec := &memRecorder{}
	ship := &fakeShip{}
	// The README step would correct the stale line before the validator sees
	// it, so this run leaves that step out to test the validator's own check.
	r := &Runner{Resolver: resolverWithRecord(t, definitionWithoutReadmeStep()), VersionFiles: testVersionFiles,
		Dir: dir, Project: "p", Ship: ship, Recorder: rec}
	res, err := r.Run(context.Background(), RunRequest{Title: "stale readme", Branches: []string{"feature"}})
	if err == nil {
		t.Fatal("run succeeded; want a failure from the validator")
	}
	if !strings.Contains(err.Error(), "readme_line_matches") {
		t.Fatalf("error %q does not name readme_line_matches", err)
	}
	for _, c := range ship.calls {
		if strings.HasPrefix(c, "push") {
			t.Fatalf("push ran after a failed validator: calls = %v", ship.calls)
		}
	}
	if res.Run.Result != autonomous.MergeRunFailed {
		t.Fatalf("run result = %q, want failed", res.Run.Result)
	}
	if v := res.Run.Validator; v == nil || v.Pass {
		t.Fatalf("run validator = %+v, want a recorded failing result", v)
	}
	if got := rec.only(t).Validator; got == nil || got.Pass {
		t.Fatalf("recorded validator = %+v, want a failing result", got)
	}
	if got := stepDetail(res.Run, "validate-release-bump"); !strings.Contains(got, "readme_line_matches") {
		t.Fatalf("step detail %q does not name the failing check", got)
	}
}

// The run's own local tag is not the previous release, so a validated run
// that tags locally still passes the one-step check and pushes.
func TestValidatedRunWithLocalTagStillPasses(t *testing.T) {
	dir := newValidatedReleaseRepo(t, "1.2.4")
	ship := localTagShip{fakeShip: &fakeShip{}, t: t, dir: dir}
	r := &Runner{VersionFiles: testVersionFiles, Dir: dir, Project: "p", Ship: ship}
	res, err := r.Run(context.Background(), RunRequest{Title: "tagged release", Branches: []string{"feature"}})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.Run.Validator == nil || !res.Run.Validator.Pass {
		t.Fatalf("run validator = %+v, want a passing result", res.Run.Validator)
	}
	if !containsCall(ship.calls, "push main v1.2.4") {
		t.Fatalf("ship calls = %v, want a push", ship.calls)
	}
}

func containsCall(calls []string, want string) bool {
	for _, c := range calls {
		if c == want {
			return true
		}
	}
	return false
}

func stepResult(t *testing.T, run autonomous.MergePipelineRun, name string) string {
	t.Helper()
	for _, s := range run.Steps {
		if s.Step == name {
			return s.Result
		}
	}
	t.Fatalf("no step %q in run", name)
	return ""
}
