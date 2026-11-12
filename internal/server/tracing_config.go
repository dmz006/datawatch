package server

import (
	"fmt"
	"strings"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/trace"
)

// tracingPatchKeys are the PUT /api/config keys under tracing.*.
func isTracingKey(k string) bool {
	return k == "tracing.mode" || k == "tracing.arg_cap_bytes" ||
		k == "tracing.retention_days" || k == "tracing.backends" ||
		strings.HasPrefix(k, "tracing.backends.")
}

func tracingInt(v interface{}) (int, error) {
	switch n := v.(type) {
	case float64:
		if n != float64(int(n)) {
			return 0, fmt.Errorf("must be a whole number")
		}
		return int(n), nil
	case int:
		return n, nil
	case int64:
		return int(n), nil
	}
	return 0, fmt.Errorf("must be a whole number")
}

func tracingBackends(v interface{}) (map[string]bool, error) {
	raw, ok := v.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("tracing.backends: must be an object of backend name to true/false")
	}
	out := make(map[string]bool, len(raw))
	for k, e := range raw {
		b, isBool := e.(bool)
		if !isBool {
			return nil, fmt.Errorf("tracing.backends.%s: must be true or false", k)
		}
		out[k] = b
	}
	return out, nil
}

// applyTracingKey applies one tracing.* key to t, returning an error for
// a bad value. Used both for PUT pre-validation (on a copy) and the real
// apply, so the two cannot drift.
func applyTracingKey(t *trace.Settings, k string, v interface{}) error {
	switch {
	case k == "tracing.mode":
		s, ok := v.(string)
		if !ok {
			return fmt.Errorf("tracing.mode: must be a string")
		}
		t.Mode = strings.ToLower(strings.TrimSpace(s))
	case k == "tracing.arg_cap_bytes":
		n, err := tracingInt(v)
		if err != nil {
			return fmt.Errorf("tracing.arg_cap_bytes: %v", err)
		}
		t.ArgCapBytes = n
	case k == "tracing.retention_days":
		n, err := tracingInt(v)
		if err != nil {
			return fmt.Errorf("tracing.retention_days: %v", err)
		}
		t.RetentionDays = n
	case k == "tracing.backends":
		m, err := tracingBackends(v)
		if err != nil {
			return err
		}
		t.Backends = m
	case strings.HasPrefix(k, "tracing.backends."):
		name := strings.TrimPrefix(k, "tracing.backends.")
		b, ok := v.(bool)
		if !ok {
			return fmt.Errorf("%s: must be true or false", k)
		}
		m := make(map[string]bool, len(t.Backends)+1)
		for bk, bv := range t.Backends {
			m[bk] = bv
		}
		m[name] = b
		t.Backends = m
	}
	return nil
}

// validateTracingPatch pre-validates every tracing.* key in a PUT
// /api/config patch against the current settings; a bad value rejects the
// whole PUT.
func validateTracingPatch(cur trace.Settings, patch map[string]interface{}) error {
	trial := cur
	touched := false
	for k, v := range patch {
		if !isTracingKey(k) {
			continue
		}
		touched = true
		if err := applyTracingKey(&trial, k, v); err != nil {
			return err
		}
	}
	if !touched {
		return nil
	}
	return config.ValidateTracing(trial)
}
