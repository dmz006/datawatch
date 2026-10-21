package session

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const (
	taskCommitTempPRDID  = "0123456789abcdef0123456789abcdef"
	taskCommitTempTitle  = "Add retry helper"
	taskCommitTempPrompt = "You are running non-interactively as part of an automated pipeline. No human is available to answer questions."
	taskCommitTempBase   = "base.txt"
)

// taskCommitTempRepoCommit builds a throwaway git repository in t.TempDir,
// commits a base file, writes the changed files, and commits them through
// ProjectGit.CommitTaskWork. It returns the subject and full message of the
// new HEAD. Every git call runs inside the temporary repository.
func taskCommitTempRepoCommit(t *testing.T, label TaskCommitLabel, changed []string) (subject, body string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not found on PATH")
	}

	dir := t.TempDir()
	for _, args := range [][]string{
		{"init", "-b", "main"},
		{"config", "user.email", "t@x"},
		{"config", "user.name", "T"},
		{"config", "commit.gpgsign", "false"},
	} {
		if err := runGit(dir, args...); err != nil {
			t.Fatalf("git %v: %v", args, err)
		}
	}

	if err := os.WriteFile(filepath.Join(dir, taskCommitTempBase), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := runGit(dir, "add", taskCommitTempBase); err != nil {
		t.Fatalf("git add base: %v", err)
	}
	if err := runGit(dir, "commit", "-m", "base"); err != nil {
		t.Fatalf("git commit base: %v", err)
	}

	for _, f := range changed {
		path := filepath.Join(dir, f)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("changed "+f+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	sha, err := NewProjectGit(dir).CommitTaskWork("sess1", label, nil)
	if err != nil {
		t.Fatalf("CommitTaskWork: %v", err)
	}
	if sha == "" {
		t.Fatal("CommitTaskWork created no commit")
	}

	subject, err = gitOutput(dir, "log", "-1", "--format=%s")
	if err != nil {
		t.Fatalf("git log subject: %v", err)
	}
	body, err = gitOutput(dir, "log", "-1", "--format=%B")
	if err != nil {
		t.Fatalf("git log body: %v", err)
	}
	return strings.TrimSpace(subject), strings.TrimSpace(body)
}

func TestTaskCommitTempRepoSubjectEqualsTitle(t *testing.T) {
	label := TaskCommitLabel{
		PRDID:     taskCommitTempPRDID,
		TaskTitle: taskCommitTempTitle,
		Prompt:    taskCommitTempPrompt,
	}
	subject, _ := taskCommitTempRepoCommit(t, label, []string{"retry.go"})

	want := "task(01234567): " + taskCommitTempTitle
	if subject != want {
		t.Errorf("subject=%q want %q", subject, want)
	}
}

func TestTaskCommitTempRepoBodyListsEveryChangedFile(t *testing.T) {
	label := TaskCommitLabel{
		PRDID:     taskCommitTempPRDID,
		TaskTitle: taskCommitTempTitle,
		Prompt:    taskCommitTempPrompt,
	}
	files := []string{"retry.go", "retry_test.go", "docs/retry.md", taskCommitTempBase}
	_, body := taskCommitTempRepoCommit(t, label, files)

	bodyLines := strings.Split(body, "\n")
	fileLines := 0
	for _, line := range bodyLines {
		if strings.HasPrefix(line, "- ") {
			fileLines++
		}
	}
	if fileLines != len(files) {
		t.Errorf("body has %d file lines, want %d:\n%s", fileLines, len(files), body)
	}
	for _, f := range files {
		want := "- " + f
		found := false
		for _, line := range bodyLines {
			if line == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("body missing line %q:\n%s", want, body)
		}
	}
}

func TestTaskCommitTempRepoSubjectNeverPromptText(t *testing.T) {
	label := TaskCommitLabel{
		PRDID:     taskCommitTempPRDID,
		TaskTitle: taskCommitTempTitle,
		Prompt:    taskCommitTempPrompt,
	}
	subject, _ := taskCommitTempRepoCommit(t, label, []string{"retry.go"})

	if strings.HasPrefix(subject, "task: You are running") {
		t.Errorf("subject starts with prompt text: %q", subject)
	}
	if strings.Contains(subject, "You are running non-interactively") {
		t.Errorf("subject contains prompt text: %q", subject)
	}
}

func TestTaskCommitTempRepoEmptyTitleNotPromptText(t *testing.T) {
	label := TaskCommitLabel{
		PRDID:     taskCommitTempPRDID,
		TaskTitle: "",
		Prompt:    taskCommitTempPrompt,
	}
	subject, _ := taskCommitTempRepoCommit(t, label, []string{"retry.go"})

	if subject == "" {
		t.Fatal("subject is empty")
	}
	if strings.HasPrefix(subject, "task: You are running") ||
		strings.Contains(subject, "You are running non-interactively") ||
		subject == taskCommitTempPrompt {
		t.Errorf("empty title produced prompt-text subject: %q", subject)
	}
}

func TestTaskCommitTempRepoPromptSummaryKeepsTitleSubject(t *testing.T) {
	label := TaskCommitLabel{
		PRDID:     taskCommitTempPRDID,
		TaskTitle: taskCommitTempTitle,
		Prompt:    taskCommitTempPrompt,
		Summary:   taskCommitTempPrompt,
	}
	subject, body := taskCommitTempRepoCommit(t, label, []string{"retry.go"})

	want := "task(01234567): " + taskCommitTempTitle
	if subject != want {
		t.Errorf("subject=%q want %q", subject, want)
	}
	if strings.Contains(body, "You are running non-interactively") {
		t.Errorf("prompt-shaped summary leaked into body:\n%s", body)
	}
}
