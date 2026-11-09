package mergepipeline

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
)

// readmeRunState is a run in progress whose release version is version and
// whose runner dates the release at now.
func readmeRunState(dir string, now time.Time, version string) *runState {
	return &runState{
		r:   &Runner{Dir: dir, Now: func() time.Time { return now }},
		run: autonomous.MergePipelineRun{ReleaseVersion: version},
	}
}

func readRepoText(t *testing.T, dir, rel string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// treeWithoutReadme lists the blobs at HEAD of dir, leaving out README.md, so
// two repositories can be compared on everything but the README.
func treeWithoutReadme(t *testing.T, dir string) string {
	t.Helper()
	var keep []string
	for _, line := range strings.Split(runGit(t, dir, "ls-tree", "-r", "HEAD"), "\n") {
		if line != "" && !strings.HasSuffix(line, "\tREADME.md") {
			keep = append(keep, line)
		}
	}
	return strings.Join(keep, "\n")
}

const readmeBefore = "# Project\n\nIntro text.\n\n## Current release\n\n" +
	"**Current release: v1.2.3 (2026-01-01).** Notes stay as they are.\n\n## More\n"

func TestReadmeReleaseLineRewritesOnlyVersionAndDate(t *testing.T) {
	dir := t.TempDir()
	putFile(t, dir, "README.md", readmeBefore)
	now := time.Date(2026, 11, 8, 9, 30, 0, 0, time.UTC)
	detail, err := readmeRunState(dir, now, "1.2.4").setReadmeReleaseLine()
	if err != nil {
		t.Fatalf("set readme line: %v", err)
	}
	want := strings.Replace(readmeBefore, "v1.2.3 (2026-01-01)", "v1.2.4 (2026-11-08)", 1)
	if got := readRepoText(t, dir, "README.md"); got != want {
		t.Fatalf("README after the step:\n%s\nwant:\n%s", got, want)
	}
	if !strings.Contains(detail, "v1.2.4") || !strings.Contains(detail, "2026-11-08") {
		t.Fatalf("detail = %q, want the version and date", detail)
	}
}

func TestReadmeReleaseLineRunTwiceLeavesIdenticalBytes(t *testing.T) {
	dir := t.TempDir()
	putFile(t, dir, "README.md", readmeBefore)
	day := time.Date(2026, 11, 8, 9, 30, 0, 0, time.UTC)
	if _, err := readmeRunState(dir, day, "1.2.4").setReadmeReleaseLine(); err != nil {
		t.Fatalf("first run: %v", err)
	}
	afterFirst := readRepoText(t, dir, "README.md")

	detail, err := readmeRunState(dir, day, "1.2.4").setReadmeReleaseLine()
	if err != nil {
		t.Fatalf("second run: %v", err)
	}
	if afterSecond := readRepoText(t, dir, "README.md"); afterSecond != afterFirst {
		t.Fatalf("second run changed the README:\nfirst:\n%s\nsecond:\n%s", afterFirst, afterSecond)
	}
	if !strings.Contains(detail, "already names v1.2.4") {
		t.Fatalf("second run detail = %q, want the no-op note", detail)
	}

	// A later date does not reopen the line: the version decides a no-op.
	later := time.Date(2026, 11, 9, 9, 30, 0, 0, time.UTC)
	if _, err := readmeRunState(dir, later, "1.2.4").setReadmeReleaseLine(); err != nil {
		t.Fatalf("run on a later day: %v", err)
	}
	if got := readRepoText(t, dir, "README.md"); got != afterFirst {
		t.Fatalf("a later-day run changed the README:\n%s", got)
	}
}

func TestReadmeReleaseLineFailsNamingFileWithoutLine(t *testing.T) {
	dir := t.TempDir()
	const body = "# Project\n\nNo release line here.\n"
	putFile(t, dir, "README.md", body)
	_, err := readmeRunState(dir, time.Now(), "1.2.4").setReadmeReleaseLine()
	if err == nil || !strings.Contains(err.Error(), "README.md") {
		t.Fatalf("want an error naming README.md, got %v", err)
	}
	if got := readRepoText(t, dir, "README.md"); got != body {
		t.Fatalf("README changed on failure: %q", got)
	}
}

func TestReadmeReleaseLineFailsOnLineWithoutVersionAndDate(t *testing.T) {
	dir := t.TempDir()
	const body = "**Current release: none yet.**\n"
	putFile(t, dir, "README.md", body)
	_, err := readmeRunState(dir, time.Now(), "1.2.4").setReadmeReleaseLine()
	if err == nil || !strings.Contains(err.Error(), "README.md") {
		t.Fatalf("want an error naming README.md, got %v", err)
	}
	if got := readRepoText(t, dir, "README.md"); got != body {
		t.Fatalf("README changed on failure: %q", got)
	}
}

// A full run releases the README with the bump: the step rewrites the line,
// and the bump commit carries the README with the version files and changelog.
func TestRunPutsReadmeReleaseLineInBumpCommit(t *testing.T) {
	dir := newMergeRepo(t)
	now := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
	r := &Runner{Resolver: resolverWithRecord(t, definitionWithoutReleaseValidator()), VersionFiles: testVersionFiles,
		Dir: dir, Project: "p", Ship: &fakeShip{}, Recorder: &memRecorder{}, Now: func() time.Time { return now }}
	res, err := r.Run(context.Background(), RunRequest{Title: "readme release", Branches: []string{"feature"}})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.Run.Result != autonomous.MergeRunOK {
		t.Fatalf("run result = %q, want ok", res.Run.Result)
	}
	assertBothVersions(t, dir, "1.2.4")
	if got := stepByName(t, res.Run, "set-readme-release-line"); got.Result != autonomous.MergeStepOK {
		t.Fatalf("set-readme-release-line = %s %q, want ok", got.Result, got.Detail)
	}
	if !strings.Contains(readRepoText(t, dir, "README.md"), "**Current release: v1.2.4 (2026-11-08).**") {
		t.Fatalf("README does not carry the release line:\n%s", readRepoText(t, dir, "README.md"))
	}
	files := runGit(t, dir, "show", "--name-only", "--format=", "HEAD")
	if !strings.Contains(files, "README.md") {
		t.Fatalf("bump commit files = %q, want README.md among them", files)
	}
}
