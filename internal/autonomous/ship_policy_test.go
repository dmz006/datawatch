package autonomous

import (
	"reflect"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

func TestShipPolicyDefaults(t *testing.T) {
	SetShipConfig(config.AutonomousShipConfig{})
	t.Cleanup(func() { SetShipConfig(config.AutonomousShipConfig{}) })
	p := ShipPolicyFor("/p")
	if p.Push != "on" || p.Tag != "on" || p.CIWatch != "on" {
		t.Fatalf("modes = %+v", p)
	}
	if p.TagFormat != "v{version}" || p.Remote != "origin" || len(p.CIWorkflows) != 0 {
		t.Fatalf("defaults = %+v", p)
	}
}

func TestShipPolicyEachMode(t *testing.T) {
	for _, m := range []string{"off", "shadow", "on", "SHADOW"} {
		c := config.AutonomousShipConfig{Push: m, Tag: m, CIWatch: m}
		p, err := ResolveShipPolicy(c, "/p")
		if err != nil {
			t.Fatalf("%s: %v", m, err)
		}
		want := config.NormalizeChecksMode(m)
		if p.Push != want || p.Tag != want || p.CIWatch != want {
			t.Fatalf("%s: %+v", m, p)
		}
	}
}

func TestShipPolicyInvalidMode(t *testing.T) {
	if _, err := ResolveShipPolicy(config.AutonomousShipConfig{Tag: "loud"}, "/p"); err == nil {
		t.Fatal("expected error for invalid mode")
	}
	c := config.AutonomousShipConfig{Projects: map[string]config.AutonomousShipOverride{"/p": {CIWatch: "bogus"}}}
	if _, err := ResolveShipPolicy(c, "/p"); err == nil {
		t.Fatal("expected error for invalid override mode")
	}
	SetShipConfig(config.AutonomousShipConfig{Push: "bogus"})
	t.Cleanup(func() { SetShipConfig(config.AutonomousShipConfig{}) })
	if p := ShipPolicyFor("/p"); p.Push != "on" {
		t.Fatalf("invalid config should fall back to defaults, got %+v", p)
	}
}

func TestShipPolicyInvalidTagFormat(t *testing.T) {
	if _, err := ResolveShipPolicy(config.AutonomousShipConfig{TagFormat: "release"}, "/p"); err == nil {
		t.Fatal("expected error for tag format without placeholder")
	}
	c := config.AutonomousShipConfig{Projects: map[string]config.AutonomousShipOverride{"/p": {TagFormat: "x"}}}
	if _, err := ResolveShipPolicy(c, "/p"); err == nil {
		t.Fatal("expected error for override tag format without placeholder")
	}
	if _, err := ResolveShipPolicy(config.AutonomousShipConfig{TagFormat: "rel-{version}"}, "/p"); err != nil {
		t.Fatal(err)
	}
}

func TestShipPolicyProjectOverride(t *testing.T) {
	c := config.AutonomousShipConfig{
		Push: "on", Remote: "origin", CIWorkflows: []string{"ci"},
		Projects: map[string]config.AutonomousShipOverride{
			"/a/b": {Push: "shadow", Tag: "off", TagFormat: "rel-{version}", Remote: "upstream", CIWorkflows: []string{"release", "lint"}},
		},
	}
	p, err := ResolveShipPolicy(c, "/a/b/")
	if err != nil {
		t.Fatal(err)
	}
	if p.Push != "shadow" || p.Tag != "off" || p.CIWatch != "on" || p.TagFormat != "rel-{version}" || p.Remote != "upstream" ||
		!reflect.DeepEqual(p.CIWorkflows, []string{"release", "lint"}) {
		t.Fatalf("override = %+v", p)
	}
	q, _ := ResolveShipPolicy(c, "/other")
	if q.Push != "on" || q.Remote != "origin" || !reflect.DeepEqual(q.CIWorkflows, []string{"ci"}) {
		t.Fatalf("non-overridden = %+v", q)
	}
}
