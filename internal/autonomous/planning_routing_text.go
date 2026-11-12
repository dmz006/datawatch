package autonomous

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/dmz006/datawatch/internal/config"
)

// PlanningRoutingView is the body of GET /api/autonomous/planning-routing.
type PlanningRoutingView struct {
	Enabled    bool                             `json:"enabled"`
	Shadow     bool                             `json:"shadow"`
	Rules      []config.PlanningRule            `json:"rules"`
	Ladders    map[string]config.PlanningLadder `json:"ladders"`
	Classes    map[string]config.RiskClass      `json:"classes"`
	Validation struct {
		Valid bool   `json:"valid"`
		Error string `json:"error"`
	} `json:"validation"`
}

// PlanningRouteDecisionView is the body of POST
// /api/autonomous/planning-routing/test.
type PlanningRouteDecisionView struct {
	Enabled  bool                `json:"enabled"`
	Rule     string              `json:"rule"`
	Classes  []string            `json:"classes"`
	Planners []config.PlannerRef `json:"planners"`
	Ladder   string              `json:"ladder"`
	Shadow   bool                `json:"shadow"`
	Fallback bool                `json:"fallback"`
	Reason   string              `json:"reason"`
}

// ParsePlanningRoutingView decodes a planning routing GET body.
func ParsePlanningRoutingView(b []byte) (PlanningRoutingView, error) {
	var v PlanningRoutingView
	err := json.Unmarshal(b, &v)
	return v, err
}

// ParsePlanningRouteDecisionView decodes a planning routing test body.
func ParsePlanningRouteDecisionView(b []byte) (PlanningRouteDecisionView, error) {
	var v PlanningRouteDecisionView
	err := json.Unmarshal(b, &v)
	return v, err
}

// ParsePlanningScorecardReport returns the rendered gate report from a
// scorecard body.
func ParsePlanningScorecardReport(b []byte) (string, error) {
	var v struct {
		Report string `json:"report"`
	}
	if err := json.Unmarshal(b, &v); err != nil {
		return "", err
	}
	return strings.TrimRight(v.Report, "\n"), nil
}

// RenderPlanningRoutingView renders the config as text for the comm and CLI
// surfaces.
func RenderPlanningRoutingView(b PlanningRoutingView) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Planning routing: enabled=%t shadow=%t\n", b.Enabled, b.Shadow)
	if b.Validation.Valid {
		sb.WriteString("Config: valid\n")
	} else {
		fmt.Fprintf(&sb, "Config: INVALID: %s\n", b.Validation.Error)
	}
	if len(b.Rules) == 0 {
		sb.WriteString("Rules: none\n")
	}
	for i, rule := range b.Rules {
		var ps []string
		for _, p := range rule.Planners {
			ps = append(ps, p.Backend+"/"+p.Model)
		}
		ladder := rule.Ladder
		if ladder == "" {
			ladder = config.PlanningRoutingLadderNameDefault
		}
		fmt.Fprintf(&sb, "Rule %d %s: planners=%s ladder=%s\n", i+1, rule.Name, strings.Join(ps, ","), ladder)
	}
	names := make([]string, 0, len(b.Ladders))
	for n := range b.Ladders {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		l := b.Ladders[n]
		fmt.Fprintf(&sb, "Ladder %s: touch_up_max=%d replan_max=%d escalate_after_attempts=%d\n", n, l.TouchUpMax, l.ReplanMax, l.EscalateAfterAttempts)
	}
	classes := make([]string, 0, len(b.Classes))
	for n := range b.Classes {
		classes = append(classes, n)
	}
	sort.Strings(classes)
	if len(classes) > 0 {
		fmt.Fprintf(&sb, "Classes: %s\n", strings.Join(classes, ", "))
	}
	return strings.TrimRight(sb.String(), "\n")
}

// RenderPlanningRouteDecisionView renders one routing decision as text.
func RenderPlanningRouteDecisionView(d PlanningRouteDecisionView) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Routing enabled: %t\n", d.Enabled)
	fmt.Fprintf(&sb, "Reason: %s\n", d.Reason)
	if d.Fallback {
		sb.WriteString("Planner: global planning backend and model (fallback)\n")
	} else {
		var ps []string
		for _, p := range d.Planners {
			ps = append(ps, p.Backend+"/"+p.Model)
		}
		fmt.Fprintf(&sb, "Planners: %s\n", strings.Join(ps, " -> "))
		fmt.Fprintf(&sb, "Ladder: %s\n", d.Ladder)
	}
	if d.Rule != "" {
		fmt.Fprintf(&sb, "Rule: %s\n", d.Rule)
	}
	if len(d.Classes) > 0 {
		fmt.Fprintf(&sb, "Classes: %s\n", strings.Join(d.Classes, ", "))
	}
	if d.Shadow {
		sb.WriteString("Shadow: decision recorded only, planner unchanged\n")
	}
	return strings.TrimRight(sb.String(), "\n")
}
