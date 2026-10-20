package autonomous

import (
	"os"
	"path/filepath"
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
					ID:                   "t1",
					Title:                "Do the work",
					DependsOn:            []string{"ghost"},
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
						ID:                         "t1",
						Title:                      "Do the work",
						VerificationScriptReason:   exemption,
					}},
				},
				{
					ID:        "s2",
					Title:     "Second story",
					DependsOn: []string{"missing-story"},
					Tasks: []Task{{
						ID:                         "t2",
						Title:                      "Do more work",
						VerificationScriptReason:   exemption,
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
						ID:                         "t1",
						Title:                      "Do the work",
						VerificationScriptReason:   exemption,
					}},
				},
				{
					ID:    "s2",
					Title: "Second story",
					Tasks: []Task{{
						ID:                         "t2",
						Title:                      "Do more work",
						VerificationScriptReason:   exemption,
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
						ID:                         "t1",
						Title:                      "Found the layout",
						VerificationScriptReason:   exemption,
					}},
				},
				{
					ID:    "s2",
					Title: "Second story",
					Tasks: []Task{
						{
							ID:                         "t2",
							Title:                      "Wire the layout",
							DependsOn:                  []string{"t1"}, // earlier story: allowed
							VerificationScriptReason:   exemption,
						},
						{
							ID:                         "t3",
							Title:                      "Tune the layout",
							DependsOn:                  []string{"t2"}, // same story: allowed
							VerificationScriptReason:   exemption,
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
		name   string
		stories []Story
		wantDetail string
	}{
		{
			name: "task-level two-node cycle A to B to A",
			stories: []Story{{
				ID:    "s1",
				Title: "Story one",
				Tasks: []Task{
					{
						ID:                         "a",
						Title:                      "first step",
						DependsOn:                  []string{"b"},
						VerificationScriptReason:   exemption,
					},
					{
						ID:                         "b",
						Title:                      "second step",
						DependsOn:                  []string{"a"},
						VerificationScriptReason:   exemption,
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
					ID:                         "c",
					Title:                      "step",
					DependsOn:                  []string{"c"},
					VerificationScriptReason:   exemption,
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
						ID:                     "t1",
						Title: "step",
						VerificationScriptReason: exemption,
					}},
				},
				{
					ID:        "s2",
					Title:     "Story two",
					DependsOn: []string{"s1"},
					Tasks: []Task{{
						ID:                     "t2",
						Title: "step",
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
						ID:                     "c",
						Title: "third",
						DependsOn:            []string{"b"},
						VerificationScriptReason: exemption,
					},
					{
						ID:                     "b",
						Title: "second",
						DependsOn:            []string{"a"},
						VerificationScriptReason: exemption,
					},
					{
						ID:                     "a",
						Title: "first",
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
				ID:                     "t1",
				Title: "Wire the reports/daily.txt reporter",
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
					ID:                         "t1",
					Title:                      "Scaffold the reporter",
					FilesGenerated:             []string{"reports/daily.txt"},
					VerificationScriptReason:   exemption,
				},
				{
					ID:                     "t2",
					Title: "Wire the reports/daily.txt reporter",
					VerificationScriptReason:   exemption,
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
				ID:                     "t1",
				Title: "Adjust pkg/util/helper.go behavior",
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
				ID:                     "t1",
				Title: "Adjust pkg/util/helper.go behavior",
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
				ID:                     "t1",
				Title: "Do the work",
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
				ID:                         "t1",
				Title:                      "Write the check",
				FilesGenerated:             []string{"verify/t1.sh"},
				VerificationScriptReason:   exemption,
			}},
		}})
		if !report.Pass {
			t.Fatalf("expected a clean pass, got %+v", report.Findings)
		}
	})
}

func TestPlanCoverageFindings_VerificationReason(t *testing.T) {
	tests := []struct {
		name    string
		task    Task
		wantPass bool
	}{
		{
			name:     "no script and no reason fails",
			task:     Task{ID: "t1", Title: "Update the notes"},
			wantPass: false,
		},
		{
			name: "explicit B124 reason passes",
			task: Task{
				ID:                       "t1",
				Title:                    "Update the notes",
				VerificationScriptReason: "docs-only change: nothing executable to check",
			},
			wantPass: true,
		},
		{
			name:     "verification script present passes",
			task:     Task{ID: "t1", Title: "Update the notes", VerificationScript: "verify/t1.sh"},
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
				ID:                         "t1",
				Title:                      "Add the patch step",
				FilesGenerated:             []string{"bl9_patch.sh"},
				VerificationScriptReason:   exemption,
			},
			wantFail: true,
		},
		{
			name:     "clean title passes",
			task:     Task{ID: "t1", Title: "Fix the retry loop", VerificationScriptReason: exemption},
			wantFail: false,
		},
		{
			name: "containment match: rebl124 embeds the bl124 pattern and fails",
			task: Task{ID: "t1", Title: "Refactor rebl124 handling", VerificationScriptReason: exemption},
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
