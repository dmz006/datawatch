package autonomous

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/dmz006/datawatch/internal/config"
)

func TestNormalizePacks(t *testing.T) {
	cases := []struct {
		name string
		in   []string
		want []string
	}{
		{"nil input", nil, nil},
		{"empty input", []string{}, nil},
		{"only blanks", []string{"", "   ", "\t"}, nil},
		{"trims whitespace", []string{"  alpha ", "beta"}, []string{"alpha", "beta"}},
		{"keeps configured order", []string{"zeta", "alpha", "mid"}, []string{"zeta", "alpha", "mid"}},
		{"dedupes after trim keeping first", []string{"beta", "alpha", " beta ", "alpha"}, []string{"beta", "alpha"}},
		{"drops blanks among names", []string{"", "beta", " ", "alpha"}, []string{"beta", "alpha"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := normalizePacks(tc.in)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("normalizePacks(%q) = %#v, want %#v", tc.in, got, tc.want)
			}
		})
	}
}

func TestNormalizePacksDoesNotMutateInput(t *testing.T) {
	in := []string{"zeta", " alpha "}
	before := append([]string(nil), in...)
	_ = normalizePacks(in)
	if !reflect.DeepEqual(in, before) {
		t.Fatalf("input mutated: got %#v, want %#v", in, before)
	}
}

func TestAutomationPacksPersistAsJSON(t *testing.T) {
	a := Automation{ID: "abcd1234", Name: "packs", Packs: normalizePacks([]string{"zeta", " alpha "})}
	raw, err := json.Marshal(a)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(raw), `"packs":["zeta","alpha"]`) {
		t.Fatalf("packs not persisted in JSON: %s", raw)
	}
	var back Automation
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !reflect.DeepEqual(back.Packs, []string{"zeta", "alpha"}) {
		t.Fatalf("round-trip Packs = %#v, want [zeta alpha]", back.Packs)
	}
}

func TestAutomationPacksOmittedWhenEmpty(t *testing.T) {
	raw, err := json.Marshal(Automation{ID: "abcd1234", Name: "none"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(raw), `"packs"`) {
		t.Fatalf("empty packs should be omitted, got %s", raw)
	}
}

func TestProjectPacksPersistAsYAMLAndJSON(t *testing.T) {
	in := config.ProjectConfigEntry{
		Dir:         "/work/p",
		Description: "keep me",
		Packs:       []string{"zeta", "alpha"},
	}
	raw, err := yaml.Marshal(in)
	if err != nil {
		t.Fatalf("yaml marshal: %v", err)
	}
	var back config.ProjectConfigEntry
	if err := yaml.Unmarshal(raw, &back); err != nil {
		t.Fatalf("yaml unmarshal: %v", err)
	}
	if !reflect.DeepEqual(back, in) {
		t.Fatalf("yaml round-trip = %#v, want %#v", back, in)
	}
	jraw, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("json marshal: %v", err)
	}
	if !strings.Contains(string(jraw), `"packs":["zeta","alpha"]`) {
		t.Fatalf("packs not in project JSON: %s", jraw)
	}
}

func TestProjectPacksOmittedWhenEmpty(t *testing.T) {
	raw, err := yaml.Marshal(config.ProjectConfigEntry{Dir: "/work/p"})
	if err != nil {
		t.Fatalf("yaml marshal: %v", err)
	}
	if strings.Contains(string(raw), "packs") {
		t.Fatalf("empty packs should be omitted, got %s", raw)
	}
}
