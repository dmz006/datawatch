package autonomous

import (
	"strings"
	"testing"
)

func TestUncommittedQuestionTextNamesSkippedPaths(t *testing.T) {
	prd := &PRD{ID: "prd-1", Title: "Skipped note PRD"}
	task := &Task{ID: "task-1", Title: "Task 1"}
	files := []UncommittedFile{{Path: "a.txt"}}

	// (a) a cause carrying the skipped-paths note names the missing path.
	cause := UncommittedCauseCommitFailed + ": " + skippedMissingPathsNote([]string{"gone.txt"})
	text := uncommittedQuestionText(prd, task, files, cause)
	for _, want := range []string{"no longer exists", "gone.txt"} {
		if !strings.Contains(text, want) {
			t.Errorf("question text missing %q:\n%s", want, text)
		}
	}

	// (b) no skipped paths: empty note, and the plain cause has no note text.
	if got := skippedMissingPathsNote(nil); got != "" {
		t.Errorf("skippedMissingPathsNote(nil) = %q, want empty", got)
	}
	plain := uncommittedQuestionText(prd, task, files, UncommittedCauseCommitFailed)
	if strings.Contains(plain, "no longer exists") {
		t.Errorf("plain cause text unexpectedly mentions skipped paths:\n%s", plain)
	}
}
