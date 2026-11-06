package placementtags

import (
	"reflect"
	"testing"
)

func equalTags(t *testing.T, name string, got, want []string) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%s: Derive = %q, want %q", name, got, want)
	}
}

func containsTag(tags []string, want string) bool {
	for _, tag := range tags {
		if tag == want {
			return true
		}
	}
	return false
}

func fullFacts() PlacementFacts {
	return PlacementFacts{
		Compute: "node1",
		Peer:    "peer1",
		Backend: "llm1",
		Lane:    "gpu",
		Role:    "worker",
		Project: "proj1",
	}
}

// TestPlacementTags_LocalNode: local work fills a compute node but no federated
// peer, so the result carries a compute tag and no peer tag.
func TestPlacementTags_LocalNode(t *testing.T) {
	f := PlacementFacts{Compute: "node-local", Lane: "gpu", Role: "worker"}
	got := Derive(f, DefaultSettings())

	wantCompute := "compute:node-local"
	if !containsTag(got, wantCompute) {
		t.Fatalf("local work should yield %q, got %q", wantCompute, got)
	}
	for _, tag := range got {
		if tag == "peer:node-local" || len(tag) >= 5 && tag[:5] == "peer:" {
			t.Fatalf("local work must not yield a peer tag, got %q in %q", tag, got)
		}
	}
}

// TestPlacementTags_AxisSwitchedOff: disabling one axis removes only that axis's
// tag while the others still derive.
func TestPlacementTags_AxisSwitchedOff(t *testing.T) {
	s := DefaultSettings()
	s.Lane.Enabled = false

	got := Derive(fullFacts(), s)

	if containsTag(got, "lane:gpu") {
		t.Fatalf("disabled lane axis must not yield a tag, got %q", got)
	}
	want := []string{"backend:llm1", "compute:node1", "peer:peer1", "project:proj1", "role:worker"}
	equalTags(t, "lane off", got, want)
}

func TestPlacementTags_EachAxisOn(t *testing.T) {
	want := []string{"backend:llm1", "compute:node1", "lane:gpu", "peer:peer1", "project:proj1", "role:worker"}
	equalTags(t, "each axis on", Derive(fullFacts(), DefaultSettings()), want)
}

func TestPlacementTags_EachAxisOff(t *testing.T) {
	axes := []struct {
		name    string
		disable func(*Settings)
		gone    string
	}{
		{"compute", func(s *Settings) { s.Compute.Enabled = false }, "compute:node1"},
		{"peer", func(s *Settings) { s.Peer.Enabled = false }, "peer:peer1"},
		{"backend", func(s *Settings) { s.Backend.Enabled = false }, "backend:llm1"},
		{"lane", func(s *Settings) { s.Lane.Enabled = false }, "lane:gpu"},
		{"role", func(s *Settings) { s.Role.Enabled = false }, "role:worker"},
		{"project", func(s *Settings) { s.Project.Enabled = false }, "project:proj1"},
	}
	for _, ax := range axes {
		t.Run(ax.name, func(t *testing.T) {
			s := DefaultSettings()
			ax.disable(&s)
			got := Derive(fullFacts(), s)
			if containsTag(got, ax.gone) {
				t.Fatalf("axis %q disabled but %q still present in %q", ax.name, ax.gone, got)
			}
			if len(got) != 5 {
				t.Fatalf("want 5 tags with one axis off, got %q", got)
			}
		})
	}
}

func TestPlacementTags_EmptyValueSkipped(t *testing.T) {
	// Only Lane is filled; even with every axis enabled, only lane is tagged.
	f := PlacementFacts{Lane: "claude"}
	want := []string{"lane:claude"}
	equalTags(t, "empty value skipped", Derive(f, DefaultSettings()), want)
}

func TestPlacementTags_CustomAxisFromFileFields(t *testing.T) {
	s := Settings{
		Compute: AxisSetting{Enabled: true, Template: "compute:{node}"},
		Custom: []CustomAxis{
			{Name: "env", Field: "env", Template: "env:{value}", Enabled: true},
		},
	}
	f := PlacementFacts{Compute: "node1", Fields: map[string]string{"env": "prod"}}

	got := Derive(f, s)
	want := []string{"compute:node1", "env:prod"}
	equalTags(t, "custom axis", got, want)
}

func TestPlacementTags_CustomAxisEmptyFieldSkipped(t *testing.T) {
	s := Settings{
		Compute: AxisSetting{Enabled: true, Template: "compute:{node}"},
		Custom:  []CustomAxis{{Name: "env", Field: "env", Template: "env:{value}", Enabled: true}},
	}
	got := Derive(PlacementFacts{Compute: "node1"}, s)
	want := []string{"compute:node1"}
	equalTags(t, "empty custom field skipped", got, want)
}

func TestPlacementTags_TemplateChange(t *testing.T) {
	s := DefaultSettings()
	s.Compute.Template = "on-{node}"
	got := Derive(PlacementFacts{Compute: "node1"}, s)
	want := []string{"on-node1"}
	equalTags(t, "template change", got, want)
}

func TestPlacementTags_DuplicateValuesCollapsed(t *testing.T) {
	s := Settings{
		Compute: AxisSetting{Enabled: true, Template: "ax:{node}"},
		Peer:    AxisSetting{Enabled: true, Template: "ax:{peer}"},
	}
	f := PlacementFacts{Compute: "x", Peer: "x"}
	got := Derive(f, s)
	want := []string{"ax:x"}
	equalTags(t, "duplicate collapsed", got, want)
}

func TestPlacementTags_ZeroSettingsYieldsNoTags(t *testing.T) {
	got := Derive(fullFacts(), Settings{})
	if len(got) != 0 {
		t.Fatalf("zero Settings should yield no tags, got %q", got)
	}
}

func TestValidateSettings_Valid(t *testing.T) {
	s := DefaultSettings()
	s.Custom = []CustomAxis{{Name: "env", Field: "env", Template: "env:{value}", Enabled: true}}
	if err := ValidateSettings(s); err != nil {
		t.Fatalf("expected valid settings, got %v", err)
	}
}

func TestValidateSettings_InvalidTemplateRejected(t *testing.T) {
	cases := map[string]func(*Settings){
		"builtin missing placeholder": func(s *Settings) { s.Compute.Template = "nobody" },
		"custom missing placeholder": func(s *Settings) {
			s.Compute.Template = "compute:{node}"
			s.Custom = []CustomAxis{{Name: "env", Field: "env", Template: "env:value", Enabled: true}}
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			s := DefaultSettings()
			mutate(&s)
			if err := ValidateSettings(s); err == nil {
				t.Fatalf("want an error for %s, got nil", name)
			}
		})
	}
}
