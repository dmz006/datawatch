package autonomous

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// PrioritySource values for PRD.PrioritySource: where a PRD's
// capacity-queue priority came from.
const (
	// PrioritySourceExplicit: the operator pinned the priority with
	// SetPRDPriority. An explicit 0 still counts as explicit and wins.
	PrioritySourceExplicit = "explicit"
	// PrioritySourceKind: the priority was resolved from the
	// priority_by_kind table (config autonomous.priority_by_kind) via the
	// PRD's effective kind.
	PrioritySourceKind = "kind"
	// PrioritySourceDefault: no explicit priority and no table entry for
	// the kind or its "other" fallback; the priority is 0.
	PrioritySourceDefault = "default"
)

// ResolvePriority returns the capacity-queue priority for p and the source
// string that labels it: "explicit", "kind" or "default".
//
//   - A PRD whose priority was set explicitly (SetPRDPriority) always wins,
//     even when the explicit value is 0 or the table holds a higher one.
//   - Otherwise the PRD's effective kind (EffectiveKind, with no backlog
//     rows) is looked up in cfg — "maintenance" is its own key, like
//     bug/feature/other.
//   - A kind that has no entry of its own falls back to the table's
//     "other" entry.
//   - A cfg without an entry for the kind nor for "other" (including an
//     empty or nil map) yields (0, "default").
//
// The function is pure: it reads p's fields and the table only, and mutates
// neither. Callers that persist a resolved value (Manager.CreatePRD,
// Manager.SetPRDPriority) also record the source on the PRD so the choice
// stays auditable on the record.
func ResolvePriority(p *PRD, cfg map[string]int) (int, string) {
	if p != nil && p.PrioritySource == PrioritySourceExplicit {
		return p.Priority, PrioritySourceExplicit
	}
	kind := PRDKindFeature
	if p != nil {
		kind = EffectiveKind(p, nil)
	}
	if v, ok := cfg[kind]; ok {
		return v, PrioritySourceKind
	}
	if v, ok := cfg[PRDKindOther]; ok {
		return v, PrioritySourceKind
	}
	return 0, PrioritySourceDefault
}

// ParsePriorityByKindValue validates a value destined for
// autonomous.priority_by_kind (the kind→capacity-priority table) and returns
// the table it represents. Accepted shapes:
//
//   - an object: {"bug":80,"feature":0} (REST/PWA raw JSON),
//   - that object as a string: '{"bug":80}' — CLI `config set` and the
//     comm-channel `configure` verb quote their values —
//   - a compact list: "bug=80,feature=0".
//
// Keys are kind names: any non-empty string is accepted. The shipped kinds
// are feature, bug, other and maintenance, and an installation may add kinds
// of its own (ResolvePriority falls back to the "other" entry). Every value
// must be an integer >= 0; non-numeric, non-integer and boolean values are
// refused with a clear message. An empty object or an empty string is a
// valid "clear the table" edit and returns an empty non-nil map so the clear
// round-trips (the config field has no omitempty on purpose).
func ParsePriorityByKindValue(v any) (map[string]int, error) {
	var raw map[string]any
	switch x := v.(type) {
	case map[string]any:
		raw = x
	case nil:
		return map[string]int{}, nil
	case string:
		s := strings.TrimSpace(x)
		if strings.HasPrefix(s, "{") {
			dec := json.NewDecoder(strings.NewReader(s))
			dec.UseNumber()
			if err := dec.Decode(&raw); err != nil {
				return nil, fmt.Errorf("priority_by_kind: malformed JSON %q: %v", s, err)
			}
		} else if s == "" {
			return map[string]int{}, nil
		} else {
			raw = map[string]any{}
			for _, part := range strings.Split(s, ",") {
				kv := strings.SplitN(strings.TrimSpace(part), "=", 2)
				if len(kv) != 2 || strings.TrimSpace(kv[0]) == "" {
					return nil, fmt.Errorf("priority_by_kind: expected an object, a JSON object string, or \"kind=priority[,kind=priority]\"; got %q", strings.TrimSpace(part))
				}
				raw[strings.TrimSpace(kv[0])] = strings.TrimSpace(kv[1])
			}
		}
	default:
		return nil, fmt.Errorf("priority_by_kind: expected an object mapping kind names to integer priorities ({\"bug\":80,\"feature\":0}), a JSON object string, or \"kind=priority\" pairs; got %T", v)
	}
	out := make(map[string]int, len(raw))
	for k, val := range raw {
		if strings.TrimSpace(k) == "" {
			return nil, fmt.Errorf("priority_by_kind: empty kind name (kind names must not be empty)")
		}
		n, verr := priorityByKindInteger(val)
		if verr != nil {
			return nil, fmt.Errorf("priority_by_kind: priority for kind %q %v", k, verr)
		}
		out[k] = n
	}
	return out, nil
}

// priorityByKindInteger is one table value: an integer >= 0.
func priorityByKindInteger(val any) (int, error) {
	switch n := val.(type) {
	case int:
		if n < 0 {
			return 0, fmt.Errorf("must be an integer >= 0")
		}
		return n, nil
	case json.Number:
		iv, err := n.Int64()
		if err != nil {
			return 0, fmt.Errorf("must be an integer >= 0; got %s", n.String())
		}
		if iv < 0 {
			return 0, fmt.Errorf("must be an integer >= 0")
		}
		return int(iv), nil
	case float64:
		if n < 0 || n != math.Trunc(n) {
			return 0, fmt.Errorf("must be an integer (got %v, which is not an integer)", n)
		}
		return int(n), nil
	case string:
		iv, err := strconv.Atoi(strings.TrimSpace(n))
		if err != nil {
			return 0, fmt.Errorf("must be an integer (got %q, which is not a number)", strings.TrimSpace(n))
		}
		if iv < 0 {
			return 0, fmt.Errorf("must be an integer >= 0 (got %d)", iv)
		}
		return iv, nil
	case bool:
		return 0, fmt.Errorf("must be an integer priority, not a boolean")
	default:
		return 0, fmt.Errorf("must be an integer priority (got %T)", val)
	}
}
