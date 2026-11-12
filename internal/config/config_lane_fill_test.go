package config

import (
	"encoding/json"
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

func TestLaneFillNewKeyDefaults(t *testing.T) {
	for name, lf := range map[string]AutonomousLaneFillConfig{
		"shipped defaults": DefaultConfig().Autonomous.LaneFill,
		"zero value":       {},
	} {
		if got := lf.EffectiveStaleAfterHours(); got != 72 {
			t.Errorf("%s: stale_after_hours = %d, want 72", name, got)
		}
		if !lf.EffectiveRequireConfirmedProposal() || !lf.EffectiveCheckMainCarries() ||
			!lf.EffectiveRequireWorktree() || !lf.EffectiveLaneCountsFromCaps() {
			t.Errorf("%s: the four switches must default to true", name)
		}
		if lf.GPUMode != "" || lf.ClaudeMode != "" {
			t.Errorf("%s: gpu_mode/claude_mode = %q/%q, want empty", name, lf.GPUMode, lf.ClaudeMode)
		}
	}
	d := DefaultConfig().Autonomous.LaneFill
	if d.EffectiveModeFor("gpu") != "shadow" || d.EffectiveModeFor("claude") != "shadow" {
		t.Fatalf("shipped default must stay shadow for both kinds, got %q/%q", d.EffectiveModeFor("gpu"), d.EffectiveModeFor("claude"))
	}
}

func TestLaneFillStaleAfterHoursZeroSurvivesRoundTrips(t *testing.T) {
	zero := 0
	in := AutonomousLaneFillConfig{Mode: "shadow", StaleAfterHours: &zero}
	raw, err := yaml.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "stale_after_hours: 0") {
		t.Fatalf("yaml must carry the explicit 0:\n%s", raw)
	}
	var fromYAML AutonomousLaneFillConfig
	if err := yaml.Unmarshal(raw, &fromYAML); err != nil {
		t.Fatal(err)
	}
	if fromYAML.StaleAfterHours == nil || fromYAML.EffectiveStaleAfterHours() != 0 {
		t.Fatalf("yaml round trip lost the explicit 0: %+v", fromYAML.StaleAfterHours)
	}
	jraw, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(jraw), `"stale_after_hours":0`) {
		t.Fatalf("json must carry the explicit 0: %s", jraw)
	}
	var fromJSON AutonomousLaneFillConfig
	if err := json.Unmarshal(jraw, &fromJSON); err != nil {
		t.Fatal(err)
	}
	if fromJSON.StaleAfterHours == nil || fromJSON.EffectiveStaleAfterHours() != 0 {
		t.Fatalf("json round trip lost the explicit 0: %+v", fromJSON.StaleAfterHours)
	}
}

func TestLaneFillStaleAfterHoursAbsentKeyReads72(t *testing.T) {
	var fromYAML, fromJSON AutonomousLaneFillConfig
	if err := yaml.Unmarshal([]byte("mode: shadow\n"), &fromYAML); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(`{"mode":"shadow"}`), &fromJSON); err != nil {
		t.Fatal(err)
	}
	if fromYAML.StaleAfterHours != nil || fromJSON.StaleAfterHours != nil {
		t.Fatal("an absent stale_after_hours must stay nil")
	}
	if fromYAML.EffectiveStaleAfterHours() != 72 || fromJSON.EffectiveStaleAfterHours() != 72 {
		t.Fatal("an absent stale_after_hours must read 72")
	}
	raw, _ := yaml.Marshal(fromYAML)
	for _, k := range []string{"stale_after_hours", "require_confirmed_proposal", "check_main_carries", "require_worktree", "lane_counts_from_caps"} {
		if strings.Contains(string(raw), k) {
			t.Fatalf("an unset %s must not be written to yaml:\n%s", k, raw)
		}
	}
}

func TestLaneFillSwitchesExplicitFalseSurvives(t *testing.T) {
	f := false
	in := AutonomousLaneFillConfig{
		RequireConfirmedProposal: &f, CheckMainCarries: &f, RequireWorktree: &f, LaneCountsFromCaps: &f,
	}
	raw, err := yaml.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var out AutonomousLaneFillConfig
	if err := yaml.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	if out.EffectiveRequireConfirmedProposal() || out.EffectiveCheckMainCarries() ||
		out.EffectiveRequireWorktree() || out.EffectiveLaneCountsFromCaps() {
		t.Fatalf("explicit false must survive the round trip:\n%s", raw)
	}
}

func TestLaneFillEffectiveModeForKind(t *testing.T) {
	c := AutonomousLaneFillConfig{Mode: "shadow", GPUMode: " ON "}
	if got := c.EffectiveModeFor("gpu"); got != "on" {
		t.Errorf("gpu mode = %q, want on", got)
	}
	if got := c.EffectiveModeFor("claude"); got != "shadow" {
		t.Errorf("claude mode = %q, want shadow (inherited)", got)
	}
	c = AutonomousLaneFillConfig{Mode: "on", ClaudeMode: "off"}
	if c.EffectiveModeFor("gpu") != "on" || c.EffectiveModeFor("claude") != "off" {
		t.Errorf("got gpu=%q claude=%q, want on/off", c.EffectiveModeFor("gpu"), c.EffectiveModeFor("claude"))
	}
	if got := (AutonomousLaneFillConfig{}).EffectiveModeFor("gpu"); got != "shadow" {
		t.Errorf("empty config gpu mode = %q, want shadow", got)
	}
}

func TestLaneFillKindModeValidation(t *testing.T) {
	for _, ok := range []string{"", "off", "shadow", "on", " ON "} {
		if err := ValidateLaneFillKindMode("gpu_mode", ok); err != nil {
			t.Errorf("ValidateLaneFillKindMode(%q) = %v, want nil", ok, err)
		}
	}
	for _, key := range []string{"gpu_mode", "claude_mode"} {
		err := ValidateLaneFillKindMode(key, "auto")
		if err == nil || !strings.Contains(err.Error(), "autonomous.lane_fill."+key) || !strings.Contains(err.Error(), `must be "off", "shadow" or "on"`) {
			t.Errorf("ValidateLaneFillKindMode(%s, auto) = %v, want an error naming the key", key, err)
		}
	}
}

func TestLaneFillLoadRejectsBadKindModeAndStale(t *testing.T) {
	for name, tc := range map[string]struct{ body, key string }{
		"gpu_mode":    {"gpu_mode: auto", "gpu_mode"},
		"claude_mode": {"claude_mode: yes", "claude_mode"},
		"stale":       {"stale_after_hours: -1", "stale_after_hours"},
		"host down":   {"host_down_after_minutes: -1", "host_down_after_minutes"},
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			if err := os.WriteFile(path, []byte("autonomous:\n  lane_fill:\n    "+tc.body+"\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := Load(path); err == nil || !strings.Contains(err.Error(), "autonomous.lane_fill."+tc.key) {
				t.Fatalf("Load error = %v, want one naming autonomous.lane_fill.%s", err, tc.key)
			}
		})
	}
}

func TestLaneFillHostDownAfterMinutesDefaultAndRoundTrip(t *testing.T) {
	for name, lf := range map[string]AutonomousLaneFillConfig{
		"shipped defaults": DefaultConfig().Autonomous.LaneFill,
		"zero value":       {},
	} {
		if got := lf.EffectiveHostDownAfterMinutes(); got != 10 {
			t.Errorf("%s: host_down_after_minutes = %d, want 10", name, got)
		}
	}
	zero := 0
	in := AutonomousLaneFillConfig{Mode: "shadow", HostDownAfterMinutes: &zero}
	raw, err := yaml.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "host_down_after_minutes: 0") {
		t.Fatalf("yaml must carry the explicit 0:\n%s", raw)
	}
	var fromYAML AutonomousLaneFillConfig
	if err := yaml.Unmarshal(raw, &fromYAML); err != nil {
		t.Fatal(err)
	}
	if fromYAML.HostDownAfterMinutes == nil || fromYAML.EffectiveHostDownAfterMinutes() != 0 {
		t.Fatalf("yaml round trip lost the explicit 0: %+v", fromYAML.HostDownAfterMinutes)
	}
	jraw, _ := json.Marshal(in)
	var fromJSON AutonomousLaneFillConfig
	if err := json.Unmarshal(jraw, &fromJSON); err != nil {
		t.Fatal(err)
	}
	if fromJSON.HostDownAfterMinutes == nil || fromJSON.EffectiveHostDownAfterMinutes() != 0 {
		t.Fatalf("json round trip lost the explicit 0: %s", jraw)
	}
	var absent AutonomousLaneFillConfig
	if err := yaml.Unmarshal([]byte("mode: shadow\n"), &absent); err != nil {
		t.Fatal(err)
	}
	if absent.HostDownAfterMinutes != nil || absent.EffectiveHostDownAfterMinutes() != 10 {
		t.Fatal("an absent host_down_after_minutes must stay nil and read 10")
	}
	if out, _ := yaml.Marshal(absent); strings.Contains(string(out), "host_down_after_minutes") {
		t.Fatalf("an unset host_down_after_minutes must not be written:\n%s", out)
	}
}
