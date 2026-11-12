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

func TestArchiveCancelledAfterMinutesDefault(t *testing.T) {
	s := DefaultSettings()
	if s.ArchiveCancelledAfterMinutes != 60 {
		t.Fatalf("default = %d, want 60", s.ArchiveCancelledAfterMinutes)
	}
	if got := s.Flat()[KeyArchiveCancelledAfterMinutes]; got != 60 {
		t.Errorf("Flat default = %v, want 60", got)
	}
	if !IsKey(KeyArchiveCancelledAfterMinutes) || KeyArchiveCancelledAfterMinutes != "supervisor_loop.archive_cancelled_after_minutes" {
		t.Errorf("key not registered: %q", KeyArchiveCancelledAfterMinutes)
	}
	found := false
	for _, k := range Keys() {
		if k == KeyArchiveCancelledAfterMinutes {
			found = true
		}
	}
	if !found {
		t.Error("Keys() omits the key")
	}
}

func TestArchiveCancelledAfterMinutesSetFlatRoundTrip(t *testing.T) {
	s := DefaultSettings()
	if err := s.Set(KeyArchiveCancelledAfterMinutes, 15); err != nil {
		t.Fatal(err)
	}
	if s.ArchiveCancelledAfterMinutes != 15 {
		t.Fatalf("field = %d, want 15", s.ArchiveCancelledAfterMinutes)
	}
	flat := s.Flat()
	if flat[KeyArchiveCancelledAfterMinutes] != 15 {
		t.Fatalf("Flat = %v, want 15", flat[KeyArchiveCancelledAfterMinutes])
	}
	back, reset := ParseSettings(flat)
	if len(reset) != 0 || back.ArchiveCancelledAfterMinutes != 15 {
		t.Fatalf("round trip: %d reset=%v", back.ArchiveCancelledAfterMinutes, reset)
	}
}

func TestArchiveCancelledAfterMinutesNegativeRefused(t *testing.T) {
	s := DefaultSettings()
	s.ArchiveCancelledAfterMinutes = -1
	if err := s.Validate(); err == nil || !strings.Contains(err.Error(), KeyArchiveCancelledAfterMinutes) {
		t.Errorf("Validate: %v", err)
	}
	s = DefaultSettings()
	err := s.Set(KeyArchiveCancelledAfterMinutes, -5)
	if err == nil || !strings.Contains(err.Error(), KeyArchiveCancelledAfterMinutes) {
		t.Errorf("Set: %v", err)
	}
	if s.ArchiveCancelledAfterMinutes != 60 {
		t.Errorf("refused Set mutated the value to %d", s.ArchiveCancelledAfterMinutes)
	}
	if err := s.Set(KeyArchiveCancelledAfterMinutes, "soon"); err == nil {
		t.Error("non-integer accepted")
	}
}

func TestArchiveCancelledAfterMinutesZeroAccepted(t *testing.T) {
	s := DefaultSettings()
	if err := s.Set(KeyArchiveCancelledAfterMinutes, 0); err != nil {
		t.Fatalf("Set 0: %v", err)
	}
	if s.ArchiveCancelledAfterMinutes != 0 {
		t.Errorf("field = %d, want 0", s.ArchiveCancelledAfterMinutes)
	}
	if err := s.Validate(); err != nil {
		t.Errorf("Validate 0: %v", err)
	}
}
