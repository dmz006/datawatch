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

// The skipped note names every git-ignored path it was given, and the
// all-skipped variant says nothing was committed.
func TestSkippedIgnoredPathsNoteNamesEachPath(t *testing.T) {
	paths := []string{".datawatch/verify/a.sh", ".datawatch/verify/b.sh"}
	note := skippedIgnoredPathsNote(paths, false)
	for _, p := range paths {
		if !strings.Contains(note, p) {
			t.Errorf("note %q must name %s", note, p)
		}
	}
	if strings.Contains(note, "nothing was committed") {
		t.Errorf("partial-skip note %q must not claim nothing was committed", note)
	}
	all := skippedIgnoredPathsNote(paths, true)
	if !strings.Contains(all, "nothing was committed") {
		t.Errorf("all-skipped note %q must say nothing was committed", all)
	}
}
