package config

import (
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestAttemptCapConfig_Defaults(t *testing.T) {
	var c AttemptCapConfig
	if !c.EffectiveEnabled() || c.EffectiveMaxTotal() != 6 || c.EffectiveFreeAttempts() != 3 {
		t.Fatalf("defaults = enabled %v max %d free %d, want true 6 3", c.EffectiveEnabled(), c.EffectiveMaxTotal(), c.EffectiveFreeAttempts())
	}
	if got := c.EffectiveBackoffMinutes(); !reflect.DeepEqual(got, []int{5, 15, 45}) {
		t.Fatalf("default back-off = %v, want [5 15 45]", got)
	}
}

func TestAttemptCapConfig_BackoffBefore(t *testing.T) {
	c := AttemptCapConfig{}
	for n, want := range map[int]int{1: 0, 2: 0, 3: 0, 4: 5, 5: 15, 6: 45, 7: 45, 20: 45} {
		if got := c.BackoffBefore(n); got != want {
			t.Errorf("BackoffBefore(%d) = %d, want %d", n, got, want)
		}
	}
	none := AttemptCapConfig{BackoffMinutes: []int{}}
	if got := none.BackoffBefore(5); got != 0 {
		t.Errorf("an empty list must mean no wait, got %d", got)
	}
	one := AttemptCapConfig{FreeAttempts: 1, BackoffMinutes: []int{2}}
	if one.BackoffBefore(1) != 0 || one.BackoffBefore(2) != 2 || one.BackoffBefore(9) != 2 {
		t.Errorf("free 1 with [2] must wait 2 from attempt 2 on")
	}
}

func TestAttemptCapConfig_Validate(t *testing.T) {
	cases := []struct {
		name string
		c    AttemptCapConfig
		want string
	}{
		{"unset is valid", AttemptCapConfig{}, ""},
		{"a cap of one", AttemptCapConfig{MaxTotal: 1}, ""},
		{"negative cap", AttemptCapConfig{MaxTotal: -1}, "autonomous.attempt_cap.max_total"},
		{"negative free attempts", AttemptCapConfig{FreeAttempts: -2}, "autonomous.attempt_cap.free_attempts"},
		{"negative delay", AttemptCapConfig{BackoffMinutes: []int{5, -1}}, "autonomous.attempt_cap.backoff_minutes[1]"},
	}
	for _, tc := range cases {
		err := tc.c.Validate()
		if tc.want == "" {
			if err != nil {
				t.Errorf("%s: %v", tc.name, err)
			}
			continue
		}
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want it to name %s", tc.name, err, tc.want)
		}
	}
}

func TestAttemptCapConfig_Clone(t *testing.T) {
	off := false
	c := AttemptCapConfig{Enabled: &off, MaxTotal: 4, BackoffMinutes: []int{1, 2}}
	d := c.Clone()
	d.BackoffMinutes[0] = 99
	*d.Enabled = true
	if c.BackoffMinutes[0] != 1 || *c.Enabled {
		t.Fatalf("the clone aliases the original: %+v", c)
	}
	if got := (AttemptCapConfig{}).Clone(); got.BackoffMinutes != nil {
		t.Fatalf("an unset list must stay nil, got %v", got.BackoffMinutes)
	}
}

func TestAttemptCapConfig_YAMLRoundTrip(t *testing.T) {
	off := false
	type wrap struct {
		AttemptCap AttemptCapConfig `yaml:"attempt_cap,omitempty"`
	}
	for _, c := range []AttemptCapConfig{
		{Enabled: &off, MaxTotal: 4, FreeAttempts: 2, BackoffMinutes: []int{1, 2, 3}},
		{BackoffMinutes: []int{}}, // an explicit empty list: no wait
		{},
	} {
		b, err := yaml.Marshal(wrap{AttemptCap: c})
		if err != nil {
			t.Fatalf("marshal %+v: %v", c, err)
		}
		var back wrap
		if err := yaml.Unmarshal(b, &back); err != nil {
			t.Fatalf("unmarshal %s: %v", b, err)
		}
		if (c.BackoffMinutes == nil) != (back.AttemptCap.BackoffMinutes == nil) {
			t.Fatalf("an unset and an empty list must stay apart: %+v -> %s -> %+v", c, b, back.AttemptCap)
		}
		if !reflect.DeepEqual(c.Clone(), back.AttemptCap.Clone()) {
			t.Fatalf("round trip %+v -> %s -> %+v", c, b, back.AttemptCap)
		}
	}
}

func TestProjectPolicy_AttemptCap(t *testing.T) {
	one, zero := 1, 0
	if err := ValidateProjectPolicy(&ProjectPolicy{AttemptCap: &one}, ""); err != nil {
		t.Fatalf("a project cap of 1: %v", err)
	}
	if err := ValidateProjectPolicy(&ProjectPolicy{AttemptCap: &zero}, ""); err == nil || !strings.Contains(err.Error(), "attempt_cap") {
		t.Fatalf("a project cap of 0 must be refused naming attempt_cap, got %v", err)
	}
	daemon := DaemonPolicySource{Autonomous: AutonomousConfig{AttemptCap: AttemptCapConfig{MaxTotal: 8}}}
	got := ResolveProjectPolicy(daemon, nil, AutomatonOverrides{})
	if got.AttemptCap.Value != 8 || got.AttemptCap.Source != PolicySourceDaemon {
		t.Fatalf("daemon tier = %+v, want 8 from the daemon", got.AttemptCap)
	}
	three := 3
	got = ResolveProjectPolicy(daemon, &ProjectPolicy{AttemptCap: &three}, AutomatonOverrides{})
	if got.AttemptCap.Value != 3 || got.AttemptCap.Source != PolicySourceProject {
		t.Fatalf("project tier = %+v, want 3 from the project", got.AttemptCap)
	}
}
