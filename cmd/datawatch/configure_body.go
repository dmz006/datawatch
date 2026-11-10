package main

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/dmz006/datawatch/internal/config"
)

// configureRequestBody builds the PUT /api/config body for one `configure
// <key>=<value>` chat command. true/false and values that are a complete JSON
// number go out as JSON literals; everything else, including a date such as
// 2026-01-01 or a path that merely starts with digits, goes out as a JSON string
// with the key and value escaped.
func configureRequestBody(key, value string) string {
	var val any = value
	if key == "session.liveness_idle_timeout_overrides" {
		// JSON object or backend=seconds pairs go out as an object. Text that
		// parses as neither goes out as a string, which the daemon refuses with
		// its own message.
		if m, err := config.ParseLivenessOverridesText(value); err == nil {
			val = m
		}
	} else if key == "plugins.services.per_service" {
		// The per-service map goes out as an object when it is one (service
		// name to lifecycle mode or {"lifecycle_mode": mode} entry), so the
		// daemon applies every entry; text that is not a JSON object goes out
		// as a string, which the daemon leaves for the per-service key form.
		var m map[string]any
		if json.Unmarshal([]byte(value), &m) == nil {
			val = m
		}
	} else if key == "autonomous.worker_profiles" || key == "autonomous.worker_profile_defaults" {
		// Reading these keys (a bare key with no "=") is not sent here: the
		// router answers it with the help text and the profile in force.
		// The worker profile tables are JSON objects. Text that is one goes
		// out as an object; anything else goes out as a string, which the
		// daemon refuses with its own message (the same validation as PUT
		// /api/config), so nothing is written.
		var m map[string]any
		if json.Unmarshal([]byte(value), &m) == nil && m != nil {
			val = m
		}
	} else if key == "autonomous.worker_profile" {
		// A profile name is always a string, even when it looks like a number.
		val = value
	} else if value == "true" || value == "false" {
		val = json.RawMessage(value)
	} else {
		var n json.Number
		if json.Unmarshal([]byte(value), &n) == nil {
			val = json.RawMessage(value)
		}
	}
	b, err := json.Marshal(map[string]any{key: val})
	if err != nil {
		return "{}"
	}
	return string(b)
}

// isLivenessConfigKey reports whether key is one of the two backend liveness settings.
func isLivenessConfigKey(key string) bool {
	return key == "session.liveness_window_seconds" || key == "session.liveness_idle_timeout_overrides"
}

// isAttemptCostDottedKey reports whether key names one field of one kind's
// first-attempt entry: autonomous.first_attempt.<kind>.backend or .model. Both
// values are strings, so the CLI always sends them as JSON strings.
func isAttemptCostDottedKey(key string) bool {
	rest, ok := strings.CutPrefix(key, "autonomous.first_attempt.")
	if !ok {
		return false
	}
	return strings.HasSuffix(rest, ".backend") || strings.HasSuffix(rest, ".model")
}

// isAttemptCostScalarKey reports whether key is one of the two whole-number
// attempt-cost settings. A value that is valid JSON and refused by the daemon
// keeps the daemon's message instead of being quoted and retried.
func isAttemptCostScalarKey(key string) bool {
	return key == "autonomous.first_attempt_escalate_after_failures" ||
		key == "autonomous.retry_resume_max_context_tokens"
}

// formatLivenessOverrides renders the overrides map for `config show`:
// "(none)" when empty, else sorted backend=seconds pairs.
func formatLivenessOverrides(m map[string]int) string {
	if len(m) == 0 {
		return "(none)"
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%d", k, m[k]))
	}
	return strings.Join(parts, ",")
}

// isMergePipelineReleaseKey reports whether key is one of the four release
// settings of the merge pipeline block, global or for one project. A refusal of
// a valid JSON value for one of them keeps the daemon's message, as the
// liveness keys do, rather than being replaced by a quoted retry.
func isMergePipelineReleaseKey(key string) bool {
	if !strings.HasPrefix(key, "autonomous.merge_pipeline.") {
		return false
	}
	for _, f := range []string{"release_size_map", "major_trigger", "release_cadence", "version_scheme"} {
		if strings.HasSuffix(key, "."+f) {
			return true
		}
	}
	return false
}
