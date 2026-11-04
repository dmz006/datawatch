package stats

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestSafeDeleteStatsJSONNames checks the SystemStats wire names and
// values for the safe-to-delete report fields.
func TestSafeDeleteStatsJSONNames(t *testing.T) {
	s := SystemStats{
		SafeDeleteLastMS:          42,
		SafeDeleteSlowReports:     3,
		SafeDeleteRefreshFailures: 1,
	}
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for key, want := range map[string]float64{
		"safe_delete_last_ms":          42,
		"safe_delete_slow_reports":     3,
		"safe_delete_refresh_failures": 1,
	} {
		got, ok := m[key]
		if !ok {
			t.Errorf("missing JSON key %q in %s", key, b)
			continue
		}
		if v, ok := got.(float64); !ok || v != want {
			t.Errorf("JSON %s = %v, want %v", key, got, want)
		}
	}
}

// TestSafeDeleteStatsOmittedAtZero checks that all three fields carry
// omitempty and disappear from the JSON when left at their zero values.
func TestSafeDeleteStatsOmittedAtZero(t *testing.T) {
	b, err := json.Marshal(SystemStats{})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, key := range []string{
		"safe_delete_last_ms",
		"safe_delete_slow_reports",
		"safe_delete_refresh_failures",
	} {
		if strings.Contains(string(b), `"`+key+`"`) {
			t.Errorf("zero-value SystemStats JSON contains %q; field must be omitempty: %s", key, b)
		}
	}
}
