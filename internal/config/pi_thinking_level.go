package config

import (
	"fmt"
	"strings"
)

// PiThinkingLevels is the allowed-value set for pi.thinking_level, the
// level passed to the pi CLI's own --thinking flag on pi-prompt launches.
// Empty string (unset) is valid and means "don't pass --thinking; pi picks
// its own model default". pi natively also accepts "xhigh"/"max" with some
// models, but the datawatch surface deliberately pins the five levels that
// are stable across providers and models — an operator can still run pi
// manually with whatever its own binary accepts.
var PiThinkingLevels = []string{"off", "minimal", "low", "medium", "high"}

// IsPiThinkingLevel reports whether v is an allowed pi.thinking_level
// value. "" (unset/clear) is always allowed.
func IsPiThinkingLevel(v string) bool {
	if v == "" {
		return true
	}
	for _, l := range PiThinkingLevels {
		if v == l {
			return true
		}
	}
	return false
}

// ValidatePiThinkingLevel returns an operator-facing error for an invalid
// pi.thinking_level value. Used at the REST/MCP/CLI write edges so a bad
// value is rejected outright (400) instead of being stored and later
// tripping pi's own flag validation at launch time.
func ValidatePiThinkingLevel(v string) error {
	if IsPiThinkingLevel(v) {
		return nil
	}
	return fmt.Errorf("pi.thinking_level %q is not allowed; use one of %s (empty clears it)",
		v, strings.Join(PiThinkingLevels, ", "))
}
