package supervisorloop

import (
	"errors"
	"strings"
	"testing"
	"time"
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

type fakeOverrides struct {
	byProject map[string]SupervisorOverrides
	err       error
}

func (f fakeOverrides) SupervisorOverrides(p string) (SupervisorOverrides, bool, error) {
	o, ok := f.byProject[p]
	return o, ok, f.err
}

func TestSupervisorDefaultsValid(t *testing.T) {
	if err := DefaultSupervisorSettings().Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestValidateSupervisorModel(t *testing.T) {
	cases := []struct {
		model string
		bad   bool
	}{
		{"claude-sonnet-5-5", false}, {"claude-haiku-5-5", false}, {"Sonnet", false},
		{"claude-opus-5-5", true}, {"OPUS", true}, {"", true}, {"gpt-x", true}, {"sonnet-opus", true},
	}
	for _, c := range cases {
		err := ValidateSupervisorModel(c.model)
		if (err != nil) != c.bad {
			t.Errorf("%q: err=%v, bad=%v", c.model, err, c.bad)
		}
	}
}

func TestSupervisorValidateTable(t *testing.T) {
	cases := []struct {
		name string
		mut  func(*SupervisorSettings)
		want string
	}{
		{"ok", func(*SupervisorSettings) {}, ""},
		{"cadence low", func(s *SupervisorSettings) { s.TickCadence = time.Second }, "tick_cadence"},
		{"cadence high", func(s *SupervisorSettings) { s.TickCadence = 48 * time.Hour }, "tick_cadence"},
		{"opus", func(s *SupervisorSettings) { s.Model = "claude-opus-5-5" }, "model"},
		{"ctx low", func(s *SupervisorSettings) { s.ContextLimit = 1 }, "context_limit"},
		{"ctx high", func(s *SupervisorSettings) { s.ContextLimit = 1 << 30 }, "context_limit"},
		{"age low", func(s *SupervisorSettings) { s.ContextAgeLimit = 0 }, "context_age_limit"},
		{"age high", func(s *SupervisorSettings) { s.ContextAgeLimit = 365 * 24 * time.Hour }, "context_age_limit"},
		{"empty finding", func(s *SupervisorSettings) { s.WakeFindings = []string{" "} }, "wake_findings"},
		{"too many", func(s *SupervisorSettings) { s.WakeFindings = make([]string, MaxWakeKinds+1) }, "wake_findings"},
	}
	for _, c := range cases {
		s := DefaultSupervisorSettings()
		c.mut(&s)
		err := s.Validate()
		if c.want == "" && err != nil {
			t.Errorf("%s: %v", c.name, err)
		}
		if c.want != "" && (err == nil || !strings.Contains(err.Error(), c.want)) {
			t.Errorf("%s: want error naming %q, got %v", c.name, c.want, err)
		}
	}
}

func TestLoadSupervisorSettings(t *testing.T) {
	cad := 5 * time.Minute
	haiku := "claude-haiku-5-5"
	opus := "claude-opus-5-5"
	limit := 50000
	age := 2 * time.Hour
	badCad := time.Second
	cases := []struct {
		name    string
		src     OverridesSource
		project string
		check   func(SupervisorSettings) bool
		wantErr bool
	}{
		{"nil source", nil, "p", func(s SupervisorSettings) bool { return s.Model == DefaultSupervisorModel }, false},
		{"unknown project", fakeOverrides{}, "p", func(s SupervisorSettings) bool { return s.TickCadence == DefaultSupervisorTickCadence }, false},
		{"full override", fakeOverrides{byProject: map[string]SupervisorOverrides{"p": {
			TickCadence: &cad, Model: &haiku, ContextLimit: &limit, ContextAgeLimit: &age, WakeFindings: []string{"task.failed"},
		}}}, "p", func(s SupervisorSettings) bool {
			return s.TickCadence == cad && s.Model == haiku && s.ContextLimit == limit && s.ContextAgeLimit == age &&
				len(s.WakeFindings) == 1 && s.WakeFindings[0] == "task.failed"
		}, false},
		{"partial override keeps defaults", fakeOverrides{byProject: map[string]SupervisorOverrides{"p": {Model: &haiku}}}, "p",
			func(s SupervisorSettings) bool {
				return s.Model == haiku && s.ContextLimit == DefaultSupervisorContextLimit && len(s.WakeFindings) == len(DefaultSupervisorWakeFindings())
			}, false},
		{"other project untouched", fakeOverrides{byProject: map[string]SupervisorOverrides{"q": {Model: &haiku}}}, "p",
			func(s SupervisorSettings) bool { return s.Model == DefaultSupervisorModel }, false},
		{"opus rejected", fakeOverrides{byProject: map[string]SupervisorOverrides{"p": {Model: &opus}}}, "p", nil, true},
		{"bad cadence rejected", fakeOverrides{byProject: map[string]SupervisorOverrides{"p": {TickCadence: &badCad}}}, "p", nil, true},
		{"source error", fakeOverrides{err: errors.New("boom")}, "p", nil, true},
	}
	for _, c := range cases {
		s, err := LoadSupervisorSettings(c.src, c.project)
		if (err != nil) != c.wantErr {
			t.Errorf("%s: err=%v", c.name, err)
			continue
		}
		if !c.wantErr && !c.check(s) {
			t.Errorf("%s: unexpected settings %+v", c.name, s)
		}
	}
}

func TestLoadSupervisorSettingsDoesNotAliasOverride(t *testing.T) {
	in := []string{"task.failed"}
	s, err := LoadSupervisorSettings(fakeOverrides{byProject: map[string]SupervisorOverrides{"p": {WakeFindings: in}}}, "p")
	if err != nil {
		t.Fatal(err)
	}
	in[0] = "changed"
	if s.WakeFindings[0] != "task.failed" {
		t.Fatal("settings alias the override slice")
	}
}
