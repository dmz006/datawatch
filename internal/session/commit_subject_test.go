package session

import (
	"strings"
	"testing"
)

func TestPRDShortID(t *testing.T) {
	tests := []struct {
		name  string
		prdID string
		want  string
	}{
		{name: "long id is cut to 8", prdID: "d4c50001abcdef12", want: "d4c50001"},
		{name: "exactly 8 kept whole", prdID: "d4c50001", want: "d4c50001"},
		{name: "short id kept whole", prdID: "abc", want: "abc"},
		{name: "empty id", prdID: "", want: ""},
		{name: "multibyte cut by rune", prdID: "ééééééééé", want: "éééééééé"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := prdShortID(tc.prdID); got != tc.want {
				t.Errorf("prdShortID(%q) = %q, want %q", tc.prdID, got, tc.want)
			}
		})
	}
}

func TestTaskCommitSubject(t *testing.T) {
	longTitle := strings.Repeat("é", 130)
	tests := []struct {
		name      string
		prdID     string
		taskTitle string
		prompt    string
		want      string
	}{
		{
			name:      "normal title with prd",
			prdID:     "d4c50001abcdef12",
			taskTitle: "Add commit subject helper",
			prompt:    "Implement the helper.\nDetails follow.",
			want:      "task(d4c50001): Add commit subject helper",
		},
		{
			name:      "empty title becomes session work",
			prdID:     "d4c50001",
			taskTitle: "   \t ",
			prompt:    "",
			want:      "task(d4c50001): session work",
		},
		{
			name:      "title longer than 120 runes is capped by rune",
			prdID:     "d4c50001",
			taskTitle: longTitle,
			prompt:    "",
			want:      "task(d4c50001): " + strings.Repeat("é", 120),
		},
		{
			name:      "embedded newlines collapse to one line",
			prdID:     "d4c50001",
			taskTitle: "Fix the\n  parser\r\n\tbug",
			prompt:    "",
			want:      "task(d4c50001): Fix the parser bug",
		},
		{
			name:      "empty prd id omits the parenthesised part",
			prdID:     "",
			taskTitle: "Do thing",
			prompt:    "",
			want:      "task: Do thing",
		},
		{
			name:      "subject equal to prompt text is replaced by title",
			prdID:     "d4c50001",
			taskTitle: "Wire the retry",
			prompt:    "Wire the retry\nthen run the tests",
			want:      "task(d4c50001): Wire the retry",
		},
		{
			name:      "pipeline prompt never becomes the subject",
			prdID:     "d4c50001",
			taskTitle: "Add tests",
			prompt:    "You are running non-interactively as part of an automated pipeline.\nDo the work.",
			want:      "task(d4c50001): Add tests",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := taskCommitSubject(tc.prdID, tc.taskTitle, tc.prompt); got != tc.want {
				t.Errorf("taskCommitSubject() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestPromptSubjectRefused(t *testing.T) {
	pipelinePrompt := "You are running non-interactively as part of an automated pipeline. Do X."
	tests := []struct {
		name    string
		subject string
		prompt  string
		title   string
		want    string
	}{
		{
			name:    "unrelated subject passes through",
			subject: "task(d4c50001): Add parser",
			prompt:  "Fix the build\nmore text",
			title:   "Add parser",
			want:    "task(d4c50001): Add parser",
		},
		{
			name:    "subject equal to prompt signature is refused",
			subject: "task(d4c50001): Fix the build",
			prompt:  "Fix the build\nmore text",
			title:   "Build fix",
			want:    "task(d4c50001): Build fix",
		},
		{
			name:    "subject starting with prompt signature is refused",
			subject: "Fix the build and also the tests",
			prompt:  "Fix the build\nmore text",
			title:   "Build fix",
			want:    "task: Build fix",
		},
		{
			name:    "signature is first 60 runes of trimmed first non-empty line",
			subject: "task(d4c50001): " + strings.Repeat("x", 60) + " tail",
			prompt:  "\n   \n" + strings.Repeat("x", 80) + "\nmore",
			title:   "Short",
			want:    "task(d4c50001): Short",
		},
		{
			name:    "pipeline prompt in subject is refused",
			subject: "task(d4c50001): " + pipelinePrompt,
			prompt:  pipelinePrompt,
			title:   "Add tests",
			want:    "task(d4c50001): Add tests",
		},
		{
			name:    "pipeline phrase without task prefix is refused",
			subject: "Some text. You are running non-interactively now",
			prompt:  "",
			title:   "Title here",
			want:    "task: Title here",
		},
		{
			name:    "empty prompt never refuses on prefix match",
			subject: "task: Anything",
			prompt:  "   \n\n",
			title:   "Anything else",
			want:    "task: Anything",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := refusePromptSubject(tc.subject, tc.prompt, tc.title); got != tc.want {
				t.Errorf("refusePromptSubject() = %q, want %q", got, tc.want)
			}
		})
	}
}
