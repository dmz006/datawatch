package config

import (
	"encoding/json"
	"os"
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
// clear it), and a fresh DefaultConfig() must start with the
// five-kind baseline (non-nil), which is the global-tier value the
// resolver falls back to.
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
	raw, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read saved yaml: %v", err)
	}
	if !strings.Contains(string(raw), "default_events") {
		t.Fatalf("explicit empty default_events elided from YAML — omitempty in effect")
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

	// A fresh default config starts with the five-kind baseline (the
	// built-in default notify set), non-nil: the global resolution tier
	// resolves to these unless the operator overrides the list.
	fresh := DefaultConfig()
	if fresh.Autonomous.Notify.DefaultEvents == nil {
		t.Fatalf("fresh DefaultConfig default_events is nil, want the five-kind baseline")
	}
	wantFresh := []string{"prd.needs_review", "prd.blocked", "prd.failed", "prd.completed", "prd.cancelled"}
	if got := fresh.Autonomous.Notify.DefaultEvents; !slices.Equal(got, wantFresh) {
		t.Fatalf("fresh DefaultConfig default_events = %v, want baseline %v", got, wantFresh)
	}
}

// TestAutonomousNotifyDefaultEventsYAML — Load semantics for
// autonomous.notify.default_events: a YAML file that omits the key keeps
// the five-kind baseline (config.Load merges over DefaultConfig()), an
// explicit `default_events: []` is the operator opt-out and loads as a
// non-nil empty list, and an explicit list loads verbatim.
func TestAutonomousNotifyDefaultEventsYAML(t *testing.T) {
	write := func(t *testing.T, body string) string {
		t.Helper()
		p := filepath.Join(t.TempDir(), "config.yaml")
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatalf("write yaml: %v", err)
		}
		return p
	}

	// Omitted key → the DefaultConfig() baseline survives the merge.
	cfg, err := Load(write(t, "autonomous:\n  enabled: false\n"))
	if err != nil {
		t.Fatalf("Load omitted-key config: %v", err)
	}
	if cfg.Autonomous.Notify.DefaultEvents == nil {
		t.Fatalf("omitted default_events loaded as nil, want the baseline list")
	}
	wantBaseline := []string{"prd.needs_review", "prd.blocked", "prd.failed", "prd.completed", "prd.cancelled"}
	if got := cfg.Autonomous.Notify.DefaultEvents; !slices.Equal(got, wantBaseline) {
		t.Fatalf("omitted default_events = %v, want baseline %v", got, wantBaseline)
	}

	// Explicit empty list → the operator opt-out: empty and non-nil.
	cfg, err = Load(write(t, "autonomous:\n  notify:\n    default_events: []\n"))
	if err != nil {
		t.Fatalf("Load explicit-empty config: %v", err)
	}
	if cfg.Autonomous.Notify.DefaultEvents == nil {
		t.Fatalf("explicit empty default_events loaded as nil, want non-nil empty")
	}
	if got := len(cfg.Autonomous.Notify.DefaultEvents); got != 0 {
		t.Fatalf("explicit empty default_events = %v, want empty", cfg.Autonomous.Notify.DefaultEvents)
	}

	// Explicit list → exactly that list.
	cfg, err = Load(write(t, "autonomous:\n  notify:\n    default_events:\n      - prd.failed\n"))
	if err != nil {
		t.Fatalf("Load explicit-list config: %v", err)
	}
	if got := cfg.Autonomous.Notify.DefaultEvents; !slices.Equal(got, []string{"prd.failed"}) {
		t.Fatalf("explicit list default_events = %v, want [prd.failed]", got)
	}
}
