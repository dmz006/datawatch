package autonomous

import (
	"strings"
	"testing"
)

// TestMergeOnlyDetector covers the IsMergeOnlySpec decision: specs whose
// entire scope is merging/rebasing/landing an existing finished branch
// are merge-only; specs carrying any of the work verbs (add, implement,
// create, write, fix, build) or no merge signal at all are not.
func TestMergeOnlyDetector(t *testing.T) {
	cases := []struct {
		name string
		spec string
		want bool
	}{
		{
			name: "merge finished branch is merge-only",
			spec: "Merge the finished branch into main and run the final verification.",
			want: true,
		},
		{
			name: "rebase then merge is merge-only",
			spec: "Rebase the existing branch onto the current main, resolve conflicts, and merge it.",
			want: true,
		},
		{
			name: "land finished branch is merge-only",
			spec: "The branch is finished; land it with a merge commit.",
			want: true,
		},
		{
			name: "merge with new work is not merge-only",
			spec: "Merge the finished branch and add the operator-facing docs for it.",
			want: false,
		},
		{
			name: "implement signal is not merge-only",
			spec: "Implement the merge queue and merge the finished branch.",
			want: false,
		},
		{
			name: "create signal is not merge-only",
			spec: "Create the release artifact and merge the finished branch.",
			want: false,
		},
		{
			name: "write signal is not merge-only",
			spec: "Write the release notes and then merge the finished branch.",
			want: false,
		},
		{
			name: "fix signal is not merge-only",
			spec: "Fix the failing check, then merge the finished branch.",
			want: false,
		},
		{
			name: "build signal is not merge-only",
			spec: "Build the package list changes and merge the finished branch.",
			want: false,
		},
		{
			name: "no merge signal is not merge-only",
			spec: "Add a new setting and cover it with tests.",
			want: false,
		},
		{
			name: "address is not the add verb",
			spec: "Address the merge review comments and land the finished branch.",
			want: true,
		},
		{
			name: "implementation is not the implement verb",
			spec: "Document the implementation choice, then merge the finished branch.",
			want: true,
		},
		{
			name: "case-insensitive merge signal",
			spec: "MERGE the finished BRANCH now.",
			want: true,
		},
		{
			name: "empty spec is not merge-only",
			spec: "",
			want: false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := IsMergeOnlySpec(c.spec); got != c.want {
				t.Errorf("IsMergeOnlySpec(%q) = %v, want %v", c.spec, got, c.want)
			}
		})
	}
}

// TestMergeOnlySpecGetsNoDocsSetTask asserts the docs set rule: applying
// the standard task templates with a merge-only spec appends no docs set
// task, while every other standard task (including the final gate) is
// still applied.
func TestMergeOnlySpecGetsNoDocsSetTask(t *testing.T) {
	story := testTaskTemplateStory(t)
	feature := "merge only spec"
	spec := "Merge the finished branch into main and run the final verification."

	got, applied := ApplyTaskTemplatesForSpec([]Story{story}, feature, "proj", spec, testTaskTemplateCfg())
	if len(got) != 1 {
		t.Fatalf("stories = %d, want 1", len(got))
	}

	var docsSet, finalGate bool
	for _, task := range got[0].Tasks {
		if strings.Contains(task.Title, "Docs set for") {
			docsSet = true
		}
		if strings.Contains(task.Title, "Final gate for") {
			finalGate = true
		}
	}
	if docsSet {
		t.Error("merge-only spec: a docs set task was appended, want none")
	}
	if !finalGate {
		t.Error("merge-only spec: the final gate task is missing, want it applied as usual")
	}

	for _, label := range applied {
		if strings.HasSuffix(label, "/"+TemplateDocsSet) {
			t.Errorf("applied labels contain a docs set entry: %q", label)
		}
	}
	gateApplied := false
	for _, label := range applied {
		if strings.HasSuffix(label, "/"+TemplateFinalGate) {
			gateApplied = true
		}
	}
	if !gateApplied {
		t.Errorf("applied labels: %+v (want the final gate applied)", applied)
	}
}

// TestOrdinarySpecKeepsDocsSetTask asserts the other side of the rule: a
// spec that carries new work keeps its docs set task when the standard
// task templates are applied with it.
func TestOrdinarySpecKeepsDocsSetTask(t *testing.T) {
	story := testTaskTemplateStory(t)
	feature := "task templates"
	spec := "Implement the merge queue and cover it with tests."

	got, applied := ApplyTaskTemplatesForSpec([]Story{story}, feature, "proj", spec, testTaskTemplateCfg())
	if len(got) != 1 {
		t.Fatalf("stories = %d, want 1", len(got))
	}

	var docsSet bool
	for _, task := range got[0].Tasks {
		if strings.Contains(task.Title, "Docs set for") {
			docsSet = true
		}
	}
	if !docsSet {
		t.Error("ordinary spec: the docs set task is missing, want it kept")
	}
	found := false
	for _, label := range applied {
		if strings.HasSuffix(label, "/"+TemplateDocsSet) {
			found = true
		}
	}
	if !found {
		t.Errorf("applied labels: %+v (want a docs set entry)", applied)
	}
}

// TestDecomposeGuidanceStatesMergeOnlyRule asserts the decompose
// guidance states the rule: a merge-only spec (only landing/merging
// existing branches, no new work) gets no docs set task, while any spec
// with new work keeps its docs set task.
func TestDecomposeGuidanceStatesMergeOnlyRule(t *testing.T) {
	m := &Manager{cfg: DefaultConfig()}
	block := m.decomposeGuidanceBlock()

	lower := strings.ToLower(block)
	for _, want := range []string{
		"merge-only spec rule",
		"merge",
		"rebase",
		"land",
		"docs set task",
		"skipped",
	} {
		if !strings.Contains(lower, strings.ToLower(want)) {
			t.Errorf("decompose guidance missing %q:\n%s", want, block)
		}
	}
}
