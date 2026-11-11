package autonomous

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
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
		{"sorts", []string{"zeta", "alpha", "mid"}, []string{"alpha", "mid", "zeta"}},
		{"dedupes after trim", []string{"alpha", " alpha ", "beta", "alpha"}, []string{"alpha", "beta"}},
		{"drops blanks among names", []string{"", "beta", " ", "alpha"}, []string{"alpha", "beta"}},
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
	if !strings.Contains(string(raw), `"packs":["alpha","zeta"]`) {
		t.Fatalf("packs not persisted in JSON: %s", raw)
	}
	var back Automation
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !reflect.DeepEqual(back.Packs, []string{"alpha", "zeta"}) {
		t.Fatalf("round-trip Packs = %#v, want [alpha zeta]", back.Packs)
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
