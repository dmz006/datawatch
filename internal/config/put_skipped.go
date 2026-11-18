package config

import (
	"encoding/json"
	"fmt"
	"strings"
)

// PutConfigSkippedNote reads a PUT /api/config reply body and, when the daemon
// skipped keys instead of storing them (an active profile that does not exist,
// a task routing block that fails validation), returns one line saying so.
// It returns "" when nothing was skipped or the body is not the daemon's JSON.
// The MCP config_set tool, the comm configure verb and CLI config set all show
// this line in place of a "Set ..." confirmation.
func PutConfigSkippedNote(body []byte) string {
	var resp struct {
		Skipped       []string `json:"skipped"`
		SkippedReason string   `json:"skipped_reason"`
		Problems      []string `json:"skipped_problems"`
	}
	if json.Unmarshal(body, &resp) != nil || len(resp.Skipped) == 0 {
		return ""
	}
	note := fmt.Sprintf("not stored: %s", strings.Join(resp.Skipped, ", "))
	if resp.SkippedReason != "" {
		note += " (" + resp.SkippedReason + ")"
	}
	if len(resp.Problems) > 0 {
		note += ": " + strings.Join(resp.Problems, "; ")
	}
	return note
}
