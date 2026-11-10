package packs

import (
	"reflect"
	"testing"
)

func TestActivePacks(t *testing.T) {
	auto := []string{"a1", "a2"}
	proj := []string{"p1"}
	glob := []string{"g1", "g2", "g3"}

	tests := []struct {
		name    string
		auto    []string
		proj    []string
		glob    []string
		enabled bool
		want    []string
	}{
		{"automaton wins", auto, proj, glob, true, []string{"a1", "a2"}},
		{"project wins when automaton empty", nil, proj, glob, true, []string{"p1"}},
		{"project wins when automaton is empty slice", []string{}, proj, glob, true, []string{"p1"}},
		{"global wins when others empty", nil, nil, glob, true, []string{"g1", "g2", "g3"}},
		{"builtin default when all empty", nil, nil, nil, true, []string{"builtin"}},
		{"builtin default when all empty slices", []string{}, []string{}, []string{}, true, []string{"builtin"}},
		{"disabled returns builtin even with lists", auto, proj, glob, false, []string{"builtin"}},
		{"disabled with no lists returns builtin", nil, nil, nil, false, []string{"builtin"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ActivePacks(tc.auto, tc.proj, tc.glob, tc.enabled)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("ActivePacks() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestActivePacks_ReturnsCopy(t *testing.T) {
	auto := []string{"a1"}
	got := ActivePacks(auto, nil, nil, true)
	got[0] = "mutated"
	if auto[0] != "a1" {
		t.Fatalf("ActivePacks must not alias the input slice; input became %v", auto)
	}

	d1 := ActivePacks(nil, nil, nil, true)
	d1[0] = "mutated"
	if d2 := ActivePacks(nil, nil, nil, true); d2[0] != "builtin" {
		t.Fatalf("default list was mutated across calls: %v", d2)
	}
}
