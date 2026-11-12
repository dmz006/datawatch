package config

import "strings"

// Values of autonomous.duplicate_session_keep: which live session survives when
// two or more live sessions match one task.
const (
	DuplicateSessionKeepOlder = "older"
	DuplicateSessionKeepNewer = "newer"
)

// NormalizeDuplicateSessionKeep returns the effective keep policy: "newer"
// only when configured so (case and space insensitive), otherwise "older".
func NormalizeDuplicateSessionKeep(v string) string {
	if strings.EqualFold(strings.TrimSpace(v), DuplicateSessionKeepNewer) {
		return DuplicateSessionKeepNewer
	}
	return DuplicateSessionKeepOlder
}
