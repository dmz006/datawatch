package config

import (
	"encoding/json"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// TestAutonomousNotifyConfigRoundTrip — B124 no-omitempty guarantee for
// autonomous.notify.default_events (see AutonomousNotifyConfig in
// config.go): a non-empty event list must survive a YAML and JSON
// round-trip unchanged, an EXPLICIT empty list must round-trip as a
// non-nil empty slice (no-omitempty: clearing the key must really
// clear it), and a fresh DefaultConfig() must start with an empty
// (not nil-misread) event set, meaning the built-in lifecycle sinks
// subscribe to nothing by default.
func TestAutonomousNotifyConfigRoundTrip(t *testing.T) {
	want := []string{"prd.updated", "prd.completed"}

	// YAML round-trip with a non-empty event set.
	cfg := DefaultConfig()
	cfg.Autonomous.Notify.DefaultEvents = slices.Clone(want)
	p := filepath.Join(t.TempDir(), "config.yaml")
	if err := Save(cfg, p); err != nil {
		t.Fatalf("Save: %v", err)
	}
	again, err := Load(p)
	if err != nil {
		t.Fatalf("Load after Save: %v", err)
	}
	if got := again.Autonomous.Notify.DefaultEvents; !slices.Equal(got, want) {
		t.Fatalf("after YAML round trip default_events = %v, want %v", got, want)
	}

	// JSON round-trip with a non-empty event set.
	b, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	var viaJSON Config
	if err := json.Unmarshal(b, &viaJSON); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if got := viaJSON.Autonomous.Notify.DefaultEvents; !slices.Equal(got, want) {
		t.Fatalf("after JSON round trip default_events = %v, want %v", got, want)
	}

	// Explicit empty list: no-omitempty means the key must be written
	// (not elided on marshal) and survive the round-trip as an empty
	// slice in both encodings.
	cfg.Autonomous.Notify.DefaultEvents = []string{}
	if err := Save(cfg, p); err != nil {
		t.Fatalf("Save explicit empty: %v", err)
	}
	again, err = Load(p)
	if err != nil {
		t.Fatalf("Load explicit empty: %v", err)
	}
	if again.Autonomous.Notify.DefaultEvents == nil {
		t.Fatalf("after YAML round trip default_events is nil, want non-nil empty slice (no-omitempty)")
	}
	if len(again.Autonomous.Notify.DefaultEvents) != 0 {
		t.Fatalf("after YAML round trip default_events = %v, want empty", again.Autonomous.Notify.DefaultEvents)
	}

	b, err = json.Marshal(cfg)
	if err != nil {
		t.Fatalf("json.Marshal explicit empty: %v", err)
	}
	if !strings.Contains(string(b), `"default_events"`) {
		t.Fatalf("explicit empty default_events elided from JSON marshal output — omitempty in effect")
	}
	var viaJSONEmpty Config
	if err := json.Unmarshal(b, &viaJSONEmpty); err != nil {
		t.Fatalf("json.Unmarshal explicit empty: %v", err)
	}
	if viaJSONEmpty.Autonomous.Notify.DefaultEvents == nil {
		t.Fatalf("after JSON round trip default_events is nil, want non-nil empty slice (no-omitempty)")
	}
	if len(viaJSONEmpty.Autonomous.Notify.DefaultEvents) != 0 {
		t.Fatalf("after JSON round trip default_events = %v, want empty", viaJSONEmpty.Autonomous.Notify.DefaultEvents)
	}

	// A fresh default config must have an empty event set (current
	// behavior: no built-in lifecycle comm messages by default).
	fresh := DefaultConfig()
	if got := len(fresh.Autonomous.Notify.DefaultEvents); got != 0 {
		t.Fatalf("fresh DefaultConfig default_events = %v (len %d), want empty", fresh.Autonomous.Notify.DefaultEvents, got)
	}
}
