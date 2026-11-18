package mergeshadow

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

const draftPath = "docs/plans/drafts/a1-changelog.md"

// branchWith commits files on top of "work" and returns to main.
func branchWith(t *testing.T, files map[string]string) string {
	t.Helper()
	repo := runnerRepo(t)
	git(t, repo, "checkout", "-q", "work")
	for p, c := range files {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(repo, p)), 0o750); err != nil {
			t.Fatal(err)
		}
		write(t, repo, p, c)
	}
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-q", "-m", "changelog")
	git(t, repo, "checkout", "-q", "main")
	return repo
}

func runNoTasks(t *testing.T, repo string) *MergePlan {
	t.Helper()
	in := runnerInput(t, repo, "work")
	in.ChangelogTasks = nil
	in.ChangelogDraftPath = draftPath
	calls := 0
	plan, err := Runner{Gates: fakeGates(t, GatePass, &calls), Now: fixedClock()}.Run(context.Background(), in)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	return plan
}

func TestPlanFallsBackToBranchDraft(t *testing.T) {
	repo := branchWith(t, map[string]string{
		draftPath: "## [1.3.0] - 2026-11-09\n\n### Added\n- drafted\n",
		// a CHANGELOG.md entry too: the draft must win over it.
		"CHANGELOG.md": "# Changelog\n\n## [1.9.0] - 2026-11-09\n\n- other\n\n## [1.2.0] - 2026-11-01\n\n- old\n",
	})
	plan := runNoTasks(t, repo)
	if plan.ChangelogSource != "draft" || plan.PlannedVersion != "1.3.0" || plan.ReleaseSize != ReleaseSizeMinor {
		t.Errorf("got source=%q version=%q size=%q", plan.ChangelogSource, plan.PlannedVersion, plan.ReleaseSize)
	}
	if plan.ChangelogBody != "### Added\n- drafted" {
		t.Errorf("body %q", plan.ChangelogBody)
	}
}

func TestPlanFallsBackToBranchChangelogEntry(t *testing.T) {
	repo := branchWith(t, map[string]string{
		"CHANGELOG.md": "# Changelog\n\n## [1.2.1] - 2026-11-09\n\n- fix\n\n## [1.2.0] - 2026-11-01\n\n- old\n",
	})
	plan := runNoTasks(t, repo)
	if plan.ChangelogSource != "branch" || plan.PlannedVersion != "1.2.1" || plan.ReleaseSize != ReleaseSizePatch {
		t.Errorf("got source=%q version=%q size=%q", plan.ChangelogSource, plan.PlannedVersion, plan.ReleaseSize)
	}
}

func TestPlanNothingOnBranchStaysMissing(t *testing.T) {
	plan := runNoTasks(t, runnerRepo(t))
	if plan.ChangelogSource != ChangelogSourceMissing || plan.PlannedVersion != "" || plan.ReleaseSize != ReleaseSizeNone {
		t.Errorf("got source=%q version=%q size=%q", plan.ChangelogSource, plan.PlannedVersion, plan.ReleaseSize)
	}
	if plan.ChangelogHeading != "" || plan.ChangelogBody != "" {
		t.Error("text invented")
	}
}

func TestEntryTaskOutputWinsOverBranch(t *testing.T) {
	repo := branchWith(t, map[string]string{draftPath: "## [1.9.0] - 2026-11-09\n\n- drafted\n"})
	in := runnerInput(t, repo, "work")
	in.ChangelogDraftPath = draftPath
	calls := 0
	plan, err := Runner{Gates: fakeGates(t, GatePass, &calls), Now: fixedClock()}.Run(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if plan.ChangelogSource != "t9" || plan.PlannedVersion != "v1.3.0" {
		t.Errorf("got source=%q version=%q", plan.ChangelogSource, plan.PlannedVersion)
	}
}
