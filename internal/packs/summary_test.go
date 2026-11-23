package packs

import (
	"reflect"
	"testing"
)

func TestSummarizeListsTasksScriptsToolsAndScopes(t *testing.T) {
	p := &Pack{
		Name:    "review-pack",
		Version: "2.1.0",
		Tasks: []Task{
			{
				Title:              "Review the change",
				VerificationScript: "verify/review.sh",
				Tools:              []string{"read_file", "grep"},
				FileScopes:         []string{"internal/**", "docs/**"},
			},
			{
				Title:              "Check the tree",
				VerificationScript: "scripts/tree/check.sh",
				Tools:              []string{"grep", "bash"},
				FileScopes:         []string{"internal/**"},
			},
			{
				Title: "Summarise without a script",
			},
		},
	}
	got := Summarize(p)

	if got.Name != "review-pack" || got.Version != "2.1.0" {
		t.Errorf("identity = %q %q", got.Name, got.Version)
	}
	wantTitles := []string{"Review the change", "Check the tree", "Summarise without a script"}
	if !reflect.DeepEqual(got.TaskTitles, wantTitles) {
		t.Errorf("TaskTitles = %v, want %v", got.TaskTitles, wantTitles)
	}
	// Scripts are reduced to file names, sorted.
	wantScripts := []string{"check.sh", "review.sh"}
	if !reflect.DeepEqual(got.VerificationScripts, wantScripts) {
		t.Errorf("VerificationScripts = %v, want %v", got.VerificationScripts, wantScripts)
	}
	// Tools are de-duplicated and sorted.
	wantTools := []string{"bash", "grep", "read_file"}
	if !reflect.DeepEqual(got.Tools, wantTools) {
		t.Errorf("Tools = %v, want %v", got.Tools, wantTools)
	}
	wantScopes := []string{"docs/**", "internal/**"}
	if !reflect.DeepEqual(got.FileScopes, wantScopes) {
		t.Errorf("FileScopes = %v, want %v", got.FileScopes, wantScopes)
	}
}

func TestSummarizeSkipsBlankEntries(t *testing.T) {
	p := &Pack{
		Name:    "blank-pack",
		Version: "1.0.0",
		Tasks: []Task{
			{Title: "  ", VerificationScript: "  ", Tools: []string{"", " "}, FileScopes: []string{""}},
		},
	}
	got := Summarize(p)
	if len(got.TaskTitles) != 0 || len(got.VerificationScripts) != 0 || len(got.Tools) != 0 || len(got.FileScopes) != 0 {
		t.Errorf("blank entries not skipped: %+v", got)
	}
}

func TestSummarizeEmptyListsAreNotNil(t *testing.T) {
	got := Summarize(&Pack{Name: "bare", Version: "1.0.0"})
	if got.TaskTitles == nil || got.VerificationScripts == nil || got.Tools == nil || got.FileScopes == nil {
		t.Errorf("empty lists must be non-nil so they encode as []: %+v", got)
	}
}

func TestSummarizeNilPack(t *testing.T) {
	got := Summarize(nil)
	if got.Name != "" || got.Version != "" || got.TaskTitles == nil {
		t.Errorf("Summarize(nil) = %+v, want empty summary", got)
	}
}
