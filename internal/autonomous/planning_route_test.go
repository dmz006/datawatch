package autonomous

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/config"
)

// fixedRoutingClock is the single deterministic clock every routing
// test shares; every decision must carry exactly this instant.
var fixedRoutingClock = time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)

func fixedRoutingNow() time.Time {
	return fixedRoutingClock
}

func routingRule(name, plannerBackend, plannerModel string, m config.PlanningMatch) config.PlanningRule {
	return config.PlanningRule{
		Name:  name,
		Match: m,
		Planners: []config.PlannerRef{
			{Backend: plannerBackend, Model: plannerModel},
			{Backend: "pi-prompt", Model: "qwen3.8:27b"},
		},
	}
}

func enabledRoutingCfg(rules ...config.PlanningRule) config.PlanningRoutingConfig {
	cfg := config.PlanningRoutingDefaults()
	cfg.Enabled = true
	cfg.Rules = rules
	return cfg
}

func TestPlanningRoutingFirstMatchOrder(t *testing.T) {
	cfg := enabledRoutingCfg(
		routingRule("first", "claude-code", "claude-opus-5-5", config.PlanningMatch{}),
		routingRule("second", "claude-code", "claude-sonnet-5", config.PlanningMatch{}),
	)
	in := RouteInput{Kind: "prd", Tags: []string{"anything"}}
	d := RoutePlanner(in, cfg, fixedRoutingNow)
	if d.Reason != RouteReasonRuleMatched {
		t.Fatalf("reason = %q, want %q", d.Reason, RouteReasonRuleMatched)
	}
	if d.RuleName != "first" {
		t.Fatalf("rule = %q, want first", d.RuleName)
	}
	if d.Planners[0].Backend != "claude-code" || d.Planners[0].Model != "claude-opus-5-5" {
		t.Fatalf("planner[0] = %+v, want first rule's planner", d.Planners[0])
	}
	if !reflect.DeepEqual(d.Planners, []config.PlannerRef{{Backend: "claude-code", Model: "claude-opus-5-5"}, {Backend: "pi-prompt", Model: "qwen3.8:27b"}}) {
		t.Fatalf("planners = %+v, want ordered first-rule list", d.Planners)
	}
	if d.Fallback {
		t.Fatal("fallback = true, want false")
	}
	if d.LadderName != config.PlanningRoutingLadderNameDefault {
		t.Fatalf("ladder = %q, want default", d.LadderName)
	}
	if !d.DecidedAt.Equal(fixedRoutingClock) {
		t.Fatalf("decided_at = %v, want fixed clock", d.DecidedAt)
	}
}

func TestPlanningRoutingMatchFieldsAlone(t *testing.T) {
	in := RouteInput{
		Kind:      "prd",
		Tags:      []string{"security", "x"},
		SpecBytes: 1000,
		Project:   "alpha",
		Files:     []string{"internal/server/auth.go"},
	}
	cases := []struct {
		name string
		m    config.PlanningMatch
		want bool
	}{
		{"empty-wildcard", config.PlanningMatch{}, true},
		{"kind-alone", config.PlanningMatch{Kind: []string{"prd"}}, true},
		{"kind-miss", config.PlanningMatch{Kind: []string{"task"}}, false},
		{"tags-alone", config.PlanningMatch{Tags: []string{"security"}}, true},
		{"tags-miss", config.PlanningMatch{Tags: []string{"nope"}}, false},
		{"project-alone", config.PlanningMatch{Project: []string{"alpha"}}, true},
		{"project-miss", config.PlanningMatch{Project: []string{"omega"}}, false},
		{"risk-class-tag", config.PlanningMatch{RiskClass: []string{"security"}}, true},
		{"risk-class-glob", config.PlanningMatch{RiskClass: []string{"security"}}, true},
		{"risk-class-design", config.PlanningMatch{RiskClass: []string{"design"}}, false},
		{"spec-min", config.PlanningMatch{SpecSizeMin: 1000}, true},
		{"spec-max", config.PlanningMatch{SpecSizeMax: 1000}, true},
		{"override-field", config.PlanningMatch{Override: "manual"}, true},
		{"override-field-miss", config.PlanningMatch{Override: "manual"}, false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			if strings.HasPrefix(tc.name, "override-field") {
				if strings.HasSuffix(tc.name, "-miss") {
					in.Override = "something-else"
				} else {
					in.Override = "manual"
				}
			}
			cfg := enabledRoutingCfg(routingRule("r", "b", "m", tc.m))
			d := RoutePlanner(in, cfg, fixedRoutingNow)
			got := d.Reason == RouteReasonRuleMatched && d.RuleName == "r"
			if got != tc.want {
				t.Fatalf("decision = %+v, want matched=%v", d, tc.want)
			}
		})
	}
}

func TestPlanningRoutingMatchFieldsCombined(t *testing.T) {
	in := RouteInput{
		Kind:      "prd",
		Tags:      []string{"security"},
		SpecBytes: 1000,
		Project:   "alpha",
		Files:     []string{"docs/readme.md"},
	}
	all := config.PlanningMatch{
		Kind:        []string{"prd"},
		Tags:        []string{"security"},
		Project:     []string{"alpha", "beta"},
		RiskClass:   []string{"security"},
		SpecSizeMin: 500,
		SpecSizeMax: 5000,
	}
	cfg := enabledRoutingCfg(routingRule("combined", "b", "m", all))
	d := RoutePlanner(in, cfg, fixedRoutingNow)
	if d.Reason != RouteReasonRuleMatched || d.RuleName != "combined" {
		t.Fatalf("decision = %+v, want combined rule to match", d)
	}

	// One field breaks each variant.
	missKind := all
	missKind.Kind = []string{"task"}
	if d := RoutePlanner(in, enabledRoutingCfg(routingRule("r", "b", "m", missKind)), fixedRoutingNow); d.Reason != RouteReasonNoMatch {
		t.Fatalf("reason = %q, want %q", d.Reason, RouteReasonNoMatch)
	}
	missSize := all
	missSize.SpecSizeMin = 1001
	if d := RoutePlanner(in, enabledRoutingCfg(routingRule("r", "b", "m", missSize)), fixedRoutingNow); d.Reason != RouteReasonNoMatch {
		t.Fatalf("reason = %q, want %q", d.Reason, RouteReasonNoMatch)
	}
}

func TestPlanningRoutingEmptyFieldWildcard(t *testing.T) {
	cfg := enabledRoutingCfg(
		config.PlanningRule{
			Name:     "tags-only",
			Match:    config.PlanningMatch{Tags: []string{"a"}},
			Planners: []config.PlannerRef{{Backend: "b", Model: "m"}},
		},
	)
	// Input with every other field populated must still match.
	in := RouteInput{Kind: "prdx", Tags: []string{"a", "b"}, SpecBytes: 99, Project: "p", Files: []string{"f"}}
	d := RoutePlanner(in, cfg, fixedRoutingNow)
	if d.Reason != RouteReasonRuleMatched || d.RuleName != "tags-only" {
		t.Fatalf("decision = %+v, want tags-only rule (other fields wildcard)", d)
	}
}

func TestPlanningRoutingSizeBounds(t *testing.T) {
	both := config.PlanningMatch{SpecSizeMin: 100, SpecSizeMax: 200}
	for name, sz := range map[string]int{"below": 99, "at-min": 100, "middle": 150, "at-max": 200, "above": 201} {
		cfg := enabledRoutingCfg(routingRule("size", "b", "m", both))
		d := RoutePlanner(RouteInput{SpecBytes: sz}, cfg, fixedRoutingNow)
		want := sz >= 100 && sz <= 200
		if (d.Reason == RouteReasonRuleMatched) != want {
			t.Fatalf("size %s (%d): reason = %q, want matched=%v", name, sz, d.Reason, want)
		}
	}
	// Unbounded when both are 0.
	cfg := enabledRoutingCfg(routingRule("unbounded", "b", "m", config.PlanningMatch{}))
	if d := RoutePlanner(RouteInput{SpecBytes: 0}, cfg, fixedRoutingNow); d.Reason != RouteReasonRuleMatched {
		t.Fatalf("reason = %q, want rule-matched for unbounded size", d.Reason)
	}
}

func TestPlanningRoutingRiskClassGlobs(t *testing.T) {
	cases := []struct {
		glob, file string
		want       bool
	}{
		{"security/**", "security/policy.yaml", true},
		{"security/**", "security/deep/nested/policy.yaml", true},
		{"security/**", "security", false},       // not under the prefix
		{"security/**", "securityx/a.go", false}, // prefix must be a path boundary
		{"internal/server/auth*", "internal/server/auth.go", true},
		{"internal/server/auth*", "internal/server/author.go", true},
		{"internal/server/auth*", "internal/server/session.go", false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.glob+"/"+tc.file, func(t *testing.T) {
			cfg := config.PlanningRoutingDefaults()
			cfg.Classes = map[string]config.RiskClass{
				"myclass": {Name: "myclass", PathGlobs: []string{tc.glob}},
			}
			cfg.Rules = []config.PlanningRule{
				routingRule("r", "b", "m", config.PlanningMatch{RiskClass: []string{"myclass"}}),
			}
			cfg.Enabled = true
			d := RoutePlanner(RouteInput{Files: []string{tc.file}}, cfg, fixedRoutingNow)
			if (d.Reason == RouteReasonRuleMatched) != tc.want {
				t.Fatalf("glob %q file %q: reason = %q, want matched=%v", tc.glob, tc.file, d.Reason, tc.want)
			}
		})
	}
}

func TestPlanningRoutingClassesSortedAndCarried(t *testing.T) {
	cfg := enabledRoutingCfg(routingRule("r", "b", "m", config.PlanningMatch{}))
	in := RouteInput{Tags: []string{"design", "ui"}, Files: []string{"security/p.yaml"}}
	d := RoutePlanner(in, cfg, fixedRoutingNow)
	if !reflect.DeepEqual(d.Classes, []string{"design", "security"}) {
		t.Fatalf("classes = %v, want [design security]", d.Classes)
	}
}

func TestPlanningRoutingOverrideRuleName(t *testing.T) {
	cfg := enabledRoutingCfg(
		routingRule("first", "b1", "m1", config.PlanningMatch{}),
		routingRule("second", "b2", "m2", config.PlanningMatch{}),
	)
	d := RoutePlanner(RouteInput{Override: "second"}, cfg, fixedRoutingNow)
	if d.Reason != RouteReasonOverride {
		t.Fatalf("reason = %q, want %q", d.Reason, RouteReasonOverride)
	}
	if d.RuleName != "second" || d.Planners[0].Backend != "b2" || d.Planners[0].Model != "m2" {
		t.Fatalf("decision = %+v, want second rule's planners", d)
	}
	if d.Fallback {
		t.Fatal("fallback = true, want false")
	}
}

func TestPlanningRoutingOverrideBackendModel(t *testing.T) {
	cfg := enabledRoutingCfg(routingRule("ignored", "b", "m", config.PlanningMatch{}))
	// Model itself carries a colon.
	d := RoutePlanner(RouteInput{Override: "pi-prompt:qwen3.8:27b"}, cfg, fixedRoutingNow)
	if d.Reason != RouteReasonOverride {
		t.Fatalf("reason = %q, want %q", d.Reason, RouteReasonOverride)
	}
	if !reflect.DeepEqual(d.Planners, []config.PlannerRef{{Backend: "pi-prompt", Model: "qwen3.8:27b"}}) {
		t.Fatalf("planners = %+v, want the single overridden planner", d.Planners)
	}
	if d.LadderName != config.PlanningRoutingLadderNameDefault {
		t.Fatalf("ladder = %q, want default", d.LadderName)
	}
	if d.RuleName != "" {
		t.Fatalf("rule = %q, want empty for a backend:model override", d.RuleName)
	}
	if d.Fallback {
		t.Fatal("fallback = true, want false")
	}
}

func TestPlanningRoutingOverrideUnknownFallsThrough(t *testing.T) {
	cfg := enabledRoutingCfg(routingRule("wild", "b", "m", config.PlanningMatch{}))
	// Not a rule name and not "backend:model": falls through to rule
	// matching and the wildcard rule still applies.
	d := RoutePlanner(RouteInput{Override: "whatever", Tags: []string{"x"}}, cfg, fixedRoutingNow)
	if d.Reason != RouteReasonRuleMatched || d.RuleName != "wild" {
		t.Fatalf("decision = %+v, want fall-through rule match", d)
	}
}

func TestPlanningRoutingDisabled(t *testing.T) {
	cfg := config.PlanningRoutingDefaults() // Enabled=false
	cfg.Rules = []config.PlanningRule{routingRule("r", "b", "m", config.PlanningMatch{})}
	d := RoutePlanner(RouteInput{Tags: []string{"design"}}, cfg, fixedRoutingNow)
	if d.Reason != RouteReasonDisabled {
		t.Fatalf("reason = %q, want %q", d.Reason, RouteReasonDisabled)
	}
	if !d.Fallback {
		t.Fatal("fallback = false, want true")
	}
	if len(d.Planners) != 0 {
		t.Fatalf("planners = %+v, want none", d.Planners)
	}
	if !d.DecidedAt.Equal(fixedRoutingClock) {
		t.Fatalf("decided_at = %v, want fixed clock", d.DecidedAt)
	}
}

func TestPlanningRoutingShadow(t *testing.T) {
	cfg := enabledRoutingCfg(routingRule("r", "b1", "m1", config.PlanningMatch{}), routingRule("s", "b2", "m2", config.PlanningMatch{}))
	cfg.Shadow = true
	in := RouteInput{Tags: []string{"design"}}
	d := RoutePlanner(in, cfg, fixedRoutingNow)
	if d.Reason != RouteReasonRuleMatched || d.RuleName != "r" {
		t.Fatalf("decision = %+v, want rule r under shadow", d)
	}
	if !d.Shadow {
		t.Fatal("shadow = false, want true")
	}
	if !reflect.DeepEqual(d.Planners, []config.PlannerRef{{Backend: "b1", Model: "m1"}, {Backend: "pi-prompt", Model: "qwen3.8:27b"}}) {
		t.Fatalf("planners = %+v, want rule r's planners in shadow", d.Planners)
	}

	// Shadow with no matching rule stays a fallback decision, flagged
	// shadow.
	cfg = enabledRoutingCfg(routingRule("nomatch", "b", "m", config.PlanningMatch{Project: []string{"other"}}))
	cfg.Shadow = true
	d = RoutePlanner(RouteInput{Project: "here"}, cfg, fixedRoutingNow)
	if d.Reason != RouteReasonNoMatch || !d.Fallback || !d.Shadow {
		t.Fatalf("decision = %+v, want shadow fallback", d)
	}
}

func TestPlanningRoutingDeterministic(t *testing.T) {
	cfg := enabledRoutingCfg(
		routingRule("a", "b1", "m1", config.PlanningMatch{Tags: []string{"x"}}),
		routingRule("b", "b2", "m2", config.PlanningMatch{RiskClass: []string{"security"}}),
	)
	in := RouteInput{Kind: "prd", Tags: []string{"security", "design", "x"}, SpecBytes: 42, Project: "p", Files: []string{"a/b.go", "security/c.go"}}
	d1 := RoutePlanner(in, cfg, fixedRoutingNow)
	d2 := RoutePlanner(in, cfg, fixedRoutingNow)
	d3 := RoutePlanner(in, cfg, func() time.Time { return fixedRoutingClock.Add(0) })
	for i, d := range []RouteDecision{d2, d3} {
		if !reflect.DeepEqual(d1, d) {
			t.Fatalf("call %d = %+v\ncall 1 = %+v", i+2, d, d1)
		}
	}
	if !d1.DecidedAt.Equal(fixedRoutingClock) {
		t.Fatalf("decided_at = %v, want fixed clock", d1.DecidedAt)
	}
}

func TestPlanningRoutingLadderName(t *testing.T) {
	cfg := enabledRoutingCfg(
		routingRule("r", "b", "m", config.PlanningMatch{}),
	)
	cfg.Rules[0].Ladder = "big"
	cfg.Ladders["big"] = config.PlanningLadder{Name: "big", TouchUpMax: 3, ReplanMax: 2, EscalateAfterAttempts: 4}
	d := RoutePlanner(RouteInput{Tags: []string{"x"}}, cfg, fixedRoutingNow)
	if d.LadderName != "big" {
		t.Fatalf("ladder = %q, want big", d.LadderName)
	}
}

func TestPlanningRoutingRulePlannerIsolation(t *testing.T) {
	cfg := enabledRoutingCfg(routingRule("r", "b", "m", config.PlanningMatch{}))
	d1 := RoutePlanner(RouteInput{}, cfg, fixedRoutingNow)
	cfg.Rules[0].Planners[0].Model = "mutated"
	d2 := RoutePlanner(RouteInput{}, cfg, fixedRoutingNow)
	if d1.Planners[0].Model != "m" {
		t.Fatalf("earlier decision mutated: %+v", d1.Planners[0])
	}
	if d2.Planners[0].Model != "mutated" {
		t.Fatalf("later decision = %+v, want mutated planner", d2.Planners[0])
	}
}
