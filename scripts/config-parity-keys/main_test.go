package main

import (
	"reflect"
	"sort"
	"testing"
)

// embeddedBlock is a normal nested config block (tagged `block`).
type embeddedBlock struct {
	Beta  string   `yaml:"beta"`
	Gamma []string `yaml:"gamma"`
}

// fixture exercises every rule the walker must follow.
type fixture struct {
	Alpha    string         `yaml:"alpha"`
	Block    embeddedBlock  `yaml:"block"`
	Inline   inlineHolder   `yaml:",inline"`
	Nested   *nested        `yaml:"nested"`
	Children map[string]int `yaml:"children,omitempty"`
	Raw      []byte         `yaml:"raw"`
	Skip     string         `yaml:"-"`
	Subs     embeddedSubs   `yaml:",inline"`
}

// inlineHolder is untagged-embedded with inline semantics at the field
// level: the fixture's `Inline` field carries `yaml:",inline"` and is a
// struct, so its children must appear at the top level.
type inlineHolder struct {
	Tagged string `yaml:"tagged"`
}

// embeddedSubs is an anonymous embedded struct with no tag at all.
type embeddedSubs struct {
	Deep deep `yaml:"deep"`
}

type deep struct {
	Leaf string `yaml:"leaf"`
}

type nested struct {
	Name string            `yaml:"name"`
	Ref  *nested           `yaml:"ref"`
	Meta map[string]string `yaml:"meta,omitempty"`
}

func fixtureKeys() []string {
	seen := map[string]bool{}
	out := []string{}
	for _, k := range ConfigKeys(reflect.TypeOf(fixture{}), "") {
		if !seen[k] {
			seen[k] = true
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

func TestConfigKeysFixture(t *testing.T) {
	got := fixtureKeys()
	want := []string{
		"alpha",
		"block.beta", // nested struct recursed
		"block.gamma",
		"children",    // map is one leaf
		"deep.leaf",   // tagless anonymous embedded struct flattened
		"nested.meta", // pointee struct recursed
		"nested.name",
		"nested.ref", // pointer to struct recursed
		"nested.ref.name",
		"nested.ref.ref",
		"raw",    // slice is one leaf
		"tagged", // yaml:",inline" field flattened into parent
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v\nwant %v", got, want)
	}
}

func TestConfigKeysSortedAndDeduped(t *testing.T) {
	got := fixtureKeys()
	if !sort.StringsAreSorted(got) {
		t.Fatalf("not sorted: %v", got)
	}
	for i := 1; i < len(got); i++ {
		if got[i] == got[i-1] {
			t.Fatalf("duplicate: %q", got[i])
		}
	}
}

func TestUntaggedNamedFieldSkipped(t *testing.T) {
	type withUntagged struct {
		Tagged   string `yaml:"tagged"`
		Untagged string
	}
	got := ConfigKeys(reflect.TypeOf(withUntagged{}), "p")
	if len(got) != 1 || got[0] != "p.tagged" {
		t.Fatalf("got %v, want [p.tagged]", got)
	}
}

func TestSkippedFieldAbsent(t *testing.T) {
	type hidden struct {
		Visible string `yaml:"visible"`
		Hidden  string `yaml:"-"`
	}
	got := ConfigKeys(reflect.TypeOf(hidden{}), "pfx")
	if len(got) != 1 || got[0] != "pfx.visible" {
		t.Fatalf("got %v, want [pfx.visible]", got)
	}
}
