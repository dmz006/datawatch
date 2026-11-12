// TestPlanCoverageFilters_CreatedFiles — the structural plan check
// (rule 3 of CheckPlanStructure) stops reporting an unresolved_file
// finding for a file the plan itself creates: the clause naming it
// carries an un-negated creation intent, or it sits in a story or
// task's FilesPlanned, and its parent directory exists or is created
// by the plan. Withheld findings are counted in
// PlanStructureReport.Suppressed; references and uncreatable paths
// still produce findings. The plans below are shaped like the real
// decomposed plans of 2026-10-30: a story that adds new source files,
// tests and a how-to doc.
package autonomous

import (
	"os"
	"path/filepath"
	"testing"
)

const createdFilesVerifyScript = "scripts/verify.sh"

// createdFilesTree builds a temporary project tree that already has an
// internal/x directory and a docs directory (as the real plans' trees
// did), with internal/missing deliberately absent.
func createdFilesTree(t *testing.T) *PRD {
	t.Helper()
	dir := t.TempDir()
	for _, sub := range []string{"internal/x", "docs"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", sub, err)
		}
	}
	writeFixtureVerifyScript(t, dir)
	return &PRD{ID: "prd-1", ProjectDir: dir}
}

// writeFixtureVerifyScript drops the executable scripts/verify.sh the fixture plans name.
func writeFixtureVerifyScript(t *testing.T, dir string) {
	t.Helper()
	p := filepath.Join(dir, "scripts", "verify.sh")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil { // #nosec G306 -- test fixture must be executable
		t.Fatal(err)
	}
}

// createdFilesStory wraps tasks in one story (the decomposed-plan
// shape) with a path-free title and description.
func createdFilesStory(tasks ...Task) []Story {
	storyID := "story-1"
	for i := range tasks {
		if tasks[i].ID == "" {
			tasks[i].ID = "task-" + string(rune('a'+i))
		}
		tasks[i].StoryID = storyID
		if tasks[i].VerificationScript == "" {
			tasks[i].VerificationScript = createdFilesVerifyScript
		}
	}
	return []Story{{ID: storyID, Title: "Add the filter feature", Tasks: tasks}}
}

// createdFilesOpts is the operator default: the created-file
// suppression on.
var createdFilesOpts = PlanStructureOptions{SuppressCreatedFiles: true}

// createdFilesReport reports the unresolved_file count and the total
// finding count of a report.
func createdFilesReport(rep *PlanStructureReport) (unresolved, total int) {
	for _, f := range rep.Findings {
		if f.Kind == FindingUnresolvedFile {
			unresolved++
		}
	}
	return unresolved, len(rep.Findings)
}

func TestPlanCoverageFilters_CreatedFilesSuppression(t *testing.T) {
	t.Run("1: created file in title, spec verb, and files list gives no finding and Suppressed 1", func(t *testing.T) {
		prd := createdFilesTree(t)
		stories := createdFilesStory(Task{
			ID:                 "task-a",
			Title:              "Create internal/x/plan.go",
			Spec:               "Create internal/x/plan.go, implementing the created-file filter.",
			FilesPlanned:       []string{"internal/x/plan.go"},
			VerificationScript: createdFilesVerifyScript,
		})
		rep := CheckPlanStructureWithOptions(prd, stories, createdFilesOpts)
		unresolved, total := createdFilesReport(rep)
		if unresolved != 0 {
			t.Fatalf("unresolved_file findings = %d, want 0: %+v", unresolved, rep.Findings)
		}
		if total != 0 {
			t.Fatalf("findings = %d, want 0: %+v", total, rep.Findings)
		}
		if rep.Suppressed != 1 {
			t.Fatalf("Suppressed = %d, want 1", rep.Suppressed)
		}
	})

	t.Run("2: file only in the task files list with an existing directory gives no finding", func(t *testing.T) {
		prd := createdFilesTree(t)
		stories := createdFilesStory(Task{
			ID:                 "task-a",
			Title:              "Wire the new filter",
			Spec:               "Cover internal/x/extra.go with the suite.",
			FilesPlanned:       []string{"internal/x/extra.go"},
			VerificationScript: createdFilesVerifyScript,
		})
		rep := CheckPlanStructureWithOptions(prd, stories, createdFilesOpts)
		unresolved, total := createdFilesReport(rep)
		if unresolved != 0 {
			t.Fatalf("unresolved_file findings = %d, want 0: %+v", unresolved, rep.Findings)
		}
		if total != 0 {
			t.Fatalf("findings = %d, want 0: %+v", total, rep.Findings)
		}
		if rep.Suppressed != 1 {
			t.Fatalf("Suppressed = %d, want 1", rep.Suppressed)
		}
	})

	t.Run("3: spec adds a how-to doc and docs exists gives no finding", func(t *testing.T) {
		prd := createdFilesTree(t)
		stories := createdFilesStory(Task{
			ID:                 "task-a",
			Title:              "Document the usage",
			Spec:               "Add docs/new.md for the operator.",
			FilesPlanned:       []string{"docs/new.md"},
			VerificationScript: createdFilesVerifyScript,
		})
		rep := CheckPlanStructureWithOptions(prd, stories, createdFilesOpts)
		unresolved, total := createdFilesReport(rep)
		if unresolved != 0 {
			t.Fatalf("unresolved_file findings = %d, want 0: %+v", unresolved, rep.Findings)
		}
		if total != 0 {
			t.Fatalf("findings = %d, want 0: %+v", total, rep.Findings)
		}
		if rep.Suppressed != 1 {
			t.Fatalf("Suppressed = %d, want 1", rep.Suppressed)
		}
	})

	t.Run("4: uncreatable parent and no task creates it keeps the finding, Suppressed 0", func(t *testing.T) {
		prd := createdFilesTree(t)
		stories := createdFilesStory(Task{
			ID:                 "task-a",
			Title:              "Create internal/missing/new.go",
			Spec:               "Create internal/missing/new.go in its new location.",
			FilesPlanned:       []string{"internal/missing/new.go"},
			VerificationScript: createdFilesVerifyScript,
		})
		rep := CheckPlanStructureWithOptions(prd, stories, createdFilesOpts)
		unresolved, _ := createdFilesReport(rep)
		if unresolved != 1 {
			t.Fatalf("unresolved_file findings = %d, want 1: %+v", unresolved, rep.Findings)
		}
		if rep.Suppressed != 0 {
			t.Fatalf("Suppressed = %d, want 0", rep.Suppressed)
		}
	})

	t.Run("5: another task creating the parent directory clears the finding", func(t *testing.T) {
		prd := createdFilesTree(t)
		stories := createdFilesStory(
			Task{
				ID:                 "task-a",
				Title:              "Create internal/missing/new.go",
				Spec:               "Create internal/missing/new.go in its new location.",
				FilesPlanned:       []string{"internal/missing/new.go"},
				VerificationScript: createdFilesVerifyScript,
			},
			Task{
				ID:                 "task-b",
				Title:              "Create the directory internal/missing",
				Spec:               "Create the directory internal/missing before the package files.",
				FilesPlanned:       []string{"internal/missing/"},
				VerificationScript: createdFilesVerifyScript,
			},
		)
		rep := CheckPlanStructureWithOptions(prd, stories, createdFilesOpts)
		unresolved, total := createdFilesReport(rep)
		if unresolved != 0 {
			t.Fatalf("unresolved_file findings = %d, want 0: %+v", unresolved, rep.Findings)
		}
		if total != 0 {
			t.Fatalf("findings = %d, want 0: %+v", total, rep.Findings)
		}
	})

	t.Run("6: options off reproduces the old findings for cases 1 and 2", func(t *testing.T) {
		prd := createdFilesTree(t)
		stories := createdFilesStory(
			Task{
				ID:                 "task-a",
				Title:              "Create internal/x/plan.go",
				Spec:               "Create internal/x/plan.go, implementing the created-file filter.",
				FilesPlanned:       []string{"internal/x/plan.go"},
				VerificationScript: createdFilesVerifyScript,
			},
			Task{
				ID:                 "task-b",
				Title:              "Wire the new filter",
				Spec:               "Cover internal/x/extra.go with the suite.",
				FilesPlanned:       []string{"internal/x/extra.go"},
				VerificationScript: createdFilesVerifyScript,
			},
		)
		rep := CheckPlanStructureWithOptions(prd, stories, PlanStructureOptions{})
		unresolved, _ := createdFilesReport(rep)
		if unresolved != 2 {
			t.Fatalf("unresolved_file findings = %d, want 2: %+v", unresolved, rep.Findings)
		}
		if rep.Suppressed != 0 {
			t.Fatalf("Suppressed = %d, want 0 with the option off", rep.Suppressed)
		}
	})

	t.Run("7: a file only referenced with no verb and no plan stays a finding", func(t *testing.T) {
		prd := createdFilesTree(t)
		stories := createdFilesStory(Task{
			ID:                 "task-a",
			Title:              "Review the pattern",
			Spec:               "See internal/x/other.go for the shape.",
			VerificationScript: createdFilesVerifyScript,
		})
		rep := CheckPlanStructureWithOptions(prd, stories, createdFilesOpts)
		unresolved, _ := createdFilesReport(rep)
		if unresolved != 1 {
			t.Fatalf("unresolved_file findings = %d, want 1: %+v", unresolved, rep.Findings)
		}
		if rep.Suppressed != 0 {
			t.Fatalf("Suppressed = %d, want 0 for a mere reference", rep.Suppressed)
		}
	})

	t.Run("8: a negated creation clause keeps the finding", func(t *testing.T) {
		prd := createdFilesTree(t)
		stories := createdFilesStory(Task{
			ID:                 "task-a",
			Title:              "Note the exclusion",
			Spec:               "Do not create internal/x/never.go; it is out of scope.",
			VerificationScript: createdFilesVerifyScript,
		})
		rep := CheckPlanStructureWithOptions(prd, stories, createdFilesOpts)
		unresolved, _ := createdFilesReport(rep)
		if unresolved != 1 {
			t.Fatalf("unresolved_file findings = %d, want 1: %+v", unresolved, rep.Findings)
		}
		if rep.Suppressed != 0 {
			t.Fatalf("Suppressed = %d, want 0 for a negated clause", rep.Suppressed)
		}
	})

	t.Run("9: duplicate mentions of one suppressed path count once", func(t *testing.T) {
		prd := createdFilesTree(t)
		stories := createdFilesStory(Task{
			ID:                 "task-a",
			Title:              "Create the created-file filter",
			Spec:               "Create internal/x/plan.go for the filter, and keep internal/x/plan.go focused on one rule.",
			FilesPlanned:       []string{"internal/x/plan.go"},
			VerificationScript: createdFilesVerifyScript,
		})
		rep := CheckPlanStructureWithOptions(prd, stories, createdFilesOpts)
		unresolved, total := createdFilesReport(rep)
		if unresolved != 0 {
			t.Fatalf("unresolved_file findings = %d, want 0: %+v", unresolved, rep.Findings)
		}
		if total != 0 {
			t.Fatalf("findings = %d, want 0: %+v", total, rep.Findings)
		}
		if rep.Suppressed != 1 {
			t.Fatalf("Suppressed = %d, want 1 (a path named twice in one task counts once)", rep.Suppressed)
		}
	})
}
