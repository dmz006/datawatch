// Whether a session's output ends with its completion marker: it reported
// done and nothing has happened since. A terminal UI keeps drawing below the
// last answer (status lines, the input box, footers), so "ends with" skips
// that decoration and nothing else.

package session

import (
	"regexp"
	"strings"
)

// maxCompletionWrapLines is how many wrapped or follow-on lines directly under
// a completion line still belong to it.
const maxCompletionWrapLines = 12

var (
	// turnStatusLine is the line left after a finished turn: "✻ Worked for 4m 2s".
	turnStatusLine = regexp.MustCompile(`^[✻✽✶✢✳·*∗]\s*\p{L}+ for \d+(\.\d+)?\s*[hms]`)
	// localCommandLine is a slash command typed at the prompt. Claude Code
	// answers its own commands without a model turn; a command that does
	// start one shows the answer on a line of its own, which ends the match.
	localCommandLine = regexp.MustCompile(`^❯[\s\x{00a0}]*/[a-z][\w:-]*(\s|$)`)
	// restoredFileLine lists a file Claude Code reloaded after compacting. A
	// tool call's own result reads "Read 49 lines" under its "●" line.
	// The path is one word; its line count may wrap to the next line.
	// Claude Code draws a no-break space after "⎿", which \s does not match.
	restoredFileLine = regexp.MustCompile(`^⎿[\s\x{00a0}]+(Referenced file\b.*|Read \S+( \(\d+( lines\))?)?)$`)
	// shellPromptLine is a bare shell prompt, as left when the agent exits.
	shellPromptLine = regexp.MustCompile(`^([\w.-]+@[\w.-]+(:[^$#%]*)?)?[$#%]$`)
)

// newTurnPrefixes open a new line of conversation: an input line, an assistant
// answer or a tool call.
var newTurnPrefixes = []string{"❯", "●", "⏺", "•", "⎿"}

// compactionNotes are what Claude Code prints when it compacts on its own.
var compactionNotes = []string{"● Compacted while idle", "✻ Conversation compacted"}

// trailingDecoration are the Claude Code notes that may follow a finished turn.
var trailingDecoration = []string{
	"⎿  Tip:", "⎿ Tip:", "● How is Claude doing this session?", "1: Bad", "new task? /clear",
}

// OutputEndsWithCompletion reports whether pane (a rendered pane capture)
// ends with a real completion marker: after the last such line there is only
// its own wrapped text and terminal decoration. Any later input, answer, tool
// call, running spinner or other text means the session moved on.
func OutputEndsWithCompletion(pane string) bool {
	text := strings.ReplaceAll(StripANSI(pane), "\r\n", "\n")
	lines := dropInputBox(strings.Split(strings.ReplaceAll(text, "\r", "\n"), "\n"))

	marker := -1
	for i := len(lines) - 1; i >= 0; i-- {
		l := strings.TrimSpace(lines[i])
		// An input line holds what somebody typed, not what the session said.
		if !strings.HasPrefix(l, "❯") && isRealCompletionLine(l, completionSummaryMarker) {
			marker = i
			break
		}
	}
	if marker < 0 {
		return false
	}

	i := marker + 1
	for n := 0; i < len(lines) && n < maxCompletionWrapLines; i, n = i+1, n+1 {
		l := strings.TrimSpace(lines[i])
		if l == "" || !strings.HasPrefix(lines[i], " ") || startsNewTurn(l) || isSpinnerLine(l) {
			break
		}
	}

	// block is the kind of client note the current paragraph belongs to, so
	// its wrapped lines (and a local command's reply) are read as part of it.
	block := noNote
	for ; i < len(lines); i++ {
		l := strings.TrimSpace(lines[i])
		wrapped := strings.HasPrefix(lines[i], " ") && !startsNewTurn(l)
		switch {
		case l == "":
			block = noNote
		case isSpinnerLine(l) || strings.Contains(l, "esc to interrupt"):
			return false // working now
		case localCommandLine.MatchString(l):
			block = commandNote
		case block == commandNote && (wrapped || strings.HasPrefix(l, "⎿")):
		case restoredFileLine.MatchString(l):
			block = clientNote
		case hasAnyPrefix(l, compactionNotes) || strings.HasPrefix(l, "※"):
			block = clientNote
		case block == clientNote && wrapped:
		case !isTrailingDecoration(l):
			return false
		}
	}
	return true
}

type noteKind int

const (
	noNote noteKind = iota
	commandNote
	clientNote
)

// dropInputBox cuts the input box and everything under it: the last "❯" row
// that sits right below the rule framing the box.
func dropInputBox(lines []string) []string {
	for i := len(lines) - 1; i >= 0; i-- {
		if !strings.HasPrefix(strings.TrimSpace(lines[i]), "❯") {
			continue
		}
		for j := i - 1; j >= 0; j-- {
			above := strings.TrimSpace(lines[j])
			if above == "" {
				continue
			}
			if isBoxSeparator(above) || strings.HasPrefix(above, "┌") || strings.HasPrefix(above, "╭") {
				return lines[:j]
			}
			break
		}
		break
	}
	return lines
}

func startsNewTurn(l string) bool { return hasAnyPrefix(l, newTurnPrefixes) }

func hasAnyPrefix(l string, prefixes []string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(l, p) {
			return true
		}
	}
	return false
}

func isTrailingDecoration(l string) bool {
	if hasAnyPrefix(l, trailingDecoration) {
		return true
	}
	if turnStatusLine.MatchString(l) || shellPromptLine.MatchString(l) || isBoxSeparator(l) {
		return true
	}
	// A line that opens a turn is never decoration, whatever it mentions.
	if startsNewTurn(l) && hasWord3(l) {
		return false
	}
	return isPureBoxDrawing(l) || isLabeledBorder(l) || strings.HasPrefix(l, "⏵") || isResponseNoiseLine(l)
}
