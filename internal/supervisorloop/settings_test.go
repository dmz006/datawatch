package supervisorloop

import (
	"strings"
	"testing"
)

func TestDefaults(t *testing.T) {
	s := DefaultSettings()
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	if !s.Enabled || s.ValidatorRetries != 2 || s.EscalateAfter != 2 || s.RunRetention != 500 {
		t.Fatalf("%+v", s)
	}
	if !s.Steps["lane_count"] || s.Jobs["pick_backend"].Style != StyleOneshot {
		t.Fatal("defaults")
	}
}

func TestBadSettingRefusedNamingKey(t *testing.T) {
	cases := map[string]any{
		KeyValidatorRetries:                     -1,
		KeyEscalateAfter:                        0,
		KeyRunRetention:                         0,
		KeyEnabled:                              "yes",
		"supervisor_loop.draft_spec.style":      "weird",
		"supervisor_loop.draft_spec.model":      5,
		"supervisor_loop.cleanup_sweep.enabled": 1,
		"supervisor_loop.nope":                  true,
		KeyRunRetention + "x":                   1,
	}
	for k, v := range cases {
		s := DefaultSettings()
		err := s.Set(k, v)
		if err == nil || !strings.Contains(err.Error(), k) {
			t.Errorf("%s: %v", k, err)
		}
		if s.RunRetention != 500 || s.Jobs["draft_spec"].Style != StyleOneshot {
			t.Errorf("%s mutated settings", k)
		}
	}
}

func TestSetGoodValues(t *testing.T) {
	s := DefaultSettings()
	for k, v := range map[string]any{
		KeyEnabled: false, KeyRunRetention: float64(7), KeyValidatorRetries: int64(1),
		"supervisor_loop.deploy_after_ship.enabled": false,
		"supervisor_loop.draft_spec.style":          "session",
		"supervisor_loop.draft_spec.backend":        "pi",
		"supervisor_loop.draft_spec.model":          "q",
	} {
		if err := s.Set(k, v); err != nil {
			t.Fatal(k, err)
		}
	}
	j := s.Jobs["draft_spec"]
	if s.Enabled || s.RunRetention != 7 || s.ValidatorRetries != 1 || s.Steps["deploy_after_ship"] || j.Style != StyleSession || j.Backend != "pi" || j.Model != "q" {
		t.Fatalf("%+v", s)
	}
}

func TestParseFallsBackToDefault(t *testing.T) {
	s, reset := ParseSettings(map[string]any{
		KeyRunRetention: "lots", KeyEscalateAfter: 4, "supervisor_loop.pick_backend.style": "bogus",
	})
	if s.RunRetention != 500 || s.EscalateAfter != 4 || s.Jobs["pick_backend"].Style != StyleOneshot {
		t.Fatalf("%+v", s)
	}
	if len(reset) != 2 {
		t.Fatal(reset)
	}
}

func TestSettingsLaneBounds(t *testing.T) {
	s := DefaultSettings()
	if s.LaneMax != 0 || s.LaneMin != 0 {
		t.Fatalf("default lane bounds = %d..%d, want unset", s.LaneMin, s.LaneMax)
	}
	if err := s.Set(KeyLaneMin, 1); err != nil {
		t.Fatal(err)
	}
	if err := s.Set(KeyLaneMax, 4); err != nil {
		t.Fatal(err)
	}
	if s.LaneMin != 1 || s.LaneMax != 4 {
		t.Errorf("bounds = %d..%d", s.LaneMin, s.LaneMax)
	}
	if err := s.Set(KeyLaneMax, 0); err != nil || s.LaneMax != 0 {
		t.Errorf("unset max: err=%v max=%d", err, s.LaneMax)
	}
	_ = s.Set(KeyLaneMax, 4)
	if err := s.Set(KeyLaneMin, 5); err == nil {
		t.Error("min above max accepted")
	}
	if err := s.Set(KeyLaneMax, 0.5); err == nil {
		t.Error("non-integer max accepted")
	}
}
