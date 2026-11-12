package config

import (
	"encoding/json"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// autonomous.questions.worker_marker defaults OFF when the key is absent.
// The field is a *bool with no omitempty so an explicit false and an
// explicit true both survive a YAML and a JSON round trip.

func TestQuestionsWorkerMarkerDefaultOff(t *testing.T) {
	q := DefaultConfig().Autonomous.Questions
	if q.WorkerMarker != nil {
		t.Fatalf("DefaultConfig(): WorkerMarker must be absent (nil), got %v", *q.WorkerMarker)
	}
	if q.EffectiveWorkerMarker() {
		t.Fatal("DefaultConfig(): worker_marker must default off")
	}

	var empty Config
	if err := yaml.Unmarshal(nil, &empty); err != nil {
		t.Fatalf("unmarshal empty YAML: %v", err)
	}
	if empty.Autonomous.Questions.WorkerMarker != nil {
		t.Fatal("empty YAML: WorkerMarker must be absent (nil)")
	}
	if empty.Autonomous.Questions.EffectiveWorkerMarker() {
		t.Fatal("empty YAML: worker_marker must default off")
	}
}

func TestQuestionsWorkerMarkerEffectiveHelper(t *testing.T) {
	cases := []struct {
		name string
		q    AutonomousQuestionsConfig
		want bool
	}{
		{"nil is off", AutonomousQuestionsConfig{}, false},
		{"explicit false is off", AutonomousQuestionsConfig{WorkerMarker: boolPtr(false)}, false},
		{"explicit true is on", AutonomousQuestionsConfig{WorkerMarker: boolPtr(true)}, true},
	}
	for _, tc := range cases {
		if got := tc.q.EffectiveWorkerMarker(); got != tc.want {
			t.Errorf("%s: EffectiveWorkerMarker() = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestQuestionsWorkerMarkerYAMLRoundTripKeepsExplicitValues(t *testing.T) {
	for _, val := range []bool{true, false} {
		in := AutonomousQuestionsConfig{WorkerMarker: boolPtr(val)}
		want := map[bool]string{true: "true", false: "false"}[val]

		y, err := yaml.Marshal(in)
		if err != nil {
			t.Fatalf("yaml marshal: %v", err)
		}
		if !strings.Contains(string(y), "worker_marker: "+want) {
			t.Fatalf("YAML lacks worker_marker: %s (an explicit value was elided):\n%s", want, y)
		}
		out := AutonomousQuestionsConfig{WorkerMarker: boolPtr(!val)} // held value must be overwritten
		if err := yaml.Unmarshal(y, &out); err != nil {
			t.Fatalf("yaml unmarshal: %v", err)
		}
		if out.WorkerMarker == nil || *out.WorkerMarker != val {
			t.Fatalf("after YAML round trip = %v, want %v", out.WorkerMarker, val)
		}
		if out.EffectiveWorkerMarker() != val {
			t.Fatalf("after YAML round trip effective = %v, want %v", out.EffectiveWorkerMarker(), val)
		}
	}
}

func TestQuestionsWorkerMarkerJSONRoundTripKeepsExplicitValues(t *testing.T) {
	for _, val := range []bool{true, false} {
		in := AutonomousQuestionsConfig{WorkerMarker: boolPtr(val)}
		want := map[bool]string{true: "true", false: "false"}[val]

		j, err := json.Marshal(in)
		if err != nil {
			t.Fatalf("json marshal: %v", err)
		}
		if !strings.Contains(string(j), `"worker_marker":`+want) {
			t.Fatalf("JSON lacks worker_marker:%s (an explicit value was elided):\n%s", want, j)
		}
		out := AutonomousQuestionsConfig{WorkerMarker: boolPtr(!val)}
		if err := json.Unmarshal(j, &out); err != nil {
			t.Fatalf("json unmarshal: %v", err)
		}
		if out.WorkerMarker == nil || *out.WorkerMarker != val {
			t.Fatalf("after JSON round trip = %v, want %v", out.WorkerMarker, val)
		}
	}
}
