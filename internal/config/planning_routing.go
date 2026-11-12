// Planning routing config: the autonomous.planning_routing block (the
// PlanningRouting field next to PlanningBackend / PlanningModel in
// internal/config/config.go).
//
// The block routes the LLM planning call to a stronger (or otherwise
// chosen) planner per rule. It is OFF by default and is inert until a
// rule matches; ExampleRules() ships example rules that are examples
// only and are never active by default.
package config

import "fmt"

// PlanningRoutingLadderNameDefault is the built-in ladder name a rule
// with an empty Ladder field refers to.
const PlanningRoutingLadderNameDefault = "default"

// Planning routing ladder count defaults: a ladder whose count field is
// left 0 reads as these.
const (
	PlanningRoutingDefaultTouchUpMax            = 1
	PlanningRoutingDefaultReplanMax             = 1
	PlanningRoutingDefaultEscalateAfterAttempts = 2
)

// PlanningRoutingConfig is the autonomous.planning_routing block.
//
// Enabled=false (the default) leaves the planner exactly as it is
// today. Shadow=true computes and records the routed decision but keeps
// the planner unchanged. Rules are matched in order; the first match
// wins. Ladders and Classes are named sub-lookups rules may reference.
type PlanningRoutingConfig struct {
	// Enabled turns planning routing on. Default false: the normally
	// resolved planner is used.
	Enabled bool `yaml:"enabled" json:"enabled"`
	// Shadow: when true, the routed decision is computed and recorded
	// but the planner is left unchanged. Default false.
	Shadow bool `yaml:"shadow" json:"shadow"`
	// Rules is the ordered matching list. An empty match field is a
	// wildcard; a non-empty list matches if ANY entry matches.
	Rules []PlanningRule `yaml:"rules,omitempty" json:"rules,omitempty"`
	// Ladders maps a ladder name to its budget; the built-in name is
	// "default". A rule with an empty Ladder field uses the default.
	Ladders map[string]PlanningLadder `yaml:"ladders,omitempty" json:"ladders,omitempty"`
	// Classes maps a risk-class name (e.g. "security", "design") to its
	// definition; built-ins exist at the names below, so an operator
	// entry with the same name overrides it.
	Classes map[string]RiskClass `yaml:"classes,omitempty" json:"classes,omitempty"`
}

// PlanningRule is one entry of PlanningRoutingConfig.Rules.
type PlanningRule struct {
	// Name identifies the rule (must be unique within a config).
	Name string `yaml:"name" json:"name"`
	// Match selects the routing input. Empty fields are wildcards.
	Match PlanningMatch `yaml:"match" json:"match"`
	// Planners is ordered: the first entry is the planner, the rest are
	// its escalation targets.
	Planners []PlannerRef `yaml:"planners" json:"planners"`
	// Ladder is the name of a ladder in PlanningRoutingConfig.Ladders;
	// empty means the default ladder.
	Ladder string `yaml:"ladder,omitempty" json:"ladder,omitempty"`
}

// PlanningMatch is a rule's selector. A list field matches when it is
// empty (wildcard) or when any of its entries matches.
type PlanningMatch struct {
	// Kind is an allowed list of routing-input kinds.
	Kind []string `yaml:"kind,omitempty" json:"kind,omitempty"`
	// Tags is an allowed list of tags.
	Tags []string `yaml:"tags,omitempty" json:"tags,omitempty"`
	// Project is an allowed list of project identifiers.
	Project []string `yaml:"project,omitempty" json:"project,omitempty"`
	// RiskClass is an allowed list of risk-class names (see
	// BuiltInRiskClasses and PlanningRoutingConfig.Classes).
	RiskClass []string `yaml:"risk_class,omitempty" json:"risk_class,omitempty"`
	// SpecSizeMin is the minimum spec size; 0 is unbounded below.
	SpecSizeMin int `yaml:"spec_size_min,omitempty" json:"spec_size_min,omitempty"`
	// SpecSizeMax is the maximum spec size; 0 is unbounded above.
	SpecSizeMax int `yaml:"spec_size_max,omitempty" json:"spec_size_max,omitempty"`
	// Override is a free-form routing override tag the rule applies.
	Override string `yaml:"override,omitempty" json:"override,omitempty"`
}

// PlannerRef names one planner (an LLM backend and its model).
type PlannerRef struct {
	// Backend is the LLM registry name.
	Backend string `yaml:"backend" json:"backend"`
	// Model is the model the backend serves.
	Model string `yaml:"model" json:"model"`
}

// PlanningLadder is the budget ladder a rule uses. A count field of 0
// reads as its default above.
type PlanningLadder struct {
	// Name is the ladder's identity (should equal its key).
	Name string `yaml:"name,omitempty" json:"name,omitempty"`
	// TouchUpMax is the maximum touch-up passes; 0 = 1.
	TouchUpMax int `yaml:"touch_up_max,omitempty" json:"touch_up_max,omitempty"`
	// ReplanMax is the maximum replans; 0 = 1.
	ReplanMax int `yaml:"replan_max,omitempty" json:"replan_max,omitempty"`
	// EscalateAfterAttempts is the attempt count at which the ladder
	// escalates to the rule's next planner; 0 = 2.
	EscalateAfterAttempts int `yaml:"escalate_after_attempts,omitempty" json:"escalate_after_attempts,omitempty"`
}

// RiskClass names a set of tags and path globs that mark inputs as
// belonging to the class.
type RiskClass struct {
	// Name is the class's identity.
	Name string `yaml:"name,omitempty" json:"name,omitempty"`
	// Tags match on the routing input's tags.
	Tags []string `yaml:"tags,omitempty" json:"tags,omitempty"`
	// PathGlobs match on the routing input's touched paths
	// (e.g. "security/**").
	PathGlobs []string `yaml:"path_globs,omitempty" json:"path_globs,omitempty"`
}

// PlanningRoutingDefaults is the block's factory: routing off, no
// rules, the built-in "default" ladder and the built-in risk classes
// ("security", "design").
func PlanningRoutingDefaults() PlanningRoutingConfig {
	return PlanningRoutingConfig{
		// Enabled and Shadow stay false (the zero value).
		Ladders: map[string]PlanningLadder{
			PlanningRoutingLadderNameDefault: PlanningLadder{
				Name:                  PlanningRoutingLadderNameDefault,
				TouchUpMax:            PlanningRoutingDefaultTouchUpMax,
				ReplanMax:             PlanningRoutingDefaultReplanMax,
				EscalateAfterAttempts: PlanningRoutingDefaultEscalateAfterAttempts,
			},
		},
		Classes: BuiltInRiskClasses(),
	}
}

// BuiltInRiskClasses are the named classes always available to a rule
// (an operator entry with the same name overrides it).
func BuiltInRiskClasses() map[string]RiskClass {
	return map[string]RiskClass{
		"security": {
			Name:      "security",
			Tags:      []string{"security"},
			PathGlobs: []string{"security/**", "internal/server/auth*"},
		},
		"design": {
			Name: "design",
			Tags: []string{"design", "ui"},
		},
	}
}

// ExampleRules are example rules routing design-tagged and
// security-tagged Automata to a stronger planner (the first entry of
// Planners; the second is its escalation target). These are examples
// only: they are never active by default (PlanningRoutingDefaults
// ships no rules and Enabled=false).
func ExampleRules() []PlanningRule {
	return []PlanningRule{
		{
			Name:  "security-strong",
			Match: PlanningMatch{RiskClass: []string{"security"}, Tags: []string{"security"}},
			Planners: []PlannerRef{
				{Backend: "claude-code", Model: "claude-opus-5-5"},
				{Backend: "pi-prompt", Model: "qwen3.8:27b"},
			},
		},
		{
			Name:  "design-strong",
			Match: PlanningMatch{Tags: []string{"design", "ui"}},
			Planners: []PlannerRef{
				{Backend: "claude-code", Model: "claude-opus-5-5"},
				{Backend: "pi-prompt", Model: "qwen3.8:27b"},
			},
		},
	}
}

// ValidatePlanningRouting rejects malformed planning_routing configs:
// duplicate rule names, rules with no planners, empty backend or model
// strings, unknown ladder references, spec size min greater than max,
// negative ladder counts, and unknown risk-class references.
func ValidatePlanningRouting(cfg PlanningRoutingConfig) error {
	knownClasses := map[string]bool{}
	for name := range BuiltInRiskClasses() {
		knownClasses[name] = true
	}
	for name := range cfg.Classes {
		knownClasses[name] = true
	}

	seenRules := map[string]bool{}
	for _, rule := range cfg.Rules {
		if seenRules[rule.Name] {
			return fmt.Errorf("planning_routing: duplicate rule name %q", rule.Name)
		}
		seenRules[rule.Name] = true

		if len(rule.Planners) == 0 {
			return fmt.Errorf("planning_routing: rule %q has no planners", rule.Name)
		}
		for i, p := range rule.Planners {
			if p.Backend == "" {
				return fmt.Errorf("planning_routing: rule %q planner %d: empty backend", rule.Name, i)
			}
			if p.Model == "" {
				return fmt.Errorf("planning_routing: rule %q planner %d: empty model", rule.Name, i)
			}
		}

		if rule.Ladder != "" {
			if _, ok := cfg.Ladders[rule.Ladder]; !ok {
				return fmt.Errorf("planning_routing: rule %q references unknown ladder %q", rule.Name, rule.Ladder)
			}
		}

		for _, class := range rule.Match.RiskClass {
			if !knownClasses[class] {
				return fmt.Errorf("planning_routing: rule %q references unknown risk class %q", rule.Name, class)
			}
		}

		if rule.Match.SpecSizeMax > 0 && rule.Match.SpecSizeMin > rule.Match.SpecSizeMax {
			return fmt.Errorf("planning_routing: rule %q: spec_size_min %d greater than spec_size_max %d",
				rule.Name, rule.Match.SpecSizeMin, rule.Match.SpecSizeMax)
		}
	}

	for name, ladder := range cfg.Ladders {
		if ladder.TouchUpMax < 0 || ladder.ReplanMax < 0 || ladder.EscalateAfterAttempts < 0 {
			return fmt.Errorf("planning_routing: ladder %q has negative count(s)", name)
		}
	}

	return nil
}
