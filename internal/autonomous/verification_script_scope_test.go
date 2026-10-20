// Verification-script scope gate tests. A verification script must check
// only the files its task changes; a script whose only check is
// `gofmt -l .` / `gofmt -l ./...` / a whole package proves the tree was
// formatted, not that the task's work is correct.
// validateVerificationScriptScope is the mechanical check; these tests
// pin the gate end-to-end through Decompose (fake planner, fake PRD,
// fake script files in a t.TempDir project, no sessions) and directly
// against the validator itself.
package autonomous

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeFakeVerifyScript writes script content to <projDir>/<relPath>
// (creating parent directories) and returns the relative path.
func writeFakeVerifyScript(t *testing.T, projDir, relPath, content string) string {
	t.Helper()
	full := filepath.Join(projDir, filepath.FromSlash(relPath))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(full, []byte(content), 0o755); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return relPath
}

// taskWithScript returns one story with one task pointing at scriptPath.
func taskWithScript(scriptPath string) []Story {
	return []Story{{
		Title: "Story",
		Tasks: []Task{{
			Title:              "Task",
			Spec:               "Do the work",
			FilesPlanned:       []string{"internal/session/queue.go"},
			VerificationScript: scriptPath,
		}},
	}}
}

func TestValidateVerificationScriptScope_WholePackageScriptRejected(t *testing.T) {
	cases := []struct {
		name   string
		script string
	}{
		{name: "package path", script: "gofmt -l internal/session"},
		{name: "dot", script: "gofmt -l ."},
		{name: "double-dot-slash-ellipsis", script: "gofmt -l ./..."},
		{name: "directory with trailing slash", script: "gofmt -w internal/session/"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			projDir := t.TempDir()
			script := writeFakeVerifyScript(t, projDir, ".datawatch/verify/fake.sh", "#!/bin/sh\n"+tc.script+"\n")
			err := validateVerificationScriptScope(projDir, taskWithScript(script))
			if err == nil {
				t.Fatal("want error, got nil")
			}
			// The offending script must be named in the error.
			if !strings.Contains(err.Error(), ".datawatch/verify/fake.sh") {
				t.Fatalf("error %q does not name the script path", err)
			}
			if !strings.Contains(err.Error(), "only the files") {
				t.Fatalf("error %q does not contain %q", err, "only the files")
			}
		})
	}
}

func TestValidateVerificationScriptScope_ChangedFilesScriptAccepted(t *testing.T) {
	cases := []struct {
		name     string // description of the script line(s) that must pass
		content  string // fake script file written into the project (empty = file not written)
		skipFile bool   // VerificationScript points at a nonexistent file
		emptyDir bool   // projectDir is "" (validator must pass through)
	}{
		{name: "git diff name-only file list", content: "gofmt -l $(git diff --name-only -- '*.go')\n"},
		{name: "variable file", content: "gofmt -l \"$f\"\n"},
		{name: "single named go file", content: "gofmt -l internal/session/queue.go\n"},
		{name: "comment mentioning gofmt only", content: "# runs gofmt -l ./... over the whole tree one day\n"},
		{name: "missing script file is skipped", skipFile: true},
		{name: "empty projectDir", content: "gofmt -l .\n", emptyDir: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			projectDir := t.TempDir()
			scriptPath := ".datawatch/verify/fake.sh"
			if tc.emptyDir {
				projectDir = ""
			} else if !tc.skipFile {
				writeFakeVerifyScript(t, projectDir, scriptPath, "#!/bin/sh\n"+tc.content)
			}
			err := validateVerificationScriptScope(projectDir, taskWithScript(scriptPath))
			if err != nil {
				t.Fatalf("want nil error, got %v", err)
			}
		})
	}
}

func TestDecompose_WholePackageScriptRejected(t *testing.T) {
	fn := func(req DecomposeRequest) (string, error) {
		return `{"title":"Format session package","stories":[{"title":"Format","tasks":[{"title":"Format session package","spec":"Format the session package","files":["internal/session/queue.go"],"verification_script":".datawatch/verify/fake.sh"}]}]}`, nil
	}
	m, err := NewManager(t.TempDir(), DefaultConfig(), fn)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	projDir := t.TempDir()
	writeFakeVerifyScript(t, projDir, ".datawatch/verify/fake.sh", "#!/bin/sh\ngofmt -l internal/session\n")
	prd, _ := m.CreatePRD("format the session package", projDir, "", "", "")

	updated, err := m.Decompose(prd.ID)
	if err == nil {
		t.Fatal("Decompose: want error for whole-package verification script, got nil")
	}
	// The offending script must be named in the error.
	if !strings.Contains(err.Error(), ".datawatch/verify/fake.sh") {
		t.Fatalf("error %q does not name the script path", err)
	}
	if !strings.Contains(err.Error(), "only the files") {
		t.Fatalf("error %q does not contain %q", err, "only the files")
	}
	// Rejected plans must not land in needs_review and must store nothing.
	stored, ok := m.store.GetPRD(prd.ID)
	if !ok {
		t.Fatal("PRD missing from store")
	}
	if stored.Status == PRDNeedsReview {
		t.Fatalf("status = %s, want not needs_review", stored.Status)
	}
	if n := len(storedStoriesFor(m, prd.ID)); n != 0 {
		t.Fatalf("stored stories = %d, want 0", n)
	}
	if updated != nil && updated.Status == PRDNeedsReview {
		t.Fatalf("returned PRD status = %s, want not needs_review", updated.Status)
	}
}

func TestDecompose_ChangedFilesScriptAccepted(t *testing.T) {
	fn := func(req DecomposeRequest) (string, error) {
		return `{"title":"Fix queue","stories":[{"title":"Fix queue","tasks":[{"title":"Fix queue","spec":"Fix the queue bug","files":["internal/session/queue.go"],"verification_script":".datawatch/verify/fake.sh"}]}]}`, nil
	}
	m, err := NewManager(t.TempDir(), DefaultConfig(), fn)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	projDir := t.TempDir()
	writeFakeVerifyScript(t, projDir, ".datawatch/verify/fake.sh", "#!/bin/sh\ngofmt -l internal/session/queue.go\n")
	prd, _ := m.CreatePRD("fix the queue", projDir, "", "", "")

	updated, err := m.Decompose(prd.ID)
	if err != nil {
		t.Fatalf("Decompose: %v", err)
	}
	if updated.Status != PRDNeedsReview {
		t.Fatalf("status = %s, want needs_review", updated.Status)
	}
	if n := len(storedStoriesFor(m, prd.ID)); n != 1 {
		t.Fatalf("stored stories = %d, want 1", n)
	}
}
