package server

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/dmz006/datawatch/internal/inference"
)

// planTierAllocationKey is the config key of the notional dollar allocation
// table; the single-tier pair form appends ".<tier>" to it.
const planTierAllocationKey = "autonomous.plan_tier_allocation_usd"

// planTierAllocationPairTier reports whether k is the single-tier pair form
// (autonomous.plan_tier_allocation_usd.<tier>) and returns the tier part.
func planTierAllocationPairTier(k string) (string, bool) {
	tier, ok := strings.CutPrefix(k, planTierAllocationKey+".")
	return tier, ok
}

// planTierAllocationForDisplay renders the tier -> dollar table for the
// config surface. A nil table renders as an empty object rather than null.
func planTierAllocationForDisplay(table map[string]float64) map[string]float64 {
	if table == nil {
		return map[string]float64{}
	}
	return table
}

// parsePlanTierAllocationValue validates one dollar figure: a finite number
// >= 0, given as a JSON number or a numeric string (0 = not declared).
func parsePlanTierAllocationValue(tier string, v any) (float64, error) {
	var f float64
	switch n := v.(type) {
	case float64:
		f = n
	case int:
		f = float64(n)
	case json.Number:
		parsed, err := n.Float64()
		if err != nil {
			return 0, fmt.Errorf("plan_tier_allocation_usd: value for %q must be a number >= 0; got %v", tier, v)
		}
		f = parsed
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(n), 64)
		if err != nil {
			return 0, fmt.Errorf("plan_tier_allocation_usd: value for %q must be a number >= 0; got %q", tier, n)
		}
		f = parsed
	case bool:
		return 0, fmt.Errorf("plan_tier_allocation_usd: value for %q must be a number >= 0, not a boolean", tier)
	default:
		return 0, fmt.Errorf("plan_tier_allocation_usd: value for %q must be a number >= 0; got %v", tier, v)
	}
	if f < 0 || math.IsNaN(f) || math.IsInf(f, 0) {
		return 0, fmt.Errorf("plan_tier_allocation_usd: value for %q must be a finite number >= 0", tier)
	}
	return f, nil
}

// parsePlanTierAllocationPair validates the single-tier pair form: the tier
// taken from the key suffix and the dollar figure from the value.
func parsePlanTierAllocationPair(tier string, v any) (float64, error) {
	if !inference.ValidPlanTier(inference.PlanTier(tier)) {
		return 0, fmt.Errorf("plan_tier_allocation_usd: unknown tier %q (allowed: %s, %s, %s, %s)",
			tier, inference.PlanTierPro, inference.PlanTierMax5x, inference.PlanTierMax20x, inference.PlanTierAPI)
	}
	return parsePlanTierAllocationValue(tier, v)
}

// parsePlanTierAllocationTable validates a value destined for
// autonomous.plan_tier_allocation_usd and returns the table it represents.
// Accepted shapes mirror parsePlanTierTable: an object, that object as a
// JSON string, or a compact "pro=20,max_5x=100" list. Every key must be a
// known tier and every value a finite number >= 0. Empty input clears the
// table and returns an empty non-nil map.
func parsePlanTierAllocationTable(v any) (map[string]float64, error) {
	var raw map[string]any
	switch x := v.(type) {
	case map[string]any:
		raw = x
	case nil:
		return map[string]float64{}, nil
	case string:
		s := strings.TrimSpace(x)
		switch {
		case s == "":
			return map[string]float64{}, nil
		case strings.HasPrefix(s, "{"):
			if err := json.Unmarshal([]byte(s), &raw); err != nil {
				return nil, fmt.Errorf("plan_tier_allocation_usd: malformed JSON %q: %v", s, err)
			}
		default:
			raw = map[string]any{}
			for _, part := range strings.Split(s, ",") {
				kv := strings.SplitN(strings.TrimSpace(part), "=", 2)
				if len(kv) != 2 || strings.TrimSpace(kv[0]) == "" || strings.TrimSpace(kv[1]) == "" {
					return nil, fmt.Errorf("plan_tier_allocation_usd: expected an object, a JSON object string, or \"tier=usd,tier=usd\"; got %q", strings.TrimSpace(part))
				}
				raw[strings.TrimSpace(kv[0])] = strings.TrimSpace(kv[1])
			}
		}
	default:
		return nil, fmt.Errorf("plan_tier_allocation_usd: expected an object (or a JSON object string); got %T", v)
	}
	out := make(map[string]float64, len(raw))
	for tier, val := range raw {
		f, err := parsePlanTierAllocationPair(tier, val)
		if err != nil {
			return nil, err
		}
		out[tier] = f
	}
	return out, nil
}
