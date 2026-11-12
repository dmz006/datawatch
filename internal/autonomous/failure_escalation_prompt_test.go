package autonomous

import (
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"
)

// longFailureAttempt returns attempt n with every text field long enough to
// push a prompt past its bound on its own.
func longFailureAttempt(n int) TaskAttempt {
	files := make([]string, 0, 50)
	for i := 0; i < 50; i++ {
		files = append(files, fmt.Sprintf("internal/pkg/module_%02d/file_%02d_with_a_long_name.go", n, i))
	}
	return TaskAttempt{
		Number:          n,
		Backend:         "opencode",
		Model:           "qwen3.8:27b",
		DurationSeconds: 900,
		Outcome:         attemptOutcomeFailed,
		FailureCategory: "content",
		FailureSummary:  strings.Repeat("failure detail ", 300),
		VerifierSummary: strings.Repeat("the verifier rejected the change ", 200),
		VerifierIssues:  []string{strings.Repeat("issue text ", 500)},
		ChangedFiles:    files,
	}
}

// TestFailureEscalation_PromptSizeBound fills a task with many long attempts.
// The prompt must stay within the bound, say that the oldest attempts were
// shortened, and keep the oldest attempts as one-line records.
func TestFailureEscalation_PromptSizeBound(t *testing.T) {
	task := &Task{}
	for n := 1; n <= 40; n++ {
		task.Attempts = append(task.Attempts, longFailureAttempt(n))
	}

	got := buildEscalationRetryPrompt(task)
	if len(got) > escalationPromptMaxChars {
		t.Fatalf("prompt is %d bytes, bound is %d", len(got), escalationPromptMaxChars)
	}
	if !strings.Contains(got, "shortened to fit the size limit") {
		t.Fatalf("prompt does not say the oldest attempts were shortened")
	}
	if strings.Contains(got, "Attempt 1\n") {
		t.Fatalf("oldest attempt must be shortened to one line, not in full form")
	}
	if !strings.Contains(got, "Attempt 1: worker opencode/qwen3.8:27b") {
		t.Fatalf("oldest attempt must keep its one-line record")
	}
	if !strings.Contains(got, "Attempt 40") {
		t.Fatalf("newest attempt must still appear")
	}
}

// TestFailureEscalation_PromptShortensOldestFirst checks that when only the
// oldest attempts need shortening, the newest stays in full form.
func TestFailureEscalation_PromptShortensOldestFirst(t *testing.T) {
	task := &Task{}
	for n := 1; n <= 12; n++ {
		task.Attempts = append(task.Attempts, longFailureAttempt(n))
	}
	got := buildEscalationRetryPrompt(task)
	if len(got) > escalationPromptMaxChars {
		t.Fatalf("prompt is %d bytes, bound is %d", len(got), escalationPromptMaxChars)
	}
	if !strings.Contains(got, "Attempt 12\n  Worker:") {
		t.Fatalf("newest attempt must stay in full form when older ones suffice")
	}
	if !strings.Contains(got, "Attempt 1: worker opencode/qwen3.8:27b") {
		t.Fatalf("oldest attempt must be shortened to one line")
	}
}

// TestFailureEscalation_PromptOmitsWhenShortLinesOverflow records more attempts
// than fit even as one line each. The oldest must be left out, the text must
// name them, and the newest must still appear.
func TestFailureEscalation_PromptOmitsWhenShortLinesOverflow(t *testing.T) {
	task := &Task{}
	for n := 1; n <= 300; n++ {
		task.Attempts = append(task.Attempts, TaskAttempt{
			Number: n, Backend: "opencode", Model: "m", DurationSeconds: 5,
			Outcome: attemptOutcomeFailed, VerifierSummary: "still broken, see issues",
		})
	}
	got := buildEscalationRetryPrompt(task)
	if len(got) > escalationPromptMaxChars {
		t.Fatalf("prompt is %d bytes, bound is %d", len(got), escalationPromptMaxChars)
	}
	if !strings.Contains(got, "are left out of these reports to fit the size limit") {
		t.Fatalf("prompt does not say attempts were left out")
	}
	if !strings.Contains(got, "Attempt 300: worker") {
		t.Fatalf("newest attempt must still appear")
	}
	if strings.Contains(got, "Attempt 1:") {
		t.Fatalf("oldest attempt must have been left out")
	}
}

// TestFailureEscalation_PromptSmallKeepsFullReports checks that a prompt within
// the bound is not shortened and carries no shortening note.
func TestFailureEscalation_PromptSmallKeepsFullReports(t *testing.T) {
	task := &Task{Attempts: []TaskAttempt{
		{Number: 1, Backend: "opencode", Model: "m", DurationSeconds: 12, Outcome: attemptOutcomeFailed,
			VerifierSummary: "first summary", VerifierIssues: []string{"alpha", "beta"}, ChangedFiles: []string{"a.go"}},
		{Number: 2, Backend: "claude-code", Model: "sonnet", DurationSeconds: 40, Outcome: attemptOutcomeStalled,
			FailureCategory: "infra", FailureSummary: "worker went quiet"},
	}}
	got := buildEscalationRetryPrompt(task)
	for _, want := range []string{
		"Attempt 1\n", "  Worker: opencode/m\n", "  Duration: 12 s\n", "  Outcome: failed\n",
		"  Verifier summary: first summary\n", "  - alpha\n", "  - beta\n", "    - a.go\n",
		"Attempt 2\n", "  Worker: claude-code/sonnet\n", "  Outcome: stalled (category infra)\n",
		"  Failure: worker went quiet\n", "  Files changed: none recorded\n",
		"Do not repeat an approach that already failed",
		"say so in your final message",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("prompt missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "shortened") || strings.Contains(got, "left out") {
		t.Fatalf("small prompt must not carry a shortening note:\n%s", got)
	}
	if strings.Index(got, "Attempt 1\n") > strings.Index(got, "Attempt 2\n") {
		t.Fatalf("attempts must be listed oldest first")
	}
}

// TestFailureEscalation_PromptIssuesBoundedPerAttempt checks that one attempt's
// verifier issues never exceed the per-attempt bound, even when the verifier
// wrote far more.
func TestFailureEscalation_PromptIssuesBoundedPerAttempt(t *testing.T) {
	a := TaskAttempt{Number: 1, Backend: "opencode", Outcome: attemptOutcomeFailed,
		VerifierIssues: []string{strings.Repeat("x", 10000), "second issue"}}
	block := formatEscalationAttemptFull(a)

	const hdr = "  Verifier issues:\n"
	start := strings.Index(block, hdr)
	end := strings.Index(block, "  Files changed")
	if start < 0 || end < 0 || end < start {
		t.Fatalf("issues section not found:\n%s", block)
	}
	issues := strings.TrimSuffix(block[start+len(hdr):end], "\n")
	if len(issues) > escalationPromptMaxIssueChars {
		t.Fatalf("issues section is %d bytes, bound is %d", len(issues), escalationPromptMaxIssueChars)
	}
	if !strings.Contains(issues, escalationTruncationMarker) {
		t.Fatalf("truncated issues must say so")
	}
}

// TestFailureEscalation_TruncateKeepsUTF8 checks that a cut never splits a
// multi-byte character and never exceeds the byte bound.
func TestFailureEscalation_TruncateKeepsUTF8(t *testing.T) {
	s := strings.Repeat("é", 1000) // two bytes per character
	got := truncateEscalationText(s, 101)
	if len(got) > 101 {
		t.Fatalf("truncated to %d bytes, want at most 101", len(got))
	}
	if !utf8.ValidString(got) {
		t.Fatalf("truncation split a character: %q", got)
	}
	if !strings.HasSuffix(got, escalationTruncationMarker) {
		t.Fatalf("truncated text must end with the marker: %q", got)
	}
	if short := "short"; truncateEscalationText(short, 100) != short {
		t.Fatalf("text within the bound must be returned unchanged")
	}
}

// TestFailureEscalation_PromptEmptyWithoutAttempts checks that a task with no
// recorded attempts produces no prompt.
func TestFailureEscalation_PromptEmptyWithoutAttempts(t *testing.T) {
	if got := buildEscalationRetryPrompt(&Task{}); got != "" {
		t.Fatalf("no attempts: want empty prompt, got %q", got)
	}
	if got := buildEscalationRetryPrompt(nil); got != "" {
		t.Fatalf("nil task: want empty prompt, got %q", got)
	}
}
