package autonomous

import (
	"os"
	"path/filepath"
	"testing"
)

// TestPlanCoverageFilters_CreatedHelpers covers textCreatesFile,
// plannedNewFile and parentDirCreatable.
func TestPlanCoverageFilters_CreatedHelpers(t *testing.T) {
	t.Run("textCreatesFile", func(t *testing.T) {
		target := "internal/autonomous/plan_coverage_new.go"
		cases := []struct {
			name string
			text string
			path string
			want bool
		}{
			{
				name: "verb before path",
				text: "This task creates internal/autonomous/plan_coverage_new.go with the helpers.",
				path: target,
				want: true,
			},
			{
				name: "verb after path",
				text: "internal/autonomous/plan_coverage_new.go is generated at decompose time.",
				path: target,
				want: true,
			},
			{
				name: "create family",
				text: "The task created internal/autonomous/plan_coverage_new.go and a sibling test.",
				path: target,
				want: true,
			},
			{
				name: "adding conjugation",
				text: "Story one is adding internal/pkg/alpha.go to the tree.",
				path: "internal/pkg/alpha.go",
				want: true,
			},
			{
				name: "writes conjugation",
				text: "The bootstrap writes internal/pkg/beta.go on first run.",
				path: "internal/pkg/beta.go",
				want: true,
			},
			{
				name: "wrote conjugation",
				text: "A previous pass wrote internal/pkg/gamma.go already.",
				path: "internal/pkg/gamma.go",
				want: true,
			},
			{
				name: "generate verb",
				text: "The script generate internal/pkg/delta.go before tests run.",
				path: "internal/pkg/delta.go",
				want: true,
			},
			{
				name: "new file phrase, no verb",
				text: "A new file internal/pkg/omega.go will live near the rest.",
				path: "internal/pkg/omega.go",
				want: true,
			},
			{
				name: "negated create",
				text: "The plan must do not create internal/autonomous/never.go.",
				path: "internal/autonomous/never.go",
				want: false,
			},
			{
				name: "negated add",
				text: "We never add docs/x.md to this story.",
				path: "docs/x.md",
				want: false,
			},
			{
				name: "negated write",
				text: "The task must not write internal/pkg/deny.go.",
				path: "internal/pkg/deny.go",
				want: false,
			},
			{
				name: "clause names a different file",
				text: "This writes internal/pkg/other.go only.",
				path: "internal/pkg/wanted.go",
				want: false,
			},
			{
				name: "path with a dot inside a sentence",
				text: "The task creates internal/autonomous/plan_coverage_new.go and adds a matching table test.",
				path: target,
				want: true,
			},
			{
				name: "file named without any creation verb",
				text: "Modify internal/pkg/helper.go to change the read budget.",
				path: "internal/pkg/helper.go",
				want: false,
			},
			{
				name: "empty text",
				text: "",
				path: target,
				want: false,
			},
			{
				name: "verb in a different clause than the path",
				text: "Create internal/pkg/first.go. Then review internal/pkg/second.go.",
				path: "internal/pkg/second.go",
				want: false,
			},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				if got := textCreatesFile(tc.text, tc.path); got != tc.want {
					t.Errorf("textCreatesFile(%q, %q) = %v, want %v", tc.text, tc.path, got, tc.want)
				}
			})
		}
	})

	t.Run("plannedNewFile", func(t *testing.T) {
		stories := []Story{
			{
				FilesPlanned: []string{"docs/guide.md"},
				Tasks: []Task{
					{FilesPlanned: []string{"pkg/helper.go"}},
					{FilesPlanned: []string{"pkg/other.go"}},
				},
			},
		}
		cases := []struct {
			path string
			want bool
		}{
			{"pkg/helper.go", true}, // exact match in a task list
			{"helper.go", true},     // base-name match against pkg/helper.go
			{"docs/guide.md", true}, // story-level list
			{"pkg/other.go", true},  // exact match in another task
			{"pkg/missing.go", false},
			{"missing.go", false},
			{"", false},
		}
		for _, tc := range cases {
			t.Run(tc.path, func(t *testing.T) {
				if got := plannedNewFile(stories, tc.path); got != tc.want {
					t.Errorf("plannedNewFile(stories, %q) = %v, want %v", tc.path, got, tc.want)
				}
			})
		}
		if got := plannedNewFile(nil, "helper.go"); got {
			t.Errorf("plannedNewFile(nil, helper.go) = true, want false")
		}
	})

	t.Run("parentDirCreatable", func(t *testing.T) {
		base := t.TempDir()
		if err := os.MkdirAll(filepath.Join(base, "internal", "autonomous"), 0o755); err != nil {
			t.Fatal(err)
		}
		absBase := t.TempDir()
		if err := os.MkdirAll(filepath.Join(absBase, "abs", "pkg"), 0o755); err != nil {
			t.Fatal(err)
		}
		creatingStory := []Story{{
			Tasks: []Task{{
				Title:        "Scaffold the package",
				Spec:         "Mkdir a new directory internal/thing/now and add the package files there.",
				FilesPlanned: []string{"internal/thing/now/"},
			}},
		}}
		denyStory := []Story{{
			Tasks: []Task{{Title: "Touch only leaves", Spec: "Do not create any directory at all."}},
		}}
		cases := []struct {
			name  string
			base  string
			path  string
			want  bool
			story []Story
		}{
			{
				name: "existing parent directory",
				base: base,
				path: "internal/autonomous/plan_coverage_new.go",
				want: true,
			},
			{
				name: "root-level file",
				base: base,
				path: "go.mod",
				want: true,
			},
			{
				name: "missing directory, no task creates it",
				base: base,
				path: "internal/nope/thing.go",
				want: false,
			},
			{
				name:  "missing directory without creation clause",
				base:  base,
				path:  "internal/nope/thing.go",
				want:  false,
				story: denyStory,
			},
			{
				name:  "missing directory, task creates it",
				base:  base,
				path:  "internal/thing/now/impl.go",
				want:  true,
				story: creatingStory,
			},
			{
				name:  "missing directory, FilesPlanned parent with trailing slash",
				base:  base,
				path:  "internal/thing/slash/f.go",
				want:  true,
				story: []Story{{Tasks: []Task{{FilesPlanned: []string{"internal/thing/slash/"}}}}},
			},
			{
				name: "absolute parent exists, absolute path",
				base: "/irrelevant-base",
				path: filepath.Join(absBase, "abs", "pkg", "f.go"),
				want: true,
			},
			{
				name: "absolute parent missing",
				base: base,
				path: "/nonexistent-dw-123/pkg/f.go",
				want: false,
			},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				if got := parentDirCreatable(tc.base, tc.story, tc.path); got != tc.want {
					t.Errorf("parentDirCreatable(%q, stories, %q) = %v, want %v", tc.base, tc.path, got, tc.want)
				}
			})
		}
	})
}
