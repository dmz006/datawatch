// Runner evidence — what the worker wait loop saw of a pi-prompt runner just
// before it declared a stall.
//
// The wait loop captures the visible pane and the runner's process list, turns
// them into a RunnerEvidence and stores it on the Task. recordFailedAttempt
// moves it onto the TaskAttempt it writes, so the operator's review report and
// the next attempt's prompt can say whether pi exited, with what status, and
// what was still running. Everything here is pure: text in, value out.
package autonomous

import (
	"strconv"
	"strings"
)

// piExitMarkerPrefix is the text the pi-prompt launch command prints before
// the exit status when pi returns non-zero. The backend builds it inside
// buildLaunchCmdWithAgentDir; it is repeated here so this package does not
// import the backend. A test keeps the two in step.
const piExitMarkerPrefix = "DATAWATCH_PI_FAILED: pi exited with status "

const (
	// runnerEvidenceTailLines is how many non-empty pane lines are kept.
	runnerEvidenceTailLines = 20
	// runnerEvidenceMaxProcesses caps the process entries kept.
	runnerEvidenceMaxProcesses = 40
)

// RunnerEvidence is the observed state of a runner when a task stalled.
type RunnerEvidence struct {
	// ExitStatus is pi's exit status when the pane showed the exit marker;
	// nil when no marker was seen.
	ExitStatus *int `json:"exit_status,omitempty"`
	// LastOutput is the last non-empty pane lines, oldest first.
	LastOutput []string `json:"last_output,omitempty"`
	// Processes describes the runner's processes at the time of the stall.
	Processes []string `json:"processes,omitempty"`
}

// ParsePiPromptExitStatus returns the exit status from the last pane line that
// starts with the pi exit marker. ok is false when no such line carries a
// number.
func ParsePiPromptExitStatus(pane string) (int, bool) {
	lines := strings.Split(pane, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimRight(lines[i], "\r")
		if !strings.HasPrefix(line, piExitMarkerPrefix) {
			continue
		}
		rest := line[len(piExitMarkerPrefix):]
		end := 0
		for end < len(rest) && rest[end] >= '0' && rest[end] <= '9' {
			end++
		}
		n, err := strconv.Atoi(rest[:end])
		if err != nil {
			return 0, false
		}
		return n, true
	}
	return 0, false
}

// RunnerEvidenceFrom builds the evidence for a pane capture and a process
// list. The tail is the last runnerEvidenceTailLines non-empty lines and the
// process list is cut to runnerEvidenceMaxProcesses entries.
func RunnerEvidenceFrom(pane string, processes []string) RunnerEvidence {
	ev := RunnerEvidence{LastOutput: LastLines(pane, runnerEvidenceTailLines)}
	if status, ok := ParsePiPromptExitStatus(pane); ok {
		ev.ExitStatus = &status
	}
	if len(processes) > runnerEvidenceMaxProcesses {
		processes = processes[:runnerEvidenceMaxProcesses]
	}
	ev.Processes = append([]string(nil), processes...)
	return ev
}
