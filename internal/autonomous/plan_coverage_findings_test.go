package autonomous

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// These tests exercise CheckPlanStructure — the deterministic, no-LLM
// structural plan check (depends_on resolution, cycles, file
// provenance, verification exemption, tracking-ID naming). Everything
// is hermetic: no daemon, no network, no LLM; file-tree cases build a
// real temporary tree with t.TempDir().

// exemption is the canonical B124 exemption reason used by tests that
// want to isolate one rule at a time.
const exemption = "config-only change: nothing executable to script"

func findingOf(t *testing.T, r *PlanStructureReport, kind string) StructuralFinding {
	t.Helper()
	for _, f := range r.Findings {
		if f.Kind == kind {
			return f
		}
	}
	t.Fatalf("no finding of kind %q; got %+v", kind, r.Findings)
	return StructuralFinding{}
}

func hasFindingOf(r *PlanStructureReport, kind string) bool {
	for _, f := range r.Findings {
		if f.Kind == kind {
			return true
		}
	}
	return false
}

func TestPlanCoverageFindings_DanglingDependsOn(t *testing.T) {
	tests := []struct {
		name       string
		stories    []Story
		wantKind   string
		wantSubstr string // must appear in the finding's Detail, naming the bad reference
	}{
		{
			name: "task-level unknown reference",
			stories: []Story{{
				ID:    "s1",
				Title: "First story",
				Tasks: []Task{{
					ID:                       "t1",
					Title:                    "Do the work",
					DependsOn:                []string{"ghost"},
					VerificationScriptReason: exemption,
				}},
			}},
			wantKind:   FindingDanglingDependsOn,
			wantSubstr: "ghost",
		},
		{
			name: "story-level unknown reference",
			stories: []Story{
				{
					ID:    "s1",
					Title: "First story",
					Tasks: []Task{{
						ID:                       "t1",
						Title:                    "Do the work",
						VerificationScriptReason: exemption,
					}},
				},
				{
					ID:        "s2",
					Title:     "Second story",
					DependsOn: []string{"missing-story"},
					Tasks: []Task{{
						ID:                       "t2",
						Title:                    "Do more work",
						VerificationScriptReason: exemption,
					}},
				},
			},
			wantKind:   FindingDanglingDependsOn,
			wantSubstr: "missing-story",
		},
		{
			name: "dependency to a later story is a forward finding",
			stories: []Story{
				{
					ID:        "s1",
					Title:     "First story",
					DependsOn: []string{"s2"}, // s2 comes later in plan order
					Tasks: []Task{{
						ID:                       "t1",
						Title:                    "Do the work",
						VerificationScriptReason: exemption,
					}},
				},
				{
					ID:    "s2",
					Title: "Second story",
					Tasks: []Task{{
						ID:                       "t2",
						Title:                    "Do more work",
						VerificationScriptReason: exemption,
					}},
				},
			},
			wantKind:   FindingForwardDependsOn,
			wantSubstr: "s2",
		},
		{
			name: "same-story dependency is fine, earlier-story is fine",
			stories: []Story{
				{
					ID:    "s1",
					Title: "First story",
					Tasks: []Task{{
						ID:                       "t1",
						Title:                    "Found the layout",
						VerificationScriptReason: exemption,
					}},
				},
				{
					ID:    "s2",
					Title: "Second story",
					Tasks: []Task{
						{
							ID:                       "t2",
							Title:                    "Wire the layout",
							FilesPlanned:             []string{"layout.go"},
							DependsOn:                []string{"t1"}, // earlier story: allowed
							VerificationScriptReason: exemption,
						},
						{
							ID:                       "t3",
							Title:                    "Tune the layout",
							DependsOn:                []string{"t2"}, // same story: allowed
							VerificationScriptReason: exemption,
						},
					},
				},
			},
			wantKind: "", // must pass — no finding at all
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			report := CheckPlanStructure(&PRD{}, tc.stories)
			if tc.wantKind == "" {
				if !report.Pass || report.Count() != 0 {
					t.Fatalf("expected a clean pass, got %+v", report.Findings)
				}
				return
			}
			if report.Pass {
				t.Fatalf("expected the check to fail (finding of kind %s), got Pass=true", tc.wantKind)
			}
			f := findingOf(t, report, tc.wantKind)
			if !strings.Contains(f.Detail, tc.wantSubstr) {
				t.Fatalf("finding detail %q does not name the bad reference %q", f.Detail, tc.wantSubstr)
			}
		})
	}
}

func TestPlanCoverageFindings_DependencyCycle(t *testing.T) {
	tests := []struct {
		name       string
		stories    []Story
		wantDetail string
	}{
		{
			name: "task-level two-node cycle A to B to A",
			stories: []Story{{
				ID:    "s1",
				Title: "Story one",
				Tasks: []Task{
					{
						ID:                       "a",
						Title:                    "first step",
						DependsOn:                []string{"b"},
						VerificationScriptReason: exemption,
					},
					{
						ID:                       "b",
						Title:                    "second step",
						DependsOn:                []string{"a"},
						VerificationScriptReason: exemption,
					},
				},
			}},
			wantDetail: "dependency cycle: a -> b -> a",
		},
		{
			name: "self-loop is a cycle",
			stories: []Story{{
				ID:    "s1",
				Title: "Story one",
				Tasks: []Task{{
					ID:                       "c",
					Title:                    "step",
					DependsOn:                []string{"c"},
					VerificationScriptReason: exemption,
				}},
			}},
			wantDetail: "dependency cycle: c -> c",
		},
		{
			name: "story-level cycle",
			stories: []Story{
				{
					ID:        "s1",
					Title:     "Story one",
					DependsOn: []string{"s2"},
					Tasks: []Task{{
						ID:                       "t1",
						Title:                    "step",
						VerificationScriptReason: exemption,
					}},
				},
				{
					ID:        "s2",
					Title:     "Story two",
					DependsOn: []string{"s1"},
					Tasks: []Task{{
						ID:                       "t2",
						Title:                    "step",
						VerificationScriptReason: exemption,
					}},
				},
			},
			wantDetail: "dependency cycle: s1 -> s2 -> s1",
		},
		{
			name: "acyclic chain a<-b<-c passes",
			stories: []Story{{
				ID:    "s1",
				Title: "Story one",
				Tasks: []Task{
					{
						ID:                       "c",
						Title:                    "third",
						DependsOn:                []string{"b"},
						VerificationScriptReason: exemption,
					},
					{
						ID:                       "b",
						Title:                    "second",
						DependsOn:                []string{"a"},
						VerificationScriptReason: exemption,
					},
					{
						ID:                       "a",
						Title:                    "first",
						VerificationScriptReason: exemption,
					},
				},
			}},
			wantDetail: "", // must pass
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			report := CheckPlanStructure(&PRD{}, tc.stories)
			if tc.wantDetail == "" {
				if !report.Pass || report.Count() != 0 {
					t.Fatalf("expected a clean pass, got %+v", report.Findings)
				}
				return
			}
			if report.Pass {
				t.Fatal("expected the check to fail on a dependency cycle")
			}
			f := findingOf(t, report, FindingDependencyCycle)
			if f.Detail != tc.wantDetail {
				t.Fatalf("cycle detail = %q, want %q", f.Detail, tc.wantDetail)
			}
		})
	}
}

func TestPlanCoverageFindings_FileProvenance(t *testing.T) {
	t.Run("referenced file now in tree nor generated fails", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.MkdirAll(filepath.Join(dir, "other"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "other", "unrelated.txt"), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		report := CheckPlanStructure(&PRD{ProjectDir: dir}, []Story{{
			ID:    "s1",
			Title: "Story one",
			Tasks: []Task{{
				ID:                       "t1",
				Title:                    "Wire the reports/daily.txt reporter",
				VerificationScriptReason: exemption,
			}},
		}})
		if report.Pass {
			t.Fatal("expected the check to fail on an unresolved file")
		}
		f := findingOf(t, report, FindingUnresolvedFile)
		if !strings.Contains(f.Detail, "reports/daily.txt") {
			t.Fatalf("detail %q does not name the missing file", f.Detail)
		}
		if f.TaskID != "t1" {
			t.Fatalf("finding task = %q, want t1", f.TaskID)
		}
	})

	t.Run("file produced by an earlier task passes", func(t *testing.T) {
		dir := t.TempDir() // deliberately empty tree
		report := CheckPlanStructure(&PRD{ProjectDir: dir}, []Story{{
			ID:    "s1",
			Title: "Story one",
			Tasks: []Task{
				{
					ID:                       "t1",
					Title:                    "Scaffold the reporter",
					FilesGenerated:           []string{"reports/daily.txt"},
					VerificationScriptReason: exemption,
				},
				{
					ID:                       "t2",
					Title:                    "Wire the reports/daily.txt reporter",
					FilesPlanned:             []string{"reporter.go"},
					VerificationScriptReason: exemption,
				},
			},
		}})
		if !report.Pass {
			t.Fatalf("expected a clean pass, got %+v", report.Findings)
		}
	})

	t.Run("file present in the project tree passes", func(t *testing.T) {
		dir := t.TempDir()
		sub := filepath.Join(dir, "pkg", "util")
		if err := os.MkdirAll(sub, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(sub, "helper.go"), []byte("package util\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		report := CheckPlanStructure(&PRD{ProjectDir: dir}, []Story{{
			ID:    "s1",
			Title: "Story one",
			Tasks: []Task{{
				ID:                       "t1",
				Title:                    "Adjust pkg/util/helper.go behavior",
				VerificationScriptReason: exemption,
			}},
		}})
		if !report.Pass {
			t.Fatalf("expected a clean pass, got %+v", report.Findings)
		}
	})

	t.Run("no ProjectDir falls back to the current tree", func(t *testing.T) {
		dir := t.TempDir()
		t.Chdir(dir)
		sub := filepath.Join("pkg", "util")
		if err := os.MkdirAll(sub, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(sub, "helper.go"), []byte("package util\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		report := CheckPlanStructure(&PRD{}, []Story{{
			ID:    "s1",
			Title: "Story one",
			Tasks: []Task{{
				ID:                       "t1",
				Title:                    "Adjust pkg/util/helper.go behavior",
				VerificationScriptReason: exemption,
			}},
		}})
		if !report.Pass {
			t.Fatalf("expected a clean pass against the current tree, got %+v", report.Findings)
		}
	})

	t.Run("story FilesGenerated entry with no producing task fails", func(t *testing.T) {
		dir := t.TempDir()
		report := CheckPlanStructure(&PRD{ProjectDir: dir}, []Story{{
			ID:             "s1",
			Title:          "Story one",
			FilesGenerated: []string{"orphan/out.sh"},
			Tasks: []Task{{
				ID:                       "t1",
				Title:                    "Do the work",
				VerificationScriptReason: exemption,
			}},
		}})
		if report.Pass {
			t.Fatal("expected the check to fail on an orphaned story FilesGenerated entry")
		}
		f := findingOf(t, report, FindingUnresolvedFile)
		if !strings.Contains(f.Detail, "orphan/out.sh") {
			t.Fatalf("detail %q does not name the orphan entry", f.Detail)
		}
	})

	t.Run("story FilesGenerated entry with a producing task passes", func(t *testing.T) {
		dir := t.TempDir()
		report := CheckPlanStructure(&PRD{ProjectDir: dir}, []Story{{
			ID:             "s1",
			Title:          "Story one",
			FilesGenerated: []string{"verify/t1.sh"},
			Tasks: []Task{{
				ID:                       "t1",
				Title:                    "Write the check",
				FilesPlanned:             []string{"verify/t1.sh"},
				FilesGenerated:           []string{"verify/t1.sh"},
				VerificationScriptReason: exemption,
			}},
		}})
		if !report.Pass {
			t.Fatalf("expected a clean pass, got %+v", report.Findings)
		}
	})
}

func TestPlanCoverageFindings_VerificationReason(t *testing.T) {
	tests := []struct {
		name     string
		task     Task
		wantPass bool
	}{
		{
			name:     "no script and no reason fails",
			task:     Task{ID: "t1", Title: "Update the notes", FilesPlanned: []string{"NOTES.md"}},
			wantPass: false,
		},
		{
			name: "explicit B124 reason passes",
			task: Task{
				ID:                       "t1",
				Title:                    "Update the notes",
				FilesPlanned:             []string{"NOTES.md"},
				VerificationScriptReason: "docs-only change: nothing executable to check",
			},
			wantPass: true,
		},
		{
			name:     "verification script present passes",
			task:     Task{ID: "t1", Title: "Update the notes", FilesPlanned: []string{"NOTES.md"}, VerificationScript: "verify/t1.sh"},
			wantPass: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			report := CheckPlanStructure(&PRD{}, []Story{{
				ID:    "s1",
				Title: "Story one",
				Tasks: []Task{tc.task},
			}})
			if tc.wantPass {
				if !report.Pass || report.Count() != 0 {
					t.Fatalf("expected a clean pass, got %+v", report.Findings)
				}
				return
			}
			if report.Pass {
				t.Fatal("expected the check to fail on a task without verification or exemption")
			}
			f := findingOf(t, report, FindingMissingVerification)
			if !strings.Contains(f.Detail, "t1") || !strings.Contains(f.Detail, "B124") {
				t.Fatalf("detail %q does not identify the task and the B124 exemption", f.Detail)
			}
		})
	}
}

func TestPlanCoverageFindings_TrackerIDNaming(t *testing.T) {
	tests := []struct {
		name     string
		task     Task
		wantFail bool
	}{
		{
			name:     "title with BL124 fails",
			task:     Task{ID: "t1", Title: "BL124: fix the retry loop", VerificationScriptReason: exemption},
			wantFail: true,
		},
		{
			name: "lowercase bl9 in a generated name fails",
			task: Task{
				ID:                       "t1",
				Title:                    "Add the patch step",
				FilesGenerated:           []string{"bl9_patch.sh"},
				VerificationScriptReason: exemption,
			},
			wantFail: true,
		},
		{
			name:     "clean title passes",
			task:     Task{ID: "t1", Title: "Fix the retry loop", VerificationScriptReason: exemption},
			wantFail: false,
		},
		{
			name:     "containment match: rebl124 embeds the bl124 pattern and fails",
			task:     Task{ID: "t1", Title: "Refactor rebl124 handling", VerificationScriptReason: exemption},
			wantFail: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			report := CheckPlanStructure(&PRD{}, []Story{{
				ID:    "s1",
				Title: "Story one",
				Tasks: []Task{tc.task},
			}})
			if tc.wantFail {
				if report.Pass {
					t.Fatal("expected the check to fail on a tracker id in a name")
				}
				f := findingOf(t, report, FindingTrackerIDInName)
				if !strings.Contains(strings.ToLower(f.Detail), "bl") {
					t.Fatalf("detail %q does not name the tracker id", f.Detail)
				}
				return
			}
			if hasFindingOf(report, FindingTrackerIDInName) {
				t.Fatalf("unexpected tracker-id finding in %+v", report.Findings)
			}
		})
	}
}

// TestPlanCoverageFindings_FalsePositiveTokens pins the conservative
// file-token recognition rules: bare names that resolve somewhere in
// the project tree, dotted identifiers, slash word lists without an
// allowlisted final extension and a real directory head, and absent
// runtime artifacts must never surface as unresolved_file findings —
// while a genuinely missing bare name still does.
func TestPlanCoverageFindings_FalsePositiveTokens(t *testing.T) {
	dir := t.TempDir()
	mk := func(rel ...string) {
		t.Helper()
		p := filepath.Join(append([]string{dir}, rel...)...)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// The three bare names each live in a *different* subdirectory of
	// the temp tree: they must resolve by base name alone, wherever
	// they sit under the project base dir. None of the false-positive
	// tokens below has a matching file, and neither "task", "story",
	// "ollama" nor "absent" exists here as a directory.
	mk("pkg", "executor.go")
	mk("web", "app.js")
	mk("services", "manager.go")

	tokens := []string{
		"executor.go",
		"app.js",
		"manager.go",
		"GuardrailVerdict.Issues",
		"json.Unmarshal",
		"absent/null/empty",
		"task/story/PRD",
		"ollama/qwen3.8",
		"output.log",
	}
	for i, tok := range tokens {
		t.Run("token "+strconv.Itoa(i)+":"+strings.ReplaceAll(tok, "/", "_"), func(t *testing.T) {
			report := CheckPlanStructure(&PRD{ProjectDir: dir}, []Story{{
				ID:    "s1",
				Title: "Story one",
				Tasks: []Task{{
					ID:                       "t1",
					Title:                    "Touch " + tok,
					VerificationScriptReason: exemption,
				}},
			}})
			var misses []StructuralFinding
			for _, f := range report.Findings {
				if f.Kind == FindingUnresolvedFile {
					misses = append(misses, f)
				}
			}
			if len(misses) != 0 {
				t.Fatalf("token %q produced unresolved_file findings: %+v", tok, misses)
			}
		})
	}

	// Guard against masking: a bare allowlisted name that exists
	// nowhere in the tree must still be flagged.
	report := CheckPlanStructure(&PRD{ProjectDir: dir}, []Story{{
		ID:    "s1",
		Title: "Story one",
		Tasks: []Task{{
			ID:                       "t1",
			Title:                    "Touch definitely_missing_xyz.go",
			VerificationScriptReason: exemption,
		}},
	}})
	var f StructuralFinding
	found := false
	for _, x := range report.Findings {
		if x.Kind == FindingUnresolvedFile {
			found = true
			f = x
		}
	}
	if !found {
		t.Fatalf("expected unresolved_file for genuinely missing bare name; got %+v", report.Findings)
	}
	if !strings.Contains(f.Detail, "definitely_missing_xyz.go") {
		t.Fatalf("detail %q does not name the missing file", f.Detail)
	}
}

// TestPlanCoverageFindings_RealMisses pins rule 3's two-sided contract
// at the same time: references that resolve to a declaration (the
// referencing story's own FilesGenerated, a task's own FilesGenerated,
// or any earlier story's task outputs — exact path or bare base name)
// must NOT surface as unresolved_file, while references that resolve to
// nothing at that point in the plan MUST still do.
func TestPlanCoverageFindings_RealMisses(t *testing.T) {
	newStory := func(ID, title string, tasks ...Task) Story {
		return Story{ID: ID, Title: title, Tasks: tasks}
	}
	newTask := func(ID, title string, generated ...string) Task {
		return Task{
			ID:                       ID,
			Title:                    title,
			FilesGenerated:           generated,
			VerificationScriptReason: exemption,
		}
	}
	tests := []struct {
		name           string
		treeFiles      []string // relative files to create under the temp project dir
		stories        []Story
		wantUnresolved bool   // an unresolved_file finding must be present
		wantToken      string // the file name the finding must name (or that must be absent from findings when wantUnresolved is false)
	}{
		{
			name:      "path under an existing directory that is absent and never generated",
			treeFiles: []string{"internal/autonomous/existing.go"},
			stories: []Story{newStory("s1", "Story one",
				newTask("t1", "Add internal/autonomous/nonexistent_helper.go"),
			)},
			wantUnresolved: true,
			wantToken:      "nonexistent_helper.go",
		},
		{
			name:      "bare name with an allowlisted extension that exists nowhere and is never generated",
			treeFiles: []string{"other/unrelated.txt"},
			stories: []Story{newStory("s1", "Story one",
				newTask("t1", "Wire the ghost_widget_xyz.go reporter"),
			)},
			wantUnresolved: true,
			wantToken:      "ghost_widget_xyz.go",
		},
		{
			name: "file generated by a LATER task in the same story is a miss at the referencing point",
			stories: []Story{newStory("s1", "Story one",
				newTask("t1", "Consume helper.go"), // before the generation
				newTask("t2", "Create pkg/helper.go", "pkg/helper.go"),
			)},
			wantUnresolved: true,
			wantToken:      "helper.go",
		},
		{
			name:      "file generated in the same task is not flagged",
			treeFiles: []string{"other/unrelated.txt"},
			stories: []Story{newStory("s1", "Story one",
				newTask("t1", "Build the pkg/wizard.go engine", "pkg/wizard.go"),
			)},
			wantUnresolved: false,
			wantToken:      "wizard.go",
		},
		{
			name: "file generated in an earlier story is not flagged",
			stories: []Story{
				newStory("s1", "Story one",
					newTask("t1", "Produce the digest", "reports/summary.md")),
				newStory("s2", "Story two",
					newTask("t2", "Consume reports/summary.md")),
			},
			wantUnresolved: false,
			wantToken:      "summary.md",
		},
		{
			name: "story-level reference resolved by the story's own FilesGenerated and its task output",
			stories: []Story{newStory("s1", "Ship the verify/bootstrap.sh runner",
				newTask("t1", "Write the runner", "verify/bootstrap.sh"))},
			wantUnresolved: false,
			wantToken:      "bootstrap.sh",
		},
		{
			name: "story-level reference resolved by an earlier story's task output",
			stories: []Story{
				newStory("s1", "Story one",
					newTask("t1", "Produce the layout", "assets/theme.css")),
				newStory("s2", "Theme the app assets/theme.css",
					newTask("t2", "Apply the theme")),
			},
			wantUnresolved: false,
			wantToken:      "theme.css",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			for _, f := range tc.treeFiles {
				args := append([]string{dir}, strings.Split(f, "/")...)
				p := filepath.Join(args...)
				if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			report := CheckPlanStructure(&PRD{ProjectDir: dir}, tc.stories)
			var misses []StructuralFinding
			for _, f := range report.Findings {
				if f.Kind == FindingUnresolvedFile {
					misses = append(misses, f)
				}
			}
			if tc.wantUnresolved {
				if len(misses) == 0 {
					t.Fatalf("expected an unresolved_file finding naming %q; got findings %+v", tc.wantToken, report.Findings)
				}
				for _, m := range misses {
					if !strings.Contains(m.Detail, tc.wantToken) {
						t.Fatalf("unresolved_file findings %+v do not name the miss %q", misses, tc.wantToken)
					}
				}
				return
			}
			if len(misses) != 0 {
				for _, m := range misses {
					if strings.Contains(m.Detail, tc.wantToken) {
						t.Fatalf("must not flag %q: %+v", tc.wantToken, m)
					}
				}
				t.Fatalf("unexpected unresolved_file findings: %+v", misses)
			}
		})
	}
}

// TestPlanCoverageFindings_AnyFindingFails checks rule 6: a finding of
// any single kind fails the whole check, and the count is the
// authoritative number for callers choosing warn vs block.
func TestPlanCoverageFindings_AnyFindingFails(t *testing.T) {
	report := CheckPlanStructure(&PRD{}, []Story{{
		ID:    "s1",
		Title: "Story one",
		Tasks: []Task{{
			// dangling dependency (rule 1) AND no verification/exemption (rule 4)
			ID:        "t1",
			Title:     "Do the work",
			DependsOn: []string{"phantom"},
		}},
	}})
	if report.Pass {
		t.Fatal("expected the check to fail")
	}
	if want := 2; report.Count() != want {
		t.Fatalf("Count() = %d, want %d (findings: %+v)", report.Count(), want, report.Findings)
	}
	if !hasFindingOf(report, FindingDanglingDependsOn) || !hasFindingOf(report, FindingMissingVerification) {
		t.Fatalf("expected both rule-1 and rule-4 findings, got %+v", report.Findings)
	}
}
