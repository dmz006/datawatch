package main

import (
	"reflect"
	"testing"
)

// fixture is a small local struct (not config.Config) exercising every
// walker rule: tag-name use, nesting, yaml:"-" skip, inline flattening,
// untagged-embedded flattening, pointer-to-struct recursion, maps/slices
// as leaves, interface as a leaf, and stable sorted de-duplicated output.
type inner struct {
	Alpha  string  `yaml:"alpha"`
	Beta   float64 `yaml:"beta,omitempty"`
	Hidden string  `yaml:"-"`
}

type embedded struct {
	Gamma  int    `yaml:"gamma"`
	Hidden string `yaml:"-"`
}

type fixture struct {
	embedded                // untagged embedded struct: flattened
	Name     string         `yaml:"name"`
	In       inner          `yaml:"in"`
	Ptr      *inner         `yaml:"ptr"`
	Tags     []string       `yaml:"tags"` // slice: one leaf
	Meta     map[string]int `yaml:"meta"` // map: one leaf
	If       describer      `yaml:"if"`   // interface: one leaf

	Flat inner `yaml:",inline"` // inline: flattened into the parent

	Nope string `yaml:"-"`
}

type describer interface {
	Desc() string
}

func TestLeafKeysFixture(t *testing.T) {
	got := LeafKeys(reflect.TypeOf(fixture{}))
	wantSet := map[string]struct{}{
		"alpha": {}, "beta": {}, "gamma": {}, "if": {},
		"in.alpha": {}, "in.beta": {},
		"meta": {}, "name": {},
		"ptr.alpha": {}, "ptr.beta": {},
		"tags": {},
	}
	if len(got) != len(wantSet) {
		t.Fatalf("got %d keys %v, want %d keys %v", len(got), got, len(wantSet), wantSet)
	}
	for _, k := range got {
		if _, ok := wantSet[k]; !ok {
			t.Errorf("unexpected key %q in %v", k, got)
		}
	}
	// Stable sorted order.
	for i := 1; i < len(got); i++ {
		if got[i-1] >= got[i] {
			t.Errorf("keys not strictly sorted unique: %v", got)
		}
	}
}

func TestLeafKeysSkipsSkippedAndUnreferenced(t *testing.T) {
	got := LeafKeys(reflect.TypeOf(fixture{}))
	for _, k := range got {
		switch k {
		case "hidden", "in.hidden", "ptr.hidden", "flat", "flat.alpha", "nope":
			t.Errorf("key %q must not appear (yaml:\"-\" and inline fields are flattened, not nested)", k)
		}
	}
}

func TestLeafKeysPointerTop(t *testing.T) {
	got := LeafKeys(reflect.TypeOf(&fixture{}))
	if len(got) == 0 {
		t.Fatal("expected keys when passing a pointer to a struct")
	}
}

func TestLeafKeysEmptyStruct(t *testing.T) {
	if got := LeafKeys(reflect.TypeOf(struct{}{})); len(got) != 0 {
		t.Fatalf("expected no keys, got %v", got)
	}
}
