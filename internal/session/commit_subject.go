// Commit subject helpers for automatically created task commits.
//
// Task commits used to take the worker prompt as their subject. Prompts are
// multi-line instruction text, so that made the history unreadable. These
// helpers build a short "task(<prd>): <title>" subject from the bare task
// title and refuse any subject that is, or starts with, the prompt text.
//
// Every function here is pure: no git calls, no I/O.
package session

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

const (
	commitPRDShortIDRunes = 8
	commitSubjectMaxRunes = 120
	commitPromptSigRunes  = 60
	commitFallbackTitle   = "session work"
	commitPromptPhrase    = "You are running non-interactively"
)

// commitSubjectPrefix matches a leading "task(...): " or "task: " prefix.
var commitSubjectPrefix = regexp.MustCompile(`^task(\([^)]*\))?: `)

// prdShortID returns the first 8 characters of a PRD ID, or the whole ID when
// it is shorter. An empty ID yields an empty string.
func prdShortID(prdID string) string {
	return capRunes(prdID, commitPRDShortIDRunes)
}

// taskCommitSubject builds the commit subject for a task. The result is
// "task(<prdShortID>): <title>", or "task: <title>" when prdID is empty. The
// subject then passes through refusePromptSubject so prompt text never
// becomes the subject.
func taskCommitSubject(prdID, taskTitle, prompt string) string {
	title := normalizeCommitTitle(taskTitle)
	subject := "task: " + title
	if short := prdShortID(prdID); short != "" {
		subject = "task(" + short + "): " + title
	}
	return refusePromptSubject(subject, prompt, taskTitle)
}

// refusePromptSubject returns subject unchanged unless it is the prompt text.
// The subject is treated as prompt text when, after its task prefix is
// stripped, it equals or starts with the prompt's first non-empty line
// (trimmed, first 60 runes), or contains the non-interactive pipeline phrase.
// In that case it returns the title-based subject, keeping any prefix.
func refusePromptSubject(subject, prompt, title string) string {
	prefix := ""
	rest := subject
	if m := commitSubjectPrefix.FindString(subject); m != "" {
		prefix = m
		rest = subject[len(m):]
	}

	if !isPromptLike(rest, prompt) {
		return subject
	}

	if prefix == "" {
		prefix = "task: "
	}
	return prefix + normalizeCommitTitle(title)
}

// isPromptLike reports whether text is prompt text: it equals or starts with
// the prompt's first non-empty line, or contains the non-interactive pipeline
// phrase.
func isPromptLike(text, prompt string) bool {
	sig := promptSignature(prompt)
	return (sig != "" && (text == sig || strings.HasPrefix(text, sig))) ||
		strings.Contains(text, commitPromptPhrase)
}

// promptSignature returns the prompt's first non-empty line, trimmed and
// capped at 60 runes. It returns an empty string when the prompt is blank.
func promptSignature(prompt string) string {
	for _, line := range strings.Split(prompt, "\n") {
		if t := strings.TrimSpace(line); t != "" {
			return capRunes(t, commitPromptSigRunes)
		}
	}
	return ""
}

// snapshotSubject names the pre-session snapshot commit. The result is
// "snapshot before task <title> (PRD <prdShortID>)", with the PRD part
// omitted when prdID is empty. The title is collapsed to one line and capped
// at 120 runes; a blank title becomes "snapshot before session work".
func snapshotSubject(prdID, taskTitle string) string {
	subject := "snapshot before task " + normalizeCommitTitle(taskTitle)
	if strings.TrimSpace(taskTitle) == "" {
		subject = "snapshot before " + commitFallbackTitle
	}
	if short := prdShortID(prdID); short != "" {
		subject += " (PRD " + short + ")"
	}
	return subject
}

// normalizeCommitTitle collapses a title to one line and caps it at 120
// runes. A blank title becomes "session work".
func normalizeCommitTitle(title string) string {
	t := strings.Join(strings.Fields(title), " ")
	if t == "" {
		return commitFallbackTitle
	}
	return strings.TrimRight(capRunes(t, commitSubjectMaxRunes), " ")
}

// capRunes returns at most n runes of s, leaving shorter strings untouched.
func capRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}
