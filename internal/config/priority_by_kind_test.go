package config

import (
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestPriorityByKindDefaults(t *testing.T) {
	want := map[string]int{"bug": 80, "maintenance": 70, "feature": 20, "other": 0}
	cases := []struct {
		name string
		got  map[string]int
	}{
		{"DefaultConfig", DefaultConfig().Autonomous.PriorityByKind},
		{"Effective(empty)", (AutonomousConfig{}).EffectivePriorityByKind()},
		{"DefaultPriorityByKindMap", DefaultPriorityByKindMap()},
	}
	for _, tc := range cases {
		for k, v := range want {
			if tc.got[k] != v {
				t.Fatalf("%s[%q] = %d, want %d (map=%v)", tc.name, k, tc.got[k], v, tc.got)
			}
		}
		if len(tc.got) != len(want) {
			t.Fatalf("%s = %v, want exactly %d entries %v", tc.name, tc.got, len(want), want)
		}
	}
}

func TestPriorityByKindEffectiveOverrides(t *testing.T) {
	cases := []struct {
		name string
		set  map[string]int
		want map[string]int
	}{
		{
			name: "empty map keeps every default",
			set:  map[string]int{},
			want: map[string]int{"bug": 80, "maintenance": 70, "feature": 20, "other": 0},
		},
		{
			name: "operator value overrides the default",
			set:  map[string]int{"bug": 50},
			want: map[string]int{"bug": 50, "maintenance": 70, "feature": 20, "other": 0},
		},
		{
			name: "operator zero clears a kind",
			set:  map[string]int{"feature": 0},
			want: map[string]int{"bug": 80, "maintenance": 70, "feature": 0, "other": 0},
		},
		{
			name: "unknown key is kept and defaults stay",
			set:  map[string]int{"epic": 60},
			want: map[string]int{"bug": 80, "maintenance": 70, "feature": 20, "other": 0, "epic": 60},
		},
		{
			name: "all kinds plus an unknown key",
			set:  map[string]int{"bug": 1, "maintenance": 2, "feature": 3, "other": 4, "security": 90},
			want: map[string]int{"bug": 1, "maintenance": 2, "feature": 3, "other": 4, "security": 90},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := (AutonomousConfig{PriorityByKind: tc.set}).EffectivePriorityByKind()
			for k, v := range tc.want {
				if got[k] != v {
					t.Fatalf("effective[%q] = %d, want %d (map=%v)", k, got[k], v, got)
				}
			}
			if len(got) != len(tc.want) {
				t.Fatalf("effective = %v, want exactly %d entries %v", got, len(tc.want), tc.want)
			}
		})
	}
}

func TestPriorityByKindDoesNotMutateShippedTable(t *testing.T) {
	other := AutonomousConfig{PriorityByKind: map[string]int{"bug": 5, "epic": 60}}
	_ = other.EffectivePriorityByKind()
	shipped := DefaultPriorityByKindMap()
	if shipped["bug"] != DefaultPriorityByKindBug || shipped["epic"] != 0 {
		t.Fatalf("shipped table mutated: %v", shipped)
	}
	if got := DefaultConfig().Autonomous.PriorityByKind["bug"]; got != DefaultPriorityByKindBug {
		t.Fatalf("DefaultConfig table mutated: %d, want %d", got, DefaultPriorityByKindBug)
	}
	// the helper must not write the operator's entries back onto the
	// receiver's own map
	if other.PriorityByKind["feature"] != 0 {
		t.Fatalf("receiver map mutated: %v", other.PriorityByKind)
	}
}

func TestPriorityByKindAndDefectIntakeYAMLRoundTrip(t *testing.T) {
	in := AutonomousConfig{
		PriorityByKind:         map[string]int{"bug": 50, "other": 0, "epic": 60},
		DefectIntakeAutomation: "defect-taker",
	}
	raw, err := yaml.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var out AutonomousConfig
	if err := yaml.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	if out.PriorityByKind["bug"] != 50 || out.PriorityByKind["other"] != 0 || out.PriorityByKind["epic"] != 60 {
		t.Fatalf("round trip map = %v", out.PriorityByKind)
	}
	if out.DefectIntakeAutomation != "defect-taker" {
		t.Fatalf("round trip defect_intake_automation = %q", out.DefectIntakeAutomation)
	}
}

func TestPriorityByKindAndDefectIntakeLoad(t *testing.T) {
	cases := []struct {
		name string
		body string
		kind map[string]int
		auto string
	}{
		{
			name: "defaults when unset",
			body: "autonomous:\n  lane_fill:\n    mode: shadow\n",
			kind: map[string]int{"bug": 80, "maintenance": 70, "feature": 20, "other": 0},
			auto: "",
		},
		{
			name: "operator map plus named automaton",
			body: "autonomous:\n  priority_by_kind:\n    bug: 50\n    epic: 60\n  defect_intake_automation: defect-taker\n",
			kind: map[string]int{"bug": 50, "epic": 60},
			auto: "defect-taker",
		},
		{
			name: "explicit empty string survives",
			body: "autonomous:\n  defect_intake_automation: \"\"\n",
			kind: map[string]int{"bug": 80, "maintenance": 70, "feature": 20, "other": 0},
			auto: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			if err := os.WriteFile(path, []byte(tc.body), 0o600); err != nil {
				t.Fatal(err)
			}
			cfg, err := Load(path)
			if err != nil {
				t.Fatal(err)
			}
			if tc.kind == nil {
				if len(cfg.Autonomous.PriorityByKind) != 0 {
					t.Fatalf("priority_by_kind = %v, want empty", cfg.Autonomous.PriorityByKind)
				}
			} else {
				for k, v := range tc.kind {
					if got := cfg.Autonomous.EffectivePriorityByKind()[k]; got != v {
						t.Fatalf("effective[%q] = %d, want %d (set=%v)", k, got, v, cfg.Autonomous.PriorityByKind)
					}
				}
			}
			if cfg.Autonomous.DefectIntakeAutomation != tc.auto {
				t.Fatalf("defect_intake_automation = %q, want %q", cfg.Autonomous.DefectIntakeAutomation, tc.auto)
			}
		})
	}
}
