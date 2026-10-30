package server

import (
	"encoding/json"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

// A PUT that clears a supervisor watch string key back to empty must reach the
// live watch: the pushed block carries the cleared keys explicitly.
func TestWithSupervisorWatchClearsCarriesClearedKeys(t *testing.T) {
	// the shape json.Marshal gives a block whose string keys are empty
	// (omitempty drops poll_seconds, the live merge cannot tell "" from absent)
	in := json.RawMessage(`{"idle_alert_minutes":5,"supervisor_watch":{"mode":"shadow"}}`)
	out := withSupervisorWatchClears(in, config.SupervisorWatchConfig{Mode: "shadow"})

	var auto struct {
		IdleAlertMinutes int                        `json:"idle_alert_minutes"`
		Block            map[string]json.RawMessage `json:"supervisor_watch"`
	}
	if err := json.Unmarshal(out, &auto); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if auto.IdleAlertMinutes != 5 {
		t.Errorf("sibling key lost: idle_alert_minutes = %d, want 5", auto.IdleAlertMinutes)
	}
	for _, key := range []string{"script_output_path", "tmux_restart_baseline", "main_checkout", "working_model", "poll_seconds"} {
		if _, ok := auto.Block[key]; !ok {
			t.Errorf("cleared key %q missing from the pushed block", key)
		}
	}
	var mode string
	_ = json.Unmarshal(auto.Block["mode"], &mode)
	if mode != "shadow" {
		t.Errorf("mode = %q, want shadow kept", mode)
	}
	var main string
	_ = json.Unmarshal(auto.Block["main_checkout"], &main)
	if main != "" {
		t.Errorf("main_checkout = %q, want the cleared empty value", main)
	}
}

func TestWithSupervisorWatchClearsKeepsSetValues(t *testing.T) {
	out := withSupervisorWatchClears(json.RawMessage(`{}`), config.SupervisorWatchConfig{MainCheckout: "repo", PollSeconds: 90})
	var auto struct {
		Block struct {
			MainCheckout string `json:"main_checkout"`
			PollSeconds  int    `json:"poll_seconds"`
		} `json:"supervisor_watch"`
	}
	if err := json.Unmarshal(out, &auto); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if auto.Block.MainCheckout != "repo" || auto.Block.PollSeconds != 90 {
		t.Errorf("block = %+v, want main_checkout repo and poll_seconds 90", auto.Block)
	}
}

func TestWithSupervisorWatchClearsLeavesUndecodableBlock(t *testing.T) {
	in := json.RawMessage(`not json`)
	if got := withSupervisorWatchClears(in, config.SupervisorWatchConfig{}); string(got) != string(in) {
		t.Errorf("undecodable input changed: %s", got)
	}
}

// poll_seconds 0 is unset on every write path: the REST patch accepts it,
// a value below the minimum and a negative one are refused.
func TestValidateSupervisorWatchPatchPollSecondsZeroIsUnset(t *testing.T) {
	key := supervisorWatchKeyPrefix + "poll_seconds"
	if err := validateSupervisorWatchPatch(map[string]interface{}{key: float64(0)}); err != nil {
		t.Errorf("poll_seconds 0 refused: %v", err)
	}
	for _, bad := range []float64{-1, 1, 59} {
		if err := validateSupervisorWatchPatch(map[string]interface{}{key: bad}); err == nil {
			t.Errorf("poll_seconds %v accepted, want refused", bad)
		}
	}
	if err := validateSupervisorWatchPatch(map[string]interface{}{key: float64(60)}); err != nil {
		t.Errorf("poll_seconds 60 refused: %v", err)
	}
}

func TestApplySupervisorWatchValuePollSecondsZeroClearsToUnset(t *testing.T) {
	swc := config.SupervisorWatchConfig{PollSeconds: 120}
	applySupervisorWatchValue(&swc, "poll_seconds", float64(0))
	if swc.PollSeconds != 0 {
		t.Fatalf("PollSeconds = %d, want 0 (unset)", swc.PollSeconds)
	}
	if got := swc.EffectivePollSeconds(); got != config.DefaultSupervisorWatchPollSeconds {
		t.Errorf("EffectivePollSeconds = %d, want default %d", got, config.DefaultSupervisorWatchPollSeconds)
	}
}
