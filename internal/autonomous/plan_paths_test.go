// Plan-path lint tests: LintPlanPaths must (a) warn exactly once per
// planned path whose target AND parent directory are missing under the PRD
// working tree — naming the unit, the path, and the missing parent — and
// (b) stay silent for new files in existing directories, already-existing
// paths, FilesGenerated entries, and non-path tokens (URLs, shell
// variables, absolute and dot-relative paths). It must also append
// warnings to the stored PRD on the decompose store path WITHOUT changing
// the outcome: the PRD still stores, still moves to needs_review.
package autonomous

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// makePlanTree builds a working tree: root/real/thing exists, so a one-
// letter typo of the "real" directory (e.g. "real") is a realistic
// missing-parent scenario.
func makePlanTree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "cmd", "datch"), 0o755); err != nil {
		t.Fatalf("mkdir cmd/datch: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "cmd", "datch", "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatalf("write main.go: %v", err)
	}
	return root
}

func TestLintPlanPathsMissingDirWarns(t *testing.T) {
	root := makePlanTree(t)
	prd := &PRD{ID: "prd1", Title: "Add dftwatch", ProjectDir: root}
	plan := []Story{{
		Title:       "Add dftwatch command",
		Description: "Wire the new dftwatch binary next to cmd/datch.",
		Tasks: []Task{{
			ID:    "t1",
			Title: "Build binary",
			Spec: `Create the entrypoint at cmd/dftwatch/main.go that wraps cmd/datch.
Also reference the docs at https://example.com/docs/path and $ROOT/lib/x
which must not both become warnings.`,
			FilesPlanned: []string{"cmd/dftwatch/main.go"},
		}},
	}}
	warns := LintPlanPaths(prd, plan)
	// Exactly one warning: the typo'd directory, named with the task,
	// the path, and the missing parent. "cmd/datch" (real) must not warn;
	// the URL-derived "example.com/docs/path" and the $ROOT-derived
	// "lib/x" must not produce candidates at all.
	if len(warns) != 1 {
		t.Fatalf("got %d warnings, want exactly 1: %q", len(warns), warns)
	}
	w := warns[0]
	for _, want := range []string{
		`task "Build binary"`,
		`"cmd/dftwatch/main.go"`,
		`"cmd/dftwatch"`,
		"does not exist under the PRD working tree",
	} {
		if !strings.Contains(w, want) {
			t.Fatalf("warning %q does not contain %q", w, want)
		}
	}
}

func TestLintPlanPathsValidPlanClean(t *testing.T) {
	root := makePlanTree(t)
	prd := &PRD{ID: "prd1", Title: "Extend datch", ProjectDir: root}
	plan := []Story{{
		Title:       "Extend datch",
		Description: "Extend cmd/datch and add the cmd/datch/flags.go helper.",
		Tasks: []Task{{
			ID:           "t1",
			Title:        "Add flags",
			Spec:         "Add a --verbose flag wired through cmd/datch/main.go and cmd/datch/flags.go.",
			FilesPlanned: []string{"cmd/datch/flags.go"}, // brand-new file in existing dir
			FilesGenerated: []string{
				".datawatch/verify/story1.sh", // generated, missing dir — must NOT warn
			},
		}},
	}}
	if warns := LintPlanPaths(prd, plan); len(warns) != 0 {
		t.Fatalf("got warnings for a valid plan, want none: %q", warns)
	}
}

func TestLintPlanPathsDedupAndExistingClean(t *testing.T) {
	root := makePlanTree(t)
	prd := &PRD{ID: "prd1", ProjectDir: root}
	// Same typo'd path in story description, story FilesPlanned, task
	// spec, and task FilesPlanned — every surface, plus an existing file
	// that must stay silent.
	plan := []Story{{
		Title:       "Add dftwatch",
		Description: "Add cmd/dftwatch alongside cmd/datch.",
		FilesPlanned: []string{
			"cmd/dftwatch/main.go", // missing dir → one warning (story)
			"cmd/datch/main.go",    // exists → silent
		},
		Tasks: []Task{{
			ID:           "t1",
			Title:        "Build binary",
			Spec:         "Create cmd/dftwatch/main.go.",
			FilesPlanned: []string{"cmd/dftwatch/main.go"},
		}},
	}}
	warns := LintPlanPaths(prd, plan)
	if len(warns) != 2 || !strings.Contains(warns[0], "story") || !strings.Contains(warns[1], "task") {
		t.Fatalf("expected one story warning + one task warning for the same missing dir, got %q", warns)
	}
}

func TestLintPlanPathsNoProjectDirSilent(t *testing.T) {
	plan := []Story{{
		Title: "X",
		Tasks: []Task{{ID: "t1", Title: "T", Spec: "touch cmd/nowhere/main.go", FilesPlanned: []string{"cmd/nowhere/main.go"}}},
	}}
	if warns := LintPlanPaths(&PRD{ID: "p", ProjectDir: ""}, plan); len(warns) != 0 {
		t.Fatalf("empty ProjectDir must be silent, got %q", warns)
	}
	if warns := LintPlanPaths(&PRD{ID: "p", ProjectDir: filepath.Join(t.TempDir(), "missing")}, plan); len(warns) != 0 {
		t.Fatalf("missing ProjectDir on disk must be silent, got %q", warns)
	}
	if warns := LintPlanPaths(nil, plan); warns != nil {
		t.Fatalf("nil PRD must be silent, got %q", warns)
	}
}

// The decompose store path must still succeed (stories stored, PRD in
// needs_review) for a valid plan that carries a missing-dir path — the
// warning lands on the stored PRD, nothing fails.
func TestDecomposeStoresMissingDirPlanWithWarningNonBlocking(t *testing.T) {
	root := makePlanTree(t)
	planJSON := `{
		"title":"Add dftwatch",
		"stories":[{
			"title":"Add dftwatch command",
			"tasks":[{
				"title":"Build binary",
				"spec":"Create the entrypoint at cmd/dftwatch/main.go.",
				"files":["cmd/dftwatch/main.go"],
				"verification_script":".datawatch/verify/story1.sh"
			}]
		}]
	}`
	fn := func(req DecomposeRequest) (string, error) { return planJSON, nil }
	m, err := NewManager(t.TempDir(), DefaultConfig(), fn)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	prd, err := m.CreatePRD("add a dftwatch command", root, "", "", "")
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	updated, err := m.Decompose(prd.ID)
	if err != nil {
		t.Fatalf("Decompose must succeed despite a missing-dir warning: %v", err)
	}
	if updated.Status != PRDNeedsReview {
		t.Fatalf("status = %s, want needs_review (warning must not change outcome)", updated.Status)
	}
	if len(storedStoriesFor(m, prd.ID)) != 1 {
		t.Fatalf("stories were not stored")
	}
	found := false
	for _, w := range updated.ScopeWarnings {
		if strings.Contains(w, `task "Build binary"`) &&
			strings.Contains(w, `"cmd/dftwatch/main.go"`) &&
			strings.Contains(w, `"cmd/dftwatch"`) {
			found = true
		}
	}
	if !found {
		t.Fatalf("stored PRD missing the plan-path warning; ScopeWarnings = %q", updated.ScopeWarnings)
	}
	// A second run is not required, but a valid plan must record NO plan-path warning.
	planOK := `{
		"title":"Extend datch",
		"stories":[{
			"title":"Extend datch",
			"tasks":[{
				"title":"Add flags",
				"spec":"Add cmd/datch/flags.go.",
				"files":["cmd/datch/flags.go"],
				"verification_script":".datawatch/verify/story1.sh"
			}]
		}]
	}`
	fnOK := func(req DecomposeRequest) (string, error) { return planOK, nil }
	m2, err := NewManager(t.TempDir(), DefaultConfig(), fnOK)
	if err != nil {
		t.Fatalf("NewManager(2): %v", err)
	}
	prd2, _ := m2.CreatePRD("extend datch", root, "", "", "")
	upd2, err := m2.Decompose(prd2.ID)
	if err != nil {
		t.Fatalf("Decompose(2): %v", err)
	}
	if upd2.Status != PRDNeedsReview {
		t.Fatal("valid plan must still reach needs_review")
	}
	for _, w := range upd2.ScopeWarnings {
		if strings.Contains(w, "plan path:") {
			t.Fatalf("valid plan must not carry a plan-path warning, got %q", w)
		}
	}
}
