package autonomous

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestBuildTasksView(t *testing.T) {
	const specMarker = "SPECMARKER-do-not-echo "
	bigSpec := strings.Repeat(specMarker, 10*1024/len(specMarker)+1)

	prd := &PRD{
		ID:         "efgh5678",
		Title:      "tasks test",
		ProjectDir: "/tmp/project",
		Status:     PRDRunning,
		Rev:        4,
	}
	failed := Task{
		ID:     "task_failed",
		Title:  "failed task",
		Spec:   bigSpec,
		Status: TaskFailed,
		Error:  "build broke",
		Verification: &VerificationResult{
			OK:       false,
			Severity: "high",
			Summary:  "tests failed",
			Issues:   []string{"ISSUEMARKER-do-not-echo"},
			DiffSHA:  "DIFFSHAMARKER0123",
		},
		FailureReason: &FailureReason{
			Category: "infra",
			Detail:   "DETAILMARKER-do-not-echo",
			Evidence: []string{"EVIDENCEMARKER-do-not-echo"},
		},
		SessionID: "sess-1",
	}
	prd.Story = append(prd.Story, Story{
		ID:     "story_one",
		PRDID:  prd.ID,
		Title:  "story one",
		Status: StoryInProgress,
		Tasks:  []Task{failed},
	})

	view := BuildTasksView(prd)
	out, err := json.Marshal(view)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	js := string(out)

	for _, want := range []string{"build broke", "tests failed", `"infra"`, "sess-1"} {
		if !strings.Contains(js, want) {
			t.Errorf("TasksView JSON missing %q: %s", want, js)
		}
	}
	for _, banned := range []string{"SPECMARKER", "ISSUEMARKER", "DIFFSHAMARKER", "DETAILMARKER", "EVIDENCEMARKER"} {
		if strings.Contains(js, banned) {
			t.Errorf("TasksView JSON leaked %q", banned)
		}
	}
	if len(view.Tasks) != 1 {
		t.Fatalf("len(Tasks) = %d, want 1", len(view.Tasks))
	}
	if view.Tasks[0].Verification == nil || view.Tasks[0].Verification.Summary != "tests failed" {
		t.Errorf("verification summary not copied: %+v", view.Tasks[0].Verification)
	}
}

func TestBuildTasksViewSizeBudget(t *testing.T) {
	const specMarker = "SPECMARKER-do-not-echo "
	bigSpec := strings.Repeat(specMarker, 10*1024/len(specMarker)+1)

	prd := &PRD{ID: "ijkl9012", Title: "big", Status: PRDRunning}
	for s := 0; s < 10; s++ {
		story := Story{
			ID:     fmt.Sprintf("story%03d", s),
			PRDID:  prd.ID,
			Title:  "story title",
			Status: StoryInProgress,
		}
		for k := 0; k < 20; k++ {
			story.Tasks = append(story.Tasks, Task{
				ID:     fmt.Sprintf("task%03d_%02d", s, k),
				Title:  "task title",
				Spec:   bigSpec,
				Status: TaskPending,
			})
		}
		prd.Story = append(prd.Story, story)
	}

	view := BuildTasksView(prd)
	if len(view.Tasks) != 200 || view.TaskCount != 200 {
		t.Fatalf("len(Tasks)=%d TaskCount=%d, want 200", len(view.Tasks), view.TaskCount)
	}

	out, err := json.Marshal(view)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	if len(out) >= 60*1024 {
		t.Fatalf("TasksView JSON is %d bytes, want under 60 KB", len(out))
	}
	if strings.Contains(string(out), "SPECMARKER") {
		t.Errorf("TasksView leaked spec text")
	}
}

func TestBuildTasksViewNil(t *testing.T) {
	view := BuildTasksView(nil)
	if len(view.Tasks) != 0 || view.TaskCount != 0 {
		t.Errorf("nil PRD: got %+v, want zero view", view)
	}
}
