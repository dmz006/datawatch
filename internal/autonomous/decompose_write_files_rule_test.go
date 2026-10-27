package autonomous

import (
	"fmt"
	"strings"
	"testing"
)

func TestPlanningPromptsCarryWriteFilesRule(t *testing.T) {
	prompts := map[string]string{
		"PlanningPrompt":        PlanningPrompt,
		"PlanningPromptSession": PlanningPromptSession,
	}
	for name, p := range prompts {
		if !strings.Contains(p, planWriteFilesRuleBullet) {
			t.Errorf("%s is missing the write-files rule bullet", name)
		}
		if !strings.Contains(p, planReadOnlyMarker) {
			t.Errorf("%s does not contain the read-only marker %q", name, planReadOnlyMarker)
		}
		for _, v := range planWriteTaskVerbs {
			if !strings.Contains(p, v) {
				t.Errorf("%s does not contain write verb %q", name, v)
			}
			if !strings.Contains(planWriteFilesRuleBullet, v) {
				t.Errorf("rule bullet does not list write verb %q", v)
			}
		}
	}
}

func TestPlanningPromptStillFormatsWithOneArgument(t *testing.T) {
	out := fmt.Sprintf(PlanningPrompt, "FEATURE-REQUEST-TEXT")
	if strings.Contains(out, "%!") {
		t.Errorf("PlanningPrompt formatting produced an error marker: %q", out)
	}
	if !strings.Contains(out, "FEATURE-REQUEST-TEXT") {
		t.Error("PlanningPrompt did not substitute the feature request")
	}
	if strings.Contains(planWriteFilesRuleBullet, "%") {
		t.Error("rule bullet must not contain a percent sign")
	}
}
