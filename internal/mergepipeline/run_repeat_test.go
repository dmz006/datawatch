package mergepipeline

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/autonomous"
)

// releaseSnapshot captures everything a release can change: the version
// files, the changelog, the commit history and the tag list. A repeat of a
// finished merge must leave every part of it as it was.
func releaseSnapshot(t *testing.T, dir string) string {
	t.Helper()
	var b strings.Builder
	for _, f := range append(append([]string{}, testVersionFiles...), "CHANGELOG.md") {
		data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(f)))
		if err != nil {
			t.Fatal(err)
		}
		b.WriteString("== " + f + "\n" + string(data))
	}
	b.WriteString("== log\n" + runGit(t, dir, "log", "--format=%H %s"))
	b.WriteString("== tags\n" + runGit(t, dir, "tag", "--list"))
	return b.String()
}

// stepByName returns the recorded step of the run with the given name.
func stepByName(t *testing.T, run autonomous.MergePipelineRun, name string) autonomous.MergeStepRun {
	t.Helper()
	for _, s := range run.Steps {
		if s.Step == name {
			return s
		}
	}
	t.Fatalf("run has no step %q; steps: %+v", name, run.Steps)
	return autonomous.MergeStepRun{}
}

// Running set-release-version twice in one run writes the version once: the
// second call is a no-op and leaves the file bytes unchanged.
func TestSetReleaseVersionTwiceInOneRunWritesOnce(t *testing.T) {
	dir := newMergeRepo(t)
	r := &Runner{VersionFiles: testVersionFiles, Dir: dir, Project: "p"}
	st := &runState{r: r}
	ctx := context.Background()

	if _, err := st.action(ctx, Step{Action: ActionSetReleaseVersion}); err != nil {
		t.Fatalf("first set-release-version: %v", err)
	}
	assertBothVersions(t, dir, "1.2.4")
	before := releaseSnapshot(t, dir)

	note, err := st.action(ctx, Step{Action: ActionSetReleaseVersion})
	if err != nil {
		t.Fatalf("second set-release-version: %v", err)
	}
	if !strings.Contains(note, "already at 1.2.4") {
		t.Fatalf("second call note = %q, want it to say already at 1.2.4", note)
	}
	if after := releaseSnapshot(t, dir); after != before {
		t.Fatalf("second call changed the release state:\nbefore:\n%s\nafter:\n%s", before, after)
	}
	if st.run.ReleaseVersion != "1.2.4" {
		t.Fatalf("run ReleaseVersion = %q, want 1.2.4", st.run.ReleaseVersion)
	}
}

// When the run's version is set but a version file no longer carries it, the
// step still refuses rather than overwrite a file it did not compute.
func TestSetReleaseVersionRefusesWhenFilesDisagree(t *testing.T) {
	dir := newMergeRepo(t)
	r := &Runner{VersionFiles: testVersionFiles, Dir: dir, Project: "p"}
	st := &runState{r: r}
	ctx := context.Background()
	if _, err := st.action(ctx, Step{Action: ActionSetReleaseVersion}); err != nil {
		t.Fatalf("set-release-version: %v", err)
	}
	putFile(t, dir, testVersionFiles[1], "package x\n\nvar Version = \"9.9.9\"\n")
	if _, err := st.action(ctx, Step{Action: ActionSetReleaseVersion}); err == nil {
		t.Fatal("expected a refusal when a version file disagrees with the run's version")
	}
}

func TestVersionFilesCarryErrors(t *testing.T) {
	dir := newMergeRepo(t)
	if _, err := (&Runner{Dir: dir}).versionFilesCarry("1.2.4"); err == nil {
		t.Fatal("no version files: want an error")
	}
	r := &Runner{VersionFiles: []string{"missing.go"}, Dir: dir}
	if _, err := r.versionFilesCarry("1.2.4"); err == nil {
		t.Fatal("missing file: want an error")
	}
	putFile(t, dir, "bare.go", "package x\n")
	r = &Runner{VersionFiles: []string{"bare.go"}, Dir: dir}
	if _, err := r.versionFilesCarry("1.2.4"); err == nil {
		t.Fatal("file without a version line: want an error")
	}
}

// The whole pipeline run twice for the same branch leaves the version files,
// the changelog, the git log and the tags exactly as the first run left them.
// The second run finds the branch already in main, so nothing is released.
func TestPipelineTwiceForSameBranchChangesNothing(t *testing.T) {
	dir := newMergeRepo(t)
	ship := &fakeShip{}
	newRunner := func(rec autonomous.MergeRunRecorder) *Runner {
		return &Runner{Resolver: resolverWithRecord(t, definitionWithoutReleaseValidator()), VersionFiles: testVersionFiles,
			Dir: dir, Project: "p", Ship: ship, Recorder: rec}
	}
	req := RunRequest{Title: "repeat release", Branches: []string{"feature"}}

	first := &memRecorder{}
	if _, err := newRunner(first).Run(context.Background(), req); err != nil {
		t.Fatalf("first run: %v", err)
	}
	assertBothVersions(t, dir, "1.2.4")
	afterFirst := releaseSnapshot(t, dir)
	shipCalls := len(ship.calls)

	second := &memRecorder{}
	res, err := newRunner(second).Run(context.Background(), req)
	if err != nil {
		t.Fatalf("second run: %v", err)
	}
	if res.Run.Result != autonomous.MergeRunOK {
		t.Fatalf("second run result = %q, want ok", res.Run.Result)
	}
	if afterSecond := releaseSnapshot(t, dir); afterSecond != afterFirst {
		t.Fatalf("second run changed the release state:\nfirst:\n%s\nsecond:\n%s", afterFirst, afterSecond)
	}
	// Only the deploy step, which the skip list leaves alone, reaches the ship runner.
	if extra := ship.calls[shipCalls:]; len(extra) != 1 || extra[0] != "deploy" {
		t.Fatalf("second run reached the ship runner with %v, want only [deploy]", extra)
	}
	for _, name := range []string{"set-release-version", "insert-changelog-heading", "commit-release-bump",
		"tag-release", "push-release"} {
		if got := stepByName(t, res.Run, name); got.Result != autonomous.MergeStepSkipped || got.Detail != nothingNewNote {
			t.Fatalf("step %s = %s %q, want skipped with the nothing-new note", name, got.Result, got.Detail)
		}
	}
}

// A run whose branch is already in main, with no earlier run to mark it,
// skips every release step with a recorded note and changes nothing. The
// deploy step is not a release step and still runs.
func TestRunWithBranchAlreadyInMainReleasesNothing(t *testing.T) {
	dir := newMergeRepo(t)
	runGit(t, dir, "merge", "--no-ff", "-q", "-m", "merge feature", "feature")
	before := releaseSnapshot(t, dir)
	ship := &fakeShip{}
	rec := &memRecorder{}
	r := &Runner{Resolver: resolverWithRecord(t, definitionWithoutReleaseValidator()), VersionFiles: testVersionFiles,
		Dir: dir, Project: "p", Ship: ship, Recorder: rec}
	res, err := r.Run(context.Background(), RunRequest{Title: "already in", Branches: []string{"feature"}})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if after := releaseSnapshot(t, dir); after != before {
		t.Fatalf("run changed the release state:\nbefore:\n%s\nafter:\n%s", before, after)
	}
	if len(ship.calls) != 1 || ship.calls[0] != "deploy" {
		t.Fatalf("ship calls = %v, want only [deploy]", ship.calls)
	}
	if res.Run.ReleaseVersion != "" {
		t.Fatalf("ReleaseVersion = %q, want empty", res.Run.ReleaseVersion)
	}
	if got := stepByName(t, res.Run, "merge-branches"); got.Result != autonomous.MergeStepOK || !strings.Contains(got.Detail, "nothing new") {
		t.Fatalf("merge-branches = %s %q, want ok with a nothing-new note", got.Result, got.Detail)
	}
	if got := stepByName(t, res.Run, "push-release"); got.Result != autonomous.MergeStepSkipped {
		t.Fatalf("push-release = %s, want skipped", got.Result)
	}
}

// When some branches are new and some already merged, the new ones are merged
// and the release steps run; the already-merged branch is only reported.
func TestMixedBranchesMergeNewAndReportOld(t *testing.T) {
	dir := newMergeRepo(t)
	runGit(t, dir, "checkout", "-q", "-b", "older")
	putFile(t, dir, "older.txt", "old\n")
	runGit(t, dir, "add", "-A")
	runGit(t, dir, "commit", "-q", "-m", "older work")
	runGit(t, dir, "checkout", "-q", "main")
	runGit(t, dir, "merge", "--no-ff", "-q", "-m", "merge older", "older")

	r := &Runner{Resolver: resolverWithRecord(t, definitionWithoutReleaseValidator()), VersionFiles: testVersionFiles,
		Dir: dir, Project: "p", Ship: &fakeShip{}, Recorder: &memRecorder{}}
	res, err := r.Run(context.Background(), RunRequest{Title: "mixed", Branches: []string{"older", "feature"}})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	assertBothVersions(t, dir, "1.2.4")
	got := stepByName(t, res.Run, "merge-branches")
	if got.Result != autonomous.MergeStepOK || !strings.Contains(got.Detail, "merged: feature") ||
		!strings.Contains(got.Detail, "already merged into main: older") {
		t.Fatalf("merge-branches = %s %q", got.Result, got.Detail)
	}
	if got := stepByName(t, res.Run, "push-release"); got.Result != autonomous.MergeStepOK {
		t.Fatalf("push-release = %s, want ok", got.Result)
	}
}

// A failing ancestry check stops the merge step rather than guessing.
func TestBranchInMainReportsMissingBranch(t *testing.T) {
	dir := newMergeRepo(t)
	st := &runState{r: &Runner{Dir: dir}, m: &Merger{Dir: dir}}
	if _, err := st.branchInMain(context.Background(), "no-such-branch"); err == nil {
		t.Fatal("missing branch: want an error")
	}
}
