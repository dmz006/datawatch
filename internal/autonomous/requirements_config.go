// requirements_config.go — shared validation for the
// autonomous.requirements min-Ollama overrides (the
// autonomous.requirements.min_ollama.<capability> config keys the REST / MCP /
// CLI / comm / web config surfaces accept and reject).
//
// Every surface reuses these functions so the reject rules are identical
// everywhere and live in exactly one place:
//
//   - ValidateMinOllamaOverrides: nil/empty clears all overrides and
//     validates; every key must be a known capability, every non-empty value
//     must be a parseable dotted semantic version; empty values clear that
//     capability's override.
//   - NormalizeMinOllamaOverrides: trims keys and values so stored config
//     and the in-code table compare on the same strings.

package autonomous

import (
	"fmt"
	"strings"
)

// ValidateMinOllamaOverrides checks a min_ollama capability→minimum-Ollama
// override map: nil or empty clears all overrides and validates; every key
// must be a known capability; every value must be empty (clear the override
// for that capability) or a parseable semantic version. It returns nil or an
// error naming the offending capability and value.
func ValidateMinOllamaOverrides(overrides map[string]string) error {
	if len(overrides) == 0 {
		return nil
	}
	known := KnownCapabilities()
	for cap, value := range overrides {
		cap = strings.TrimSpace(cap)
		if cap == "" {
			return fmt.Errorf("autonomous.requirements.min_ollama: capability key must not be empty")
		}
		if _, ok := minimumOllamaByCapability[cap]; !ok {
			return fmt.Errorf("autonomous.requirements.min_ollama: unknown capability %q (known capabilities: %s)",
				cap, strings.Join(known, ", "))
		}
		value = strings.TrimSpace(value)
		if value == "" {
			continue // empty clears the override for this capability
		}
		if _, ok := parseDotted(value); !ok {
			return fmt.Errorf("autonomous.requirements.min_ollama: capability %q: invalid minimum Ollama %q (expected a dotted semantic version, e.g. 0.35.0)",
				cap, value)
		}
	}
	return nil
}

// NormalizeMinOllamaOverrides returns a copy of overrides with keys and
// values trimmed. Empty-valued entries are kept: an empty value clears that
// capability's in-code floor, so it is meaningful. It validates first and
// returns the error unchanged on failure.
func NormalizeMinOllamaOverrides(overrides map[string]string) (map[string]string, error) {
	if err := ValidateMinOllamaOverrides(overrides); err != nil {
		return nil, err
	}
	if len(overrides) == 0 {
		return nil, nil
	}
	out := make(map[string]string, len(overrides))
	for cap, value := range overrides {
		out[strings.TrimSpace(cap)] = strings.TrimSpace(value)
	}
	return out, nil
}
