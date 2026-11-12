package config

import (
	"encoding/json"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// TestQuestionsConfig_DisabledByDefault pins the shipped state: the
// question switch is off in DefaultConfig and stays off when an empty
// YAML file is loaded (no explicit value → disabled), so the REST
// endpoints stay closed until an operator enables the switch.
func TestQuestionsConfig_DisabledByDefault(t *testing.T) {
	if DefaultConfig().Autonomous.Questions.Enabled {
		t.Fatal("DefaultConfig(): autonomous.questions.enabled must be false")
	}
	var empty Config
	if err := yaml.Unmarshal(nil, &empty); err != nil {
		t.Fatalf("unmarshal empty YAML: %v", err)
	}
	if empty.Autonomous.Questions.Enabled {
		t.Fatal("empty YAML must leave autonomous.questions.enabled false")
	}
}

// TestQuestionsConfig_YAMLRoundTripKeepsBothValues: an explicit true AND
// an explicit false must survive a YAML and a JSON round trip (the field
// has no omitempty) and must overwrite a value held before unmarshalling.
func TestQuestionsConfig_YAMLRoundTripKeepsBothValues(t *testing.T) {
	for name, in := range map[string]AutonomousQuestionsConfig{
		"true":  {Enabled: true},
		"false": {Enabled: false},
	} {
		y, err := yaml.Marshal(in)
		if err != nil {
			t.Fatalf("%s: marshal: %v", name, err)
		}
		if want := "enabled: " + name; !strings.Contains(string(y), want) {
			t.Fatalf("%s: YAML lacks %q (an explicit value was elided):\n%s", name, want, y)
		}
		out := AutonomousQuestionsConfig{Enabled: !in.Enabled}
		if err := yaml.Unmarshal(y, &out); err != nil {
			t.Fatalf("%s: unmarshal: %v", name, err)
		}
		if out.Enabled != in.Enabled {
			t.Fatalf("%s: after YAML round trip = %+v", name, out)
		}

		j, err := json.Marshal(in)
		if err != nil {
			t.Fatalf("%s: json marshal: %v", name, err)
		}
		if want := `"enabled":` + name; !strings.Contains(string(j), want) {
			t.Fatalf("%s: JSON lacks %s (an explicit value was elided):\n%s", name, want, j)
		}
		out = AutonomousQuestionsConfig{Enabled: !in.Enabled}
		if err := json.Unmarshal(j, &out); err != nil {
			t.Fatalf("%s: json unmarshal: %v", name, err)
		}
		if out.Enabled != in.Enabled {
			t.Fatalf("%s: after JSON round trip = %+v", name, out)
		}
	}
}
