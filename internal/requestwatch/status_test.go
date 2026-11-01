package requestwatch

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

func sampleObservations() []Observation {
	return []Observation{
		{SessionID: "s1", AutomatonID: "a1", TaskID: "t1", Role: RoleWorker, State: StateGenerating},
		{SessionID: "s2", AutomatonID: "a1", TaskID: "t2", Role: RoleVerifier, State: StateIdle},
		{SessionID: "s3", AutomatonID: "a2", TaskID: "t3", Role: RoleWorker, State: StateUnreachable},
	}
}

func TestBuildStatusDefaults(t *testing.T) {
	st := BuildStatus(config.RequestWatchConfig{}, sampleObservations(), Filter{})
	if !st.Enabled || st.PollSeconds != 30 || st.NoProgressMinutes != 30 {
		t.Fatalf("defaults wrong: %+v", st)
	}
	if len(st.Observations) != 3 {
		t.Fatalf("want 3 observations, got %d", len(st.Observations))
	}
}

func TestBuildStatusConfiguredValues(t *testing.T) {
	cfg := config.RequestWatchConfig{PollSeconds: 5, NoProgressMinutes: 12}
	st := BuildStatus(cfg, nil, Filter{})
	if st.PollSeconds != 5 || st.NoProgressMinutes != 12 {
		t.Fatalf("configured values not reported: %+v", st)
	}
}

func TestBuildStatusFilters(t *testing.T) {
	obs := sampleObservations()
	cases := []struct {
		name string
		f    Filter
		want []string
	}{
		{"session", Filter{Session: "s2"}, []string{"s2"}},
		{"automaton", Filter{Automaton: "a1"}, []string{"s1", "s2"}},
		{"both match", Filter{Session: "s1", Automaton: "a1"}, []string{"s1"}},
		{"both disagree", Filter{Session: "s1", Automaton: "a2"}, nil},
		{"no match session", Filter{Session: "nope"}, nil},
		{"no match automaton", Filter{Automaton: "nope"}, nil},
	}
	for _, c := range cases {
		st := BuildStatus(config.RequestWatchConfig{}, obs, c.f)
		if st.Observations == nil {
			t.Errorf("%s: list is nil", c.name)
		}
		var got []string
		for _, o := range st.Observations {
			got = append(got, o.SessionID)
		}
		if strings.Join(got, ",") != strings.Join(c.want, ",") {
			t.Errorf("%s: got %v want %v", c.name, got, c.want)
		}
	}
}

func TestBuildStatusDisabledIsEmpty(t *testing.T) {
	off := false
	st := BuildStatus(config.RequestWatchConfig{Enabled: &off}, sampleObservations(), Filter{})
	if st.Enabled {
		t.Fatal("disabled feature must report enabled false")
	}
	if st.Observations == nil || len(st.Observations) != 0 {
		t.Fatalf("disabled feature must give an empty list, got %v", st.Observations)
	}
}

func TestBuildStatusListNeverNull(t *testing.T) {
	for _, obs := range [][]Observation{nil, {}} {
		st := BuildStatus(config.RequestWatchConfig{}, obs, Filter{})
		b, err := json.Marshal(st)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(b), `"observations":[]`) {
			t.Fatalf("observations must marshal as an empty list: %s", b)
		}
	}
}

func TestBuildStatusDoesNotModifyInput(t *testing.T) {
	obs := sampleObservations()
	_ = BuildStatus(config.RequestWatchConfig{}, obs, Filter{Session: "s3"})
	if len(obs) != 3 || obs[0].SessionID != "s1" {
		t.Fatalf("input modified: %+v", obs)
	}
}

func TestStatusUnsetTimesAreOmitted(t *testing.T) {
	b, err := json.Marshal(NewTracker(Identity{SessionID: "s1"}).Observation())
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"last_change", "last_poll", "error"} {
		if _, ok := m[k]; ok {
			t.Fatalf("%s present for an unsampled observation: %s", k, b)
		}
	}
}
