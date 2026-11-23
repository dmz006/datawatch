package session

import (
	"io"
	"os"
	"strings"
)

// GoneEndNote is the end note recorded on a session whose process is gone and
// whose record was finalised by the gone-state rule.
const GoneEndNote = "process gone, finalised"

// GoneFinalState decides the true final state of a session whose process is
// gone, from the tail of its output. Terminal escape sequences are stripped and
// the last non-empty line is examined: when it begins with the completion
// marker the session finished its work and the state is StateComplete;
// otherwise the process vanished before finishing and the state is StateKilled.
// The second return value is always GoneEndNote.
func GoneFinalState(outputTail string) (State, string) {
	clean := StripANSI(outputTail)
	// Bare carriage returns (in-line overdraw) also act as line boundaries.
	clean = strings.ReplaceAll(clean, "\r", "\n")
	lines := strings.Split(clean, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, completionSummaryMarker) {
			return StateComplete, GoneEndNote
		}
		break
	}
	return StateKilled, GoneEndNote
}

// ReadOutputTail returns the last maxBytes bytes of the file at path. It
// returns the empty string when the file is missing, unreadable, or maxBytes is
// not positive; it never returns an error.
func ReadOutputTail(path string, maxBytes int64) string {
	if maxBytes <= 0 {
		return ""
	}
	f, err := os.Open(path) // #nosec G304 -- caller supplies the session output path
	if err != nil {
		return ""
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return ""
	}
	start := info.Size() - maxBytes
	if start < 0 {
		start = 0
	}
	if _, err := f.Seek(start, io.SeekStart); err != nil {
		return ""
	}
	data, err := io.ReadAll(f)
	if err != nil {
		return ""
	}
	return string(data)
}
