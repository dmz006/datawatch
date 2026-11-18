// decompose_full_path_spec_test.go — regression tests pinned by the
// long-hyphenated file path spec feature request.
//
// Regression pinned: decomposed specs must keep file names complete so the
// plan-coverage file-token extractor never sees a half-name. The structural
// lint (plan_coverage_findings.go) extracts file-like tokens that stop at
// whitespace, so a path split by a space, shortened with an ellipsis, or
// wrapped into prose is reported as a file that "names file … which exists
// neither in the project tree … nor in an earlier task's FilesGenerated",
// even when a perfectly good file really does exist. The tests here pin the
// other half of that contract too: a spec that names one genuinely existing,
// long hyphenated path (no inner space) plus a "Run …" sentence, and nothing
// else, must produce NO "names file" finding from either the structural
// check or the decompose lint.
package autonomous

import (
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

func TestPlanningPromptsCarryFullPathRule(t *testing.T) {
	prompts := map[string]string{
		"PlanningPrompt":        PlanningPrompt,
		"PlanningPromptSession": PlanningPromptSession,
	}
	marker := "must be written exactly as its real repo-relative path"
	for name, p := range prompts {
		if !strings.Contains(p, planFullFilePathRuleBullet) {
			t.Errorf("%s is missing the full file path rule bullet", name)
		}
		if !strings.Contains(p, marker) {
			t.Errorf("%s is missing the marker %q", name, marker)
		}
		if !strings.Contains(planFullFilePathRuleBullet, marker) {
			t.Errorf("full path rule bullet does not contain the marker %q", marker)
		}
	}
}

// fullPathSpecPlannerJSON is the raw decompose JSON a planner would emit for
// the feature request: a title plus one story with one write task whose spec
// (a) names a long hyphenated path that really exists in this repo, with no
// space anywhere inside the name, and (b) contains a sentence beginning with
// "Run". It does not name any other file.
const fullPathSpecPlannerJSON = `{
  "title": "Keep decomposed-spec file names whole",
  "stories": [
    {
      "id": "s1",
      "title": "Reference the plan registry report test",
      "description": "A spec that names a long hyphenated path must keep the file name in one piece.",
      "files": ["scripts/check-plan-registry-reports-test.sh"],
      "tasks": [
        {
          "id": "t1",
          "title": "Write a note about scripts/check-plan-registry-reports-test.sh",
          "spec": "Update the note so it references scripts/check-plan-registry-reports-test.sh in full. Run the final gate verification and confirm the report file was written."
        }
      ]
    }
  ]
}`

// TestDecomposeSpecFullPathFileNames pins the regression: a spec that names
// one genuinely existing, long hyphenated path (no inner space) plus a "Run"
// sentence, and nothing else, must not be reported as a cut or missing file
// name by either the plan-coverage structural check or the decompose lint.
//
// The project dir is the repo root (this test package lives two levels below
// it), which keeps the check deterministic: the named script exists in the
// tree, so the file-token extractor — handed the full name, not a fragment —
// resolves it and reports nothing.
func TestDecomposeSpecFullPathFileNames(t *testing.T) {
	title, stories, err := ParsePlanning(fullPathSpecPlannerJSON)
	if err != nil {
		t.Fatalf("ParsePlanning failed: %v", err)
	}
	if title == "" {
		t.Errorf("ParsePlanning returned an empty title")
	}
	if len(stories) != 1 || len(stories[0].Tasks) != 1 {
		t.Fatalf("expected one story with one task, got %d stories", len(stories))
	}

	const namesFile = "names file"

	structReport := CheckPlanStructureWithOptions(
		&PRD{ProjectDir: "../../"},
		stories,
		PlanStructureOptions{},
	)
	for _, f := range structReport.Findings {
		if strings.Contains(f.Detail, namesFile) {
			t.Errorf("structural check reported a file name split: %q", f.Detail)
		}
	}

	lintFindings := LintTasks(stories, EffectiveRules(config.DecomposeLintConfig{}))
	for _, f := range lintFindings {
		if strings.Contains(f.Message, namesFile) {
			t.Errorf("decompose lint reported a file name split: %q", f.Message)
		}
		if strings.Contains(f.Matched, namesFile) {
			t.Errorf("decompose lint matched a file name split: %q", f.Matched)
		}
	}
}
