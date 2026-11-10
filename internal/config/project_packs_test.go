package config

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestNormalizePacks(t *testing.T) {
	cases := []struct {
		in, want []string
	}{
		{nil, nil},
		{[]string{}, nil},
		{[]string{"", " ", "\t"}, nil},
		{[]string{" b ", "a", "b", ""}, []string{"b", "a"}},
	}
	for _, tc := range cases {
		if got := NormalizePacks(tc.in); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("NormalizePacks(%q) = %#v, want %#v", tc.in, got, tc.want)
		}
	}
}

func TestProjectConfigEntryPacksRoundTrip(t *testing.T) {
	e := ProjectConfigEntry{Dir: "/srv/acme", Packs: []string{"z", "a"}}
	raw, err := json.Marshal(e)
	if err != nil || !strings.Contains(string(raw), `"packs":["z","a"]`) {
		t.Fatalf("json = %s, err %v", raw, err)
	}
	var jb ProjectConfigEntry
	if err := json.Unmarshal(raw, &jb); err != nil || !reflect.DeepEqual(jb.Packs, e.Packs) {
		t.Fatalf("json back = %#v, err %v", jb.Packs, err)
	}
	y, err := yaml.Marshal(e)
	if err != nil || !strings.Contains(string(y), "packs:") {
		t.Fatalf("yaml = %s, err %v", y, err)
	}
	var yb ProjectConfigEntry
	if err := yaml.Unmarshal(y, &yb); err != nil || !reflect.DeepEqual(yb.Packs, e.Packs) {
		t.Fatalf("yaml back = %#v, err %v", yb.Packs, err)
	}
	bare, _ := json.Marshal(ProjectConfigEntry{Dir: "/x"})
	if strings.Contains(string(bare), "packs") {
		t.Fatalf("empty packs not omitted: %s", bare)
	}
}
