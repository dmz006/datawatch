package autonomous

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestBuildWriteView_ShapeAndSize(t *testing.T) {
	const specMarker = "SPECMARKER-do-not-echo "
	bigSpec := strings.Repeat(specMarker, 10*1024/len(specMarker)+1)

	prd := &PRD{ID: "abcd1234", Status: PRDRunning, Rev: 7}
	story := Story{ID: "story001", PRDID: prd.ID, Status: StoryInProgress}
	for i := 0; i < 200; i++ {
		story.Tasks = append(story.Tasks, Task{
			ID:     fmt.Sprintf("task%03d", i),
			Title:  "task title",
			Spec:   bigSpec,
			Status: TaskPending,
		})
	}
	prd.Story = []Story{story}

	target := prd.Story[0].Tasks[42]
	view := BuildWriteView(prd, target.ID, "", "task updated")

	out, err := json.Marshal(view)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	if len(out) >= 512 {
		t.Fatalf("WriteView JSON is %d bytes, want under 512", len(out))
	}

	var keys map[string]json.RawMessage
	if err := json.Unmarshal(out, &keys); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	want := []string{"id", "status", "rev", "changed_task_id", "changed_task_status", "message"}
	if len(keys) != len(want) {
		t.Fatalf("got keys %v, want exactly %v", keys, want)
	}
	for _, k := range want {
		if _, ok := keys[k]; !ok {
			t.Errorf("missing key %q in %s", k, out)
		}
	}

	if strings.Contains(string(out), "SPECMARKER") {
		t.Errorf("WriteView leaked spec text: %s", out)
	}
	if view.ChangedTaskStatus != TaskPending {
		t.Errorf("ChangedTaskStatus = %q, want %q", view.ChangedTaskStatus, TaskPending)
	}
}

func TestBuildWriteView_UnknownTask(t *testing.T) {
	prd := &PRD{ID: "abcd1234", Status: PRDRunning, Rev: 3}
	prd.Story = []Story{{ID: "story001", Status: StoryPending, Tasks: []Task{
		{ID: "task0001", Status: TaskCompleted},
	}}}

	view := BuildWriteView(prd, "no-such-task", "", "unknown")

	out, err := json.Marshal(view)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(out, &keys); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	for k := range keys {
		if strings.HasPrefix(k, "changed_") {
			t.Errorf("unexpected key %q for unknown task: %s", k, out)
		}
	}
}
