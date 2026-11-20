package autonomous

import (
	"strings"
	"testing"
)

func allowedTestStories() []Story {
	return []Story{{
		Title: "First story",
		Tasks: []Task{
			{Title: "Add the widget", FilesPlanned: []string{"internal/widget/widget.go"}, VerificationScript: "scripts/verify-widget.sh"},
			{Title: "Document the widget", FilesPlanned: []string{"docs/howto/widget.md"}, FilesGenerated: []string{"internal/widget/widget_test.go"}},
		},
	}}
}

func allowedStandardList() string {
	return strings.Join(preflightStandardGeneratedFiles(), " ")
}

func TestVerifyScriptPreflightAllowed(t *testing.T) {
	t.Run("OmittedPathYieldsOneFinding", func(t *testing.T) {
		script := "#!/bin/bash\nALLOWED=\"internal/widget/widget.go internal/widget/widget_test.go scripts/verify-widget.sh " + allowedStandardList() + "\"\n"
		f := preflightAllowedFilesFindings("", "scripts/verify-widget.sh", []byte(script), allowedTestStories())
		if len(f) != 1 {
			t.Fatalf("findings = %+v, want one", f)
		}
		if f[0].RuleID != RuleVerifyScriptAllowedFiles || f[0].Line != 0 || f[0].Script != "scripts/verify-widget.sh" {
			t.Fatalf("finding = %+v", f[0])
		}
		if !strings.Contains(f[0].Message, "docs/howto/widget.md") || !strings.Contains(f[0].Message, "Document the widget") {
			t.Fatalf("message %q must name the path and its task", f[0].Message)
		}
	})
	t.Run("FullListYieldsNone", func(t *testing.T) {
		script := "#!/bin/bash\nALLOWED=\"internal/widget/ docs/howto/widget.md scripts/verify-widget.sh " + allowedStandardList() + "\"\n"
		if f := preflightAllowedFilesFindings("", "s.sh", []byte(script), allowedTestStories()); len(f) != 0 {
			t.Fatalf("findings = %+v, want none", f)
		}
	})
	t.Run("GitDiffListYieldsNone", func(t *testing.T) {
		script := "#!/bin/bash\nset -eu\nbase=$(git merge-base HEAD main)\nchanged=$(git diff --name-only \"$base\")\necho \"$changed\"\n"
		if f := preflightAllowedFilesFindings("", "s.sh", []byte(script), allowedTestStories()); len(f) != 0 {
			t.Fatalf("findings = %+v, want none", f)
		}
	})
	t.Run("CleanScriptYieldsNone", func(t *testing.T) {
		script := "#!/bin/bash\nset -eu\necho ok\nexit 0\n"
		if f := preflightAllowedFilesFindings("", "s.sh", []byte(script), allowedTestStories()); len(f) != 0 {
			t.Fatalf("findings = %+v, want none", f)
		}
	})
}
