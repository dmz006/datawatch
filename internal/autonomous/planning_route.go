package autonomous

import (
	"path"
	"sort"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/config"
)

// Reason strings returned in RouteDecision.Reason. Fixed vocabulary:
// the set is closed so callers and logs can match on exact values.
const (
	// RouteReasonRuleMatched: enabled routing, a rule matched, its
	// planners and ladder are the routed decision.
	RouteReasonRuleMatched = "rule-matched"
	// RouteReasonOverride: an explicit override (rule name or
	// backend:model) selected the decision.
	RouteReasonOverride = "override"
	// RouteReasonNoMatch: routing is enabled but no rule matched; the
	// caller falls back to the global planning backend and model.
	RouteReasonNoMatch = "no-rule-matched"
	// RouteReasonDisabled: routing is disabled; same fallback.
	RouteReasonDisabled = "routing-disabled"
)

// RouteInput is the selector state passed to RoutePlanner. All fields
// are plain values (no LLM, no I/O).
type RouteInput struct {
	// Kind is the routing input's kind.
	Kind string
	// Tags are the input's tags.
	Tags []string
	// SpecBytes is the spec size in bytes.
	SpecBytes int
	// Project is the project identifier.
	Project string
	// Files are the repo-relative paths the Automaton's tasks touch.
	Files []string
	// Override is an explicit routing override: a rule name, or
	// "backend:model" to select a planner directly.
	Override string
}

// RouteDecision is the pure output of RoutePlanner. When Fallback is
// true the caller must use the global planning backend and model it
// supplies (Planners is empty and LadderName is ""); otherwise the
// caller uses Planners (first entry is the planner, the rest are its
// escalation targets) and LadderName.
//
// Precedence contract (callers must keep this): explicit per-PRD
// decomposition fields and the Automaton's own planning fields always
// win over routing. RoutePlanner is only consulted when neither of
// those is set. When Shadow is true the decision was computed exactly
// the same way, it must be recorded, but the caller must NOT change
// the planner.
type RouteDecision struct {
	// RuleName is the selected rule's name ("" for a backend:model
	// override or a fallback).
	RuleName string
	// Classes are the risk-class names the input resolves to, sorted
	// (deterministic).
	Classes []string
	// Planners is the routed planner list, in order (empty on
	// fallback).
	Planners []config.PlannerRef
	// LadderName names the planner ladder; "" on fallback, the
	// default ladder or the rule's ladder otherwise.
	LadderName string
	// Shadow reports that the decision was computed for the record
	// only; the caller must keep the previously resolved planner.
	Shadow bool
	// Fallback reports that the caller should use the global planning
	// backend and model instead.
	Fallback bool
	// Reason is one of the RouteReason* constants.
	Reason string
	// DecidedAt is the fixed clock reading for this decision.
	DecidedAt time.Time
}

// RoutePlanner resolves the planning route for in under cfg. It is a
// pure function: given identical in, cfg and now it returns identical
// decisions. The precedence contract is documented on RouteDecision
// above; RoutePlanner itself never inspects a PRD or an Automaton, so
// it cannot override explicit per-PRD decomposition fields or the
// Automaton's own planning fields — the caller must simply not call
// it in that case.
func RoutePlanner(in RouteInput, cfg config.PlanningRoutingConfig, now func() time.Time) RouteDecision {
	decidedAt := now()
	classes := resolvedRiskClasses(cfg, in)

	if !cfg.Enabled {
		return RouteDecision{
			Classes:   classes,
			Fallback:  true,
			Reason:    RouteReasonDisabled,
			DecidedAt: decidedAt,
		}
	}
	shadow := cfg.Shadow

	// Overrides are evaluated before rule walking. A value equal to a
	// rule name selects that rule directly; a "backend:model" value
	// selects that planner directly with the default ladder. A value
	// that is neither falls through to normal rule matching.
	if in.Override != "" {
		for i := range cfg.Rules {
			if cfg.Rules[i].Name == in.Override {
				rule := cfg.Rules[i]
				d := routedRouteDecision(rule, classes, shadow, RouteReasonOverride, decidedAt)
				d.RuleName = rule.Name
				return d
			}
		}
		if be, model, ok := splitBackendModel(in.Override); ok {
			return RouteDecision{
				Classes:    classes,
				Planners:   []config.PlannerRef{{Backend: be, Model: model}},
				LadderName: config.PlanningRoutingLadderNameDefault,
				Shadow:     shadow,
				Reason:     RouteReasonOverride,
				DecidedAt:  decidedAt,
			}
		}
	}

	// Rule walking: first match wins.
	for i := range cfg.Rules {
		rule := cfg.Rules[i]
		if ruleMatches(rule, in, classes) {
			return routedRouteDecision(rule, classes, shadow, RouteReasonRuleMatched, decidedAt)
		}
	}
	return RouteDecision{
		Classes:   classes,
		Shadow:    shadow,
		Fallback:  true,
		Reason:    RouteReasonNoMatch,
		DecidedAt: decidedAt,
	}
}

// routedRouteDecision packages a matched rule's planners and ladder
// into a decision. Planners is copied so callers mutating the config
// cannot alter a returned decision.
func routedRouteDecision(rule config.PlanningRule, classes []string, shadow bool, reason string, decidedAt time.Time) RouteDecision {
	planners := make([]config.PlannerRef, len(rule.Planners))
	copy(planners, rule.Planners)
	ladder := rule.Ladder
	if ladder == "" {
		ladder = config.PlanningRoutingLadderNameDefault
	}
	return RouteDecision{
		RuleName:   rule.Name,
		Classes:    classes,
		Planners:   planners,
		LadderName: ladder,
		Shadow:     shadow,
		Reason:     reason,
		DecidedAt:  decidedAt,
	}
}

// resolvedRiskClasses returns the sorted names of every risk class
// (built-ins plus configured overrides) whose tags or path globs the
// input satisfies. Deterministic: iteration order comes from the
// sorted name list, never from a map.
func resolvedRiskClasses(cfg config.PlanningRoutingConfig, in RouteInput) []string {
	merged := map[string]config.RiskClass{}
	for name, class := range config.BuiltInRiskClasses() {
		merged[name] = class
	}
	for name, class := range cfg.Classes {
		merged[name] = class
	}
	names := make([]string, 0, len(merged))
	for name := range merged {
		names = append(names, name)
	}
	sort.Strings(names)
	out := []string{}
	for _, name := range names {
		if riskClassMatches(merged[name], in) {
			out = append(out, name)
		}
	}
	return out
}

// riskClassMatches reports whether any of the class's tags appears in
// the input's tags or any input file matches one of its path globs.
func riskClassMatches(class config.RiskClass, in RouteInput) bool {
	for _, tag := range class.Tags {
		if tag != "" && routingContains(in.Tags, tag) {
			return true
		}
	}
	for _, glob := range class.PathGlobs {
		if glob == "" {
			continue
		}
		for _, file := range in.Files {
			if riskGlobMatches(glob, file) {
				return true
			}
		}
	}
	return false
}

// riskGlobMatches applies one path glob: a glob ending in "/**"
// matches any path under that prefix; every other glob uses
// path.Match.
func riskGlobMatches(glob, file string) bool {
	if strings.HasSuffix(glob, "/**") {
		prefix := strings.TrimSuffix(glob, "/**")
		return strings.HasPrefix(file, prefix+"/") && file != prefix
	}
	ok, err := path.Match(glob, file)
	return err == nil && ok
}

// ruleMatches reports whether every non-empty match field of the rule
// passes. Empty list fields are wildcards; the spec-size bounds are
// unbounded at 0.
func ruleMatches(rule config.PlanningRule, in RouteInput, matchedClasses []string) bool {
	m := rule.Match
	if len(m.Kind) > 0 && !routingContains(m.Kind, in.Kind) {
		return false
	}
	if len(m.Tags) > 0 {
		hit := false
		for _, tag := range m.Tags {
			if tag != "" && routingContains(in.Tags, tag) {
				hit = true
			}
		}
		if !hit {
			return false
		}
	}
	if len(m.Project) > 0 && !routingContains(m.Project, in.Project) {
		return false
	}
	if len(m.RiskClass) > 0 {
		hit := false
		for _, name := range m.RiskClass {
			if name != "" && routingContains(matchedClasses, name) {
				hit = true
			}
		}
		if !hit {
			return false
		}
	}
	if m.SpecSizeMin > 0 && in.SpecBytes < m.SpecSizeMin {
		return false
	}
	if m.SpecSizeMax > 0 && in.SpecBytes > m.SpecSizeMax {
		return false
	}
	if m.Override != "" && m.Override != in.Override {
		return false
	}
	return true
}

// splitBackendModel splits an override of the form "backend:model"
// into its two non-empty parts (the first colon separates them, so a
// model may itself carry one, e.g. "ollama/qwen3.8:27b").
func splitBackendModel(s string) (string, string, bool) {
	backend, model, found := strings.Cut(s, ":")
	return backend, model, found && backend != "" && model != ""
}

// routingContains reports whether want is in have.
func routingContains(have []string, want string) bool {
	for _, v := range have {
		if v == want {
			return true
		}
	}
	return false
}
