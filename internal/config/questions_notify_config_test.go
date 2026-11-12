package config

import (
	"encoding/json"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// questionNotifyToggle names one of the three question notify toggles and
// how to get and set it on AutonomousQuestionsConfig.
type questionNotifyToggle struct {
	name   string
	set    func(*AutonomousQuestionsConfig, bool)
	get    func(AutonomousQuestionsConfig) bool
	active func(AutonomousQuestionsConfig) bool
}

var questionNotifyToggles = []questionNotifyToggle{
	{
		name:   "notify_messaging",
		set:    func(q *AutonomousQuestionsConfig, b bool) { q.NotifyMessaging = boolPtr(b) },
		get:    func(q AutonomousQuestionsConfig) bool { return q.NotifyMessaging != nil && *q.NotifyMessaging },
		active: func(q AutonomousQuestionsConfig) bool { return q.EffectiveNotifyMessaging() },
	},
	{
		name:   "notify_alert",
		set:    func(q *AutonomousQuestionsConfig, b bool) { q.NotifyAlert = boolPtr(b) },
		get:    func(q AutonomousQuestionsConfig) bool { return q.NotifyAlert != nil && *q.NotifyAlert },
		active: func(q AutonomousQuestionsConfig) bool { return q.EffectiveNotifyAlert() },
	},
	{
		name:   "return_path",
		set:    func(q *AutonomousQuestionsConfig, b bool) { q.ReturnPath = boolPtr(b) },
		get:    func(q AutonomousQuestionsConfig) bool { return q.ReturnPath != nil && *q.ReturnPath },
		active: func(q AutonomousQuestionsConfig) bool { return q.EffectiveReturnPath() },
	},
}

// The question notify toggles (autonomous.questions.notify_messaging,
// .notify_alert, .return_path) default ON when the key is absent: an
// operator is notified when a question is asked and answered, the alert
// fires only for high priority questions, and the return path permits the
// per-question session line. Because Go's zero value (false) cannot
// distinguish "absent" from "explicitly off", the fields are *bool and
// the Effective* helpers resolve nil to true.

// TestQuestionNotifyConfig_DefaultsOnWhenKeyAbsent pins the shipped
// state: all three toggles are absent (nil) in DefaultConfig and after
// loading an empty YAML file, and the Effective* helpers report on.
func TestQuestionNotifyConfig_DefaultsOnWhenKeyAbsent(t *testing.T) {
	q := DefaultConfig().Autonomous.Questions
	for _, tc := range questionNotifyToggles {
		if !tc.active(q) {
			t.Fatalf("DefaultConfig(): %s must default on", tc.name)
		}
	}
	if q.NotifyMessaging != nil || q.NotifyAlert != nil || q.ReturnPath != nil {
		t.Fatalf("DefaultConfig(): toggles must be absent (nil), got %+v", q)
	}

	var empty Config
	if err := yaml.Unmarshal(nil, &empty); err != nil {
		t.Fatalf("unmarshal empty YAML: %v", err)
	}
	q = empty.Autonomous.Questions
	if q.NotifyMessaging != nil || q.NotifyAlert != nil || q.ReturnPath != nil {
		t.Fatalf("empty YAML: toggles must be absent (nil), got %+v", q)
	}
	for _, tc := range questionNotifyToggles {
		if !tc.active(q) {
			t.Fatalf("empty YAML: %s must default on", tc.name)
		}
	}
}

// TestQuestionNotifyConfig_ExplicitFalseSurvivesRoundTrip: an explicit
// true AND an explicit false for each toggle must survive a YAML and a
// JSON round trip (none of the fields has omitempty) and must overwrite
// a value held before unmarshalling — an operator who switched a toggle
// off keeps it off after a reload.
func TestQuestionNotifyConfig_ExplicitFalseSurvivesRoundTrip(t *testing.T) {
	for _, tc := range questionNotifyToggles {
		t.Run(tc.name, func(t *testing.T) {
			for _, val := range []bool{true, false} {
				in := AutonomousQuestionsConfig{}
				tc.set(&in, val)

				y, err := yaml.Marshal(in)
				if err != nil {
					t.Fatalf("yaml marshal: %v", err)
				}
				yWant := tc.name + ": " + map[bool]string{true: "true", false: "false"}[val]
				if !strings.Contains(string(y), yWant) {
					t.Fatalf("YAML lacks %q (an explicit value was elided):\n%s", yWant, y)
				}
				out := AutonomousQuestionsConfig{}
				tc.set(&out, !val) // held value must be overwritten
				if err := yaml.Unmarshal(y, &out); err != nil {
					t.Fatalf("yaml unmarshal: %v", err)
				}
				if got := tc.get(out); got != val {
					t.Fatalf("after YAML round trip = %v, want %v", got, val)
				}
				if got := tc.active(out); got != val {
					t.Fatalf("after YAML round trip effective = %v, want %v", got, val)
				}

				j, err := json.Marshal(in)
				if err != nil {
					t.Fatalf("json marshal: %v", err)
				}
				jWant := `"` + tc.name + `":` + map[bool]string{true: "true", false: "false"}[val]
				if !strings.Contains(string(j), jWant) {
					t.Fatalf("JSON lacks %s (an explicit value was elided):\n%s", jWant, j)
				}
				out = AutonomousQuestionsConfig{}
				tc.set(&out, !val)
				if err := json.Unmarshal(j, &out); err != nil {
					t.Fatalf("json unmarshal: %v", err)
				}
				if got := tc.get(out); got != val {
					t.Fatalf("after JSON round trip = %v, want %v", got, val)
				}
				if got := tc.active(out); got != val {
					t.Fatalf("after JSON round trip effective = %v, want %v", got, val)
				}
			}
		})
	}
}

// TestQuestionNotifyConfig_EffectiveHelpersNilIsOn pins the pointer
// semantics directly: nil resolves on for each helper, an explicit false
// resolves off, an explicit true resolves on.
func TestQuestionNotifyConfig_EffectiveHelpersNilIsOn(t *testing.T) {
	cases := []struct {
		name string
		q    AutonomousQuestionsConfig
	}{
		{"absent", AutonomousQuestionsConfig{}},
		{"messaging-off", AutonomousQuestionsConfig{NotifyMessaging: boolPtr(false)}},
		{"alert-off", AutonomousQuestionsConfig{NotifyAlert: boolPtr(false)}},
		{"return-path-off", AutonomousQuestionsConfig{ReturnPath: boolPtr(false)}},
		{"all-on", AutonomousQuestionsConfig{
			NotifyMessaging: boolPtr(true),
			NotifyAlert:     boolPtr(true),
			ReturnPath:      boolPtr(true),
		}},
		{"all-off", AutonomousQuestionsConfig{
			NotifyMessaging: boolPtr(false),
			NotifyAlert:     boolPtr(false),
			ReturnPath:      boolPtr(false),
		}},
	}
	for _, tc := range cases {
		want := map[string]struct {
			m, a, r bool
		}{
			"absent":          {true, true, true},
			"messaging-off":   {false, true, true},
			"alert-off":       {true, false, true},
			"return-path-off": {true, true, false},
			"all-on":          {true, true, true},
			"all-off":         {false, false, false},
		}[tc.name]
		if got := tc.q.EffectiveNotifyMessaging(); got != want.m {
			t.Fatalf("%s: EffectiveNotifyMessaging = %v, want %v", tc.name, got, want.m)
		}
		if got := tc.q.EffectiveNotifyAlert(); got != want.a {
			t.Fatalf("%s: EffectiveNotifyAlert = %v, want %v", tc.name, got, want.a)
		}
		if got := tc.q.EffectiveReturnPath(); got != want.r {
			t.Fatalf("%s: EffectiveReturnPath = %v, want %v", tc.name, got, want.r)
		}
	}
}
