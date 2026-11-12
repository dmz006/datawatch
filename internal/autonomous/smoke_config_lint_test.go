package autonomous

import (
	"strings"
	"testing"
)

func TestLintSmokeConfig_Table(t *testing.T) {
	for _, tc := range []struct {
		name    string
		title   string
		spec    string
		kind    string
		problem bool
	}{
		{
			name:    "kind smoke cp home config dir",
			title:   "Bring up the daemon for the live smoke",
			spec:    "cp -r $HOME/.config into the repository and start the daemon so it reads the real config",
			kind:    TaskKindSmoke,
			problem: true,
		},
		{
			name:    "kind smoke rsync datawatch.yaml",
			title:   "Smoke test",
			spec:    "rsync the operator datawatch.yaml into this repo so the daemon picks it up unmodified",
			kind:    TaskKindSmoke,
			problem: true,
		},
		{
			name:    "title live check symlink home config",
			title:   "Live check of the operator daemon",
			spec:    "symlink /home/<user>/.config into the repository root and start the service",
			kind:    "",
			problem: true,
		},
		{
			name:    "kind smoke snapshot operator secrets",
			title:   "Capture the live state",
			spec:    "Snapshot the operator's secrets into the repo so the failure can be replayed",
			kind:    TaskKindSmoke,
			problem: true,
		},
		{
			name:    "spec says smoke, ln -s config folder",
			title:   "Get a repeatable environment",
			spec:    "For the smoke run, ln -s the operator's config directory into the repo checkout",
			kind:    "",
			problem: true,
		},
		{
			name:    "kind smoke cp database file",
			title:   "Live smoke",
			spec:    "copy the SQLite database file sqlite.db into the repository for inspection",
			kind:    TaskKindSmoke,
			problem: true,
		},
		{
			name:    "title smoke and database dir",
			title:   "Smoke the operator installation",
			spec:    "rsync the operator postgres database directory into the repo and point the daemon at it",
			kind:    "",
			problem: true,
		},
		{
			name:  "clean: synthetic config in temp dir",
			title: "Smoke test with a throwaway installation",
			spec: "Create a synthetic user in a temporary directory and point DATAWATCH_HOME at it; " +
				"write a minimal datawatch.yaml fixture of our own so the daemon starts against fake data",
			kind:    TaskKindSmoke,
			problem: false,
		},
		{
			name:    "clean: copy repo into scratch folder outside repo",
			title:   "Smoke the packaging",
			spec:    "Copy the repository sources into a scratch folder under /tmp outside the repository and run the smoke script there",
			kind:    TaskKindSmoke,
			problem: false,
		},
		{
			name:    "clean: mentions config and secrets without a copy verb",
			title:   "Verify production startup",
			spec:    "Check that the daemon loads its config and keeps its secrets readable but never writes them into the repo",
			kind:    TaskKindSmoke,
			problem: false,
		},
		{
			name:    "clean: snapshot of the worker's own build only",
			title:   "Smoke the release artefact",
			spec:    "Take a snapshot of the build artifacts produced in the scratch folder and attach their checksums",
			kind:    TaskKindSmoke,
			problem: false,
		},
		{
			name:    "non-smoke: copy operator config is out of scope for this rule",
			title:   "Refactor config loading",
			spec:    "Move the home config directory handling behind an interface; the migration script cp's examples from the docs",
			kind:    "feature",
			problem: false,
		},
		{
			name:    "non-smoke: plain task with cp and secrets wording",
			title:   "Add unit tests for the parser",
			spec:    "cp testdata into a temp dir; the parser must not leak secrets into test output",
			kind:    "",
			problem: false,
		},
		{
			name:    "kind final_gate is not smoke by kind, flags only via wording",
			title:   "End to end check",
			spec:    "Copy the operator datawatch.yaml into the repository",
			kind:    "final_gate",
			problem: false, // not a smoke/live-check task by kind and no smoke wording
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := LintSmokeConfig(tc.title, tc.spec, tc.kind)
			if tc.problem {
				if len(got) == 0 {
					t.Fatalf("expected a problem, got none")
				}
				for _, p := range got {
					if !strings.Contains(p, "operator") {
						t.Fatalf("problem %q does not name the operator data at risk", p)
					}
				}
			} else if len(got) != 0 {
				t.Fatalf("expected no problems, got %q", got)
			}
		})
	}
}

// TestLintPlanSmokeConfig_WiredIntoPlanLint asserts the smoke config lint runs
// in the plan-lint path next to the scope lint, against in-memory fake plans.
func TestLintPlanSmokeConfig_WiredIntoPlanLint(t *testing.T) {
	prd := &PRD{ID: "prd-smoke", Title: "Fake PRD", ProjectDir: "/home/<user>/proj"}
	stories := []Story{
		{
			ID: "s1", PRDID: prd.ID, Title: "Verify the daemon", Status: StoryPending,
			Tasks: []Task{
				{ID: "t1", Title: "Live check", Kind: TaskKindSmoke,
					Spec: "Snapshot the operator's secrets into the repo and start the daemon against them"},
				{ID: "t2", Title: "Smoke with synthetic data", Kind: TaskKindSmoke,
					Spec: "Build a synthetic config in a temp directory outside the repository and point the daemon at it"},
				{ID: "t3", Title: "Refactor", Kind: "feature",
					Spec: "Tidy the config loader"},
			},
		},
	}

	problems := LintPlanScope(prd, stories)
	if len(problems) == 0 {
		t.Fatal("expected plan-lint problems, got none")
	}
	var smokeProblems int
	for _, p := range problems {
		if strings.Contains(p, "operator") && !strings.Contains(p, "outside the allowed directories") {
			smokeProblems++
		}
	}
	if smokeProblems != 1 {
		t.Fatalf("expected exactly one smoke-config problem (task t1), got %d in %q", smokeProblems, problems)
	}
	for _, p := range problems {
		if strings.Contains(p, "operator") && strings.Contains(p, "outside the allowed directories") {
			t.Fatalf("scope-lint and smoke-lint messages confused: %q", p)
		}
	}
}
