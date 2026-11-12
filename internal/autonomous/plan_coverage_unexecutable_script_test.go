package autonomous

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPlanCoverageUnexecutableVerificationScript(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "ok.sh"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	prd := &PRD{ProjectDir: dir}
	flagged := func(prd *PRD, task Task) bool {
		task.ID = "t1"
		stories := []Story{{ID: "s1", Title: "story", Tasks: []Task{task}}}
		for _, f := range CheckPlanStructure(prd, stories).Findings {
			if f.Kind == FindingUnexecutableVerification {
				if f.TaskID != "t1" || f.Detail == "" {
					t.Errorf("bad finding: %+v", f)
				}
				return true
			}
		}
		return false
	}
	tests := []struct {
		name string
		prd  *PRD
		task Task
		want bool
	}{
		{"command string", prd, Task{VerificationScript: "go test ./..."}, true},
		{"command string unknown base", nil, Task{VerificationScript: "go test ./..."}, true},
		{"missing path", prd, Task{VerificationScript: "missing.sh"}, true},
		{"existing executable", prd, Task{VerificationScript: "ok.sh"}, false},
		{"generated script", prd, Task{VerificationScript: "gen.sh", FilesGenerated: []string{"gen.sh"}}, false},
		{"empty script", prd, Task{VerificationScriptReason: "r"}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := flagged(tc.prd, tc.task); got != tc.want {
				t.Errorf("flagged=%v want %v", got, tc.want)
			}
		})
	}
}
