package mergeshadow

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/config"
)

// TestReplayReviewNamesCommitBeforeFixBlocksPlan replays a review that passed
// the work branch before a fix landed. The review names the commit that was
// reviewed, which is the commit before the fix; the branch head is the fix. The
// plan must be blocked by a review_stale finding, the gates must not run, and
// the plan must stay unapplied.
func TestReplayReviewNamesCommitBeforeFixBlocksPlan(t *testing.T) {
	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repo, 0o750); err != nil {
		t.Fatal(err)
	}
	git(t, repo, "init", "-q", "-b", "main")
	write(t, repo, "a.txt", "base\n")
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-q", "-m", "base")

	const branch = "automaton/rr000001"
	git(t, repo, "checkout", "-q", "-b", branch, "main")
	write(t, repo, "feature.txt", "feature, as reviewed\n")
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-q", "-m", "feature")
	reviewed := strings.TrimSpace(git(t, repo, "rev-parse", "HEAD"))

	write(t, repo, "feature.txt", "feature, with the fix\n")
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-q", "-m", "fix after review")
	head := strings.TrimSpace(git(t, repo, "rev-parse", "HEAD"))
	if head == reviewed {
		t.Fatal("fixture: head equals the reviewed commit")
	}
	git(t, repo, "checkout", "-q", "main")

	dataDir := t.TempDir()
	in := Input{
		AutomatonID: "rr000001",
		Project:     "replay",
		RepoPath:    repo,
		Branch:      branch,
		BaseRef:     "main",
		DataDir:     dataDir,
		Pipeline:    config.MergePipelineConfig{VersionFiles: []string{"version.go"}},
		ChangelogTasks: []ChangelogTask{{
			ID:     "t1",
			Kind:   ChangelogTaskKind,
			Output: "## [v1.0.0] - 2026-11-09\n\n### Added\n- feature\n",
		}},
		GateTimeout: time.Minute,
		Review:      &ReviewVerdict{Commit: reviewed},
	}

	gateCalls := 0
	gates := func(context.Context, string, time.Duration) []GateResult {
		gateCalls++
		return nil
	}

	plan, err := Runner{Gates: gates, Now: fixedClock()}.Run(context.Background(), in)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if plan.Applied {
		t.Error("plan marked applied; a blocked plan must stay unapplied")
	}
	if gateCalls != 0 {
		t.Errorf("gates ran %d time(s) against a blocked plan, want 0", gateCalls)
	}

	var stale *Finding
	for i := range plan.Findings {
		if plan.Findings[i].Code == ReviewStaleCode {
			stale = &plan.Findings[i]
		}
	}
	if stale == nil {
		t.Fatalf("findings %+v, want a review_stale finding", plan.Findings)
	}
	if !stale.Blocking {
		t.Errorf("review_stale finding %+v is not blocking", stale)
	}
	if !strings.Contains(stale.Message, reviewed) || !strings.Contains(stale.Message, head) {
		t.Errorf("finding message %q does not name the reviewed and head commits", stale.Message)
	}

	stored, err := Load(dataDir, in.AutomatonID)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if stored.Applied {
		t.Error("stored plan marked applied")
	}
	if len(stored.Findings) != 1 || stored.Findings[0].Code != ReviewStaleCode {
		t.Errorf("stored findings %+v, want the review_stale finding", stored.Findings)
	}
}
