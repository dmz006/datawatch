// Failure escalation — retry prompt.
//
// When a task has recorded failed attempts, the next worker is told what each
// earlier attempt tried and why it failed. buildEscalationRetryPrompt renders
// those records as plain text and keeps the result within
// escalationPromptMaxChars. The oldest attempts are shortened to one line
// first and left out last, and the text says so when that happens.
package autonomous

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// Size bounds for the escalation retry prompt. Lengths are byte counts, which
// are never less than character counts, so a bound met in bytes is met in
// characters too.
const (
	// escalationPromptMaxChars bounds the whole retry prompt.
	escalationPromptMaxChars = 12000
	// escalationPromptMaxIssueChars bounds the verifier issues of one attempt.
	escalationPromptMaxIssueChars = 2000
	// escalationPromptMaxSummaryChars bounds the failure and verifier
	// summaries of one attempt.
	escalationPromptMaxSummaryChars = 1000
	// escalationPromptMaxShortChars bounds the summary shown for an attempt
	// that has been shortened to one line.
	escalationPromptMaxShortChars = 200
	// escalationTruncationMarker is appended to any text cut to a bound.
	escalationTruncationMarker = " [truncated]"
)

// escalationRetryHint returns the retry hint for the next spawn of t. When
// escalation is disabled it returns hint unchanged, which is the legacy
// behaviour. When escalation is enabled and t has recorded failed attempts, it
// returns hint followed by the escalation retry prompt.
func (m *Manager) escalationRetryHint(prd *PRD, t *Task, hint string) string {
	if !m.resolveFailureEscalation(prd, t).Enabled {
		return hint
	}
	var prompt string
	m.store.WithLock(func() { prompt = buildEscalationRetryPrompt(t) })
	switch {
	case prompt == "":
		return hint
	case hint == "":
		return prompt
	default:
		return hint + "\n\n" + prompt
	}
}

// buildEscalationRetryPrompt renders the recorded failed attempts of t as the
// plain-text report the next worker reads before it starts. It returns "" when
// t has no recorded attempts. Each attempt is listed in order with its number,
// worker, duration, outcome, verifier summary and issues, and the files it
// changed. The result is at most escalationPromptMaxChars bytes: when it would
// be longer, the oldest attempts are shortened to one line each first, and
// then left out, and the text names the attempts affected.
//
// The caller must hold the store lock when t is shared, because the function
// reads t.Attempts.
func buildEscalationRetryPrompt(t *Task) string {
	if t == nil || len(t.Attempts) == 0 {
		return ""
	}
	attempts := t.Attempts
	n := len(attempts)
	full := make([]string, n)
	short := make([]string, n)
	for i, a := range attempts {
		full[i] = formatEscalationAttemptFull(a)
		short[i] = formatEscalationAttemptShort(a)
	}
	// shortened counts the oldest attempts shown as one line each. omitted
	// counts the oldest attempts left out entirely; it only grows once every
	// attempt is shortened, so omitted <= shortened always holds.
	shortened, omitted := 0, 0
	for {
		prompt := renderEscalationPrompt(attempts, full, short, shortened, omitted)
		if len(prompt) <= escalationPromptMaxChars || omitted == n {
			return prompt
		}
		if shortened < n {
			shortened++
		} else {
			omitted++
		}
	}
}

// renderEscalationPrompt assembles the prompt for the given shortening state.
// attempts[:omitted] are left out, attempts[omitted:shortened] appear as their
// short lines, and the rest appear in full.
func renderEscalationPrompt(attempts []TaskAttempt, full, short []string, shortened, omitted int) string {
	var b strings.Builder
	b.WriteString("Earlier attempts at this task did not finish successfully. Their reports follow, oldest first.\n")
	if omitted > 0 {
		fmt.Fprintf(&b, "Attempts %d to %d are left out of these reports to fit the size limit.\n",
			attempts[0].Number, attempts[omitted-1].Number)
	}
	if shortened > omitted {
		fmt.Fprintf(&b, "The oldest attempts are shortened to fit the size limit: attempts %d to %d are shown in one line each.\n",
			attempts[omitted].Number, attempts[shortened-1].Number)
	}
	b.WriteString("\n")
	for i := omitted; i < len(attempts); i++ {
		if i < shortened {
			b.WriteString(short[i])
		} else {
			b.WriteString(full[i])
		}
	}
	b.WriteString("\nRead these reports before you start. Do not repeat an approach that already failed; choose a different one.")
	b.WriteString(" If the specification itself looks wrong, say so in your final message and give the reason.\n")
	return b.String()
}

// formatEscalationAttemptFull renders one attempt as a multi-line block.
func formatEscalationAttemptFull(a TaskAttempt) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Attempt %d\n", a.Number)
	fmt.Fprintf(&b, "  Worker: %s\n", escalationWorkerLabel(a))
	fmt.Fprintf(&b, "  Duration: %d s\n", a.DurationSeconds)
	fmt.Fprintf(&b, "  Outcome: %s\n", escalationOutcomeLabel(a))
	if a.FailureSummary != "" {
		fmt.Fprintf(&b, "  Failure: %s\n", truncateEscalationText(a.FailureSummary, escalationPromptMaxSummaryChars))
	}
	if a.VerifierSummary != "" {
		fmt.Fprintf(&b, "  Verifier summary: %s\n", truncateEscalationText(a.VerifierSummary, escalationPromptMaxSummaryChars))
	}
	if len(a.VerifierIssues) > 0 {
		issues := truncateEscalationText("  - "+strings.Join(a.VerifierIssues, "\n  - "), escalationPromptMaxIssueChars)
		fmt.Fprintf(&b, "  Verifier issues:\n%s\n", issues)
	}
	if len(a.ChangedFiles) == 0 {
		b.WriteString("  Files changed: none recorded\n")
	} else {
		b.WriteString("  Files changed:\n")
		for _, f := range a.ChangedFiles {
			fmt.Fprintf(&b, "    - %s\n", f)
		}
	}
	return b.String()
}

// formatEscalationAttemptShort renders one attempt as a single line, keeping
// its number, worker, duration, outcome and the first line of its report.
func formatEscalationAttemptShort(a TaskAttempt) string {
	report := a.VerifierSummary
	if report == "" {
		report = a.FailureSummary
	}
	first, _, _ := strings.Cut(report, "\n")
	return fmt.Sprintf("Attempt %d: worker %s, %d s, %s. %s\n",
		a.Number, escalationWorkerLabel(a), a.DurationSeconds, escalationOutcomeLabel(a),
		truncateEscalationText(first, escalationPromptMaxShortChars))
}

// escalationWorkerLabel names the worker an attempt ran on as backend/model.
func escalationWorkerLabel(a TaskAttempt) string {
	backend := a.Backend
	if backend == "" {
		backend = "default worker"
	}
	model := a.Model
	if model == "" {
		model = "default model"
	}
	label := backend + "/" + model
	if a.Effort != "" {
		label += ", effort " + string(a.Effort)
	}
	return label
}

// escalationOutcomeLabel names how an attempt ended, with its failure category
// when one was recorded.
func escalationOutcomeLabel(a TaskAttempt) string {
	if a.FailureCategory == "" {
		return a.Outcome
	}
	return a.Outcome + " (category " + a.FailureCategory + ")"
}

// truncateEscalationText cuts s to at most max bytes, ending the cut text with
// escalationTruncationMarker. It never splits a multi-byte character.
func truncateEscalationText(s string, max int) string {
	if len(s) <= max {
		return s
	}
	cut := max - len(escalationTruncationMarker)
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + escalationTruncationMarker
}
