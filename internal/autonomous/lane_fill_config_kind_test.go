package autonomous

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

func laneFillKindTestYAML() config.AutonomousLaneFillConfig {
	zero, no := 0, false
	return config.AutonomousLaneFillConfig{
		Mode:                     "shadow",
		GPULanes:                 2,
		ClaudeLanes:              3,
		StaleAfterHours:          &zero,
		RequireConfirmedProposal: &no,
		CheckMainCarries:         &no,
		RequireWorktree:          &no,
		LaneCountsFromCaps:       &no,
		GPUMode:                  "on",
		ClaudeMode:               "off",
	}
}

func TestLaneFillConfigKindModeBridgeRoundTrip(t *testing.T) {
	in := laneFillKindTestYAML()
	bridged := LaneFillConfigFromYAML(in)
	if bridged.GPUMode != "on" || bridged.ClaudeMode != "off" {
		t.Fatalf("per-kind modes lost: %q/%q", bridged.GPUMode, bridged.ClaudeMode)
	}
	if got := bridged.yaml(); !reflect.DeepEqual(got, in) {
		t.Fatalf("bridge round trip changed the block:\n in=%+v\nout=%+v", in, got)
	}
}

func TestLaneFillConfigKindModeEffectiveModeFor(t *testing.T) {
	c := LaneFillConfig{Mode: "shadow", GPUMode: "on"}
	if got := c.EffectiveModeFor(LaneTypeGPU); got != "on" {
		t.Errorf("gpu = %q, want on", got)
	}
	if got := c.EffectiveModeFor(LaneTypeClaude); got != "shadow" {
		t.Errorf("claude = %q, want shadow (inherited)", got)
	}
	d := DefaultLaneFillConfig()
	if d.EffectiveModeFor(LaneTypeGPU) != "shadow" || d.EffectiveModeFor(LaneTypeClaude) != "shadow" {
		t.Errorf("shipped default must be shadow for both kinds, got %q/%q",
			d.EffectiveModeFor(LaneTypeGPU), d.EffectiveModeFor(LaneTypeClaude))
	}
}

func TestLaneFillConfigKindModeDefaultsOfNewKeys(t *testing.T) {
	d := DefaultLaneFillConfig()
	if d.EffectiveStaleAfterHours() != 72 || !d.EffectiveRequireConfirmedProposal() ||
		!d.EffectiveCheckMainCarries() || !d.EffectiveRequireWorktree() || !d.EffectiveLaneCountsFromCaps() {
		t.Fatalf("new keys must default to 72/true/true/true/true: %+v", d)
	}
}

func TestLaneFillConfigKindModeExplicitValuesSurvive(t *testing.T) {
	bridged := LaneFillConfigFromYAML(laneFillKindTestYAML())
	if bridged.EffectiveStaleAfterHours() != 0 {
		t.Errorf("explicit stale 0 must read 0, got %d", bridged.EffectiveStaleAfterHours())
	}
	if bridged.EffectiveRequireConfirmedProposal() || bridged.EffectiveCheckMainCarries() ||
		bridged.EffectiveRequireWorktree() || bridged.EffectiveLaneCountsFromCaps() {
		t.Error("explicit false switches must read false")
	}
	raw, err := json.Marshal(bridged)
	if err != nil {
		t.Fatal(err)
	}
	var back LaneFillConfig
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(back, bridged) {
		t.Fatalf("json round trip changed the manager block:\n in=%+v\nout=%+v", bridged, back)
	}
}
