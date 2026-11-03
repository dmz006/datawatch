// Package config — liveness rule for print-mode backends.
//
// Print-mode backends (named "<family>-prompt": pi-prompt, goose-prompt,
// opencode-prompt, ...) are headless one-run adapters, unlike interactive
// backends (claude-code, pi, goose, ...). The liveness rule governs how long
// a session on a backend may sit idle before it is considered waiting for
// input. The rule lives in the config package (not internal/session, which
// already imports config) so callers anywhere resolve effective values the
// same way; the isPromptFamily rule mirrors session.IsPromptBackend exactly.
package config

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// DefaultLivenessWindowSeconds is the liveness window (seconds) applied to
// print-mode backends when session.liveness_window_seconds is unset or 0.
const DefaultLivenessWindowSeconds = 120

// LivenessWindowSecondsEffective resolves session.liveness_window_seconds to
// the value callers must use: 0 (or unset) means the built-in default
// (DefaultLivenessWindowSeconds), a positive value is used verbatim, and a
// negative value is treated like 0 rather than silently used.
func (c *Config) LivenessWindowSecondsEffective() int {
	v := c.Session.LivenessWindowSeconds
	if v <= 0 {
		return DefaultLivenessWindowSeconds
	}
	return v
}

// promptModeIdleTimeoutDefault is the input-idle timeout (seconds) a
// print-mode backend gets when no per-backend override is set: 0, i.e. off.
// Print-mode runs are headless single prompts; they never wait on a human,
// so the interactive 10-second input-idle clock does not apply.
const promptModeIdleTimeoutDefault = 0

// effectiveSessionInputIdleTimeout resolves session.input_idle_timeout to the
// value callers must use: 0 (or unset) means the built-in default (10).
func (c *Config) effectiveSessionInputIdleTimeout() int {
	if c.Session.InputIdleTimeout > 0 {
		return c.Session.InputIdleTimeout
	}
	return 10
}

// isPromptFamily reports whether backendName names a print-mode backend, i.e.
// its lower-cased, trimmed name ends in "-prompt". Mirrors
// session.IsPromptBackend (internal/config cannot import internal/session).
func isPromptFamily(backendName string) bool {
	return strings.HasSuffix(strings.ToLower(strings.TrimSpace(backendName)), "-prompt")
}

// lookupLivenessIdleTimeoutOverride finds the operator's override for
// backendName in session.liveness_idle_timeout_overrides. Keys are backend
// family names ("pi" matches "pi-prompt"); full backend names ("pi-prompt")
// also match. Returns the override and whether a matching key was found.
func (c *Config) lookupLivenessIdleTimeoutOverride(backendName string) (int, bool) {
	if len(c.Session.LivenessIdleTimeoutOverrides) == 0 {
		return 0, false
	}
	if v, ok := c.Session.LivenessIdleTimeoutOverrides[backendName]; ok {
		return v, true
	}
	if isPromptFamily(backendName) {
		family := backendName[:len(backendName)-len("-prompt")]
		if v, ok := c.Session.LivenessIdleTimeoutOverrides[family]; ok {
			return v, true
		}
	}
	return 0, false
}

// LivenessInputIdleTimeoutEffective returns the input-idle timeout (seconds)
// a session on backendName uses, callers never reading raw zero values:
//
//  1. an explicit override in session.liveness_idle_timeout_overrides
//     (keyed by family name, e.g. "pi" for "pi-prompt", or by the full
//     backend name) wins, including an explicit 0;
//  2. otherwise a print-mode backend (name ends in "-prompt") defaults to 0,
//     i.e. off — headless runs never wait for input;
//  3. otherwise the global session.input_idle_timeout applies, 0 (or unset)
//     meaning its default of 10.
func (c *Config) LivenessInputIdleTimeoutEffective(backendName string) int {
	if v, ok := c.lookupLivenessIdleTimeoutOverride(strings.TrimSpace(backendName)); ok {
		return v
	}
	if isPromptFamily(backendName) {
		return promptModeIdleTimeoutDefault
	}
	return c.effectiveSessionInputIdleTimeout()
}

// ValidateLivenessSettings checks the two liveness settings before they are
// stored: window is session.liveness_window_seconds (0 means the default,
// DefaultLivenessWindowSeconds; negative is refused) and overrides is
// session.liveness_idle_timeout_overrides (backend name to whole seconds,
// 0 meaning off; negative is refused). Every override key must name a known
// backend: either a full name from knownBackends ("pi-prompt") or the family
// of a print-mode one ("pi" for "pi-prompt"), the same two forms
// lookupLivenessIdleTimeoutOverride resolves. Unknown names are refused with
// an error that names the key. A nil knownBackends skips the name check.
func ValidateLivenessSettings(window int, overrides map[string]int, knownBackends []string) error {
	if window < 0 {
		return fmt.Errorf("session.liveness_window_seconds: %d is invalid; use whole seconds >= 0 (0 = default %d)",
			window, DefaultLivenessWindowSeconds)
	}
	var known map[string]bool
	if knownBackends != nil {
		known = make(map[string]bool, len(knownBackends)*2)
		for _, n := range knownBackends {
			n = strings.ToLower(strings.TrimSpace(n))
			known[n] = true
			if isPromptFamily(n) {
				known[strings.TrimSuffix(n, "-prompt")] = true
			}
		}
	}
	keys := make([]string, 0, len(overrides))
	for k := range overrides {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if known != nil && !known[strings.ToLower(strings.TrimSpace(k))] {
			return fmt.Errorf("session.liveness_idle_timeout_overrides: unknown backend %q; use a backend name or family such as \"pi\" or \"pi-prompt\"", k)
		}
		if overrides[k] < 0 {
			return fmt.Errorf("session.liveness_idle_timeout_overrides.%s: %d is invalid; use whole seconds >= 0 (0 = off)", k, overrides[k])
		}
	}
	return nil
}

// ParseLivenessOverridesText reads the text forms an operator types for
// session.liveness_idle_timeout_overrides on the CLI, MCP and chat surfaces:
// a JSON object ({"pi":300}) or comma separated backend=seconds pairs
// (pi=300,goose=0). An empty string is an empty map, which clears the
// overrides. Anything else returns an error that says what was expected.
// Backend names and negative values are checked later by
// ValidateLivenessSettings, so every surface shares one refusal.
func ParseLivenessOverridesText(s string) (map[string]int, error) {
	const usage = "session.liveness_idle_timeout_overrides: %q is invalid; use a JSON object such as {\"pi\":300} or backend=seconds pairs such as pi=300,goose=0 (whole seconds, 0 = off)"
	s = strings.TrimSpace(s)
	if s == "" {
		return map[string]int{}, nil
	}
	out := map[string]int{}
	if strings.HasPrefix(s, "{") {
		var obj map[string]json.Number
		dec := json.NewDecoder(strings.NewReader(s))
		dec.UseNumber()
		if err := dec.Decode(&obj); err != nil {
			return nil, fmt.Errorf(usage, s)
		}
		for k, n := range obj {
			v, err := strconv.Atoi(n.String())
			if err != nil {
				return nil, fmt.Errorf(usage, s)
			}
			out[k] = v
		}
		return out, nil
	}
	for _, part := range strings.Split(s, ",") {
		k, v, found := strings.Cut(strings.TrimSpace(part), "=")
		k = strings.TrimSpace(k)
		n, err := strconv.Atoi(strings.TrimSpace(v))
		if !found || k == "" || err != nil {
			return nil, fmt.Errorf(usage, s)
		}
		out[k] = n
	}
	return out, nil
}
