// Detector for a pi failure line already present in a session's output.log.
package autonomous

import (
	"os"
	"strings"
)

// piFailedLinePrefix starts every failure line the pi-prompt launch command
// prints, whatever the reason that follows.
const piFailedLinePrefix = "DATAWATCH_PI_FAILED"

// PiFailedLineEvidence reports whether the output log at logPath already
// contains a line starting with the pi failure marker. When it does it returns
// the evidence built by RunnerEvidenceFromLog and true; otherwise it returns
// the zero value and false. It reads the file once and never waits, so a
// missing, unreadable or still-quiet log simply yields false.
func PiFailedLineEvidence(logPath, node, model string) (RunnerEvidence, bool) {
	data, err := os.ReadFile(logPath)
	if err != nil {
		return RunnerEvidence{}, false
	}
	clean := terminalControlRe.ReplaceAllString(string(data), "")
	clean = strings.ReplaceAll(clean, "\r", "")
	for _, l := range strings.Split(clean, "\n") {
		if strings.HasPrefix(l, piFailedLinePrefix) {
			return RunnerEvidenceFromLog(logPath, node, model), true
		}
	}
	return RunnerEvidence{}, false
}
