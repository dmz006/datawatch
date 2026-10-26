package autonomous

import "sort"

// automationMemberTags returns the union of the tag IDs carried by the member
// Automata of a, sorted. It returns nil when no member Automaton carries a
// tag, so the view omits the field. An Automation never stores tags of its
// own; this is computed from the members on each read.
func (m *Manager) automationMemberTags(a *Automation) []string {
	if a == nil {
		return nil
	}
	set := map[string]bool{}
	for _, mem := range a.Members {
		if mem.PRDID == "" {
			continue
		}
		prd, ok := m.store.GetPRD(mem.PRDID)
		if !ok {
			continue
		}
		for _, t := range prd.TagIDs {
			set[t] = true
		}
	}
	if len(set) == 0 {
		return nil
	}
	out := make([]string, 0, len(set))
	for t := range set {
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}
