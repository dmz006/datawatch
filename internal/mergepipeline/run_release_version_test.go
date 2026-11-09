package mergepipeline

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// versionIn reads the X.Y.Z from a version file of dir.
func versionIn(t *testing.T, dir, rel string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	line := ""
	for _, l := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(l, "var Version = ") {
			line = l
		}
	}
	return strings.TrimSuffix(strings.TrimPrefix(line, `var Version = "`), `"`)
}

// assertBothVersions fails unless every version file of dir carries want.
func assertBothVersions(t *testing.T, dir, want string) {
	t.Helper()
	for _, f := range testVersionFiles {
		if got := versionIn(t, dir, f); got != want {
			t.Fatalf("%s carries %q, want %q", f, got, want)
		}
	}
}

func TestPatchRunWritesNextPatchAndRecordsIt(t *testing.T) {
	dir := newMergeRepo(t)
	rec := &memRecorder{}
	r := &Runner{Resolver: resolverWithRecord(t, definitionWithoutReleaseValidator()), VersionFiles: testVersionFiles, Dir: dir, Project: "p", Ship: &fakeShip{}, Recorder: rec}
	res, err := r.Run(context.Background(), RunRequest{Title: "patch release", Branches: []string{"feature"}})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	assertBothVersions(t, dir, "1.2.4")
	if got := res.Run.ReleaseVersion; got != "1.2.4" {
		t.Fatalf("run ReleaseVersion = %q, want 1.2.4", got)
	}
	if got := rec.only(t).ReleaseVersion; got != "1.2.4" {
		t.Fatalf("recorded ReleaseVersion = %q, want 1.2.4", got)
	}
}

func TestMinorRunWritesNextMinorWithPatchZero(t *testing.T) {
	dir := newMergeRepo(t)
	rec := &memRecorder{}
	r := &Runner{Resolver: resolverWithRecord(t, definitionWithoutReleaseValidator()), VersionFiles: testVersionFiles, Dir: dir, Project: "p", Ship: &fakeShip{}, Recorder: rec,
		ReleaseSize: string(ReleaseSizeMinor)}
	if _, err := r.Run(context.Background(), RunRequest{Title: "minor release", Branches: []string{"feature"}}); err != nil {
		t.Fatalf("run: %v", err)
	}
	assertBothVersions(t, dir, "1.3.0")
	if got := rec.only(t).ReleaseVersion; got != "1.3.0" {
		t.Fatalf("recorded ReleaseVersion = %q, want 1.3.0", got)
	}
}

// When main's version is raised between two runs, the second run computes
// its version from the raised value, not from the first run's result.
func TestSecondRunComputesFromRaisedMainVersion(t *testing.T) {
	dir := newMergeRepo(t)
	first := &memRecorder{}
	r1 := &Runner{Resolver: resolverWithRecord(t, definitionWithoutReleaseValidator()), VersionFiles: testVersionFiles, Dir: dir, Project: "p", Ship: &fakeShip{}, Recorder: first}
	if _, err := r1.Run(context.Background(), RunRequest{Title: "first", Branches: []string{"feature"}}); err != nil {
		t.Fatalf("first run: %v", err)
	}
	assertBothVersions(t, dir, "1.2.4")

	// Raise main's version by hand, then add a branch for the second run.
	for _, f := range testVersionFiles {
		putFile(t, dir, f, "package x\n\nvar Version = \"1.7.0\"\n")
	}
	runGit(t, dir, "commit", "-q", "-am", "raise version")
	runGit(t, dir, "checkout", "-q", "-b", "feature2")
	putFile(t, dir, "second.txt", "more\n")
	runGit(t, dir, "add", "-A")
	runGit(t, dir, "commit", "-q", "-m", "second work")
	runGit(t, dir, "checkout", "-q", "main")

	second := &memRecorder{}
	r2 := &Runner{Resolver: resolverWithRecord(t, definitionWithoutReleaseValidator()), VersionFiles: testVersionFiles, Dir: dir, Project: "p", Ship: &fakeShip{}, Recorder: second}
	if _, err := r2.Run(context.Background(), RunRequest{Title: "second", Branches: []string{"feature2"}}); err != nil {
		t.Fatalf("second run: %v", err)
	}
	assertBothVersions(t, dir, "1.7.1")
	if got := second.only(t).ReleaseVersion; got != "1.7.1" {
		t.Fatalf("second run ReleaseVersion = %q, want 1.7.1", got)
	}
}

// A pipeline with no set-release-version step has no run version, so its tag
// step reads main's version as it stands.
func TestPipelineWithoutSetStepTagsCurrentVersion(t *testing.T) {
	dir := newMergeRepo(t)
	def := Definition{Steps: []Step{{Name: "tag-release", Kind: KindShip, Action: ActionTagRelease}}}
	ship := &fakeShip{}
	r := &Runner{VersionFiles: testVersionFiles, Dir: dir, Project: "p", Ship: ship,
		Resolver: resolverWithRecord(t, def)}
	res, err := r.Run(context.Background(), RunRequest{Title: "t", Branches: []string{"feature"}})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(ship.calls) != 1 || ship.calls[0] != "tag v1.2.3" {
		t.Fatalf("tag calls = %v, want [tag v1.2.3]", ship.calls)
	}
	if res.Run.ReleaseVersion != "" {
		t.Fatalf("run ReleaseVersion = %q, want empty without a set step", res.Run.ReleaseVersion)
	}
}
