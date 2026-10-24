package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestLaneFillDefaults(t *testing.T) {
	lf := DefaultConfig().Autonomous.LaneFill
	if lf.EffectiveMode() != "shadow" || lf.Mode != "shadow" {
		t.Fatalf("mode = %q / %q, want shadow", lf.Mode, lf.EffectiveMode())
	}
	if lf.EffectiveGPULanes() != 2 || lf.EffectiveClaudeLanes() != 3 {
		t.Fatalf("lanes = %d/%d, want 2/3", lf.EffectiveGPULanes(), lf.EffectiveClaudeLanes())
	}
	if lf.EffectiveIdleAlertMinutes() != 30 || lf.EffectiveDecomposeAheadMin() != 2 || lf.EffectivePollSeconds() != 60 {
		t.Fatalf("got idle=%d ahead=%d poll=%d", lf.EffectiveIdleAlertMinutes(), lf.EffectiveDecomposeAheadMin(), lf.EffectivePollSeconds())
	}
	if lf.EffectiveGPUBackend() != "pi-prompt" || lf.EffectiveClaudeBackend() != "claude-code" || lf.GPUModel != "" {
		t.Fatalf("backends = %q %q model %q", lf.EffectiveGPUBackend(), lf.EffectiveClaudeBackend(), lf.GPUModel)
	}
}

func TestLaneFillEffectiveHelpers(t *testing.T) {
	var unset AutonomousLaneFillConfig
	if unset.EffectiveMode() != "shadow" {
		t.Fatalf("empty mode resolves to %q", unset.EffectiveMode())
	}
	if unset.EffectivePollSeconds() != 60 {
		t.Fatalf("zero poll resolves to %d", unset.EffectivePollSeconds())
	}
	if unset.EffectiveGPUBackend() != "pi-prompt" || unset.EffectiveClaudeBackend() != "claude-code" {
		t.Fatalf("empty backends resolve to %q %q", unset.EffectiveGPUBackend(), unset.EffectiveClaudeBackend())
	}
	if got := (AutonomousLaneFillConfig{PollSeconds: 2}).EffectivePollSeconds(); got != 5 {
		t.Fatalf("poll floor = %d, want 5", got)
	}
	if got := (AutonomousLaneFillConfig{Mode: " ON "}).EffectiveMode(); got != "on" {
		t.Fatalf("mode = %q, want on", got)
	}
	zero := AutonomousLaneFillConfig{}
	if zero.EffectiveClaudeLanes() != 0 || zero.EffectiveIdleAlertMinutes() != 0 {
		t.Fatalf("explicit zeros must stay zero: claude=%d idle=%d", zero.EffectiveClaudeLanes(), zero.EffectiveIdleAlertMinutes())
	}
}

func TestLaneFillYAMLRoundTripKeepsExplicitZeros(t *testing.T) {
	in := AutonomousLaneFillConfig{Mode: "on", GPULanes: 0, ClaudeLanes: 0, IdleAlertMinutes: 0, DecomposeAheadMin: 0, PollSeconds: 15, GPUBackend: "pi-prompt", GPUModel: "m", ClaudeBackend: "claude-code"}
	raw, err := yaml.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"gpu_lanes: 0", "claude_lanes: 0", "idle_alert_minutes: 0", "decompose_ahead_min: 0"} {
		if !strings.Contains(string(raw), k) {
			t.Fatalf("marshalled yaml lacks %q:\n%s", k, raw)
		}
	}
	var out AutonomousLaneFillConfig
	if err := yaml.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	if out != in {
		t.Fatalf("round trip: got %+v, want %+v", out, in)
	}

	path := filepath.Join(t.TempDir(), "config.yaml")
	body := "autonomous:\n  lane_fill:\n    claude_lanes: 0\n    gpu_lanes: 4\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	lf := cfg.Autonomous.LaneFill
	if lf.ClaudeLanes != 0 || lf.GPULanes != 4 || lf.PollSeconds != 60 || lf.EffectiveMode() != "shadow" {
		t.Fatalf("loaded lane_fill = %+v", lf)
	}
}

func TestLaneFillValidation(t *testing.T) {
	for _, ok := range []string{"", "off", "shadow", "on", " ON ", "Shadow"} {
		if err := ValidateLaneFillMode(ok); err != nil {
			t.Errorf("ValidateLaneFillMode(%q) = %v, want nil", ok, err)
		}
	}
	for _, bad := range []string{"auto", "enabled", "true", "warn"} {
		err := ValidateLaneFillMode(bad)
		if err == nil || !strings.Contains(err.Error(), "autonomous.lane_fill.mode") {
			t.Errorf("ValidateLaneFillMode(%q) = %v, want an error naming autonomous.lane_fill.mode", bad, err)
		}
	}
}

func TestLaneFillLoadRejectsBadValues(t *testing.T) {
	for name, body := range map[string]string{
		"mode":         "mode: auto",
		"gpu_lanes":    "gpu_lanes: -1",
		"claude_lanes": "claude_lanes: -2",
		"idle":         "idle_alert_minutes: -5",
		"ahead":        "decompose_ahead_min: -1",
		"poll":         "poll_seconds: -10",
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			if err := os.WriteFile(path, []byte("autonomous:\n  lane_fill:\n    "+body+"\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := Load(path); err == nil || !strings.Contains(err.Error(), "autonomous.lane_fill") {
				t.Fatalf("Load error = %v, want one naming autonomous.lane_fill", err)
			}
		})
	}
}
