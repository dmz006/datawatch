// Runner evidence from a session's output.log: the file counterpart of
// RunnerEvidenceFrom, for when no live pane is available.
package autonomous

import (
	"os"
	"regexp"
	"strings"
)

// runnerLogTailLines is how many non-empty log lines are kept.
const runnerLogTailLines = 40

// terminalControlRe matches CSI and OSC escape sequences plus other two-byte
// escapes.
var terminalControlRe = regexp.MustCompile(
	`\x1b\[[0-?]*[ -/]*[@-~]|\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)|\x1b[@-Z\\-_]`)

// RunnerEvidenceFromLog builds evidence from the session output log at
// logPath. It keeps the last forty non-empty lines with terminal control
// sequences and carriage returns removed, and takes the exit status from the
// last "DATAWATCH_PI_FAILED: pi exited with status N" line the pi wrapper
// prints (parsed by ParsePiPromptExitStatus). A missing or unreadable file
// yields evidence carrying only node and model.
func RunnerEvidenceFromLog(logPath, node, model string) RunnerEvidence {
	ev := RunnerEvidence{Node: node, Model: model}
	data, err := os.ReadFile(logPath)
	if err != nil {
		return ev
	}
	clean := terminalControlRe.ReplaceAllString(string(data), "")
	clean = strings.ReplaceAll(clean, "\r", "")
	var lines []string
	for _, l := range strings.Split(clean, "\n") {
		if strings.TrimSpace(l) != "" {
			lines = append(lines, l)
		}
	}
	if status, ok := ParsePiPromptExitStatus(clean); ok {
		ev.ExitStatus = &status
	}
	if len(lines) > runnerLogTailLines {
		lines = lines[len(lines)-runnerLogTailLines:]
	}
	ev.LastOutput = lines
	return ev
}
