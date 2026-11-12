package mergepipeline

import (
	"context"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/autonomous"
)

// tagStepRun builds a runState over the repository at dir for a release
// version, so the tag action can be run directly, once or more, the way a
// pipeline's tag step would run it. The ship runner is the production one, so
// the tag really lands in the repository.
func tagStepRun(t *testing.T, dir, version string) *runState {
	t.Helper()
	r := &Runner{Dir: dir, Ship: ExecShipRunner{}}
	return &runState{
		r:   r,
		m:   &Merger{Dir: dir},
		run: autonomous.MergePipelineRun{ReleaseVersion: version},
	}
}

// countTags returns how many local tags match name exactly.
func countTags(t *testing.T, dir, name string) int {
	t.Helper()
	out := runGit(t, dir, "tag", "--list", name)
	n := 0
	for _, l := range strings.Split(strings.TrimSpace(out), "\n") {
		if strings.TrimSpace(l) == name {
			n++
		}
	}
	return n
}

// The tag step runs twice on the same HEAD: the second run changes nothing and
// the tag count stays one.
func TestTagReleaseRunTwiceLeavesOneTag(t *testing.T) {
	dir := newMergeRepo(t)
	s := tagStepRun(t, dir, "1.2.3")

	note, err := s.ship(context.Background(), ActionTagRelease)
	if err != nil {
		t.Fatalf("first tag run: %v", err)
	}
	if note != "tagged v1.2.3" {
		t.Fatalf("first note = %q, want %q", note, "tagged v1.2.3")
	}
	if got := countTags(t, dir, "v1.2.3"); got != 1 {
		t.Fatalf("after first run tag count = %d, want 1", got)
	}
	headBefore := runGit(t, dir, "rev-parse", "HEAD")

	note, err = s.ship(context.Background(), ActionTagRelease)
	if err != nil {
		t.Fatalf("second tag run must succeed: %v", err)
	}
	if !strings.Contains(note, "already tagged v1.2.3") {
		t.Fatalf("second note = %q, want an already-tagged note", note)
	}
	if got := countTags(t, dir, "v1.2.3"); got != 1 {
		t.Fatalf("after second run tag count = %d, want 1", got)
	}
	if got := strings.TrimSpace(runGit(t, dir, "rev-parse", "v1.2.3^{commit}")); got != headBefore {
		t.Fatalf("tag moved to %s, want HEAD %s", got, headBefore)
	}
}

// A tag that exists on another commit fails the step, names the tag and the
// commit it points at, and leaves that tag where it was.
func TestTagReleaseFailsWhenTagIsOnAnotherCommit(t *testing.T) {
	dir := newMergeRepo(t)
	older := strings.TrimSpace(runGit(t, dir, "rev-parse", "HEAD"))
	runGit(t, dir, "tag", "v1.2.3", older)
	putFile(t, dir, "after.txt", "later work\n")
	runGit(t, dir, "add", "after.txt")
	runGit(t, dir, "commit", "-q", "-m", "later work")
	head := strings.TrimSpace(runGit(t, dir, "rev-parse", "HEAD"))
	if head == older {
		t.Fatal("test setup: HEAD must differ from the tagged commit")
	}

	s := tagStepRun(t, dir, "1.2.3")
	note, err := s.ship(context.Background(), ActionTagRelease)
	if err == nil {
		t.Fatalf("tag on another commit must fail the step, got note %q", note)
	}
	if !strings.Contains(err.Error(), "v1.2.3") || !strings.Contains(err.Error(), older) {
		t.Fatalf("error %q must name the tag v1.2.3 and the commit %s", err, older)
	}
	if got := strings.TrimSpace(runGit(t, dir, "rev-parse", "v1.2.3^{commit}")); got != older {
		t.Fatalf("existing tag moved to %s, want it kept at %s", got, older)
	}
	if s.tagMade != "" {
		t.Fatalf("tagMade = %q, want empty: the run made no tag", s.tagMade)
	}
}

// A tag this run did not create is not recorded as the run's, so a redo never
// deletes it.
func TestTagReleaseAlreadyAtHeadIsNotRecordedAsRunsTag(t *testing.T) {
	dir := newMergeRepo(t)
	runGit(t, dir, "tag", "v1.2.3")
	s := tagStepRun(t, dir, "1.2.3")

	if _, err := s.ship(context.Background(), ActionTagRelease); err != nil {
		t.Fatalf("tag already at HEAD must succeed: %v", err)
	}
	if s.tagMade != "" {
		t.Fatalf("tagMade = %q, want empty for a tag the run did not make", s.tagMade)
	}
	if got := countTags(t, dir, "v1.2.3"); got != 1 {
		t.Fatalf("tag count = %d, want 1", got)
	}
}
