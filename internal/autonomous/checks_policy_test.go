package autonomous

import (
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

func TestChecksPolicyDefaults(t *testing.T) {
	SetChecksConfig(config.AutonomousChecksConfig{})
	p := ChecksPolicyFor("/some/project")
	if p.PreStartVerifier != "on" || p.PlanRules != "on" || p.AfterDone != "on" || p.RegistryReadme != "on" || !p.BlockWhenMissing {
		t.Fatalf("defaults wrong: %+v", p)
	}
	if !p.Blocks() {
		t.Fatal("default pre_start_verifier should block")
	}
}

func TestChecksPolicyEveryMode(t *testing.T) {
	for _, m := range []string{"off", "shadow", "on", " ON ", "Shadow"} {
		c := config.AutonomousChecksConfig{PreStartVerifier: m, PlanRules: m, AfterDone: m, RegistryReadme: m}
		p, err := ResolveChecksPolicy(c, "/p")
		if err != nil {
			t.Fatalf("mode %q: %v", m, err)
		}
		want := strings.ToLower(strings.TrimSpace(m))
		if p.PreStartVerifier != want || p.PlanRules != want || p.AfterDone != want || p.RegistryReadme != want {
			t.Fatalf("mode %q: got %+v", m, p)
		}
		if p.Blocks() != (want == "on") {
			t.Fatalf("mode %q: Blocks=%v", m, p.Blocks())
		}
	}
}

func TestChecksPolicyUnknownMode(t *testing.T) {
	_, err := ResolveChecksPolicy(config.AutonomousChecksConfig{AfterDone: "loud"}, "/p")
	if err == nil || !strings.Contains(err.Error(), "autonomous.checks.after_done") || !strings.Contains(err.Error(), "loud") {
		t.Fatalf("want clear error, got %v", err)
	}
	_, err = ResolveChecksPolicy(config.AutonomousChecksConfig{
		Projects: map[string]config.AutonomousChecksOverride{"/p": {PlanRules: "maybe"}},
	}, "/p")
	if err == nil || !strings.Contains(err.Error(), "maybe") {
		t.Fatalf("want override error, got %v", err)
	}
	// ChecksPolicyFor falls back to defaults on an invalid installed config.
	SetChecksConfig(config.AutonomousChecksConfig{PlanRules: "bogus"})
	defer SetChecksConfig(config.AutonomousChecksConfig{})
	if p := ChecksPolicyFor("/p"); p.PlanRules != "on" {
		t.Fatalf("fallback: %+v", p)
	}
}

func TestChecksPolicyBlockWhenMissing(t *testing.T) {
	f := false
	p, _ := ResolveChecksPolicy(config.AutonomousChecksConfig{BlockWhenMissing: &f}, "/p")
	if p.BlockWhenMissing {
		t.Fatal("explicit false must survive")
	}
}

func TestChecksPolicyPerProjectOverride(t *testing.T) {
	f := false
	c := config.AutonomousChecksConfig{
		PreStartVerifier: "shadow",
		Projects: map[string]config.AutonomousChecksOverride{
			"/work/alpha": {PreStartVerifier: "off", AfterDone: "shadow", BlockWhenMissing: &f},
		},
	}
	SetChecksConfig(c)
	defer SetChecksConfig(config.AutonomousChecksConfig{})

	a := ChecksPolicyFor("/work/alpha/")
	if a.PreStartVerifier != "off" || a.AfterDone != "shadow" || a.PlanRules != "on" || a.BlockWhenMissing {
		t.Fatalf("override: %+v", a)
	}
	b := ChecksPolicyFor("/work/beta")
	if b.PreStartVerifier != "shadow" || b.AfterDone != "on" || !b.BlockWhenMissing {
		t.Fatalf("global: %+v", b)
	}
	if e := ChecksPolicyFor(""); e.PreStartVerifier != "shadow" {
		t.Fatalf("empty dir: %+v", e)
	}
}
