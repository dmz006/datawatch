// merge_shadow_config_set.go — datawatch config set for the three
// autonomous.merge_shadow.* settings. The CLI validates with the daemon
// API's own rules (server.ValidateMergeShadowValue) before anything is
// sent, sends each value with its natural JSON type, and prints the help
// link that opens the matching definitions entry.
package main

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/server"
)

// mergeShadowHelpLink is the one-line help link printed after a successful
// set of any autonomous.merge_shadow.* key: the "Shadow merge plan" entry
// of the definitions, the same target the PWA help links open.
const mergeShadowHelpLink = "https://github.com/dmz006/datawatch/blob/main/docs/datawatch-definitions.md#shadow-merge-plan"

// mergeShadowConfigSet handles the autonomous.merge_shadow.* keys in
// runConfigSet. It returns handled=true for every key under the prefix,
// valid or not, so none of them reaches the generic raw-then-quoted path:
// a value the API would refuse is refused here with the API's own message,
// and an accepted one is PUT with its natural JSON type (boolean, duration
// string, number) and followed by the help link.
func mergeShadowConfigSet(cfg *config.Config, key, value string) (handled bool, err error) {
	field, ok := strings.CutPrefix(key, "autonomous.merge_shadow.")
	if !ok {
		return false, nil
	}
	value, typed, ok := mergeShadowTypedValue(field, value)
	if !ok {
		// The text is not the field's type; ask the API validator for the
		// refusal so the CLI and the daemon say the same thing.
		if verr := server.ValidateMergeShadowValue(field, value); verr != nil {
			return true, fmt.Errorf("config set: %w", verr)
		}
		return true, fmt.Errorf("config set: %s: %q is not a %s", key, value, mergeShadowFieldType(field))
	}
	if verr := server.ValidateMergeShadowValue(field, typed); verr != nil {
		return true, fmt.Errorf("config set: %w", verr)
	}
	body, merr := json.Marshal(map[string]any{key: typed})
	if merr != nil {
		return true, fmt.Errorf("config set: %w", merr)
	}
	purl := loopbackBaseURL(cfg) + "/api/config"
	if err := putConfig(purl, cfg.Server.Token, string(body)); err != nil {
		return true, fmt.Errorf("config set: %w", err)
	}
	fmt.Printf("Set %s = %s\n", key, value)
	fmt.Printf("Help: %s\n", mergeShadowHelpLink)
	return true, nil
}

// mergeShadowTypedValue maps the command-line text to the JSON type the
// daemon expects for each field: a boolean for enabled, the duration
// string as-is for gate_timeout, and a whole number for plan_keep (100.0
// parses — JSON cannot tell it from 100 — and the API validator decides).
func mergeShadowTypedValue(field, value string) (text string, typed any, ok bool) {
	switch field {
	case "enabled":
		b, err := strconv.ParseBool(strings.TrimSpace(value))
		if err != nil {
			return value, nil, false
		}
		return value, b, true
	case "gate_timeout":
		return value, strings.TrimSpace(value), true
	case "plan_keep":
		n, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
		if err != nil || n != math.Trunc(n) {
			return value, nil, false
		}
		return value, n, true
	}
	return value, nil, false
}

func mergeShadowFieldType(field string) string {
	switch field {
	case "enabled":
		return "true or false"
	case "gate_timeout":
		return "duration string such as \"20m\""
	case "plan_keep":
		return "positive whole number"
	}
	return "enabled, gate_timeout or plan_keep"
}
