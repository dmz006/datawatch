package autonomous

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestBuildSummaryView(t *testing.T) {
	const specMarker = "SPECMARKER-do-not-echo "
	bigSpec := strings.Repeat(specMarker, 10*1024/len(specMarker)+1)

	prd := &PRD{
		ID:         "abcd1234",
		Title:      "summary test",
		ProjectDir: "/tmp/project",
		Status:     PRDRunning,
		Rev:        9,
		Decisions:  make([]Decision, 3),
	}
	statuses := []TaskStatus{TaskPending, TaskCompleted, TaskInProgress, TaskFailed}
	for s := 0; s < 50; s++ {
		story := Story{
			ID:          fmt.Sprintf("story%03d", s),
			PRDID:       prd.ID,
			Title:       "story title",
			Description: bigSpec,
			Status:      StoryInProgress,
		}
		for k := 0; k < 4; k++ {
			story.Tasks = append(story.Tasks, Task{
				ID:     fmt.Sprintf("task%03d_%d", s, k),
				Title:  "task title",
				Spec:   bigSpec,
				Status: statuses[k],
			})
		}
		prd.Story = append(prd.Story, story)
	}

	view := BuildSummaryView(prd)

	out, err := json.Marshal(view)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	if len(out) >= 20*1024 {
		t.Fatalf("SummaryView JSON is %d bytes, want under 20 KB", len(out))
	}
	if strings.Contains(string(out), "SPECMARKER") {
		t.Errorf("SummaryView leaked spec text")
	}

	var tree interface{}
	if err := json.Unmarshal(out, &tree); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	forbidden := map[string]bool{"spec": true, "description": true, "decisions": true, "verdicts": true}
	var walk func(v interface{})
	walk = func(v interface{}) {
		switch node := v.(type) {
		case map[string]interface{}:
			for k, child := range node {
				if forbidden[k] {
					t.Errorf("SummaryView has forbidden key %q", k)
				}
				walk(child)
			}
		case []interface{}:
			for _, child := range node {
				walk(child)
			}
		}
	}
	walk(tree)

	if view.StoryCount != 50 || view.TaskCount != 200 {
		t.Errorf("StoryCount=%d TaskCount=%d, want 50 and 200", view.StoryCount, view.TaskCount)
	}
	if view.DecisionCount != 3 {
		t.Errorf("DecisionCount=%d, want 3", view.DecisionCount)
	}
	if view.CIWatch != nil {
		t.Errorf("CIWatch = %+v, want nil when the PRD has none", view.CIWatch)
	}
	for _, story := range view.Stories {
		sum := 0
		for _, n := range story.TaskStatusCounts {
			sum += n
		}
		if sum != story.TaskCount {
			t.Errorf("story %s: task_status_counts sum %d != task_count %d", story.ID, sum, story.TaskCount)
		}
		if story.TaskCount != 4 {
			t.Errorf("story %s: task_count %d, want 4", story.ID, story.TaskCount)
		}
	}
}

func TestBuildSummaryView_NilPRD(t *testing.T) {
	view := BuildSummaryView(nil)
	if view.ID != "" || view.StoryCount != 0 || view.TaskCount != 0 {
		t.Errorf("nil PRD gave non-zero view: %+v", view)
	}
}

// An untagged Automaton still carries "tags": [] and "primary_tag_id" in its
// JSON, so the list shape does not change with the tag count.
func TestBuildSummaryView_UntaggedCarriesEmptyTagFields(t *testing.T) {
	raw, err := json.Marshal(BuildSummaryView(&PRD{ID: "untagged01", Status: PRDDraft}))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got map[string]json.RawMessage
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if string(got["tags"]) != "[]" {
		t.Errorf("tags = %s, want []", got["tags"])
	}
	if string(got["primary_tag_id"]) != `""` {
		t.Errorf("primary_tag_id = %s, want empty string", got["primary_tag_id"])
	}
}
