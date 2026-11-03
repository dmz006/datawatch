package autonomous

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
